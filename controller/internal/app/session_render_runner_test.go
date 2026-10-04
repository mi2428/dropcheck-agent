package app

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"dropcheck/controller/internal/command"
	"dropcheck/controller/internal/control"
	"dropcheck/controller/internal/controlpb"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
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

}

func TestStatusDetailSwitchChangesPCPresentation(t *testing.T) {
	result := &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_OK, Payload: &controlpb.CommandResult_WifiStatus{WifiStatus: &controlpb.WifiStatus{Enabled: true, State: "connected", Connection: &controlpb.WifiConnection{Ssid: "Lab", SupplicantState: "COMPLETED"}}}}
	base, err := renderCommandResult("fixture", result, command.Options{}, outputText)
	if err != nil {
		t.Fatal(err)
	}
	detail, err := renderCommandResult("fixture", result, command.Options{Detail: true}, outputText)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(base, "More") || strings.Contains(base, "supplicant") || !strings.Contains(detail, "supplicant") || strings.Contains(detail, "More") {
		t.Fatalf("default/detail mismatch: default=%q detail=%q", base, detail)
	}
	fields := []*controlpb.DiagnosticField{{Key: "fixture_detail_only", Value: "preserved"}}
	for _, tc := range []struct {
		name   string
		result *controlpb.CommandResult
	}{
		{"capabilities", &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_OK, Payload: &controlpb.CommandResult_WifiCapabilities{WifiCapabilities: &controlpb.WifiCapabilities{Fields: fields}}}},
		{"diagnostics", &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_OK, Payload: &controlpb.CommandResult_WifiDiagnostics{WifiDiagnostics: &controlpb.WifiDiagnostics{Capabilities: &controlpb.WifiCapabilities{Fields: fields}}}}},
	} {
		base, err := renderCommandResult("fixture", tc.result, command.Options{}, outputText)
		if err != nil {
			t.Fatal(err)
		}
		detail, err := renderCommandResult("fixture", tc.result, command.Options{Detail: true}, outputText)
		if err != nil || strings.Contains(base, "fixture_detail_only") || !strings.Contains(detail, "fixture_detail_only") {
			t.Fatalf("%s default/detail: %v %q %q", tc.name, err, base, detail)
		}
	}
}

