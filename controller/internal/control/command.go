package control

import (
	"context"
	"fmt"
	"sync/atomic"

	"dropcheck/controller/internal/controlpb"
)

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
