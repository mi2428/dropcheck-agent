package render

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode"

	"dropcheck/controller/internal/command"
	"dropcheck/controller/internal/controlpb"
	"dropcheck/controller/internal/harness"
	"dropcheck/controller/internal/pipeline"
)

func styleField(t *testing.T, text, label, value string, indent int) int {
	t.Helper()
	prefix := strings.Repeat(" ", indent) + label + ":"
	for line := range strings.SplitSeq(text, "\n") {
		if after, ok := strings.CutPrefix(line, prefix); ok {
			tail := after
			if !strings.HasPrefix(tail, " ") || strings.TrimSpace(tail) != value {
				t.Errorf("%s: want value %q after colon and padding, got %q", label, value, line)
			}
			return displayWidth(prefix) + len(tail) - len(strings.TrimLeft(tail, " "))
		}
	}
	t.Errorf("%s: missing owned field at indent %d", label, indent)
	return -1
}

func TestStyleConsistencyFieldsWidthMatrix(t *testing.T) {
	rows := [][2]string{{"A", "0"}, {"長い項目", "false"}, {"e\u0301", "2001:db8::1"}, {"👩\u200d🔬", "https://example.test/a:b?q=x=y"}, {"Empty", ""}, {"Message", "first\nsecond"}, {"SSID", strings.Repeat("東京e\u0301👩\u200d🔬", 6)}}
	compact := func(s string) string {
		return strings.Map(func(r rune) rune {
			if unicode.IsSpace(r) {
				return -1
			}
			return r
		}, s)
	}
	for _, width := range []int{0, 8, 24, 40, 80, 120} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			text := Fields(rows, Presentation{Width: width})
			for _, row := range rows {
				if !strings.Contains(compact(text), compact(row[1])) {
					t.Errorf("lost value for %s", row[0])
				}
			}
			for line := range strings.SplitSeq(text, "\n") {
				if width > 0 && displayWidth(line) > width {
					t.Errorf("display width %d exceeds %d", displayWidth(line), width)
				}
			}
			if width == 0 || width >= 80 {
				column := -1
				for _, row := range rows[:4] {
					got := styleField(t, text, row[0], row[1], 2)
					if column >= 0 && column != got {
						t.Errorf("display columns differ: %d != %d", column, got)
					}
					column = got
				}
			}
		})
	}
}

func TestStyleConsistencyTableRoles(t *testing.T) {
	var b strings.Builder
	b.WriteString("Scan\n")
	writeDisplayTable(&b, []displayTableColumn{{header: "SSID"}, {header: "AGE(s)"}}, [][]string{{"東京", "0"}, {"e\u0301👩\u200d🔬", "12.5"}})
	writeRecord(&b, "More", "show wifi scan")
	text := wrapPresentation(b.String(), 120)
	if !strings.HasPrefix(text, "Scan\n\nSSID  AGE(s)\n") || !strings.Contains(text, "12.5\n\n  More:") {
		t.Fatal("table boundaries or header role changed")
	}
	for _, row := range []string{"東京  0", "e\u0301👩\u200d🔬   12.5"} {
		if !strings.Contains(text, row) {
			t.Errorf("table must use display widths, two-space gap and left-aligned seconds: missing %q", row)
		}
	}
}

func TestStyleConsistencyNestedSourceContinuation(t *testing.T) {
	logical := SourceFields([][2]string{{"        深い項目", "first\nsecond\n2001:db8::1"}, {"Shallow", "third\nfourth"}})
	text := LayoutText(logical, Presentation{Width: 24})
	if !strings.HasPrefix(text, "          深い項目:\n") {
		t.Error("nested field label must retain its 10-space indent and immediate colon")
	}
	for _, value := range []string{"first", "second", "2001:db8::1"} {
		if !strings.Contains(text, "\n"+strings.Repeat(" ", 12)+value+"\n") {
			t.Errorf("nested multiline value %q must retain its first value's 12-space continuation indent", value)
		}
	}
	if !strings.Contains(text, "\n  Shallow:\n") {
		t.Error("shallow field must reset the preceding nested field's label indent")
	}
	for _, value := range []string{"third", "fourth"} {
		if !strings.Contains(text, "\n    "+value+"\n") {
			t.Errorf("shallow multiline value %q must reset the preceding nested field's continuation indent", value)
		}
	}
	for line := range strings.SplitSeq(text, "\n") {
		if displayWidth(line) > 24 {
			t.Errorf("nested continuation exceeds width 24: %q", line)
		}
	}
}

