package watch

import (
	"context"
	"strings"
	"testing"
	"time"

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
	opNames []string
}

func (r *sequenceResultRunner) Run(_ context.Context, _ control.AgentInfo, op command.Operation) (runner.Result, error) {
	r.opNames = append(r.opNames, op.Name)
	index := r.calls
	r.calls++
	if index >= len(r.results) {
		index = len(r.results) - 1
	}
	return runner.Result{Result: r.results[index]}, nil
}

type gatewayPingRunner struct {
	t        *testing.T
	routes   []string
	calls    []string
	pingHost string
}

func (r *gatewayPingRunner) Run(_ context.Context, _ control.AgentInfo, op command.Operation) (runner.Result, error) {
	r.calls = append(r.calls, op.Name)
	switch op.Name {
	case "ip.status":
		return runner.Result{Result: &controlpb.CommandResult{
			Status: controlpb.CommandResult_STATUS_OK,
			Payload: &controlpb.CommandResult_IpStatus{IpStatus: &controlpb.IpStatus{
				Routes: r.routes,
			}},
		}}, nil
	case "ping":
		ping := op.Command.GetPing()
		r.pingHost = ping.GetHost()
		count := ping.GetCount()
		return runner.Result{Result: &controlpb.CommandResult{
			Status: controlpb.CommandResult_STATUS_OK,
			Payload: &controlpb.CommandResult_Ping{Ping: &controlpb.PingResult{
				Host:        ping.GetHost(),
				Count:       count,
				Transmitted: count,
				Received:    count,
			}},
		}}, nil
	default:
		r.t.Fatalf("unexpected operation %q", op.Name)
		return runner.Result{}, nil
	}
}

type okRunner struct{}

func (okRunner) Run(context.Context, control.AgentInfo, command.Operation) (runner.Result, error) {
	return runner.Result{Result: &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_OK}}, nil
}

type blockingAfterRunner struct {
	blockOnCall int
	started     chan string
	calls       int
}

func (r *blockingAfterRunner) Run(ctx context.Context, _ control.AgentInfo, op command.Operation) (runner.Result, error) {
	r.calls++
	if r.calls == r.blockOnCall {
		r.started <- op.Name
		<-ctx.Done()
		return runner.Result{}, ctx.Err()
	}
	return runner.Result{Result: &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_OK}}, nil
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

func TestRunCheckGatewayPingUsesDefaultGateway(t *testing.T) {
	var events []Event
	emit := func(event Event) error {
		events = append(events, event)
		return nil
	}
	runner := &gatewayPingRunner{
		t:      t,
		routes: []string{"0.0.0.0/0 -> 192.168.23.254 wlan0"},
	}

	ok, err := runCheck(
		context.Background(),
		runner,
		control.AgentInfo{ID: "agent-a"},
		12,
		Target{Name: "ap1", SSID: "Lab"},
		Check{
			Name:   "Ping GW IPv4",
			Type:   "gateway_ping",
			Family: "ipv4",
			Count:  2,
			compiledExpect: []Matcher{
				{Metric: "received", Op: ">=", Want: "1"},
			},
		},
		emit,
	)
	if err != nil {
		t.Fatalf("runCheck() error = %v", err)
	}
	if !ok {
		t.Fatal("gateway ping should pass")
	}
	if got, want := strings.Join(runner.calls, ","), "ip.status,ping"; got != want {
		t.Fatalf("operations = %s, want %s", got, want)
	}
	if runner.pingHost != "192.168.23.254" {
		t.Fatalf("ping host = %q, want gateway", runner.pingHost)
	}
	finished, ok := lastEvent(events, EventStepFinished)
	if !ok || finished.Step.Type != "gateway_ping" || finished.Step.Operation != "gateway.ping" || finished.Status != "ok" {
		t.Fatalf("gateway ping finished event = %#v", finished)
	}
}

func TestGatewayPingHostScopesIPv6LinkLocalGateway(t *testing.T) {
	host, ok, reason := gatewayPingHostFromStatus(&controlpb.IpStatus{
		Routes: []string{"::/0 -> fe80::1 wlan0 mtu 0"},
	}, "ipv6")
	if !ok {
		t.Fatalf("gateway host not found: %s", reason)
	}
	if host != "fe80::1%wlan0" {
		t.Fatalf("gateway host = %q, want scoped link-local address", host)
	}
}

