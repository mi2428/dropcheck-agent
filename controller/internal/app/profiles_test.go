package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"dropcheck/controller/internal/command"
	"dropcheck/controller/internal/control"
	"dropcheck/controller/internal/controlpb"
	"dropcheck/controller/internal/harness"
	"dropcheck/controller/internal/runner"
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

func TestLinkExistingWiFiReadOnlyAndRevalidation(t *testing.T) {
	for _, tc := range []struct {
		name, ssid, family, change, want string
		ops                              int
	}{
		{"ipv4", "Lab", "ipv4", "", "pass", 2},
		{"ipv6", "Lab", "ipv6", "", "pass", 2},
		{"wrong-case", "lab", "ipv4", "", "fail", 1},
		{"unknown", "Lab", "ipv4", "unknown", "missing", 1},
		{"lost-network", "Lab", "ipv4", "lost", "missing", 1},
		{"unknown-after-first", "Lab", "ipv4", "unknown-ip", "missing", 2},
		{"vpn", "Lab", "ipv4", "vpn", "missing", 1},
		{"changed-after-first", "Lab", "ipv4", "post", "fail", 2},
		{"changed-interface", "Lab", "ipv4", "iface", "fail", 2},
		{"lost-after-first", "Lab", "ipv4", "post-lost", "missing", 2},
		{"missing-dns", "Lab", "ipv4", "dns", "fail", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := harness.Compile(linkPlan(tc.ssid, tc.family), []control.AgentInfo{{ID: "agent-a"}})
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
			report, _ := harness.Execute(context.Background(), plan, r, harness.ExecuteOptions{Rounds: 1})
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
			}
		})
	}
}

func TestLinkInvalidPlanAndHistoricalPreflight(t *testing.T) {
	if err := harness.Validate(harness.Plan{Networks: []harness.Network{harness.ObservedWiFi("lab").SSID("Lab").PSK("not-a-valid-secret")}, Checks: []harness.Check{harness.WiFiStatus()}}); err == nil {
		t.Fatal("read-only plan accepted a connection credential")
	}
	if err := harness.Validate(harness.Plan{Networks: []harness.Network{harness.ObservedWiFi("lab").SSID("Lab").BSSID("02:00:00:00:00:11")}, Checks: []harness.Check{harness.WiFiStatus()}}); err == nil {
		t.Fatal("read-only plan promised strict BSSID pinning")
	}
	if err := harness.Validate(harness.Plan{Networks: []harness.Network{harness.ObservedWiFi("lab").SSID("Lab")}, Checks: []harness.Check{harness.Ping("example.test")}}); err == nil {
		t.Fatal("read-only plan accepted active probe")
	}
	s := &shellState{server: control.NewServer("synthetic", nil), selected: "expired-agent"}
	compiled, err := harness.Compile(linkPlan("Lab", "ipv4"), []control.AgentInfo{{ID: "old-agent"}})
	if err != nil {
		t.Fatal(err)
	}
	old, err := harness.Execute(context.Background(), compiled, &linkRunner{change: func(op command.Operation, _ int) *controlpb.CommandResult {
		return linkResult("Lab", "101", "ipv4")
	}}, harness.ExecuteOptions{Rounds: 1})
	if err != nil || !old.Passed() {
		t.Fatalf("old fixture is not PASS: %s %v", old.Outcome, err)
	}
	s.lastReport, s.lastProfile, s.lastSSID = &old, "link", "Lab"
	if view, err := renderProfileReport(s, outputText, false); err != nil || !strings.Contains(view, "Android validated: false (observation only)") {
		t.Fatalf("false validation must not fail link or imply internet: %q %v", view, err)
	}
	before, _ := json.Marshal(old)
	for _, tc := range []struct{ name, ssid, family, rejection string }{
		{"link", "credential-like-ssid", "ipv4", "invalid BSSID; pinning unsupported"},
		{"unsupported", "", "", "profile unsupported; candidates: link, lab, internet, eht"},
		{"link", "Lab", "ipv4", ""}, // expired pinned agent; no fallback
		{"link", "", "invalid", ""}, // zero-config/family
	} {
		previous := s.lastReport
		if err := s.runCheck(context.Background(), tc.name, tc.ssid, tc.family, "", tc.rejection, outputJSON, pipePipeline{}, true); err == nil {
			t.Fatal("strict check accepted failed prerequisite")
		}
		if s.lastReport == previous || s.lastReport.RunID == previous.RunID || !s.lastReport.Started.After(old.Started) || s.lastReport.Outcome != harness.MissingOutcome || s.lastReport.Passed() {
			t.Fatalf("preflight kept old PASS: %+v", s.lastReport)
		}
		if strings.Contains(s.lastReport.Steps[0].Name, "secret") || s.lastReport.Steps[0].Scope.AgentID != s.selected {
			t.Fatal("unsafe/unpinned preflight report")
		}
		if tc.rejection != "" && s.lastSSID != "" {
			t.Fatal("untrusted invalid SSID leaked to historical report")
		}
	}
	last := s.lastReport
	if err := s.runCheck(context.Background(), "", "", "", "", "", outputText, pipePipeline{}, true); err != nil || s.lastReport != last {
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
	lastText, err := renderProfileReport(s, outputText, true)
	if err != nil || !strings.Contains(lastText, "Historical") || !strings.Contains(lastText, "MISSING") || strings.Contains(lastText, old.RunID) {
		t.Fatalf("last=%q err=%v", lastText, err)
	}
	noReport, err := renderProfileReport(&shellState{}, outputText, false)
	if err != nil || !strings.Contains(noReport, "No report") {
		t.Fatal(noReport, err)
	}
}

func TestOneShotInvalidCheckNeedsNoSession(t *testing.T) {
	for _, args := range [][]string{
		{"check", "link", "ssid", "Lab", "bssid", "02:00:00:00:00:11"},
		{"check", "internet", "ssid", "Lab"},
		{"check", "link", "ssid", "Lab", "family", "auto"},
	} {
		if err := runCLI(context.Background(), shellOptions{ADBPath: "not-a-real-adb"}, args); err == nil {
			t.Fatalf("invalid check accepted: %v", args)
		}
	}
}