func TestStyleConsistencyRichMLOOwnedRoles(t *testing.T) {
	identity := "password=Guest 東京e\u0301👩\u200d🔬"
	elements := []*controlpb.WifiInformationElement{detailedEHTMultiLinkTestIE(), rnrTestIE(), multipleBSSIDTestIE(), rmEnabledCapabilitiesTestIE()}
	security := &controlpb.WifiSecurityDetails{RsnPresent: true, RsnVersion: 1, Warnings: []string{"decode: key=0 password=qa-rich-secret"}}
	fields := []*controlpb.DiagnosticField{{Key: "identity.state", Value: "available"}, {Key: "ap_mlo_link_id.state", Value: "available"}}
	connection := &controlpb.WifiConnection{Ssid: identity, Bssid: "02:00:00:00:00:02", ApMldMacAddress: "02:00:00:00:00:01", WifiStandard: "802.11be", InformationElements: elements, SecurityDetails: security, ObservationFields: fields, Raw: "RAW-ONLY-STYLE-MARKER"}
	ap := &controlpb.WifiScanResult{Ssid: identity, Bssid: connection.Bssid, ApMldMacAddress: connection.ApMldMacAddress, WifiStandard: "802.11be", InformationElements: elements, SecurityDetails: security, ObservationFields: fields}
	diagnostics := &controlpb.WifiDiagnostics{Status: &controlpb.WifiStatus{Enabled: true, Connection: connection}, Scan: &controlpb.WifiScan{Results: []*controlpb.WifiScanResult{ap}}}
	result := &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_OK, Payload: &controlpb.CommandResult_WifiDiagnostics{WifiDiagnostics: diagnostics}}
	options := command.Options{WifiRenderMode: command.WifiRenderModeEHT}
	var logical strings.Builder
	renderWifiMLO(&logical, presentationResult(result, options).GetWifiDiagnostics(), options, Presentation{})
	for label, count := range map[string]int{"  Security": 2, "  Roaming": 2, "Elements": 2, "RNR": 1, "Multiple BSSID": 1, "Identity": 1, "Radio": 1, "Information elements": 1, "SDK flags": 1} {
		if got := strings.Count("\n"+logical.String(), "\n"+label+"\t"); got != count {
			t.Errorf("owned %s fields: got %d, want %d", label, got, count)
		}
	}
	for _, width := range []int{0, 92} {
		text, err := CommandResult("acquired", result, options, pipeline.FormatText, Presentation{Width: width})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(text, "qa-rich-secret") || strings.Contains(text, "RAW-ONLY-STYLE-MARKER") || !strings.Contains(text, identity) {
			t.Error("rich MLO lost typed identity or crossed a privacy/raw boundary")
		}
		for line := range strings.SplitSeq(text, "\n") {
			if width > 0 && displayWidth(line) > width {
				t.Errorf("rich MLO display exceeds %d: %q", width, line)
			}
		}
	}
	var warnings strings.Builder
	renderWifiMLODiagnostics(&warnings, diagnostics.Status, nil, diagnostics.Scan, connection, []*controlpb.WifiScanResult{ap})
	styleField(t, LayoutText(warnings.String(), Presentation{}), "Warnings", "none", 2)
}

func TestStyleConsistencyCommandErrorRecords(t *testing.T) {
	text, err := CommandError("東京e\u0301👩\u200d🔬", "id", fmt.Errorf("partial: 2001:db8::1"), pipeline.FormatText, true)
	if err != nil {
		t.Fatal(err)
	}
	a := styleField(t, text, "Agent", "東京e\u0301👩\u200d🔬", 2)
	b := styleField(t, text, "Error", "partial: 2001:db8::1", 2)
	if a != b {
		t.Error("dispatch-error values must share a column")
	}
}

