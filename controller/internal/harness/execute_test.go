package harness

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
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

func (f cleanupSinkFunc) Emit(ctx context.Context, e Event) error { return f(ctx, e) }

func runtimeFixture(op command.Operation) *controlpb.CommandResult {
	r := &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_OK}
	available := func(groups ...string) []*controlpb.DiagnosticField {
		var fields []*controlpb.DiagnosticField
		for _, group := range groups {
			fields = append(fields, &controlpb.DiagnosticField{Key: group + ".state", Value: "available"})
		}
		return fields
	}
	ip := &controlpb.IpStatus{Validated: true, InterfaceName: "wlan0", Routes: []string{"0.0.0.0/0 -> 192.0.2.1 wlan0", "::/0 -> fe80::1 dev wlan0"}, ObservationFields: available("capabilities", "link_properties")}
	wifi := &controlpb.WifiStatus{Enabled: true, Connection: &controlpb.WifiConnection{Ssid: "Lab", Bssid: "02:00:00:00:00:01", ObservationFields: available("identity", "rssi", "tx_link_speed_mbps", "rx_link_speed_mbps", "associated_mlo_links", "affiliated_mlo_links", "ap_mld_mac_address", "ap_mlo_link_id")}, IpStatus: ip, ObservationFields: available("radio", "connection")}
	switch c := op.Command.Command.(type) {
	case *controlpb.RunCommand_ConnectWifi:
		r.Payload = &controlpb.CommandResult_ConnectWifi{ConnectWifi: &controlpb.ConnectWifiResult{Ssid: c.ConnectWifi.Ssid, Connected: true, IpStatus: ip}}
	case *controlpb.RunCommand_DisconnectWifi, *controlpb.RunCommand_ForgetWifi, *controlpb.RunCommand_ReconnectWifi:
		r.Payload = &controlpb.CommandResult_WifiOperation{WifiOperation: &controlpb.WifiOperationResult{Ok: true, Status: wifi}}
	case *controlpb.RunCommand_WaitWifiConnected, *controlpb.RunCommand_AssertWifi:
		r.Payload = &controlpb.CommandResult_WifiAssert{WifiAssert: &controlpb.WifiAssertResult{Passed: true, Status: wifi}}
	case *controlpb.RunCommand_GetWifiStatus:
		r.Payload = &controlpb.CommandResult_WifiStatus{WifiStatus: wifi}
	case *controlpb.RunCommand_GetIpStatus:
		r.Payload = &controlpb.CommandResult_IpStatus{IpStatus: ip}
	case *controlpb.RunCommand_GetWifiCapabilities:
		r.Payload = &controlpb.CommandResult_WifiCapabilities{WifiCapabilities: &controlpb.WifiCapabilities{SupportedBands: []string{"2.4ghz", "5ghz", "6ghz"}}}
	case *controlpb.RunCommand_GetWifiScan, *controlpb.RunCommand_GetFreshWifiScan:
		r.Payload = &controlpb.CommandResult_WifiScan{WifiScan: &controlpb.WifiScan{Results: []*controlpb.WifiScanResult{{Ssid: "Lab", Bssid: "02:00:00:00:00:01", ObservationAgeMs: new(uint64(0))}}}}
	case *controlpb.RunCommand_GetWifiScanDetail:
		r.Payload = &controlpb.CommandResult_WifiScanDetail{WifiScanDetail: &controlpb.WifiScanDetail{Target: c.GetWifiScanDetail.Target}}
	case *controlpb.RunCommand_GetWifiDiagnostics:
		r.Payload = &controlpb.CommandResult_WifiDiagnostics{WifiDiagnostics: &controlpb.WifiDiagnostics{Status: wifi, Scan: &controlpb.WifiScan{}, Capabilities: &controlpb.WifiCapabilities{}}}
	case *controlpb.RunCommand_Ping:
		r.Payload = &controlpb.CommandResult_Ping{Ping: &controlpb.PingResult{Host: c.Ping.Host, Count: c.Ping.Count, Transmitted: c.Ping.Count, Received: c.Ping.Count}}
	case *controlpb.RunCommand_Traceroute:
		r.Payload = &controlpb.CommandResult_Traceroute{Traceroute: &controlpb.TracerouteResult{Host: c.Traceroute.Host, ReachedTarget: new(true), Hops: []*controlpb.TracerouteHop{{Index: 1, Addresses: []string{"192.0.2.1"}}}}}
	case *controlpb.RunCommand_PathMtu:
		r.Payload = &controlpb.CommandResult_PathMtu{PathMtu: &controlpb.PathMtuResult{Discovered: true, PathMtuBytes: 1500}}
	case *controlpb.RunCommand_GlobalIp:
		r.Payload = &controlpb.CommandResult_GlobalIp{GlobalIp: &controlpb.GlobalIpResult{}}
	case *controlpb.RunCommand_ResolveDns:
		r.Payload = &controlpb.CommandResult_ResolveDns{ResolveDns: &controlpb.ResolveDnsResult{}}
	case *controlpb.RunCommand_HttpCheck:
		r.Payload = &controlpb.CommandResult_HttpCheck{HttpCheck: &controlpb.HttpCheckResult{Status: c.HttpCheck.ExpectedStatus, Matched: true}}
	case *controlpb.RunCommand_Wget:
		r.Payload = &controlpb.CommandResult_Wget{Wget: &controlpb.WgetResult{BytesRead: 1}}
	case *controlpb.RunCommand_MonitorWifi:
		r.Payload = &controlpb.CommandResult_WifiMonitor{WifiMonitor: &controlpb.WifiMonitorResult{}}
	case *controlpb.RunCommand_CycleWifi:
		r.Payload = &controlpb.CommandResult_WifiCycle{WifiCycle: &controlpb.WifiCycleResult{}}
	}
	return r
}

