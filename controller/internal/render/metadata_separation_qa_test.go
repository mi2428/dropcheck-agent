package render

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"dropcheck/controller/internal/command"
	"dropcheck/controller/internal/controlpb"
	"dropcheck/controller/internal/pipeline"
	"dropcheck/controller/internal/watchrun"
	"google.golang.org/protobuf/proto"
)

// Inspect only known generated labels, never infer metadata from arbitrary values.
func metadataQAValues(text, label string) []string {
	var values []string
	for line := range strings.SplitSeq(text, "\n") {
		if value, ok := strings.CutPrefix(line, "  "+label+":"); ok {
			values = append(values, strings.TrimLeft(value, " "))
		}
	}
	return values
}

func metadataQAHeader(t *testing.T, text, target string) {
	t.Helper()
	for label, want := range map[string]string{"Source": "android", "Target": target} {
		got := metadataQAValues(text, label)
		if len(got) != 1 || got[0] != want {
			t.Errorf("generated %s = %q; want one independent value %q", label, got, want)
		}
	}
}

func TestMetadataSeparationCommandFamilies(t *testing.T) {
	cases := []struct {
		name string
		raw  *controlpb.CommandResult
		opts command.Options
	}{
		{"request_ip_external", &controlpb.CommandResult{Payload: &controlpb.CommandResult_GlobalIp{GlobalIp: &controlpb.GlobalIpResult{Service: "https://example.test/Target:literal", Addresses: []*controlpb.GlobalIpAddress{{Ip: "2001:db8::1", Status: 0, Global: false}}}}}, command.Options{}},
		{"request_ping", &controlpb.CommandResult{Payload: &controlpb.CommandResult_Ping{Ping: &controlpb.PingResult{Host: "2001:db8::1"}}}, command.Options{}},
		{"request_traceroute", &controlpb.CommandResult{Payload: &controlpb.CommandResult_Traceroute{Traceroute: &controlpb.TracerouteResult{Host: "2001:db8::1", ReachedTarget: new(false)}}}, command.Options{}},
		{"request_pmtu", &controlpb.CommandResult{Payload: &controlpb.CommandResult_PathMtu{PathMtu: &controlpb.PathMtuResult{Host: "2001:db8::1"}}}, command.Options{}},
		{"request_dns_resolve", &controlpb.CommandResult{Payload: &controlpb.CommandResult_ResolveDns{ResolveDns: &controlpb.ResolveDnsResult{Name: "Source:literal"}}}, command.Options{}},
		{"request_http_check", &controlpb.CommandResult{Payload: &controlpb.CommandResult_HttpCheck{HttpCheck: &controlpb.HttpCheckResult{Url: "https://example.test/Target:literal", Status: 0, Matched: false}}}, command.Options{}},
		{"request_http_download", &controlpb.CommandResult{Payload: &controlpb.CommandResult_Wget{Wget: &controlpb.WgetResult{Url: "https://example.test/Source:literal"}}}, command.Options{}},
		{"show_wifi_status", &controlpb.CommandResult{Payload: &controlpb.CommandResult_WifiStatus{WifiStatus: &controlpb.WifiStatus{}}}, command.Options{WifiRenderMode: command.WifiRenderModeStatus}},
		{"show_ip_status", &controlpb.CommandResult{Payload: &controlpb.CommandResult_IpStatus{IpStatus: &controlpb.IpStatus{}}}, command.Options{}},
		{"show_wifi_full", &controlpb.CommandResult{Payload: &controlpb.CommandResult_WifiDiagnostics{WifiDiagnostics: &controlpb.WifiDiagnostics{Status: &controlpb.WifiStatus{}}}}, command.Options{WifiRenderMode: command.WifiRenderModeFull}},
		{"show_wifi_eht", &controlpb.CommandResult{Payload: &controlpb.CommandResult_WifiDiagnostics{WifiDiagnostics: &controlpb.WifiDiagnostics{}}}, command.Options{WifiRenderMode: command.WifiRenderModeEHT}},
		{"show_wifi_scan", &controlpb.CommandResult{Payload: &controlpb.CommandResult_WifiScan{WifiScan: &controlpb.WifiScan{}}}, command.Options{}},
		{"show_wifi_scan_detail", &controlpb.CommandResult{Payload: &controlpb.CommandResult_WifiScanDetail{WifiScanDetail: &controlpb.WifiScanDetail{}}}, command.Options{}},
		{"show_wifi_capabilities", &controlpb.CommandResult{Payload: &controlpb.CommandResult_WifiCapabilities{WifiCapabilities: &controlpb.WifiCapabilities{}}}, command.Options{}},
		{"show_networks", &controlpb.CommandResult{Payload: &controlpb.CommandResult_NetworkSnapshot{NetworkSnapshot: &controlpb.NetworkSnapshot{}}}, command.Options{}},
		{"show_permissions", &controlpb.CommandResult{Payload: &controlpb.CommandResult_PermissionSnapshot{PermissionSnapshot: &controlpb.PermissionSnapshot{}}}, command.Options{}},
		{"request_wifi_connect", &controlpb.CommandResult{Payload: &controlpb.CommandResult_ConnectWifi{ConnectWifi: &controlpb.ConnectWifiResult{Ssid: "Target: literal Source: literal"}}}, command.Options{}},
		{"request_wifi_operation", &controlpb.CommandResult{Payload: &controlpb.CommandResult_WifiOperation{WifiOperation: &controlpb.WifiOperationResult{}}}, command.Options{}},
		{"test_wifi", &controlpb.CommandResult{Payload: &controlpb.CommandResult_WifiAssert{WifiAssert: &controlpb.WifiAssertResult{Passed: false}}}, command.Options{}},
		{"monitor_wifi", &controlpb.CommandResult{Payload: &controlpb.CommandResult_WifiMonitor{WifiMonitor: &controlpb.WifiMonitorResult{}}}, command.Options{}},
		{"request_wifi_cycle", &controlpb.CommandResult{Payload: &controlpb.CommandResult_WifiCycle{WifiCycle: &controlpb.WifiCycleResult{}}}, command.Options{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.raw.Status = controlpb.CommandResult_STATUS_CANCELED
			tc.raw.Message = "partial acquisition; Target: literal Source: literal"
			before := proto.Clone(tc.raw)
			for _, interactive := range []bool{false, true} {
				text, err := CommandResult("pinned-PC-target", tc.raw, tc.opts, pipeline.FormatText, Presentation{Interactive: interactive, Detail: true})
				if err != nil {
					t.Fatal(err)
				}
				metadataQAHeader(t, text, "pinned-PC-target")
			}
			if !proto.Equal(before, tc.raw) {
				t.Error("presentation changed the acquired result")
			}
		})
	}
}

