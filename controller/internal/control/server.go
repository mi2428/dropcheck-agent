package control

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"dropcheck/controller/internal/controlpb"
	"google.golang.org/grpc"
)

type AgentInfo struct {
	ID        string
	SessionID string
	Hello     *controlpb.AgentHello
	Connected time.Time
}

type LogEvent struct {
	AgentID   string
	SessionID string
	CommandID string
	Level     controlpb.CommandLog_Level
	Message   string
	Time      time.Time
}

type CommandResponse struct {
	Result *controlpb.CommandResult
	Error  *controlpb.CommandError
}

type Server struct {
	controlpb.UnimplementedDropcheckControlServer

	token string
	onLog func(LogEvent)

	seq uint64

	mu      sync.Mutex
	conns   map[string]*agentConn
	infos   map[string]AgentInfo
	waiters map[string]commandWaiter
	ready   chan AgentInfo
}

type agentConn struct {
	id        string
	sessionID string
	sendCh    chan *controlpb.ControllerFrame
	done      chan struct{}
	closeOnce sync.Once
}

type commandWaiter struct {
	agentID string
	ch      chan CommandResponse
}

func (c *agentConn) close() {
	c.closeOnce.Do(func() {
		close(c.done)
	})
}

func NewServer(token string, onLog func(LogEvent)) *Server {
	return &Server{
		token:   token,
		onLog:   onLog,
		conns:   make(map[string]*agentConn),
		infos:   make(map[string]AgentInfo),
		waiters: make(map[string]commandWaiter),
		ready:   make(chan AgentInfo, 32),
	}
}

func (s *Server) Register(server *grpc.Server) {
	controlpb.RegisterDropcheckControlServer(server, s)
}

func (s *Server) WaitAgent(ctx context.Context) (AgentInfo, error) {
	s.mu.Lock()
	agents := s.agentListLocked()
	if len(agents) > 0 {
		info := agents[0]
		s.mu.Unlock()
		return info, nil
	}
	s.mu.Unlock()

	select {
	case info := <-s.ready:
		return info, nil
	case <-ctx.Done():
		return AgentInfo{}, ctx.Err()
	}
}

func (s *Server) WaitAgents(ctx context.Context, count int) ([]AgentInfo, error) {
	if count <= 0 {
		return nil, nil
	}
	for {
		s.mu.Lock()
		agents := s.agentListLocked()
		if len(agents) >= count {
			s.mu.Unlock()
			return agents, nil
		}
		s.mu.Unlock()

		select {
		case <-s.ready:
		case <-ctx.Done():
			return s.Agents(), ctx.Err()
		}
	}
}

func (s *Server) Agents() []AgentInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.agentListLocked()
}

func (s *Server) ResolveAgent(target string) (AgentInfo, error) {
	needle := target
	if needle == "" {
		return AgentInfo{}, fmt.Errorf("agent target is empty")
	}
	agents := s.Agents()
	for _, info := range agents {
		if agentMatches(info, needle) {
			return info, nil
		}
	}
	var matched []AgentInfo
	for _, info := range agents {
		if agentPrefixMatches(info, needle) {
			matched = append(matched, info)
		}
	}
	switch len(matched) {
	case 0:
		return AgentInfo{}, fmt.Errorf("agent %q is not connected", target)
	case 1:
		return matched[0], nil
	default:
		return AgentInfo{}, fmt.Errorf("agent %q is ambiguous", target)
	}
}

func (s *Server) Run(ctx context.Context, agentID string, commandID string, cmd *controlpb.RunCommand) (*controlpb.CommandResult, error) {
	respCh := make(chan CommandResponse, 1)

	s.mu.Lock()
	conn := s.conns[agentID]
	if conn == nil {
		s.mu.Unlock()
		return nil, fmt.Errorf("agent %q is not connected", agentID)
	}
	s.waiters[commandID] = commandWaiter{agentID: agentID, ch: respCh}
	frame := &controlpb.ControllerFrame{
		Seq:       atomic.AddUint64(&s.seq, 1),
		SessionId: conn.sessionID,
		CommandId: commandID,
		Body: &controlpb.ControllerFrame_RunCommand{
			RunCommand: cmd,
		},
	}
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.waiters, commandID)
		s.mu.Unlock()
	}()

	select {
	case conn.sendCh <- frame:
	case <-conn.done:
		return nil, fmt.Errorf("agent disconnected")
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	select {
	case resp := <-respCh:
		if resp.Error != nil {
			return nil, fmt.Errorf("%s: %s", resp.Error.Message, resp.Error.Detail)
		}
		if resp.Result == nil {
			return nil, fmt.Errorf("agent returned an empty response")
		}
		return resp.Result, nil
	case <-conn.done:
		return nil, fmt.Errorf("agent disconnected")
	case <-ctx.Done():
		_ = s.Cancel(context.Background(), agentID, commandID, "controller command context ended")
		return nil, ctx.Err()
	}
}

