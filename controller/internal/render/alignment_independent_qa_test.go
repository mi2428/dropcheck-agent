package render

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"testing"
	"unicode"

	"github.com/mi2428/dropcheck-agent/controller/internal/command"
	"github.com/mi2428/dropcheck-agent/controller/internal/controlpb"
	"github.com/mi2428/dropcheck-agent/controller/internal/pipeline"
	"github.com/mi2428/dropcheck-agent/controller/internal/shell"
	"github.com/mi2428/dropcheck-agent/controller/internal/watchrun"
	"google.golang.org/protobuf/proto"
)

func independentCompact(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, value)
}

func TestIndependentAlignmentCanceledPartDoesNotLeakReason(t *testing.T) {
	result := watchrun.OperationResult{Parts: []watchrun.OperationRecord{{
		Name: "passive partial acquisition",
		Raw:  &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_CANCELED, Message: "password=independent-secret-marker"},
	}}}
	for _, width := range []int{8, 24, 40, 80, 120} {
		out, err := operationResult("self", result, Presentation{Width: width}, func(string) string { return "self" })
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(independentCompact(out), "independent-secret-marker") {
			t.Errorf("width=%d canceled-part Reason leaked a synthetic credential", width)
		}
	}
}

func TestIndependentAlignmentTracerouteRetainsTypedDetail(t *testing.T) {
	result := &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_FAILED, Payload: &controlpb.CommandResult_Traceroute{Traceroute: &controlpb.TracerouteResult{
		Host: "2001:db8::1", MaxHops: 29, SizeBytes: 1259, ExitCode: 73, ReachedTarget: new(false), ObservationError: "typed observation unavailable",
	}}}
	out, err := CommandResult("self", result, command.Options{Detail: true}, Presentation{Detail: true, Width: 120})
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"29", "1259", "73"} {
		if !strings.Contains(out, value) {
			t.Errorf("interactive detail discarded typed traceroute value %s", value)
		}
	}
}