func TestStyleConsistencyMissingAcquisitionRecord(t *testing.T) {
	text, err := OperationResult("self", harness.OperationResult{}, pipeline.FormatText, Presentation{})
	if err != nil {
		t.Fatal(err)
	}
	styleField(t, text, "Acquisition", "? (aggregate typed payload unavailable)", 2)
}

func TestStyleConsistencySummaryDynamicLabelColumns(t *testing.T) {
	label := strings.Repeat("受信状態", 5)
	result := &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_OK, Payload: &controlpb.CommandResult_WifiStatus{WifiStatus: &controlpb.WifiStatus{Permissions: []string{"A=0", label + "=false"}}}}
	for _, width := range []int{0, 120} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			text, err := CommandResult("self", result, command.Options{WifiRenderMode: command.WifiRenderModeStatus}, pipeline.FormatText, Presentation{Width: width, Interactive: true})
			if err != nil {
				t.Fatal(err)
			}
			if styleField(t, text, "A", "0", 2) != styleField(t, text, label, "false", 2) {
				t.Error("summary values must align even for long CJK labels")
			}
		})
	}
}

func TestStyleConsistencyMLOCompositeField(t *testing.T) {
	var b strings.Builder
	renderWifiMLOHEDetails(&b, "connection", &controlpb.WifiHeCapabilities{PpeThresholdsPresent: true, PpeThresholdsHex: "00"}, nil, &controlpb.WifiHeUoraParameterSet{}, nil)
	text := wrapPresentation(b.String(), 120)
	a := styleField(t, text, "he_ppe", "nss=0 ru=<none> hex=0x00", 4)
	c := styleField(t, text, "he_uora", "eocw_min=0 eocw_max=0", 4)
	if a != c {
		t.Error("HE composite values must align inside their group")
	}
}

func TestStyleConsistencyMLOWarningList(t *testing.T) {
	var b strings.Builder
	renderWifiMLODiagnostics(&b, &controlpb.WifiStatus{}, nil, &controlpb.WifiScan{}, nil, nil)
	text := wrapPresentation(b.String(), 120)
	for _, token := range []string{"wifi_disabled", "eht_scan_results=0"} {
		owned := false
		for line := range strings.SplitSeq(text, "\n") {
			if strings.Contains(line, token) && (strings.HasPrefix(line, "  - ") || strings.HasPrefix(line, "  * ") || strings.HasPrefix(line, "  Warning:") || strings.HasPrefix(line, "  Warnings:")) {
				owned = true
			}
			if token == "eht_scan_results=0" && strings.HasPrefix(line, "  eht_scan_results:") && strings.TrimSpace(strings.SplitN(line, ":", 2)[1]) == "0" {
				owned = true
			}
			if token == "wifi_disabled" && strings.HasPrefix(line, "  wifi_disabled:") && (strings.TrimSpace(strings.SplitN(line, ":", 2)[1]) == "true" || strings.TrimSpace(strings.SplitN(line, ":", 2)[1]) == "yes") {
				owned = true
			}
		}
		if !owned {
			t.Errorf("generated warning %q needs an owned field or a consistent bullet list", token)
		}
	}
}

