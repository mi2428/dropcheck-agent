package app

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"dropcheck/controller/internal/control"
	"dropcheck/controller/internal/controlpb"
	"google.golang.org/grpc/metadata"
)

func TestRenderAgentsAndTargetFromConnectedAgent(t *testing.T) {
	state, cleanup := connectedShellState(t)
	defer cleanup()

	text, err := renderAgents(agentListView(state), outputText)
	if err != nil {
		t.Fatalf("renderAgents(text) error = %v", err)
	}
	for _, want := range []string{"SEL", "*", "R5CT12345", "Acme Pixel", "SDK"} {
		if !strings.Contains(text, want) {
			t.Fatalf("renderAgents(text) = %q, missing %q", text, want)
		}
	}

	rawJSON, err := renderAgents(agentListView(state), outputJSON)
	if err != nil {
		t.Fatalf("renderAgents(json) error = %v", err)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(rawJSON), &rows); err != nil {
		t.Fatalf("renderAgents(json) unmarshal error = %v: %s", err, rawJSON)
	}
	if len(rows) != 1 {
		t.Fatalf("renderAgents(json) rows = %#v", rows)
	}
	if rows[0]["selected"] != true || rows[0]["id"] != "agent-a" || rows[0]["adb_serial"] != "R5CT12345" {
		t.Fatalf("renderAgents(json) row = %#v", rows[0])
	}

	target, err := renderTarget(targetView(state), outputText)
	if err != nil {
		t.Fatalf("renderTarget(text) error = %v", err)
	}
	if !strings.Contains(target, "Target: R5CT12345 (id=agent-a adb_serial=R5CT12345)") {
		t.Fatalf("renderTarget(text) = %q", target)
	}

	state.selected = "disconnected-agent"
	state.selectedLabel = "previous-agent"
	target, err = renderTarget(targetView(state), outputText)
	if err != nil {
		t.Fatalf("renderTarget(disconnected) error = %v", err)
	}
	if target != "Target: previous-agent (disconnected)\n" {
		t.Fatalf("renderTarget(disconnected) = %q", target)
	}
}

func TestRunOperationForAgentsDispatchesAndRendersResult(t *testing.T) {
	state, stream, cleanup := connectedShellStateWithStream(t)
	defer cleanup()

	agent, err := state.server.ResolveAgent("agent-a")
	if err != nil {
		t.Fatalf("ResolveAgent() error = %v", err)
	}

	frameCh := make(chan *controlpb.ControllerFrame, 1)
	go func() {
		for frame := range stream.sent {
			if frame.GetRunCommand() == nil {
				continue
			}
			frameCh <- frame
			stream.recv <- &controlpb.AgentFrame{
				CommandId: frame.GetCommandId(),
				Body: &controlpb.AgentFrame_Result{Result: &controlpb.CommandResult{
					Status: controlpb.CommandResult_STATUS_OK,
					Payload: &controlpb.CommandResult_Ping{Ping: &controlpb.PingResult{
						Host:      "example.test",
						Count:     1,
						ElapsedMs: 4,
						Output: "1 packets transmitted, 1 packets received, 0% packet loss\n" +
							"rtt min/avg/max/mdev = 1.000/1.500/2.000/0.100 ms\n",
					}},
				}},
			}
			return
		}
	}()

	out, err := captureStdout(t, func() error {
		return runOperationForAgents(
			context.Background(),
			state,
			[]control.AgentInfo{agent},
			operationFromCommandArgs([]string{"ping", "example.test", "1"}),
			commandOutputOptions{},
		)
	})
	if err != nil {
		t.Fatalf("runOperationForAgents() error = %v", err)
	}

	select {
	case frame := <-frameCh:
		ping := frame.GetRunCommand().GetPing()
		if ping == nil || ping.GetHost() != "example.test" || ping.GetCount() != 1 {
			t.Fatalf("dispatched ping = %#v", ping)
		}
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for dispatched command")
	}

	for _, want := range []string{"Latency: 4ms", "Ping: host=example.test", "transmitted=1 received=1"} {
		if !strings.Contains(out, want) {
			t.Fatalf("runOperationForAgents output = %q, missing %q", out, want)
		}
	}
}

func connectedShellState(t *testing.T) (*shellState, func()) {
	state, _, cleanup := connectedShellStateWithStream(t)
	return state, cleanup
}

func connectedShellStateWithStream(t *testing.T) (*shellState, *testControlSessionStream, func()) {
	t.Helper()
	server := control.NewServer("token", nil)
	stream := newTestControlSessionStream()
	stream.recv <- &controlpb.AgentFrame{
		SessionId: "session-a",
		Body: &controlpb.AgentFrame_Hello{Hello: &controlpb.AgentHello{
			Token:             "token",
			ControllerAgentId: "agent-a",
			AdbSerial:         "R5CT12345",
			AppVersion:        "debug",
			Device: &controlpb.DeviceInfo{
				Manufacturer: "Acme",
				Model:        "Pixel",
				Sdk:          35,
			},
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
	state := &shellState{server: server}
	state.setSelectedAgent(info)

	cleanup := func() {
		stream.close()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("Session() error = %v", err)
			}
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for Session cleanup")
		}
	}
	return state, stream, cleanup
}

type testControlSessionStream struct {
	ctx    context.Context
	cancel context.CancelFunc
	recv   chan *controlpb.AgentFrame
	sent   chan *controlpb.ControllerFrame
}

func newTestControlSessionStream() *testControlSessionStream {
	ctx, cancel := context.WithCancel(context.Background())
	return &testControlSessionStream{
		ctx:    ctx,
		cancel: cancel,
		recv:   make(chan *controlpb.AgentFrame, 8),
		sent:   make(chan *controlpb.ControllerFrame, 8),
	}
}

func (s *testControlSessionStream) close() {
	s.cancel()
	close(s.sent)
}

func (s *testControlSessionStream) Send(frame *controlpb.ControllerFrame) error {
	select {
	case s.sent <- frame:
		return nil
	case <-s.ctx.Done():
		return io.EOF
	}
}

func (s *testControlSessionStream) Recv() (*controlpb.AgentFrame, error) {
	select {
	case frame := <-s.recv:
		return frame, nil
	case <-s.ctx.Done():
		return nil, io.EOF
	}
}

func (s *testControlSessionStream) SetHeader(metadata.MD) error  { return nil }
func (s *testControlSessionStream) SendHeader(metadata.MD) error { return nil }
func (s *testControlSessionStream) SetTrailer(metadata.MD)       {}
func (s *testControlSessionStream) Context() context.Context     { return s.ctx }
func (s *testControlSessionStream) SendMsg(any) error            { return nil }
func (s *testControlSessionStream) RecvMsg(any) error            { return nil }

func captureStdout(t *testing.T, run func() error) (string, error) {
	t.Helper()
	original := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	os.Stdout = writer
	runErr := run()
	os.Stdout = original
	if err := writer.Close(); err != nil {
		t.Fatalf("stdout pipe close error = %v", err)
	}
	out, readErr := io.ReadAll(reader)
	if readErr != nil {
		t.Fatalf("stdout pipe read error = %v", readErr)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("stdout pipe reader close error = %v", err)
	}
	return string(out), runErr
}
