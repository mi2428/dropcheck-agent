package app

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mi2428/dropcheck-agent/controller/internal/control"
	"github.com/mi2428/dropcheck-agent/controller/internal/controlpb"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
)

type shellPresentation struct {
	OK    bool     `json:"ok"`
	Lines []string `json:"lines"`
}

func shellEnvelope(t *testing.T, out string) shellPresentation {
	t.Helper()
	var envelope shellPresentation
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatalf("invalid Shell envelope: %s (%v)", out, err)
	}
	if err := json.Unmarshal([]byte(out), &fields); err != nil || len(fields) != 2 || fields["ok"] == nil || fields["lines"] == nil {
		t.Fatalf("Shell output escaped the ok/lines presentation contract: %s", out)
	}
	if string(fields["ok"]) != "true" && string(fields["ok"]) != "false" {
		t.Fatalf("Shell outcome is not a typed boolean: %s", out)
	}
	return envelope
}

func captureShellLine(t *testing.T, ctx context.Context, state *shellState, line string) (string, string, error) {
	t.Helper()
	stderr, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}
	previous := os.Stderr
	os.Stderr = stderr
	defer func() { os.Stderr = previous; _ = stderr.Close() }()
	out, runErr := captureStdout(t, func() error {
		_, err := runReplLine(ctx, state, line)
		return err
	})
	data, err := os.ReadFile(stderr.Name())
	if err != nil {
		t.Fatal(err)
	}
	return out, string(data), runErr
}

func TestRenderAgentsAndTargetFromConnectedAgent(t *testing.T) {
	state, cleanup := connectedShellState(t)
	defer cleanup()
	for _, suffix := range []string{"", " | display json"} {
		out, _, err := captureShellLine(t, t.Context(), state, "show devices"+suffix)
		if err != nil {
			t.Fatal(err)
		}
		if suffix != "" {
			envelope := shellEnvelope(t, out)
			if !envelope.OK {
				t.Fatal("local agent list failed")
			}
			out = strings.Join(envelope.Lines, "\n")
		}
		for _, want := range []string{"SEL", "*", "R5CT12345", "Acme Pixel", "SDK"} {
			if !strings.Contains(out, want) {
				t.Fatalf("Shell agent list missing %q: %s", want, out)
			}
		}
	}
}

func TestAgentNumberRequiresExplicitMarker(t *testing.T) {
	state, cleanup := connectedShellState(t)
	defer cleanup()
	if agent, err := resolveShellAgent(state, "#1"); err != nil || agent.ID != "agent-a" {
		t.Fatalf("explicit display number = %v, %v", agent.ID, err)
	}
	if _, err := resolveShellAgent(state, "1"); err == nil {
		t.Fatal("bare numeric selector silently chose display number instead of serial")
	}
}

func TestShellPromptUsesAgentLabelAndModeSuffix(t *testing.T) {
	state, cleanup := connectedShellState(t)
	defer cleanup()
	if got := state.prompt(); got != "R5CT12345> " {
		t.Fatal(got)
	}
	state.targetAll = true
	if got := state.prompt(); got != "all> " {
		t.Fatal(got)
	}
	disconnected := &shellState{selected: "missing-agent", selectedLabel: "previous-agent"}
	if got := disconnected.prompt(); got != "previous-agent> " {
		t.Fatal("disconnected prompt lost the acquisition identity", got)
	}
}