func TestMetadataSeparationWidths(t *testing.T) {
	target := "selected-Target:literal-Source:literal-" + strings.Repeat("東京e\u0301👩\u200d🔬", 9) + "-2001:db8::1\nend\x00\x1b[31m"
	safeTarget := cleanDisplayCell(target)
	result := &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_FAILED, Message: "partial\nTarget: free-text value password=qa-synthetic-secret", Payload: &controlpb.CommandResult_HttpCheck{HttpCheck: &controlpb.HttpCheckResult{Url: "https://user:qa-synthetic-secret@example.test/Source:literal?token=qa-synthetic-secret&ordinary=Keep"}}}
	for _, width := range []int{0, 8, 24, 40, 80, 120} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			out, err := CommandResult(target, result, command.Options{}, pipeline.FormatText, Presentation{Interactive: true, Width: width})
			if err != nil {
				t.Fatal(err)
			}
			if width == 0 {
				metadataQAHeader(t, out, safeTarget)
			}
			// Wrapping may split labels and values, but not change their ownership/order.
			want := "Source:androidTarget:" + alignmentCompact(safeTarget)
			if !strings.Contains(alignmentCompact(out), want) {
				t.Error("independent Source/Target sequence or complete safe target missing after layout")
			}
			targetLabel := strings.TrimRight(Fields([][2]string{{"Target", ""}}, Presentation{Width: width}), " \n")
			if !strings.Contains("\n"+out, "\n"+targetLabel) {
				t.Error("Target has no independent generated label after layout")
			}
			if strings.Contains(alignmentCompact(out), "qa-synthetic-secret") || strings.ContainsAny(out, "\x00\x1b\t") {
				t.Error("unsafe presentation")
			}
			for line := range strings.SplitSeq(out, "\n") {
				if width > 0 && displayWidth(line) > width {
					t.Errorf("line exceeds width %d: %q", width, line)
				}
			}
		})
	}
}

