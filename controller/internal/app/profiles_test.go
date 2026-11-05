package app

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"dropcheck/controller/internal/command"
	"dropcheck/controller/internal/control"
	"dropcheck/controller/internal/controlpb"
	"dropcheck/controller/internal/runner"
	"dropcheck/controller/internal/watchrun"
)

type linkRunner struct {
	operations []string
	change     func(command.Operation, int) *controlpb.CommandResult
}

func (r *linkRunner) Run(_ context.Context, _ control.AgentInfo, op command.Operation) (runner.Result, error) {
	r.operations = append(r.operations, op.Name)
	return runner.Result{Result: r.change(op, len(r.operations))}, nil
}

func linkAvailable(groups ...string) []*controlpb.DiagnosticField {
	var fields []*controlpb.DiagnosticField
	for _, group := range groups {
		fields = append(fields, &controlpb.DiagnosticField{Key: group + ".state", Value: "available"})
	}
	return fields
}

func linkResult(ssid, network, family string) *controlpb.CommandResult {
	ip := &controlpb.IpStatus{NetworkId: network, InterfaceName: "wlan0", Transports: []string{"wifi"}, ObservationFields: linkAvailable("capabilities", "link_properties"), Wifi: &controlpb.WifiConnection{Ssid: ssid, ObservationFields: linkAvailable("identity")}, Addresses: []string{"192.0.2.10/24", "2001:db8::10/64"}, Routes: []string{"0.0.0.0/0 -> 192.0.2.1 wlan0", "::/0 -> fe80::1 dev wlan0"}, DnsServers: []string{"192.0.2.53", "2001:db8::53"}}
	if family == "no-dns" {
		ip.DnsServers = nil
	}
	result := &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_OK}
	result.Payload = &controlpb.CommandResult_IpStatus{IpStatus: ip}
	return result
}

func TestConfiguredLabUsesExistingNetworkAndStopsAfterFailedPrerequisite(t *testing.T) {
	config := command.LabConfig{Gateway: "192.0.2.1", DNS: "lab.example.test", Resolver: "selected", HTTP: "https://lab.example.test/health", ExpectedStatus: 204, TimeoutMs: 5000}
	plan, err := labPlan("Lab", "ipv4", config)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := watchrun.Compile(plan, []control.AgentInfo{{ID: "agent-a"}})
	if err != nil {
		t.Fatal(err)
	}
	preview := compiled.Preview()
	if preview.Targets[0].Connect != nil || preview.Targets[0].Wait != nil || *preview.Targets[0].DisconnectAfter || *preview.Targets[0].ForgetAfter || len(preview.Targets[0].Checks) != 5 {
		t.Fatalf("lab owns Wi-Fi or lacks checks: %+v", preview.Targets[0])
	}
	if checks := preview.Targets[0].Checks; !strings.Contains(checks[1].Traffic, "family=ipv4") || !strings.Contains(checks[2].Traffic, "selected Network configured resolver") || !strings.Contains(checks[2].Traffic, "DNS_RECORD_TYPE_A") || !strings.Contains(checks[3].Traffic, "expected status=204") || checks[3].Destination != config.HTTP {
		t.Fatalf("configured family/resolver/status/destination absent from preview: %+v", checks)
	}
	for _, tc := range []struct {
		name    string
		failAt  int
		want    []string
		outcome watchrun.Outcome
	}{
		{"pass", 0, []string{"ip.status", "ping", "dns", "http", "ip.status"}, watchrun.OutcomePass},
		{"lost-before-probe", 1, []string{"ip.status"}, watchrun.MissingOutcome},
		{"dns-failure", 3, []string{"ip.status", "ping", "dns"}, watchrun.FailOutcome},
		{"http-wrong-status", 4, []string{"ip.status", "ping", "dns", "http"}, watchrun.FailOutcome},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &linkRunner{change: func(op command.Operation, n int) *controlpb.CommandResult {
				cmd := op.Command
				if cmd.GetPing() != nil && (cmd.GetPing().GetSelector().GetSsid() != "Lab" || cmd.GetPing().GetHost() != config.Gateway || cmd.GetPing().GetCount() != 3) || cmd.GetResolveDns() != nil && (cmd.GetResolveDns().GetSelector().GetSsid() != "Lab" || cmd.GetResolveDns().GetName() != config.DNS || len(cmd.GetResolveDns().GetQtypes()) != 1) || cmd.GetHttpCheck() != nil && (cmd.GetHttpCheck().GetSelector().GetSsid() != "Lab" || cmd.GetHttpCheck().GetUrl() != config.HTTP || cmd.GetHttpCheck().GetExpectedStatus() != 204) {
					t.Error("lab command does not match configured selected-network contract")
				}
				switch op.Name {
				case "ip.status":
					result := linkResult("Lab", "101", "ipv4")
					if n == tc.failAt {
						result.GetIpStatus().NetworkId = ""
					}
					return result
				case "ping":
					v := uint32(1)
					return &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_OK, Payload: &controlpb.CommandResult_Ping{Ping: &controlpb.PingResult{Received: &v}}}
				case "dns":
					answers := []*controlpb.DnsAnswer{{Type: controlpb.DnsRecordType_DNS_RECORD_TYPE_A, Address: "192.0.2.5"}}
					if n == tc.failAt {
						answers = nil
					}
					return &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_OK, Payload: &controlpb.CommandResult_ResolveDns{ResolveDns: &controlpb.ResolveDnsResult{Answers: answers}}}
				case "http":
					return &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_OK, Payload: &controlpb.CommandResult_HttpCheck{HttpCheck: &controlpb.HttpCheckResult{Status: 204, Matched: n != tc.failAt}}}
				}
				t.Fatalf("unexpected %s", op.Name)
				return nil
			}}
			report, err := watchrun.Execute(context.Background(), compiled, r, watchrun.ExecuteOptions{Rounds: 1})
			if err != nil || report.Outcome != tc.outcome || !slices.Equal(r.operations, tc.want) {
				t.Fatalf("ops=%v outcome=%s err=%v want=%v/%s", r.operations, report.Outcome, err, tc.want, tc.outcome)
			}
		})
	}
}

