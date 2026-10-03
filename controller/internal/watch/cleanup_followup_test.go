package watch

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"dropcheck/controller/internal/command"
	"dropcheck/controller/internal/control"
	"dropcheck/controller/internal/controlpb"
	"dropcheck/controller/internal/runner"
)

func TestRunWithOptionsCancellationPreservesFailures(t *testing.T) {
	primary := errors.New("synthetic primary failure")
	cleanup := errors.New("synthetic cleanup failure")
	sink := errors.New("synthetic sink failure")
	for _, tc := range []struct {
		name       string
		waitErr    error
		cleanupErr error
		sinkErr    error
		want       error
	}{
		{name: "pure cancel", waitErr: context.Canceled},
		{name: "wrapped cancel", waitErr: fmt.Errorf("wait: %w", context.Canceled)},
		{name: "nested joined cancel", waitErr: errors.Join(context.Canceled, fmt.Errorf("wait: %w", errors.Join(context.Canceled, context.Canceled)))},
		{name: "independent primary", waitErr: primary, want: primary},
		{name: "joined primary", waitErr: errors.Join(context.Canceled, fmt.Errorf("wait: %w", primary)), want: primary},
		{name: "cleanup failure", waitErr: context.Canceled, cleanupErr: cleanup, want: cleanup},
		{name: "independent cleanup deadline", waitErr: context.Canceled, cleanupErr: context.DeadlineExceeded, want: context.DeadlineExceeded},
		{name: "cleanup sink failure", waitErr: context.Canceled, sinkErr: sink, want: sink},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var operations []string
			opRunner := cleanupRunnerFunc(func(opCtx context.Context, op command.Operation) (runner.Result, error) {
				operations = append(operations, op.Name)
				switch op.Name {
				case "wifi.wait":
					cancel()
					return runner.Result{}, tc.waitErr
				case "wifi.disconnect", "wifi.forget":
					deadline, bounded := opCtx.Deadline()
					if opCtx.Err() != nil || !bounded || time.Until(deadline) > cleanupTimeout {
						t.Fatalf("cleanup context error=%v bounded=%v", opCtx.Err(), bounded)
					}
					if op.Name == "wifi.disconnect" && tc.cleanupErr != nil {
						return runner.Result{}, tc.cleanupErr
					}
				}
				return runner.Result{Result: &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_OK}}, nil
			})
			err := RunWithOptions(ctx, Plan{Targets: []Target{{SSID: "Test Network", ForgetAfter: new(true)}}}, opRunner, control.AgentInfo{}, cleanupSinkFunc(func(_ context.Context, event Event) error {
				if event.Step.Type == "cleanup" {
					return tc.sinkErr
				}
				return nil
			}), RunOptions{})
			if tc.want == nil && err != nil || tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("RunWithOptions error=%v, want preserved %v", err, tc.want)
			}
			if countString(operations, "wifi.disconnect") != 1 || countString(operations, "wifi.forget") != 1 {
				t.Fatalf("cleanup was not attempted exactly once: %v", operations)
			}
		})
	}
}

func TestRunWithOptionsCanceledSinkPreservesIndependentError(t *testing.T) {
	primary := errors.New("synthetic primary sink failure")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := RunWithOptions(ctx, Plan{Targets: []Target{{SSID: "Test Network"}}}, okRunner{}, control.AgentInfo{}, cleanupSinkFunc(func(_ context.Context, event Event) error {
		if event.Kind == EventStepStarted && event.Step.Name == "wait_connected" {
			cancel()
			return errors.Join(fmt.Errorf("sink: %w", context.Canceled), primary)
		}
		return nil
	}), RunOptions{})
	if !errors.Is(err, primary) {
		t.Fatalf("independent primary sink failure lost: %v", err)
	}
}

func TestRunWithOptionsCanceledSiblingOperationsPreservePrimaryError(t *testing.T) {
	for _, phase := range []string{"capabilities", "mac rotation", "gateway status", "gateway ping"} {
		t.Run(phase, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			primary := errors.New("synthetic sibling operation failure")
			target := Target{SSID: "Test Network"}
			plan := Plan{}
			if phase == "mac rotation" {
				target.MacRotation = macRotationPerTarget
			}
			if phase == "gateway status" || phase == "gateway ping" {
				plan.Checks = []Check{{Type: "gateway_ping", Family: "ipv4"}}
			}
			plan.Targets = []Target{target}
			opRunner := cleanupRunnerFunc(func(_ context.Context, op command.Operation) (runner.Result, error) {
				if phase == "capabilities" && op.Name == "wifi.capabilities" || phase == "mac rotation" && op.Name == "wifi.forget" || phase == "gateway status" && op.Name == "ip.status" || phase == "gateway ping" && op.Name == "ping" {
					cancel()
					return runner.Result{}, errors.Join(context.Canceled, primary)
				}
				result := &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_OK}
				if op.Name == "ip.status" {
					result.Payload = &controlpb.CommandResult_IpStatus{IpStatus: &controlpb.IpStatus{Routes: []string{"0.0.0.0/0 -> 192.0.2.1 wlan0"}}}
				}
				return runner.Result{Result: result}, nil
			})
			if err := RunWithOptions(ctx, plan, opRunner, control.AgentInfo{}, nil, RunOptions{}); !errors.Is(err, primary) {
				t.Fatalf("primary error lost at %s: %v", phase, err)
			}
		})
	}
}
