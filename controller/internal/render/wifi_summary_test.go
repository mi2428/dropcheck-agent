package render

import (
	"strings"
	"testing"
	"unicode"

	"dropcheck/controller/internal/command"
	"dropcheck/controller/internal/controlpb"
	"dropcheck/controller/internal/pipeline"
)

func TestWifiSummaryHierarchyAndAlignment(t *testing.T) {
	status := &controlpb.WifiStatus{Enabled: true, State: "enabled", Connection: &controlpb.WifiConnection{
		Ssid: "password=Guest 東京", Bssid: "02:00:00:11:22:33", NetworkId: 42, FrequencyMhz: 6115,
		ObservationFields: []*controlpb.DiagnosticField{{Key: "identity.state", Value: "available"}},
		HeCapabilities:    &controlpb.WifiHeCapabilities{Features: []string{"he-feature-marker"}},
	}, IpStatus: &controlpb.IpStatus{NetworkId: "24", Mtu: 1500, Domains: "example.test", HttpProxy: "proxy-marker",
		Capabilities: []string{"INTERNET", "VALIDATED"}, Ipv6Ra: []*controlpb.DiagnosticField{{Key: "accept_ra", Value: "2"}},
		ObservationFields: []*controlpb.DiagnosticField{{Key: "link_properties.state", Value: "available"}, {Key: "capabilities.state", Value: "available"}},
	}}
	result := &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_OK, Payload: &controlpb.CommandResult_WifiStatus{WifiStatus: status}}
	for _, width := range []int{0, 80, 120} {
		out, err := CommandResult("fixture", result, command.Options{WifiRenderMode: command.WifiRenderModeStatus}, pipeline.FormatText, Presentation{Interactive: true, Width: width})
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"\nConnection\n", "\n  State\n", "\n  Identity\n", "\n  Radio\n", "\n  HE/EHT (decoded)\n", "\n  Network capabilities\n", "\n  IPv6 RA (Android)\n", "password=Guest 東京", "he-feature-marker", "proxy-marker"} {
			if !strings.Contains(out, want) {
				t.Errorf("width=%d missing %q", width, want)
			}
		}
		for line := range strings.SplitSeq(out, "\n") {
			indent := len(line) - len(strings.TrimLeft(line, " "))
			if colon := strings.Index(line, ":"); indent >= 4 && indent < 28 && colon > indent {
				value := colon + 1 + len(line[colon+1:]) - len(strings.TrimLeft(line[colon+1:], " "))
				if line[colon-1] == ' ' || colon < 29 && value < len(line) && value != 30 {
					t.Errorf("width=%d detached colon or unaligned value: %q", width, line)
				}
			}
		}
		lines := strings.Count(out, "\n")
		t.Logf("summary width=%d lines=%d", width, lines)
		if lines > 105 {
			t.Fatalf("unexpected summary scope: %d lines", lines)
		}
		if strings.Contains(out, "\n\n  ") {
			t.Fatal("subsections have unnecessary blank separators")
		}
	}
}

func TestWifiSummaryContinuationKeepsValuesAndIndentation(t *testing.T) {
	value := strings.Repeat("東京e\u0301👩\u200d🔬", 8)
	var out strings.Builder
	writeWifiSummaryGroup(&out, "  Identity", Presentation{Width: 60}, kv("SSID", value), kv("DNS", "192.0.2.53\n2001:db8::53"))
	for _, line := range strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")[1:] {
		if displayWidth(line) > 60 {
			t.Fatalf("line overflow: %q", line)
		}
		if !strings.HasPrefix(line, "    SSID:") && !strings.HasPrefix(line, "    DNS:") && !strings.HasPrefix(line, strings.Repeat(" ", 30)) {
			t.Fatalf("continuation escaped its value column: %q", line)
		}
	}
	compact := strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, out.String())
	for _, want := range []string{value, "192.0.2.53", "2001:db8::53"} {
		if !strings.Contains(compact, want) {
			t.Fatalf("value clipped: %q", want)
		}
	}
}

func TestWifiSummaryCompactRowsRetainTheirComponents(t *testing.T) {
	status := &controlpb.WifiStatus{Enabled: true, State: "enabled", ActiveNetwork: "949", WifiNetworkCount: 2, Connection: &controlpb.WifiConnection{
		Ssid: "Lab", NetworkId: 42, SubscriptionId: 7, HiddenSsid: true, Restricted: false, ApplicableRedactions: "3",
		PasspointFqdn: "passpoint.example.test", PasspointProviderFriendlyName: "Provider", PasspointUniqueId: "unique-marker",
		FrequencyMhz: 6115, ChannelWidth: "80MHz", RssiDbm: -55, TxLinkSpeedMbps: 1200, RxLinkSpeedMbps: 900, LinkSpeedMbps: 700,
		ObservationFields: []*controlpb.DiagnosticField{{Key: "identity.state", Value: "available"}, {Key: "rssi.state", Value: "available"}, {Key: "tx_link_speed_mbps.state", Value: "available"}, {Key: "rx_link_speed_mbps.state", Value: "available"}},
	}, IpStatus: &controlpb.IpStatus{NetworkId: "24", Internet: true, Validated: false, PrivateDnsActive: true, PrivateDnsServerName: "resolver.example.test",
		OwnerUid: 1001, EnterpriseIds: []string{"alpha", "beta"}, SubscriptionIds: []int32{7, 8},
		ObservationFields: []*controlpb.DiagnosticField{{Key: "capabilities.state", Value: "available"}, {Key: "link_properties.state", Value: "available"}, {Key: "default_network.state", Value: "available"}, {Key: "default_network_id", Value: "24"}, {Key: "selected_is_default", Value: "true"}},
	}}
	var out strings.Builder
	renderWifiSummary(&out, status, Presentation{Interactive: true})
	for _, want := range []string{"framework=enabled", "active=949; Wi-Fi=2", "network-id=42; subscription-id=7", "hidden=true; restricted=false; redactions=3", "fqdn=passpoint.example.test; provider=Provider; unique-id=unique-marker", "ch=33; width=80MHz; 6115MHz", "-55dBm", "tx=1200Mbps; rx=900Mbps; link=700Mbps", "24; selected=true", "internet=true; validated=false", "active=true; server=resolver.example.test", "uid=1001; enterprise=alpha,beta; subscriptions=7,8"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("compact row lost %q", want)
		}
	}
}