func TestLinkExistingWiFiReadOnlyAndRevalidation(t *testing.T) {
	for _, tc := range []struct {
		name, ssid, family, change, want string
		ops                              int
	}{
		{"ipv4", "Lab", "ipv4", "", "pass", 2},
		{"ipv6", "Lab", "ipv6", "", "pass", 2},
		{"all", "Lab", "all", "", "pass", 2},
		{"all-missing-dns", "Lab", "all", "dns", "fail", 1},
		{"wrong-case", "lab", "ipv4", "", "missing", 1},
		{"unknown", "Lab", "ipv4", "unknown", "missing", 1},
		{"lost-network", "Lab", "ipv4", "lost", "missing", 1},
		{"unknown-after-first", "Lab", "ipv4", "unknown-ip", "missing", 2},
		{"vpn", "Lab", "ipv4", "vpn", "missing", 1},
		{"changed-after-first", "Lab", "ipv4", "post", "missing", 2},
		{"changed-interface", "Lab", "ipv4", "iface", "missing", 2},
		{"lost-after-first", "Lab", "ipv4", "post-lost", "missing", 2},
		{"missing-dns", "Lab", "ipv4", "dns", "fail", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := watchrun.Compile(linkPlan(tc.ssid, tc.family), []control.AgentInfo{{ID: "agent-a"}})
			if err != nil {
				t.Fatal(err)
			}
			if plan.Preview().Targets[0].Connect != nil || plan.Preview().Targets[0].Wait != nil {
				t.Fatal("read-only preview advertises connection/wait")
			}
			r := &linkRunner{change: func(op command.Operation, number int) *controlpb.CommandResult {
				if op.Name == "ip.status" && op.Command.GetGetIpStatus().GetSelector().GetSsid() != tc.ssid {
					t.Error("IP observation not bound to literal SSID")
				}
				ssid, network, family := "Lab", "101", tc.family
				if tc.change == "unknown" {
					ssid = "<unknown ssid>"
				}
				if tc.change == "lost" {
					network = ""
				}
				if tc.change == "post" && number == 2 {
					network = "102"
				}
				if tc.change == "dns" {
					family = "no-dns"
				}
				result := linkResult(ssid, network, family)
				if tc.change == "vpn" {
					result.GetIpStatus().Transports = []string{"wifi", "vpn"}
				}
				if tc.change == "unknown-ip" && number == 2 {
					result.GetIpStatus().Wifi = nil
				}
				if tc.change == "iface" && number == 2 {
					result.GetIpStatus().InterfaceName = "wlan1"
				}
				if tc.change == "post-lost" && number == 2 {
					result.GetIpStatus().NetworkId = ""
				}
				return result
			}}
			report, _ := watchrun.Execute(context.Background(), plan, r, watchrun.ExecuteOptions{Rounds: 1})
			if len(r.operations) != tc.ops || string(report.Outcome) != tc.want {
				t.Fatalf("ops=%v outcome=%s want=%s", r.operations, report.Outcome, tc.want)
			}
			for _, op := range r.operations {
				if op != "ip.status" {
					t.Fatalf("mutation/probe: %s", op)
				}
			}
			if tc.want == "pass" {
				if !report.Passed() || report.Counts.Passed != 2 || len(report.Steps[0].Attempts[0].Findings) < 4 {
					t.Fatalf("typed link report=%+v", report)
				}
				if tc.family == "all" {
					view, err := renderProfileReport(&shellState{lastProfile: "link", lastSSID: tc.ssid, lastReport: &report}, false)
					if err != nil || !strings.Contains(view, "Android validated: false (observation only)") {
						t.Fatalf("all-family validation observation lost: %s (%v)", view, err)
					}
				}
			}
		})
	}
}

