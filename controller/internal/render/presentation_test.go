package render

import (
	"fmt"
	"strings"
	"testing"

	"dropcheck/controller/internal/command"
	"dropcheck/controller/internal/controlpb"
	"dropcheck/controller/internal/pipeline"
	"google.golang.org/protobuf/proto"
)

func TestPresentationWidthMatrixPreservesSafeIdentity(t *testing.T) {
	ssid := "東京Ｇ e\u0301 👩\u200d🔬 Lab Case"
	macs := []string{"02:00:00:11:22:33", "06:00:00:11:22:33"}
	scan := &controlpb.WifiScan{}
	for i, mac := range macs {
		scan.Results = append(scan.Results, &controlpb.WifiScanResult{Ssid: ssid, Bssid: mac, RssiDbm: int32(-48 - i), Band: "6ghz", FrequencyMhz: 6135, ChannelWidth: "320MHz", WifiStandard: "802.11be", SecurityTypes: []string{"wpa2_psk", "wpa3_sae", "future_Mode"}})
	}
	scan.Fields = []*controlpb.DiagnosticField{{Key: "scan_source", Value: "cached (refresh failed)"}, {Key: "fresh_scan_results_updated", Value: "false"}}
	result := &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_FAILED, ElapsedMs: 9000, Message: "fresh scan timed out; retained APs are reference", Payload: &controlpb.CommandResult_WifiScan{WifiScan: scan}}
	before := proto.Clone(result)
	for _, width := range []int{24, 32, 40, 48, 60, 80, 96, 120} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			out, err := CommandResult("self", result, command.Options{WifiScanBrief: true}, pipeline.FormatText, Presentation{Width: width})
			if err != nil {
				t.Fatal(err)
			}
			for line := range strings.SplitSeq(out, "\n") {
				if displayWidth(line) > width {
					t.Fatalf("width %d overflow: %q", width, line)
				}
			}
			joined := strings.ReplaceAll(out, "\n", "")
			for _, value := range append(macs, "psk+sae+future_Mode", "fresh scan timed out", "👩\u200d🔬", "e\u0301") {
				if !strings.Contains(joined, value) {
					t.Fatalf("missing %q at %d: %s", value, width, out)
				}
			}
			if strings.Index(out, "FAILED") > strings.Index(out, "Source") || strings.Contains(out, "Wi-Fi scan  OK") {
				t.Fatalf("failure not first: %s", out)
			}
		})
	}
	if !proto.Equal(before, result) {
		t.Fatal("presentation changed typed result")
	}
	json, err := CommandResult("self", result, command.Options{}, pipeline.FormatJSON, Presentation{Width: 24})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(json, ssid) || !strings.Contains(json, macs[1]) {
		t.Fatalf("JSON clipped values: %s", json)
	}
}

func TestPresentationIPAndURLWidthMatrix(t *testing.T) {
	ip := "ffff:ffff:ffff:ffff:ffff:ffff:255.255.255.255%long-interface-name/128"
	url := "https://example.test/a/very/long/CaseSensitive/path?ordinary=value"
	results := []*controlpb.CommandResult{
		{Status: controlpb.CommandResult_STATUS_OK, Payload: &controlpb.CommandResult_IpStatus{IpStatus: &controlpb.IpStatus{NetworkId: "105", Addresses: []string{ip}, Routes: []string{"::/0 -> fe80::1%long-interface-name"}, DnsServers: []string{ip}, ObservationFields: []*controlpb.DiagnosticField{{Key: "link_properties.state", Value: "available"}}}}},
		{Status: controlpb.CommandResult_STATUS_FAILED, Message: "observed=0.00049 expected<=0.00048", Payload: &controlpb.CommandResult_HttpCheck{HttpCheck: &controlpb.HttpCheckResult{Url: url, Error: "failed"}}},
	}
	for _, width := range []int{24, 32, 40, 48, 60, 80, 96, 120} {
		for i, result := range results {
			out, err := CommandResult("self", result, command.Options{}, pipeline.FormatText, Presentation{Width: width})
			if err != nil {
				t.Fatal(err)
			}
			for line := range strings.SplitSeq(out, "\n") {
				if displayWidth(line) > width {
					t.Fatalf("overflow at %d: %q", width, line)
				}
			}
			full := strings.ReplaceAll(out, "\n", "")
			value := ip
			if i == 1 {
				value = url
			}
			if !strings.Contains(full, value) {
				t.Fatalf("identity lost at %d: %s", width, out)
			}
			if i == 1 && !strings.Contains(full, "0.00049 expected<=0.00048") {
				t.Fatalf("failure precision lost: %s", out)
			}
		}
	}
}

