package control

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"dropcheck/controller/internal/controlpb"
	"google.golang.org/grpc/metadata"
)

func TestAgentsAreSortedAndResolvedByExactOrPrefix(t *testing.T) {
	server := NewServer("token", nil)
	addTestAgent(server, AgentInfo{
		ID:        "agent-b",
		SessionID: "session-b",
		Hello:     &controlpb.AgentHello{AdbSerial: "serial-b", ControllerAgentId: "controller-b"},
		Connected: time.Unix(2, 0),
	})
	addTestAgent(server, AgentInfo{
		ID:        "agent-a",
		SessionID: "session-a",
		Hello: &controlpb.AgentHello{
			AdbSerial:         "serial-a",
			ControllerAgentId: "controller-a",
			Device:            &controlpb.DeviceInfo{Manufacturer: "Google", Model: "Pixel 7a"},
		},
		Connected: time.Unix(1, 0),
	})

	agents := server.Agents()
	gotIDs := []string{agents[0].ID, agents[1].ID}
	if !slices.Equal(gotIDs, []string{"agent-a", "agent-b"}) {
		t.Fatalf("Agents IDs = %#v", gotIDs)
	}

	for _, target := range []string{"agent-a", "session-a", "serial-a", "controller-a", "Pixel 7a", "pixel7a", "serial-b"} {
		t.Run(target, func(t *testing.T) {
			info, err := server.ResolveAgent(target)
			if err != nil {
				t.Fatalf("ResolveAgent(%q) error = %v", target, err)
			}
			if target == "serial-b" {
				if info.ID != "agent-b" {
					t.Fatalf("ResolveAgent(%q) ID = %q", target, info.ID)
				}
				return
			}
			if info.ID != "agent-a" {
				t.Fatalf("ResolveAgent(%q) ID = %q", target, info.ID)
			}
		})
	}

	if _, err := server.ResolveAgent("serial-"); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("ResolveAgent(serial-) error = %v, want ambiguous", err)
	}
	if _, err := server.ResolveAgent("missing"); err == nil || !strings.Contains(err.Error(), "not connected") {
		t.Fatalf("ResolveAgent(missing) error = %v, want not connected", err)
	}
	if _, err := server.ResolveAgent(""); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("ResolveAgent(empty) error = %v, want empty", err)
	}
}

func TestWaitAgentsReturnsExistingAndContextPartial(t *testing.T) {
	server := NewServer("token", nil)
	addTestAgent(server, AgentInfo{
		ID:        "agent-a",
		SessionID: "session-a",
		Hello:     &controlpb.AgentHello{AdbSerial: "serial-a"},
	})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	agents, err := server.WaitAgents(ctx, 1)
	if err != nil {
		t.Fatalf("WaitAgents(existing) error = %v", err)
	}
	if len(agents) != 1 || agents[0].ID != "agent-a" {
		t.Fatalf("WaitAgents(existing) = %#v", agents)
	}

	ctx, cancel = context.WithCancel(context.Background())
	cancel()
	agents, err = server.WaitAgents(ctx, 2)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("WaitAgents(canceled) error = %v, want context.Canceled", err)
	}
	if len(agents) != 1 || agents[0].ID != "agent-a" {
		t.Fatalf("WaitAgents(canceled) = %#v, want partial connected agents", agents)
	}
}

func TestRunSendsCommandAndReceivesResult(t *testing.T) {
	server := NewServer("token", nil)
	conn := addTestConn(server, "agent-a", "session-a")
	cmd := &controlpb.RunCommand{
		Label: "wifi status",
		Command: &controlpb.RunCommand_GetWifiStatus{
			GetWifiStatus: &controlpb.GetWifiStatus{},
		},
	}
	wantResult := &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_OK, Message: "done"}

	done := make(chan struct {
		result *controlpb.CommandResult
		err    error
	}, 1)
	go func() {
		result, err := server.Run(context.Background(), "agent-a", "cmd-1", cmd)
		done <- struct {
			result *controlpb.CommandResult
			err    error
		}{result: result, err: err}
	}()

	frame := receiveControllerFrame(t, conn.sendCh)
	if frame.GetSessionId() != "session-a" || frame.GetCommandId() != "cmd-1" || frame.GetRunCommand() != cmd {
		t.Fatalf("Run frame = %#v", frame)
	}
	if frame.GetSeq() == 0 {
		t.Fatalf("Run frame seq = 0")
	}

	server.handleAgentFrame(conn, &controlpb.AgentFrame{
		CommandId: "cmd-1",
		Body:      &controlpb.AgentFrame_Result{Result: wantResult},
	})

	got := receiveRunResult(t, done)
	if got.err != nil {
		t.Fatalf("Run() error = %v", got.err)
	}
	if got.result != wantResult {
		t.Fatalf("Run() result = %#v, want %#v", got.result, wantResult)
	}
	if _, ok := server.waiters["cmd-1"]; ok {
		t.Fatalf("waiter was not cleaned up")
	}
}

