package render

import (
	"strings"
	"testing"
	"time"

	"github.com/mi2428/dropcheck-agent/controller/internal/controlpb"
	"github.com/mi2428/dropcheck-agent/controller/internal/watchrun"
)

func TestReportKeepsCleanupProblemsRecoveryAndTypedPrecision(t *testing.T) {
	started := time.Unix(1, 0)
	scope := watchrun.Scope{Kind: "check", AgentID: "agent-1", TargetID: "target-1", CheckID: "check-1"}
	report := watchrun.Report{
		Outcome: watchrun.OutcomePass, State: "finished", Started: started, Ended: started.Add(time.Second),
		Counts:   watchrun.Counts{Passed: 1, Skipped: 1},
		Agents:   []watchrun.AgentProgress{{Agent: watchrun.AgentSnapshot{ID: "agent-1", Name: "fixture-agent"}, Outcome: watchrun.OutcomePass}},
		Cleanup:  []watchrun.OperationRecord{{Name: "disconnect", Error: "cleanup timeout", Started: started, Ended: started.Add(time.Millisecond)}},
		Problems: []watchrun.Problem{{Kind: "cleanup", Scope: scope, Round: 1, Message: "cleanup remains unresolved"}},
		Steps: []watchrun.StepReport{
			{Scope: scope, Round: 1, Name: "latency", Outcome: watchrun.OutcomePass, Attempts: []watchrun.Attempt{
				{Scope: scope, Outcome: watchrun.FailOutcome, Number: 1, Started: started, Ended: started.Add(time.Millisecond), Findings: []watchrun.Finding{{TargetID: "target-1", Target: "東京 Lab Case", Metric: "latency", Observed: "0.00ms", Expected: "<= 0.00ms", ObservedValue: 0.00049, ExpectedValue: 0.00048, Message: "constraint failed"}}},
				{Scope: scope, Outcome: watchrun.OutcomePass, Number: 2, Started: started, Ended: started.Add(time.Millisecond), Findings: []watchrun.Finding{{Metric: "latency", Observed: "0.00ms", Expected: "<= 0.00ms", ObservedValue: 0.00047, ExpectedValue: 0.00048, Passed: true}}},
			}},
			{Scope: scope, Name: "optional", Outcome: watchrun.SkipOutcome, SkipReason: watchrun.SkipUnsupported},
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
		flat := alignmentCompact(out)
		for _, value := range []string{"Overall outcome (core): PASS", "Core passed: false", "cleanup timeout", "cleanup remains unresolved", "attempt=1 FAIL", "attempt=2 PASS", "0.00049", "0.00048", "Skip reason: unsupported", "n/a (skipped)", "東京 Lab Case"} {
			if !strings.Contains(flat, alignmentCompact(value)) {
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
	report := watchrun.Report{Outcome: watchrun.CanceledOutcome, Cleanup: []watchrun.OperationRecord{{Name: "forget", Raw: &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_CANCELED, Message: "operator canceled"}}}, Steps: []watchrun.StepReport{{Name: "not started", Outcome: watchrun.CanceledOutcome}}}
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

func TestReportResultTargetsUseAgentIdentityAndKeepScopeSeparate(t *testing.T) {
	raw := &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_CANCELED}
	scope := watchrun.Scope{Kind: "check", AgentID: "selected-id", TargetID: "ssid-target", CheckID: "check-id"}
	report := watchrun.Report{
		Agents:  []watchrun.AgentProgress{{Agent: watchrun.AgentSnapshot{ID: scope.AgentID, Name: "selected-device"}}},
		Cleanup: []watchrun.OperationRecord{{Scope: scope, Name: "disconnect", Raw: raw}},
		Steps:   []watchrun.StepReport{{Scope: scope, Attempts: []watchrun.Attempt{{Scope: scope, Result: watchrun.OperationResult{Raw: raw}}}}},
	}
	out, err := Report(report, Presentation{Detail: true})
	if err != nil {
		t.Fatal(err)
	}
	if targets := metadataQAValues(out, "Target"); len(targets) != 2 || targets[0] != "selected-device" || targets[1] != "selected-device" {
		t.Fatalf("acquisition targets are not the selected agent: %q", targets)
	}
	if !strings.Contains(out, "target=ssid-target check=check-id") {
		t.Fatal("check scope was lost")
	}
	if ReportAgentLabel(report, "unknown-id") != "unknown-id" || ReportAgentLabel(report, "") != "? (agent metadata unavailable)" {
		t.Fatal("missing identity was fabricated")
	}
}

func TestReportRedactsFreeTextFailures(t *testing.T) {
	report := watchrun.Report{
		Outcome:  watchrun.FailOutcome,
		Problems: []watchrun.Problem{{Message: "password=secret"}},
		Cleanup:  []watchrun.OperationRecord{{Error: "token=secret"}},
		Steps: []watchrun.StepReport{{
			Name: "wifi", Outcome: watchrun.FailOutcome, Reason: "https://example.test/?token=secret",
			Attempts: []watchrun.Attempt{{Reason: "psk=secret", Result: watchrun.OperationResult{Parts: []watchrun.OperationRecord{{Error: "api_key=secret"}}}}},
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