func TestShellDispatchesAndRendersTypedPing(t *testing.T) {
	state, stream, cleanup := connectedShellStateWithStream(t)
	defer cleanup()
	frameCh := make(chan *controlpb.ControllerFrame, 1)
	go func() {
		for frame := range stream.sent {
			if frame.GetRunCommand() == nil {
				continue
			}
			frameCh <- frame
			stream.recv <- &controlpb.AgentFrame{CommandId: frame.GetCommandId(), Body: &controlpb.AgentFrame_Result{Result: &controlpb.CommandResult{
				Status:  controlpb.CommandResult_STATUS_OK,
				Payload: &controlpb.CommandResult_Ping{Ping: &controlpb.PingResult{Host: "example.test", Count: 1, Transmitted: new(uint32(1)), Received: new(uint32(1)), PacketLossPercent: new(float64(0)), ElapsedMs: 4}},
			}}}
			return
		}
	}()
	out, _, err := captureShellLine(t, t.Context(), state, "request ping example.test count 1")
	if err != nil {
		t.Fatal(err)
	}
	ping := receiveTestControllerFrame(t, frameCh).GetRunCommand().GetPing()
	if ping.GetHost() != "example.test" || ping.GetCount() != 1 {
		t.Fatalf("dispatched ping = %v", ping)
	}
	for _, want := range []string{"4ms", "example.test", "Transmitted: 1", "Received: 1"} {
		if !strings.Contains(strings.Join(strings.Fields(out), " "), want) {
			t.Fatalf("Shell ping missing %q: %s", want, out)
		}
	}
}

func TestShellFailedMissingAndWrongPayloadStayFailed(t *testing.T) {
	for _, tc := range []struct {
		name string
		body *controlpb.AgentFrame
		want string
	}{
		{"failed", &controlpb.AgentFrame{Body: &controlpb.AgentFrame_Result{Result: &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_FAILED, Message: "ping failed"}}}, "FAILED"},
		{"canceled", &controlpb.AgentFrame{Body: &controlpb.AgentFrame_Result{Result: &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_CANCELED, Message: "ping canceled"}}}, "CANCELED"},
		{"command error", &controlpb.AgentFrame{Body: &controlpb.AgentFrame_Error{Error: &controlpb.CommandError{Message: "agent error"}}}, "agent error"},
		{"nil result", &controlpb.AgentFrame{Body: &controlpb.AgentFrame_Result{}}, "UNSPECIFIED"},
		{"missing payload", &controlpb.AgentFrame{Body: &controlpb.AgentFrame_Result{Result: &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_OK}}}, "no payload"},
		{"wrong payload", &controlpb.AgentFrame{Body: &controlpb.AgentFrame_Result{Result: &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_OK, Payload: &controlpb.CommandResult_IpStatus{IpStatus: &controlpb.IpStatus{}}}}}, "MISSING payload"},
	} {
		for _, suffix := range []string{"", " | display json"} {
			t.Run(tc.name+suffix, func(t *testing.T) {
				state, stream, cleanup := connectedShellStateWithStream(t)
				defer cleanup()
				go func() {
					for frame := range stream.sent {
						if frame.GetRunCommand() != nil {
							body := proto.Clone(tc.body).(*controlpb.AgentFrame)
							body.CommandId = frame.GetCommandId()
							stream.recv <- body
							return
						}
					}
				}()
				out, stderr, err := captureShellLine(t, t.Context(), state, "request ping example.test count 1"+suffix)
				if err != nil || stderr == "" {
					t.Fatalf("Shell failure was not visible: err=%v stderr=%s", err, stderr)
				}
				if suffix != "" {
					envelope := shellEnvelope(t, out)
					if envelope.OK {
						t.Fatal("failed acquisition became a successful presentation")
					}
					out = strings.Join(envelope.Lines, "\n")
				}
				if !strings.Contains(strings.ToLower(out), strings.ToLower(tc.want)) {
					t.Fatalf("Shell failure lost %q: %s", tc.want, out)
				}
			})
		}
	}
}

func TestShellHTTPEnvelopePreservesPublicURL(t *testing.T) {
	const target = "https://www.google.com/generate_204"
	state, stream, cleanup := connectedShellStateWithStream(t)
	defer cleanup()
	sentURL := make(chan string, 1)
	go func() {
		for frame := range stream.sent {
			if frame.GetRunCommand() != nil {
				sentURL <- frame.GetRunCommand().GetHttpCheck().GetUrl()
				stream.recv <- &controlpb.AgentFrame{CommandId: frame.GetCommandId(), Body: &controlpb.AgentFrame_Result{Result: &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_OK, Payload: &controlpb.CommandResult_HttpCheck{HttpCheck: &controlpb.HttpCheckResult{Url: target, Status: 204, ExpectedStatus: 204, Matched: true}}}}}
				return
			}
		}
	}()
	out, _, err := captureShellLine(t, t.Context(), state, "request http check "+target+" expected-status 204 | display json")
	if got := <-sentURL; got != target {
		t.Fatalf("sent URL changed: %q", got)
	}
	envelope := shellEnvelope(t, out)
	if err != nil || !envelope.OK || !strings.Contains(strings.Join(envelope.Lines, "\n"), target) {
		t.Fatalf("public HTTP URL changed in the safe envelope: %s (%v)", out, err)
	}
}

