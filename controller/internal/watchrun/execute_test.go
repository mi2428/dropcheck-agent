package watchrun

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

	"github.com/mi2428/dropcheck-agent/controller/internal/command"
	"github.com/mi2428/dropcheck-agent/controller/internal/control"
	"github.com/mi2428/dropcheck-agent/controller/internal/controlpb"
	"github.com/mi2428/dropcheck-agent/controller/internal/runner"
	"google.golang.org/protobuf/proto"
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
	case *controlpb.RunCommand_GetNetworks:
		r.Payload = &controlpb.CommandResult_NetworkSnapshot{NetworkSnapshot: &controlpb.NetworkSnapshot{ObservedAtUnixMs: 1}}
	case *controlpb.RunCommand_GetNetwork:
		id := c.GetNetwork.NetworkId
		if c.GetNetwork.DefaultNetwork {
			id = "42"
		}
		ip.NetworkId = id
		r.Payload = &controlpb.CommandResult_NetworkSnapshot{NetworkSnapshot: &controlpb.NetworkSnapshot{ObservedAtUnixMs: 1, Networks: []*controlpb.NetworkDiagnostics{{NetworkId: id, Active: c.GetNetwork.DefaultNetwork, IpStatus: ip}}}}
	case *controlpb.RunCommand_GetPermissions:
		r.Payload = &controlpb.CommandResult_PermissionSnapshot{PermissionSnapshot: &controlpb.PermissionSnapshot{ObservedAtUnixMs: 1}}
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
		if op.Command.GetGetFreshWifiScan() != nil {
			for _, key := range []string{"fresh_scan_receiver_registered", "fresh_scan_start_scan", "fresh_scan_broadcast_received", "fresh_scan_results_updated", "fresh_scan_wait_completed"} {
				r.GetWifiScan().Fields = append(r.GetWifiScan().Fields, &controlpb.DiagnosticField{Key: key, Value: "true"})
			}
		}
	case *controlpb.RunCommand_GetWifiScanDetail:
		r.Payload = &controlpb.CommandResult_WifiScanDetail{WifiScanDetail: &controlpb.WifiScanDetail{Target: c.GetWifiScanDetail.Target}}
	case *controlpb.RunCommand_GetWifiDiagnostics:
		r.Payload = &controlpb.CommandResult_WifiDiagnostics{WifiDiagnostics: &controlpb.WifiDiagnostics{Status: wifi, Scan: &controlpb.WifiScan{}, Capabilities: &controlpb.WifiCapabilities{}}}
	case *controlpb.RunCommand_Ping:
		r.Payload = &controlpb.CommandResult_Ping{Ping: &controlpb.PingResult{Host: c.Ping.Host, Count: c.Ping.Count, Transmitted: new(c.Ping.Count), Received: new(c.Ping.Count)}}
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
	compiled, err := Compile(plan, []control.AgentInfo{{ID: "agent-a", Hello: &controlpb.AgentHello{Capabilities: []string{"wifi.forget.typed"}}}})
	if err != nil {
		t.Fatal(err)
	}
	return compiled
}

func operationFixture(op command.Operation, err error) Check {
	if err != nil {
		panic(err)
	}
	return Check{Name: op.Name, Operation: op}
}