func TestGatewayPingHostScopesIPv6LinkLocalGatewayWithDevRoute(t *testing.T) {
	host, ok, reason := gatewayPingHostFromStatus(&controlpb.IpStatus{
		Routes: []string{"default via fe80::1 dev wlan0 metric 100"},
	}, "ipv6")
	if !ok {
		t.Fatalf("gateway host not found: %s", reason)
	}
	if host != "fe80::1%wlan0" {
		t.Fatalf("gateway host = %q, want scoped link-local address", host)
	}
}

func TestRunCheckGatewayPingReportsMissingGateway(t *testing.T) {
	var events []Event
	emit := func(event Event) error {
		events = append(events, event)
		return nil
	}
	runner := &gatewayPingRunner{
		t:      t,
		routes: []string{"192.168.23.0/24 -> 0.0.0.0 wlan0"},
	}

	ok, err := runCheck(
		context.Background(),
		runner,
		control.AgentInfo{ID: "agent-a"},
		13,
		Target{Name: "ap1", SSID: "Lab"},
		Check{Name: "Ping GW IPv4", Type: "gateway_ping", Family: "ipv4"},
		emit,
	)
	if err != nil {
		t.Fatalf("runCheck() error = %v", err)
	}
	if ok {
		t.Fatal("gateway ping should fail when no default gateway is present")
	}
	if got := strings.Join(runner.calls, ","); strings.Contains(got, "ping") {
		t.Fatalf("gateway ping should not run ping without a gateway: calls=%s", got)
	}
	finding, ok := firstEvent(events, EventFinding, nil)
	if !ok || finding.Finding == nil || finding.Finding.Metric != "status" || !strings.Contains(finding.Finding.Message, "default gateway not found") {
		t.Fatalf("missing gateway finding = %#v", finding)
	}
}

func TestRunTargetRequiredCheckSkipsRemainingChecks(t *testing.T) {
	disconnectAfter := false
	plan := Plan{
		Name: "lab-watch",
		Checks: []Check{
			{
				Name:     "IP Provisioning",
				Type:     "ip_status",
				Required: true,
				compiledExpect: []Matcher{
					{Metric: "ipv6_default_route", Op: "==", Want: "true"},
				},
			},
			{Name: "Ping CF IPv6", Type: "ping", Host: "2606:4700:4700::1111"},
		},
	}
	target := Target{Name: "ap1", SSID: "Lab", DisconnectAfter: &disconnectAfter}
	runner := &sequenceResultRunner{results: []*controlpb.CommandResult{
		{Status: controlpb.CommandResult_STATUS_OK},
		{Status: controlpb.CommandResult_STATUS_OK},
		{Status: controlpb.CommandResult_STATUS_OK, Payload: &controlpb.CommandResult_IpStatus{IpStatus: &controlpb.IpStatus{
			Routes: []string{"0.0.0.0/0 -> 192.168.23.254 wlan0"},
		}}},
	}}
	var events []Event
	emit := func(event Event) error {
		events = append(events, event)
		return nil
	}

	result, err := runTarget(context.Background(), plan, runner, control.AgentInfo{ID: "agent-a"}, 14, target, bandSupport{}, nil, nil, emit)
	if err != nil {
		t.Fatalf("runTarget() error = %v", err)
	}
	if result != targetFailed {
		t.Fatalf("runTarget() result = %v, want targetFailed", result)
	}
	if got := strings.Join(runner.opNames, ","); strings.Contains(got, "ping") {
		t.Fatalf("required check should skip later ping, operations=%s", got)
	}
	skipped, ok := firstEvent(events, EventStepFinished, func(event Event) bool {
		return event.Step.Name == "Ping CF IPv6"
	})
	if !ok || skipped.Status != "skipped" || !skipped.Step.Skipped || !strings.Contains(skipped.Message, "required check failed: IP Provisioning") {
		t.Fatalf("remaining check was not skipped with required-check message: %#v", skipped)
	}
	finished, ok := lastEvent(events, EventTargetFinished)
	if !ok || finished.Status != "failed" {
		t.Fatalf("target finished event = %#v, want failed", finished)
	}
}