func compiledFixture(t *testing.T, plan Plan) *CompiledPlan {
	t.Helper()
	compiled, err := Compile(plan, []control.AgentInfo{{ID: "agent-a"}})
	if err != nil {
		t.Fatal(err)
	}
	return compiled
}

func TestRuntimeRecoveryRetainsFailedAndSuccessfulAttempts(t *testing.T) {
	plan := Plan{Networks: []Network{WiFi("lab").SSID("Lab").PSK("synthetic-passphrase")}, Checks: []Check{Ping("example.test").Retry(2, 0).Repeat(2)}}
	compiled := compiledFixture(t, plan)
	var ops []string
	pingCalls := 0
	report, err := Execute(context.Background(), compiled, cleanupRunnerFunc(func(_ context.Context, op command.Operation) (runner.Result, error) {
		ops = append(ops, op.Name)
		if op.Name == "ping" {
			pingCalls++
			if pingCalls == 1 {
				return runner.Result{}, errors.New("synthetic transport failure")
			}
		}
		return runner.Result{Result: runtimeFixture(op)}, nil
	}), ExecuteOptions{Rounds: 1})
	if err != nil || !report.Passed() {
		t.Fatalf("recovered report=%+v err=%v", report, err)
	}
	for _, step := range report.Steps {
		if step.Name == "ping example.test" {
			if len(step.Attempts) != 3 || step.Attempts[0].Outcome != FailOutcome || step.Attempts[1].Outcome != OutcomePass || step.Attempts[2].Repeat != 2 {
				t.Fatalf("attempts=%+v", step.Attempts)
			}
		}
	}
	if ops[len(ops)-1] != "wifi.disconnect" {
		t.Fatalf("cleanup order=%v", ops)
	}
}

func TestRuntimeInvalidPlanPerformsZeroOperations(t *testing.T) {
	for _, check := range []Check{Ping("bad host"), Ping("example.test").Family("invalid"), Ping("example.test").Count(^uint32(0)), WithPolicy(Ping("example.test"), Policy{Delay: -1}, false), NewCheck("nil", "", command.Operation{}, Policy{}, false)} {
		if _, err := Compile(Plan{Networks: []Network{WiFi("lab").SSID("Lab")}, Checks: []Check{check}}, []control.AgentInfo{{ID: "a"}}); err == nil {
			t.Fatalf("invalid check compiled: %T", check)
		}
	}
	if _, err := Compile(Plan{Networks: []Network{WiFi("lab").SSID("Lab").PSKEnv("DROPCHECK_SYNTHETIC_MISSING_SECRET")}}, []control.AgentInfo{{ID: "a"}}); err == nil {
		t.Fatal("missing secret compiled")
	}
}

func TestBSSIDOnlyCannotDispatchDefaultNetworkProbes(t *testing.T) {
	plan := Plan{Networks: []Network{WiFi("bssid-only").BSSID("02:00:00:00:00:01")}, Checks: []Check{Ping("example.test"), IPStatus(), GatewayPing("gateway", "", GatewayPingOptions{}, Policy{}, false)}}
	if err := Validate(plan); err == nil || !strings.Contains(err.Error(), "SSID") {
		t.Fatalf("BSSID-only static validation=%v", err)
	}
	if _, err := Compile(plan, []control.AgentInfo{{ID: "a"}}); err == nil {
		t.Fatal("BSSID-only plan compiled and could connect/probe the default Network")
	}
	// Compile is the only route to Execute: no runner is contacted, including connect.
}

func TestWireUnsignedValuesCannotWrapAndroidSignedInt(t *testing.T) {
	op, err := command.PingOperation(command.PingOptions{Host: "example.test"})
	if err != nil {
		t.Fatal(err)
	}
	op.Command.GetPing().TimeoutMs = 1 << 31
	if _, err := Compile(Plan{Networks: []Network{WiFi("lab").SSID("Lab")}, Checks: []Check{NewCheck("ping", "", op, Policy{}, false)}}, []control.AgentInfo{{ID: "a"}}); err == nil {
		t.Fatal("unsigned timeout would wrap to a negative Java Int after connection")
	}
}