func TestIndependentAlignmentPopulatedOutputBoundaries(t *testing.T) {
	identity := "password=Guest-日本語e\u0301👩\u200d🔬-" + strings.Repeat("long", 15)
	ip := "2001:db8:ffff:ffff:ffff:ffff:ffff:ffff"
	marker := "証拠e\u0301👩\u200d🔬-" + strings.Repeat("reference", 12)
	raw := "Raw:    preserve:    spaces\nhttps://example.test/a:b\n" + marker
	for name, fixture := range map[string]struct {
		result  *controlpb.CommandResult
		options command.Options
		values  []string
	}{
		"ping raw":         {result: &controlpb.CommandResult{Payload: &controlpb.CommandResult_Ping{Ping: &controlpb.PingResult{Host: ip, Transmitted: proto.Uint32(0), Received: proto.Uint32(0), MinMs: proto.Float64(0), Output: raw}}}, values: []string{ip, marker, "Transmitted", "Received"}},
		"trace partial":    {result: &controlpb.CommandResult{Payload: &controlpb.CommandResult_Traceroute{Traceroute: &controlpb.TracerouteResult{Host: ip, ReachedTarget: new(false), ObservationError: marker, Hops: []*controlpb.TracerouteHop{{Index: 0, Hostnames: []string{marker}, Addresses: []string{ip}, RttMs: []float64{0}, TimedOut: false}}}}}, values: []string{ip, marker, "false"}},
		"test":             {result: &controlpb.CommandResult{Payload: &controlpb.CommandResult_WifiAssert{WifiAssert: &controlpb.WifiAssertResult{Checks: []*controlpb.DiagnosticCheck{{Key: "ssid", Expected: identity, Actual: identity, Passed: false, Message: marker}}}}}, values: []string{identity, marker, "false"}},
		"monitor":          {result: &controlpb.CommandResult{Payload: &controlpb.CommandResult_WifiMonitor{WifiMonitor: &controlpb.WifiMonitorResult{Events: []*controlpb.WifiEvent{{Type: marker, Message: ip, Fields: []*controlpb.DiagnosticField{{Key: "ssid", Value: identity}}}}}}}, values: []string{identity, marker, ip}},
		"cycle":            {result: &controlpb.CommandResult{Payload: &controlpb.CommandResult_WifiCycle{WifiCycle: &controlpb.WifiCycleResult{Steps: []*controlpb.WifiCycleStep{{Index: 0, Connect: &controlpb.ConnectWifiResult{Ssid: identity}, Errors: []string{marker}}}}}}, values: []string{identity, marker, "false"}},
		"MLO free strings": {result: &controlpb.CommandResult{Payload: &controlpb.CommandResult_WifiDiagnostics{WifiDiagnostics: &controlpb.WifiDiagnostics{Scan: &controlpb.WifiScan{Results: []*controlpb.WifiScanResult{{Ssid: identity, Bssid: "02:00:00:11:22:33", WifiStandard: "802.11be", Band: marker, SecurityTypes: []string{"eap_wpa3_enterprise", marker}}}}}}}, options: command.Options{WifiRenderMode: command.WifiRenderModeEHT}, values: []string{identity, marker, "02:00:00:11:22:33"}},
		"networks":         {result: &controlpb.CommandResult{Payload: &controlpb.CommandResult_NetworkSnapshot{NetworkSnapshot: &controlpb.NetworkSnapshot{Errors: []string{marker}}}}, values: []string{marker}},
		"permissions":      {result: &controlpb.CommandResult{Payload: &controlpb.CommandResult_PermissionSnapshot{PermissionSnapshot: &controlpb.PermissionSnapshot{Permissions: []*controlpb.DiagnosticField{{Key: marker, Value: "false"}}}}}, values: []string{marker, "false"}},
		"wifi status":      {result: &controlpb.CommandResult{Payload: &controlpb.CommandResult_WifiStatus{WifiStatus: &controlpb.WifiStatus{Connection: &controlpb.WifiConnection{Raw: "OBJECT-RAW", ObservationFields: []*controlpb.DiagnosticField{{Key: "note", Value: marker}}}, Permissions: []string{ip}}}}, values: []string{marker, ip}},
		"connect":          {result: &controlpb.CommandResult{Payload: &controlpb.CommandResult_ConnectWifi{ConnectWifi: &controlpb.ConnectWifiResult{Ssid: identity, Message: marker}}}, values: []string{identity, marker, "false"}},
		"ip status":        {result: &controlpb.CommandResult{Payload: &controlpb.CommandResult_IpStatus{IpStatus: &controlpb.IpStatus{RawLinkProperties: "OBJECT-RAW", ObservationFields: []*controlpb.DiagnosticField{{Key: "note", Value: marker}}}}}, values: []string{marker}},
		"path mtu":         {result: &controlpb.CommandResult{Payload: &controlpb.CommandResult_PathMtu{PathMtu: &controlpb.PathMtuResult{Host: ip, Error: marker, Probes: []*controlpb.PathMtuProbe{{Passed: false}}}}}, values: []string{ip, marker, "false"}},
		"global ip":        {result: &controlpb.CommandResult{Payload: &controlpb.CommandResult_GlobalIp{GlobalIp: &controlpb.GlobalIpResult{Addresses: []*controlpb.GlobalIpAddress{{Ip: ip, Error: marker}}}}}, values: []string{ip, marker, "false"}},
		"dns":              {result: &controlpb.CommandResult{Payload: &controlpb.CommandResult_ResolveDns{ResolveDns: &controlpb.ResolveDnsResult{Name: marker, Answers: []*controlpb.DnsAnswer{{Address: ip}}}}}, values: []string{marker, ip}},
		"http":             {result: &controlpb.CommandResult{Payload: &controlpb.CommandResult_HttpCheck{HttpCheck: &controlpb.HttpCheckResult{Url: "https://example.test/a:b", Error: marker}}}, values: []string{"https://example.test/a:b", marker, "false"}},
		"wget":             {result: &controlpb.CommandResult{Payload: &controlpb.CommandResult_Wget{Wget: &controlpb.WgetResult{Url: "https://example.test/a:b", ContentLength: -1, Error: marker}}}, values: []string{"https://example.test/a:b", marker, "-1"}},
		"scan":             {result: &controlpb.CommandResult{Payload: &controlpb.CommandResult_WifiScan{WifiScan: &controlpb.WifiScan{Results: []*controlpb.WifiScanResult{{Ssid: identity, Bssid: "02:00:00:11:22:33", SecurityTypes: []string{"eap_wpa3_enterprise"}, ObservationAgeMs: proto.Uint64(0)}}}}}, options: command.Options{WifiScanBrief: true}, values: []string{identity, "02:00:00:11:22:33", "wpa3-eap"}},
		"capabilities":     {result: &controlpb.CommandResult{Payload: &controlpb.CommandResult_WifiCapabilities{WifiCapabilities: &controlpb.WifiCapabilities{SupportedFeatures: []string{marker}, UnsupportedSecurityModes: []string{"eap_wpa3_enterprise"}}}}, values: []string{marker, "wpa3-eap", "no"}},
		"operation":        {result: &controlpb.CommandResult{Payload: &controlpb.CommandResult_WifiOperation{WifiOperation: &controlpb.WifiOperationResult{Operation: "reconnect", Fields: []*controlpb.DiagnosticField{{Key: "ssid", Value: identity}}, Errors: []string{marker}}}}, values: []string{identity, marker, "false"}},
		"scan detail":      {result: &controlpb.CommandResult{Payload: &controlpb.CommandResult_WifiScanDetail{WifiScanDetail: &controlpb.WifiScanDetail{Target: identity, Results: []*controlpb.WifiScanResult{{Ssid: identity, Bssid: "02:00:00:11:22:33", VenueName: marker, Raw: "OBJECT-RAW"}}}}}, values: []string{identity, marker}},
		"debug WifiInfo":   {result: &controlpb.CommandResult{Payload: &controlpb.CommandResult_WifiStatus{WifiStatus: &controlpb.WifiStatus{Connection: &controlpb.WifiConnection{Raw: raw}}}}, options: command.Options{RawView: "wifi info"}, values: []string{marker, "preserve", "https://example.test/a:b"}},
		"debug ScanResult": {result: &controlpb.CommandResult{Payload: &controlpb.CommandResult_WifiScan{WifiScan: &controlpb.WifiScan{Results: []*controlpb.WifiScanResult{{Raw: raw}}}}}, options: command.Options{RawView: "wifi scan"}, values: []string{marker, "preserve", "https://example.test/a:b"}},
		"debug Network":    {result: &controlpb.CommandResult{Payload: &controlpb.CommandResult_NetworkSnapshot{NetworkSnapshot: &controlpb.NetworkSnapshot{Networks: []*controlpb.NetworkDiagnostics{{NetworkId: "42", IpStatus: &controlpb.IpStatus{NetworkId: "42", RawLinkProperties: raw}}}}}}, options: command.Options{RawView: "network link-properties", RawNetworkID: "42"}, values: []string{marker, "preserve", "https://example.test/a:b"}},
	} {
		t.Run(name, func(t *testing.T) {
			fixture.result.Status = controlpb.CommandResult_STATUS_FAILED
			before := proto.Clone(fixture.result)
			for _, width := range []int{8, 24, 40, 80, 120} {
				out, err := CommandResult("self", fixture.result, fixture.options, Presentation{Detail: true, Width: width})
				if err != nil {
					t.Fatal(err)
				}
				for _, value := range fixture.values {
					if !strings.Contains(independentCompact(out), independentCompact(value)) {
						t.Errorf("width=%d lost %q", width, value)
					}
				}
				for line := range strings.SplitSeq(out, "\n") {
					if strings.ContainsRune(line, '\t') || displayWidth(line) > width {
						t.Errorf("width=%d invalid display line %q", width, line)
					}
				}
			}
			logical, err := CommandResult("self", fixture.result, fixture.options, Presentation{Detail: true})
			if err != nil {
				t.Fatal(err)
			}
			out, err := pipeline.JSON(logical, false)
			if err != nil || !json.Valid([]byte(out)) || !proto.Equal(before, fixture.result) {
				t.Fatalf("JSON or source mutated: %v", err)
			}
		})
	}
}

