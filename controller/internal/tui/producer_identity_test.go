package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/mi2428/dropcheck-agent/controller/internal/command"
	"github.com/mi2428/dropcheck-agent/controller/internal/control"
	"github.com/mi2428/dropcheck-agent/controller/internal/controlpb"
	"github.com/mi2428/dropcheck-agent/controller/internal/runner"
	"github.com/mi2428/dropcheck-agent/controller/internal/watchrun"
)

type identityBoundaryRunner struct{}

func (identityBoundaryRunner) Run(_ context.Context, agent control.AgentInfo, op command.Operation) (runner.Result, error) {
	ping := op.Command.GetPing()
	raw := &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_OK, Message: fmt.Sprintf("evidence %s %s", agent.ID, ping.Host), Payload: &controlpb.CommandResult_Ping{Ping: &controlpb.PingResult{Host: ping.Host, Count: ping.Count, Transmitted: new(ping.Count), Received: new(ping.Count)}}}
	if agent.ID == "agent/b" && ping.Host == "first.test" {
		raw.Status = controlpb.CommandResult_STATUS_FAILED
	}
	return runner.Result{Result: raw}, nil
}

type identityBoundarySink func(context.Context, watchrun.Event) error

func (f identityBoundarySink) Emit(ctx context.Context, event watchrun.Event) error {
	return f(ctx, event)
}

