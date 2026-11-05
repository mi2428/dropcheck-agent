package watch

import (
	"context"
	"strings"
	"testing"

	"github.com/mi2428/dropcheck-agent/controller/internal/command"
	"github.com/mi2428/dropcheck-agent/controller/internal/control"
	"github.com/mi2428/dropcheck-agent/controller/internal/controlpb"
	"github.com/mi2428/dropcheck-agent/controller/internal/runner"
	"github.com/mi2428/dropcheck-agent/controller/internal/watchrun"
)

func TestYAMLKnownFieldsAndInvalidOperationsFailBeforeDispatch(t *testing.T) {
	for _, fragment := range []string{
		"type: unknown", "type: ping\n    family: invalid", "type: ping\n    count: 0",
		"type: path_mtu\n    min_mtu: 1500\n    max_mtu: 1200", "type: ping\n    host: bad host",
		"type: ip_status\n    expect: {addresses: {cidr: not-a-cidr}}", "type: ping\n    requiredd: true",
		"type: ping\n    expect: {not_a_metric: true}", "type: ping\n    policy: {attempts: 0}",
		"type: wifi_scan\n    expect: {ap: {channel: 0}}", "type: wifi_scan\n    expect: {ap: {bssid: bad}}",
		"type: wifi_eht\n    expect: {mlo_connected: false}", "type: wifi_eht\n    expect: {mlo_coverage: 'true'}",
	} {
		input := "version: 1\ntargets:\n  - ssid: Lab\nchecks:\n  - " + fragment + "\n"
		plan, err := Parse([]byte(input))
		if err == nil {
			_, err = watchrun.Compile(plan, []control.AgentInfo{{ID: "a"}})
		}
		if err == nil {
			t.Errorf("invalid YAML accepted: %s", fragment)
		}
	}
}