func TestShellFailureNeverPrintsCredential(t *testing.T) {
	state, stream, cleanup := connectedShellStateWithStream(t)
	defer cleanup()
	go func() {
		for frame := range stream.sent {
			if frame.GetRunCommand() != nil {
				stream.recv <- &controlpb.AgentFrame{CommandId: frame.GetCommandId(), Body: &controlpb.AgentFrame_Result{Result: &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_FAILED, Message: "connection failed for example-secret"}}}
				return
			}
		}
	}()
	out, stderr, err := captureShellLine(t, t.Context(), state, "request wifi connect Lab passphrase example-secret | display json")
	if err != nil || shellEnvelope(t, out).OK || !strings.Contains(stderr, "FAILED") || strings.Contains(out+stderr, "example-secret") || !state.failedUse {
		t.Fatalf("unsafe Shell failure/result: %v %s %s", err, out, stderr)
	}
}

func TestShellBroadcastProducesIndependentNDJSON(t *testing.T) {
	state, streamA, cleanupA := connectedShellStateWithStream(t)
	defer cleanupA()
	streamB, cleanupB := connectAdditionalTestAgent(t, state.server, "session-b", "agent-b", "serial-b", 35)
	defer cleanupB()
	state.targetAll = true
	respondToPingCommand(streamA, 4)
	go func() {
		for frame := range streamB.sent {
			if frame.GetRunCommand() != nil {
				streamB.recv <- &controlpb.AgentFrame{CommandId: frame.GetCommandId(), Body: &controlpb.AgentFrame_Result{Result: &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_FAILED, Message: "agent-b failed"}}}
				return
			}
		}
	}()
	out, stderr, err := captureShellLine(t, t.Context(), state, "request ping example.test count 1 | display json")
	if err != nil || !strings.Contains(stderr, "serial-b") {
		t.Fatalf("broadcast failure disappeared: %s (%v)", stderr, err)
	}
	seen := map[string]bool{}
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		envelope := shellEnvelope(t, line)
		text := strings.Join(envelope.Lines, "\n")
		switch {
		case strings.Contains(text, "Agent: R5CT12345"):
			if !envelope.OK || seen["a"] {
				t.Fatal("agent A lost its independent success/identity")
			}
			seen["a"] = true
		case strings.Contains(text, "Agent: serial-b"):
			if envelope.OK || seen["b"] || !strings.Contains(text, "agent-b failed") {
				t.Fatal("agent B lost its independent failure/identity")
			}
			seen["b"] = true
		default:
			t.Fatalf("broadcast envelope lost acquisition target: %s", line)
		}
	}
	if len(seen) != 2 {
		t.Fatalf("broadcast results missing: %s", out)
	}
}

