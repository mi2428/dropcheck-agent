package render

import (
	"strings"
	"testing"
	"time"

	"github.com/mi2428/dropcheck-agent/controller/internal/controlpb"
	"github.com/mi2428/dropcheck-agent/controller/internal/tuirun"
)

func TestReportKeepsCleanupProblemsRecoveryAndTypedPrecision(t *testing.T) {
	started := time.Unix(1, 0)
	scope := tuirun.Scope{Kind: "check", AgentID: "agent-1", TargetID: "target-1", CheckID: "check-1"}
	report := tuirun.Report{
		Outcome: tuirun.OutcomePass, State: "finished", Started: started, Ended: started.Add(time.Second),
		Counts:   tuirun.Counts{Passed: 1, Skipped: 1},
		Agents:   []tuirun.AgentProgress{{Agent: tuirun.AgentSnapshot{ID: "agent-1", Name: "fixture-agent"}, Outcome: tuirun.OutcomePass}},
		Cleanup:  []tuirun.OperationRecord{{Name: "disconnect", Error: "cleanup timeout", Started: started, Ended: started.Add(time.Millisecond)}},
		Problems: []tuirun.Problem{{Kind: "cleanup", Scope: scope, Round: 1, Message: "cleanup remains unresolved"}},
		Steps: []tuirun.StepReport{
			{Scope: scope, Round: 1, Name: "latency", Outcome: tuirun.OutcomePass, Attempts: []tuirun.Attempt{
				{Scope: scope, Outcome: tuirun.FailOutcome, Number: 1, Started: started, Ended: started.Add(time.Millisecond), Findings: []tuirun.Finding{{TargetID: "target-1", Target: "東京 Lab Case", Metric: "latency", Observed: "0.00ms", Expected: "<= 0.00ms", ObservedValue: 0.00049, ExpectedValue: 0.00048, Message: "constraint failed"}}},
				{Scope: scope, Outcome: tuirun.OutcomePass, Number: 2, Started: started, Ended: started.Add(time.Millisecond), Findings: []tuirun.Finding{{Metric: "latency", Observed: "0.00ms", Expected: "<= 0.00ms", ObservedValue: 0.00047, ExpectedValue: 0.00048, Passed: true}}},
			}},
			{Scope: scope, Name: "optional", Outcome: tuirun.SkipOutcome, SkipReason: tuirun.SkipUnsupported},
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
	report := tuirun.Report{Outcome: tuirun.CanceledOutcome, Cleanup: []tuirun.OperationRecord{{Name: "forget", Raw: &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_CANCELED, Message: "operator canceled"}}}, Steps: []tuirun.StepReport{{Name: "not started", Outcome: tuirun.CanceledOutcome}}}
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
	scope := tuirun.Scope{Kind: "check", AgentID: "selected-id", TargetID: "ssid-target", CheckID: "check-id"}
	report := tuirun.Report{
		Agents:  []tuirun.AgentProgress{{Agent: tuirun.AgentSnapshot{ID: scope.AgentID, Name: "selected-device"}}},
		Cleanup: []tuirun.OperationRecord{{Scope: scope, Name: "disconnect", Raw: raw}},
		Steps:   []tuirun.StepReport{{Scope: scope, Attempts: []tuirun.Attempt{{Scope: scope, Result: tuirun.OperationResult{Raw: raw}}}}},
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
	report := tuirun.Report{
		Outcome:  tuirun.FailOutcome,
		Problems: []tuirun.Problem{{Message: "password=secret"}},
		Cleanup:  []tuirun.OperationRecord{{Error: "token=secret"}},
		Steps: []tuirun.StepReport{{
			Name: "wifi", Outcome: tuirun.FailOutcome, Reason: "https://example.test/?token=secret",
			Attempts: []tuirun.Attempt{{Reason: "psk=secret", Result: tuirun.OperationResult{Parts: []tuirun.OperationRecord{{Error: "api_key=secret"}}}}},
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