func TestIndependentAlignmentHelpLocalErrorAndReportBoundaries(t *testing.T) {
	marker := "日本語e\u0301👩\u200d🔬-" + strings.Repeat("long", 24)
	var help strings.Builder
	shell.WriteHelpTopic(&help, "request")
	for _, width := range []int{8, 24, 40, 80, 120} {
		local := Fields([][2]string{{"Target", marker}, {"Broadcast", "false"}, {"Layout width", "0"}}, Presentation{Width: width})
		errorText, err := CommandError("self", fmt.Errorf("%s\npassword=independent-secret-marker\u001b[31m\u0000", marker), true)
		if err != nil {
			t.Fatal(err)
		}
		report, err := Report(watchrun.Report{Outcome: watchrun.FailOutcome, Problems: []watchrun.Problem{{Message: marker}}, Cleanup: []watchrun.OperationRecord{{Error: "password=independent-secret-marker"}}}, Presentation{Width: width})
		if err != nil {
			t.Fatal(err)
		}
		for name, out := range map[string]string{"help": LayoutText(help.String(), Presentation{Width: width}), "local": local, "error": LayoutText(errorText, Presentation{Width: width}), "report": report} {
			if strings.Contains(independentCompact(out), "independent-secret-marker") {
				t.Errorf("%s width=%d leaked a synthetic credential", name, width)
			}
			if name != "help" && !strings.Contains(independentCompact(out), independentCompact(marker)) {
				t.Errorf("%s width=%d lost Unicode value", name, width)
			}
			for line := range strings.SplitSeq(out, "\n") {
				if displayWidth(line) > width || strings.ContainsAny(line, "\t\u001b\u0000") {
					t.Errorf("%s width=%d invalid line %q", name, width, line)
				}
			}
		}
	}
}

