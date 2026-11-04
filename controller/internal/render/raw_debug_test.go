package render

import (
	"strings"
	"testing"

	"dropcheck/controller/internal/command"
	"dropcheck/controller/internal/controlpb"
	"dropcheck/controller/internal/pipeline"
	"google.golang.org/protobuf/proto"
)

func TestRawDebugOnlyShowsRequestedDumpsAndPreservesFailureAndPrivacy(t *testing.T) {
	link := &controlpb.MloLinkInfo{Raw: "LINK-RAW zero=0 flag=false"}
	conn := &controlpb.WifiConnection{Ssid: "structured-only", Raw: "WIFI-RAW password=debug-secret\nsecond-line", AssociatedMloLinks: []*controlpb.MloLinkInfo{link}, InformationElements: []*controlpb.WifiInformationElement{{BytesHex: "aabbcc"}}}
	ip := &controlpb.IpStatus{NetworkId: "42", Mtu: 1500, RawCapabilities: "CAPS-RAW token=debug-secret", RawLinkProperties: "PROPERTIES-RAW", Wifi: conn}
	for _, tc := range []struct {
		view         string
		result       *controlpb.CommandResult
		want, absent string
	}{
		{"wifi info", &controlpb.CommandResult{Payload: &controlpb.CommandResult_WifiStatus{WifiStatus: &controlpb.WifiStatus{Connection: conn}}}, "WIFI-RAW", "CAPS-RAW"},
		{"wifi scan", &controlpb.CommandResult{Payload: &controlpb.CommandResult_WifiScan{WifiScan: &controlpb.WifiScan{Errors: []string{"scan acquisition incomplete"}, Results: []*controlpb.WifiScanResult{
			{Ssid: "password=Guest", Raw: "SCAN-RAW token=debug-secret", AffiliatedMloLinks: []*controlpb.MloLinkInfo{link}}, {Ssid: "other", Raw: "WRONG-AP-RAW"},
		}}}}, "SCAN-RAW", "WRONG-AP-RAW"},
		{"network capabilities", &controlpb.CommandResult{Payload: &controlpb.CommandResult_NetworkSnapshot{NetworkSnapshot: &controlpb.NetworkSnapshot{Networks: []*controlpb.NetworkDiagnostics{{NetworkId: "42", IpStatus: ip}}}}}, "CAPS-RAW", "PROPERTIES-RAW"},
		{"network wifi-info", &controlpb.CommandResult{Payload: &controlpb.CommandResult_NetworkSnapshot{NetworkSnapshot: &controlpb.NetworkSnapshot{Networks: []*controlpb.NetworkDiagnostics{{NetworkId: "42", IpStatus: ip}}}}}, "WIFI-RAW", "CAPS-RAW"},
		{"network link-properties", &controlpb.CommandResult{Payload: &controlpb.CommandResult_NetworkSnapshot{NetworkSnapshot: &controlpb.NetworkSnapshot{Networks: []*controlpb.NetworkDiagnostics{{NetworkId: "42", IpStatus: ip}}}}}, "PROPERTIES-RAW", "CAPS-RAW"},
	} {
		t.Run(tc.view, func(t *testing.T) {
			tc.result.Status = controlpb.CommandResult_STATUS_FAILED
			tc.result.Message = "acquisition incomplete"
			before := proto.Clone(tc.result)
			for _, format := range []pipeline.Format{pipeline.FormatText, pipeline.FormatJSON} {
				out, err := CommandResult("target", tc.result, command.Options{RawView: tc.view, RawNetworkID: "42", WifiScanSSID: "password=Guest"}, format)
				if err != nil || !strings.Contains(out, tc.want) || !strings.Contains(strings.ToLower(out), "failed") || !strings.Contains(out, "acquisition incomplete") {
					t.Fatalf("raw evidence or outcome lost: %q %v", out, err)
				}
				for _, absent := range []string{tc.absent, "debug-secret", "structured-only", "aabbcc", "MTU:", "1500", "\x1b"} {
					if strings.Contains(out, absent) {
						t.Fatalf("raw projection leaked/unexpected field %q: %s", absent, out)
					}
				}
				if !proto.Equal(tc.result, before) {
					t.Fatal("debug modified the captured payload")
				}
			}
		})
	}
}

