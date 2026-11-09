package tuirun

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mi2428/dropcheck-agent/controller/internal/command"
	"github.com/mi2428/dropcheck-agent/controller/internal/control"
	"github.com/mi2428/dropcheck-agent/controller/internal/runner"
)

type diagnosticHookRunner struct {
	t                           *testing.T
	fail                        string
	cancel                      context.CancelFunc
	primary                     error
	started, stopped, collected int
	active                      bool
	ops                         []string
	causes                      []FailureCauseContext
}

func (r *diagnosticHookRunner) Run(ctx context.Context, _ control.AgentInfo, op command.Operation) (runner.Result, error) {
	r.ops = append(r.ops, op.Name)
	connection := op.Name == "wifi.connect" || op.Name == "wifi.wait"
	if r.active != connection {
		r.t.Errorf("monitor lifecycle for %s: active=%v", op.Name, r.active)
	}
	if ctx.Err() != nil {
		r.t.Errorf("cleanup received canceled context: %v", ctx.Err())
	}
	if connection && r.cancel != nil {
		r.cancel()
		return runner.Result{}, context.Canceled
	}
	if op.Name == r.fail {
		return runner.Result{}, r.primary
	}
	return runner.Result{Result: runtimeFixture(op)}, nil
}

func (r *diagnosticHookRunner) ObserveFailureCause(ctx context.Context, agent control.AgentInfo, cause FailureCauseContext, emit func(string)) func() {
	if ctx.Err() != nil || agent.ID != "agent-a" || cause.Target.ID == "" || cause.Step.ID == "" {
		r.t.Errorf("invalid monitor scope: agent=%s cause=%+v", agent.ID, cause)
	}
	r.started++
	r.active = true
	r.causes = append(r.causes, cause)
	emit("wifi connect state: supplicant=ASSOCIATING")
	joined := make(chan struct{})
	go func() {
		defer close(joined)
		<-ctx.Done()
		emit("late diagnostic after cancellation")
	}()
	return func() {
		<-joined
		r.active = false
		r.stopped++
	}
}

func (r *diagnosticHookRunner) FailureCause(ctx context.Context, agent control.AgentInfo, cause FailureCauseContext) string {
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) > failureCauseTimeout || ctx.Err() != nil || agent.ID != "agent-a" || cause.Err != r.primary {
		r.t.Errorf("collector was not bounded/attributed: deadline=%v cause=%+v", deadline, cause)
	}
	r.collected++
	return "wifi failure cause: diagnostic unavailable"
}

func TestProducerConnectWaitDiagnosticsAreScopedStoppedAndSecondary(t *testing.T) {
	for _, failing := range []string{"", "wifi.connect", "wifi.wait"} {
		t.Run("failure="+failing, func(t *testing.T) {
			plan := compiledFixture(t, Plan{Networks: []Network{{SSID: "Lab", PSK: SecretValue("synthetic-passphrase"), WaitConnected: true, DisconnectAfter: true, ForgetAfter: true}}})
			r := &diagnosticHookRunner{t: t, fail: failing, primary: errors.New("synthetic primary failure")}
			var logs []Event
			report, err := Execute(context.Background(), plan, r, ExecuteOptions{Rounds: 2, Sinks: []Sink{cleanupSinkFunc(func(_ context.Context, event Event) error {
				if event.Kind == EventLog {
					logs = append(logs, event)
				}
				return nil
			})}})
			if failing == "" && (err != nil || !report.Passed()) || failing != "" && !errors.Is(err, r.primary) {
				t.Fatalf("hook replaced primary outcome: report=%s err=%v", report.Outcome, err)
			}
			monitors := 4
			if failing == "wifi.connect" {
				monitors = 2
			}
			collectors := 0
			if failing != "" {
				collectors = 2
			}
			if r.started != monitors || r.stopped != monitors || r.collected != collectors || r.active {
				t.Fatalf("hook lifecycle: starts=%d stops=%d collectors=%d active=%v", r.started, r.stopped, r.collected, r.active)
			}
			cleanups := 0
			for _, op := range r.ops {
				if op == "wifi.disconnect" || op == "wifi.forget" {
					cleanups++
				}
			}
			if cleanups != 4 || len(logs) != monitors+collectors {
				t.Fatalf("cleanup/log delivery: cleanups=%d logs=%d", cleanups, len(logs))
			}
			for _, event := range logs {
				if event.Scope.AgentID != "agent-a" || event.Agent.ID != "agent-a" || event.Scope.TargetID != "target/0" || event.Target.ID != "target/0" || event.Scope.CheckID != event.Step.ID || event.Round < 1 || event.Round > 2 || strings.Contains(event.Message, "late diagnostic") {
					t.Fatalf("cross-scope or late diagnostic: %+v", event)
				}
			}
		})
	}
}

func TestProducerCancellationStopsMonitorWithoutNewDiagnosticsAndCleansUp(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	plan := compiledFixture(t, Plan{Networks: []Network{{SSID: "Lab", PSK: SecretValue("synthetic-passphrase"), WaitConnected: true, DisconnectAfter: true, ForgetAfter: true}}})
	r := &diagnosticHookRunner{t: t, cancel: cancel}
	_, err := Execute(ctx, plan, r, ExecuteOptions{Rounds: 1})
	if err != nil || r.started != 1 || r.stopped != 1 || r.collected != 0 || r.active || strings.Join(r.ops, ",") != "wifi.connect,wifi.disconnect,wifi.forget" {
		t.Fatalf("cancel started new diagnostics or lost cleanup: ops=%v starts=%d stops=%d collect=%d err=%v", r.ops, r.started, r.stopped, r.collected, err)
	}
}

func TestProducerDurationsUseAttemptTimestampsIncludingKnownZero(t *testing.T) {
	check := NewCheck("status", "", command.WifiStatusOperation(), Policy{}, false)
	plan := compiledFixture(t, Plan{Networks: []Network{{SSID: "Lab", ReadOnly: true}}, Checks: []Check{check}})
	for _, elapsed := range []time.Duration{0, 12 * time.Millisecond} {
		t.Run(elapsed.String(), func(t *testing.T) {
			clock := time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)
			terminals := 0
			_, err := execute(context.Background(), plan, cleanupRunnerFunc(func(_ context.Context, op command.Operation) (runner.Result, error) {
				clock = clock.Add(elapsed)
				return runner.Result{Result: runtimeFixture(op)}, nil
			}), ExecuteOptions{Rounds: 1, Sinks: []Sink{cleanupSinkFunc(func(_ context.Context, event Event) error {
				if event.Kind == EventAttemptFinished || event.Kind == EventStepFinished || event.Kind == EventTargetFinished {
					terminals++
					if !event.DurationKnown || event.Duration != elapsed.Milliseconds() {
						t.Errorf("producer timing: %+v", event)
					}
				}
				if event.Attempt != nil && event.Duration != event.Attempt.Ended.Sub(event.Attempt.Started).Milliseconds() {
					t.Error("event duration differs from measured attempt")
				}
				return nil
			})}}, func() time.Time { return clock }, sleepContext)
			if err != nil || terminals != 3 {
				t.Fatalf("terminal count=%d err=%v", terminals, err)
			}
		})
	}
}