func TestIndependentAlignmentRawAndAgeBoundaries(t *testing.T) {
	raw := "Raw:    preserve:    spaces\nhttps://example.test/a:b"
	result := &controlpb.CommandResult{Payload: &controlpb.CommandResult_Ping{Ping: &controlpb.PingResult{Output: raw}}}
	out, err := CommandResult("self", result, command.Options{})
	if err != nil || !strings.Contains(out, raw) {
		t.Fatalf("raw colons were interpreted as fields: %v", err)
	}
	for _, ms := range []uint64{0, 999, 1000, 1001, 1<<53 - 1, 1<<53 + 1, math.MaxUint64 - 1, math.MaxUint64} {
		want := strconv.FormatUint(ms/1000, 10)
		if ms%1000 != 0 {
			want += "." + strings.TrimRight(fmt.Sprintf("%03d", ms%1000), "0")
		}
		if got := scanAgeSeconds(&controlpb.WifiScanResult{ObservationAgeMs: &ms}); got != want {
			t.Errorf("%dms: got %q want %q", ms, got, want)
		}
	}
}

func TestIndependentAlignmentIdentityFilterPrecedesDisplaySanitization(t *testing.T) {
	aps := []*controlpb.WifiScanResult{
		{Ssid: "Guest\t東京", Bssid: "02:00:00:11:22:01", WifiStandard: "802.11be"},
		{Ssid: "Guest 東京", Bssid: "02:00:00:11:22:02", WifiStandard: "802.11be"},
	}
	scan := &controlpb.WifiScan{Results: aps}
	for selected, ap := range aps {
		for _, family := range []string{"scan", "MLO"} {
			t.Run(fmt.Sprintf("%s/%d", family, selected), func(t *testing.T) {
				result := &controlpb.CommandResult{Payload: &controlpb.CommandResult_WifiScan{WifiScan: scan}}
				options := command.Options{WifiScanSSID: ap.Ssid, WifiScanBrief: true}
				if family == "MLO" {
					result.Payload = &controlpb.CommandResult_WifiDiagnostics{WifiDiagnostics: &controlpb.WifiDiagnostics{Scan: scan}}
					options = command.Options{WifiRenderMode: command.WifiRenderModeEHT, WifiEHTSSID: ap.Ssid}
				}
				out, err := CommandResult("self", result, options, Presentation{Width: 120})
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(out, ap.Bssid) {
					t.Error("display sanitization removed the selected observation")
				}
				if strings.Contains(out, aps[1-selected].Bssid) {
					t.Error("display sanitization widened the filter to a different SSID")
				}
			})
		}
	}
}