func TestRuntimeRecoveryRetainsFailedAndSuccessfulAttempts(t *testing.T) {
	check := operationFixture(command.PingOperation(command.PingOptions{Host: "example.test"}))
	check.Policy = Policy{Attempts: 2, Repeat: 2}
	plan := Plan{Networks: []Network{{Name: "lab", SSID: "Lab", PSK: SecretValue("synthetic-passphrase"), WaitConnected: true, RequireIP: true, DisconnectAfter: true}}, Checks: []Check{check}}
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
		if step.Name == "ping" {
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
	for _, options := range []command.PingOptions{{Host: "bad host"}, {Host: "example.test", Family: "invalid"}, {Host: "example.test", Count: "4294967295"}} {
		op, err := command.PingOperation(options)
		if err == nil {
			_, err = Compile(Plan{Networks: []Network{{SSID: "Lab", PSK: SecretValue("synthetic-passphrase")}}, Checks: []Check{{Name: "invalid", Operation: op}}}, []control.AgentInfo{{ID: "a"}})
		}
		if err == nil {
			t.Fatalf("invalid operation compiled: %+v", options)
		}
	}
	op := operationFixture(command.PingOperation(command.PingOptions{Host: "example.test"})).Operation
	for _, check := range []Check{NewCheck("invalid policy", "", op, Policy{Delay: -1}, false), NewCheck("nil", "", command.Operation{}, Policy{}, false), {}} {
		if _, err := Compile(Plan{Networks: []Network{{Name: "lab", SSID: "Lab", PSK: SecretValue("synthetic-passphrase"), WaitConnected: true, RequireIP: true, DisconnectAfter: true}}, Checks: []Check{check}}, []control.AgentInfo{{ID: "a"}}); err == nil {
			t.Fatalf("invalid check compiled: %T", check)
		}
	}
	if _, err := Compile(Plan{Networks: []Network{{Name: "lab", SSID: "Lab", PSK: SecretEnv("DROPCHECK_SYNTHETIC_MISSING_SECRET"), WaitConnected: true, RequireIP: true, DisconnectAfter: true}}}, []control.AgentInfo{{ID: "a"}}); err == nil {
		t.Fatal("missing secret compiled")
	}
}

func TestBSSIDOnlyCannotDispatchDefaultNetworkProbes(t *testing.T) {
	plan := Plan{Networks: []Network{{Name: "bssid-only", BSSID: "02:00:00:00:00:01", WaitConnected: true, RequireIP: true, DisconnectAfter: true}}, Checks: []Check{operationFixture(command.PingOperation(command.PingOptions{Host: "example.test"})), {Name: "ip status", Operation: command.IPStatusOperation()}, GatewayPing("gateway", "", GatewayPingOptions{}, Policy{}, false)}}
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
	if _, err := Compile(Plan{Networks: []Network{{Name: "lab", SSID: "Lab", PSK: SecretValue("synthetic-passphrase"), WaitConnected: true, RequireIP: true, DisconnectAfter: true}}, Checks: []Check{NewCheck("ping", "", op, Policy{}, false)}}, []control.AgentInfo{{ID: "a"}}); err == nil {
		t.Fatal("unsigned timeout would wrap to a negative Java Int after connection")
	}
}

func TestPreviewDisclosesActualProbeDestinationsWithoutSecrets(t *testing.T) {
	secret := "synthetic-only-credential"
	checks := []Check{operationFixture(command.PingOperation(command.PingOptions{Host: "example.test"})), operationFixture(command.DNSOperation("example.test", "", "")), operationFixture(command.HTTPOperation("https://example.test/path", "", "")), operationFixture(command.DownloadOperation("https://example.test/file", "")), operationFixture(command.GlobalIPOperation("", ""))}
	p := compiledFixture(t, Plan{Networks: []Network{{Name: "lab", SSID: "Lab", PSK: SecretValue(secret), WaitConnected: true, RequireIP: true, DisconnectAfter: true}}, Checks: checks})
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
	redacted := compiledFixture(t, Plan{Networks: []Network{{Name: "lab", SSID: "Lab", PSK: SecretValue(secret), WaitConnected: true, RequireIP: true, DisconnectAfter: true}}, Checks: []Check{operationFixture(command.HTTPOperation("https://example.test/"+secret, "", ""))}}).Preview()
	if strings.Contains(fmt.Sprint(redacted), secret) || redacted.Targets[0].Checks[0].Destination != "https://example.test/<redacted>" {
		t.Fatal("preview leaked credential embedded in a destination")
	}
}

func TestCompoundPreviewMatchesAcquisitionPartsAndTimeout(t *testing.T) {
	eht, err := command.WifiEHTOperationWithOptions(command.WifiEHTOptions{Fresh: true})
	if err != nil {
		t.Fatal(err)
	}
	checks := []Check{
		NewCheck("fresh eht", "", eht, Policy{}, false),
		GatewayPing("gateway", "", GatewayPingOptions{}, Policy{}, false),
	}
	p := compiledFixture(t, Plan{Networks: []Network{{Name: "lab", SSID: "Lab", PSK: SecretValue("synthetic-passphrase"), WaitConnected: true, RequireIP: true, DisconnectAfter: true}}, Checks: checks})
	view := p.Preview().Targets[0].Checks
	if len(view) != 2 {
		t.Fatalf("preview checks=%+v", view)
	}
	fresh, err := command.WifiFreshScanOperation("all", "10000")
	if err != nil {
		t.Fatal(err)
	}
	ping, err := command.PingOperation(command.PingOptions{Host: "validation.invalid"})
	if err != nil {
		t.Fatal(err)
	}
	wants := []struct {
		parts   []string
		timeout time.Duration
		traffic string
	}{
		{[]string{"wifi.scan.fresh", "wifi.eht"}, command.TimeoutFor(fresh.Command) + command.TimeoutFor(eht.Command), "one active Wi-Fi scan"},
		{[]string{"ip.status", "ping"}, command.TimeoutFor(command.IPStatusOperation().Command) + command.TimeoutFor(ping.Command), "3 ICMP packets"},
	}
	r := agentRunnerFunc(func(_ context.Context, _ control.AgentInfo, op command.Operation) (runner.Result, error) {
		return runner.Result{Result: runtimeFixture(op)}, nil
	})
	for i, want := range wants {
		info := view[i]
		if !reflect.DeepEqual(info.Operations, want.parts) || info.OperationTimeout != want.timeout || info.Policy.Timeout < want.timeout || !strings.Contains(info.Traffic, want.traffic) {
			t.Fatalf("compound preview[%d]=%+v want parts=%v timeout=%v", i, info, want.parts, want.timeout)
		}
		var result OperationResult
		if i == 0 {
			result, err = executeOperation(context.Background(), r, control.AgentInfo{ID: "a"}, p.targets[0].checks[i].operation)
		} else {
			result, err = executeGateway(context.Background(), r, control.AgentInfo{ID: "a"}, p.targets[0].network, *p.targets[0].checks[i].gateway)
		}
		if err != nil || len(result.Parts) != len(want.parts) {
			t.Fatalf("actual compound[%d]=%+v err=%v", i, result.Parts, err)
		}
		for j, part := range result.Parts {
			if part.Name != want.parts[j] {
				t.Fatalf("actual part[%d][%d]=%s, want %s", i, j, part.Name, want.parts[j])
			}
		}
	}
}

func TestCostMatchesFakeOperationPartsAndBlocksExcessiveRounds(t *testing.T) {
	eht, err := command.WifiEHTOperationWithOptions(command.WifiEHTOptions{Fresh: true})
	if err != nil {
		t.Fatal(err)
	}
	p := compiledFixture(t, Plan{Networks: []Network{{Name: "lab", SSID: "Lab", PSK: SecretValue("synthetic-passphrase"), RequireIP: true, DisconnectAfter: true}}, Checks: []Check{
		NewCheck("fresh eht", "", eht, Policy{}, false),
		GatewayPing("gateway", "", GatewayPingOptions{}, Policy{}, false),
	}})
	cost, err := p.Cost(2, false)
	if err != nil || cost.AttemptsPerRound != 3 || cost.OperationsPerRound != 6 || cost.OperationsTotal != 12 || cost.UnboundedDownload {
		t.Fatalf("cost=%+v err=%v", cost, err)
	}
	var calls uint64
	_, err = Execute(context.Background(), p, agentRunnerFunc(func(_ context.Context, _ control.AgentInfo, op command.Operation) (runner.Result, error) {
		calls++
		return runner.Result{Result: runtimeFixture(op)}, nil
	}), ExecuteOptions{Rounds: 2})
	if err != nil || calls != cost.OperationsTotal {
		t.Fatalf("actual acquisitions=%d cost=%+v err=%v", calls, cost, err)
	}
	if _, err := p.Cost(maxRetainedAttempts, false); err == nil {
		t.Fatal("excessive finite run accepted before Start")
	}
	loop, err := p.Cost(0, true)
	if err != nil || loop.OperationsPerRound != 6 || loop.OperationsTotal != 0 {
		t.Fatalf("loop cost=%+v err=%v", loop, err)
	}
}

func TestExistingWiFiActiveProbeRequiresExplicitBoundaryWithoutMutation(t *testing.T) {
	agent := []control.AgentInfo{{ID: "agent-a"}}
	checks := []Check{operationFixture(command.PingOperation(command.PingOptions{Host: "192.0.2.1"}))}
	if _, err := Compile(Plan{Networks: []Network{{Name: "status", SSID: "Lab", ReadOnly: true, Checks: checks}}}, agent); err == nil {
		t.Fatal("status-only observation accepted active traffic")
	}
	compiled, err := Compile(Plan{Networks: []Network{{Name: "probe", SSID: "Lab", ReadOnly: true, ActiveProbes: true, Checks: checks}}}, agent)
	if err != nil {
		t.Fatal(err)
	}
	if compiled.Preview().Targets[0].Connect != nil || compiled.Preview().Targets[0].Wait != nil || *compiled.Preview().Targets[0].DisconnectAfter {
		t.Fatal("active observation acquired connection ownership")
	}
	if _, err := Compile(Plan{Networks: []Network{{Name: "probe", SSID: "Lab", ReadOnly: true, ActiveProbes: true, Checks: []Check{NewCheck("mutate", "", command.WifiDisconnectOperation(), Policy{}, true)}}}}, agent); err == nil {
		t.Fatal("active observation accepted Wi-Fi mutation")
	}
}

func TestCostIncludesRotationAndPreflightRetainedAttempts(t *testing.T) {
	p := compiledFixture(t, Plan{Networks: []Network{{Name: "lab", SSID: "Lab", PSK: SecretValue("synthetic-passphrase"), Band: "5ghz", Rotation: "per_round", WaitConnected: true, RequireIP: true, DisconnectAfter: true}}, Checks: []Check{{Name: "ip status", Operation: command.IPStatusOperation()}}})
	cost, err := p.Cost(1, false)
	if err != nil || cost.AttemptsPerRound != 6 || cost.OperationsPerRound != 7 {
		t.Fatalf("cost=%+v err=%v", cost, err)
	}
	var dispatches uint64
	_, err = Execute(context.Background(), p, agentRunnerFunc(func(_ context.Context, _ control.AgentInfo, op command.Operation) (runner.Result, error) {
		dispatches++
		return runner.Result{Result: runtimeFixture(op)}, nil
	}), ExecuteOptions{Rounds: 1})
	if err != nil || dispatches != cost.OperationsTotal {
		t.Fatalf("dispatches=%d cost=%+v err=%v", dispatches, cost, err)
	}
}

func TestRuntimeMissingTransportAndCleanupRemainVisible(t *testing.T) {
	primary, cleanup, delivery := errors.New("primary"), errors.New("cleanup"), errors.New("delivery")
	compiled := compiledFixture(t, Plan{Networks: []Network{{Name: "lab", SSID: "Lab", PSK: SecretValue("synthetic-passphrase"), WaitConnected: true, RequireIP: true, DisconnectAfter: true, ForgetAfter: true}}, Checks: []Check{operationFixture(command.PingOperation(command.PingOptions{Host: "example.test"}))}})
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
	plan := Plan{Networks: []Network{{Name: "same", SSID: "Lab", PSK: SecretValue("synthetic-passphrase"), Agent: "synthetic-a", WaitConnected: true, RequireIP: true, DisconnectAfter: true}, {Name: "same", SSID: "lab", PSK: SecretValue("synthetic-passphrase"), WaitConnected: true, RequireIP: true, DisconnectAfter: true}}, Checks: []Check{operationFixture(command.PingOperation(command.PingOptions{Host: "example.test"})), operationFixture(command.PingOperation(command.PingOptions{Host: "example.test"}))}}
	p, err := Compile(plan, agents)
	if err != nil {
		t.Fatal(err)
	}
	view := p.Preview()
	if view.Targets[0].ID == view.Targets[1].ID || view.Checks[0].ID == view.Checks[1].ID {
		t.Fatal("display names merged identities")
	}
	if len(p.targets[0].agents) != 1 || p.targets[0].agents[0].ID != "a" || len(p.targets[1].agents) != 2 {
		t.Fatal("target agent binding was lost")
	}
	for _, unavailable := range [][]control.AgentInfo{nil, {agents[1]}, {{ID: "x", Hello: &controlpb.AgentHello{AdbSerial: "synthetic-a"}}, {ID: "y", Hello: &controlpb.AgentHello{AdbSerial: "synthetic-a"}}}} {
		if _, err := Compile(plan, unavailable); err == nil {
			t.Fatal("missing or ambiguous target agent accepted")
		}
	}
}

func TestRuntimeScopedSkipReachesOnlyTheSelectedFakeOperation(t *testing.T) {
	agents := []control.AgentInfo{{ID: "a"}, {ID: "b"}}
	for i := range agents {
		agents[i].Hello = &controlpb.AgentHello{Capabilities: []string{"wifi.forget.typed"}}
	}
	p, err := Compile(Plan{Networks: []Network{{Name: "lab", SSID: "Lab", PSK: SecretValue("synthetic-passphrase"), WaitConnected: true, RequireIP: true, DisconnectAfter: true, ForgetAfter: true}}, Checks: []Check{operationFixture(command.PingOperation(command.PingOptions{Host: "example.test"}))}}, agents)
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
	raw := &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_OK, Payload: &controlpb.CommandResult_Ping{Ping: &controlpb.PingResult{Received: proto.Uint32(0), PacketLossPercent: proto.Float64(0), Output: "3 packets transmitted, 3 received, 100% packet loss"}}}
	findings, outcome, _ := evaluateAttempt(Network{}, s, OperationResult{Raw: raw}, nil, nil)
	if outcome != OutcomePass || len(findings) != 2 {
		t.Fatalf("typed zero overwritten: %s %v", outcome, findings)
	}
	raw.GetPing().Received, raw.GetPing().PacketLossPercent = nil, nil
	findings, outcome, _ = evaluateAttempt(Network{}, s, OperationResult{Raw: raw}, nil, nil)
	if outcome != MissingOutcome || len(findings) != 2 {
		t.Fatalf("unreported ping stats became known zero: %s %v", outcome, findings)
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
			p := compiledFixture(t, Plan{Networks: []Network{{Name: "one", SSID: "Lab", PSK: SecretValue("synthetic-passphrase"), Rotation: "per_round", WaitConnected: true, RequireIP: true, DisconnectAfter: true}, {Name: "two", SSID: "lab", PSK: SecretValue("synthetic-passphrase"), Rotation: "per_round", WaitConnected: true, RequireIP: true, DisconnectAfter: true}}})
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
	check := operationFixture(command.PingOperation(command.PingOptions{Host: "example.test"}))
	check.Policy, check.Required = Policy{Attempts: 2, Repeat: 2, Eventually: time.Second, StableFor: 2 * time.Second, Interval: time.Second}, true
	p := compiledFixture(t, Plan{Networks: []Network{{Name: "lab", SSID: "Lab", PSK: SecretValue("synthetic-passphrase"), RequireIP: true}}, Checks: []Check{check}})
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
	check := operationFixture(command.PingOperation(command.PingOptions{Host: "example.test"}))
	check.Policy = Policy{Attempts: 1, Eventually: time.Second, Interval: time.Second}
	p := compiledFixture(t, Plan{Networks: []Network{{Name: "lab", SSID: "Lab", PSK: SecretValue("synthetic-passphrase"), RequireIP: true, DisconnectAfter: true}}, Checks: []Check{check}})
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
	check := operationFixture(command.PingOperation(command.PingOptions{Host: "example.test"}))
	check.Policy = Policy{Timeout: 40 * time.Millisecond}
	p := compiledFixture(t, Plan{Networks: []Network{{Name: "lab", SSID: "Lab", PSK: SecretValue("synthetic-passphrase"), RequireIP: true, DisconnectAfter: true}}, Checks: []Check{check}})
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
	p := compiledFixture(t, Plan{Networks: []Network{{Name: "lab", SSID: "Lab", PSK: SecretValue("synthetic-passphrase"), RequireIP: true, DisconnectAfter: true, ForgetAfter: true}}, Checks: []Check{operationFixture(command.PingOperation(command.PingOptions{Host: "example.test"}))}})
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
	p := compiledFixture(t, Plan{Networks: []Network{{Name: "lab", SSID: "Lab", PSK: SecretValue("synthetic-passphrase"), RequireIP: true, DisconnectAfter: true}}, Checks: []Check{operationFixture(command.PingOperation(command.PingOptions{Host: "example.test"}))}})
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
	p := compiledFixture(t, Plan{Networks: []Network{{Name: "lab", SSID: "Lab", PSK: SecretValue("synthetic-only-credential"), WaitConnected: true, RequireIP: true, DisconnectAfter: true}}, Checks: []Check{operationFixture(command.PingOperation(command.PingOptions{Host: "example.test"}))}})
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
	p := compiledFixture(t, Plan{Networks: []Network{{Name: "lab", SSID: "Lab", PSK: SecretValue("synthetic-passphrase"), WaitConnected: true, RequireIP: true, DisconnectAfter: true}}, Checks: []Check{operationFixture(command.PingOperation(command.PingOptions{Host: "example.test"}))}})
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
	p := compiledFixture(t, Plan{Networks: []Network{{Name: "lab", SSID: "Lab", PSK: SecretValue("synthetic-passphrase"), WaitConnected: true, RequireIP: true, DisconnectAfter: true}}, Checks: []Check{operationFixture(command.PingOperation(command.PingOptions{Host: "example.test"}))}})
	mutator := cleanupSinkFunc(func(_ context.Context, event Event) error {
		if event.Attempt != nil && event.Attempt.Result.Raw.GetPing() != nil {
			event.Attempt.Result.Raw.GetPing().Received = proto.Uint32(999)
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
			if event.Attempt.Result.Raw.GetPing().GetReceived() == 999 || event.Attempt.Result.Parts[0].Raw.Message == "mutated part" {
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
	p := compiledFixture(t, Plan{Networks: []Network{{Name: "lab", SSID: "Lab", PSK: SecretValue("synthetic-passphrase"), WaitConnected: true, RequireIP: true, DisconnectAfter: true}}})
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
	add(command.WifiForgetSSIDOperation("Lab"), nil)
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
	add(command.NetworksOperation(), nil)
	add(command.NetworkOperation("default"))
	add(command.PermissionsOperation(), nil)
	commands := (&controlpb.RunCommand{}).ProtoReflect().Descriptor().Oneofs().ByName("command")
	seen := map[string]bool{}
	for _, op := range operations {
		field := string(op.Command.ProtoReflect().WhichOneof(commands).Name())
		if seen[field] {
			t.Fatalf("duplicate wire operation in inventory: %s", field)
		}
		seen[field] = true
	}
	for i := 0; i < commands.Fields().Len(); i++ {
		if field := commands.Fields().Get(i); !seen[string(field.Name())] {
			t.Errorf("wire operation has no acquisition/report coverage: %s", field.Name())
		}
	}
	var checks []Check
	for _, op := range operations {
		checks = append(checks, NewCheck(op.Name, "", op, Policy{}, false))
	}
	p := compiledFixture(t, Plan{Networks: []Network{{Name: "lab", SSID: "Lab", PSK: SecretValue("synthetic-passphrase"), WaitConnected: true, RequireIP: true, DisconnectAfter: true}}, Checks: checks})
	report, err := Execute(context.Background(), p, cleanupRunnerFunc(func(_ context.Context, op command.Operation) (runner.Result, error) {
		return runner.Result{Result: runtimeFixture(op)}, nil
	}), ExecuteOptions{Rounds: 1})
	if err != nil || !report.Passed() || report.Counts.Passed != uint64(len(operations)+2) {
		t.Fatalf("inventory report=%+v err=%v", report, err)
	}
	for _, step := range report.Steps {
		for _, attempt := range step.Attempts {
			if attempt.Result.Raw == nil {
				t.Fatal("raw typed result lost")
			}
		}
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
		}), control.AgentInfo{ID: "a"}, Network{Name: "lab", SSID: "Lab", PSK: SecretValue("synthetic-passphrase"), WaitConnected: true, RequireIP: true, DisconnectAfter: true}, GatewayPingOptions{Family: "ipv6"})
		if failed && (err == nil || calls != 0) {
			t.Fatal("failed IP launched gateway ping")
		}
		if !failed && (err != nil || calls != 1 || len(result.Parts) != 2) {
			t.Fatalf("gateway attempt=%+v err=%v", result, err)
		}
	}
}

func TestBoundTargetsAttributeOperationsAndCleanup(t *testing.T) {
	agents := []control.AgentInfo{{ID: "a", Hello: &controlpb.AgentHello{AdbSerial: "serial-a", Capabilities: []string{"wifi.forget.typed"}}}, {ID: "b", Hello: &controlpb.AgentHello{AdbSerial: "serial-b", Capabilities: []string{"wifi.forget.typed"}}}}
	p, err := Compile(Plan{Networks: []Network{
		{Name: "one", SSID: "Lab", PSK: SecretValue("synthetic-passphrase"), Agent: "serial-a", WaitConnected: true, RequireIP: true, DisconnectAfter: true, ForgetAfter: true, Checks: []Check{operationFixture(command.PingOperation(command.PingOptions{Host: "example.test"}))}},
		{Name: "two", SSID: "Lab", PSK: SecretValue("synthetic-passphrase"), Agent: "serial-b", WaitConnected: true, RequireIP: true, DisconnectAfter: true, ForgetAfter: true, Checks: []Check{operationFixture(command.DNSOperation("example.test", "", ""))}},
	}, Checks: []Check{{Name: "ip status", Operation: command.IPStatusOperation(), Required: true}}}, agents)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	probes := map[string]int{}
	report, err := Execute(context.Background(), p, agentRunnerFunc(func(_ context.Context, agent control.AgentInfo, op command.Operation) (runner.Result, error) {
		if op.Name == "ping" || op.Name == "dns" {
			mu.Lock()
			probes[agent.ID+"/"+op.Name]++
			mu.Unlock()
		}
		return runner.Result{Result: runtimeFixture(op)}, nil
	}), ExecuteOptions{Rounds: 1})
	if err != nil || !report.Passed() {
		t.Fatalf("bound execution=%+v error=%v", report, err)
	}
	if !reflect.DeepEqual(probes, map[string]int{"a/ping": 1, "b/dns": 1}) {
		t.Fatalf("probes crossed bound targets: %v", probes)
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
}

func TestRawAndFreeTextCredentialRedactionAtProducer(t *testing.T) {
	secret := "synthetic-only-credential"
	expect, err := CompileExpectations(map[string]any{"ssid": secret})
	if err != nil {
		t.Fatal(err)
	}
	p := compiledFixture(t, Plan{Name: secret, Networks: []Network{{Name: secret, SSID: "Lab", PSK: SecretValue(secret), WaitConnected: true, RequireIP: true, DisconnectAfter: true}}, Checks: []Check{NewCheck(secret, "", command.WifiStatusOperation(), Policy{}, false, expect...)}})
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

func TestStructuredFindingCredentialRedactionAtProducer(t *testing.T) {
	secret := "synthetic-only-credential"
	p := compiledFixture(t, Plan{Networks: []Network{{Name: "lab", SSID: "Lab", PSK: SecretValue(secret), WaitConnected: true, RequireIP: true, DisconnectAfter: true}}})
	value := map[string]any{"nested": []any{secret, &controlpb.CommandResult{Message: secret}}, "key-" + secret: []string{secret}}
	result := p.sanitizeResult(OperationResult{Findings: []Finding{{ObservedValue: value, ExpectedValue: value}}})
	encoded, err := json.Marshal(result)
	if err != nil || strings.Contains(string(encoded), secret) {
		t.Fatalf("structured finding leaked a known credential: %s, %v", encoded, err)
	}
	if value["nested"].([]any)[1].(*controlpb.CommandResult).Message != secret || value["key-"+secret].([]string)[0] != secret {
		t.Fatal("redaction changed the evaluator's original typed values")
	}
}