func TestPreviewDisclosesActualProbeDestinationsWithoutSecrets(t *testing.T) {
	secret := "synthetic-only-credential"
	checks := []Check{Ping("example.test"), DNS("example.test"), HTTP("https://example.test/path"), Download("https://example.test/file"), GlobalIP()}
	p := compiledFixture(t, Plan{Networks: []Network{WiFi("lab").SSID("Lab").PSK(secret)}, Checks: checks})
	view := p.Preview()
	if len(view.Targets[0].Checks) != len(checks) {
		t.Fatalf("checks=%+v", view.Targets[0].Checks)
	}
	wants := []string{"example.test", "example.test", "https://example.test/path", "https://example.test/file", "http://ifconfig.me/ip"}
	for i, check := range view.Targets[0].Checks {
		if check.Destination != wants[i] || check.Traffic == "" || check.Policy.Attempts == 0 || check.Policy.Repeat == 0 {
			t.Fatalf("check[%d] destination/traffic/policy=%+v", i, check)
		}
	}
	if strings.Contains(fmt.Sprint(view), secret) {
		t.Fatal("preview disclosed credential")
	}
	redacted := compiledFixture(t, Plan{Networks: []Network{WiFi("lab").SSID("Lab").PSK(secret)}, Checks: []Check{HTTP("https://example.test/" + secret)}}).Preview()
	if strings.Contains(fmt.Sprint(redacted), secret) || redacted.Targets[0].Checks[0].Destination != "https://example.test/<redacted>" {
		t.Fatal("preview leaked credential embedded in a destination")
	}
}

func TestRuntimeMissingTransportAndCleanupRemainVisible(t *testing.T) {
	primary, cleanup, delivery := errors.New("primary"), errors.New("cleanup"), errors.New("delivery")
	compiled := compiledFixture(t, Plan{Networks: []Network{WiFi("lab").SSID("Lab").ForgetAfter(true)}, Checks: []Check{Ping("example.test")}})
	var ops []string
	report, err := Execute(context.Background(), compiled, cleanupRunnerFunc(func(ctx context.Context, op command.Operation) (runner.Result, error) {
		ops = append(ops, op.Name)
		if op.Name == "ping" {
			return runner.Result{}, primary
		}
		if op.Name == "wifi.disconnect" {
			if ctx.Err() != nil {
				t.Error("cleanup inherited cancellation")
			}
			return runner.Result{}, cleanup
		}
		return runner.Result{Result: runtimeFixture(op)}, nil
	}), ExecuteOptions{Rounds: 1, Sinks: []Sink{cleanupSinkFunc(func(_ context.Context, event Event) error {
		if event.Step.Type == "cleanup" {
			return delivery
		}
		return nil
	})}})
	if !errors.Is(err, primary) || !errors.Is(err, cleanup) || !errors.Is(err, delivery) || report.Passed() {
		t.Fatalf("errors lost: %v %+v", err, report)
	}
	if countString(ops, "wifi.disconnect") != 1 || countString(ops, "wifi.forget") != 1 {
		t.Fatalf("cleanup=%v", ops)
	}
}

func TestRuntimeOrdinalIdentityAndAgentSelectionFailClosed(t *testing.T) {
	agents := []control.AgentInfo{{ID: "a", Hello: &controlpb.AgentHello{AdbSerial: "synthetic-a"}}, {ID: "b", Hello: &controlpb.AgentHello{AdbSerial: "synthetic-b"}}}
	plan := Plan{Networks: []Network{WiFi("same").SSID("Lab").Agent("synthetic-a"), WiFi("same").SSID("lab")}, Checks: []Check{Ping("example.test"), Ping("example.test")}}
	p, err := Compile(plan, agents)
	if err != nil {
		t.Fatal(err)
	}
	view := p.Preview()
	if view.Targets[0].ID == view.Targets[1].ID || view.Checks[0].ID == view.Checks[1].ID {
		t.Fatal("display names merged identities")
	}
	if _, err := p.Select(Selection{TargetIDs: []string{view.Targets[0].ID}}); err == nil {
		t.Fatal("zero agents accepted")
	}
	if _, err := p.Select(Selection{AgentIDs: []string{"b"}, TargetIDs: []string{view.Targets[0].ID}}); err == nil {
		t.Fatal("explicit target reassigned to survivor")
	}
	selected, err := p.Select(Selection{AgentIDs: []string{"b"}, TargetIDs: []string{view.Targets[1].ID}})
	if err != nil || len(selected.targets[0].agents) != 1 || selected.targets[0].agents[0].ID != "b" || len(p.targets[1].agents) != 2 {
		t.Fatalf("selection mutated source: %v", err)
	}
}

