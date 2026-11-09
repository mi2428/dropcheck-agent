package tuirun

import (
	"context"
	"testing"

	"github.com/mi2428/dropcheck-agent/controller/internal/command"
)

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
