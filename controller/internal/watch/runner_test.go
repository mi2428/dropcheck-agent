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

type wifiAssertFailureRunner struct{}

func (wifiAssertFailureRunner) Run(context.Context, control.AgentInfo, command.Operation) (runner.Result, error) {
	return runner.Result{Result: &controlpb.CommandResult{
		Status:  controlpb.CommandResult_STATUS_FAILED,
		Message: "wifi condition timeout",
		Payload: &controlpb.CommandResult_WifiAssert{WifiAssert: &controlpb.WifiAssertResult{
			ElapsedMs: 30000,
			Checks: []*controlpb.DiagnosticCheck{
				{Key: "connected", Expected: "true", Actual: "true", Passed: true},
				{Key: "ssid", Expected: "SHIZK RADIO", Actual: "SHIZK RADIO", Passed: true},
				{Key: "bssid", Expected: "70:a7:41:a0:9a:6f", Actual: "70:a7:41:a0:9a:6f", Passed: true},
				{Key: "band", Expected: "5ghz", Actual: "5ghz", Passed: true},
				{Key: "ip", Expected: "present", Actual: "absent"},
				{Key: "validated", Expected: "true", Actual: "false"},
			},
		}},
	}}, nil
}

type forbiddenRunner struct {
	t *testing.T
}

func (r forbiddenRunner) Run(context.Context, control.AgentInfo, command.Operation) (runner.Result, error) {
	r.t.Fatal("operation runner should not be called for an unsupported target band")
	return runner.Result{}, nil
}

type sequenceRunner struct {
	statuses []controlpb.CommandResult_Status
	messages []string
	calls    int
}

func (r *sequenceRunner) Run(context.Context, control.AgentInfo, command.Operation) (runner.Result, error) {
	index := r.calls
	r.calls++
	if index >= len(r.statuses) {
		index = len(r.statuses) - 1
	}
	message := ""
	if index >= 0 && index < len(r.messages) {
		message = r.messages[index]
	}
	return runner.Result{Result: &controlpb.CommandResult{
		Status:  r.statuses[index],
		Message: message,
	}}, nil
}

type sequenceResultRunner struct {
	results []*controlpb.CommandResult
	calls   int
}