func TestStyleConsistencyHEOperationColoringFixture(t *testing.T) {
	identity := "password=Guest 東京e\u0301👩\u200d🔬"
	flags := []string{"future_" + strings.Repeat("東京e\u0301👩\u200d🔬", 20), "tail_flag"}
	operation := &controlpb.WifiHeOperation{BasicMcsNssSetHex: "0000", Flags: flags, Truncated: true, Warnings: []string{"decode: key=0 password=qa-style-secret"}}
	connection := &controlpb.WifiConnection{Ssid: identity, HeOperation: operation, HeSpatialReuseParameterSet: &controlpb.WifiHeSpatialReuseParameterSet{}, ObservationFields: []*controlpb.DiagnosticField{{Key: "identity.state", Value: "available"}}}
	result := &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_OK, Payload: &controlpb.CommandResult_WifiDiagnostics{WifiDiagnostics: &controlpb.WifiDiagnostics{Status: &controlpb.WifiStatus{Enabled: true, Connection: connection}}}}
	text, err := CommandResult("self", result, command.Options{WifiRenderMode: command.WifiRenderModeEHT}, pipeline.FormatText, Presentation{})
	if err != nil {
		t.Fatal(err)
	}
	styleField(t, text, "ssid", identity, 2)
	for _, rows := range [][][2]string{
		{{"he_oper", "params=0x0 basic_mcs_nss=0x0000 bss_color=0 disabled=false"}, {"he_oper flags", strings.Join(flags, ",")}, {"he_oper_truncated", "true"}, {"he_oper_warnings", "decode: key=0 password=<redacted>"}},
		{{"he_operation", "bss_color=0 disabled=false partial=false"}, {"he_operation_warnings", "decode: key=0 password=<redacted>"}, {"spatial_reuse", "control=0x0 flags=<none>"}},
	} {
		column := -1
		for _, row := range rows {
			got := styleField(t, text, row[0], row[1], 4)
			if column >= 0 && column != got {
				t.Errorf("HE group value columns differ: %d != %d", column, got)
			}
			column = got
		}
	}
	if strings.Contains(text, "qa-style-secret") {
		t.Error("HE warning exposed a credential")
	}
}

func TestStyleConsistencyEHTOperationPuncturingFixture(t *testing.T) {
	identity := "password=Guest 東京e\u0301👩\u200d🔬"
	flags := []string{"future_" + strings.Repeat("東京e\u0301👩\u200d🔬", 20), "tail_flag"}
	operation := &controlpb.WifiEhtOperation{DisabledSubchannelBitmapPresent: true, DisabledSubchannelBitmapHex: "0000", Flags: flags, Warnings: []string{"decode: key=0 password=qa-style-secret"}}
	connection := &controlpb.WifiConnection{Ssid: identity, EhtOperation: operation, HeCapabilities: &controlpb.WifiHeCapabilities{Mac: &controlpb.WifiHeMacCapabilities{}}, ObservationFields: []*controlpb.DiagnosticField{{Key: "identity.state", Value: "available"}}}
	result := &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_OK, Payload: &controlpb.CommandResult_WifiDiagnostics{WifiDiagnostics: &controlpb.WifiDiagnostics{Status: &controlpb.WifiStatus{Enabled: true, Connection: connection}}}}
	text, err := CommandResult("self", result, command.Options{WifiRenderMode: command.WifiRenderModeEHT}, pipeline.FormatText, Presentation{})
	if err != nil {
		t.Fatal(err)
	}
	styleField(t, text, "ssid", identity, 2)
	for _, rows := range [][][2]string{
		{{"oper", "op_info=false width=<unknown> width_mhz=0 code=0"}, {"oper channels", "ccfs0=0 ccfs1=0 mcs15_disabled=false group_bu_exp=0"}, {"oper flags", strings.Join(flags, ",")}, {"oper disabled", "disabled=0x0000 punctured=<none>"}, {"oper_warnings", "decode: key=0 password=<redacted>"}},
		{{"he_preamble_puncturing_rx", "<unknown>"}, {"he_punctured_sounding", "false"}, {"eht_disabled_subchannel_bitmap", "0x0000 punctured=<none>"}},
	} {
		column := -1
		for _, row := range rows {
			got := styleField(t, text, row[0], row[1], 4)
			if column >= 0 && column != got {
				t.Errorf("EHT group value columns differ: %d != %d", column, got)
			}
			column = got
		}
	}
	if strings.Contains(text, "qa-style-secret") {
		t.Error("EHT warning exposed a credential")
	}
}

