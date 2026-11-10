package tuistate

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/mi2428/dropcheck-agent/controller/internal/command"
	"github.com/mi2428/dropcheck-agent/controller/internal/control"
	"github.com/mi2428/dropcheck-agent/controller/internal/controlpb"
	"github.com/mi2428/dropcheck-agent/controller/internal/runner"
	"github.com/mi2428/dropcheck-agent/controller/internal/tuirun"
)

func controlProgressPlan(t *testing.T, agents []control.AgentInfo) (*tuirun.CompiledPlan, State) {
	t.Helper()
	op, err := command.PingOperation(command.PingOptions{Host: "example.test"})
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := tuirun.Compile(tuirun.Plan{
		Networks: []tuirun.Network{{Name: "lab", SSID: "Lab", ReadOnly: true, ActiveProbes: true}},
		Checks:   []tuirun.Check{tuirun.NewCheck("ping", "", op, tuirun.Policy{Repeat: 2}, false)},
	}, agents)
	if err != nil {
		t.Fatal(err)
	}
	view := compiled.Preview()
	return compiled, New(view.Targets, view.Checks, view.Agents, time.Now())
}

func controlProgressPing(op command.Operation) runner.Result {
	count := op.Command.GetPing().GetCount()
	return runner.Result{Result: &controlpb.CommandResult{
		Status:  controlpb.CommandResult_STATUS_OK,
		Payload: &controlpb.CommandResult_Ping{Ping: &controlpb.PingResult{Host: "example.test", Count: count, Transmitted: new(count), Received: new(count)}},
	}}
}

func controlProgressScope(state State, agentID string) tuirun.Scope {
	for _, target := range state.Targets {
		if AgentKey(target.Agent) != agentID || len(target.PlannedSteps) == 0 {
			continue
		}
		return tuirun.Scope{Kind: tuirun.ScopeCheck, AgentID: agentID, TargetID: target.Target.ID, CheckID: target.PlannedSteps[0].ID}
	}
	return tuirun.Scope{}
}

func waitControlProgressEvent(t *testing.T, events <-chan tuirun.Event, what string) tuirun.Event {
	t.Helper()
	select {
	case event := <-events:
		return event
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
		return tuirun.Event{}
	}
}

func TestProducerControlProgressKeepsPausedAgentWithCompletedPeer(t *testing.T) {
	compiled, state := controlProgressPlan(t, []control.AgentInfo{{ID: "a"}, {ID: "b"}})
	controls := tuirun.NewControls()
	scope := controlProgressScope(state, "a")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	started := make(chan string, 2)
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseAll := func() { releaseOnce.Do(func() { close(release) }) }
	defer releaseAll()
	var seenMu sync.Mutex
	seen := map[string]bool{}
	r := boundaryRunner(func(_ context.Context, agent control.AgentInfo, op command.Operation) (runner.Result, error) {
		if op.Name == "ping" {
			seenMu.Lock()
			first := !seen[agent.ID]
			seen[agent.ID] = true
			seenMu.Unlock()
			if first {
				started <- agent.ID
			}
			<-release
		}
		return controlProgressPing(op), nil
	})

	var mu sync.Mutex
	paused := make(chan tuirun.Event, 1)
	completed := make(chan tuirun.Event, 1)
	sink := boundarySink(func(_ context.Context, event tuirun.Event) error {
		mu.Lock()
		state.Apply(event)
		mu.Unlock()
		switch {
		case event.Kind == tuirun.EventControlApplied && event.Agent.ID == "a" && event.Status == "paused":
			select {
			case paused <- event:
			default:
			}
		case event.Kind == tuirun.EventAgentProgress && event.Agent.ID == "b" && event.Status == "completed":
			select {
			case completed <- event:
			default:
			}
		}
		return nil
	})
	done := make(chan error, 1)
	go func() {
		_, err := tuirun.Execute(ctx, compiled, r, tuirun.ExecuteOptions{Rounds: 1, Controls: controls, Sinks: []tuirun.Sink{sink}})
		done <- err
	}()

	for range 2 {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatal("agents did not reach the fake operation")
		}
	}
	if err := controls.Pause(scope); err != nil {
		t.Fatal(err)
	}
	releaseAll()

	pausedEvent := waitControlProgressEvent(t, paused, "paused control applied")
	completedEvent := waitControlProgressEvent(t, completed, "completed agent progress")
	if pausedEvent.Round != 1 || pausedEvent.Agent.ID != "a" || pausedEvent.Scope.Kind != tuirun.ScopeCheck || pausedEvent.Scope.AgentID != "a" {
		t.Fatalf("paused event lost round/agent/check scope: %+v", pausedEvent)
	}
	if completedEvent.Round != 1 || completedEvent.Agent.ID != "b" {
		t.Fatalf("completed progress lost round/agent identity: %+v", completedEvent)
	}
	mu.Lock()
	phase, status := state.Phase, state.RoundStatus
	mu.Unlock()
	if phase != "paused" || status != "running" {
		t.Fatalf("paused agent with completed peer state=%q/%q", phase, status)
	}

	if err := controls.Resume(scope); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("execution failed: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("resume did not finish the paused agent")
	}
	mu.Lock()
	phase, status = state.Phase, state.RoundStatus
	mu.Unlock()
	if phase != "completed" || status != "ok" {
		t.Fatalf("finished aggregate state=%q/%q", phase, status)
	}
}