func TestIndependentAlignmentScanRowsAndLogicalPipes(t *testing.T) {
	scan := &controlpb.WifiScan{}
	for i := range 27 {
		scan.Results = append(scan.Results, &controlpb.WifiScanResult{Ssid: "password=Guest 東京e\u0301👩\u200d🔬", Bssid: fmt.Sprintf("02:00:00:11:22:%02x", i), ObservationAgeMs: new(uint64(i)), SecurityTypes: []string{"eap_wpa3_enterprise"}})
	}
	result := &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_FAILED, Payload: &controlpb.CommandResult_WifiScan{WifiScan: scan}}
	for _, width := range []int{8, 24, 40, 80, 120} {
		out, err := CommandResult("self", result, command.Options{WifiScanBrief: true}, Presentation{Width: width})
		if err != nil {
			t.Fatal(err)
		}
		for _, ap := range scan.Results {
			if !strings.Contains(independentCompact(out), ap.Bssid) {
				t.Errorf("width=%d lost AP %s", width, ap.Bssid)
			}
		}
	}
	logical, err := CommandResult("self", result, command.Options{WifiScanBrief: true})
	if err != nil {
		t.Fatal(err)
	}
	pipe, err := pipeline.Parse([]string{"match 'SSID|BSSID'", "last 2"})
	if err != nil {
		t.Fatal(err)
	}
	filtered, err := pipe.Apply(logical)
	if err != nil || !strings.Contains(filtered, "02:00:00:11:22:1a") || !strings.Contains(filtered, "password=Guest") {
		t.Fatalf("logical pipe lost safe identities: %v", err)
	}
	encoded, err := pipeline.JSON(logical, false)
	var envelope struct {
		OK    bool     `json:"ok"`
		Lines []string `json:"lines"`
	}
	if err != nil || json.Unmarshal([]byte(encoded), &envelope) != nil || envelope.OK || !strings.Contains(strings.Join(envelope.Lines, "\n"), "password=Guest") {
		t.Fatal("presentation JSON changed failure or literal identity")
	}
	var table strings.Builder
	writeDisplayTable(&table, []displayTableColumn{{header: "NAME"}, {header: "VALUE"}, {header: "AGE(s)"}}, [][]string{{"日本語", "abcdefg", "0"}, {"e\u0301👩\u200d🔬", "v", "123456.789"}})
	for _, token := range []string{"AGE(s)", "0", "123456.789"} {
		if got := displayColumn(renderedLineContaining(table.String(), token), token); got != 17 {
			t.Errorf("AGE left edge=%d, want 17: %q", got, table.String())
		}
	}
	if !strings.Contains(table.String(), "日本語  abcdefg  0") {
		t.Fatal("table does not have two-cell column gaps")
	}
}

