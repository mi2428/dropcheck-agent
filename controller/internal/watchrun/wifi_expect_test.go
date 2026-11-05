package watchrun

import (
	"testing"

	"dropcheck/controller/internal/command"
	"dropcheck/controller/internal/control"
	"dropcheck/controller/internal/controlpb"
)

func TestWatchAPSelectorsUseTypedPayloads(t *testing.T) {
	ap := &controlpb.WifiScanResult{
		Ssid: "Lab", Bssid: "aa:bb:cc:dd:ee:ff", FrequencyMhz: 6135,
		WifiStandard: "802.11be", ChannelWidth: "CHANNEL_WIDTH_320MHZ", SecurityTypes: []string{"WPA3-SAE"},
		ApMldMacAddress: "02:00:00:00:00:01", ApMloLinkId: 0,
		AffiliatedMloLinks: []*controlpb.MloLinkInfo{{LinkId: 2}},
	}
	fields := map[string]any{"ssid": "Lab", "bssid": "AA:BB:CC:DD:EE:FF", "band": "6 GHz", "standard": "Wi-Fi 7", "security": "wpa3_sae", "channel_width": "320", "channel": 37, "ap_mld": "02:00:00:00:00:01", "mlo_link_id": 0}
	raw := &controlpb.CommandResult{Payload: &controlpb.CommandResult_WifiScan{WifiScan: &controlpb.WifiScan{Results: []*controlpb.WifiScanResult{ap}}}}
	evaluate := func(fields map[string]any, raw *controlpb.CommandResult) Finding {
		t.Helper()
		expect, err := CompileExpectations(map[string]any{"ap": fields})
		if err != nil {
			t.Fatal(err)
		}
		return expect[0].Evaluate(Result{Run: RunResult{Raw: raw}})[0]
	}
	if f := evaluate(fields, raw); !f.Passed || f.Missing {
		t.Fatalf("typed AP did not match normalized selectors: %+v", f)
	}
	fields["mlo_link_id"] = 2
	if f := evaluate(fields, raw); !f.Passed {
		t.Fatalf("affiliated link ID was lost: %+v", f)
	}
	for key, mismatch := range map[string]any{"ssid": "Other", "bssid": "00:00:00:00:00:00", "band": "5g", "standard": "ax", "security": "open", "channel_width": "80", "channel": 1, "ap_mld": "00:00:00:00:00:00", "mlo_link_id": 3} {
		original := fields[key]
		fields[key] = mismatch
		if f := evaluate(fields, raw); f.Passed || f.Missing {
			t.Errorf("AP selector %s mismatch was not an observed failure: %+v", key, f)
		}
		fields[key] = original
	}
	for _, tc := range []struct {
		fields map[string]any
		pass   bool
	}{
		{map[string]any{"count": 1}, true}, {map[string]any{"count": 0}, false},
		{map[string]any{"min_count": 1}, true}, {map[string]any{"min_count": 2}, false},
	} {
		if f := evaluate(tc.fields, raw); f.Passed != tc.pass || f.Missing {
			t.Fatalf("AP count: fields=%v finding=%+v", tc.fields, f)
		}
	}
	for _, missing := range []*controlpb.CommandResult{nil, {Payload: &controlpb.CommandResult_IpStatus{IpStatus: &controlpb.IpStatus{}}}} {
		if f := evaluate(fields, missing); !f.Missing || f.Passed {
			t.Fatalf("missing/wrong scan payload became known: %+v", f)
		}
	}
	detail := &controlpb.CommandResult{Payload: &controlpb.CommandResult_WifiScanDetail{WifiScanDetail: &controlpb.WifiScanDetail{Results: []*controlpb.WifiScanResult{ap}}}}
	if f := evaluate(fields, detail); !f.Passed {
		t.Fatalf("scan detail payload lost AP matching: %+v", f)
	}
	if f := evaluate(map[string]any{"count": 0}, &controlpb.CommandResult{Payload: &controlpb.CommandResult_WifiScan{WifiScan: &controlpb.WifiScan{}}}); !f.Passed {
		t.Fatalf("known empty scan was not zero: %+v", f)
	}
}

func TestWatchSpecialExpectationsRejectInvalidInput(t *testing.T) {
	for _, values := range []map[string]any{
		{"ap": true}, {"ap": map[string]any{"unknown": true}}, {"ap": map[string]any{"bssid": "bad"}},
		{"ap": map[string]any{"ap_mld": "bad"}}, {"ap": map[string]any{"band": "bad"}},
		{"ap": map[string]any{"channel": 0}}, {"ap": map[string]any{"channel": "bad"}},
		{"ap": map[string]any{"mlo_link_id": -1}}, {"ap": map[string]any{"mlo_link_id": 16}},
		{"ap": map[string]any{"count": "bad"}}, {"ap": map[string]any{"min_count": "bad"}},
		{"mlo_connected": false}, {"mlo_coverage": "true"}, {"mlo_metadata_complete": 1},
	} {
		if _, err := CompileExpectations(values); err == nil {
			t.Errorf("invalid expectation accepted: %v", values)
		}
	}
}

