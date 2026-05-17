package watch

import (
	"context"
	"strings"
	"testing"

	"dropcheck/controller/internal/command"
	"dropcheck/controller/internal/control"
	"dropcheck/controller/internal/controlpb"
	"dropcheck/controller/internal/runner"
)

type failureCauseRunner struct{}

func (failureCauseRunner) Run(context.Context, control.AgentInfo, command.Operation) (runner.Result, error) {
	return runner.Result{Result: &controlpb.CommandResult{
		Status:  controlpb.CommandResult_STATUS_FAILED,
		Message: "wifi connect timed out",
	}}, nil
}

func (failureCauseRunner) FailureCause(context.Context, control.AgentInfo, FailureCauseContext) string {
	return "wifi failure cause: association rejected status=37 reason=REQUEST_DECLINED"
}

type liveFailureCauseRunner struct{}

func (liveFailureCauseRunner) Run(context.Context, control.AgentInfo, command.Operation) (runner.Result, error) {
	return runner.Result{Result: &controlpb.CommandResult{
		Status:  controlpb.CommandResult_STATUS_OK,
		Message: "ok",
	}}, nil
}

func (liveFailureCauseRunner) WatchFailureCause(_ context.Context, _ control.AgentInfo, _ FailureCauseContext, emit func(string)) func() {
	emit("wifi failure cause: association rejected status=37 reason=REQUEST_DECLINED")
	return func() {}
}

func TestRunRequiredStepEmitsFailureCauseLog(t *testing.T) {
	var events []Event
	emit := func(event Event) error {
		events = append(events, event)
		return nil
	}
	ok, err := runRequiredStep(
		context.Background(),
		failureCauseRunner{},
		control.AgentInfo{ID: "agent-a", Hello: &controlpb.AgentHello{AdbSerial: "serial-a"}},
		7,
		Target{Name: "lab-u6-2g", SSID: "Lab", BSSID: "aa:bb:cc:dd:ee:ff"},
		StepSnapshot{Name: "connect", Type: "connect", Operation: "wifi.connect"},
		command.Operation{Name: "wifi.connect"},
		emit,
	)
	if err != nil {
		t.Fatalf("runRequiredStep() error = %v", err)
	}
	if ok {
		t.Fatal("runRequiredStep() ok = true, want false")
	}
	if len(events) != 3 {
		t.Fatalf("events = %d, want 3: %#v", len(events), events)
	}
	log := events[2]
	if log.Kind != EventLog || log.Status != "warn" {
		t.Fatalf("failure cause event = %#v, want warn log", log)
	}
	if log.Round != 7 || log.Target.Name != "lab-u6-2g" || log.Step.Name != "connect" {
		t.Fatalf("failure cause context = %#v", log)
	}
	if !strings.Contains(log.Message, "REQUEST_DECLINED") {
		t.Fatalf("failure cause message = %q", log.Message)
	}
}

func TestRunRequiredStepEmitsLiveFailureCauseLog(t *testing.T) {
	var events []Event
	emit := func(event Event) error {
		events = append(events, event)
		return nil
	}
	ok, err := runRequiredStep(
		context.Background(),
		liveFailureCauseRunner{},
		control.AgentInfo{ID: "agent-a", Hello: &controlpb.AgentHello{AdbSerial: "serial-a"}},
		8,
		Target{Name: "lab-u6-2g", SSID: "Lab", BSSID: "aa:bb:cc:dd:ee:ff"},
		StepSnapshot{Name: "connect", Type: "connect", Operation: "wifi.connect"},
		command.Operation{Name: "wifi.connect"},
		emit,
	)
	if err != nil {
		t.Fatalf("runRequiredStep() error = %v", err)
	}
	if !ok {
		t.Fatal("runRequiredStep() ok = false, want true")
	}
	if len(events) != 3 {
		t.Fatalf("events = %d, want 3: %#v", len(events), events)
	}
	if events[1].Kind != EventLog || !strings.Contains(events[1].Message, "REQUEST_DECLINED") {
		t.Fatalf("live failure cause event = %#v", events[1])
	}
	if events[2].Kind != EventStepFinished || events[2].Status != "ok" {
		t.Fatalf("step finished event = %#v", events[2])
	}
}
