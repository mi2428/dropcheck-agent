package tuirun

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/mi2428/dropcheck-agent/controller/internal/command"
)

func TestControlRequestIsPublishedBeforeItsEffects(t *testing.T) {
	controls := NewControls()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	_, stop := context.WithCancelCause(ctx)
	defer stop(nil)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	events := make(chan Event, 2)
	if err := controls.bind(controlsTestPlan(t), stop, func(event Event) error {
		if event.Kind == EventControlRequested {
			close(entered)
			<-release
		}
		// Observers may query control state; publication must not hold controls.mu.
		controls.HasScope(Scope{Kind: ScopeRun})
		events <- event
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	requested := make(chan error, 1)
	go func() { requested <- controls.Pause(Scope{Kind: ScopeRun}) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("request did not reach publication")
	}
	waited := make(chan error, 1)
	go func() {
		waited <- controls.wait(ctx, Scope{Kind: ScopeCheck, AgentID: "agent-a", TargetID: "target/0", CheckID: "check/0"}, 1)
	}()
	select {
	case event := <-events:
		t.Fatalf("effect overtook the blocked request: %s", event.Kind)
	case <-time.After(50 * time.Millisecond):
	}
	unblock()
	for _, want := range []EventKind{EventControlRequested, EventControlApplied} {
		select {
		case event := <-events:
			if event.Kind != want {
				t.Fatalf("event=%s want=%s", event.Kind, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("missing %s", want)
		}
	}
	cancel()
	if err := <-requested; err != nil {
		t.Fatal(err)
	}
	if err := <-waited; err != context.Canceled {
		t.Fatalf("pause waiter did not stop: %v", err)
	}
}

func controlsTestPlan(t *testing.T) *CompiledPlan {
	t.Helper()
	op, err := command.PingOperation(command.PingOptions{Host: "example.test"})
	if err != nil {
		t.Fatal(err)
	}
	return compiledFixture(t, Plan{
		Networks: []Network{{Name: "lab", SSID: "Lab", PSK: SecretValue("synthetic-passphrase"), WaitConnected: true, RequireIP: true, DisconnectAfter: true}},
		Checks:   []Check{operationFixture(op, nil)},
	})
}

func TestControlsHasScopeFailsClosedOutsideAnActiveRun(t *testing.T) {
	run := Scope{Kind: ScopeRun}
	agentScope := Scope{Kind: ScopeAgent, AgentID: "agent-a"}
	checkScope := Scope{Kind: ScopeCheck, AgentID: "agent-a", TargetID: "target/0", CheckID: "check/0"}

	var nilControls *Controls
	if nilControls.HasScope(run) {
		t.Fatal("nil controls accepted a scope")
	}
	controls := NewControls()
	if controls.HasScope(run) {
		t.Fatal("unbound controls accepted a scope")
	}

	_, stop := context.WithCancelCause(context.Background())
	defer stop(nil)
	if err := controls.bind(controlsTestPlan(t), stop, func(Event) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if !controls.HasScope(run) || !controls.HasScope(agentScope) || !controls.HasScope(checkScope) {
		t.Fatal("bound controls rejected a compiled scope")
	}
	if controls.HasScope(Scope{Kind: ScopeCheck, AgentID: "unknown", TargetID: "target/0", CheckID: "check/0"}) || controls.HasScope(Scope{}) {
		t.Fatal("bound controls accepted an unknown scope")
	}

	controls.finish()
	if controls.HasScope(run) || controls.HasScope(agentScope) || controls.HasScope(checkScope) {
		t.Fatal("finished controls still accepted a scope")
	}
}

func TestControlsCanceledUsesParentScopeContainmentAndPersists(t *testing.T) {
	controls := NewControls()
	_, stop := context.WithCancelCause(context.Background())
	defer stop(nil)
	if err := controls.bind(controlsTestPlan(t), stop, func(Event) error { return nil }); err != nil {
		t.Fatal(err)
	}
	runScope := Scope{Kind: ScopeRun}
	agentScope := Scope{Kind: ScopeAgent, AgentID: "agent-a"}
	targetScope := Scope{Kind: ScopeTarget, AgentID: "agent-a", TargetID: "target/0"}
	checkScope := Scope{Kind: ScopeCheck, AgentID: "agent-a", TargetID: "target/0", CheckID: "check/0"}

	var nilControls *Controls
	if nilControls.Canceled(runScope) {
		t.Fatal("nil controls reported a cancellation")
	}
	if controls.Canceled(Scope{}) || controls.Canceled(Scope{Kind: ScopeCheck, AgentID: "unknown", TargetID: "target/0", CheckID: "check/0"}) {
		t.Fatal("unknown scope reported a cancellation")
	}
	if controls.Canceled(runScope) || controls.Canceled(agentScope) || controls.Canceled(targetScope) || controls.Canceled(checkScope) {
		t.Fatal("nothing was cancelled yet")
	}

	if err := controls.Cancel(targetScope); err != nil {
		t.Fatal(err)
	}
	if !controls.Canceled(targetScope) || !controls.Canceled(checkScope) {
		t.Fatal("cancelled target scope did not cover its check")
	}
	if controls.Canceled(agentScope) || controls.Canceled(runScope) {
		t.Fatal("a child cancellation made a parent scope look cancelled")
	}
	if controls.Canceled(Scope{Kind: ScopeTarget, AgentID: "agent-a", TargetID: "other"}) {
		t.Fatal("an unrelated target reported a cancellation")
	}

	controls.nextRound()
	if !controls.Canceled(checkScope) {
		t.Fatal("cancel should persist after the round boundary")
	}
}

func TestControlsCanceledIgnoresParentWhenOnlyChildWasCancelled(t *testing.T) {
	controls := NewControls()
	_, stop := context.WithCancelCause(context.Background())
	defer stop(nil)
	if err := controls.bind(controlsTestPlan(t), stop, func(Event) error { return nil }); err != nil {
		t.Fatal(err)
	}
	agentScope := Scope{Kind: ScopeAgent, AgentID: "agent-a"}
	targetScope := Scope{Kind: ScopeTarget, AgentID: "agent-a", TargetID: "target/0"}
	checkScope := Scope{Kind: ScopeCheck, AgentID: "agent-a", TargetID: "target/0", CheckID: "check/0"}

	if err := controls.Cancel(checkScope); err != nil {
		t.Fatal(err)
	}
	if !controls.Canceled(checkScope) {
		t.Fatal("cancelled check scope did not report a cancellation")
	}
	if controls.Canceled(targetScope) || controls.Canceled(agentScope) {
		t.Fatal("a child cancellation must not cancel its parents")
	}
}

func TestControlsCanceledWorksForRunScope(t *testing.T) {
	controls := NewControls()
	_, stop := context.WithCancelCause(context.Background())
	defer stop(nil)
	if err := controls.bind(controlsTestPlan(t), stop, func(Event) error { return nil }); err != nil {
		t.Fatal(err)
	}
	runScope := Scope{Kind: ScopeRun}
	checkScope := Scope{Kind: ScopeCheck, AgentID: "agent-a", TargetID: "target/0", CheckID: "check/0"}
	if err := controls.Cancel(runScope); err != nil {
		t.Fatal(err)
	}
	if !controls.Canceled(runScope) || !controls.Canceled(checkScope) {
		t.Fatal("a cancelled run should cover every child scope")
	}
}
