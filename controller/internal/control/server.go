package control

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"dropcheck/controller/internal/controlpb"
	"google.golang.org/grpc"
)

// AgentInfo is the controller's current view of one connected Android agent.
type AgentInfo struct {
	// ID is the stable identifier used for command routing.
	ID string
	// SessionID is the gRPC session identifier reported by the agent.
	SessionID string
	// Hello is the authenticated hello frame that described the agent.
	Hello *controlpb.AgentHello
	// Connected is the time the controller accepted the session.
	Connected time.Time
}

// LogEvent is an agent log entry normalized for application-level handlers.
type LogEvent struct {
	// AgentID identifies the connected agent that emitted the log.
	AgentID string
	// SessionID identifies the gRPC stream that carried the log.
	SessionID string
	// CommandID is set when the log belongs to a running command.
	CommandID string
	// Level is the protobuf log severity.
	Level controlpb.CommandLog_Level
	// Message is the human-readable log message.
	Message string
	// Time is the agent-provided timestamp, or controller time when absent.
	Time time.Time
}

// CommandResponse is the internal delivery envelope for an agent command.
type CommandResponse struct {
	// Result is set when the agent completed the command normally.
	Result *controlpb.CommandResult
	// Error is set when the agent rejected or failed the command.
	Error *controlpb.CommandError
}

// Server tracks authenticated agents and dispatches commands over gRPC.
//
// Server is safe for concurrent use by the app shell, CLI execution path, and
// gRPC stream handlers.
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

// NewServer creates a control server that accepts agents presenting token.
//
// onLog is optional. When provided, it is called from gRPC handling goroutines
// for agent logs and command acknowledgements.
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

// Register registers s with a grpc.Server.
func (s *Server) Register(server *grpc.Server) {
	controlpb.RegisterDropcheckControlServer(server, s)
}

// WaitAgent waits for at least one agent and returns the first sorted agent.
//
// If an agent is already connected, WaitAgent returns immediately. Otherwise it
// blocks until an agent connects or ctx is canceled.
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

// WaitAgents waits until count agents are connected.
//
// When ctx is canceled before count agents connect, WaitAgents returns the
// agents currently connected together with the context error.
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

// Agents returns the currently connected agents in stable display order.
func (s *Server) Agents() []AgentInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.agentListLocked()
}

// ResolveAgent resolves target to one connected agent.
//
// target may be an exact or unique-prefix match for agent ID, session ID, adb
// serial, or the controller agent ID reported in the hello frame.
func (s *Server) ResolveAgent(target string) (AgentInfo, error) {
	needle := strings.TrimSpace(target)
	if needle == "" {
		return AgentInfo{}, fmt.Errorf("agent target is empty")
	}
	agents := s.Agents()
	var exact []AgentInfo
	for _, info := range agents {
		if agentMatches(info, needle) {
			exact = append(exact, info)
		}
	}
	switch len(exact) {
	case 1:
		return exact[0], nil
	case 0:
	default:
		return AgentInfo{}, fmt.Errorf("agent %q is ambiguous", target)
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
	normalized := normalizeAgentSelector(target)
	for _, value := range agentSelectorValues(info) {
		if value == target || normalized != "" && normalizeAgentSelector(value) == normalized {
			return true
		}
	}
	return false
}

func agentPrefixMatches(info AgentInfo, target string) bool {
	normalized := normalizeAgentSelector(target)
	for _, value := range agentSelectorValues(info) {
		if stringsHasPrefix(value, target) || normalized != "" && strings.HasPrefix(normalizeAgentSelector(value), normalized) {
			return true
		}
	}
	return false
}

func agentSelectorValues(info AgentInfo) []string {
	device := info.Hello.GetDevice()
	deviceName := device.GetModel()
	manufacturerModel := stringsTrimJoin(device.GetManufacturer(), device.GetModel())
	return []string{
		info.ID,
		info.SessionID,
		info.Hello.GetAdbSerial(),
		info.Hello.GetControllerAgentId(),
		deviceName,
		manufacturerModel,
		device.GetDevice(),
	}
}

func stringsHasPrefix(value string, prefix string) bool {
	return value != "" && len(prefix) > 0 && len(value) >= len(prefix) && value[:len(prefix)] == prefix
}

func normalizeAgentSelector(value string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(value)) {
		if unicode.IsSpace(r) || r == '-' || r == '_' || r == '(' || r == ')' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func stringsTrimJoin(parts ...string) string {
	var values []string
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			values = append(values, part)
		}
	}
	return strings.Join(values, " ")
}