func TestTypedTracerouteViaControlsShellOutcome(t *testing.T) {
	for _, suffix := range []string{"", " | display json"} {
		for _, tc := range []struct {
			name, typed, raw string
			pass             bool
		}{
			{"missing typed hop despite raw line", "192.0.2.2", "1  192.0.2.1  1ms", false},
			{"matched typed hop despite absent raw line", "192.0.2.1", "no raw trace", true},
		} {
			t.Run(tc.name+suffix, func(t *testing.T) {
				state, stream, cleanup := connectedShellStateWithStream(t)
				defer cleanup()
				go func() {
					for frame := range stream.sent {
						if frame.GetRunCommand() != nil {
							stream.recv <- &controlpb.AgentFrame{CommandId: frame.GetCommandId(), Body: &controlpb.AgentFrame_Result{Result: &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_OK, Payload: &controlpb.CommandResult_Traceroute{Traceroute: &controlpb.TracerouteResult{Host: "fixture.invalid", Output: tc.raw, ReachedTarget: new(true), Hops: []*controlpb.TracerouteHop{{Index: 1, Addresses: []string{tc.typed}}}}}}}}
							return
						}
					}
				}()
				out, stderr, err := captureShellLine(t, t.Context(), state, "request traceroute fixture.invalid via 192.0.2.1"+suffix)
				if err != nil {
					t.Fatal(err)
				}
				if suffix != "" {
					envelope := shellEnvelope(t, out)
					if envelope.OK != tc.pass {
						t.Fatalf("typed via judgement changed: %s", out)
					}
					out = strings.Join(envelope.Lines, "\n")
				}
				if !strings.Contains(out, "trace.via") || !tc.pass && (!strings.Contains(strings.ToLower(out), "fail") || !strings.Contains(stderr, "FAILED")) {
					t.Fatalf("typed hop finding disappeared: %s %s", out, stderr)
				}
			})
		}
	}
}

func TestShellSeparatesMultiAgentTextOutput(t *testing.T) {
	state, streamA, cleanupA := connectedShellStateWithStream(t)
	defer cleanupA()
	streamB, cleanupB := connectAdditionalTestAgent(t, state.server, "session-b", "agent-b", "45240DLAQ007HG", 36)
	defer cleanupB()
	state.targetAll = true
	respondToPingCommand(streamA, 4)
	respondToPingCommand(streamB, 7)
	out, _, err := captureShellLine(t, t.Context(), state, "request ping example.test count 1")
	if err != nil || strings.Count(out, "Agent: ") != 2 || !strings.Contains(out, "\n\nAgent: ") {
		t.Fatalf("agent text blocks merged: %s (%v)", out, err)
	}
	for _, want := range []string{"R5CT12345", "45240DLAQ007HG", "4ms", "7ms"} {
		if !strings.Contains(out, want) {
			t.Fatalf("broadcast lost %q: %s", want, out)
		}
	}
}

func TestTargetResolutionContracts(t *testing.T) {
	for _, resolve := range []struct {
		name string
		run  func(*shellState) ([]control.AgentInfo, error)
	}{{"command", (*shellState).commandTargets}, {"watch", watchTargetAgents}} {
		for _, scenario := range []string{"single", "multiple", "explicit", "broadcast", "lost", "lost prefix"} {
			t.Run(resolve.name+"/"+scenario, func(t *testing.T) {
				state, _, cleanupA := connectedShellStateWithStream(t)
				if scenario == "single" {
					defer cleanupA()
					state.selected = ""
				} else {
					id := "agent-b"
					if scenario == "lost prefix" {
						id = "agent-a-other"
					}
					streamB, cleanupB := connectAdditionalTestAgent(t, state.server, "session-b", id, "serial-b", 35)
					defer cleanupB()
					switch scenario {
					case "lost", "lost prefix":
						cleanupA()
						if state.selected != "agent-a" || state.prompt() != state.selectedLabel+"> " {
							t.Fatal("disconnected selection or prompt changed identity")
						}
						ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
						defer cancel()
						for _, line := range []string{"request ping example.test count 1", "debug adb wifi"} {
							if _, err := runReplLine(ctx, state, line); err != nil {
								t.Fatal(err)
							}
						}
						for len(streamB.sent) > 0 {
							if frame := <-streamB.sent; frame.GetRunCommand() != nil {
								t.Fatal("lost selection dispatched to replacement")
							}
						}
					default:
						defer cleanupA()
						state.selected = ""
						if scenario == "explicit" {
							state.selected = id
						}
						state.targetAll = scenario == "broadcast"
					}
				}
				agents, err := resolve.run(state)
				if scenario == "multiple" || strings.HasPrefix(scenario, "lost") {
					want := "multiple"
					if strings.HasPrefix(scenario, "lost") {
						want = `selected Android agent "agent-a" is not connected`
					}
					if err == nil || !strings.Contains(err.Error(), want) || strings.Contains(err.Error(), "token") || len(agents) != 0 {
						t.Fatalf("targets = %v, error = %v", agents, err)
					}
					return
				}
				wantCount, wantID := 1, "agent-a"
				if scenario == "broadcast" {
					wantCount = 2
				} else if scenario == "explicit" {
					wantID = "agent-b"
				}
				if err != nil || len(agents) != wantCount || wantCount == 1 && agents[0].ID != wantID {
					t.Fatalf("targets = %v, error = %v", agents, err)
				}
			})
		}
	}
}