func TestRuntimeScopedSkipReachesOnlyTheSelectedFakeOperation(t *testing.T) {
	agents := []control.AgentInfo{{ID: "a"}, {ID: "b"}}
	p, err := Compile(Plan{Networks: []Network{WiFi("lab").SSID("Lab").ForgetAfter(true)}, Checks: []Check{Ping("example.test")}}, agents)
	if err != nil {
		t.Fatal(err)
	}
	controls := NewControls()
	started := make(chan string, 2)
	var mu sync.Mutex
	var canceled []string
	r := agentRunnerFunc(func(ctx context.Context, agent control.AgentInfo, op command.Operation) (runner.Result, error) {
		if op.Name == "ping" {
			started <- agent.ID
			if agent.ID == "a" {
				<-ctx.Done()
				mu.Lock()
				canceled = append(canceled, agent.ID)
				mu.Unlock()
				return runner.Result{}, ctx.Err()
			}
		}
		return runner.Result{Result: runtimeFixture(op)}, nil
	})
	done := make(chan Report, 1)
	go func() {
		report, _ := Execute(context.Background(), p, r, ExecuteOptions{Rounds: 1, Controls: controls})
		done <- report
	}()
	for range 2 {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("agents did not reach concurrent fake operations")
		}
	}
	if err := controls.Skip(Scope{Kind: ScopeCheck, AgentID: "a", TargetID: "target/0", CheckID: "check/0"}); err != nil {
		t.Fatal(err)
	}
	select {
	case report := <-done:
		if len(report.Agents) != 2 {
			t.Fatal("aggregate missing agent")
		}
	case <-time.After(time.Second):
		t.Fatal("scoped skip/barrier deadlocked")
	}
	if !reflect.DeepEqual(canceled, []string{"a"}) {
		t.Fatalf("wrong scope=%v", canceled)
	}
}

type agentRunnerFunc func(context.Context, control.AgentInfo, command.Operation) (runner.Result, error)

func (f agentRunnerFunc) Run(ctx context.Context, a control.AgentInfo, op command.Operation) (runner.Result, error) {
	return f(ctx, a, op)
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

func TestRuntimeTypedZeroAndContradictoryRawAreNotFallbacks(t *testing.T) {
	expect, err := CompileExpectations(map[string]any{"received": uint64(0), "loss_percent": float64(0)})
	if err != nil {
		t.Fatal(err)
	}
	ping, _ := command.PingOperation(command.PingOptions{Host: "example.test"})
	s, err := compileStep(step{name: "ping", operation: ping, expectations: expect}, "check/0", "ping")
	if err != nil {
		t.Fatal(err)
	}
	raw := &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_OK, Payload: &controlpb.CommandResult_Ping{Ping: &controlpb.PingResult{Output: "3 packets transmitted, 3 received, 100% packet loss"}}}
	findings, outcome, _ := evaluateAttempt(Network{}, s, OperationResult{Raw: raw}, nil, nil)
	if outcome != OutcomePass || len(findings) != 2 {
		t.Fatalf("typed zero overwritten: %s %v", outcome, findings)
	}
	_, outcome, _ = evaluateAttempt(Network{}, s, OperationResult{}, nil, nil)
	if outcome != MissingOutcome {
		t.Fatal("nil became zero/pass")
	}
	raw.Payload = &controlpb.CommandResult_IpStatus{IpStatus: &controlpb.IpStatus{}}
	_, outcome, _ = evaluateAttempt(Network{}, s, OperationResult{Raw: raw}, nil, nil)
	if outcome != MissingOutcome {
		t.Fatal("wrong payload became pass")
	}
}

func TestRuntimeMACRotationCaseAndFailureGates(t *testing.T) {
	for _, phase := range []string{"before", "after", "case"} {
		t.Run(phase, func(t *testing.T) {
			p := compiledFixture(t, Plan{Networks: []Network{WiFi("one").SSID("Lab").MACRotation("per_round"), WiFi("two").SSID("lab").MACRotation("per_round")}})
			var ops []string
			forget := 0
			report, _ := Execute(context.Background(), p, cleanupRunnerFunc(func(_ context.Context, op command.Operation) (runner.Result, error) {
				ops = append(ops, op.Name)
				if op.Name == "wifi.forget" {
					forget++
					if phase == "before" && forget <= 2 || phase == "after" && forget > 2 {
						result := runtimeFixture(op)
						result.Status = controlpb.CommandResult_STATUS_FAILED
						return runner.Result{Result: result}, nil
					}
				}
				return runner.Result{Result: runtimeFixture(op)}, nil
			}), ExecuteOptions{Rounds: 1})
			if forget != 4 {
				t.Fatalf("SSID case was merged: forget=%d", forget)
			}
			if phase == "before" && countString(ops, "wifi.connect") != 0 {
				t.Fatalf("failed rotation connected: %v", ops)
			}
			if phase != "case" && report.Passed() {
				t.Fatalf("rotation failure became pass: %+v", report)
			}
		})
	}
}