func TestStyleConsistencyRawDebugAndAvailability(t *testing.T) {
	result := &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_CANCELED, Message: "partial: reference", Payload: &controlpb.CommandResult_WifiStatus{WifiStatus: &controlpb.WifiStatus{Connection: &controlpb.WifiConnection{Raw: "native foo=0: 2001:db8::1 password=qa-synthetic-secret"}}}}
	text, err := CommandResult("acquired", result, command.Options{RawView: "wifi info"}, pipeline.FormatText, Presentation{Width: 120})
	if err != nil {
		t.Fatal(err)
	}
	styleField(t, text, "Source", "android", 2)
	styleField(t, text, "Target", "acquired", 2)
	if strings.Contains(text, "qa-synthetic-secret") || !strings.Contains(text, "native foo=0: 2001:db8::1") || !strings.Contains(text, "CANCELED") {
		t.Fatal("raw reference syntax, safety or outcome changed")
	}
	fields := []*controlpb.DiagnosticField{{Key: "field.state", Value: "available"}}
	for _, value := range []string{"0", "false"} {
		if observedValue(fields, "field", value) != value {
			t.Errorf("lost observed %s", value)
		}
	}
	if observedValue(fields, "field", "") != "none" || !strings.HasPrefix(observedValue(nil, "field", "0"), "?") {
		t.Fatal("availability was inferred from scalar content")
	}
}

func TestStyleConsistencyResultRouteMatrix(t *testing.T) {
	ssid := "password=Guest 東京e\u0301👩\u200d🔬"
	results := []*controlpb.CommandResult{
		{Payload: &controlpb.CommandResult_WifiStatus{WifiStatus: &controlpb.WifiStatus{Connection: &controlpb.WifiConnection{Ssid: ssid}}}},
		{Payload: &controlpb.CommandResult_WifiCapabilities{WifiCapabilities: &controlpb.WifiCapabilities{SupportedFeatures: []string{"mlo"}}}},
		{Payload: &controlpb.CommandResult_WifiScan{WifiScan: &controlpb.WifiScan{Results: []*controlpb.WifiScanResult{{Ssid: ssid, ObservationAgeMs: new(uint64)}}}}},
		{Payload: &controlpb.CommandResult_WifiDiagnostics{WifiDiagnostics: &controlpb.WifiDiagnostics{Status: &controlpb.WifiStatus{}, Scan: &controlpb.WifiScan{}}}},
		{Payload: &controlpb.CommandResult_Ping{Ping: &controlpb.PingResult{Host: "2001:db8::1", Output: "native key=0: reference"}}},
		{Payload: &controlpb.CommandResult_HttpCheck{HttpCheck: &controlpb.HttpCheckResult{Url: "https://example.test/a:b?ordinary=x=y&token=qa-synthetic-secret"}}},
		{Payload: &controlpb.CommandResult_WifiAssert{WifiAssert: &controlpb.WifiAssertResult{}}},
		{Payload: &controlpb.CommandResult_WifiMonitor{WifiMonitor: &controlpb.WifiMonitorResult{}}},
		{Payload: &controlpb.CommandResult_WifiCycle{WifiCycle: &controlpb.WifiCycleResult{}}},
	}
	for i, result := range results {
		for _, status := range []controlpb.CommandResult_Status{controlpb.CommandResult_STATUS_OK, controlpb.CommandResult_STATUS_FAILED, controlpb.CommandResult_STATUS_CANCELED} {
			for _, width := range []int{0, 8, 24, 40, 80, 120} {
				for _, interactive := range []bool{false, true} {
					t.Run(fmt.Sprintf("%d/%s/%d/interactive=%t", i, status, width, interactive), func(t *testing.T) {
						result.Status = status
						result.Message = "partial acquisition\nfree text: key=0"
						text, err := CommandResult("acquired-device", result, command.Options{}, pipeline.FormatText, Presentation{Width: width, Interactive: interactive, Detail: true})
						if err != nil || text == "" || strings.Contains(text, "qa-synthetic-secret") {
							t.Fatalf("safe route failed: %v", err)
						}
						if width == 0 || width >= 80 {
							a := styleField(t, text, "Source", "android", 2)
							b := styleField(t, text, "Target", "acquired-device", 2)
							if a != b {
								t.Error("metadata value columns differ")
							}
						}
						encoded, err := CommandResult("acquired-device", result, command.Options{}, pipeline.FormatJSON)
						var object map[string]json.RawMessage
						if err != nil || json.Unmarshal([]byte(encoded), &object) != nil {
							t.Fatalf("invalid proto JSON: %v", err)
						}
						for _, key := range []string{"source", "target", "style", "parts"} {
							if _, ok := object[key]; ok {
								t.Errorf("style key %s leaked into proto JSON", key)
							}
						}
					})
				}
			}
		}
	}
}