func TestWifiEHTFreshContextCancellationStopsComposition(t *testing.T) {
	state, stream, cleanup := connectedShellStateWithStream(t)
	defer cleanup()
	state.adbPath = filepath.Join(t.TempDir(), "missing-adb")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	frames := make(chan *controlpb.ControllerFrame, 4)
	go func() {
		for frame := range stream.sent {
			if frame.GetRunCommand() != nil || frame.GetCancelCommand() != nil {
				frames <- frame
			}
			if frame.GetRunCommand().GetGetFreshWifiScan() != nil {
				cancel()
			}
		}
	}()
	out, _, err := captureShellLine(t, ctx, state, "request wifi scan eht | display json")
	if err != nil || shellEnvelope(t, out).OK || !strings.Contains(out, "context canceled") {
		t.Fatalf("canceled Shell acquisition became success: %s (%v)", out, err)
	}
	first, second := receiveTestControllerFrame(t, frames), receiveTestControllerFrame(t, frames)
	if first.GetRunCommand().GetGetFreshWifiScan() == nil || second.GetCancelCommand() == nil || first.GetCommandId() != second.GetCommandId() || len(frames) != 0 {
		t.Fatal("canceled scan did not stop diagnostics and send its cancel frame")
	}
}

func TestWifiEHTFreshDiagnosticsFailuresStayVisible(t *testing.T) {
	for _, tc := range []struct {
		name   string
		result *controlpb.CommandResult
		want   string
	}{
		{"nil", nil, "empty response"},
		{"OK wrong payload", &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_OK, Payload: &controlpb.CommandResult_Ping{Ping: &controlpb.PingResult{}}}, "wrong payload"},
		{"failed", &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_FAILED, Message: "diagnostics failed", Payload: &controlpb.CommandResult_WifiDiagnostics{WifiDiagnostics: &controlpb.WifiDiagnostics{}}}, "diagnostics failed"},
		{"canceled blank message", &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_CANCELED, Payload: &controlpb.CommandResult_WifiDiagnostics{WifiDiagnostics: &controlpb.WifiDiagnostics{}}}, "CANCELED"},
	} {
		for _, suffix := range []string{"", " | display json"} {
			t.Run(tc.name+suffix, func(t *testing.T) {
				state, stream, cleanup := connectedShellStateWithStream(t)
				defer cleanup()
				state.adbPath = filepath.Join(t.TempDir(), "missing-adb")
				go func() {
					for frame := range stream.sent {
						if frame.GetRunCommand() == nil {
							continue
						}
						result := tc.result
						if frame.GetRunCommand().GetGetFreshWifiScan() != nil {
							result = &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_FAILED, Message: "fresh scan incomplete", Payload: &controlpb.CommandResult_WifiScan{WifiScan: &controlpb.WifiScan{}}}
						}
						stream.recv <- &controlpb.AgentFrame{CommandId: frame.GetCommandId(), Body: &controlpb.AgentFrame_Result{Result: result}}
						if frame.GetRunCommand().GetGetWifiDiagnostics() != nil {
							return
						}
					}
				}()
				out, stderr, err := captureShellLine(t, t.Context(), state, "request wifi scan eht"+suffix)
				if err != nil || stderr == "" {
					t.Fatalf("diagnostics failure disappeared: %s (%v)", out, err)
				}
				if suffix != "" {
					envelope := shellEnvelope(t, out)
					if envelope.OK {
						t.Fatal("failed diagnostics became successful")
					}
					out = strings.Join(envelope.Lines, "\n")
				}
				if !strings.Contains(out, tc.want) || tc.name != "nil" && tc.name != "OK wrong payload" && !strings.Contains(out, "fresh scan incomplete") {
					t.Fatalf("failure/provenance missing: %s", out)
				}
			})
		}
	}
}