func TestRuntimePolicyClockAndStableRetryRecovery(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	nowFn := func() time.Time { return now }
	sleep := func(ctx context.Context, d time.Duration) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		now = now.Add(d)
		return nil
	}
	p := compiledFixture(t, Plan{Networks: []Network{WiFi("lab").SSID("Lab").WaitConnected(false).DisconnectAfter(false)}, Checks: []Check{WithPolicy(Ping("example.test"), Policy{Attempts: 2, Repeat: 2, Eventually: time.Second, StableFor: 2 * time.Second, Interval: time.Second}, true)}})
	calls := 0
	report, err := execute(context.Background(), p, cleanupRunnerFunc(func(_ context.Context, op command.Operation) (runner.Result, error) {
		if op.Name == "ping" {
			calls++
			if calls%2 == 1 {
				return runner.Result{Result: &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_FAILED, Message: "recoverable"}}, nil
			}
		}
		return runner.Result{Result: runtimeFixture(op)}, nil
	}), ExecuteOptions{Rounds: 1}, nowFn, sleep)
	if err != nil || !report.Passed() || calls < 8 {
		t.Fatalf("policy=%+v calls=%d error=%v", report, calls, err)
	}
}

func TestRuntimeEventuallyDelayCancellationIsReportedAsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := compiledFixture(t, Plan{Networks: []Network{WiFi("lab").SSID("Lab").WaitConnected(false)}, Checks: []Check{WithPolicy(Ping("example.test"), Policy{Attempts: 1, Eventually: time.Second, Interval: time.Second}, false)}})
	report, err := execute(ctx, p, cleanupRunnerFunc(func(_ context.Context, op command.Operation) (runner.Result, error) {
		result := runtimeFixture(op)
		if op.Name == "ping" {
			result.Status = controlpb.CommandResult_STATUS_FAILED
		}
		return runner.Result{Result: result}, nil
	}), ExecuteOptions{Rounds: 1}, time.Now, func(ctx context.Context, _ time.Duration) error {
		cancel()
		return ctx.Err()
	})
	if err != nil || report.State != "stopped" || report.Steps[1].Outcome != CanceledOutcome {
		t.Fatalf("eventual poll cancellation=%+v err=%v", report, err)
	}
}

func TestPausedCheckDeadlineSkipsProbeAndStillCleansUp(t *testing.T) {
	p := compiledFixture(t, Plan{Networks: []Network{WiFi("lab").SSID("Lab").WaitConnected(false)}, Checks: []Check{WithPolicy(Ping("example.test"), Policy{Timeout: 40 * time.Millisecond}, false)}})
	controls := NewControls()
	entered, release := make(chan struct{}), make(chan struct{})
	var ops []string
	type result struct {
		report Report
		err    error
	}
	done := make(chan result, 1)
	go func() {
		report, err := Execute(context.Background(), p, cleanupRunnerFunc(func(_ context.Context, op command.Operation) (runner.Result, error) {
			ops = append(ops, op.Name)
			if op.Name == "wifi.connect" {
				close(entered)
				<-release
			}
			return runner.Result{Result: runtimeFixture(op)}, nil
		}), ExecuteOptions{Rounds: 1, Controls: controls})
		done <- result{report, err}
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("connect not started")
	}
	if err := controls.Pause(Scope{Kind: ScopeCheck, AgentID: "agent-a", TargetID: "target/0", CheckID: "check/0"}); err != nil {
		t.Fatal(err)
	}
	close(release)
	select {
	case got := <-done:
		if got.err != nil || got.report.Steps[1].Outcome != FailOutcome || !strings.Contains(got.report.Steps[1].Reason, "deadline") || countString(ops, "ping") != 0 || countString(ops, "wifi.disconnect") != 1 {
			t.Fatalf("pause deadline/cleanup: ops=%v report=%+v err=%v", ops, got.report, got.err)
		}
	case <-time.After(time.Second):
		t.Fatal("paused check did not honor policy deadline")
	}
}

func TestBlockedResultSinkCannotPreventTargetCleanup(t *testing.T) {
	p := compiledFixture(t, Plan{Networks: []Network{WiFi("lab").SSID("Lab").WaitConnected(false).ForgetAfter(true)}, Checks: []Check{Ping("example.test")}})
	var ops []string
	blocked := false
	report, err := Execute(context.Background(), p, cleanupRunnerFunc(func(_ context.Context, op command.Operation) (runner.Result, error) {
		ops = append(ops, op.Name)
		return runner.Result{Result: runtimeFixture(op)}, nil
	}), ExecuteOptions{Rounds: 1, Sinks: []Sink{cleanupSinkFunc(func(ctx context.Context, event Event) error {
		if !blocked && event.Kind == EventAttemptFinished && event.Scope.CheckID == "check/0" {
			// The first result acknowledgment stalls until the delivery bound.
			blocked = true
			<-ctx.Done()
			return ctx.Err()
		}
		return nil
	})}})
	if !errors.Is(err, context.DeadlineExceeded) || report.State != "failed" || countString(ops, "wifi.disconnect") != 1 || countString(ops, "wifi.forget") != 1 {
		t.Fatalf("blocked result hid cleanup: ops=%v report=%+v err=%v", ops, report, err)
	}
	if ops[len(ops)-2] != "wifi.disconnect" || ops[len(ops)-1] != "wifi.forget" {
		t.Fatalf("cleanup order=%v", ops)
	}
}