func TestRunTargetRequiredStatusCheckWaitsUntilExpectationsPass(t *testing.T) {
	previousPoll := checkExpectationPollInterval
	checkExpectationPollInterval = time.Millisecond
	defer func() { checkExpectationPollInterval = previousPoll }()

	disconnectAfter := false
	plan := Plan{
		Name: "lab-watch",
		Checks: []Check{
			{
				Name:     "IP Provisioning",
				Type:     "ip_status",
				Required: true,
				Timeout:  Duration{Duration: 50 * time.Millisecond},
				compiledExpect: []Matcher{
					{Metric: "ipv6_default_route", Op: "==", Want: "true"},
				},
			},
			{Name: "Ping CF IPv6", Type: "ping", Host: "2606:4700:4700::1111"},
		},
	}
	target := Target{Name: "ap1", SSID: "Lab", DisconnectAfter: &disconnectAfter}
	runner := &sequenceResultRunner{results: []*controlpb.CommandResult{
		{Status: controlpb.CommandResult_STATUS_OK},
		{Status: controlpb.CommandResult_STATUS_OK},
		{Status: controlpb.CommandResult_STATUS_OK, Payload: &controlpb.CommandResult_IpStatus{IpStatus: &controlpb.IpStatus{
			Routes: []string{"0.0.0.0/0 -> 192.168.23.254 wlan0"},
		}}},
		{Status: controlpb.CommandResult_STATUS_OK, Payload: &controlpb.CommandResult_IpStatus{IpStatus: &controlpb.IpStatus{
			Routes: []string{"0.0.0.0/0 -> 192.168.23.254 wlan0", "::/0 -> fe80::1 wlan0"},
		}}},
		{Status: controlpb.CommandResult_STATUS_OK},
	}}
	var events []Event
	emit := func(event Event) error {
		events = append(events, event)
		return nil
	}

	result, err := runTarget(context.Background(), plan, runner, control.AgentInfo{ID: "agent-a"}, 15, target, bandSupport{}, nil, nil, emit)
	if err != nil {
		t.Fatalf("runTarget() error = %v", err)
	}
	if result != targetPassed {
		t.Fatalf("runTarget() result = %v, want targetPassed", result)
	}
	if got := countString(runner.opNames, "ip.status"); got != 2 {
		t.Fatalf("ip.status calls = %d, want 2; operations=%v", got, runner.opNames)
	}
	if got := strings.Join(runner.opNames, ","); !strings.Contains(got, "ping") {
		t.Fatalf("required check should allow later ping after expectations pass, operations=%s", got)
	}
	finished, ok := firstEvent(events, EventStepFinished, func(event Event) bool {
		return event.Step.Name == "Ping CF IPv6"
	})
	if !ok || finished.Status != "ok" {
		t.Fatalf("later check was not run after provisioning passed: %#v", finished)
	}
}

func TestRunTargetPerTargetMacRotationForgetsBeforeConnectAndAfterCleanup(t *testing.T) {
	disconnectAfter := false
	forgetAfter := false
	plan := Plan{Name: "lab-watch"}
	target := Target{
		Name:             "ap1",
		SSID:             "Lab",
		MacRotation:      macRotationPerTarget,
		MacRandomization: "non-persistent",
		DisconnectAfter:  &disconnectAfter,
		ForgetAfter:      &forgetAfter,
	}
	runner := &sequenceResultRunner{results: []*controlpb.CommandResult{
		{Status: controlpb.CommandResult_STATUS_FAILED, Message: "wifi network not found"},
		{Status: controlpb.CommandResult_STATUS_OK},
		{Status: controlpb.CommandResult_STATUS_OK},
		{Status: controlpb.CommandResult_STATUS_OK},
		{Status: controlpb.CommandResult_STATUS_OK},
	}}
	var events []Event
	emit := func(event Event) error {
		events = append(events, event)
		return nil
	}

	result, err := runTarget(context.Background(), plan, runner, control.AgentInfo{ID: "agent-a"}, 16, target, bandSupport{}, nil, nil, emit)
	if err != nil {
		t.Fatalf("runTarget() error = %v", err)
	}
	if result != targetPassed {
		t.Fatalf("runTarget() result = %v, want targetPassed", result)
	}
	got := strings.Join(runner.opNames, ",")
	want := "wifi.forget,wifi.connect,wifi.wait,wifi.disconnect,wifi.forget"
	if got != want {
		t.Fatalf("operations = %s, want %s", got, want)
	}
	if logs := countEvents(events, EventLog, func(event Event) bool {
		return strings.Contains(event.Message, "mac_rotation=per_target") && strings.Contains(event.Message, "result=not_found")
	}); logs != 1 {
		t.Fatalf("mac rotation not-found log count = %d, want 1: %#v", logs, events)
	}
}

