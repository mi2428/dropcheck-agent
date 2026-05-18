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

type forbiddenRunner struct {
	t *testing.T
}

func (r forbiddenRunner) Run(context.Context, control.AgentInfo, command.Operation) (runner.Result, error) {
	r.t.Fatal("operation runner should not be called for an unsupported target band")
	return runner.Result{}, nil
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

func TestRunRoundSkipsUnsupportedTargetBandWithoutFailure(t *testing.T) {
	var events []Event
	emit := func(event Event) error {
		events = append(events, event)
		return nil
	}
	plan := Plan{
		Name: "lab-watch",
		Targets: []Target{{
			Name: "lab-6g",
			SSID: "Lab",
			Band: "6ghz",
		}},
		Checks: []Check{{Name: "Ping CF IPv6", Type: "ping", Host: "2606:4700:4700::1111"}},
	}
	support := bandSupportFromCapabilities(&controlpb.WifiCapabilities{SupportedBands: []string{"2.4GHz", "5GHz"}})
	failed, err := runRound(context.Background(), plan, forbiddenRunner{t: t}, control.AgentInfo{ID: "agent-a"}, 9, support, emit)
	if err != nil {
		t.Fatalf("runRound() error = %v", err)
	}
	if failed != 0 {
		t.Fatalf("runRound() failed targets = %d, want 0 for unsupported-band skip", failed)
	}
	if len(events) != 7 {
		t.Fatalf("events = %d, want round start, target start, 3 skipped steps, target finish, round finish: %#v", len(events), events)
	}
	var skippedSteps int
	for _, event := range events {
		if event.Kind == EventFinding {
			t.Fatalf("unsupported-band skip should not emit findings: %#v", event)
		}
		if event.Kind == EventStepFinished && event.Status == "skipped" && event.Step.Skipped {
			skippedSteps++
		}
	}
	if skippedSteps != 3 {
		t.Fatalf("skipped steps = %d, want connect, wait_connected, and one check", skippedSteps)
	}
	if got := events[5]; got.Kind != EventTargetFinished || got.Status != "skipped" {
		t.Fatalf("target finished event = %#v, want skipped", got)
	}
	if got := events[6]; got.Kind != EventRoundFinished || got.Status != "ok" || !strings.Contains(got.Message, "failed=0 skipped=1") {
		t.Fatalf("round finished event = %#v, want ok with skipped count", got)
	}
}
