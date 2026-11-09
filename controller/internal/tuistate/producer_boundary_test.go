package tuistate

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mi2428/dropcheck-agent/controller/internal/command"
	"github.com/mi2428/dropcheck-agent/controller/internal/control"
	"github.com/mi2428/dropcheck-agent/controller/internal/controlpb"
	"github.com/mi2428/dropcheck-agent/controller/internal/runner"
	"github.com/mi2428/dropcheck-agent/controller/internal/tuirun"
)

type boundaryRunner func(context.Context, control.AgentInfo, command.Operation) (runner.Result, error)

func (f boundaryRunner) Run(ctx context.Context, agent control.AgentInfo, op command.Operation) (runner.Result, error) {
	return f(ctx, agent, op)
}

type boundarySink func(context.Context, tuirun.Event) error

func (f boundarySink) Emit(ctx context.Context, event tuirun.Event) error { return f(ctx, event) }

func TestPlannedStepsFollowActualCompiledPreviewMetadata(t *testing.T) {
	plan, err := tuirun.Compile(tuirun.Plan{Networks: []tuirun.Network{
		{SSID: "Observed", ReadOnly: true},
		{SSID: "Owned without wait", PSK: tuirun.SecretValue("synthetic-passphrase")},
		{SSID: "Owned with wait", PSK: tuirun.SecretValue("synthetic-passphrase"), WaitConnected: true, DisconnectAfter: true, ForgetAfter: true},
	}, Checks: []tuirun.Check{tuirun.NewCheck("status", "status-id", command.WifiStatusOperation(), tuirun.Policy{}, false)}}, []control.AgentInfo{{ID: "a", Hello: &controlpb.AgentHello{Capabilities: []string{"wifi.forget.typed"}}}})
	if err != nil {
		t.Fatal(err)
	}
	preview := plan.Preview()
	state := New(preview.Targets, preview.Checks, preview.Agents, time.Now())
	want := [][]string{{"status-id"}, {"connect", "status-id"}, {"connect", "wait_connected", "status-id", "cleanup/wifi.disconnect", "cleanup/wifi.forget"}}
	for i, target := range state.Targets {
		var ids []string
		for _, step := range target.PlannedSteps {
			ids = append(ids, step.ID)
		}
		if !reflect.DeepEqual(ids, want[i]) {
			t.Fatalf("target %d planned=%v want=%v", i, ids, want[i])
		}
	}
	// Per-target metadata is sufficient even without the redundant global list.
	if got := PlannedStepsForTarget(preview.Targets[2], nil); len(got) != 5 {
		t.Fatalf("preview-only plan=%v", got)
	}
}

func TestReducerDropsBulkPayloadsWithoutMutatingSourceOrModalMetadata(t *testing.T) {
	raw := &controlpb.CommandResult{Message: strings.Repeat("large synthetic payload", 1<<16)}
	finding := &tuirun.Finding{TargetID: "target/0", CheckID: "check/0", Check: "status", Metric: "status", Observed: "failed", Expected: "pass", Message: "modal evidence", ObservedValue: raw, ExpectedValue: map[string]any{"raw": raw}}
	attempt := &tuirun.Attempt{Result: tuirun.OperationResult{Raw: raw, Parts: []tuirun.OperationRecord{{Raw: raw}}}}
	event := tuirun.Event{Kind: tuirun.EventStepFinished, Round: 1, Time: time.Now(), Agent: tuirun.AgentSnapshot{ID: "a"}, Target: tuirun.TargetSnapshot{ID: "target/0", Name: "Lab"}, Step: tuirun.StepSnapshot{ID: "check/0", Name: "status", Status: "ok"}, Status: "ok", Attempt: attempt, Report: &tuirun.StepReport{Attempts: []tuirun.Attempt{*attempt}}, Agents: []tuirun.AgentProgress{{Agent: tuirun.AgentSnapshot{ID: "bulk"}}}, Finding: finding}
	state := New(nil, nil, nil, event.Time)
	state.Apply(event)
	event.Kind = tuirun.EventFinding
	state.Apply(event)
	for _, entry := range state.EventLogEntries {
		if entry.Event.Attempt != nil || entry.Event.Report != nil || entry.Event.Agents != nil || entry.Event.Finding.ObservedValue != nil || entry.Event.Finding.ExpectedValue != nil {
			t.Fatal("UI log retained acquisition payloads")
		}
		if entry.Event.Finding == finding || entry.Event.Finding.Message != "modal evidence" || entry.Event.Scope != event.Scope || entry.Line == "" {
			t.Fatal("UI lost detached modal metadata/line")
		}
	}
	if state.FailedChecks[0].Finding.ObservedValue != nil || state.FailedChecks[0].Finding.ExpectedValue != nil || finding.ObservedValue != raw || attempt.Result.Raw != raw || len(event.Report.Attempts) != 1 {
		t.Fatal("UI retained bulk findings or changed core source")
	}
}