func TestRunSendsCancelWhenContextEnds(t *testing.T) {
	server := NewServer("token", nil)
	conn := addTestConn(server, "agent-a", "session-a")
	cmd := &controlpb.RunCommand{
		Label: "wifi status",
		Command: &controlpb.RunCommand_GetWifiStatus{
			GetWifiStatus: &controlpb.GetWifiStatus{},
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct {
		result *controlpb.CommandResult
		err    error
	}, 1)
	go func() {
		result, err := server.Run(ctx, "agent-a", "cmd-1", cmd)
		done <- struct {
			result *controlpb.CommandResult
			err    error
		}{result: result, err: err}
	}()

	_ = receiveControllerFrame(t, conn.sendCh)
	cancel()
	cancelFrame := receiveControllerFrame(t, conn.sendCh)
	if cancelFrame.GetCommandId() != "cmd-1" || cancelFrame.GetCancelCommand().GetReason() != "controller command context ended" {
		t.Fatalf("cancel frame = %#v", cancelFrame)
	}

	got := receiveRunResult(t, done)
	if !errors.Is(got.err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled", got.err)
	}
	if got.result != nil {
		t.Fatalf("Run() result = %#v, want nil", got.result)
	}
}

func TestHandleAgentFrameLogsAndDeliversError(t *testing.T) {
	var events []LogEvent
	server := NewServer("token", func(event LogEvent) {
		events = append(events, event)
	})
	conn := &agentConn{id: "agent-a", sessionID: "session-a", sendCh: make(chan *controlpb.ControllerFrame), done: make(chan struct{})}
	respCh := make(chan CommandResponse, 1)
	server.waiters["cmd-1"] = commandWaiter{agentID: "agent-a", ch: respCh}

	server.handleAgentFrame(conn, &controlpb.AgentFrame{
		CommandId: "cmd-1",
		Body: &controlpb.AgentFrame_Log{Log: &controlpb.CommandLog{
			Level:      controlpb.CommandLog_LEVEL_WARN,
			Message:    "low signal",
			UnixTimeMs: 1234,
		}},
	})
	server.handleAgentFrame(conn, &controlpb.AgentFrame{
		CommandId: "cmd-1",
		Body:      &controlpb.AgentFrame_Accepted{Accepted: &controlpb.CommandAccepted{CommandName: "wifi status"}},
	})
	server.handleAgentFrame(conn, &controlpb.AgentFrame{
		CommandId: "cmd-1",
		Body:      &controlpb.AgentFrame_Error{Error: &controlpb.CommandError{Message: "failed", Detail: "detail"}},
	})

	if len(events) != 2 {
		t.Fatalf("events len = %d, want 2: %#v", len(events), events)
	}
	if events[0].AgentID != "agent-a" || events[0].SessionID != "session-a" || events[0].CommandID != "cmd-1" || events[0].Message != "low signal" || events[0].Level != controlpb.CommandLog_LEVEL_WARN || events[0].Time.UnixMilli() != 1234 {
		t.Fatalf("log event = %#v", events[0])
	}
	if events[1].Level != controlpb.CommandLog_LEVEL_DEBUG || events[1].Message != "accepted: wifi status" {
		t.Fatalf("accepted event = %#v", events[1])
	}

	select {
	case resp := <-respCh:
		if resp.Error.GetMessage() != "failed" || resp.Error.GetDetail() != "detail" {
			t.Fatalf("delivered response = %#v", resp)
		}
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for delivered error")
	}
}

func TestSessionAuthenticatesRegistersAndCleansUpWaiters(t *testing.T) {
	server := NewServer("token", nil)
	stream := newFakeSessionStream()
	stream.recvCh <- &controlpb.AgentFrame{
		SessionId: "session-a",
		Body: &controlpb.AgentFrame_Hello{Hello: &controlpb.AgentHello{
			Token:             "token",
			ControllerAgentId: "agent-a",
			AdbSerial:         "serial-a",
		}},
	}

	done := make(chan error, 1)
	go func() {
		done <- server.Session(stream)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	info, err := server.WaitAgent(ctx)
	if err != nil {
		t.Fatalf("WaitAgent() error = %v", err)
	}
	if info.ID != "agent-a" || info.SessionID != "session-a" || info.Hello.GetAdbSerial() != "serial-a" {
		t.Fatalf("registered agent = %#v", info)
	}

	respCh := make(chan CommandResponse, 1)
	server.mu.Lock()
	server.waiters["cmd-1"] = commandWaiter{agentID: "agent-a", ch: respCh}
	server.mu.Unlock()

	close(stream.recvCh)
	if err := receiveSessionError(t, done); err != nil {
		t.Fatalf("Session() error = %v", err)
	}
	if agents := server.Agents(); len(agents) != 0 {
		t.Fatalf("Agents() after disconnect = %#v, want empty", agents)
	}
	select {
	case resp := <-respCh:
		if resp.Error.GetMessage() != "agent disconnected" || resp.Error.GetDetail() != "gRPC session ended" {
			t.Fatalf("disconnect response = %#v", resp)
		}
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for disconnect response")
	}
}

func TestSessionRejectsMissingOrMismatchedHello(t *testing.T) {
	tests := []struct {
		name string
		msg  *controlpb.AgentFrame
		want string
	}{
		{
			name: "missing hello",
			msg:  &controlpb.AgentFrame{SessionId: "session-a"},
			want: "agent did not send hello",
		},
		{
			name: "token mismatch",
			msg: &controlpb.AgentFrame{
				SessionId: "session-a",
				Body:      &controlpb.AgentFrame_Hello{Hello: &controlpb.AgentHello{Token: "wrong"}},
			},
			want: "agent hello token mismatch",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := NewServer("token", nil)
			stream := newFakeSessionStream()
			stream.recvCh <- tt.msg
			close(stream.recvCh)

			err := server.Session(stream)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Session() error = %v, want %q", err, tt.want)
			}
			if agents := server.Agents(); len(agents) != 0 {
				t.Fatalf("Agents() = %#v, want empty", agents)
			}
		})
	}
}