func TestProducerControlProgressSingleAgentResumeIsRunning(t *testing.T) {
	compiled, state := controlProgressPlan(t, []control.AgentInfo{{ID: "a"}})
	controls := tuirun.NewControls()
	scope := controlProgressScope(state, "a")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseAll := func() { releaseOnce.Do(func() { close(release) }) }
	defer releaseAll()
	var seenMu sync.Mutex
	first := true
	r := boundaryRunner(func(_ context.Context, _ control.AgentInfo, op command.Operation) (runner.Result, error) {
		if op.Name == "ping" {
			seenMu.Lock()
			once := first
			first = false
			seenMu.Unlock()
			if once {
				started <- struct{}{}
			}
			<-release
		}
		return controlProgressPing(op), nil
	})

	var mu sync.Mutex
	paused := make(chan string, 1)
	resumed := make(chan string, 1)
	sink := boundarySink(func(_ context.Context, event tuirun.Event) error {
		mu.Lock()
		state.Apply(event)
		phase := state.Phase
		mu.Unlock()
		if event.Kind != tuirun.EventControlApplied || event.Agent.ID != "a" {
			return nil
		}
		switch event.Status {
		case "paused":
			select {
			case paused <- phase:
			default:
			}
		case "running":
			select {
			case resumed <- phase:
			default:
			}
		}
		return nil
	})
	done := make(chan error, 1)
	go func() {
		_, err := tuirun.Execute(ctx, compiled, r, tuirun.ExecuteOptions{Rounds: 1, Controls: controls, Sinks: []tuirun.Sink{sink}})
		done <- err
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("agent did not reach the fake operation")
	}
	if err := controls.Pause(scope); err != nil {
		t.Fatal(err)
	}
	releaseAll()
	select {
	case phase := <-paused:
		if phase != "paused" {
			t.Fatalf("single-agent paused phase=%q", phase)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("paused control was not applied")
	}
	if err := controls.Resume(scope); err != nil {
		t.Fatal(err)
	}
	select {
	case phase := <-resumed:
		if phase != "running" {
			t.Fatalf("single-agent resume phase=%q", phase)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("running control was not applied")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("execution failed: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("resume did not finish the agent")
	}
}

func TestProducerSkipTargetFinishedCarriesTargetScope(t *testing.T) {
	op, err := command.PingOperation(command.PingOptions{Host: "example.test"})
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := tuirun.Compile(tuirun.Plan{
		Networks: []tuirun.Network{{Name: "lab", SSID: "Lab", PSK: tuirun.SecretValue("synthetic-passphrase"), Rotation: "per_target", WaitConnected: true, RequireIP: true, DisconnectAfter: true}},
		Checks:   []tuirun.Check{tuirun.NewCheck("ping", "", op, tuirun.Policy{}, false)},
	}, []control.AgentInfo{{ID: "a", Hello: &controlpb.AgentHello{Capabilities: []string{"wifi.forget.typed"}}}})
	if err != nil {
		t.Fatal(err)
	}
	var skipped []tuirun.Event
	_, err = tuirun.Execute(context.Background(), compiled, boundaryRunner(func(_ context.Context, _ control.AgentInfo, op command.Operation) (runner.Result, error) {
		if op.Name == "wifi.forget" {
			return runner.Result{Result: &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_FAILED, Message: "synthetic rotation failure"}}, nil
		}
		return controlProgressPing(op), nil
	}), tuirun.ExecuteOptions{Rounds: 1, Sinks: []tuirun.Sink{boundarySink(func(_ context.Context, event tuirun.Event) error {
		if event.Kind == tuirun.EventTargetFinished && event.Status == "skipped" {
			skipped = append(skipped, event)
		}
		return nil
	})}})
	if err != nil {
		t.Fatalf("execution failed: %v", err)
	}
	if len(skipped) != 1 {
		t.Fatalf("skipped target events=%d", len(skipped))
	}
	event := skipped[0]
	if event.Scope.Kind != tuirun.ScopeTarget || event.Scope.AgentID != event.Agent.ID || event.Scope.AgentID != "a" || event.Scope.TargetID == "" || event.Scope.TargetID != event.Target.ID || event.Round != 1 {
		t.Fatalf("skipped target scope=%+v agent=%+v target=%+v round=%d", event.Scope, event.Agent, event.Target, event.Round)
	}
}