func TestWatchMLOPresenceCoverageAndMetadata(t *testing.T) {
	for _, key := range []string{"mlo_connected", "mlo_coverage", "mlo_metadata_complete"} {
		t.Run(key, func(t *testing.T) {
			expect, err := CompileExpectations(map[string]any{key: true})
			if err != nil {
				t.Fatal(err)
			}
			check := func(raw *controlpb.CommandResult, pass, missing bool) {
				t.Helper()
				f := expect[0].Evaluate(Result{Run: RunResult{Raw: raw}})[0]
				if f.Passed != pass || f.Missing != missing {
					t.Fatalf("%s pass=%v missing=%v finding=%+v", key, pass, missing, f)
				}
			}
			check(nil, false, true)
			check(runtimeFixture(command.IPStatusOperation()), false, true)
			raw := runtimeFixture(command.WifiDiagnosticsOperation())
			d := raw.GetWifiDiagnostics()
			conn := d.Status.Connection
			conn.ApMldMacAddress, conn.ApMloLinkId = "02:00:00:00:00:01", 0
			conn.AssociatedMloLinks = []*controlpb.MloLinkInfo{{LinkId: 2}}
			ap := &controlpb.WifiScanResult{WifiStandard: "Wi-Fi 7", ApMldMacAddress: "02:00:00:00:00:01", ApMloLinkId: 0, AffiliatedMloLinks: []*controlpb.MloLinkInfo{{LinkId: 2}}, ObservationFields: []*controlpb.DiagnosticField{{Key: "ap_mlo_link_id.state", Value: "available"}}}
			d.Scan.Results = []*controlpb.WifiScanResult{ap}
			check(raw, true, false)
			switch key {
			case "mlo_connected":
				conn.ApMldMacAddress, conn.AssociatedMloLinks, conn.AffiliatedMloLinks = "", nil, nil
				check(raw, false, false)
				conn.ObservationFields = nil
				check(raw, false, true)
				d.Status.Connection = nil
				check(raw, false, true)
			case "mlo_coverage":
				ap.AffiliatedMloLinks = nil
				check(raw, false, false)
				conn.ObservationFields = nil
				check(raw, false, true)
				d.Scan = nil
				check(raw, false, true)
			case "mlo_metadata_complete":
				ap.ApMldMacAddress = ""
				check(raw, false, false)
				ap.ObservationFields = nil
				check(raw, false, true)
				d.Scan.Results = nil
				check(raw, false, false)
				d.Scan = nil
				check(raw, false, true)
			}
		})
	}
}

func TestTypedFalseAndUnavailableObservationAreDistinct(t *testing.T) {
	expect, err := CompileExpectations(map[string]any{"enabled": false})
	if err != nil {
		t.Fatal(err)
	}
	raw := runtimeFixture(command.WifiStatusOperation())
	raw.GetWifiStatus().Enabled = false
	f := expect[0].Evaluate(Result{Run: RunResult{Raw: raw}})[0]
	if !f.Passed || f.Missing {
		t.Fatalf("known false became missing: %+v", f)
	}
	raw.GetWifiStatus().ObservationFields = nil
	f = expect[0].Evaluate(Result{Run: RunResult{Raw: raw}})[0]
	if f.Passed || !f.Missing {
		t.Fatalf("unavailable radio became false: %+v", f)
	}
}

func TestPlainDataCompilationRejectsInconsistentModesAndFreezesGateway(t *testing.T) {
	for _, plan := range []Plan{
		{Networks: []Network{{SSID: "Lab", ActiveProbes: true}}},
		{Networks: []Network{{SSID: "Lab", ReadOnly: true, DisconnectAfter: true}}},
		{Networks: []Network{{SSID: "Lab"}}, Checks: []Check{{Name: "both", Operation: command.IPStatusOperation(), Gateway: &GatewayPingOptions{}}}},
	} {
		if err := Validate(plan); err == nil {
			t.Fatalf("inconsistent runtime data accepted: %+v", plan)
		}
	}
	opts := &GatewayPingOptions{Family: "ipv4", Count: 3}
	plan, err := Compile(Plan{Networks: []Network{{SSID: "Lab"}}, Checks: []Check{{Name: "gateway", Gateway: opts}}}, []control.AgentInfo{{ID: "a"}})
	if err != nil {
		t.Fatal(err)
	}
	opts.Family, opts.Count = "invalid", 999
	if got := plan.targets[0].checks[0].gateway; got.Family != "ipv4" || got.Count != 3 {
		t.Fatalf("compiled gateway shares mutable input: %+v", got)
	}
}
