package control

import (
	"context"
	"fmt"
	"time"

	"github.com/mi2428/dropcheck-agent/controller/internal/controlpb"
)

// RunPinned sends cmd on the snapshot's original connection and waits for its
// terminal result. Snapshot validation and connection selection share one lock.
// commandID must be unique among in-flight commands. Cancel delivery while
// awaiting a result is best-effort and has a 100ms budget.
func (s *Server) RunPinned(ctx context.Context, agent AgentInfo, commandID string, cmd *controlpb.RunCommand) (*controlpb.CommandResult, error) {
	if agent.ID == "" || agent.SessionID == "" {
		return nil, fmt.Errorf("bound agent identity is incomplete")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	respCh := make(chan CommandResponse, 1)

	s.mu.Lock()
	conn := s.conns[agent.ID]
	if conn == nil {
		s.mu.Unlock()
		return nil, fmt.Errorf("agent %q is not connected", agent.ID)
	}
	if conn.sessionID != agent.SessionID || !agent.Connected.IsZero() && !s.infos[agent.ID].Connected.Equal(agent.Connected) {
		s.mu.Unlock()
		return nil, fmt.Errorf("selected agent connection was replaced; select it again")
	}
	if _, exists := s.waiters[commandID]; exists {
		s.mu.Unlock()
		return nil, fmt.Errorf("command %q is already running", commandID)
	}
	if err := ctx.Err(); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	// Register the waiter before enqueueing the frame so an immediate result
	// from the agent cannot race ahead of the receiver setup.
	s.waiters[commandID] = commandWaiter{conn: conn, ch: respCh}
	frame := &controlpb.ControllerFrame{
		Seq:       s.seq.Add(1),
		SessionId: conn.sessionID,
		CommandId: commandID,
		Body: &controlpb.ControllerFrame_RunCommand{
			RunCommand: cmd,
		},
	}
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		if s.waiters[commandID].ch == respCh {
			delete(s.waiters, commandID)
		}
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
		cancelCtx, stop := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer stop()
		_ = s.cancelConn(cancelCtx, conn, commandID, "controller command context ended")
		return nil, ctx.Err()
	}
}

func (s *Server) cancelConn(ctx context.Context, conn *agentConn, commandID string, reason string) error {
	frame := &controlpb.ControllerFrame{
		Seq:       s.seq.Add(1),
		SessionId: conn.sessionID,
		CommandId: commandID,
		Body: &controlpb.ControllerFrame_CancelCommand{
			CancelCommand: &controlpb.CancelCommand{Reason: reason},
		},
	}

	select {
	case conn.sendCh <- frame:
		return nil
	case <-conn.done:
		return fmt.Errorf("agent disconnected")
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Server) deliver(conn *agentConn, commandID string, resp CommandResponse) {
	s.mu.Lock()
	waiter := s.waiters[commandID]
	if waiter.conn != conn || s.conns[conn.id] != conn {
		s.mu.Unlock()
		return
	}
	select {
	case waiter.ch <- resp:
	default:
		// The response channel is buffered and RunPinned only needs one terminal
		// response. Drop duplicates from retries or late frames after cleanup.
	}
	s.mu.Unlock()
}