func TestShellPromptUsesAgentLabelAndModeSuffix(t *testing.T) {
	state, cleanup := connectedShellState(t)
	defer cleanup()

	if got, want := state.prompt(), "R5CT12345# "; got != want {
		t.Fatalf("prompt() = %q, want %q", got, want)
	}

	if got, want := state.prompt(), "R5CT12345# "; got != want {
		t.Fatalf("request prompt() = %q, want %q", got, want)
	}

	state.targetAll = true
	if got, want := state.prompt(), "all# "; got != want {
		t.Fatalf("all request prompt() = %q, want %q", got, want)
	}

	disconnected := &shellState{selected: "missing-agent", selectedLabel: "previous-agent"}
	if got, want := disconnected.prompt(), "previous-agent# "; got != want {
		t.Fatalf("disconnected prompt() = %q, want %q", got, want)
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

	op, err := command.PingOperation(command.PingOptions{Host: "example.test", Count: "1"})
	if err != nil {
		t.Fatalf("PingOperation() error = %v", err)
	}

	out, err := captureStdout(t, func() error {
		return runOperationForAgents(
			context.Background(),
			state,
			[]control.AgentInfo{agent},
			op,
			commandOutputOptions{strict: true},
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

func TestRunOperationForAgentsStrictFailure(t *testing.T) {
	for _, tc := range []struct {
		name string
		body *controlpb.AgentFrame
		want string
	}{
		{"failed", &controlpb.AgentFrame{Body: &controlpb.AgentFrame_Result{Result: &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_FAILED, Message: "ping failed"}}}, "FAILED"},
		{"canceled", &controlpb.AgentFrame{Body: &controlpb.AgentFrame_Result{Result: &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_CANCELED, Message: "ping canceled"}}}, "CANCELED"},
		{"command error", &controlpb.AgentFrame{Body: &controlpb.AgentFrame_Error{Error: &controlpb.CommandError{Message: "agent error"}}}, "agent error"},
	} {
		for _, format := range []outputFormat{outputText, outputJSON} {
			for _, strict := range []bool{false, true} {
				t.Run(tc.name+"/"+string(format)+"/strict="+strconv.FormatBool(strict), func(t *testing.T) {
					state, stream, cleanup := connectedShellStateWithStream(t)
					defer cleanup()
					agent, err := state.server.ResolveAgent("agent-a")
					if err != nil {
						t.Fatal(err)
					}
					go func() {
						for frame := range stream.sent {
							if frame.GetRunCommand() != nil {
								tc.body.CommandId = frame.GetCommandId()
								stream.recv <- tc.body
								return
							}
						}
					}()
					op, err := command.PingOperation(command.PingOptions{Host: "example.test", Count: "1"})
					if err != nil {
						t.Fatal(err)
					}
					out, err := captureStdout(t, func() error {
						return runOperationForAgents(context.Background(), state, []control.AgentInfo{agent}, op, commandOutputOptions{format: format, strict: strict})
					})
					if (err != nil) != strict || strict && !strings.Contains(err.Error(), tc.want) {
						t.Fatalf("error = %v, strict = %v", err, strict)
					}
					if !strings.Contains(strings.ToLower(out), strings.ToLower(tc.want)) {
						t.Fatalf("output = %q, want %q", out, tc.want)
					}
				})
			}
		}
	}
}

func TestStrictFailureNeverReturnsOrPrintsCredential(t *testing.T) {
	state, stream, cleanup := connectedShellStateWithStream(t)
	defer cleanup()
	state.adbPath = filepath.Join(t.TempDir(), "missing-adb")
	agent, err := selectedAgent(state)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for frame := range stream.sent {
			if frame.GetRunCommand() == nil {
				continue
			}
			stream.recv <- &controlpb.AgentFrame{CommandId: frame.GetCommandId(), Body: &controlpb.AgentFrame_Result{Result: &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_FAILED, Message: "connection failed for example-secret"}}}
			return
		}
	}()
	op, err := command.WifiConnectOperation(command.WifiConnectOptions{SSID: "Lab", Passphrase: "example-secret"})
	if err != nil {
		t.Fatal(err)
	}
	out, err := captureStdout(t, func() error {
		return runOperationForAgents(context.Background(), state, []control.AgentInfo{agent}, op, commandOutputOptions{strict: true})
	})
	if err == nil || !strings.Contains(err.Error(), "FAILED") || strings.Contains(err.Error(), "example-secret") || strings.Contains(out, "example-secret") {
		t.Fatalf("unsafe strict error/result: %v %s", err, out)
	}
}

func TestRunOperationForAgentsStrictReportsAllAgents(t *testing.T) {
	state, streamA, cleanupA := connectedShellStateWithStream(t)
	defer cleanupA()
	streamB, cleanupB := connectAdditionalTestAgent(t, state.server, "session-b", "agent-b", "serial-b", 35)
	defer cleanupB()
	respondToPingCommand(streamA, 4)
	go func() {
		for frame := range streamB.sent {
			if frame.GetRunCommand() != nil {
				streamB.recv <- &controlpb.AgentFrame{CommandId: frame.GetCommandId(), Body: &controlpb.AgentFrame_Result{Result: &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_FAILED, Message: "agent-b failed"}}}
				return
			}
		}
	}()
	op, err := command.PingOperation(command.PingOptions{Host: "example.test", Count: "1"})
	if err != nil {
		t.Fatal(err)
	}
	out, err := captureStdout(t, func() error {
		return runOperationForAgents(context.Background(), state, state.server.Agents(), op, commandOutputOptions{format: outputJSON, strict: true})
	})
	if err == nil || !strings.Contains(err.Error(), "agent-b failed") || strings.Count(out, `"agent"`) != 2 {
		t.Fatalf("error = %v, output = %q", err, out)
	}
}