func TestYAMLObservedWiFiExecutesOnlyReadOnlyStatus(t *testing.T) {
	plan, err := Parse([]byte("targets: [{ssid: Lab, observe: true}]\nchecks: [{type: ip_status, policy: {attempts: 1}}]\n"))
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := watchrun.Compile(plan, []control.AgentInfo{{ID: "a"}})
	if err != nil {
		t.Fatal(err)
	}
	target := compiled.Preview().Targets[0]
	if target.Connect != nil || target.Wait != nil || *target.DisconnectAfter || *target.ForgetAfter {
		t.Fatalf("observed target owns the connection: %+v", target)
	}
	r := &parityRunner{}
	_, _ = watchrun.Execute(context.Background(), compiled, r, watchrun.ExecuteOptions{Rounds: 1})
	if len(r.operations) != 1 || r.operations[0].Name != "ip.status" || r.operations[0].Command.GetGetIpStatus().GetSelector().GetSsid() != "Lab" {
		t.Fatalf("observed target performed unexpected operations: %+v", r.operations)
	}
	for _, option := range []string{"passphrase: placeholder", "passphrase_env: UNUSED", "disconnect_after: true", "wait_connected: true", "mac_rotation: per_round"} {
		if _, err := Parse([]byte("targets: [{ssid: Lab, observe: true, " + option + "}]\nchecks: [{type: ip_status}]\n")); err == nil {
			t.Errorf("observed target accepted mutation option %s", option)
		}
	}
	probe, err := Parse([]byte("targets: [{ssid: Lab, observe: true}]\nchecks: [{type: ping, host: example.test}]\n"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := watchrun.Compile(probe, []control.AgentInfo{{ID: "a"}}); err == nil {
		t.Fatal("observed target accepted active probe")
	}
}

func TestYAMLObservedNetworkIdentityFailsClosedAndReachesProbe(t *testing.T) {
	plan, err := Parse([]byte("targets: [{ssid: Lab, observe: true, active_probes: true, bssid: 'aa:bb:cc:dd:ee:ff', network_id: '949'}]\nchecks: [{type: ping, host: 192.0.2.1}]\n"))
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := watchrun.Compile(plan, []control.AgentInfo{{ID: "a"}})
	if err != nil {
		t.Fatal(err)
	}
	preview := compiled.Preview().Targets[0]
	if preview.NetworkID != "949" || preview.BSSID != "aa:bb:cc:dd:ee:ff" || preview.Connect != nil {
		t.Fatalf("unexpected preview: %+v", preview)
	}
	r := &parityRunner{}
	_, _ = watchrun.Execute(context.Background(), compiled, r, watchrun.ExecuteOptions{Rounds: 1})
	if len(r.operations) != 1 || r.operations[0].Command.GetPing().GetSelector().GetNetworkId() != "949" || r.operations[0].Command.GetPing().GetSelector().GetBssid() != "aa:bb:cc:dd:ee:ff" {
		t.Fatalf("identity lost before dispatch: %+v", r.operations)
	}
	for _, input := range []string{
		"targets: [{ssid: Lab, network_id: '949'}]\nchecks: [{type: ip_status}]\n",
		"targets: [{ssid: Lab, observe: true, network_id: '0949'}]\nchecks: [{type: ip_status}]\n",
		"targets: [{ssid: Lab, observe: true, bssid: 'aa:bb:cc:dd:ee:ff'}]\nchecks: [{type: wifi_status}]\n",
	} {
		plan, err := Parse([]byte(input))
		if err == nil {
			err = watchrun.Validate(plan)
		}
		if err == nil {
			t.Fatalf("accepted unselectable network: %s", input)
		}
	}
}

func TestYAMLActiveObservationRequiresExplicitConsentAndDestination(t *testing.T) {
	for _, input := range []string{
		"targets: [{ssid: Lab, active_probes: true}]\nchecks: [{type: ping, host: 192.0.2.1}]\n",
		"targets: [{ssid: Lab, observe: true, active_probes: true}]\nchecks: [{type: ping}]\n",
		"targets: [{ssid: Lab, observe: true, active_probes: true}]\nchecks: [{type: http}]\n",
		"targets: [{ssid: Lab, observe: true, active_probes: true}]\nchecks: [{type: global_ip}]\n",
		"targets: [{ssid: Lab, observe: true, active_probes: true, disconnect_after: true}]\nchecks: [{type: ping, host: 192.0.2.1}]\n",
	} {
		if _, err := Parse([]byte(input)); err == nil {
			t.Fatalf("unsafe active observation accepted: %s", input)
		}
	}
	plan, err := Parse([]byte("targets: [{ssid: Lab, observe: true, active_probes: true}]\nchecks: [{type: ping, host: 192.0.2.1}]\n"))
	if err != nil {
		t.Fatal(err)
	}
	p, err := watchrun.Compile(plan, []control.AgentInfo{{ID: "a"}})
	if err != nil {
		t.Fatal(err)
	}
	preview := p.Preview().Targets[0]
	if preview.Connect != nil || preview.Wait != nil || *preview.DisconnectAfter || *preview.ForgetAfter || preview.Checks[0].Destination != "192.0.2.1" {
		t.Fatalf("active existing Wi-Fi preview hides mutation or destination: %+v", preview)
	}
	r := &parityRunner{}
	_, _ = watchrun.Execute(context.Background(), p, r, watchrun.ExecuteOptions{Rounds: 1})
	if len(r.operations) != 1 || r.operations[0].Name != "ping" || r.operations[0].Command.GetPing().GetSelector().GetSsid() != "Lab" {
		t.Fatalf("active observation dispatched outside selected Wi-Fi: %+v", r.operations)
	}
}

func TestLegacyYAMLExternalDefaultsAreExplicitInPreview(t *testing.T) {
	plan, err := Parse([]byte(`targets: [{ssid: Lab}]
checks:
  - {type: ping}
  - {type: dns}
  - {type: http}
  - {type: download}
`))
	if err != nil {
		t.Fatal(err)
	}
	p, err := watchrun.Compile(plan, []control.AgentInfo{{ID: "a"}})
	if err != nil {
		t.Fatal(err)
	}
	checks := p.Preview().Targets[0].Checks
	wants := []string{"1.1.1.1", "example.com", "http://connectivitycheck.gstatic.com/generate_204", "http://1.1.1.1/cdn-cgi/trace"}
	if len(checks) != len(wants) {
		t.Fatalf("unexpected external operations: %+v", checks)
	}
	for i, check := range checks {
		if check.Destination != wants[i] || check.Traffic == "" || check.Policy.Attempts != 4 {
			t.Fatalf("YAML default check[%d]=%+v", i, check)
		}
	}
}

func TestYAMLVariablesTargetChecksAndLiteralSSID(t *testing.T) {
	plan, err := Parse([]byte(`
version: 1
vars: {host: example.test}
targets:
  - name: same
    ssid: "Lab "
    vars: {host: first.example.test}
    checks:
      - name: "${ssid} local"
        type: dns
        query: "${host}"
  - name: same
    ssid: lab
checks:
  - name: "${ssid} ping"
    type: ping
    host: "${host}"
`))
	if err != nil {
		t.Fatal(err)
	}
	p, err := watchrun.Compile(plan, []control.AgentInfo{{ID: "a"}})
	if err != nil {
		t.Fatal(err)
	}
	view := p.Preview()
	if view.Targets[0].SSID != "Lab " || view.Targets[0].ID == view.Targets[1].ID || len(view.Targets[0].Checks) != 2 {
		t.Fatalf("identity/target checks=%+v", view.Targets)
	}
	for _, vars := range []string{"{a: '${b}', b: '${a}'}", "{a: '${missing}'}"} {
		if _, err := Parse([]byte("vars: " + vars + "\ntargets: [{ssid: Lab}]\nchecks: [{type: ping}]")); err == nil {
			t.Fatalf("invalid vars compiled: %s", vars)
		}
	}
}

type parityRunner struct{ operations []command.Operation }

func (r *parityRunner) Run(_ context.Context, _ control.AgentInfo, op command.Operation) (runner.Result, error) {
	r.operations = append(r.operations, op)
	result := &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_OK}
	switch c := op.Command.Command.(type) {
	case *controlpb.RunCommand_ConnectWifi:
		result.Payload = &controlpb.CommandResult_ConnectWifi{ConnectWifi: &controlpb.ConnectWifiResult{Connected: true, Ssid: c.ConnectWifi.Ssid}}
	case *controlpb.RunCommand_WaitWifiConnected:
		result.Payload = &controlpb.CommandResult_WifiAssert{WifiAssert: &controlpb.WifiAssertResult{Passed: true}}
	case *controlpb.RunCommand_Ping:
		result.Payload = &controlpb.CommandResult_Ping{Ping: &controlpb.PingResult{Received: new(uint32), Output: "3 packets transmitted, 3 received, 100% packet loss"}}
	case *controlpb.RunCommand_DisconnectWifi:
		result.Payload = &controlpb.CommandResult_WifiOperation{WifiOperation: &controlpb.WifiOperationResult{Ok: true}}
	}
	return runner.Result{Result: result}, nil
}

func TestGoYAMLTypedParityAndFamilyInventory(t *testing.T) {
	yamlPlan, err := Parse([]byte(`targets: [{ssid: Lab}]
checks:
  - type: ping
    host: example.test
    family: ipv6
    policy: {attempts: 1}
    expect: {received: 0}
`))
	if err != nil {
		t.Fatal(err)
	}
	op, err := command.PingOperation(command.PingOptions{Host: "example.test", Family: "ipv6"})
	if err != nil {
		t.Fatal(err)
	}
	expect, err := watchrun.CompileExpectations(map[string]any{"received": 0})
	if err != nil {
		t.Fatal(err)
	}
	goPlan := watchrun.Plan{Networks: []watchrun.Network{{Name: "lab", SSID: "Lab", WaitConnected: true, RequireIP: true, DisconnectAfter: true}}, Checks: []watchrun.Check{watchrun.NewCheck("ping", "", op, watchrun.Policy{}, false, expect...)}}
	for _, plan := range []watchrun.Plan{goPlan, yamlPlan} {
		p, err := watchrun.Compile(plan, []control.AgentInfo{{ID: "a"}})
		if err != nil {
			t.Fatal(err)
		}
		r := &parityRunner{}
		report, err := watchrun.Execute(context.Background(), p, r, watchrun.ExecuteOptions{Rounds: 1})
		if err != nil || !report.Passed() {
			t.Fatalf("typed parity report=%+v err=%v", report, err)
		}
		for _, op := range r.operations {
			if op.Name == "ping" {
				if op.Command.GetPing().Family != controlpb.IpFamily_IP_FAMILY_IPV6 || op.Command.GetPing().GetSelector().Ssid != "Lab" {
					t.Fatal("family/selector lost")
				}
			}
		}
	}
}

func TestYAMLCapabilityInventoryCompilesAllRetainedProbes(t *testing.T) {
	for kind := range strings.FieldsSeq("wifi_status wifi_diagnostics wifi_eht wifi_capabilities wifi_scan scan_detail ip_status ping traceroute path_mtu dns http download global_ip gateway_ping") {
		plan, err := Parse([]byte("targets: [{ssid: Lab}]\nchecks: [{type: " + kind + "}]"))
		if err == nil {
			_, err = watchrun.Compile(plan, []control.AgentInfo{{ID: "a"}})
		}
		if err != nil {
			t.Errorf("%s inventory: %v", kind, err)
		}
	}
}

func TestYAMLAPMLOExpectationsKeepRuntimeMetricIdentities(t *testing.T) {
	plan, err := Parse([]byte(`targets: [{ssid: Lab}]
checks:
  - type: wifi_scan
    expect: {ap: {ssid: Lab, standard: be, mlo_link_id: 0, count: 1}}
  - type: wifi_eht
    expect: {mlo_connected: true, mlo_coverage: true, mlo_metadata_complete: true}
`))
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := watchrun.Compile(plan, []control.AgentInfo{{ID: "a"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(compiled.Preview().Targets[0].Checks) != 2 {
		t.Fatal("YAML checks were lost")
	}
	ap := &controlpb.WifiScanResult{Ssid: "Lab", WifiStandard: "802.11be", ApMldMacAddress: "02:00:00:00:00:01", ApMloLinkId: 0, ObservationFields: []*controlpb.DiagnosticField{{Key: "ap_mlo_link_id.state", Value: "available"}}}
	connection := &controlpb.WifiConnection{ApMldMacAddress: ap.ApMldMacAddress, ApMloLinkId: 0, ObservationFields: []*controlpb.DiagnosticField{{Key: "associated_mlo_links.state", Value: "available"}}}
	scan := &controlpb.WifiScan{Results: []*controlpb.WifiScanResult{ap}}
	raw := []*controlpb.CommandResult{
		{Payload: &controlpb.CommandResult_WifiScan{WifiScan: scan}},
		{Payload: &controlpb.CommandResult_WifiDiagnostics{WifiDiagnostics: &controlpb.WifiDiagnostics{Status: &controlpb.WifiStatus{Connection: connection}, Scan: scan}}},
	}
	want := []string{"scan.ap_count", "mlo.connected.present", "mlo.relation.associated_links_covered_by_scan", "mlo.metadata.complete"}
	var metrics []string
	for i, check := range plan.Networks[0].Checks {
		for _, expect := range check.Expectations {
			findings := expect.Evaluate(watchrun.Result{Run: watchrun.RunResult{Raw: raw[i]}})
			if len(findings) != 1 || !findings[0].Passed || findings[0].Missing {
				t.Fatalf("YAML expectation failed: %+v", findings)
			}
			metrics = append(metrics, findings[0].Metric)
		}
	}
	if strings.Join(metrics, ",") != strings.Join(want, ",") {
		t.Fatalf("YAML event metric identities changed: got=%v want=%v", metrics, want)
	}
}