func (r *sequenceResultRunner) Run(context.Context, control.AgentInfo, command.Operation) (runner.Result, error) {
	index := r.calls
	r.calls++
	if index >= len(r.results) {
		index = len(r.results) - 1
	}
	return runner.Result{Result: r.results[index]}, nil
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
	retryLogs := countEvents(events, EventLog, func(event Event) bool {
		return strings.Contains(event.Message, "retrying connect")
	})
	if retryLogs != operationRetryLimit {
		t.Fatalf("retry logs = %d, want %d: %#v", retryLogs, operationRetryLimit, events)
	}
	log, ok := firstEvent(events, EventLog, func(event Event) bool {
		return strings.Contains(event.Message, "REQUEST_DECLINED")
	})
	if !ok {
		t.Fatalf("events missing failure cause log: %#v", events)
	}
	if log.Kind != EventLog || log.Status != "warn" {
		t.Fatalf("failure cause event = %#v, want warn log", log)
	}
	if log.Round != 7 || log.Target.Name != "lab-u6-2g" || log.Step.Name != "connect" {
		t.Fatalf("failure cause context = %#v", log)
	}
	if !strings.Contains(log.Message, "REQUEST_DECLINED") {
		t.Fatalf("failure cause message = %q", log.Message)
	}
	finished, ok := lastEvent(events, EventStepFinished)
	if !ok || finished.Status != "failed" {
		t.Fatalf("final step event = %#v, want failed", finished)
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

func TestRunRequiredStepIncludesWifiAssertFailureDetail(t *testing.T) {
	var events []Event
	emit := func(event Event) error {
		events = append(events, event)
		return nil
	}
	ok, err := runRequiredStep(
		context.Background(),
		wifiAssertFailureRunner{},
		control.AgentInfo{ID: "agent-a"},
		9,
		Target{Name: "ub2(6G)", SSID: "SHIZK RADIO", BSSID: "70:a7:41:a0:9a:6f", Band: "5ghz"},
		StepSnapshot{Name: "wait_connected", Type: "wait_connected", Operation: "wifi.wait"},
		command.Operation{Name: "wifi.wait"},
		emit,
	)
	if err != nil {
		t.Fatalf("runRequiredStep() error = %v", err)
	}
	if ok {
		t.Fatal("runRequiredStep() ok = true, want false")
	}
	finished, ok := lastEvent(events, EventStepFinished)
	if !ok || finished.Status != "failed" {
		t.Fatalf("final step event = %#v, want failed", finished)
	}
	message := finished.Step.Message
	for _, want := range []string{
		"wifi condition timeout",
		"last_pass=band",
		"failed=ip(actual=absent expected=present),validated(actual=false expected=true)",
		"assert_elapsed_ms=30000",
	} {
		if !strings.Contains(message, want) {
			t.Fatalf("step message = %q, missing %q", message, want)
		}
	}
}

func TestRunCheckRetriesFailureAndSuppressesTransientFinding(t *testing.T) {
	var events []Event
	emit := func(event Event) error {
		events = append(events, event)
		return nil
	}
	runner := &sequenceRunner{
		statuses: []controlpb.CommandResult_Status{
			controlpb.CommandResult_STATUS_FAILED,
			controlpb.CommandResult_STATUS_OK,
		},
		messages: []string{"packet loss", "ok"},
	}
	ok, err := runCheck(
		context.Background(),
		runner,
		control.AgentInfo{ID: "agent-a"},
		10,
		Target{Name: "ap1", SSID: "Lab"},
		Check{Name: "Ping CF IPv4", Type: "ping", Host: "1.1.1.1"},
		emit,
	)
	if err != nil {
		t.Fatalf("runCheck() error = %v", err)
	}
	if !ok {
		t.Fatal("runCheck() ok = false, want true after retry")
	}
	if runner.calls != 2 {
		t.Fatalf("operation calls = %d, want 2", runner.calls)
	}
	if findings := countEvents(events, EventFinding, nil); findings != 0 {
		t.Fatalf("retry-success check should not emit findings, got %d: %#v", findings, events)
	}
	if failedSteps := countEvents(events, EventStepFinished, func(event Event) bool { return event.Status == "failed" }); failedSteps != 0 {
		t.Fatalf("transient failed attempts should not emit failed step_finished events: %#v", events)
	}
	retryLog, ok := firstEvent(events, EventLog, func(event Event) bool {
		return strings.Contains(event.Message, "retrying Ping CF IPv4")
	})
	if !ok {
		t.Fatalf("events missing retry log: %#v", events)
	}
	if retryLog.Status != "warn" || retryLog.Target.Name != "ap1" || retryLog.Step.Name != "Ping CF IPv4" {
		t.Fatalf("retry log context = %#v", retryLog)
	}
	finished, ok := lastEvent(events, EventStepFinished)
	if !ok || finished.Status != "ok" || !strings.Contains(finished.Message, "retry succeeded") {
		t.Fatalf("final step event = %#v, want retry-success ok", finished)
	}
}

func TestRunCheckRetriesExpectationFailureAndSuppressesTransientFinding(t *testing.T) {
	var events []Event
	emit := func(event Event) error {
		events = append(events, event)
		return nil
	}
	runner := &sequenceResultRunner{results: []*controlpb.CommandResult{
		{
			Status: controlpb.CommandResult_STATUS_OK,
			Payload: &controlpb.CommandResult_Ping{Ping: &controlpb.PingResult{
				Received:          4,
				PacketLossPercent: 20,
			}},
		},
		{
			Status: controlpb.CommandResult_STATUS_OK,
			Payload: &controlpb.CommandResult_Ping{Ping: &controlpb.PingResult{
				Received:          5,
				PacketLossPercent: 0,
			}},
		},
	}}
	ok, err := runCheck(
		context.Background(),
		runner,
		control.AgentInfo{ID: "agent-a"},
		11,
		Target{Name: "ap1", SSID: "Lab"},
		Check{
			Name: "Ping CF IPv4",
			Type: "ping",
			Host: "1.1.1.1",
			compiledExpect: []Matcher{
				{Metric: "received", Op: "==", Want: "5"},
				{Metric: "loss_percent", Op: "<=", Want: "0"},
			},
		},
		emit,
	)
	if err != nil {
		t.Fatalf("runCheck() error = %v", err)
	}
	if !ok {
		t.Fatal("runCheck() ok = false, want true after expectation retry")
	}
	if runner.calls != 2 {
		t.Fatalf("operation calls = %d, want 2", runner.calls)
	}
	if findings := countEvents(events, EventFinding, nil); findings != 0 {
		t.Fatalf("retry-success expectation failure should not emit findings, got %d: %#v", findings, events)
	}
	if _, ok := firstEvent(events, EventLog, func(event Event) bool {
		return strings.Contains(event.Message, "retrying Ping CF IPv4")
	}); !ok {
		t.Fatalf("events missing retry log: %#v", events)
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

func countEvents(events []Event, kind EventKind, match func(Event) bool) int {
	count := 0
	for _, event := range events {
		if event.Kind != kind {
			continue
		}
		if match != nil && !match(event) {
			continue
		}
		count++
	}
	return count
}

func firstEvent(events []Event, kind EventKind, match func(Event) bool) (Event, bool) {
	for _, event := range events {
		if event.Kind != kind {
			continue
		}
		if match != nil && !match(event) {
			continue
		}
		return event, true
	}
	return Event{}, false
}

func lastEvent(events []Event, kind EventKind) (Event, bool) {
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Kind == kind {
			return events[i], true
		}
	}
	return Event{}, false
}