func TestRunRoundPerRoundMacRotationForgetsOnceBeforeAndAfterRound(t *testing.T) {
	disconnectAfter := false
	forgetAfter := false
	plan := Plan{
		Name: "lab-watch",
		Targets: []Target{
			{Name: "ap1", SSID: "Lab", MacRotation: macRotationPerRound, MacRandomization: "non-persistent", DisconnectAfter: &disconnectAfter, ForgetAfter: &forgetAfter},
			{Name: "ap2", SSID: "Lab", MacRotation: macRotationPerRound, MacRandomization: "non-persistent", DisconnectAfter: &disconnectAfter, ForgetAfter: &forgetAfter},
		},
	}
	runner := &sequenceResultRunner{results: []*controlpb.CommandResult{
		{Status: controlpb.CommandResult_STATUS_FAILED, Message: "wifi network not found"},
		{Status: controlpb.CommandResult_STATUS_OK},
		{Status: controlpb.CommandResult_STATUS_OK},
		{Status: controlpb.CommandResult_STATUS_OK},
		{Status: controlpb.CommandResult_STATUS_OK},
		{Status: controlpb.CommandResult_STATUS_OK},
		{Status: controlpb.CommandResult_STATUS_OK},
		{Status: controlpb.CommandResult_STATUS_OK},
	}}
	var events []Event
	emit := func(event Event) error {
		events = append(events, event)
		return nil
	}

	failed, err := runRound(context.Background(), plan, runner, control.AgentInfo{ID: "agent-a"}, 17, bandSupport{}, nil, nil, nil, emit)
	if err != nil {
		t.Fatalf("runRound() error = %v", err)
	}
	if failed != 0 {
		t.Fatalf("runRound() failed targets = %d, want 0", failed)
	}
	if got := countString(runner.opNames, "wifi.forget"); got != 2 {
		t.Fatalf("wifi.forget calls = %d, want 2; operations=%v", got, runner.opNames)
	}
	if got := countString(runner.opNames, "wifi.disconnect"); got != 2 {
		t.Fatalf("wifi.disconnect calls = %d, want 2; operations=%v", got, runner.opNames)
	}
	for _, event := range events {
		if event.Kind == EventStepFinished && event.Step.Name == "forget" {
			t.Fatalf("per-round rotation should not forget after each target: %#v", event)
		}
	}
}

