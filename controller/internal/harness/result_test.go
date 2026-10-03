package harness

import (
	"context"
	"strings"
	"testing"

	"dropcheck/controller/internal/command"
	"dropcheck/controller/internal/control"
	"dropcheck/controller/internal/controlpb"
)

func TestArchiveRunnerConsumesMatchingStepsInIndexOrder(t *testing.T) {
	op, err := command.PingOperation(command.PingOptions{Host: "8.8.8.8", Count: "3"})
	if err != nil {
		t.Fatal(err)
	}
	target := ResultTarget{
		Name:       "lab",
		SourceName: "fixture",
		Steps: []*controlpb.StandaloneMeasurementStep{
			archivedPingStep(6, op.Command, 2, controlpb.CommandResult_STATUS_OK),
			archivedPingStep(4, op.Command, 0, controlpb.CommandResult_STATUS_FAILED),
			archivedPingStep(4, op.Command, 1, controlpb.CommandResult_STATUS_OK),
		},
	}
	target.Steps[1].Attempt = 2
	target.Steps[2].Attempt = 1
	r := &archiveRunner{target: target}
	for attempt, wantReceived := range []uint32{1, 0, 2} {
		result, err := r.Run(context.Background(), control.AgentInfo{}, op)
		if err != nil {
			t.Fatalf("Run attempt %d: %v", attempt+1, err)
		}
		if got := result.Result.GetPing().GetReceived(); got != wantReceived {
			t.Fatalf("attempt %d received = %d, want %d", attempt+1, got, wantReceived)
		}
	}
	if _, err := r.Run(context.Background(), control.AgentInfo{}, op); err == nil || !strings.Contains(err.Error(), "no archived ping step") {
		t.Fatalf("exhausted Run error = %v, want missing recorded observation", err)
	}
}

func TestArchiveRetryFailsAfterRecordedObservationsAreExhausted(t *testing.T) {
	op, err := command.PingOperation(command.PingOptions{Host: "8.8.8.8", Count: "3"})
	if err != nil {
		t.Fatal(err)
	}
	r := &archiveRunner{target: ResultTarget{Steps: []*controlpb.StandaloneMeasurementStep{
		archivedPingStep(1, op.Command, 0, controlpb.CommandResult_STATUS_FAILED),
	}}}
	failures := runCheckWithRetry(context.Background(), r, control.AgentInfo{}, Network{}, step{
		name: "ping", operation: op, policy: runPolicy{retryAttempts: 2},
	})
	if len(failures) != 1 || !strings.Contains(failures[0], "no archived ping step") {
		t.Fatalf("failures = %v, want exhausted observation", failures)
	}
}

func archivedPingStep(index uint32, cmd *controlpb.RunCommand, received uint32, status controlpb.CommandResult_Status) *controlpb.StandaloneMeasurementStep {
	return &controlpb.StandaloneMeasurementStep{
		StepIndex: index,
		StepName:  "ping",
		Command:   cmd,
		Result: &controlpb.CommandResult{
			Status: status,
			Payload: &controlpb.CommandResult_Ping{Ping: &controlpb.PingResult{
				Host:        "8.8.8.8",
				Transmitted: 3,
				Received:    received,
			}},
		},
	}
}