func (s *Server) Cancel(ctx context.Context, agentID string, commandID string, reason string) error {
	s.mu.Lock()
	conn := s.conns[agentID]
	if conn == nil {
		s.mu.Unlock()
		return fmt.Errorf("agent %q is not connected", agentID)
	}
	frame := &controlpb.ControllerFrame{
		Seq:       atomic.AddUint64(&s.seq, 1),
		SessionId: conn.sessionID,
		CommandId: commandID,
		Body: &controlpb.ControllerFrame_CancelCommand{
			CancelCommand: &controlpb.CancelCommand{Reason: reason},
		},
	}
	s.mu.Unlock()

	select {
	case conn.sendCh <- frame:
		return nil
	case <-conn.done:
		return fmt.Errorf("agent disconnected")
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Server) Session(stream controlpb.DropcheckControl_SessionServer) error {
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	hello := first.GetHello()
	if hello == nil {
		return fmt.Errorf("agent did not send hello")
	}
	if hello.Token != s.token {
		return fmt.Errorf("agent hello token mismatch")
	}

	sessionID := first.GetSessionId()
	if sessionID == "" {
		sessionID = "agent-" + time.Now().Format("20060102-150405.000")
	}
	agentID := hello.GetControllerAgentId()
	if agentID == "" {
		agentID = hello.GetAdbSerial()
	}
	if agentID == "" {
		agentID = sessionID
	}
	conn := &agentConn{
		id:        agentID,
		sessionID: sessionID,
		sendCh:    make(chan *controlpb.ControllerFrame, 16),
		done:      make(chan struct{}),
	}
	info := AgentInfo{ID: agentID, SessionID: sessionID, Hello: hello, Connected: time.Now()}

	s.mu.Lock()
	if previous := s.conns[agentID]; previous != nil {
		previous.close()
	}
	s.conns[agentID] = conn
	s.infos[agentID] = info
	s.mu.Unlock()

	select {
	case s.ready <- info:
	default:
	}

	sendDone := make(chan error, 1)
	go func() {
		heartbeat := time.NewTicker(time.Second)
		defer heartbeat.Stop()
		for {
			select {
			case frame := <-conn.sendCh:
				if err := stream.Send(frame); err != nil {
					sendDone <- err
					return
				}
			case tick := <-heartbeat.C:
				frame := &controlpb.ControllerFrame{
					Seq:       atomic.AddUint64(&s.seq, 1),
					SessionId: conn.sessionID,
					Body: &controlpb.ControllerFrame_Heartbeat{
						Heartbeat: &controlpb.ControllerHeartbeat{
							UnixTimeMs: tick.UnixMilli(),
						},
					},
				}
				if err := stream.Send(frame); err != nil {
					sendDone <- err
					return
				}
			case <-conn.done:
				sendDone <- nil
				return
			}
		}
	}()

	defer func() {
		s.mu.Lock()
		if s.conns[conn.id] == conn {
			delete(s.conns, conn.id)
			delete(s.infos, conn.id)
		}
		for id, waiter := range s.waiters {
			if waiter.agentID != conn.id {
				continue
			}
			delete(s.waiters, id)
			waiter.ch <- CommandResponse{Error: &controlpb.CommandError{
				Message: "agent disconnected",
				Detail:  "gRPC session ended",
			}}
		}
		s.mu.Unlock()
		conn.close()
	}()

	for {
		select {
		case err := <-sendDone:
			if err != nil {
				return err
			}
			return nil
		default:
		}

		msg, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		s.handleAgentFrame(conn, msg)
	}
}

func (s *Server) handleAgentFrame(conn *agentConn, frame *controlpb.AgentFrame) {
	switch body := frame.Body.(type) {
	case *controlpb.AgentFrame_Log:
		if s.onLog != nil {
			when := time.UnixMilli(body.Log.UnixTimeMs)
			if body.Log.UnixTimeMs == 0 {
				when = time.Now()
			}
			s.onLog(LogEvent{
				AgentID:   conn.id,
				SessionID: conn.sessionID,
				CommandID: frame.CommandId,
				Level:     body.Log.Level,
				Message:   body.Log.Message,
				Time:      when,
			})
		}
	case *controlpb.AgentFrame_Result:
		s.deliver(frame.CommandId, CommandResponse{Result: body.Result})
	case *controlpb.AgentFrame_Error:
		s.deliver(frame.CommandId, CommandResponse{Error: body.Error})
	case *controlpb.AgentFrame_Accepted:
		if s.onLog != nil {
			s.onLog(LogEvent{
				AgentID:   conn.id,
				SessionID: conn.sessionID,
				CommandID: frame.CommandId,
				Level:     controlpb.CommandLog_LEVEL_DEBUG,
				Message:   "accepted: " + body.Accepted.CommandName,
				Time:      time.Now(),
			})
		}
	case *controlpb.AgentFrame_Heartbeat:
		_ = body
	}
}

func (s *Server) deliver(commandID string, resp CommandResponse) {
	s.mu.Lock()
	waiter := s.waiters[commandID]
	s.mu.Unlock()
	if waiter.ch == nil {
		return
	}
	select {
	case waiter.ch <- resp:
	default:
	}
}

func (s *Server) agentListLocked() []AgentInfo {
	agents := make([]AgentInfo, 0, len(s.infos))
	for _, info := range s.infos {
		agents = append(agents, info)
	}
	sort.Slice(agents, func(i, j int) bool {
		return agentSortKey(agents[i]) < agentSortKey(agents[j])
	})
	return agents
}

func agentSortKey(info AgentInfo) string {
	if serial := info.Hello.GetAdbSerial(); serial != "" {
		return serial
	}
	return info.ID
}

func agentMatches(info AgentInfo, target string) bool {
	return info.ID == target ||
		info.SessionID == target ||
		info.Hello.GetAdbSerial() == target ||
		info.Hello.GetControllerAgentId() == target
}

func agentPrefixMatches(info AgentInfo, target string) bool {
	return stringsHasPrefix(info.ID, target) ||
		stringsHasPrefix(info.SessionID, target) ||
		stringsHasPrefix(info.Hello.GetAdbSerial(), target) ||
		stringsHasPrefix(info.Hello.GetControllerAgentId(), target)
}

func stringsHasPrefix(value string, prefix string) bool {
	return value != "" && len(prefix) > 0 && len(value) >= len(prefix) && value[:len(prefix)] == prefix
}

func RandomHex(bytes int) (string, error) {
	buf := make([]byte, bytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
