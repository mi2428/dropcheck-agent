package render

import (
	"strings"
	"testing"
	"time"

	"dropcheck/controller/internal/controlpb"
	"dropcheck/controller/internal/harness"
)

func TestReportKeepsCleanupProblemsRecoveryAndTypedPrecision(t *testing.T) {
	started := time.Unix(1, 0)
	scope := harness.Scope{Kind: "check", AgentID: "agent-1", TargetID: "target-1", CheckID: "check-1"}
	report := harness.Report{
		Outcome: harness.OutcomePass, State: "finished", Started: started, Ended: started.Add(time.Second),
		Counts:   harness.Counts{Passed: 1, Skipped: 1},
		Agents:   []harness.AgentProgress{{Agent: harness.AgentSnapshot{ID: "agent-1", Name: "fixture-agent"}, Outcome: harness.OutcomePass}},
		Cleanup:  []harness.OperationRecord{{Name: "disconnect", Error: "cleanup timeout", Started: started, Ended: started.Add(time.Millisecond)}},
		Problems: []harness.Problem{{Kind: "cleanup", Scope: scope, Round: 1, Message: "cleanup remains unresolved"}},
		Steps: []harness.StepReport{
			{Scope: scope, Round: 1, Name: "latency", Outcome: harness.OutcomePass, Attempts: []harness.Attempt{
				{Scope: scope, Outcome: harness.FailOutcome, Number: 1, Started: started, Ended: started.Add(time.Millisecond), Findings: []harness.Finding{{TargetID: "target-1", Target: "東京 Lab Case", Metric: "latency", Observed: "0.00ms", Expected: "<= 0.00ms", ObservedValue: 0.00049, ExpectedValue: 0.00048, Message: "constraint failed"}}},
				{Scope: scope, Outcome: harness.OutcomePass, Number: 2, Started: started, Ended: started.Add(time.Millisecond), Findings: []harness.Finding{{Metric: "latency", Observed: "0.00ms", Expected: "<= 0.00ms", ObservedValue: 0.00047, ExpectedValue: 0.00048, Passed: true}}},
			}},
			{Scope: scope, Name: "optional", Outcome: harness.SkipOutcome, SkipReason: harness.SkipUnsupported},
		},
	}
	for _, width := range []int{24, 32, 40, 48, 60, 80, 96, 120} {
		out, err := Report(report, Presentation{Width: width})
		if err != nil {
			t.Fatal(err)
		}
		for line := range strings.SplitSeq(out, "\n") {
			if displayWidth(line) > width {
				t.Fatalf("overflow: %q", line)
			}
		}
		flat := strings.ReplaceAll(out, "\n", "")
		for _, value := range []string{"Overall outcome (core): PASS", "Core passed: false", "cleanup timeout", "cleanup remains unresolved", "attempt=1 FAIL", "attempt=2 PASS", "0.00049", "0.00048", "Skip reason: unsupported", "n/a (skipped)", "東京 Lab Case"} {
			if !strings.Contains(flat, value) {
				t.Fatalf("missing %q at width %d: %s", value, width, out)
			}
		}
		if strings.Contains(flat, "0.00047") {
			t.Fatal("success values should remain in detail, not default")
		}
	}
	detail, err := Report(report, Presentation{Detail: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(detail, "0.00047") {
		t.Fatal("successful typed value missing in detail")
	}
}

func TestCleanupCanceledAndMissingAttemptTimelineRemainExplicit(t *testing.T) {
	report := harness.Report{Outcome: harness.CanceledOutcome, Cleanup: []harness.OperationRecord{{Name: "forget", Raw: &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_CANCELED, Message: "operator canceled"}}}, Steps: []harness.StepReport{{Name: "not started", Outcome: harness.CanceledOutcome}}}
	out, err := Report(report, Presentation{})
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"CANCELED", "operator canceled", "? (no attempt timeline recorded)"} {
		if !strings.Contains(out, value) {
			t.Fatalf("missing %s: %s", value, out)
		}
	}
	if strings.Contains(out, "0ms") {
		t.Fatal("fabricated an elapsed time")
	}
}

func TestReportRedactsFreeTextFailures(t *testing.T) {
	report := harness.Report{
		Outcome:  harness.FailOutcome,
		Problems: []harness.Problem{{Message: "password=secret"}},
		Cleanup:  []harness.OperationRecord{{Error: "token=secret"}},
		Steps: []harness.StepReport{{
			Name: "wifi", Outcome: harness.FailOutcome, Reason: "https://example.test/?token=secret",
			Attempts: []harness.Attempt{{Reason: "psk=secret", Result: harness.OperationResult{Parts: []harness.OperationRecord{{Error: "api_key=secret"}}}}},
		}},
	}
	out, err := Report(report, Presentation{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "secret") || !strings.Contains(out, "<redacted>") {
		t.Fatalf("unsafe report: %s", out)
	}
}