func TestLinkInvalidPlanAndHistoricalPreflight(t *testing.T) {
	if err := watchrun.Validate(watchrun.Plan{Networks: []watchrun.Network{{Name: "lab", ReadOnly: true, SSID: "Lab", PSK: watchrun.SecretValue("not-a-valid-secret")}}, Checks: []watchrun.Check{{Name: "wifi status", Operation: command.WifiStatusOperation()}}}); err == nil {
		t.Fatal("read-only plan accepted a connection credential")
	}
	if err := watchrun.Validate(watchrun.Plan{Networks: []watchrun.Network{{Name: "lab", ReadOnly: true, SSID: "Lab", BSSID: "02:00:00:00:00:11"}}, Checks: []watchrun.Check{{Name: "wifi status", Operation: command.WifiStatusOperation()}}}); err == nil {
		t.Fatal("read-only plan promised strict BSSID pinning")
	}
	probe, err := command.PingOperation(command.PingOptions{Host: "example.test"})
	if err != nil {
		t.Fatal(err)
	}
	if err := watchrun.Validate(watchrun.Plan{Networks: []watchrun.Network{{Name: "lab", ReadOnly: true, SSID: "Lab"}}, Checks: []watchrun.Check{{Name: "ping", Operation: probe}}}); err == nil {
		t.Fatal("read-only plan accepted active probe")
	}
	s := &shellState{server: control.NewServer("synthetic", nil), selected: "expired-agent"}
	compiled, err := watchrun.Compile(linkPlan("Lab", "ipv4"), []control.AgentInfo{{ID: "old-agent"}})
	if err != nil {
		t.Fatal(err)
	}
	old, err := watchrun.Execute(context.Background(), compiled, &linkRunner{change: func(op command.Operation, _ int) *controlpb.CommandResult {
		return linkResult("Lab", "101", "ipv4")
	}}, watchrun.ExecuteOptions{Rounds: 1})
	if err != nil || !old.Passed() {
		t.Fatalf("old fixture is not PASS: %s %v", old.Outcome, err)
	}
	s.lastReport, s.lastProfile, s.lastSSID = &old, "link", "Lab"
	if view, err := renderProfileReport(s, false); err != nil || !strings.Contains(view, "Android validated: false (observation only)") {
		t.Fatalf("false validation must not fail link or imply internet: %q %v", view, err)
	}
	before, _ := json.Marshal(old)
	for _, tc := range []struct {
		line string
		want watchrun.Outcome
	}{
		{"test link ssid credential-like-ssid family ipv4 bssid 02:00:00:00:00:11", watchrun.SkipOutcome},
		{"test link ssid Lab family ipv4", watchrun.MissingOutcome},
		{`test link ssid "" family invalid`, watchrun.MissingOutcome},
	} {
		previous := s.lastReport
		out, _, err := captureShellLine(t, context.Background(), s, tc.line+" | display json")
		if err != nil || shellEnvelope(t, out).OK {
			t.Fatal("Shell test accepted failed prerequisite", err)
		}
		if s.lastReport == previous || s.lastReport.RunID == previous.RunID || !s.lastReport.Started.After(old.Started) || s.lastReport.Outcome != tc.want || s.lastReport.Passed() {
			t.Fatalf("preflight kept old PASS: %+v", s.lastReport)
		}
		if tc.want == watchrun.SkipOutcome && s.lastReport.Steps[0].SkipReason != watchrun.SkipUnsupported {
			t.Fatalf("unsupported check lost skip reason: %+v", s.lastReport.Steps[0])
		}
		if strings.Contains(s.lastReport.Steps[0].Name, "secret") || s.lastReport.Steps[0].Scope.AgentID != s.selected {
			t.Fatal("unsafe/unpinned preflight report")
		}
		if tc.want == watchrun.SkipOutcome && s.lastSSID != "" {
			t.Fatal("untrusted invalid SSID leaked to historical report")
		}
	}
	last := s.lastReport
	if _, err := runReplLine(context.Background(), s, "test"); err != nil || s.lastReport != last {
		t.Fatal("bare check replaced last attempt", err)
	}
	s.failedUse = true
	if report, err := executeProfile(context.Background(), s, "link", "Lab", "ipv4", "", nil); err != nil || report.Passed() || report.Steps[0].Reason != "previous connection attempt failed; target not ready" {
		t.Fatalf("failed use followed by check=%+v %v", report, err)
	}
	after, _ := json.Marshal(old)
	if string(before) != string(after) {
		t.Fatal("historical report mutated")
	}
	lastText, err := renderProfileReport(s, true)
	if err != nil || !strings.Contains(lastText, "Historical") || !strings.Contains(lastText, "MISSING") || strings.Contains(lastText, old.RunID) {
		t.Fatalf("last=%q err=%v", lastText, err)
	}
	noReport, err := renderProfileReport(&shellState{}, false)
	if err != nil || !strings.Contains(noReport, "No report") {
		t.Fatal(noReport, err)
	}
}