func TestLoopRetainsBoundedRecentRoundsAndStopsOnCancel(t *testing.T) {
	p := compiledFixture(t, Plan{Networks: []Network{WiFi("lab").SSID("Lab").WaitConnected(false)}, Checks: []Check{Ping("example.test")}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	report, err := Execute(ctx, p, cleanupRunnerFunc(func(_ context.Context, op command.Operation) (runner.Result, error) {
		return runner.Result{Result: runtimeFixture(op)}, nil
	}), ExecuteOptions{Loop: true, Sinks: []Sink{cleanupSinkFunc(func(_ context.Context, event Event) error {
		if event.Kind == EventRoundFinished && event.Round == 3 {
			cancel()
		}
		return nil
	})}})
	if err != nil || report.State != "stopped" || report.EvictedRounds != 1 || len(report.Steps) != 4 || report.Counts.Passed != 6 {
		t.Fatalf("unbounded/incorrect loop retention=%+v err=%v", report, err)
	}
	for _, step := range report.Steps {
		if step.Round < 2 || step.Round > 3 {
			t.Fatalf("old round retained: %d", step.Round)
		}
	}
}

func TestRuntimeOrderedEventsAndSecretSafePreview(t *testing.T) {
	p := compiledFixture(t, Plan{Networks: []Network{WiFi("lab").SSID("Lab").PSK("synthetic-only-credential")}, Checks: []Check{Ping("example.test")}})
	if strings.Contains(fmt.Sprint(p.Preview()), "synthetic-only-credential") {
		t.Fatal("preview leaked secret")
	}
	var seq uint64
	_, err := Execute(context.Background(), p, cleanupRunnerFunc(func(_ context.Context, op command.Operation) (runner.Result, error) {
		return runner.Result{Result: runtimeFixture(op)}, nil
	}), ExecuteOptions{Rounds: 2, Sinks: []Sink{cleanupSinkFunc(func(_ context.Context, event Event) error {
		if event.Seq <= seq {
			t.Error("event order regressed")
		}
		seq = event.Seq
		return nil
	})}})
	if err != nil || seq == 0 {
		t.Fatalf("ordered execution=%v", err)
	}
}

func TestRetainedBudgetOverflowRejectsBeforeDispatch(t *testing.T) {
	p := compiledFixture(t, Plan{Networks: []Network{WiFi("lab").SSID("Lab")}, Checks: []Check{Ping("example.test")}})
	// Even a corrupted compiled policy must not wrap the defensive Execute
	// budget. Explicit Timeout bypasses duration-derived count validation.
	p.targets[0].checks[0].policy = Policy{Attempts: ^uint32(0), Repeat: ^uint32(0), Eventually: time.Second, Interval: time.Nanosecond, Timeout: time.Second}
	called := 0
	_, err := Execute(context.Background(), p, cleanupRunnerFunc(func(context.Context, command.Operation) (runner.Result, error) { called++; return runner.Result{}, nil }), ExecuteOptions{Rounds: 1})
	if err == nil || called != 0 {
		t.Fatalf("overflow bypassed preflight: calls=%d err=%v", called, err)
	}
	if _, err := checkedSum(^uint64(0), 1); err == nil {
		t.Fatal("addition wrapped")
	}
}

func TestMutatingSinkCannotChangeNextSinkOrRetainedReport(t *testing.T) {
	p := compiledFixture(t, Plan{Networks: []Network{WiFi("lab").SSID("Lab")}, Checks: []Check{Ping("example.test")}})
	mutator := cleanupSinkFunc(func(_ context.Context, event Event) error {
		if event.Attempt != nil && event.Attempt.Result.Raw.GetPing() != nil {
			event.Attempt.Result.Raw.GetPing().Received = 999
			event.Attempt.Result.Parts[0].Raw.Message = "mutated part"
		}
		if event.Report != nil && len(event.Report.Attempts) > 0 {
			event.Report.Attempts[0].Reason = "mutated step"
		}
		if event.Finding != nil {
			event.Finding.Expected = "mutated finding"
		}
		if len(event.Agents) > 0 {
			event.Agents[0].State = "mutated agent"
		}
		return nil
	})
	seen := false
	observer := cleanupSinkFunc(func(_ context.Context, event Event) error {
		if event.Attempt != nil && event.Attempt.Result.Raw.GetPing() != nil {
			seen = true
			if event.Attempt.Result.Raw.GetPing().Received == 999 || event.Attempt.Result.Parts[0].Raw.Message == "mutated part" {
				t.Error("first sink changed second sink")
			}
		}
		if event.Report != nil && len(event.Report.Attempts) > 0 && event.Report.Attempts[0].Reason == "mutated step" {
			t.Error("step snapshot shared between sinks")
		}
		return nil
	})
	report, err := Execute(context.Background(), p, cleanupRunnerFunc(func(_ context.Context, op command.Operation) (runner.Result, error) {
		return runner.Result{Result: runtimeFixture(op)}, nil
	}), ExecuteOptions{Rounds: 1, Sinks: []Sink{mutator, observer}})
	if err != nil || !seen {
		t.Fatalf("execution=%v seen=%v", err, seen)
	}
	for _, step := range report.Steps {
		for _, attempt := range step.Attempts {
			if attempt.Result.Raw.GetPing().GetReceived() == 999 || attempt.Reason == "mutated step" {
				t.Fatal("sink changed retained report")
			}
		}
	}
}

func TestTerminalDeliveryFailureFinalizesConsistentReport(t *testing.T) {
	failure := errors.New("terminal delivery failed")
	p := compiledFixture(t, Plan{Networks: []Network{WiFi("lab").SSID("Lab")}})
	report, err := Execute(context.Background(), p, cleanupRunnerFunc(func(_ context.Context, op command.Operation) (runner.Result, error) {
		return runner.Result{Result: runtimeFixture(op)}, nil
	}), ExecuteOptions{Rounds: 1, Sinks: []Sink{cleanupSinkFunc(func(_ context.Context, event Event) error {
		if event.Kind == EventRunFinished {
			return failure
		}
		return nil
	})}})
	if !errors.Is(err, failure) || report.State != "failed" || report.Outcome != FailOutcome || report.Passed() {
		t.Fatalf("inconsistent terminal report=%+v err=%v", report, err)
	}
	if len(report.Problems) == 0 || report.Problems[len(report.Problems)-1].Kind != "delivery" {
		t.Fatal("terminal delivery problem missing")
	}
}

// This inventory runs every live wire operation through the same compiler,
// operation boundary, evaluator and report, rather than merely listing names.
func TestCapabilityParityInventory(t *testing.T) {
	var operations []command.Operation
	add := func(op command.Operation, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		operations = append(operations, op)
	}
	add(command.WifiStatusOperation(), nil)
	add(command.WifiDiagnosticsOperation(), nil)
	add(command.WifiCapabilitiesOperation(), nil)
	add(command.WifiScanOperation("all"))
	add(command.WifiFreshScanOperation("all", "1000"))
	add(command.WifiScanDetailOperation("Lab", "all"))
	add(command.WifiConnectOperation(command.WifiConnectOptions{SSID: "Lab", Passphrase: "synthetic-passphrase"}))
	add(command.WifiDisconnectOperation(), nil)
	add(command.WifiForgetOperation("Lab"), nil)
	add(command.WifiWaitConnectedOperation("Lab", command.WifiExpectationOptions{}))
	add(command.WifiAssertOperation(command.WifiExpectationOptions{SSID: "Lab"}))
	add(command.WifiMonitorOperation("1000", "100"))
	add(command.WifiReconnectOperation("1000"))
	add(command.WifiCycleOperation(command.WifiCycleOptions{WifiConnectOptions: command.WifiConnectOptions{SSID: "Lab", Passphrase: "synthetic-passphrase"}, Count: "1"}))
	add(command.IPStatusOperation(), nil)
	add(command.PingOperation(command.PingOptions{Host: "example.test", Family: "ipv6"}))
	add(command.TracerouteOperation(command.TracerouteOptions{Host: "example.test", Family: "ipv6"}))
	add(command.PathMTUOperation(command.PathMTUOptions{Host: "example.test", Family: "auto"}))
	add(command.GlobalIPOperation("all", "1000"))
	add(command.DNSOperation("example.test", "ALL", "1000"))
	add(command.HTTPOperation("https://example.test", "200", "1000"))
	add(command.DownloadOperation("https://example.test", "1000"))
	if len(operations) != 22 {
		t.Fatalf("inventory=%d", len(operations))
	}
	var checks []Check
	for _, op := range operations {
		checks = append(checks, NewCheck(op.Name, "", op, Policy{}, false, Assert("raw typed access", func(r Result) error {
			if r.Run.Raw == nil {
				return fmt.Errorf("raw result lost")
			}
			return nil
		})))
	}
	p := compiledFixture(t, Plan{Networks: []Network{WiFi("lab").SSID("Lab")}, Checks: checks})
	report, err := Execute(context.Background(), p, cleanupRunnerFunc(func(_ context.Context, op command.Operation) (runner.Result, error) {
		return runner.Result{Result: runtimeFixture(op)}, nil
	}), ExecuteOptions{Rounds: 1})
	if err != nil || !report.Passed() || report.Counts.Passed != 24 {
		t.Fatalf("inventory report=%+v err=%v", report, err)
	}
}

func TestGatewayZoneAndFailedIPSuppressPing(t *testing.T) {
	for _, failed := range []bool{false, true} {
		calls := 0
		result, err := executeGateway(context.Background(), cleanupRunnerFunc(func(_ context.Context, op command.Operation) (runner.Result, error) {
			if op.Name == "ip.status" {
				if failed {
					return runner.Result{}, errors.New("IP acquisition failed")
				}
				raw := runtimeFixture(op)
				raw.GetIpStatus().Routes = []string{"::/0 -> fe80::1 dev wlan0"}
				return runner.Result{Result: raw}, nil
			}
			calls++
			if op.Command.GetPing().Host != "fe80::1%wlan0" || op.Command.GetPing().Selector.Ssid != "Lab" || op.Command.GetPing().Family != controlpb.IpFamily_IP_FAMILY_IPV6 {
				t.Errorf("gateway identity/family/zone lost: %v", op.Command.GetPing())
			}
			return runner.Result{Result: runtimeFixture(op)}, nil
		}), control.AgentInfo{ID: "a"}, WiFi("lab").SSID("Lab"), GatewayPingOptions{Family: "ipv6"})
		if failed && (err == nil || calls != 0) {
			t.Fatal("failed IP launched gateway ping")
		}
		if !failed && (err != nil || calls != 1 || len(result.Parts) != 2) {
			t.Fatalf("gateway attempt=%+v err=%v", result, err)
		}
	}
}

func TestExactScopesDoNotRerunPassedCartesianPairsAndAttributeCleanup(t *testing.T) {
	agents := []control.AgentInfo{{ID: "a"}, {ID: "b"}}
	p, err := Compile(Plan{Networks: []Network{WiFi("one").SSID("Lab").ForgetAfter(true), WiFi("two").SSID("Lab").ForgetAfter(true)}, Checks: []Check{WithPolicy(IPStatus(), Policy{}, true), Ping("example.test"), DNS("example.test")}}, agents)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := p.Select(Selection{Scopes: []Scope{
		{Kind: ScopeCheck, AgentID: "a", TargetID: "target/0", CheckID: "check/1"},
		{Kind: ScopeCheck, AgentID: "b", TargetID: "target/1", CheckID: "check/2"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	probes := map[string]int{}
	report, err := Execute(context.Background(), selected, agentRunnerFunc(func(_ context.Context, agent control.AgentInfo, op command.Operation) (runner.Result, error) {
		if op.Name == "ping" || op.Name == "dns" {
			mu.Lock()
			probes[agent.ID+"/"+op.Name]++
			mu.Unlock()
		}
		return runner.Result{Result: runtimeFixture(op)}, nil
	}), ExecuteOptions{Rounds: 1})
	if err != nil || !report.Passed() {
		t.Fatalf("exact execution=%+v error=%v", report, err)
	}
	if !reflect.DeepEqual(probes, map[string]int{"a/ping": 1, "b/dns": 1}) {
		t.Fatalf("passed Cartesian work rerun: %v", probes)
	}
	for _, step := range report.Steps {
		for _, attempt := range step.Attempts {
			for _, part := range attempt.Result.Parts {
				if part.Scope != step.Scope {
					t.Fatalf("operation attribution missing: %+v", part)
				}
			}
		}
	}
	if len(report.Cleanup) != 4 {
		t.Fatalf("cleanup records=%d", len(report.Cleanup))
	}
	for _, part := range report.Cleanup {
		if part.Scope.Kind != ScopeCheck || part.Scope.AgentID == "a" && part.Scope.TargetID != "target/0" || part.Scope.AgentID == "b" && part.Scope.TargetID != "target/1" || part.Scope.AgentID == "" {
			t.Fatalf("cleanup attribution=%+v", part.Scope)
		}
	}
	for _, target := range selected.targets {
		if len(target.checks) != 2 || target.checks[0].id != "check/0" {
			t.Fatalf("required prefix missing: %+v", target.checks)
		}
	}
	if _, err := p.Select(Selection{Scopes: []Scope{{Kind: ScopeCheck, AgentID: "unknown", TargetID: "target/0", CheckID: "check/1"}}}); err == nil {
		t.Fatal("unknown exact scope accepted")
	}
	if _, err := p.Select(Selection{Scopes: []Scope{{Kind: ScopeTarget, AgentID: "a", TargetID: "target/0"}}, AgentIDs: []string{"a"}}); err == nil {
		t.Fatal("mixed exact/flat selection accepted")
	}
}

func TestRawAndFreeTextCredentialRedactionAtProducer(t *testing.T) {
	secret := "synthetic-only-credential"
	p := compiledFixture(t, Plan{Name: secret, Networks: []Network{WiFi(secret).SSID("Lab").PSK(secret)}, Checks: []Check{NewCheck(secret, "", command.WifiStatusOperation(), Policy{}, false, Assert(secret, func(Result) error { return fmt.Errorf("diagnostic %s", secret) }))}})
	view := p.Preview()
	if strings.Contains(fmt.Sprint(view), secret) {
		t.Fatal("preview free text leaked")
	}
	report, _ := Execute(context.Background(), p, cleanupRunnerFunc(func(_ context.Context, op command.Operation) (runner.Result, error) {
		raw := runtimeFixture(op)
		raw.Message = secret
		if raw.GetWifiStatus() != nil {
			raw.GetWifiStatus().Connection.Raw = secret
		}
		return runner.Result{Result: raw}, nil
	}), ExecuteOptions{Rounds: 1, Sinks: []Sink{cleanupSinkFunc(func(_ context.Context, event Event) error {
		data, err := json.Marshal(event)
		if err != nil {
			return err
		}
		if strings.Contains(string(data), secret) {
			t.Error("event/JSONL producer leaked credential")
		}
		return nil
	})}})
	if strings.Contains(fmt.Sprint(report), secret) {
		t.Fatal("retained report/raw message leaked")
	}
}