func TestRunTargetOperatorSkipCancelsCurrentCheckAndLeavesCheckPending(t *testing.T) {
	disconnectAfter := false
	plan := Plan{
		Name: "lab-watch",
		Checks: []Check{
			{Name: "ping cloudflare", Type: "ping", Host: "1.1.1.1"},
			{Name: "dns a", Type: "dns", Query: "example.com"},
		},
	}
	target := Target{Name: "ap1", SSID: "Lab", DisconnectAfter: &disconnectAfter}
	skip := NewSkipController()
	opRunner := &blockingAfterRunner{blockOnCall: 3, started: make(chan string, 1)}
	var events []Event
	emit := func(event Event) error {
		events = append(events, event)
		return nil
	}

	done := make(chan struct {
		result targetResult
		err    error
	}, 1)
	go func() {
		result, err := runTarget(context.Background(), plan, opRunner, control.AgentInfo{ID: "agent-a"}, 12, target, bandSupport{}, nil, skip, emit)
		done <- struct {
			result targetResult
			err    error
		}{result: result, err: err}
	}()

	select {
	case op := <-opRunner.started:
		if op != "ping" {
			t.Fatalf("blocked operation = %q, want ping", op)
		}
	case <-time.After(time.Second):
		t.Fatal("runner did not reach blocking check")
	}
	skip.Skip()

	var got struct {
		result targetResult
		err    error
	}
	select {
	case got = <-done:
	case <-time.After(time.Second):
		t.Fatal("runTarget did not finish after skip")
	}
	if got.err != nil {
		t.Fatalf("runTarget() error = %v", got.err)
	}
	if got.result != targetSkipped {
		t.Fatalf("runTarget() result = %v, want targetSkipped", got.result)
	}
	if opRunner.calls != 3 {
		t.Fatalf("operation calls = %d, want connect, wait, and current check only", opRunner.calls)
	}
	if findings := countEvents(events, EventFinding, nil); findings != 0 {
		t.Fatalf("operator skip should not emit findings, got %d: %#v", findings, events)
	}
	skipped, ok := firstEvent(events, EventStepFinished, func(event Event) bool {
		return event.Step.Name == "ping cloudflare"
	})
	if !ok {
		t.Fatalf("events missing skipped check finish: %#v", events)
	}
	if skipped.Status != "skipped" || skipped.Step.Status != "pending" || !skipped.Step.Skipped {
		t.Fatalf("skipped step event = %#v, want event skipped with pending step state", skipped)
	}
	if got, ok := lastEvent(events, EventTargetFinished); !ok || got.Status != "skipped" {
		t.Fatalf("target finished event = %#v, want skipped", got)
	}
	if _, ok := firstEvent(events, EventStepStarted, func(event Event) bool {
		return event.Step.Name == "dns a"
	}); ok {
		t.Fatalf("operator skip should move to the next target, not run the next check: %#v", events)
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
	failed, err := runRound(context.Background(), plan, forbiddenRunner{t: t}, control.AgentInfo{ID: "agent-a"}, 9, support, nil, nil, nil, emit)
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

func TestRunRoundBarrierDelaysRoundFinishedUntilAllAgentsArrive(t *testing.T) {
	disconnectAfter := false
	plan := Plan{
		Name: "lab-watch",
		Targets: []Target{{
			Name:            "lab-5g",
			SSID:            "Lab",
			DisconnectAfter: &disconnectAfter,
		}},
	}
	barrier := NewRoundBarrier(2)
	eventsA := make(chan Event, 16)
	doneA := make(chan error, 1)
	go func() {
		_, err := runRound(context.Background(), plan, okRunner{}, control.AgentInfo{ID: "agent-a"}, 1, bandSupport{}, nil, nil, barrier, func(event Event) error {
			eventsA <- event
			return nil
		})
		doneA <- err
	}()

	for {
		select {
		case event := <-eventsA:
			if event.Kind == EventRoundFinished {
				t.Fatalf("agent-a emitted round_finished before agent-b reached the barrier: %#v", event)
			}
			if event.Kind == EventTargetFinished {
				goto targetFinished
			}
		case <-time.After(time.Second):
			t.Fatal("agent-a did not reach target_finished")
		}
	}

targetFinished:
	select {
	case event := <-eventsA:
		t.Fatalf("agent-a emitted event after target finish before agent-b arrived: %#v", event)
	case err := <-doneA:
		t.Fatalf("agent-a runRound returned before agent-b arrived: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	eventsB := make(chan Event, 16)
	doneB := make(chan error, 1)
	go func() {
		_, err := runRound(context.Background(), plan, okRunner{}, control.AgentInfo{ID: "agent-b"}, 1, bandSupport{}, nil, nil, barrier, func(event Event) error {
			eventsB <- event
			return nil
		})
		doneB <- err
	}()

	select {
	case err := <-doneA:
		if err != nil {
			t.Fatalf("agent-a runRound error after barrier release: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("agent-a did not finish after agent-b reached the barrier")
	}
	select {
	case err := <-doneB:
		if err != nil {
			t.Fatalf("agent-b runRound error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("agent-b did not finish")
	}
	if !eventChannelContains(eventsA, EventRoundFinished) {
		t.Fatal("agent-a did not emit round_finished after barrier release")
	}
	if !eventChannelContains(eventsB, EventRoundFinished) {
		t.Fatal("agent-b did not emit round_finished")
	}
}

func eventChannelContains(events <-chan Event, kind EventKind) bool {
	for {
		select {
		case event := <-events:
			if event.Kind == kind {
				return true
			}
		default:
			return false
		}
	}
}

func countString(values []string, want string) int {
	count := 0
	for _, value := range values {
		if value == want {
			count++
		}
	}
	return count
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
