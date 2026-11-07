package watchrun

import (
	"context"
	"testing"

	"github.com/mi2428/dropcheck-agent/controller/internal/command"
	"github.com/mi2428/dropcheck-agent/controller/internal/controlpb"
	"github.com/mi2428/dropcheck-agent/controller/internal/runner"
)

func TestWifiStatusRequiresTargetIdentity(t *testing.T) {
	for _, mode := range []string{"observe", "active", "owned"} {
		for _, operation := range []command.Operation{command.WifiStatusOperation(), command.WifiDiagnosticsOperation()} {
			if mode == "observe" && operation.Command.GetGetWifiDiagnostics() != nil {
				continue // Passive observation permits only Wi-Fi/IP status.
			}
			for _, tc := range []struct {
				name, ssid string
				known      bool
				outcome    Outcome
			}{
				{"matching", " Lab ", true, OutcomePass},
				{"different", "Other", true, FailOutcome},
				{"trimmed", "Lab", true, FailOutcome},
				{"unknown", " Lab ", false, MissingOutcome},
			} {
				t.Run(mode+"/"+operation.Name+"/"+tc.name, func(t *testing.T) {
					network := Network{SSID: " Lab ", ReadOnly: mode != "owned", ActiveProbes: mode == "active"}
					if mode == "owned" {
						network.PSK = SecretValue("synthetic-passphrase")
					}
					// Optional expectations cannot waive the target's identity constraint.
					expect, err := CompileExpectations(map[string]any{"ssid": "dontcare"})
					if err != nil {
						t.Fatal(err)
					}
					plan := compiledFixture(t, Plan{Networks: []Network{network}, Checks: []Check{NewCheck("status", "", operation, Policy{Attempts: 1}, false, expect...)}})
					report, err := Execute(t.Context(), plan, cleanupRunnerFunc(func(_ context.Context, op command.Operation) (runner.Result, error) {
						raw := runtimeFixture(op)
						status := raw.GetWifiStatus()
						if raw.GetWifiDiagnostics() != nil {
							status = raw.GetWifiDiagnostics().Status
						}
						if status != nil {
							status.Connection = &controlpb.WifiConnection{Ssid: tc.ssid}
							if tc.known {
								status.Connection.ObservationFields = []*controlpb.DiagnosticField{{Key: "identity.state", Value: "available"}}
							}
						}
						return runner.Result{Result: raw}, nil
					}), ExecuteOptions{Rounds: 1})
					if err != nil || report.Outcome != tc.outcome {
						t.Fatalf("outcome=%s want=%s err=%v", report.Outcome, tc.outcome, err)
					}
				})
			}
		}
	}
}

func TestFlatStringExpectationsPreserveLiteralValues(t *testing.T) {
	for _, tc := range []struct {
		actual, expected string
		passed           bool
	}{
		{"Lab", "Lab", true}, {"Other", "Lab", false},
		{"01", "1", false}, {"TRUE", "true", false},
		{"Lab", " Lab ", false}, {" Lab ", " Lab ", true},
	} {
		expect, err := CompileExpectations(map[string]any{"ssid": tc.expected})
		if err != nil {
			t.Fatal(err)
		}
		raw := &controlpb.CommandResult{Payload: &controlpb.CommandResult_WifiStatus{WifiStatus: &controlpb.WifiStatus{Connection: &controlpb.WifiConnection{Ssid: tc.actual, ObservationFields: []*controlpb.DiagnosticField{{Key: "identity.state", Value: "available"}}}}}}
		findings := expect[0].Evaluate(Result{Run: RunResult{Raw: raw}})
		if len(findings) != 1 || findings[0].Passed != tc.passed || findings[0].Missing {
			t.Fatalf("actual=%q expected=%q findings=%+v", tc.actual, tc.expected, findings)
		}
	}
	for _, tc := range []struct {
		metric string
		raw    *controlpb.CommandResult
		want   any
	}{
		{"received", runtimeFixture(operationFixture(command.PingOperation(command.PingOptions{Host: "example.test"})).Operation), ">= 1"},
		{"default_route", runtimeFixture(command.IPStatusOperation()), true},
	} {
		expect, err := CompileExpectations(map[string]any{tc.metric: tc.want})
		if err != nil {
			t.Fatal(err)
		}
		if findings := expect[0].Evaluate(Result{Run: RunResult{Raw: tc.raw}}); len(findings) != 1 || !findings[0].Passed {
			t.Fatalf("typed numeric/boolean control: %+v", findings)
		}
	}
}
