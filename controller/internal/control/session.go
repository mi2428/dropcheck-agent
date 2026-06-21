package control

import (
	"fmt"
	"io"
	"time"

	"dropcheck/controller/internal/controlpb"
)

// Session implements the bidirectional gRPC stream used by Android agents.
//
// The first frame must contain an AgentHello with the session token. After
// authentication, the stream carries controller heartbeats and commands in one
// direction and agent logs/results/errors in the other.
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

	// Prefer caller-controlled IDs, then adb serials, and finally the generated
	// stream session ID. This makes command targeting stable when the Android
	// service reconnects, while still supporting devices that cannot report a
	// serial.
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
		// A reconnect with the same agent ID supersedes the old stream. Closing
		// the old connection wakes any sender goroutine and causes in-flight
		// waiters to receive a disconnect error from that stream's cleanup.
		previous.close()
	}
	s.conns[agentID] = conn
	s.infos[agentID] = info
	s.mu.Unlock()

	select {
	case s.ready <- info:
	default:
		// Readiness is advisory. If the buffer is full, WaitAgents will still
		// observe the agent through the protected infos map on its next loop.
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
				// Heartbeats keep the bidirectional stream active even when no
				// command is running and give the agent a controller timestamp.
				frame := &controlpb.ControllerFrame{
					Seq:       s.seq.Add(1),
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
		// Every in-flight command for this stream must be completed exactly once.
		// Without this, callers waiting in Run could block until their context
		// deadline even though the transport has already ended.
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