func TestShellWifiEHTFreshResults(t *testing.T) {
	scan := &controlpb.WifiScan{Fields: []*controlpb.DiagnosticField{{Key: "fresh_scan_elapsed_ms", Value: "123"}}, Results: []*controlpb.WifiScanResult{{Ssid: "Lab", Bssid: "aa:bb:cc:dd:ee:ff", WifiStandard: "802.11be"}}}
	for _, key := range []string{"fresh_scan_receiver_registered", "fresh_scan_start_scan", "fresh_scan_broadcast_received", "fresh_scan_results_updated", "fresh_scan_wait_completed"} {
		scan.Fields = append(scan.Fields, &controlpb.DiagnosticField{Key: key, Value: "true"})
	}
	for _, tc := range []struct {
		name               string
		status             controlpb.CommandResult_Status
		payload            *controlpb.WifiScan
		commandErr         string
		empty, diagnostics bool
		want               string
	}{
		{"failed cached", controlpb.CommandResult_STATUS_FAILED, scan, "", false, true, "fresh scan incomplete"},
		{"failed no payload", controlpb.CommandResult_STATUS_FAILED, nil, "", false, false, "fresh scan incomplete"},
		{"canceled", controlpb.CommandResult_STATUS_CANCELED, scan, "", false, false, "CANCELED"},
		{"transport", 0, nil, "scan transport failure", false, false, "scan transport failure"},
		{"nil", 0, nil, "", true, false, "empty response"},
		{"ok no payload", controlpb.CommandResult_STATUS_OK, nil, "", false, false, "evidence is missing"},
		{"success", controlpb.CommandResult_STATUS_OK, scan, "", false, true, "fresh"},
	} {
		for _, suffix := range []string{"", " | display json"} {
			t.Run(tc.name+suffix, func(t *testing.T) {
				state, stream, cleanup := connectedShellStateWithStream(t)
				defer cleanup()
				state.adbPath = filepath.Join(t.TempDir(), "missing-adb")
				frameCh := make(chan *controlpb.ControllerFrame, 4)
				go func() {
					for frame := range stream.sent {
						cmd := frame.GetRunCommand()
						if cmd == nil {
							continue
						}
						frameCh <- frame
						response := &controlpb.AgentFrame{CommandId: frame.GetCommandId()}
						if cmd.GetGetFreshWifiScan() != nil {
							result := &controlpb.CommandResult{Status: tc.status}
							if tc.status != controlpb.CommandResult_STATUS_OK {
								result.Message = "fresh scan incomplete"
							}
							if tc.payload != nil {
								result.Payload = &controlpb.CommandResult_WifiScan{WifiScan: proto.Clone(tc.payload).(*controlpb.WifiScan)}
							}
							if tc.empty {
								result = nil
							}
							response.Body = &controlpb.AgentFrame_Result{Result: result}
							if tc.commandErr != "" {
								response.Body = &controlpb.AgentFrame_Error{Error: &controlpb.CommandError{Message: tc.commandErr}}
							}
						} else {
							response.Body = &controlpb.AgentFrame_Result{Result: &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_OK, Payload: &controlpb.CommandResult_WifiDiagnostics{WifiDiagnostics: &controlpb.WifiDiagnostics{Status: &controlpb.WifiStatus{}, Capabilities: &controlpb.WifiCapabilities{}, Scan: &controlpb.WifiScan{Results: []*controlpb.WifiScanResult{{Ssid: "STALE"}}}}}}}
						}
						stream.recv <- response
					}
				}()
				out, stderr, err := captureShellLine(t, t.Context(), state, "request wifi scan eht timeout 9000"+suffix)
				if err != nil {
					t.Fatal(err)
				}
				if suffix != "" {
					envelope := shellEnvelope(t, out)
					if envelope.OK != (tc.name == "success") {
						t.Fatalf("fresh outcome changed: %s", out)
					}
					out = strings.Join(envelope.Lines, "\n")
				}
				if !strings.Contains(strings.ToLower(out), strings.ToLower(tc.want)) || tc.name != "success" && stderr == "" {
					t.Fatalf("fresh result/failure missing: %s %s", out, stderr)
				}
				first := receiveTestControllerFrame(t, frameCh)
				if fresh := first.GetRunCommand().GetGetFreshWifiScan(); fresh == nil || fresh.GetTimeoutMs() != 9000 || fresh.GetBand() != controlpb.WifiBand_WIFI_BAND_ALL {
					t.Fatalf("fresh command = %v", first.GetRunCommand())
				}
				if tc.diagnostics {
					if second := receiveTestControllerFrame(t, frameCh); second.GetRunCommand().GetGetWifiDiagnostics() == nil {
						t.Fatalf("second command = %v", second.GetRunCommand())
					}
					for _, want := range []string{"123", "Lab"} {
						if !strings.Contains(out, want) {
							t.Fatalf("output missing %q: %s", want, out)
						}
					}
				}
				if len(frameCh) != 0 || strings.Contains(out, "STALE") {
					t.Fatalf("unexpected follow-up or stale scan: %s", out)
				}
				if tc.name == "failed cached" && (!strings.Contains(out, "cached (refresh failed)") || !strings.Contains(out, "fresh_scan")) {
					t.Fatalf("cached provenance missing: %s", out)
				}
			})
		}
	}
}

