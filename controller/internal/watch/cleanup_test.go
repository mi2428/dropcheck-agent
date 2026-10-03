package watch

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"dropcheck/controller/internal/command"
	"dropcheck/controller/internal/control"
	"dropcheck/controller/internal/controlpb"
	"dropcheck/controller/internal/runner"
)

type cleanupRunnerFunc func(context.Context, command.Operation) (runner.Result, error)

func (f cleanupRunnerFunc) Run(ctx context.Context, _ control.AgentInfo, op command.Operation) (runner.Result, error) {
	return f(ctx, op)
}

type cleanupSinkFunc func(context.Context, Event) error

func (f cleanupSinkFunc) Emit(ctx context.Context, event Event) error { return f(ctx, event) }

func TestRunTargetCleanupOnEveryExit(t *testing.T) {
	for _, phase := range []string{"normal", "skip", "cancel", "sink failure", "check failure", "pause failure", "connect failure"} {
		for _, optOut := range []bool{false, true} {
			t.Run(phase+"/optOut="+strconv.FormatBool(optOut), func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				skip, pause := NewSkipController(), NewPauseController()
				target := Target{SSID: "Test Network", ForgetAfter: new(true)}
				if optOut {
					target.DisconnectAfter, target.ForgetAfter = new(false), new(false)
				}
				plan := Plan{}
				if phase == "check failure" {
					plan.Checks = []Check{{Type: "invalid"}}
				}
				var operations []string
				opRunner := cleanupRunnerFunc(func(opCtx context.Context, op command.Operation) (runner.Result, error) {
					operations = append(operations, op.Name)
					if op.Name == "wifi.disconnect" || op.Name == "wifi.forget" {
						deadline, bounded := opCtx.Deadline()
						if opCtx.Err() != nil || !bounded || time.Until(deadline) > cleanupTimeout {
							t.Fatalf("cleanup context: error=%v deadline=%v", opCtx.Err(), deadline)
						}
					}
					if op.Name == "wifi.connect" && phase == "pause failure" {
						pause.Pause()
						cancel()
					}
					if op.Name == "wifi.connect" && phase == "connect failure" {
						return runner.Result{Result: &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_FAILED}}, nil
					}
					if op.Name == "wifi.wait" {
						switch phase {
						case "skip":
							skip.Skip()
							return runner.Result{}, opCtx.Err()
						case "cancel":
							cancel()
							return runner.Result{}, opCtx.Err()
						}
					}
					return runner.Result{Result: &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_OK}}, nil
				})
				sinkErr := errors.New("test sink failed")
				var events []Event
				result, err := runTarget(ctx, plan, opRunner, control.AgentInfo{}, 1, target, bandSupport{}, pause, skip, func(event Event) error {
					events = append(events, event)
					if phase == "sink failure" && event.Kind == EventStepFinished && event.Step.Name == "wait_connected" {
						return sinkErr
					}
					return nil
				})
				for _, op := range []string{"wifi.disconnect", "wifi.forget"} {
					want := 1
					if optOut {
						want = 0
					}
					if got := countString(operations, op); got != want {
						t.Fatalf("%s calls = %d, want %d; operations=%v", op, got, want, operations)
					}
				}
				switch phase {
				case "skip":
					if err != nil || result != targetSkipped {
						t.Fatalf("skip outcome = %v, %v", result, err)
					}
				case "cancel", "pause failure":
					if !errors.Is(err, context.Canceled) {
						t.Fatalf("error = %v, want canceled", err)
					}
				case "sink failure":
					if !errors.Is(err, sinkErr) {
						t.Fatalf("primary error lost: %v", err)
					}
				case "check failure":
					if err == nil {
						t.Fatal("invalid check unexpectedly succeeded")
					}
				default:
					if err != nil {
						t.Fatal(err)
					}
				}
			})
		}
	}
}

func TestRunTargetDoesNotCleanUpBeforeConnectAttempt(t *testing.T) {
	sinkErr := errors.New("connect start event failed")
	_, err := runTarget(context.Background(), Plan{}, forbiddenRunner{t}, control.AgentInfo{}, 1, Target{SSID: "Test Network"}, bandSupport{}, nil, nil, func(event Event) error {
		if event.Kind == EventStepStarted {
			return sinkErr
		}
		return nil
	})
	if !errors.Is(err, sinkErr) {
		t.Fatalf("error = %v", err)
	}
}

func TestCleanupFailurePreservesPrimaryErrorAndAttemptsForget(t *testing.T) {
	primary := errors.New("primary sink failure")
	var operations []string
	var events []Event
	opRunner := cleanupRunnerFunc(func(_ context.Context, op command.Operation) (runner.Result, error) {
		operations = append(operations, op.Name)
		status := controlpb.CommandResult_STATUS_OK
		if op.Name == "wifi.disconnect" {
			status = controlpb.CommandResult_STATUS_FAILED
		}
		return runner.Result{Result: &controlpb.CommandResult{Status: status, Message: "test result"}}, nil
	})
	_, err := runTarget(context.Background(), Plan{}, opRunner, control.AgentInfo{}, 1, Target{SSID: "Test Network", ForgetAfter: new(true)}, bandSupport{}, nil, nil, func(event Event) error {
		events = append(events, event)
		if event.Kind == EventStepFinished && event.Step.Name == "wait_connected" || event.Step.Type == "cleanup" && event.Kind == EventStepStarted {
			return primary
		}
		return nil
	})
	if !errors.Is(err, primary) || !strings.Contains(err.Error(), "disconnect cleanup failed") {
		t.Fatalf("primary or cleanup failure lost: %v", err)
	}
	if countString(operations, "wifi.disconnect") != 1 || countString(operations, "wifi.forget") != 1 {
		t.Fatalf("cleanup operations = %v", operations)
	}
	if _, ok := firstEvent(events, EventStepFinished, func(event Event) bool { return event.Step.Name == "disconnect" && event.Status == "failed" }); !ok {
		t.Fatal("cleanup failure event missing")
	}
}

func TestWatchCleanupEventsHaveIndependentContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	opRunner := cleanupRunnerFunc(func(opCtx context.Context, op command.Operation) (runner.Result, error) {
		if op.Name == "wifi.wait" {
			cancel()
			return runner.Result{}, opCtx.Err()
		}
		return runner.Result{Result: &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_OK}}, nil
	})
	cleanupEvents := 0
	err := Run(ctx, Plan{Targets: []Target{{SSID: "Test Network"}}}, opRunner, control.AgentInfo{}, cleanupSinkFunc(func(eventCtx context.Context, event Event) error {
		if event.Step.Type == "cleanup" {
			cleanupEvents++
			if eventCtx.Err() != nil {
				t.Fatalf("cleanup event has canceled context: %v", eventCtx.Err())
			}
			if _, bounded := eventCtx.Deadline(); !bounded {
				t.Fatal("cleanup event delivery is not bounded")
			}
		}
		return nil
	}))
	if err != nil || cleanupEvents != 2 {
		t.Fatalf("Run() error=%v cleanupEvents=%d", err, cleanupEvents)
	}
}

func TestCleanupTimeoutRecordsFailureAndStillAttemptsForget(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	var operations []string
	var events []Event
	opRunner := cleanupRunnerFunc(func(opCtx context.Context, op command.Operation) (runner.Result, error) {
		operations = append(operations, op.Name)
		<-opCtx.Done()
		return runner.Result{}, opCtx.Err()
	})
	err := runCleanup(ctx, opRunner, control.AgentInfo{}, 1, Target{SSID: "Test Network", ForgetAfter: new(true)}, func(event Event) error {
		events = append(events, event)
		return nil
	})
	if !errors.Is(err, context.DeadlineExceeded) || countString(operations, "wifi.forget") != 1 {
		t.Fatalf("error=%v operations=%v", err, operations)
	}
	if countEvents(events, EventStepFinished, func(event Event) bool { return event.Status == "failed" }) != 2 {
		t.Fatalf("timeout failure events missing: %v", events)
	}
}

func TestCleanupPauseBoundaryResumesOrTerminates(t *testing.T) {
	for _, action := range []string{"resume", "cancel", "skip"} {
		t.Run(action, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			pause, skip := NewPauseController(), NewSkipController()
			boundary := make(chan struct{}, 1)
			cleanups := make(chan string, 4)
			opRunner := cleanupRunnerFunc(func(opCtx context.Context, op command.Operation) (runner.Result, error) {
				if op.Name == "wifi.disconnect" || op.Name == "wifi.forget" {
					if opCtx.Err() != nil {
						return runner.Result{}, errors.New("cleanup inherited cancellation")
					}
					cleanups <- op.Name
				}
				return runner.Result{Result: &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_OK}}, nil
			})
			type outcome struct {
				result targetResult
				err    error
			}
			done := make(chan outcome, 1)
			go func() {
				result, err := runTarget(ctx, Plan{}, opRunner, control.AgentInfo{}, 1, Target{SSID: "Test Network", ForgetAfter: new(true)}, bandSupport{}, pause, skip, func(event Event) error {
					if event.Kind == EventStepFinished && event.Step.Name == "wait_connected" {
						pause.Pause()
						boundary <- struct{}{}
					}
					return nil
				})
				done <- outcome{result, err}
			}()
			select {
			case <-boundary:
			case <-time.After(time.Second):
				t.Fatal("target did not reach cleanup boundary")
			}
			gateCtx, stop := context.WithTimeout(ctx, time.Second)
			defer stop()
			ticker := time.NewTicker(time.Millisecond)
			defer ticker.Stop()
			for {
				skip.mu.Lock()
				active := len(skip.active)
				skip.mu.Unlock()
				if active == 1 {
					break
				}
				select {
				case <-ticker.C:
				case <-gateCtx.Done():
					t.Fatal("cleanup pause gate did not register skip context")
				}
			}
			select {
			case op := <-cleanups:
				t.Fatalf("cleanup %s ran while paused", op)
			case got := <-done:
				t.Fatalf("target returned while paused: %v", got)
			case <-time.After(20 * time.Millisecond):
			}
			switch action {
			case "resume":
				pause.Resume()
			case "cancel":
				cancel()
			case "skip":
				skip.Skip()
			}
			select {
			case got := <-done:
				want := targetPassed
				if action == "skip" {
					want = targetSkipped
				}
				if action == "cancel" {
					want = targetFailed
					if !errors.Is(got.err, context.Canceled) {
						t.Fatalf("cancel error = %v", got.err)
					}
				} else if got.err != nil {
					t.Fatal(got.err)
				}
				if got.result != want {
					t.Fatalf("result = %v, want %v", got.result, want)
				}
			case <-time.After(time.Second):
				t.Fatalf("target did not finish after %s", action)
			}
			if len(cleanups) != 2 || <-cleanups != "wifi.disconnect" || <-cleanups != "wifi.forget" {
				t.Fatal("disconnect and forget were not each attempted exactly once")
			}
			skip.mu.Lock()
			active := len(skip.active)
			skip.mu.Unlock()
			if active != 0 {
				t.Fatal("pause skip context leaked")
			}
		})
	}
}