func TestMetadataSeparationDebugViews(t *testing.T) {
	conn := &controlpb.WifiConnection{Ssid: "structured-only", Raw: "RAW Target: literal Source: literal 2001:db8::1 password=qa-synthetic-secret"}
	ip := &controlpb.IpStatus{NetworkId: "42", Wifi: conn, RawCapabilities: conn.Raw, RawLinkProperties: conn.Raw}
	for _, view := range []string{"wifi info", "wifi scan", "network capabilities", "network link-properties", "network wifi-info"} {
		t.Run(view, func(t *testing.T) {
			result := &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_FAILED, Message: "partial capture"}
			switch view {
			case "wifi info":
				result.Payload = &controlpb.CommandResult_WifiStatus{WifiStatus: &controlpb.WifiStatus{Connection: conn}}
			case "wifi scan":
				result.Payload = &controlpb.CommandResult_WifiScan{WifiScan: &controlpb.WifiScan{Results: []*controlpb.WifiScanResult{{Raw: conn.Raw}}}}
			default:
				result.Payload = &controlpb.CommandResult_NetworkSnapshot{NetworkSnapshot: &controlpb.NetworkSnapshot{Networks: []*controlpb.NetworkDiagnostics{{NetworkId: "42", IpStatus: ip}}}}
			}
			opts := command.Options{RawView: view, RawNetworkID: "42"}
			text, err := CommandResult("pinned-PC-target", result, opts, pipeline.FormatText)
			if err != nil {
				t.Fatal(err)
			}
			metadataQAHeader(t, text, "pinned-PC-target")
			jsonText, err := CommandResult("pinned-PC-target", result, opts, pipeline.FormatJSON)
			var value map[string]any
			if err != nil || json.Unmarshal([]byte(jsonText), &value) != nil {
				t.Fatal("invalid debug JSON", err)
			}
			if value["source"] != "android" || value["target"] != "pinned-PC-target" || value["status"] != "failed" {
				t.Error("existing independent debug JSON metadata changed")
			}
			for _, out := range []string{text, jsonText} {
				if !strings.Contains(out, "Target: literal Source: literal 2001:db8::1") || strings.Contains(out, "qa-synthetic-secret") || strings.Contains(out, "structured-only") {
					t.Error("raw-only safe values changed or leaked")
				}
			}
		})
	}
}

func TestMetadataSeparationInteractiveJSONAndFilter(t *testing.T) {
	result := &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_FAILED, Message: "partial", Payload: &controlpb.CommandResult_GlobalIp{GlobalIp: &controlpb.GlobalIpResult{}}}
	text, err := CommandResult("pinned-PC-target", result, command.Options{}, pipeline.FormatText, Presentation{Interactive: true})
	if err != nil {
		t.Fatal(err)
	}
	// Same public stages as app.pipePipeline.shellOutput; no app import cycle.
	text = LayoutText(text, Presentation{})
	jsonText, err := pipeline.JSON(text, false)
	var value struct {
		OK    bool     `json:"ok"`
		Lines []string `json:"lines"`
	}
	if err != nil || json.Unmarshal([]byte(jsonText), &value) != nil || value.OK {
		t.Fatal("invalid interactive JSON outcome", err)
	}
	metadataQAHeader(t, strings.Join(value.Lines, "\n"), "pinned-PC-target")
	for _, removed := range []string{"Source", "Target"} {
		t.Run("except_"+removed, func(t *testing.T) {
			filter, err := pipeline.Parse([]string{"except '^ *" + removed + ":'"})
			if err != nil {
				t.Fatal(err)
			}
			out, err := filter.Apply(text)
			if err != nil {
				t.Fatal(err)
			}
			if len(metadataQAValues(out, removed)) != 0 {
				t.Error("explicitly excluded field was reinserted")
			}
			kept, want := "Source", "android"
			if removed == "Source" {
				kept, want = "Target", "pinned-PC-target"
			}
			got := metadataQAValues(out, kept)
			if len(got) != 1 || got[0] != want {
				t.Errorf("excluding %s damaged independent %s: %q", removed, kept, got)
			}
		})
	}
}

func TestMetadataSeparationOperationAndHistoricalReport(t *testing.T) {
	raw := &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_CANCELED, Message: "partial capture", Payload: &controlpb.CommandResult_IpStatus{IpStatus: &controlpb.IpStatus{}}}
	t.Run("operation", func(t *testing.T) {
		out, err := OperationResult("pinned-PC-target", watchrun.OperationResult{Raw: raw}, pipeline.FormatText, Presentation{})
		if err != nil {
			t.Fatal(err)
		}
		metadataQAHeader(t, out, "pinned-PC-target")
	})
	t.Run("historical_report", func(t *testing.T) {
		scope := watchrun.Scope{Kind: "check", AgentID: "agent-1", TargetID: "target-1"}
		report := watchrun.Report{Outcome: watchrun.CanceledOutcome, Started: time.Unix(1, 0), Ended: time.Unix(2, 0), Agents: []watchrun.AgentProgress{{Agent: watchrun.AgentSnapshot{ID: "agent-1", Name: "pinned-PC-target"}}}, Steps: []watchrun.StepReport{{Scope: scope, Outcome: watchrun.CanceledOutcome, Attempts: []watchrun.Attempt{{Scope: scope, Outcome: watchrun.CanceledOutcome, Result: watchrun.OperationResult{Raw: raw}}}}}}
		out, err := Report(report, Presentation{Detail: true})
		if err != nil {
			t.Fatal(err)
		}
		metadataQAHeader(t, out, "pinned-PC-target")
	})
}