func TestProducerToStateDurationSummaryKeepsKnownZeroAndUnknownSeparate(t *testing.T) {
	compiled, err := tuirun.Compile(tuirun.Plan{Networks: []tuirun.Network{{SSID: "Lab", ReadOnly: true}}, Checks: []tuirun.Check{tuirun.NewCheck("status", "status", command.WifiStatusOperation(), tuirun.Policy{}, false)}}, []control.AgentInfo{{ID: "a"}})
	if err != nil {
		t.Fatal(err)
	}
	preview := compiled.Preview()
	state := New(preview.Targets, preview.Checks, preview.Agents, time.Now())
	var latest tuirun.Event
	var total, maximum int64
	report, err := tuirun.Execute(context.Background(), compiled, boundaryRunner(func(_ context.Context, _ control.AgentInfo, _ command.Operation) (runner.Result, error) {
		status := &controlpb.WifiStatus{Connection: &controlpb.WifiConnection{Ssid: "Lab", ObservationFields: []*controlpb.DiagnosticField{{Key: "identity.state", Value: "available"}}}}
		return runner.Result{Result: &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_OK, Payload: &controlpb.CommandResult_WifiStatus{WifiStatus: status}}}, nil
	}), tuirun.ExecuteOptions{Rounds: 2, Sinks: []tuirun.Sink{boundarySink(func(_ context.Context, event tuirun.Event) error {
		state.Apply(event)
		if event.Kind == tuirun.EventStepFinished {
			latest = event
			if !event.DurationKnown || event.Duration != event.Report.Attempts[0].Ended.Sub(event.Report.Attempts[0].Started).Milliseconds() {
				t.Error("real producer timing missing or inconsistent")
			}
			total += event.Duration
			maximum = max(maximum, event.Duration)
		}
		return nil
	})}})
	if err != nil || !report.Passed() {
		t.Fatalf("producer did not pass: outcome=%s err=%v", report.Outcome, err)
	}
	// Exercise zero and absent observations at the same reducer boundary.
	latest.Round, latest.Time, latest.Duration, latest.DurationKnown = 3, latest.Time.Add(time.Second), 0, true
	state.Apply(latest)
	latest.Round, latest.Time, latest.DurationKnown = 4, latest.Time.Add(time.Second), false
	state.Apply(latest)
	for _, summary := range state.PassingCheckSummaries() {
		if summary.Step.ID != "status" {
			continue
		}
		if summary.Count != 4 || summary.DurationCount != 3 || summary.DurationTotal != total || summary.AvgDuration() != (total+1)/3 || summary.MaxDuration != maximum || summary.DurationKnown {
			t.Fatalf("zero/unknown summary: %+v", summary)
		}
		return
	}
	t.Fatal("real producer created no passing summary")
}

func TestFailureRateDoesNotBorrowAnotherSameNameCheckRound(t *testing.T) {
	state := New(nil, nil, nil, time.Now())
	target := tuirun.TargetSnapshot{ID: "target/0", Name: "same"}
	state.Apply(tuirun.Event{Kind: tuirun.EventFinding, Round: 1, Target: target, Step: tuirun.StepSnapshot{ID: "a", Name: "same", Status: "failed"}, Finding: &tuirun.Finding{TargetID: target.ID, CheckID: "a", Check: "same", Metric: "status", Expected: "pass"}})
	state.Apply(tuirun.Event{Kind: tuirun.EventStepFinished, Round: 2, Target: target, Step: tuirun.StepSnapshot{ID: "b", Name: "same", Status: "ok"}})
	summary := state.FailedCheckSummaries()[0]
	if summary.FailPercent != 100 || summary.FailStreak != 1 {
		t.Fatalf("a sibling check diluted this check's failure rate/streak: %+v", summary)
	}
	if PassingCheckKey(tuirun.AgentSnapshot{}, target, tuirun.StepSnapshot{ID: "a"}) == PassingCheckKey(tuirun.AgentSnapshot{}, tuirun.TargetSnapshot{Name: target.ID}, tuirun.StepSnapshot{ID: "a"}) {
		t.Fatal("a legacy target label impersonated a stable target ID")
	}
}
