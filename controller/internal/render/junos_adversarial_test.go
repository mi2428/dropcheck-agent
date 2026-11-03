package render

import (
	"strings"
	"testing"
	"unicode"

	"dropcheck/controller/internal/command"
	"dropcheck/controller/internal/controlpb"
	"dropcheck/controller/internal/pipeline"
)

func TestJunosQAFullBundleIncludesEvidenceAtEveryWidth(t *testing.T) {
	conn := &controlpb.WifiConnection{Ssid: "password=Guest", NetworkId: 4321, SubscriptionId: 5678, Raw: "wifi-marker password=qa-new-secret", ObservationFields: []*controlpb.DiagnosticField{{Key: "identity.state", Value: "available"}}, InformationElements: []*controlpb.WifiInformationElement{{Id: 221, ByteCount: 3, BytesHex: "a1b2c3"}}}
	raw := &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_OK, Payload: &controlpb.CommandResult_WifiDiagnostics{WifiDiagnostics: &controlpb.WifiDiagnostics{
		Status: &controlpb.WifiStatus{Connection: conn}, Capabilities: &controlpb.WifiCapabilities{},
		Scan:     &controlpb.WifiScan{Results: []*controlpb.WifiScanResult{{Ssid: "AP", Bssid: "aa:bb:cc:dd:ee:ff", VenueName: "venue-marker", OperatorFriendlyName: "operator-marker", Raw: "scan-marker token=qa-new-secret"}}},
		Networks: []*controlpb.NetworkDiagnostics{{NetworkId: "42", IpStatus: &controlpb.IpStatus{NetworkId: "42", Wifi: &controlpb.WifiConnection{NetworkId: 8765, SubscriptionId: 7890, Raw: "network-marker"}}}},
	}}}
	for _, mode := range []string{command.WifiRenderModeFull, command.WifiRenderModeEHT} {
		for _, width := range []int{0, 24, 40, 80, 120} {
			out, err := CommandResult("test", raw, command.Options{Detail: true, WifiRenderMode: mode}, pipeline.FormatText, Presentation{Interactive: true, Detail: true, Width: width})
			if err != nil || strings.Contains(out, "qa-new-secret") {
				t.Fatal(width, err, "credential leak")
			}
			compact := strings.Map(func(r rune) rune {
				if unicode.IsSpace(r) {
					return -1
				}
				return r
			}, out)
			for _, value := range []string{"4321", "5678", "8765", "7890", "network-marker", "a1b2c3", "wifi-marker", "scan-marker", "venue-marker", "operator-marker", "password=Guest", "EHT/MLOinterpretation"} {
				if !strings.Contains(compact, value) {
					t.Errorf("mode=%s width=%d missing=%s", mode, width, value)
				}
			}
		}
	}
}

func TestJunosQAHistoricalWifiTestRetainsItsSnapshot(t *testing.T) {
	result := &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_FAILED, Payload: &controlpb.CommandResult_WifiAssert{WifiAssert: &controlpb.WifiAssertResult{
		Status: &controlpb.WifiStatus{Connection: &controlpb.WifiConnection{NetworkId: 5432, Raw: "assert-marker password=qa-hidden-secret"}},
	}}}
	out, err := CommandResult("target", SanitizedResult(result), command.Options{Detail: true}, pipeline.FormatText, Presentation{Interactive: true, Detail: true})
	if err != nil || !strings.Contains(out, "5432") || !strings.Contains(out, "assert-marker") || strings.Contains(out, "qa-hidden-secret") {
		t.Fatal("historical observation lost or unsafe", err)
	}
}