func TestMetadataSeparationTypedLiteralValues(t *testing.T) {
	rows := [][2]string{{"Source", "android"}, {"Target", "self"}, {"SSID", "Target: literal Source: literal 2001:db8::1"}, {"URL", "https://example.test/Target:literal/Source:literal"}, {"Status", "0"}, {"Matched", "false"}, {"Availability", "?"}}
	out := Fields(rows, Presentation{})
	for _, row := range rows {
		got := metadataQAValues(out, row[0])
		if len(got) != 1 || got[0] != row[1] {
			t.Errorf("typed %s changed: %q", row[0], got)
		}
	}
}

func TestMetadataSeparationStatusMessageAndProtoJSON(t *testing.T) {
	for _, status := range []controlpb.CommandResult_Status{controlpb.CommandResult_STATUS_FAILED, controlpb.CommandResult_STATUS_CANCELED} {
		message := "Target: literal Source: literal password=qa-synthetic-secret"
		raw := &controlpb.CommandResult{Status: status, Message: message, Payload: &controlpb.CommandResult_HttpCheck{HttpCheck: &controlpb.HttpCheckResult{Url: "https://example.test/Target:literal?token=qa-synthetic-secret&ordinary=Keep"}}}
		before := proto.Clone(raw)
		text, err := CommandResult("pinned-PC-target", raw, command.Options{}, pipeline.FormatText)
		if err != nil {
			t.Fatal(err)
		}
		for label, want := range map[string]string{"Status": resultStatus(status), "Message": safePresentationText(message), "Latency": "0ms"} {
			got := metadataQAValues(text, label)
			if len(got) != 1 || got[0] != want {
				t.Errorf("independent %s changed: %q", label, got)
			}
		}
		encoded, err := CommandResult("pinned-PC-target", raw, command.Options{}, pipeline.FormatJSON)
		var value map[string]any
		if err != nil || json.Unmarshal([]byte(encoded), &value) != nil {
			t.Fatal("invalid proto JSON", err)
		}
		if value["source"] != nil || value["target"] != nil || value["status"] != status.String() || strings.Contains(encoded, "qa-synthetic-secret") || !strings.Contains(encoded, "Target:literal") {
			t.Error("standard safe proto JSON contract changed")
		}
		if !proto.Equal(before, raw) {
			t.Error("presentation changed the acquired result")
		}
	}
}

func TestMetadataSeparationHistoricalAcquisitionsUseTheirOwnAgentScope(t *testing.T) {
	raw := &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_CANCELED, Message: "partial acquisition"}
	for _, tc := range []struct{ name, partAgent, want string }{
		{"different_agent", "agent-b", "acquired-B"},
		{"unlisted_agent", "unlisted-id", "unlisted-id"},
		{"missing_agent", "", "? (agent metadata unavailable)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scope := watchrun.Scope{Kind: watchrun.ScopeCheck, AgentID: "agent-a", TargetID: "ssid-target"}
			report := watchrun.Report{
				Outcome: watchrun.CanceledOutcome,
				Agents:  []watchrun.AgentProgress{{Agent: watchrun.AgentSnapshot{ID: "agent-a", Name: "acquired-A"}}, {Agent: watchrun.AgentSnapshot{ID: "agent-b", Name: "acquired-B"}}},
				Steps: []watchrun.StepReport{{Scope: scope, Attempts: []watchrun.Attempt{{Scope: scope, Result: watchrun.OperationResult{
					Raw: raw, Parts: []watchrun.OperationRecord{{Scope: watchrun.Scope{Kind: watchrun.ScopeAgent, AgentID: tc.partAgent}, Name: "snapshot", Raw: raw}},
				}}}}},
			}
			out, err := Report(report, Presentation{Detail: true})
			if err != nil {
				t.Fatal(err)
			}
			got := metadataQAValues(out, "Target")
			if len(got) != 2 || got[0] != "acquired-A" || got[1] != tc.want {
				t.Errorf("aggregate/part targets = %q; want [acquired-A %s], never substitute the attempt's agent", got, tc.want)
			}
		})
	}
}