func TestTypedTracerouteViaControlsTextJSONAndStrictExit(t *testing.T) {
	for _, format := range []outputFormat{outputText, outputJSON} {
		for _, tc := range []struct {
			name, typed, raw string
			pass             bool
		}{
			{"missing typed hop despite raw line", "192.0.2.2", "1  192.0.2.1  1ms", false},
			{"matched typed hop despite absent raw line", "192.0.2.1", "no raw trace", true},
		} {
			t.Run(tc.name+"/"+string(format), func(t *testing.T) {
				state, stream, cleanup := connectedShellStateWithStream(t)
				defer cleanup()
				state.adbPath = filepath.Join(t.TempDir(), "missing-adb")
				agent, err := selectedAgent(state)
				if err != nil {
					t.Fatal(err)
				}
				go func() {
					for frame := range stream.sent {
						if frame.GetRunCommand() == nil {
							continue
						}
						reached := true
						stream.recv <- &controlpb.AgentFrame{CommandId: frame.GetCommandId(), Body: &controlpb.AgentFrame_Result{Result: &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_OK, Payload: &controlpb.CommandResult_Traceroute{Traceroute: &controlpb.TracerouteResult{Host: "fixture.invalid", Output: tc.raw, ReachedTarget: &reached, Hops: []*controlpb.TracerouteHop{{Index: 1, Addresses: []string{tc.typed}}}}}}}}
						return
					}
				}()
				op, err := command.TracerouteOperation(command.TracerouteOptions{Host: "fixture.invalid", Via: []string{"192.0.2.1"}})
				if err != nil {
					t.Fatal(err)
				}
				out, err := captureStdout(t, func() error {
					return runOperationForAgents(context.Background(), state, []control.AgentInfo{agent}, op, commandOutputOptions{format: format, strict: true})
				})
				if (err == nil) != tc.pass {
					t.Fatalf("status=%v, output=%s", err, out)
				}
				if !tc.pass && (!strings.Contains(out, "trace.via") || !strings.Contains(strings.ToLower(out), "failed")) {
					t.Fatalf("typed finding missing: %s", out)
				}
				if format == outputJSON {
					var body map[string]any
					if err := json.Unmarshal([]byte(out), &body); err != nil {
						t.Fatal(err)
					}
					want := "STATUS_OK"
					if !tc.pass {
						want = "STATUS_FAILED"
					}
					if body["status"] != want || body["findings"] == nil {
						t.Fatalf("json status/findings: %s", out)
					}
				}
			})
		}
	}
}

func TestRunOperationForAgentsSeparatesMultiAgentTextOutput(t *testing.T) {
	state, streamA, cleanupA := connectedShellStateWithStream(t)
	defer cleanupA()

	streamB, cleanupB := connectAdditionalTestAgent(t, state.server, "session-b", "agent-b", "45240DLAQ007HG", 36)
	defer cleanupB()

	agentA, err := state.server.ResolveAgent("agent-a")
	if err != nil {
		t.Fatalf("ResolveAgent(agent-a) error = %v", err)
	}
	agentB, err := state.server.ResolveAgent("agent-b")
	if err != nil {
		t.Fatalf("ResolveAgent(agent-b) error = %v", err)
	}

	respondToPingCommand(streamA, 4)
	respondToPingCommand(streamB, 7)

	op, err := command.PingOperation(command.PingOptions{Host: "example.test", Count: "1"})
	if err != nil {
		t.Fatalf("PingOperation() error = %v", err)
	}

	out, err := captureStdout(t, func() error {
		return runOperationForAgents(
			context.Background(),
			state,
			[]control.AgentInfo{agentA, agentB},
			op,
			commandOutputOptions{},
		)
	})
	if err != nil {
		t.Fatalf("runOperationForAgents() error = %v", err)
	}

	for _, want := range []string{
		"Agent: R5CT12345\nPing  OK  4ms",
		"Agent: 45240DLAQ007HG\nPing  OK  7ms",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("runOperationForAgents output = %q, missing %q", out, want)
		}
	}
	if strings.Count(out, "Agent: ") != 2 {
		t.Fatalf("runOperationForAgents output = %q, want 2 agent headers", out)
	}
	if !strings.Contains(out, "\n\nAgent: ") {
		t.Fatalf("runOperationForAgents output did not separate agent blocks:\n%s", out)
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
					state.adbPath = filepath.Join(t.TempDir(), "missing-adb")
					switch scenario {
					case "lost", "lost prefix":
						cleanupA()
						if state.selected != "agent-a" || state.prompt() != state.selectedLabel+"# " {
							t.Fatal("disconnected selection or prompt changed identity")
						}
						ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
						defer cancel()
						for _, line := range []string{"ping example.test count 1", "show adb cmd wifi status"} {
							if _, err := parseShellLine(line); err != nil {
								t.Fatal(err)
							}
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
	agent, err := selectedAgent(state)
	if err != nil {
		t.Fatal(err)
	}
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
	op, err := command.WifiEHTOperationWithOptions(command.WifiEHTOptions{Fresh: true})
	if err != nil {
		t.Fatal(err)
	}
	out, err := captureStdout(t, func() error {
		return runOperationForAgents(ctx, state, []control.AgentInfo{agent}, op, commandOutputOptions{format: outputJSON, strict: true})
	})
	if err == nil || !strings.Contains(out, "context canceled") {
		t.Fatalf("error = %v, output = %s", err, out)
	}
	first := receiveTestControllerFrame(t, frames)
	second := receiveTestControllerFrame(t, frames)
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
		{"canceled blank message", &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_CANCELED, Payload: &controlpb.CommandResult_WifiDiagnostics{WifiDiagnostics: &controlpb.WifiDiagnostics{}}}, "STATUS_CANCELED"},
	} {
		for _, format := range []outputFormat{outputText, outputJSON} {
			t.Run(tc.name+"/"+string(format), func(t *testing.T) {
				state, stream, cleanup := connectedShellStateWithStream(t)
				defer cleanup()
				state.adbPath = filepath.Join(t.TempDir(), "missing-adb")
				agent, err := selectedAgent(state)
				if err != nil {
					t.Fatal(err)
				}
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
				op, err := command.WifiEHTOperationWithOptions(command.WifiEHTOptions{Fresh: true})
				if err != nil {
					t.Fatal(err)
				}
				out, err := captureStdout(t, func() error {
					return runOperationForAgents(context.Background(), state, []control.AgentInfo{agent}, op, commandOutputOptions{format: format, strict: true})
				})
				if err == nil || !strings.Contains(out, tc.want) || tc.name != "nil" && tc.name != "OK wrong payload" && !strings.Contains(out, "fresh scan incomplete") {
					t.Fatalf("error = %v, output = %s", err, out)
				}
			})
		}
	}
}

