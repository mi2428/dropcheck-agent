package tuirun

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mi2428/dropcheck-agent/controller/internal/command"
	"github.com/mi2428/dropcheck-agent/controller/internal/controlpb"
	"github.com/mi2428/dropcheck-agent/controller/internal/runner"
)

func TestFailureGatingAndCleanup(t *testing.T) {
	for _, scenario := range []string{"connect_status", "connect_error", "wait_status", "wait_error", "retry_exhausted", "repeat_failed", "missing_network", "pre_canceled"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			check := operationFixture(command.PingOperation(command.PingOptions{Host: "8.8.8.8", Count: "5"}))
			expect, err := CompileExpectations(map[string]any{"received": 5})
			if err != nil {
				t.Fatal(err)
			}
			check.Expectations = expect
			want := []string{"wifi.connect", "wifi.wait", "wifi.disconnect", "wifi.forget"}
			failedOperation := "wifi.wait"
			var replies []uint32
			switch scenario {
			case "connect_status", "connect_error":
				failedOperation = "wifi.connect"
				want = []string{"wifi.connect", "wifi.disconnect", "wifi.forget"}
			case "retry_exhausted", "repeat_failed":
				failedOperation = ""
				replies = []uint32{0, 1}
				check.Policy.Attempts = 2
				if scenario == "repeat_failed" {
					replies = []uint32{5, 0}
					check.Policy = Policy{Repeat: 2}
				}
				want = []string{"wifi.connect", "wifi.wait", "ping", "ping", "dns", "wifi.disconnect", "wifi.forget"}
			case "missing_network", "pre_canceled":
				want = nil
			}
			plan := Plan{Networks: []Network{{Name: "lab", SSID: "Lab", PSK: SecretValue("synthetic-secret"), WaitConnected: true, RequireIP: true, DisconnectAfter: true, ForgetAfter: true}}, Checks: []Check{check, operationFixture(command.DNSOperation("example.com", "", ""))}}
			if scenario == "missing_network" {
				plan.Networks = nil
				if err := Validate(plan); err == nil {
					t.Fatal("missing network accepted before dispatch")
				}
				return
			}
			if scenario == "pre_canceled" {
				cancel()
			}
			var operations []string
			calls := 0
			report, err := Execute(ctx, compiledFixture(t, plan), cleanupRunnerFunc(func(_ context.Context, op command.Operation) (runner.Result, error) {
				operations = append(operations, op.Name)
				result := runtimeFixture(op)
				if op.Name == failedOperation {
					if strings.HasSuffix(scenario, "_error") {
						return runner.Result{}, errors.New("forced failure")
					}
					result.Status, result.Message = controlpb.CommandResult_STATUS_FAILED, "forced failure"
				}
				if op.Name == "ping" {
					if calls >= len(replies) {
						t.Fatal("unexpected ping call")
					}
					result.GetPing().Received = new(replies[calls])
					calls++
				}
				return runner.Result{Result: result}, nil
			}), ExecuteOptions{Rounds: 1})
			if report.Passed() || !reflect.DeepEqual(operations, want) || calls != len(replies) {
				t.Fatalf("gating/cleanup: operations=%v want=%v calls=%d report=%+v err=%v", operations, want, calls, report, err)
			}
			if strings.HasSuffix(scenario, "_error") && err == nil {
				t.Fatal("transport error was hidden")
			}
			if scenario == "pre_canceled" && report.Outcome != CanceledOutcome {
				t.Fatalf("pre-canceled run=%+v", report)
			}
			var observed []uint32
			for _, step := range report.Steps {
				for _, attempt := range step.Attempts {
					if ping := attempt.Result.Raw.GetPing(); ping != nil {
						observed = append(observed, ping.GetReceived())
					}
				}
			}
			if !reflect.DeepEqual(observed, replies) {
				t.Fatalf("attempt measurements lost: %v want=%v", observed, replies)
			}
		})
	}
}