func TestProducerIdentitySurvivesQueueSummariesAndModalLogMatching(t *testing.T) {
	first, err := command.PingOperation(command.PingOptions{Host: "first.test"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := command.PingOperation(command.PingOptions{Host: "second.test"})
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := watchrun.Compile(watchrun.Plan{Networks: []watchrun.Network{{Name: "same", SSID: "Lab", ReadOnly: true, ActiveProbes: true}, {Name: "same", SSID: "Lab", ReadOnly: true, ActiveProbes: true}}, Checks: []watchrun.Check{
		watchrun.NewCheck("same", "check/a", first, watchrun.Policy{}, false),
		watchrun.NewCheck("same", "check/b", second, watchrun.Policy{}, false),
	}}, []control.AgentInfo{{ID: "agent/a", Hello: &controlpb.AgentHello{AdbSerial: "same"}}, {ID: "agent/b", Hello: &controlpb.AgentHello{AdbSerial: "same"}}})
	if err != nil {
		t.Fatal(err)
	}
	preview := compiled.Preview()
	m := newModelWithChecks(preview.Name, preview.Targets, preview.Checks, nil, preview.Agents)
	var started watchrun.Event
	_, err = watchrun.Execute(context.Background(), compiled, identityBoundaryRunner{}, watchrun.ExecuteOptions{Rounds: 2, Sinks: []watchrun.Sink{identityBoundarySink(func(_ context.Context, event watchrun.Event) error {
		m.Apply(event)
		if event.Kind == watchrun.EventStepStarted && event.Scope.CheckID == "check/b" {
			started = event
		}
		return nil
	})}})
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Targets) != 4 || len(m.CheckStatusChecks()) != 2 || len(m.PassingCheckSummaries()) != 8 || len(m.FailedCheckSummaries()) != 2 {
		t.Fatalf("identities collapsed: targets=%d checks=%v passes=%d failures=%d", len(m.Targets), m.CheckStatusChecks(), len(m.PassingCheckSummaries()), len(m.FailedCheckSummaries()))
	}
	for _, summary := range m.PassingCheckSummaries() {
		if summary.Count != 2 {
			t.Fatalf("cross-round/agent/target pass aggregation: %+v", summary)
		}
		if targetSummaryStep(summary.Step) {
			continue
		}
		for _, entry := range m.EventLogEntries {
			matches := scopedEventAgentMatches(entry.Event.Agent, summary.Agent) && scopedEventTargetMatches(entry.Event, summary.Target) && eventMatchesStep(entry.Event, summary.Step)
			want := entry.Event.Scope.AgentID == summary.Agent.ID && entry.Event.Scope.TargetID == summary.Target.ID && entry.Event.Scope.CheckID == summary.Step.ID
			if matches != want {
				t.Fatalf("pass modal cross-contamination: event=%+v summary=%+v", entry.Event, summary)
			}
		}
		if lines := m.passingCheckRelatedLogLines(summary, 100); len(lines) != 4 {
			t.Fatalf("pass modal mixed logs: %v", lines)
		}
	}
	for _, summary := range m.FailedCheckSummaries() {
		if summary.Count != 2 || summary.Agent.ID != "agent/b" || summary.Finding.CheckID != "check/a" {
			t.Fatalf("failure identity: %+v", summary)
		}
		if lines := m.failedCheckRelatedLogLines(summary, 100); len(lines) != 6 {
			t.Fatalf("failure modal mixed logs: %v", lines)
		}
	}
	// Reuse a real producer start event to check the current duplicate-name row.
	m.Apply(started)
	target := m.EnsureTarget(started.Agent, started.Target)
	steps := runQueueVisibleSteps(*target)
	if len(steps) != 2 || runQueueStepCurrent(*target, steps[0]) || !runQueueStepCurrent(*target, steps[1]) {
		t.Fatalf("queue/current identity lost: %+v", steps)
	}
	lines := m.runQueueTreeLinesForAgent(160, started.Agent, false)
	current := 0
	for _, line := range lines {
		if line.Current {
			current++
		}
	}
	if current != 1 {
		t.Fatalf("duplicate-name queue marked %d rows current", current)
	}
	if current, ok := currentStepState(*target); !ok || current.ID != "check/b" {
		t.Fatalf("header selected a different same-name check: %+v", current)
	}
	started.Kind, started.Step.Status = watchrun.EventStepFinished, "ok"
	m.Apply(started)
	if current := runQueueCurrentStep(*target); current.ID != "check/b" {
		t.Fatalf("queue lost the last terminal row identity: %+v", current)
	}
}

func TestStableIDsNeverFallBackToLegacyNameOrType(t *testing.T) {
	event := watchrun.Event{Agent: watchrun.AgentSnapshot{ID: "a", Name: "same"}, Target: watchrun.TargetSnapshot{ID: "target/a", Name: "same"}, Step: watchrun.StepSnapshot{ID: "check/a", Name: "same", Type: "ping"}}
	if eventMatchesStep(event, watchrun.StepSnapshot{ID: "check/b", Name: "same", Type: "ping"}) || eventMatchesStep(event, watchrun.StepSnapshot{Name: "same", Type: "ping"}) || scopedEventTargetMatches(event, watchrun.TargetSnapshot{ID: "target/b", Name: "same"}) || scopedEventTargetMatches(event, watchrun.TargetSnapshot{Name: "same"}) || scopedEventAgentMatches(event.Agent, watchrun.AgentSnapshot{ID: "b", Name: "same"}) || scopedEventAgentMatches(event.Agent, watchrun.AgentSnapshot{Name: "same"}) {
		t.Fatal("stable identity was overridden by legacy labels")
	}
	event.Scope.CheckID = "check/b"
	if eventMatchesStep(event, event.Step) {
		t.Fatal("inconsistent scope identity matched")
	}
	if rows := mergeRunQueueSteps([]stepState{{ID: "a", Name: "same"}, {ID: "b", Name: "same"}}, []stepState{{ID: "b", Name: "same", Status: "running"}}); len(rows) != 2 || rows[0].Status != "" || rows[1].Status != "running" {
		t.Fatalf("merge identity=%v", rows)
	}
	if got := strings.Join(passingCheckListRightColumns(passingCheckSummary{DurationCount: 1}), " "); !strings.Contains(got, "0ms 0ms") {
		t.Fatalf("known zero displayed as unknown: %s", got)
	}
	if got := strings.Join(passingCheckListRightColumns(passingCheckSummary{}), " "); strings.Contains(got, "0ms") {
		t.Fatalf("unknown displayed as zero: %s", got)
	}
}