func TestRawDebugRejectsAbsentWrongAndMismatchedPayloads(t *testing.T) {
	for _, result := range []*controlpb.CommandResult{
		{},
		{Payload: &controlpb.CommandResult_NetworkSnapshot{NetworkSnapshot: &controlpb.NetworkSnapshot{}}},
		{Payload: &controlpb.CommandResult_NetworkSnapshot{NetworkSnapshot: &controlpb.NetworkSnapshot{Networks: []*controlpb.NetworkDiagnostics{{NetworkId: "43", IpStatus: &controlpb.IpStatus{NetworkId: "43", RawCapabilities: "WRONG-RAW"}}}}}},
		{Payload: &controlpb.CommandResult_NetworkSnapshot{NetworkSnapshot: &controlpb.NetworkSnapshot{Networks: []*controlpb.NetworkDiagnostics{{NetworkId: "42", IpStatus: &controlpb.IpStatus{NetworkId: "43", RawCapabilities: "WRONG-RAW"}}}}}},
		{Payload: &controlpb.CommandResult_NetworkSnapshot{NetworkSnapshot: &controlpb.NetworkSnapshot{Networks: []*controlpb.NetworkDiagnostics{{NetworkId: "42", IpStatus: &controlpb.IpStatus{NetworkId: "42"}}}}}},
	} {
		out, err := CommandResult("target", result, command.Options{RawView: "network capabilities", RawNetworkID: "42"}, pipeline.FormatText)
		if err == nil || out != "" {
			t.Fatal("missing/mismatched raw capture was accepted", out, err)
		}
	}
}

func TestStructuredJSONOmitsNestedObjectDumpsButKeepsIEAndSecurityHex(t *testing.T) {
	conn := &controlpb.WifiConnection{Raw: "OBJECT-RAW", InformationElements: []*controlpb.WifiInformationElement{{Id: 221, BytesHex: "aabbcc"}},
		SecurityDetails: &controlpb.WifiSecurityDetails{RawRsnHex: "112233"}, AssociatedMloLinks: []*controlpb.MloLinkInfo{{Raw: "LINK-RAW"}},
		HeCapabilities: &controlpb.WifiHeCapabilities{McsNss: []*controlpb.WifiMcsNssSupport{{Raw: 7}}}}
	result := &controlpb.CommandResult{Payload: &controlpb.CommandResult_WifiDiagnostics{WifiDiagnostics: &controlpb.WifiDiagnostics{
		Status: &controlpb.WifiStatus{Connection: conn, IpStatus: &controlpb.IpStatus{RawCapabilities: "CAPS-RAW", RawLinkProperties: "PROPERTIES-RAW", Wifi: conn}},
		Scan:   &controlpb.WifiScan{Results: []*controlpb.WifiScanResult{{Raw: "SCAN-RAW", InformationElements: conn.InformationElements}}},
	}}}
	before := proto.Clone(result)
	plain, err := CommandResult("target", result, command.Options{}, pipeline.FormatJSON)
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := CommandResultEnvelope("target", "command", result)
	if err != nil {
		t.Fatal(err)
	}
	for _, out := range []string{plain, envelope} {
		for _, absent := range []string{"OBJECT-RAW", "LINK-RAW", "CAPS-RAW", "PROPERTIES-RAW", "SCAN-RAW", `"raw_capabilities"`, `"raw_link_properties"`} {
			if strings.Contains(out, absent) {
				t.Fatalf("structured JSON retained %s", absent)
			}
		}
		for _, kept := range []string{"aabbcc", "112233", "raw_rsn_hex"} {
			if !strings.Contains(out, kept) {
				t.Fatalf("typed hex evidence removed: %s", kept)
			}
		}
	}
	if structuredResult(result).GetWifiDiagnostics().GetStatus().GetConnection().GetHeCapabilities().GetMcsNss()[0].GetRaw() != 7 {
		t.Fatal("numeric raw bit representation was mistaken for an object string dump")
	}
	if !proto.Equal(result, before) {
		t.Fatal("structured projection modified source data")
	}
}