func TestIndependentAlignmentDiagnosticTableDoesNotHardwrapHeaders(t *testing.T) {
	result := &controlpb.CommandResult{Payload: &controlpb.CommandResult_WifiOperation{WifiOperation: &controlpb.WifiOperationResult{
		Operation: "reconnect", Fields: []*controlpb.DiagnosticField{{Key: "mlo_" + strings.Repeat("日本語", 30), Value: "2001:db8:ffff:ffff:ffff:ffff:ffff:ffff"}},
	}}}
	for _, width := range []int{8, 24, 40, 80, 120} {
		out, err := CommandResult("self", result, command.Options{}, Presentation{Width: width})
		if err != nil {
			t.Fatal(err)
		}
		for line := range strings.SplitSeq(out, "\n") {
			if strings.TrimSpace(line) == "FIELD" {
				t.Errorf("width=%d diagnostic table header was hard-wrapped away from its VALUE column", width)
				break
			}
		}
	}
}

func TestIndependentAlignmentFilteredCloneKeepsTotalsAndJSONContract(t *testing.T) {
	aps := []*controlpb.WifiScanResult{
		{Ssid: "Guest\x1b[31m東京", Bssid: "02:00:00:11:22:01", WifiStandard: "802.11be"},
		{Ssid: "Guest東京", Bssid: "02:00:00:11:22:02", WifiStandard: "802.11be"},
	}
	for _, total := range []string{"", "17"} {
		scan := &controlpb.WifiScan{Results: aps}
		wantTotal := total
		if total == "" {
			wantTotal = "2"
		} else {
			scan.Fields = []*controlpb.DiagnosticField{{Key: "scan_result_total_count", Value: total}}
		}
		for _, family := range []string{"scan", "MLO"} {
			for selected, ap := range aps {
				for _, byBSSID := range []bool{false, true} {
					result := &controlpb.CommandResult{Payload: &controlpb.CommandResult_WifiScan{WifiScan: scan}}
					options := command.Options{WifiScanBrief: true, WifiScanSSID: ap.Ssid}
					if byBSSID {
						options.WifiScanSSID, options.WifiScanBSSID = "", ap.Bssid
					}
					if family == "MLO" {
						result.Payload = &controlpb.CommandResult_WifiDiagnostics{WifiDiagnostics: &controlpb.WifiDiagnostics{Scan: scan}}
						options.WifiRenderMode, options.WifiEHTSSID, options.WifiEHTBSSID = command.WifiRenderModeEHT, options.WifiScanSSID, options.WifiScanBSSID
					}
					before := proto.Clone(result)
					view := presentationResult(result, options)
					filtered := view.GetWifiScan()
					if family == "MLO" {
						filtered = view.GetWifiDiagnostics().GetScan()
					}
					if len(filtered.GetResults()) != 1 || filtered.GetResults()[0].GetBssid() != ap.Bssid || diagnosticFieldMap(filtered.GetFields())["scan_result_total_count"] != wantTotal {
						t.Fatalf("%s selected=%d bssid=%t changed target or acquired total", family, selected, byBSSID)
					}
					out, err := CommandResult("self", result, options, Presentation{Width: 24})
					if err != nil || !strings.Contains(independentCompact(out), ap.Bssid) || strings.Contains(independentCompact(out), aps[1-selected].Bssid) {
						t.Fatalf("%s selected=%d bssid=%t re-filtered the sanitized identity: %v", family, selected, byBSSID, err)
					}
					logical, err := CommandResult("self", result, options)
					if err != nil {
						t.Fatal(err)
					}
					encoded, err := pipeline.JSON(logical, true)
					if err != nil || !json.Valid([]byte(encoded)) || !strings.Contains(encoded, ap.Bssid) || strings.Contains(encoded, aps[1-selected].Bssid) || !proto.Equal(before, result) {
						t.Fatalf("%s changed selected Shell JSON values or input proto: %v", family, err)
					}
				}
			}
		}
	}
}