func TestSafePresentationSanitizesBeforeLayoutAndJSON(t *testing.T) {
	url := "https://user:credential@example.test/Case?token=credential&ordinary=Keep"
	result := &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_FAILED, Message: "password=credential\x1b[31m\x00oops", Payload: &controlpb.CommandResult_HttpCheck{HttpCheck: &controlpb.HttpCheckResult{Url: url}}}
	for _, format := range []pipeline.Format{pipeline.FormatText, pipeline.FormatJSON} {
		out, err := CommandResult("self", result, command.Options{}, format)
		if err != nil {
			t.Fatal(err)
		}
		for _, unsafe := range []string{"credential", "\x1b", "\x00", "\u200b"} {
			if strings.Contains(out, unsafe) {
				t.Fatalf("unsafe output: %q", out)
			}
		}
		if !strings.Contains(out, "ordinary=Keep") {
			t.Fatalf("safe URL query was removed: %s", out)
		}
	}
	if result.GetHttpCheck().GetUrl() != url {
		t.Fatal("redaction changed original typed data")
	}
}

func TestSafePresentationDoesNotRedactNonSecretSSIDText(t *testing.T) {
	ssid := "password=Guest Case"
	fields := []*controlpb.DiagnosticField{{Key: "identity.state", Value: "available"}}
	result := &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_OK, Payload: &controlpb.CommandResult_WifiStatus{WifiStatus: &controlpb.WifiStatus{Connection: &controlpb.WifiConnection{Ssid: ssid, ObservationFields: fields}}}}
	for _, format := range []pipeline.Format{pipeline.FormatText, pipeline.FormatJSON} {
		out, err := CommandResult("self", result, command.Options{}, format)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, ssid) {
			t.Fatalf("non-secret SSID redacted: %s", out)
		}
	}
}

func TestCapabilityListsArePresenceAuthority(t *testing.T) {
	capabilities := &controlpb.WifiCapabilities{SupportedBands: []string{"6ghz"}, UnsupportedBands: []string{"60ghz"}, SupportedFeatures: []string{"future_feature"}}
	var b strings.Builder
	renderDeviceCapabilities(&b, capabilities, Presentation{})
	for _, value := range []string{"6G: yes", "60G: no", "5G: ?", "mlo: ?", "future_feature: yes"} {
		if !strings.Contains(b.String(), value) {
			t.Fatalf("missing %q: %s", value, b.String())
		}
	}
}

func TestObservationMetadataNeverGuessesScalarPresence(t *testing.T) {
	available := []*controlpb.DiagnosticField{{Key: "field.state", Value: "available"}}
	unavailable := []*controlpb.DiagnosticField{{Key: "field.state", Value: "unavailable"}, {Key: "field.reason", Value: "permission denied"}}
	for _, value := range []string{"", "0", "false"} {
		if !strings.HasPrefix(observedValue(nil, "field", value), "?") {
			t.Fatalf("guessed presence for %q", value)
		}
	}
	if observedValue(available, "field", "") != "none" || observedValue(available, "field", "0") != "0" || observedBool(available, "field", false) != "no" {
		t.Fatal("known empty/zero/false conflated")
	}
	if !strings.Contains(observedValue(unavailable, "field", "0"), "permission denied") {
		t.Fatal("missing availability reason")
	}
	age := uint64(0)
	if scanObservationAge(&controlpb.WifiScanResult{ObservationAgeMs: &age}) != "0ms" || !strings.HasPrefix(scanObservationAge(&controlpb.WifiScanResult{}), "?") {
		t.Fatal("age zero and absence conflated")
	}
	if wifiChannelWidth(&controlpb.WifiConnection{Raw: "channelWidth: 80MHz"}) != "" {
		t.Fatal("restored channel width from raw text")
	}
	if wifiConnectionHasMLO(&controlpb.WifiConnection{WifiStandard: "802.11be"}) {
		t.Fatal("inferred established MLO from PHY alone")
	}
}

func testObservationFields(fields ...string) []*controlpb.DiagnosticField {
	values := make([]*controlpb.DiagnosticField, 0, len(fields))
	for _, field := range fields {
		values = append(values, &controlpb.DiagnosticField{Key: field + ".state", Value: "available"})
	}
	return values
}
