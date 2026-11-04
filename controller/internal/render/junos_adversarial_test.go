package render

import (
	"strings"
	"testing"
	"unicode"

	"dropcheck/controller/internal/command"
	"dropcheck/controller/internal/controlpb"
	"dropcheck/controller/internal/pipeline"
	"google.golang.org/protobuf/proto"
)

func TestWifiStatusFullKeepsStructuredEvidenceWithoutRawDumps(t *testing.T) {
	link := &controlpb.MloLinkInfo{LinkId: 3, Raw: "link-raw-marker", ObservationFields: []*controlpb.DiagnosticField{{Key: "identity.state", Value: "available"}}}
	ie := &controlpb.WifiInformationElement{Id: 127, ByteCount: 3, BytesHex: "000008"}
	conn := &controlpb.WifiConnection{Ssid: "password=Guest", NetworkId: 4321, WifiStandard: "802.11be", FrequencyMhz: 6115, ChannelWidth: "80MHz", RssiDbm: -55, TxLinkSpeedMbps: 1200, RxLinkSpeedMbps: 900,
		SecurityDetails: &controlpb.WifiSecurityDetails{RsnPresent: true, PmfCapable: true, PmfRequired: true}, Raw: "wifi-raw-marker password=qa-status-secret", InformationElements: []*controlpb.WifiInformationElement{ie},
		AffiliatedMloLinks: []*controlpb.MloLinkInfo{link}, AssociatedMloLinks: []*controlpb.MloLinkInfo{link},
		ObservationFields: []*controlpb.DiagnosticField{{Key: "identity.state", Value: "available"}, {Key: "identity.source", Value: "typed-source-marker"}, {Key: "rssi.state", Value: "available"}, {Key: "tx_link_speed_mbps.state", Value: "available"}, {Key: "rx_link_speed_mbps.state", Value: "available"}}}
	result := &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_FAILED, Message: "collection incomplete", Payload: &controlpb.CommandResult_WifiDiagnostics{WifiDiagnostics: &controlpb.WifiDiagnostics{
		Status: &controlpb.WifiStatus{Connection: conn, IpStatus: &controlpb.IpStatus{NetworkId: "24", RawLinkProperties: "ip-link-raw-marker", RawCapabilities: "ip-caps-raw-marker",
			Addresses: []string{"192.0.2.2/24", "2001:db8::2/64"}, Routes: []string{"0.0.0.0/0 -> 192.0.2.1 wlan0"}, DnsServers: []string{"192.0.2.53"},
			ObservationFields: []*controlpb.DiagnosticField{{Key: "link_properties.state", Value: "available"}},
			Wifi:              &controlpb.WifiConnection{NetworkId: 6543, Raw: "ip-wifi-raw-marker", InformationElements: []*controlpb.WifiInformationElement{ie}, AssociatedMloLinks: []*controlpb.MloLinkInfo{link}}}},
		Capabilities: &controlpb.WifiCapabilities{Errors: []string{"device-error-marker"}},
		Scan: &controlpb.WifiScan{Errors: []string{"scan-error-marker"}, Results: []*controlpb.WifiScanResult{{Ssid: "AP", VenueName: "venue-marker", Raw: "scan-raw-marker token=qa-status-secret",
			InformationElements: []*controlpb.WifiInformationElement{ie}, AffiliatedMloLinks: []*controlpb.MloLinkInfo{link}}}},
		Networks: []*controlpb.NetworkDiagnostics{{NetworkId: "42", IpStatus: &controlpb.IpStatus{NetworkId: "42", RawLinkProperties: "network-link-raw-marker", RawCapabilities: "network-caps-raw-marker",
			Wifi: &controlpb.WifiConnection{NetworkId: 8765, Raw: "network-wifi-raw-marker", InformationElements: []*controlpb.WifiInformationElement{ie}, AffiliatedMloLinks: []*controlpb.MloLinkInfo{link}}}}},
	}}}
	// Bulk metadata and decoded IE detail must not turn the overview into a dump.
	for range 300 {
		result.GetWifiDiagnostics().Status.ObservationFields = append(result.GetWifiDiagnostics().Status.ObservationFields, &controlpb.DiagnosticField{Key: "detail-only-metadata", Value: "metadata-marker"})
	}
	before := proto.Clone(result)
	for _, full := range []bool{false, true} {
		mode := command.WifiRenderModeStatus
		input := &controlpb.CommandResult{Status: result.Status, Message: result.Message, Payload: &controlpb.CommandResult_WifiStatus{WifiStatus: result.GetWifiDiagnostics().Status}}
		if full {
			mode = command.WifiRenderModeFull
			input = result
		}
		for _, width := range []int{0, 24, 40, 80, 120} {
			out, err := CommandResult("test", input, command.Options{Detail: full, WifiRenderMode: mode}, pipeline.FormatText, Presentation{Interactive: true, Detail: full, Width: width})
			if err != nil {
				t.Fatal(err)
			}
			compact := strings.Map(func(r rune) rune {
				if unicode.IsSpace(r) {
					return -1
				}
				return r
			}, out)
			for _, value := range []string{"password=Guest", "4321", "11v_bss_transition", "-55dBm", "1200Mbps", "900Mbps", "capable=truerequired=true", "192.0.2.2/24", "2001:db8::2/64", "192.0.2.1", "192.0.2.53", "FAILED", "collectionincomplete"} {
				if !strings.Contains(compact, value) {
					t.Errorf("full=%t width=%d lost structured value %s", full, width, value)
				}
			}
			for _, value := range []string{"6543", "8765", "venue-marker", "typed-source-marker", "device-error-marker", "scan-error-marker", "EHT/MLOinterpretation", "metadata-marker"} {
				if strings.Contains(compact, value) != full {
					t.Errorf("full=%t width=%d wrong detail visibility for %s", full, width, value)
				}
			}
			if strings.Contains(compact, "000008") != full {
				t.Error("structured IE hex visibility changed")
			}
			for _, value := range []string{"wifi-raw-marker", "link-raw-marker", "ip-link-raw-marker", "ip-caps-raw-marker", "ip-wifi-raw-marker", "scan-raw-marker", "network-link-raw-marker", "network-caps-raw-marker", "network-wifi-raw-marker", "RawWifiInforeference", "RawScanResultreference", "RawMloLinkreference", "RawLinkProperties", "Rawcapabilities"} {
				if strings.Contains(compact, value) {
					t.Errorf("full=%t width=%d wrong raw visibility for %s", full, width, value)
				}
			}
			if strings.Contains(compact, "qa-status-secret") || !proto.Equal(result, before) {
				t.Fatal("raw visibility leaked credentials or mutated the acquired payload")
			}
			if !full && (width == 0 || width >= 80) {
				lines := strings.Count(out, "\n")
				t.Logf("summary width=%d lines=%d", width, lines)
				if lines > 320 || strings.Contains(out, "show wifi status detail") {
					t.Fatal("summary exceeded its normal output target or advertised obsolete syntax")
				}
			}
		}
	}
}

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
			if mode == command.WifiRenderModeEHT {
				if !strings.Contains(compact, "ConnectedMLO") || strings.Contains(compact, "network-marker") || strings.Contains(compact, "RawWifiInfo") {
					t.Fatal("EHT view is not focused on EHT/MLO evidence")
				}
				continue
			}
			for _, value := range []string{"4321", "5678", "8765", "7890", "a1b2c3", "venue-marker", "operator-marker", "password=Guest", "EHT/MLOinterpretation"} {
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
	if err != nil || !strings.Contains(out, "5432") || strings.Contains(out, "assert-marker") || strings.Contains(out, "qa-hidden-secret") {
		t.Fatal("historical observation lost or unsafe", err)
	}
}