func TestStableChecksTakeNewMeasurements(t *testing.T) {
	check := operationFixture(command.PingOperation(command.PingOptions{Host: "8.8.8.8", Count: "5"}))
	check.Policy = Policy{StableFor: 3 * time.Millisecond, Interval: time.Millisecond}
	calls := 0
	report, err := Execute(context.Background(), compiledFixture(t, Plan{Networks: []Network{{SSID: "Lab", PSK: SecretValue("synthetic-passphrase")}}, Checks: []Check{check}}), cleanupRunnerFunc(func(_ context.Context, op command.Operation) (runner.Result, error) {
		if op.Name == "ping" {
			calls++
		}
		return runner.Result{Result: runtimeFixture(op)}, nil
	}), ExecuteOptions{Rounds: 1})
	if err != nil || !report.Passed() || calls < 2 {
		t.Fatalf("stability did not reacquire: calls=%d report=%+v err=%v", calls, report, err)
	}
}

func TestExpectationRetryRecoveryKeepsIndependentRepeatMeasurements(t *testing.T) {
	check := operationFixture(command.PingOperation(command.PingOptions{Host: "8.8.8.8", Count: "5"}))
	expect, err := CompileExpectations(map[string]any{"received": ">= 4"})
	if err != nil {
		t.Fatal(err)
	}
	check.Expectations, check.Policy = expect, Policy{Attempts: 2, Repeat: 2}
	replies := []uint32{0, 5, 4}
	calls := 0
	report, err := Execute(context.Background(), compiledFixture(t, Plan{Networks: []Network{{SSID: "Lab", PSK: SecretValue("synthetic-passphrase")}}, Checks: []Check{check}}), cleanupRunnerFunc(func(_ context.Context, op command.Operation) (runner.Result, error) {
		raw := runtimeFixture(op)
		if op.Name == "ping" {
			if calls >= len(replies) {
				t.Fatal("unexpected retry")
			}
			raw.GetPing().Received = new(replies[calls])
			calls++
		}
		return runner.Result{Result: raw}, nil
	}), ExecuteOptions{Rounds: 1})
	if err != nil || !report.Passed() || calls != 3 {
		t.Fatalf("expectation retry did not recover: calls=%d report=%+v err=%v", calls, report, err)
	}
	step := report.Steps[1]
	if len(step.Attempts) != 3 || len(terminalFindings(step)) != 0 {
		t.Fatalf("recovered failure became terminal: %+v", step)
	}
	for i, attempt := range step.Attempts {
		if attempt.Result.Raw.GetPing().GetReceived() != replies[i] || attempt.Repeat != []uint32{1, 1, 2}[i] || attempt.Findings[0].Passed != (i > 0) {
			t.Fatalf("repeat/attempt observation lost: %+v", attempt)
		}
	}
}

func TestFlatMatchersKeepMissingAndFailedDistinct(t *testing.T) {
	op := operationFixture(command.PingOperation(command.PingOptions{Host: "example.test"})).Operation
	raw := runtimeFixture(op)
	raw.GetPing().Received = new(uint32(2))
	for _, tc := range []struct {
		values  map[string]any
		missing bool
	}{
		{map[string]any{"received": ">= 5"}, false},
		{map[string]any{"answer_count": ">= 1"}, true},
		{map[string]any{"addresses": map[string]any{"cidr": "10.0.0.0/8"}}, true},
	} {
		expect, err := CompileExpectations(tc.values)
		if err != nil {
			t.Fatal(err)
		}
		findings := expect[0].Evaluate(Result{Run: RunResult{Raw: raw}})
		if len(findings) != 1 || findings[0].Passed || findings[0].Missing != tc.missing {
			t.Fatalf("unexpected evaluation: %+v", findings)
		}
	}
	ip := runtimeFixture(command.IPStatusOperation())
	ip.GetIpStatus().Addresses = []string{"192.0.2.1/24"}
	expect, err := CompileExpectations(map[string]any{"addresses": map[string]any{"cidr": "10.0.0.0/8"}})
	if err != nil {
		t.Fatal(err)
	}
	finding := expect[0].Evaluate(Result{Run: RunResult{Raw: ip}})[0]
	if finding.Passed || finding.Missing {
		t.Fatalf("CIDR mismatch became missing/pass: %+v", finding)
	}
}