func TestRunOperationForAgentsWifiEHTFreshResults(t *testing.T) {
	scan := &controlpb.WifiScan{
		Fields:  []*controlpb.DiagnosticField{{Key: "fresh_scan_elapsed_ms", Value: "123"}},
		Results: []*controlpb.WifiScanResult{{Ssid: "Lab", Bssid: "aa:bb:cc:dd:ee:ff", WifiStandard: "802.11be"}},
	}
	for _, tc := range []struct {
		name        string
		status      controlpb.CommandResult_Status
		payload     *controlpb.WifiScan
		commandErr  string
		empty       bool
		diagnostics bool
		want        string
	}{
		{"failed cached", controlpb.CommandResult_STATUS_FAILED, scan, "", false, true, "fresh scan incomplete"},
		{"failed no payload", controlpb.CommandResult_STATUS_FAILED, nil, "", false, false, "fresh scan incomplete"},
		{"canceled", controlpb.CommandResult_STATUS_CANCELED, scan, "", false, false, "CANCELED"},
		{"transport", 0, nil, "scan transport failure", false, false, "scan transport failure"},
		{"nil", 0, nil, "", true, false, "empty response"},
		{"ok no payload", controlpb.CommandResult_STATUS_OK, nil, "", false, false, "no payload"},
		{"success", controlpb.CommandResult_STATUS_OK, scan, "", false, true, "fresh"},
	} {
		for _, format := range []outputFormat{outputText, outputJSON} {
			t.Run(tc.name+"/"+string(format), func(t *testing.T) {
				state, stream, cleanup := connectedShellStateWithStream(t)
				defer cleanup()
				state.adbPath = filepath.Join(t.TempDir(), "missing-adb")
				agent, err := selectedAgent(state)
				if err != nil {
					t.Fatal(err)
				}
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
							response.Body = &controlpb.AgentFrame_Result{Result: &controlpb.CommandResult{
								Status: controlpb.CommandResult_STATUS_OK,
								Payload: &controlpb.CommandResult_WifiDiagnostics{WifiDiagnostics: &controlpb.WifiDiagnostics{
									Scan: &controlpb.WifiScan{Results: []*controlpb.WifiScanResult{{Ssid: "STALE"}}},
								}},
							}}
						}
						stream.recv <- response
					}
				}()
				op, err := command.WifiEHTOperationWithOptions(command.WifiEHTOptions{Fresh: true, Timeout: "9000"})
				if err != nil {
					t.Fatal(err)
				}
				op.Options.Detail = true
				out, err := captureStdout(t, func() error {
					return runOperationForAgents(context.Background(), state, []control.AgentInfo{agent}, op, commandOutputOptions{format: format, strict: true})
				})
				if (err == nil) != (tc.name == "success") || !strings.Contains(strings.ToLower(out), strings.ToLower(tc.want)) {
					t.Fatalf("error = %v, output = %q", err, out)
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
				if format == outputJSON {
					var body map[string]any
					if err := json.Unmarshal([]byte(out), &body); err != nil {
						t.Fatal(err)
					}
					if tc.name == "failed cached" && body["status"] != "STATUS_FAILED" || tc.name == "canceled" && body["status"] != "STATUS_CANCELED" {
						t.Fatalf("JSON status = %v", body["status"])
					}
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
						Host:      ping.GetHost(),
						Count:     ping.GetCount(),
						ElapsedMs: elapsedMs,
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