func connectedShellState(t *testing.T) (*shellState, func()) {
	state, _, cleanup := connectedShellStateWithStream(t)
	return state, cleanup
}

func connectAdditionalTestAgent(t *testing.T, server *control.Server, sessionID string, agentID string, serial string, sdk int32) (*testControlSessionStream, func()) {
	t.Helper()
	wantAgents := len(server.Agents()) + 1
	stream := newTestControlSessionStream()
	stream.recv <- &controlpb.AgentFrame{
		SessionId: sessionID,
		Body: &controlpb.AgentFrame_Hello{Hello: &controlpb.AgentHello{
			Token:             "token",
			ControllerAgentId: agentID,
			AdbSerial:         serial,
			AppVersion:        "debug",
			Device: &controlpb.DeviceInfo{
				Manufacturer: "Acme",
				Model:        "Pixel",
				Sdk:          sdk,
			},
		}},
	}

	done := make(chan error, 1)
	go func() {
		done <- server.Session(stream)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := server.WaitAgents(ctx, wantAgents); err != nil {
		t.Fatalf("WaitAgents() error = %v", err)
	}

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
	return stream, cleanup
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

func receiveTestControllerFrame(t *testing.T, ch <-chan *controlpb.ControllerFrame) *controlpb.ControllerFrame {
	t.Helper()
	select {
	case frame := <-ch:
		return frame
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for controller frame")
		return nil
	}
}

func respondToPingCommand(stream *testControlSessionStream, elapsedMs int64) {
	go func() {
		for frame := range stream.sent {
			ping := frame.GetRunCommand().GetPing()
			if ping == nil {
				continue
			}
			stream.recv <- &controlpb.AgentFrame{
				CommandId: frame.GetCommandId(),
				Body: &controlpb.AgentFrame_Result{Result: &controlpb.CommandResult{
					Status: controlpb.CommandResult_STATUS_OK,
					Payload: &controlpb.CommandResult_Ping{Ping: &controlpb.PingResult{
						Host:              ping.GetHost(),
						Count:             ping.GetCount(),
						Transmitted:       proto.Uint32(1),
						Received:          proto.Uint32(1),
						PacketLossPercent: proto.Float64(0),
						ElapsedMs:         elapsedMs,
						Output: "1 packets transmitted, 1 packets received, 0% packet loss\n" +
							"rtt min/avg/max/mdev = 1.000/1.500/2.000/0.100 ms\n",
					}},
				}},
			}
			return
		}
	}()
}

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