func addTestAgent(server *Server, info AgentInfo) {
	server.mu.Lock()
	defer server.mu.Unlock()
	server.infos[info.ID] = info
}

func addTestConn(server *Server, agentID string, sessionID string) *agentConn {
	conn := &agentConn{
		id:        agentID,
		sessionID: sessionID,
		sendCh:    make(chan *controlpb.ControllerFrame, 2),
		done:      make(chan struct{}),
	}
	server.mu.Lock()
	server.conns[agentID] = conn
	server.infos[agentID] = AgentInfo{ID: agentID, SessionID: sessionID, Hello: &controlpb.AgentHello{AdbSerial: agentID}}
	server.mu.Unlock()
	return conn
}

func receiveControllerFrame(t *testing.T, ch <-chan *controlpb.ControllerFrame) *controlpb.ControllerFrame {
	t.Helper()
	select {
	case frame := <-ch:
		return frame
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for controller frame")
		return nil
	}
}

func receiveSessionError(t *testing.T, ch <-chan error) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for Session result")
		return nil
	}
}

type fakeSessionStream struct {
	recvCh chan *controlpb.AgentFrame
	sendCh chan *controlpb.ControllerFrame
	ctx    context.Context
}

func newFakeSessionStream() *fakeSessionStream {
	return &fakeSessionStream{
		recvCh: make(chan *controlpb.AgentFrame, 4),
		sendCh: make(chan *controlpb.ControllerFrame, 4),
		ctx:    context.Background(),
	}
}

func (s *fakeSessionStream) Send(frame *controlpb.ControllerFrame) error {
	s.sendCh <- frame
	return nil
}

func (s *fakeSessionStream) Recv() (*controlpb.AgentFrame, error) {
	frame, ok := <-s.recvCh
	if !ok {
		return nil, io.EOF
	}
	return frame, nil
}

func (s *fakeSessionStream) SetHeader(metadata.MD) error {
	return nil
}

func (s *fakeSessionStream) SendHeader(metadata.MD) error {
	return nil
}

func (s *fakeSessionStream) SetTrailer(metadata.MD) {}

func (s *fakeSessionStream) Context() context.Context {
	return s.ctx
}

func (s *fakeSessionStream) SendMsg(any) error {
	return nil
}

func (s *fakeSessionStream) RecvMsg(any) error {
	return nil
}

func receiveRunResult(t *testing.T, ch <-chan struct {
	result *controlpb.CommandResult
	err    error
}) struct {
	result *controlpb.CommandResult
	err    error
} {
	t.Helper()
	select {
	case got := <-ch:
		return got
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for Run result")
		return struct {
			result *controlpb.CommandResult
			err    error
		}{}
	}
}
