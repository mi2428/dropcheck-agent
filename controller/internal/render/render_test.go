package render

import (
	"slices"
	"strings"
	"testing"

	"github.com/mi2428/dropcheck-agent/controller/internal/command"
	"github.com/mi2428/dropcheck-agent/controller/internal/controlpb"
)

func TestIPViewSeparatesAddressRoutingAndDetailGroups(t *testing.T) {
	var out strings.Builder
	renderIPView(&out, &controlpb.IpStatus{
		Addresses:         []string{"2001:db8::1/64"},
		Routes:            []string{"::/0 via fe80::1"},
		DnsServers:        []string{"2001:db8::53"},
		ObservationFields: testObservationFields("link_properties", "capabilities", "default_network"),
	}, Presentation{})
	text := out.String()
	for _, boundary := range []string{
		"Other addresses\n  Addr\tnone\n\nRoutes\t::/0 via fe80::1",
		"Routes\t::/0 via fe80::1\nDNS\t2001:db8::53",
	} {
		if !strings.Contains(text, boundary) {
			t.Fatalf("missing IP semantic boundary %q:\n%s", boundary, text)
		}
	}
	if !strings.Contains(text, "Private DNS\toff; server=none") || !strings.Contains(text, "\n\nCapabilities\tnone") {
		t.Fatalf("routing and capability groups are not separated:\n%s", text)
	}
}

func TestMLOLinkRecordsSeparateSiblingLinks(t *testing.T) {
	var out strings.Builder
	renderMLOLinks(&out, "Associated links", []*controlpb.MloLinkInfo{
		{LinkId: 1, ApMacAddress: "02:00:00:00:00:01"},
		{LinkId: 2, ApMacAddress: "02:00:00:00:00:02"},
	})
	text := out.String()
	if !strings.Contains(text, "STA_MAC\t? (identity: observation metadata unavailable)\n\n  L\t?") || !strings.Contains(text, "Associated links\n  L\t?") {
		t.Fatalf("sibling link boundary or parent indentation missing:\n%s", text)
	}
}

func TestAdaptiveScanSeparatesAPsFromAppendices(t *testing.T) {
	var out strings.Builder
	renderAdaptiveScan(&out, &controlpb.WifiScan{
		Results: []*controlpb.WifiScanResult{{Ssid: "one", Bssid: "02:00:00:00:00:01"}},
		Fields:  []*controlpb.DiagnosticField{{Key: "custom", Value: "field"}},
		Errors:  []string{"scan warning"},
	}, command.Options{}, Presentation{})
	text := out.String()
	for _, boundary := range []string{
		"AP 1\n",
		"Passpoint\tfalse\n\nUnits\t",
		"Units\tRSSI=dBm; BW=MHz; band G=GHz; PHY=802.11\n\nScan fields\n",
		"custom\tfield\n\nErrors\n",
	} {
		if !strings.Contains(text, boundary) {
			t.Fatalf("missing scan appendix/AP boundary %q:\n%s", boundary, text)
		}
	}
}

func TestMLOAffiliatedLinksSeparateAPAndSiblingBlocks(t *testing.T) {
	var out strings.Builder
	result := &controlpb.WifiScanResult{
		Ssid: "lab", Bssid: "02:00:00:00:00:01",
		AffiliatedMloLinks: []*controlpb.MloLinkInfo{{LinkId: 1}, {LinkId: 2}},
	}
	renderWifiMLOScanLinks(&out, []wifiMLOGroup{newWifiMLOGroup([]*controlpb.WifiScanResult{result})}, nil)
	text := out.String()
	for _, boundary := range []string{
		"sdk_flags twt=false 11az_ntb=false ranging_prot=false secure_he_ltf=false 11mc=false\n\n  affiliated_links\n",
		"parent_bssid=02:00:00:00:00:01\n\n",
	} {
		if !strings.Contains(text, boundary) {
			t.Fatalf("missing nested MLO boundary %q:\n%s", boundary, text)
		}
	}
	if strings.Count(text, "[-] affiliated link") != 2 {
		t.Fatalf("missing sibling link blocks:\n%s", text)
	}
}

func TestMLOScanHE6GHzSeparatesAPBlocks(t *testing.T) {
	results := []*controlpb.WifiScanResult{
		{Ssid: "one", Bssid: "02:00:00:00:00:01", He_6GhzCapabilities: &controlpb.WifiHe6GhzCapabilities{}},
		{Ssid: "two", Bssid: "02:00:00:00:00:02", He_6GhzCapabilities: &controlpb.WifiHe6GhzCapabilities{}},
	}
	var out strings.Builder
	renderWifiMLOScanHE6GHzDetails(&out, []wifiMLOGroup{newWifiMLOGroup(results)})
	text := out.String()
	if !strings.Contains(text, "ap ssid=one bssid=02:00:00:00:00:01") || !strings.Contains(text, "\n\n  ap ssid=two bssid=02:00:00:00:00:02") {
		t.Fatalf("scan HE 6GHz AP blocks are not separated:\n%s", text)
	}
}

func TestRenderCommandResultShowsPayloadLatency(t *testing.T) {
	result := &controlpb.CommandResult{
		Status:    controlpb.CommandResult_STATUS_OK,
		ElapsedMs: 99,
		Payload: &controlpb.CommandResult_ResolveDns{
			ResolveDns: &controlpb.ResolveDnsResult{
				Name:      "example.test",
				ElapsedMs: 42,
				Answers: []*controlpb.DnsAnswer{{
					Type:    controlpb.DnsRecordType_DNS_RECORD_TYPE_A,
					Address: "192.0.2.1",
				}},
			},
		},
	}

	out, err := CommandResult("agent", result, command.Options{})
	if err != nil {
		t.Fatalf("renderCommandResult() error = %v", err)
	}
	if !slices.Equal(metadataQAValues(out, "Latency"), []string{"42ms"}) {
		t.Fatalf("rendered output = %q, missing payload latency", out)
	}
}

func TestScanAPDetailsKeepParentAndChildLevels(t *testing.T) {
	result := &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_OK, Payload: &controlpb.CommandResult_WifiScan{WifiScan: &controlpb.WifiScan{
		Results: []*controlpb.WifiScanResult{
			{Bssid: "02:00:00:00:00:01", Ssid: "first", OperatorFriendlyName: "owner-property", ApMloLinkId: 2, SecurityDetails: &controlpb.WifiSecurityDetails{RsnPresent: true, RsnVersion: 1}, InformationElements: []*controlpb.WifiInformationElement{{Id: 48, BytesHex: "0102"}}, ObservationFields: []*controlpb.DiagnosticField{{Key: "ap_mlo_link_id.state", Value: "available"}, {Key: "ap_link_id", Value: "2"}}},
			{Bssid: "02:00:00:00:00:02", Ssid: "second"},
		},
	}}}
	out, err := CommandResult("self", result, command.Options{}, Presentation{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "\n\n[ ] AP 2\n") || strings.Contains(out, "\n\n\n[ ] AP 2\n") {
		t.Fatalf("AP blocks need exactly one blank separator:\n%s", out)
	}
	for _, want := range []string{
		"AP 1\n  SSID:                          first",
		"    IE 1: id=48",
		"AP 2\n  SSID:                          second",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing hierarchy %q in:\n%s", want, out)
		}
	}
	for _, want := range []struct {
		label  string
		value  string
		indent int
	}{{"Wi-Fi Security Details", "", 2}, {"rsn:", "version=1", 4}, {"Fields", "", 2}, {"ap_link_id:", "2", 4}} {
		found := false
		for _, line := range strings.Split(out, "\n") {
			if strings.Contains(line, want.label) && strings.Contains(line, want.value) && len(line)-len(strings.TrimLeft(line, " ")) == want.indent {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing nested line with %d-space indentation: %q", want.indent, want.label)
		}
	}
	indentOf := func(label, value string) int {
		for _, line := range strings.Split(out, "\n") {
			if strings.Contains(line, label) && strings.Contains(line, value) {
				return len(line) - len(strings.TrimLeft(line, " "))
			}
		}
		return -1
	}
	if operatorIndent, securityIndent := indentOf("Operator:", "owner-property"), indentOf("rsn:", "version=1"); operatorIndent != 4 || securityIndent != 4 {
		t.Fatalf("AP property after the Security section was captured by that section: operator=%d security=%d\n%s", operatorIndent, securityIndent, out)
	}
	if linkIndent, securityIndent := indentOf("AP link ID:", "2"), indentOf("rsn:", "version=1"); linkIndent != 4 || securityIndent != 4 {
		t.Fatalf("AP link ID was captured by the Security section: link=%d security=%d\n%s", linkIndent, securityIndent, out)
	}
	if strings.Contains(out, "\nWi-Fi Security Details\n") || strings.Contains(out, "\nap_link_id:") {
		t.Fatalf("AP-owned details escaped their parent:\n%s", out)
	}
}

func TestNetworkSnapshotSeparatesNetworkSiblingsAndIPChildren(t *testing.T) {
	result := &controlpb.CommandResult{Payload: &controlpb.CommandResult_NetworkSnapshot{NetworkSnapshot: &controlpb.NetworkSnapshot{Networks: []*controlpb.NetworkDiagnostics{
		{NetworkId: "7", Active: true, IpStatus: &controlpb.IpStatus{NetworkId: "7", ObservationFields: testObservationFields("link_properties", "capabilities", "default_network"), Addresses: []string{"192.0.2.7/24"}}},
		{NetworkId: "8", IpStatus: &controlpb.IpStatus{NetworkId: "8", ObservationFields: testObservationFields("link_properties", "capabilities", "default_network"), Addresses: []string{"2001:db8::8/64"}}},
	}}}}
	out, err := CommandResult("self", result, command.Options{}, Presentation{})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Network 1", "Network 2", "192.0.2.7/24", "2001:db8::8/64", "IPv4", "IPv6"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing network/IP output %q:\n%s", want, out)
		}
	}
	lines := strings.Split(out, "\n")
	for _, heading := range []struct {
		text   string
		indent int
	}{{"Network 1", 0}, {"Network 2", 0}, {"IPv4", 2}, {"IPv6", 2}} {
		found := false
		for _, line := range lines {
			if strings.Contains(line, heading.text) && len(line)-len(strings.TrimLeft(line, " ")) == heading.indent {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%q has wrong hierarchy indentation:\n%s", heading.text, out)
		}
	}
}

func TestDiagnosticsScanTableStaysInsideScanSummary(t *testing.T) {
	result := &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_OK, Payload: &controlpb.CommandResult_WifiDiagnostics{WifiDiagnostics: &controlpb.WifiDiagnostics{
		Scan: &controlpb.WifiScan{Results: []*controlpb.WifiScanResult{{Ssid: "Lab", Bssid: "02:00:00:00:00:01", RssiDbm: -45, FrequencyMhz: 5180}}},
	}}}
	out, err := CommandResult("self", result, command.Options{WifiScanBrief: true}, Presentation{Width: 120})
	if err != nil {
		t.Fatal(err)
	}
	summary := strings.Index(out, "Scan summary")
	if summary < 0 {
		t.Fatalf("missing Scan summary:\n%s", out)
	}
	checked := map[string]bool{"SSID": false, "Lab": false}
	inSummary := false
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "Scan summary" {
			inSummary = true
			continue
		}
		if inSummary && strings.TrimSpace(line) == "Device" {
			break
		}
		for value := range checked {
			if inSummary && strings.Contains(line, value) {
				if len(line)-len(strings.TrimLeft(line, " ")) < 2 {
					t.Fatalf("scan table escaped its owning section: %q\n%s", line, out)
				}
				checked[value] = true
			}
		}
	}
	if !checked["SSID"] || !checked["Lab"] {
		t.Fatalf("missing diagnostics scan table:\n%s", out)
	}
}

func TestMonitorEventFieldsStayWithinTheirEventEvidence(t *testing.T) {
	result := &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_OK, Payload: &controlpb.CommandResult_WifiMonitor{WifiMonitor: &controlpb.WifiMonitorResult{
		Events: []*controlpb.WifiEvent{{UnixTimeMs: 1234, Type: "connected", Fields: []*controlpb.DiagnosticField{{Key: "network_id", Value: "7"}}}},
	}}}
	out, err := CommandResult("self", result, command.Options{}, Presentation{})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []struct {
		value  string
		indent int
	}{{"Event 1 evidence", 0}, {"Fields", 2}, {"network_id:", 4}} {
		found := false
		for _, line := range strings.Split(out, "\n") {
			if strings.Contains(line, want.value) && len(line)-len(strings.TrimLeft(line, " ")) == want.indent {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("event hierarchy missing %q at indentation %d:\n%s", want.value, want.indent, out)
		}
	}
}

func TestRenderWifiRequestResultsKeepCapturedState(t *testing.T) {
	status := &controlpb.WifiStatus{
		Enabled:           true,
		State:             "enabled",
		ObservationFields: testObservationFields("radio", "connection"),
		Connection: &controlpb.WifiConnection{
			ObservationFields: testObservationFields("identity"),
			Ssid:              "AutoShowSSID",
			Bssid:             "aa:bb:cc:dd:ee:ff",
			Ipv4Address:       "192.0.2.10",
		},
		IpStatus: &controlpb.IpStatus{
			ObservationFields: testObservationFields("link_properties"),
			InterfaceName:     "wlan0",
			Addresses:         []string{"192.0.2.10/24"},
		},
	}
	tests := []struct {
		name          string
		want          string
		res           *controlpb.CommandResult
		capturedState bool
	}{
		{
			name: "connect",
			want: "Connect:",
			res: &controlpb.CommandResult{Payload: &controlpb.CommandResult_ConnectWifi{ConnectWifi: &controlpb.ConnectWifiResult{
				Ssid:      "Lab",
				Connected: true,
				Message:   "connected",
				IpStatus:  status.GetIpStatus(),
			}}},
		},
		{
			name:          "operation",
			want:          "Wi-Fi Operation",
			capturedState: true,
			res: &controlpb.CommandResult{Payload: &controlpb.CommandResult_WifiOperation{WifiOperation: &controlpb.WifiOperationResult{
				Operation: "reconnect",
				Ok:        true,
				Message:   "reconnected",
				Status:    status,
			}}},
		},
		{
			name:          "assert",
			want:          "Wi-Fi Assert",
			capturedState: true,
			res: &controlpb.CommandResult{Payload: &controlpb.CommandResult_WifiAssert{WifiAssert: &controlpb.WifiAssertResult{
				Passed: true,
				Checks: []*controlpb.DiagnosticCheck{{
					Key:      "connected",
					Expected: "true",
					Actual:   "true",
					Passed:   true,
				}},
				Status: status,
			}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := CommandResult("agent", tt.res, command.Options{})
			if err != nil {
				t.Fatalf("CommandResult() error = %v", err)
			}
			if !strings.Contains(out, tt.want) {
				t.Fatalf("rendered output = %q, missing %q", out, tt.want)
			}
			for _, value := range []string{"AutoShowSSID", "192.0.2.10"} {
				if strings.Contains(out, value) != tt.capturedState {
					t.Fatalf("captured state visibility=%t for %q: %s", tt.capturedState, value, out)
				}
			}
		})
	}
}

func TestRenderCommandResultShowsCommandLatencyFallback(t *testing.T) {
	result := &controlpb.CommandResult{
		Status:    controlpb.CommandResult_STATUS_OK,
		ElapsedMs: 7,
		Payload: &controlpb.CommandResult_WifiStatus{
			WifiStatus: &controlpb.WifiStatus{Enabled: true},
		},
	}

	out, err := CommandResult("agent", result, command.Options{})
	if err != nil {
		t.Fatalf("renderCommandResult() error = %v", err)
	}
	if !slices.Equal(metadataQAValues(out, "Latency"), []string{"7ms"}) {
		t.Fatalf("rendered output = %q, missing command latency", out)
	}
}

func TestRenderWifiStatusShowsChannelAndBandwidth(t *testing.T) {
	result := &controlpb.CommandResult{
		Status: controlpb.CommandResult_STATUS_OK,
		Payload: &controlpb.CommandResult_WifiStatus{
			WifiStatus: &controlpb.WifiStatus{
				Enabled: true,
				Permissions: []string{
					"ACCESS_FINE_LOCATION=granted",
					"NEARBY_WIFI_DEVICES=granted",
				},
				Connection: &controlpb.WifiConnection{
					Ssid:          "Lab",
					Bssid:         "aa:bb:cc:dd:ee:ff",
					FrequencyMhz:  5200,
					ChannelWidth:  "80MHz",
					LinkSpeedMbps: 573,
				},
				IpStatus: &controlpb.IpStatus{
					NetworkId: "102",
					Wifi: &controlpb.WifiConnection{
						Ssid:         "Lab",
						FrequencyMhz: 5200,
					},
				},
			},
		},
	}

	out, err := CommandResult("agent", result, command.Options{})
	if err != nil {
		t.Fatalf("renderCommandResult() error = %v", err)
	}
	for _, want := range []string{"\nConnection detail\n", "CH/BW", "40/80MHz", "Link speed Mbps", "573"} {
		if !strings.Contains(out, want) {
			t.Fatalf("rendered output = %q, missing %q", out, want)
		}
	}
	if strings.Count(out, "\nConnection detail\n") != 1 {
		t.Fatalf("rendered output = %q, duplicated wifi connection", out)
	}
}

func TestRenderWifiStatusShowsCapabilities(t *testing.T) {
	result := &controlpb.CommandResult{
		Status: controlpb.CommandResult_STATUS_OK,
		Payload: &controlpb.CommandResult_WifiStatus{
			WifiStatus: &controlpb.WifiStatus{
				Enabled: true,
				Permissions: []string{
					"ACCESS_FINE_LOCATION=granted",
					"NEARBY_WIFI_DEVICES=granted",
				},
				Connection: &controlpb.WifiConnection{
					ObservationFields: testObservationFields("identity", "rssi", "tx_link_speed_mbps", "rx_link_speed_mbps"),
					Ssid:              "Lab",
					Bssid:             "aa:bb:cc:dd:ee:ff",
					FrequencyMhz:      5975,
					LinkSpeedMbps:     1200,
					TxLinkSpeedMbps:   900,
					RxLinkSpeedMbps:   1200,
					MacAddress:        "02:00:00:00:00:09",
					WifiStandard:      "802.11be",
					SecurityType:      "wpa3_sae",
					InformationElements: []*controlpb.WifiInformationElement{{
						Id:        54,
						ByteCount: 3,
						BytesHex:  "010203",
					}, {
						Id:        70,
						ByteCount: 5,
						BytesHex:  "0100000000",
					}, {
						Id:        127,
						ByteCount: 3,
						BytesHex:  "000008",
					}, {
						Id:        255,
						IdExt:     108,
						ByteCount: 2,
						BytesHex:  "beef",
					}},
				},
				IpStatus: &controlpb.IpStatus{
					ObservationFields: testObservationFields("link_properties", "capabilities"),
					NetworkId:         "102",
					Transports:        []string{"wifi"},
					Capabilities:      []string{"internet", "validated", "not_roaming", "not_metered"},
					DownstreamKbps:    1200000,
					UpstreamKbps:      600000,
					SignalStrength:    -52,
					RawCapabilities:   "raw_caps",
					Addresses:         []string{"192.0.2.10/24", "2001:db8::10/64", "2001:db8::11/64"},
					DnsServers:        []string{"192.0.2.1", "1.1.1.1"},
					DhcpServer:        "192.0.2.254",
					Routes:            []string{"0.0.0.0/0 -> 192.0.2.1 wlan0", "192.0.2.0/24 -> 0.0.0.0 wlan0"},
					Ipv6Ra: []*controlpb.DiagnosticField{
						{Key: "default_route", Value: "missing"},
						{Key: "accept_ra", Value: "2"},
						{Key: "accept_ra_defrtr", Value: "1"},
						{Key: "accept_ra_min_lft", Value: "180"},
					},
					Domains:          "local",
					PrivateDnsActive: true,
				},
			},
		},
	}

	out, err := CommandResult("agent", result, command.Options{})
	if err != nil {
		t.Fatalf("renderCommandResult() error = %v", err)
	}
	ipOut, err := CommandResult("agent", &controlpb.CommandResult{Status: result.GetStatus(), Payload: &controlpb.CommandResult_IpStatus{IpStatus: result.GetWifiStatus().GetIpStatus()}}, command.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "192.0.2.10/24") || strings.Contains(out, "all_granted") {
		t.Fatalf("L2 repeats IP or normal permission dump: %s", out)
	}
	out += ipOut
	for _, want := range []string{
		"AP Capabilities",
		"roaming",
		"11r",
		"11k",
		"11v_bss_transition",
		"phy",
		"eht",
		"roaming: 11k 11r 11v_bss_transition",
		"link",
		"Tx link rate: 900Mbps",
		"Rx link rate: 1200Mbps",
		"STA MAC",
		"02:00:00:00:00:09",
		"IE 4: id=255 extension=108 bytes=2 hex=beef",
		"Network:",
		"not_metered",
		"Capabilities:",
		"Bandwidth:",
		"down=1200000kbps up=600000kbps",
		"Signal strength:",
		"-52",
		"IPv4",
		"192.0.2.10/24",
		"Addr: 2001:db8::10/64\nAddr: 2001:db8::11/64",
		"DNS:",
		"192.0.2.1",
		"DNS: 192.0.2.1\nDNS: 1.1.1.1",
		"Domains:",
		"local",
		"DHCP server:",
		"192.0.2.254",
		"Private DNS:",
		"on; server=none",
		"Routes:",
		"0.0.0.0/0 -> 192.0.2.1 wlan0",
		"Routes: 0.0.0.0/0 -> 192.0.2.1 wlan0\nRoutes: 192.0.2.0/24 -> 0.0.0.0 wlan0",
		"IPv6 RA (android)",
		"default_route=missing",
		"accept_ra_min_lft=180",
	} {
		if !strings.Contains(strings.Join(strings.Fields(out), " "), strings.Join(strings.Fields(want), " ")) {
			t.Fatalf("rendered output = %q, missing %q", out, want)
		}
	}
	if strings.Index(out, "\nRoutes:") >= strings.Index(out, "\nDNS:") || strings.Index(out, "\nDNS:") >= strings.Index(out, "\nDomains:") {
		t.Fatalf("network rows are not ordered routes -> DNS -> domains:\n%s", out)
	}
	for _, unwanted := range []string{
		"Connection Capabilities",
		"CAPABILITY",
		"Connection Information Elements",
		"id=255 ext=108 bytes=2",
		"Network Capabilities",
		"\nAddresses\n",
		"\nDNS\n",
		"\nDHCP\n",
		"\nPrivate DNS\n",
		"raw_caps",
		"all_granted ACCESS_FINE_LOCATION,NEARBY_WIFI_DEVICES",
		"not_metered,not_roaming",
		"192.0.2.1,1.1.1.1",
		"0.0.0.0/0 -> 192.0.2.1 wlan0 | 192.0.2.0/24 -> 0.0.0.0 wlan0",
		"11k,11r,11v",
		"security=wpa3_sae",
	} {
		if strings.Contains(out, unwanted) {
			t.Fatalf("rendered output = %q, included unwanted %q", out, unwanted)
		}
	}
	if strings.Index(out, "11k") >= strings.Index(out, "11r") ||
		strings.Index(out, "11r") >= strings.Index(out, "11v") {
		t.Fatalf("rendered output = %q, capability rows are not sorted", out)
	}
}

func TestRenderWifiStatusShowsStructuredHEAndEHTDetails(t *testing.T) {
	result := &controlpb.CommandResult{
		Status: controlpb.CommandResult_STATUS_OK,
		Payload: &controlpb.CommandResult_WifiStatus{
			WifiStatus: &controlpb.WifiStatus{
				Connection: &controlpb.WifiConnection{
					Ssid: "Lab",
					HeCapabilities: &controlpb.WifiHeCapabilities{
						MacCapabilitiesHex: "060084010090",
						PhyCapabilitiesHex: "1c0200980301c000c00c00",
						Features:           []string{"ofdma_random_access", "partial_bw_ul_mu_mimo", "ul_2x996_tone_ru"},
						McsNss: []*controlpb.WifiMcsNssSupport{{
							Standard: "he", Bandwidth: "le_80mhz", Direction: "rx", McsRange: "0-11", Nss: 2,
						}, {
							Standard: "he", Bandwidth: "le_80mhz", Direction: "tx", McsRange: "0-11", Nss: 2,
						}},
						PpeThresholdsPresent: true,
						PpeNssCount:          2,
						PpeRuIndices:         []string{"242-tone", "2x996-tone"},
						PpeThresholdsHex:     "79010203040506",
					},
					HeOperation: &controlpb.WifiHeOperation{
						Parameters:         0x11020008,
						Flags:              []string{"twt_required", "6ghz_operation_info_present"},
						BssColor:           17,
						BasicMcsNssSetHex:  "ffff",
						ChannelWidth:       "80MHz",
						PrimaryChannel:     5,
						CenterFreqSegment0: 31,
					},
					He_6GhzCapabilities: he6GhzCapabilitiesTestValue(),
					EhtCapabilities: &controlpb.WifiEhtCapabilities{
						MacCapabilitiesHex: "774e",
						PhyCapabilitiesHex: "1680d1e33f08077e03",
						Features:           []string{"242_tone_ru_gt_20mhz", "non_ofdma_ul_mu_mimo_320mhz", "mu_beamformer_320mhz"},
						McsNss: []*controlpb.WifiMcsNssSupport{{
							Standard: "eht", Bandwidth: "le_80mhz", Direction: "rx", McsRange: "0-9", MaxNss: 1,
						}, {
							Standard: "eht", Bandwidth: "le_80mhz", Direction: "tx", McsRange: "0-9", MaxNss: 2,
						}},
						PpeThresholdsPresent: true,
						PpeNssCount:          2,
						PpeRuIndices:         []string{"242-tone", "4x996-tone"},
						PpeThresholdsHex:     "f10102030405060708",
					},
					EhtOperation: &controlpb.WifiEhtOperation{
						Parameters:               0x43,
						Flags:                    []string{"disabled_subchannel_bitmap_present", "mcs15_disabled"},
						BasicMcsNssSetHex:        "11223344",
						ChannelWidth:             "320MHz",
						CenterFreqSegment0:       31,
						CenterFreqSegment1:       63,
						DisabledSubchannelBitmap: 0x0a,
					},
					HeUoraParameterSet: &controlpb.WifiHeUoraParameterSet{
						EocwMin: 5,
						EocwMax: 5,
					},
					HeMuEdcaParameterSet: &controlpb.WifiHeMuEdcaParameterSet{
						QosInfo: 1,
						Ac: []*controlpb.WifiHeMuEdcaAcRecord{{
							Ac: "be", Aci: 0, Aifsn: 3, EcwMin: 15, EcwMax: 9, Timer: 32,
						}},
					},
					HeSpatialReuseParameterSet: &controlpb.WifiHeSpatialReuseParameterSet{
						SrControl:                0x1c,
						Flags:                    []string{"srg_information_present"},
						NonSrgObssPdMaxOffset:    10,
						SrgObssPdMinOffset:       20,
						SrgObssPdMaxOffset:       30,
						SrgBssColorBitmapHex:     "0102030405060708",
						SrgPartialBssidBitmapHex: "1112131415161718",
					},
				},
			},
		},
	}

	out, err := CommandResult("agent", result, command.Options{})
	if err != nil {
		t.Fatalf("renderCommandResult() error = %v", err)
	}
	for _, want := range []string{
		"HE/EHT Details",
		"he_cap",
		"ofdma_random_access",
		"mcs_nss he/le_80mhz/0-11 rx=nss2 tx=nss2",
		"eht_cap",
		"242_tone_ru_gt_20mhz",
		"mcs_nss eht/le_80mhz/0-9 rx=nss1 tx=nss2",
		"he_oper",
		"width=80MHz primary=5 ccfs0=31 ccfs1=0",
		"he_6ghz_cap",
		"max_mpdu=11454",
		"max_ampdu=262143",
		"eht_oper",
		"width=320MHz ccfs0=31 ccfs1=63",
		"disabled_subchannel_bitmap=0xa",
		"uora",
		"eocw_min=5 eocw_max=5",
		"mu_edca",
		"spatial_reuse",
		"srg_bss_color_bitmap=0x0102030405060708",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("rendered output = %q, missing %q", out, want)
		}
	}
}

func TestRenderWifiStatusPreservesSeparateWifiInfoNetworkValues(t *testing.T) {
	result := &controlpb.CommandResult{
		Status: controlpb.CommandResult_STATUS_OK,
		Payload: &controlpb.CommandResult_WifiStatus{
			WifiStatus: &controlpb.WifiStatus{
				Connection: &controlpb.WifiConnection{
					Ssid:        "Lab",
					NetworkId:   119,
					Ipv4Address: "192.0.2.10",
				},
			},
		},
	}

	out, err := CommandResult("agent", result, command.Options{})
	if err != nil {
		t.Fatalf("renderCommandResult() error = %v", err)
	}
	for label, want := range map[string]string{"Network configuration ID": "119", "WifiInfo IPv4 (separate observation)": "192.0.2.10"} {
		if !slices.Equal(metadataQAValues(out, label), []string{want}) {
			t.Fatalf("rendered output = %q, missing %q", out, want)
		}
	}
	if strings.Contains(out, "Related IP snapshot") || strings.Contains(out, "\n  ip ") {
		t.Fatalf("rendered output fabricated a Network observation from WifiInfo: %q", out)
	}
}

func TestRenderWifiStatusSuppressesDuplicateNetworkSignal(t *testing.T) {
	result := &controlpb.CommandResult{
		Status: controlpb.CommandResult_STATUS_OK,
		Payload: &controlpb.CommandResult_WifiStatus{
			WifiStatus: &controlpb.WifiStatus{
				Connection: &controlpb.WifiConnection{
					Ssid:    "Lab",
					RssiDbm: -48,
				},
				IpStatus: &controlpb.IpStatus{
					Capabilities:   []string{"not_metered"},
					SignalStrength: -48,
				},
			},
		},
	}

	out, err := CommandResult("agent", result, command.Options{})
	if err != nil {
		t.Fatalf("renderCommandResult() error = %v", err)
	}
	if strings.Contains(out, "signal_strength") {
		t.Fatalf("rendered output = %q, included duplicate signal_strength", out)
	}
	if strings.Contains(out, "not_metered") {
		t.Fatalf("L2 output repeats IP network capabilities: %q", out)
	}
}

func TestRenderWifiStatusSummarizesMLOWithoutLinkDump(t *testing.T) {
	result := &controlpb.CommandResult{
		Status: controlpb.CommandResult_STATUS_OK,
		Payload: &controlpb.CommandResult_WifiStatus{
			WifiStatus: &controlpb.WifiStatus{
				Enabled: true,
				Connection: &controlpb.WifiConnection{
					ObservationFields: testObservationFields("identity", "ap_mld_mac_address", "associated_mlo_links"),
					Ssid:              "Lab",
					Bssid:             "aa:bb:cc:dd:ee:ff",
					FrequencyMhz:      5975,
					ApMldMacAddress:   "02:00:00:00:00:01",
					ApMloLinkId:       2,
					AssociatedMloLinks: []*controlpb.MloLinkInfo{{
						LinkId:          2,
						State:           "active",
						Band:            "6ghz",
						Channel:         5,
						RssiDbm:         -48,
						TxLinkSpeedMbps: 1200,
						RxLinkSpeedMbps: 1200,
						ApMacAddress:    "02:00:00:00:00:02",
					}},
				},
			},
		},
	}

	out, err := CommandResult("agent", result, command.Options{}, Presentation{DetailAvailable: true})
	if err != nil {
		t.Fatalf("renderCommandResult() error = %v", err)
	}
	for _, unwanted := range []string{
		"\nMLO\n",
		"\nMLO Links\n",
		"ap_mld",
		"ap_link_id",
	} {
		if strings.Contains(out, unwanted) {
			t.Fatalf("rendered output = %q, included %q", out, unwanted)
		}
	}
	if compact := strings.Join(strings.Fields(out), " "); !strings.Contains(compact, "MLD: 02:00:00:00:00:01") || !strings.Contains(compact, "Associated: 1") {
		t.Fatalf("missing current MLO summary: %s", out)
	}
}

func TestScanMLOLinkIDRequiresPresenceEvenFor11be(t *testing.T) {
	got := scanMLOLinkID(&controlpb.WifiScanResult{
		WifiStandard:      "802.11be",
		ApMloLinkId:       0,
		ObservationFields: testObservationFields("ap_mlo_link_id"),
	})
	if got != "0" {
		t.Fatalf("scanMLOLinkID() = %q, want 0", got)
	}
	got = scanMLOLinkID(&controlpb.WifiScanResult{
		ApMloLinkId: 0,
	})
	if !strings.HasPrefix(got, "?") {
		t.Fatalf("scanMLOLinkID() without metadata = %q, want unknown", got)
	}
	got = scanMLOLinkID(&controlpb.WifiScanResult{
		WifiStandard: "802.11ax",
		ApMloLinkId:  -1,
		InformationElements: []*controlpb.WifiInformationElement{
			ehtMultiLinkTestIE(),
		},
	})
	if got != "2" {
		t.Fatalf("scanMLOLinkID() from EHT Multi-Link IE = %q, want 2", got)
	}
}

func TestRenderWifiScanShowsMLOFields(t *testing.T) {
	result := &controlpb.CommandResult{
		Status: controlpb.CommandResult_STATUS_OK,
		Payload: &controlpb.CommandResult_WifiScan{
			WifiScan: &controlpb.WifiScan{
				Fields: []*controlpb.DiagnosticField{{
					Key:   "requested_band",
					Value: "all",
				}},
				Results: []*controlpb.WifiScanResult{{
					Ssid:            "Lab",
					Bssid:           "aa:bb:cc:dd:ee:ff",
					Capabilities:    "[RSN-SAE-CCMP][ESS]",
					Raw:             "scan-object-raw-marker",
					RssiDbm:         -48,
					Band:            "6ghz",
					FrequencyMhz:    5975,
					WifiStandard:    "802.11be",
					SecurityTypes:   []string{"wpa3_sae"},
					ApMldMacAddress: "02:00:00:00:00:01",
					ApMloLinkId:     2,
					AffiliatedMloLinks: []*controlpb.MloLinkInfo{{
						ObservationFields: testObservationFields("identity", "rates"),
						LinkId:            2,
						State:             "active",
						Band:              "6ghz",
						Channel:           5,
						RssiDbm:           -48,
						TxLinkSpeedMbps:   1200,
						RxLinkSpeedMbps:   1200,
						ApMacAddress:      "02:00:00:00:00:02",
						StaMacAddress:     "02:00:00:00:00:03",
					}},
					InformationElements: []*controlpb.WifiInformationElement{{
						Id:        54,
						ByteCount: 3,
						BytesHex:  "010203",
					}, {
						Id:        70,
						ByteCount: 5,
						BytesHex:  "0100000000",
					}, {
						Id:        127,
						ByteCount: 3,
						BytesHex:  "000008",
					}, {
						Id:        255,
						IdExt:     35,
						ByteCount: 2,
						BytesHex:  "abcd",
					}},
				}, {
					Ssid:         "Legacy",
					Bssid:        "11:22:33:44:55:66",
					RssiDbm:      -60,
					Band:         "5ghz",
					FrequencyMhz: 5200,
				}},
			},
		},
	}

	out, err := CommandResult("agent", result, command.Options{})
	if err != nil {
		t.Fatalf("renderCommandResult() error = %v", err)
	}
	for _, want := range []string{
		"Band:",
		"shown=2",
		"total",
		"MLD",
		"FLAGS",
		"02:00:00:00:00:01",
		"AP_MAC",
		"active",
		"1200",
		"11k,11r,11v",
		"[RSN-SAE-CCMP][ESS]",
		"IE 3: id=127 extension=0 bytes=3 hex=000008",
	} {
		if !strings.Contains(strings.Join(strings.Fields(out), " "), want) {
			t.Fatalf("rendered output = %q, missing %q", out, want)
		}
	}
	for _, unwanted := range []string{
		"ANDROID_CAPABILITIES",
		"scan-object-raw-marker",
		"he_capabilities",
		"Scan Connection Capabilities",
		"Scan Information Elements",
		"802.11k",
		"802.11r",
		"802.11v_bss_transition",
	} {
		if strings.Contains(out, unwanted) {
			t.Fatalf("rendered output = %q, included unwanted %q", out, unwanted)
		}
	}
	for _, unwanted := range []string{
		"FIELD",
		"VALUE",
		"scan_result_count",
		"scan_result_total_count",
		"\nScan Results\n",
	} {
		if strings.Contains(out, unwanted) {
			t.Fatalf("rendered output = %q, included unwanted heading %q", out, unwanted)
		}
	}
}

func TestRenderWifiScanBriefOmitsVerboseSections(t *testing.T) {
	result := &controlpb.CommandResult{
		Status: controlpb.CommandResult_STATUS_OK,
		Payload: &controlpb.CommandResult_WifiScan{
			WifiScan: &controlpb.WifiScan{
				Fields: []*controlpb.DiagnosticField{{Key: "requested_band", Value: "all"}},
				Results: []*controlpb.WifiScanResult{{
					Ssid:            "Lab",
					Bssid:           "aa:bb:cc:dd:ee:ff",
					RssiDbm:         -48,
					Band:            "6ghz",
					FrequencyMhz:    5975,
					WifiStandard:    "802.11be",
					ApMldMacAddress: "02:00:00:00:00:01",
					ApMloLinkId:     2,
					SecurityDetails: wifi7SecurityDetailsTestValue(),
					AffiliatedMloLinks: []*controlpb.MloLinkInfo{{
						LinkId:       1,
						State:        "idle",
						Band:         "5ghz",
						Channel:      44,
						RssiDbm:      -55,
						ApMacAddress: "aa:bb:cc:dd:ee:01",
					}},
				}},
			},
		},
	}

	out, err := CommandResult("agent", result, command.Options{WifiScanBrief: true})
	if err != nil {
		t.Fatalf("CommandResult() error = %v", err)
	}
	for _, want := range []string{"Wi-Fi scan", "SSID", "PHY", "Lab", "be", "AGE(s)", "Full"} {
		if !strings.Contains(out, want) {
			t.Fatalf("rendered output = %q, missing %q", out, want)
		}
	}
	for _, unwanted := range []string{"Affiliated", "Wi-Fi Security Details", "ready", "strict"} {
		if strings.Contains(out, unwanted) {
			t.Fatalf("rendered output = %q, unexpected %q", out, unwanted)
		}
	}
	full, err := CommandResult("agent", result, command.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"gcmp256=true", "sae_gdh=true", "ft_sae_gdh=true", "sae_h2e", "ssid_protection", "beacon_protection=true", "capabilities=0x00c0"} {
		if !strings.Contains(full, want) {
			t.Fatalf("full scan lost decoded security %q: %s", want, full)
		}
	}
}

func TestRenderWifiScanAlignsFullWidthSSID(t *testing.T) {
	result := &controlpb.CommandResult{
		Status: controlpb.CommandResult_STATUS_OK,
		Payload: &controlpb.CommandResult_WifiScan{
			WifiScan: &controlpb.WifiScan{
				Results: []*controlpb.WifiScanResult{{
					Ssid:         "grape",
					Bssid:        "aa:bb:cc:dd:ee:ff",
					RssiDbm:      -45,
					Band:         "6ghz",
					FrequencyMhz: 5975,
					WifiStandard: "802.11ax",
					SecurityTypes: []string{
						"sae",
					},
				}, {
					Ssid:         "たか",
					Bssid:        "11:22:33:44:55:66",
					RssiDbm:      -49,
					Band:         "2.4ghz",
					FrequencyMhz: 2432,
					WifiStandard: "802.11ax",
					SecurityTypes: []string{
						"psk",
					},
				}},
			},
		},
	}

	out, err := CommandResult("agent", result, command.Options{})
	if err != nil {
		t.Fatalf("CommandResult() error = %v", err)
	}
	asciiLine := renderedLineContaining(out, "aa:bb:cc:dd:ee:ff")
	fullWidthLine := renderedLineContaining(out, "11:22:33:44:55:66")
	if asciiLine == "" || fullWidthLine == "" {
		t.Fatalf("rendered output missing scan rows:\n%s", out)
	}
	asciiBSSIDColumn := displayColumn(asciiLine, "aa:bb:cc:dd:ee:ff")
	fullWidthBSSIDColumn := displayColumn(fullWidthLine, "11:22:33:44:55:66")
	if asciiBSSIDColumn != fullWidthBSSIDColumn {
		t.Fatalf("BSSID column mismatch ascii=%d fullwidth=%d\nascii: %q\nfull:  %q\n%s", asciiBSSIDColumn, fullWidthBSSIDColumn, asciiLine, fullWidthLine, out)
	}
	if got := displayWidth("たか"); got != 4 {
		t.Fatalf("displayWidth(full-width SSID) = %d, want 4", got)
	}
}

func TestRenderWifiScanGroupsSSIDAndSortsByRSSI(t *testing.T) {
	result := &controlpb.CommandResult{
		Status: controlpb.CommandResult_STATUS_OK,
		Payload: &controlpb.CommandResult_WifiScan{
			WifiScan: &controlpb.WifiScan{
				Results: []*controlpb.WifiScanResult{{
					Ssid:         "Office",
					Bssid:        "11:11:11:11:11:02",
					RssiDbm:      -65,
					Band:         "5ghz",
					FrequencyMhz: 5200,
					WifiStandard: "802.11ax",
				}, {
					Ssid:         "Lab",
					Bssid:        "22:22:22:22:22:02",
					RssiDbm:      -70,
					Band:         "6ghz",
					FrequencyMhz: 6055,
					WifiStandard: "802.11be",
				}, {
					Ssid:         "Cafe",
					Bssid:        "33:33:33:33:33:01",
					RssiDbm:      -40,
					Band:         "6ghz",
					FrequencyMhz: 5975,
					WifiStandard: "802.11be",
				}, {
					Ssid:         "Lab",
					Bssid:        "22:22:22:22:22:01",
					RssiDbm:      -50,
					Band:         "5ghz",
					FrequencyMhz: 5180,
					WifiStandard: "802.11ax",
				}, {
					Ssid:         "Office",
					Bssid:        "11:11:11:11:11:01",
					RssiDbm:      -55,
					Band:         "2.4ghz",
					FrequencyMhz: 2412,
					WifiStandard: "802.11ax",
				}},
			},
		},
	}

	tests := []struct {
		name    string
		options command.Options
	}{
		{name: "default"},
		{name: "brief", options: command.Options{WifiScanBrief: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := CommandResult("agent", result, tt.options)
			if err != nil {
				t.Fatalf("CommandResult() error = %v", err)
			}
			rows := scanTableRows(out)
			wantOrder := []string{
				"33:33:33:33:33:01",
				"22:22:22:22:22:01",
				"22:22:22:22:22:02",
				"11:11:11:11:11:01",
				"11:11:11:11:11:02",
			}
			if len(rows) < len(wantOrder) {
				t.Fatalf("scan table rows = %#v, want at least %d rows\n%s", rows, len(wantOrder), out)
			}
			lastIndex := -1
			for _, bssid := range wantOrder {
				index := indexOfRowContaining(rows, bssid)
				if index < 0 {
					t.Fatalf("scan table missing row for %s:\n%s", bssid, out)
				}
				if index <= lastIndex {
					t.Fatalf("scan row order wrong for %s rows=%#v\n%s", bssid, rows, out)
				}
				lastIndex = index
			}
		})
	}
}

func renderedLineContaining(out string, needle string) string {
	for line := range strings.SplitSeq(out, "\n") {
		if strings.Contains(line, needle) {
			return line
		}
	}
	return ""
}

func displayColumn(line string, needle string) int {
	before, _, ok := strings.Cut(line, needle)
	if !ok {
		return -1
	}
	return displayWidth(before)
}

func scanTableRows(out string) []string {
	rows := []string{}
	for line := range strings.SplitSeq(out, "\n") {
		line = strings.TrimLeft(line, " ")
		if strings.HasPrefix(line, "BSSID:") || strings.HasPrefix(line, "AP_MAC:") {
			rows = append(rows, line)
		}
	}
	return rows
}

func indexOfRowContaining(rows []string, needle string) int {
	for i, row := range rows {
		if strings.Contains(row, needle) {
			return i
		}
	}
	return -1
}

func TestRenderWifiMLOAggregatesDiagnostics(t *testing.T) {
	result := &controlpb.CommandResult{
		Status: controlpb.CommandResult_STATUS_OK,
		Payload: &controlpb.CommandResult_WifiDiagnostics{
			WifiDiagnostics: &controlpb.WifiDiagnostics{
				Status: &controlpb.WifiStatus{
					Enabled: true,
					State:   "enabled",
					Connection: &controlpb.WifiConnection{
						Ssid:              "Lab",
						Bssid:             "aa:bb:cc:dd:ee:ff",
						ObservationFields: testObservationFields("identity"),
						FrequencyMhz:      5975,
						WifiStandard:      "802.11be",
						ApMldMacAddress:   "02:00:00:00:00:01",
						ApMloLinkId:       2,
						InformationElements: []*controlpb.WifiInformationElement{
							ehtMultiLinkTestIE(),
							rmEnabledCapabilitiesTestIE(),
							extendedCapabilitiesBssTransitionTestIE(),
							mobilityDomainTestIE(),
						},
						HeCapabilities:             heCapabilitiesTestValue(),
						HeOperation:                heOperationTestValue(),
						HeSpatialReuseParameterSet: heSpatialReuseTestValue(),
						He_6GhzCapabilities:        he6GhzCapabilitiesTestValue(),
						EhtCapabilities:            ehtCapabilitiesTestValue(),
						EhtOperation:               ehtOperationTestValue(),
						AssociatedMloLinks: []*controlpb.MloLinkInfo{{
							LinkId:       2,
							State:        "active",
							Band:         "6ghz",
							Channel:      5,
							RssiDbm:      -45,
							ApMacAddress: "aa:bb:cc:dd:ee:ff",
						}},
						SecurityDetails: wifi7SecurityDetailsTestValue(),
					},
				},
				Capabilities: &controlpb.WifiCapabilities{
					SupportedBands:         []string{"6GHz"},
					SupportedStandards:     []string{"802.11ax", "802.11be"},
					SupportedSecurityModes: []string{"wpa3_sae", "wpa3_sae_h2e", "wpa3_sae_public_key", "owe"},
					SupportedFeatures:      []string{"tid_to_link_mapping_negotiation", "dual_band_simultaneous", "sta_concurrency_multi_internet"},
				},
				Networks: []*controlpb.NetworkDiagnostics{{
					NetworkId: "100",
					Active:    true,
					IpStatus: &controlpb.IpStatus{
						Wifi: &controlpb.WifiConnection{
							Ssid:            "Lab",
							Bssid:           "aa:bb:cc:dd:ee:ff",
							WifiStandard:    "802.11be",
							ApMldMacAddress: "02:00:00:00:00:01",
							ApMloLinkId:     2,
						},
					},
				}},
				Scan: &controlpb.WifiScan{
					Fields: []*controlpb.DiagnosticField{{Key: "scan_result_count", Value: "1"}},
					Results: []*controlpb.WifiScanResult{{
						Ssid:                           "Lab",
						Bssid:                          "aa:bb:cc:dd:ee:ff",
						RssiDbm:                        -45,
						Band:                           "6ghz",
						FrequencyMhz:                   5975,
						ChannelWidth:                   "CHANNEL_WIDTH_80MHZ",
						WifiStandard:                   "802.11be",
						Responder_80211Mc:              true,
						Responder_80211AzNtb:           true,
						RangingFrameProtectionRequired: true,
						SecureHeLtfSupported:           true,
						TwtResponder:                   true,
						ApMldMacAddress:                "02:00:00:00:00:01",
						ApMloLinkId:                    2,
						InformationElements: []*controlpb.WifiInformationElement{
							ehtMultiLinkTestIE(),
							rnrTestIE(),
							rmEnabledCapabilitiesTestIE(),
							extendedCapabilitiesBssTransitionTestIE(),
							mobilityDomainTestIE(),
							multipleBSSIDTestIE(),
						},
						HeCapabilities:             heCapabilitiesTestValue(),
						HeOperation:                heOperationTestValue(),
						HeSpatialReuseParameterSet: heSpatialReuseTestValue(),
						He_6GhzCapabilities:        he6GhzCapabilitiesTestValue(),
						EhtCapabilities:            ehtCapabilitiesTestValue(),
						EhtOperation:               ehtOperationTestValue(),
						SecurityDetails:            wifi7SecurityDetailsTestValue(),
						AffiliatedMloLinks: []*controlpb.MloLinkInfo{{
							LinkId:       1,
							State:        "idle",
							Band:         "5ghz",
							Channel:      44,
							RssiDbm:      -55,
							ApMacAddress: "aa:bb:cc:dd:ee:01",
						}},
					}, {
						Ssid:         "Legacy",
						Bssid:        "11:22:33:44:55:66",
						RssiDbm:      -40,
						Band:         "5ghz",
						FrequencyMhz: 5200,
						WifiStandard: "802.11ax",
						ApMloLinkId:  -1,
					}},
				},
			},
		},
	}

	out, err := CommandResult("agent", result, command.Options{WifiRenderMode: command.WifiRenderModeEHT})
	if err != nil {
		t.Fatalf("CommandResult() error = %v", err)
	}
	for _, want := range []string{
		"Current AP Relation",
		"Connected MLO",
		"EHT Scan",
		"Nearby EHT APs",
		"EHT_W",
		"PUNCT",
		"EHT Scan Links",
		"Marks: *=connected BSSID ~=same MLD -=other observed MLO link",
		"Network MLO",
		"MLO Capability Signals",
		"[*] Lab",
		"type=ap ap_mld=02:00:00:00:00:01 link=2 bssid=aa:bb:cc:dd:ee:ff",
		"band=6ghz ch=5 freq=5975MHz width=80MHz eht_width=320MHz puncture=1,3 rssi=-45dBm",
		"  affiliated_links",
		"    [~] affiliated link",
		"Identity: type=aff link=1 ap_mac=aa:bb:cc:dd:ee:01",
		"Connected EHT Multi-Link Elements",
		"ml_control raw=0x07f0 type=basic(0) presence=link_id_info,bss_parameters_change_count,medium_synchronization_delay,eml_capabilities,mld_capabilities_and_operations,ap_mld_id,extended_mld_capabilities_and_operations bytes=37",
		"common_info len=18 mld_mac=02:00:00:00:00:01 link_id=2 bss_param_change_count=7 medium_sync_delay=0x1032 eml_capabilities=0x8f08 mld_capabilities=0x3370 ap_mld_id=5 ext_mld_capabilities=0x6100",
		"medium_sync raw=0x1032 duration=16 ofdm_ed_threshold=2 max_txop=3",
		"eml raw=0x8f08 flags=emlsr,emlmr",
		"mld raw=0x3370 flags=srs,aar,link_reconfig,aligned_twt",
		"ext_mld raw=0x6100 flags=op_param_update,nstr_update,emlsr_enabled_one_link",
		"per_link link_id=2 control=0x0972 complete=true",
		"sta_info_len=12 sta_mac=02:00:00:00:00:02 beacon_interval_tu=100",
		"dtim=1/3",
		"Scan EHT Multi-Link Elements",
		"Connected EHT Puncturing",
		"Scan EHT Puncturing",
		"he_preamble_puncturing_rx: preamble_puncturing_rx_80mhz_second_20mhz",
		"he_punctured_sounding: true",
		"eht_disabled_subchannel_bitmap: 0x000a punctured=1,3",
		"Connected HE 6GHz Details",
		"Scan HE 6GHz Details",
		"Connected Wi-Fi Security",
		"Scan Wi-Fi 7 Security",
		"gcmp256=true",
		"sae_gdh=true",
		"beacon_protection=true",
		"personal_ready=true",
		"wifi7_strict pairwise_gcmp256_only=true akm_gdh_only=true group_data_gcmp256=true group_mgmt_256=true fallback=<none> strict_ready=true",
		"Connected Roaming / Transition",
		"summary 11k=true 11v_bss_transition=true 11r=true ft_akm=ft_sae_gdh rnr=false",
		"Scan Roaming / Transition",
		"summary 11k=true 11v_bss_transition=true 11r=true ft_akm=ft_sae_gdh rnr=true",
		"Connected BSS Coloring",
		"Scan BSS Coloring",
		"he_operation: bss_color=17 disabled=false partial=true",
		"srg_bss_color_bitmap: 0x0102030405060708",
		"COLOR",
		"17(part)",
		"Connected HE Details",
		"Scan HE Details",
		"he_mac flags: twt_responder,om_control,punctured_sounding",
		"he features: twt_responder,punctured_sounding",
		"ie rsn=false rsnxe=false ext_cap=true rnr=true mbssid=true noninherit=false eht_mle=true ap_mld=true link_id=true",
		"sdk_flags twt=true 11az_ntb=true ranging_prot=true secure_he_ltf=true 11mc=true",
		"Scan RNR Details",
		"rnr band=6ghz width=80MHz channel=5 freq=5975MHz op_class=133",
		"mld ap_mld_id=7 link_id=2",
		"Scan Multiple BSSID Details",
		"profile #1",
		"noninherit=ids:48/ext:106",
		"profile_security #1",
		"Wi-Fi 7 Device Readiness",
		"band_6ghz",
		"wpa3_sae_h2e",
		"dual_band_simultaneous",
		"Connected EHT Details",
		"max_mpdu=7991",
		"triggered_txop_mode1",
		"scs_traffic_description",
		"eht features: 320mhz_in_6ghz,rx_4096qam_wider_dl_ofdma",
		"max_ampdu=262143",
		"320mhz=true",
		"oper flags: disabled_subchannel_bitmap_present,mcs15_disabled",
		"width_mhz=320",
		"disabled=0x000a",
		"punctured=1,3",
		"Scan EHT Details",
		"tid_to_link_mapping_negotiation",
		"Diagnostics / Warnings",
	} {
		if !strings.Contains(strings.Join(strings.Fields(out), " "), strings.Join(strings.Fields(want), " ")) {
			t.Fatalf("rendered output = %q, missing %q", out, want)
		}
	}
	wrapped, err := CommandResult("agent", result, command.Options{WifiRenderMode: command.WifiRenderModeEHT}, Presentation{Width: 92})
	if err != nil {
		t.Fatal(err)
	}
	for line := range strings.SplitSeq(wrapped, "\n") {
		if displayWidth(line) > 92 {
			t.Fatalf("visible detail exceeds width 92: %q", line)
		}
	}
	if strings.Contains(out, "MARK") {
		t.Fatalf("rendered output = %q, unexpected MARK column", out)
	}
	if strings.Contains(out, "connected_ap_mld_not_seen_in_scan") {
		t.Fatalf("rendered output = %q, unexpected connected_ap_mld_not_seen_in_scan", out)
	}
	if strings.Contains(out, "scan Lab") {
		t.Fatalf("rendered output = %q, unexpected scan label", out)
	}
	if strings.Contains(out, "Legacy") {
		t.Fatalf("rendered output = %q, unexpected non-MLO AP", out)
	}
}

func TestRenderWifiScanBriefMLOFiltersAndExpandsAffiliatedLinks(t *testing.T) {
	result := &controlpb.CommandResult{
		Status: controlpb.CommandResult_STATUS_OK,
		Payload: &controlpb.CommandResult_WifiScan{
			WifiScan: &controlpb.WifiScan{
				Fields: []*controlpb.DiagnosticField{{Key: "requested_band", Value: "all"}},
				Results: []*controlpb.WifiScanResult{{
					Ssid:            "Lab",
					Bssid:           "aa:bb:cc:dd:ee:ff",
					RssiDbm:         -45,
					Band:            "6ghz",
					FrequencyMhz:    5975,
					WifiStandard:    "802.11be",
					ApMldMacAddress: "02:00:00:00:00:01",
					ApMloLinkId:     2,
					SecurityDetails: wifi7SecurityDetailsTestValue(),
					AffiliatedMloLinks: []*controlpb.MloLinkInfo{{
						ObservationFields: testObservationFields("identity", "rates"),
						LinkId:            1,
						State:             "idle",
						Band:              "5ghz",
						Channel:           44,
						RssiDbm:           -55,
						ApMacAddress:      "aa:bb:cc:dd:ee:01",
					}},
				}, {
					Ssid:         "GhostBE",
					Bssid:        "11:22:33:44:55:66",
					RssiDbm:      -60,
					Band:         "6ghz",
					FrequencyMhz: 6055,
					WifiStandard: "802.11be",
					ApMloLinkId:  -1,
				}, {
					Ssid:            "LegacyMLO",
					Bssid:           "22:33:44:55:66:77",
					RssiDbm:         -65,
					Band:            "5ghz",
					FrequencyMhz:    5180,
					WifiStandard:    "802.11ax",
					ApMldMacAddress: "03:00:00:00:00:01",
					ApMloLinkId:     -1,
				}},
			},
		},
	}

	out, err := CommandResult("agent", result, command.Options{WifiScanBrief: true, WifiScanMLO: true})
	if err != nil {
		t.Fatalf("CommandResult() error = %v", err)
	}
	for _, want := range []string{"Wi-Fi scan", "SSID", "BSSID", "MLD", "Lab", "aa:bb:cc:dd:ee:ff", "aa:bb:cc:dd:ee:01", "State: idle", "Band/CH: 5ghz/44", "LegacyMLO"} {
		if !strings.Contains(strings.Join(strings.Fields(out), " "), want) {
			t.Fatalf("rendered output = %q, missing %q", out, want)
		}
	}
	for _, unwanted := range []string{"GhostBE", "Scan Affiliated MLO Links", "Scan Wi-Fi Security Details", "ready", "strict", "W7SEC", "AP_LINK", "AFFILIATED", "[A]", "[L]"} {
		if strings.Contains(out, unwanted) {
			t.Fatalf("rendered output = %q, unexpected %q", out, unwanted)
		}
	}
	if strings.Contains(out, "STANDARD") {
		t.Fatalf("rendered output = %q, unexpected STANDARD column", out)
	}
	if count := strings.Count(out, "Lab"); count != 1 {
		t.Fatalf("rendered output expected single visible Lab label, got %d:\n%s", count, out)
	}
	affiliatedLine := renderedLineContaining(out, "aa:bb:cc:dd:ee:01")
	if affiliatedLine == "" {
		t.Fatalf("rendered output missing affiliated line:\n%s", out)
	}
	if !strings.HasPrefix(strings.TrimLeft(affiliatedLine, " "), "AP_MAC:") {
		t.Fatalf("affiliated identity is not a complete continuation: %s", affiliatedLine)
	}
	parentLine := renderedLineContaining(out, "aa:bb:cc:dd:ee:ff")
	if parentLine == "" {
		t.Fatalf("rendered output missing parent line:\n%s", out)
	}
	if compact := strings.Join(strings.Fields(out), " "); !strings.Contains(compact, "APs: shown=2 total=3") || !strings.Contains(compact, "Filter: mlo") {
		t.Fatalf("missing explicit filter/display counts: %s", out)
	}
}

func TestRenderWifiScanBriefMLOKeepsAPAndAffiliatedIdentityOrder(t *testing.T) {
	result := &controlpb.CommandResult{
		Status: controlpb.CommandResult_STATUS_OK,
		Payload: &controlpb.CommandResult_WifiScan{
			WifiScan: &controlpb.WifiScan{
				Fields: []*controlpb.DiagnosticField{{Key: "requested_band", Value: "all"}},
				Results: []*controlpb.WifiScanResult{{
					Ssid:            "hp2",
					Bssid:           "98:8f:00:f0:74:f0",
					RssiDbm:         -62,
					Band:            "5ghz",
					FrequencyMhz:    5520,
					WifiStandard:    "802.11be",
					SecurityTypes:   []string{"sae"},
					ApMldMacAddress: "98:8f:00:f0:75:10",
					ApMloLinkId:     1,
					SecurityDetails: &controlpb.WifiSecurityDetails{
						RsnxeCapabilities: []string{"sae_h2e"},
					},
					AffiliatedMloLinks: []*controlpb.MloLinkInfo{{
						ObservationFields: testObservationFields("identity"),
						LinkId:            0,
						State:             "unassociated",
						Band:              "6ghz",
						ApMacAddress:      "98:8f:00:f0:75:10",
					}},
				}, {
					Ssid:            "hp2",
					Bssid:           "98:8f:00:f0:75:10",
					RssiDbm:         -65,
					Band:            "6ghz",
					FrequencyMhz:    6175,
					WifiStandard:    "802.11be",
					SecurityTypes:   []string{"sae"},
					ApMldMacAddress: "98:8f:00:f0:75:10",
					ApMloLinkId:     0,
					SecurityDetails: &controlpb.WifiSecurityDetails{
						RsnxeCapabilities: []string{"sae_h2e"},
					},
					AffiliatedMloLinks: []*controlpb.MloLinkInfo{{
						ObservationFields: testObservationFields("identity"),
						LinkId:            1,
						State:             "unassociated",
						Band:              "5ghz",
						ApMacAddress:      "98:8f:00:f0:74:f0",
					}},
				}, {
					Ssid:            "cs5",
					Bssid:           "f0:d8:05:77:3b:07",
					RssiDbm:         -70,
					Band:            "5ghz",
					FrequencyMhz:    5580,
					WifiStandard:    "802.11be",
					SecurityTypes:   []string{"sae"},
					ApMldMacAddress: "f2:d8:05:77:3b:18",
					ApMloLinkId:     1,
					AffiliatedMloLinks: []*controlpb.MloLinkInfo{{
						ObservationFields: testObservationFields("identity"),
						LinkId:            2,
						State:             "unassociated",
						Band:              "6ghz",
						ApMacAddress:      "f0:d8:05:77:3b:0f",
					}},
				}},
			},
		},
	}

	out, err := CommandResult("agent", result, command.Options{WifiScanBrief: true, WifiScanMLO: true})
	if err != nil {
		t.Fatalf("CommandResult() error = %v", err)
	}
	rows := scanTableRows(out)
	if len(rows) < 6 {
		t.Fatalf("scan rows = %#v, want at least 6 rows\n%s", rows, out)
	}
	for _, mac := range []string{"98:8f:00:f0:75:10", "98:8f:00:f0:74:f0", "f0:d8:05:77:3b:07", "f0:d8:05:77:3b:0f"} {
		if indexOfRowContaining(rows, mac) < 0 {
			t.Fatalf("missing full AP/link MAC %s: %s", mac, out)
		}
	}
	compact := strings.Join(strings.Fields(out), " ")
	if len(metadataQAValues(out, "SSID")) != 3 || strings.Count(compact, "SSID: hp2") != 2 || strings.Count(compact, "SSID: cs5") != 1 {
		t.Fatalf("case-sensitive SSID no longer belongs to each AP record: %s", out)
	}
	for i, want := range []string{"BSSID: 98:8f:00:f0:74:f0", "AP_MAC: 98:8f:00:f0:75:10", "BSSID: 98:8f:00:f0:75:10", "AP_MAC: 98:8f:00:f0:74:f0", "BSSID: f0:d8:05:77:3b:07", "AP_MAC: f0:d8:05:77:3b:0f"} {
		if strings.Join(strings.Fields(rows[i]), " ") != want {
			t.Fatalf("AP/link identity order changed: %v", rows)
		}
	}
	if strings.Index(compact, "BSSID: 98:8f:00:f0:74:f0") > strings.Index(compact, "BSSID: 98:8f:00:f0:75:10") {
		t.Fatalf("APs not RSSI-descending within SSID: %s", out)
	}
	if strings.Contains(out, "---") {
		t.Fatalf("rendered output = %q, unexpected group separator", out)
	}
}

func TestRenderWifiScanBriefMLOIncludesUnknownStandardWithMetadata(t *testing.T) {
	result := &controlpb.CommandResult{
		Status: controlpb.CommandResult_STATUS_OK,
		Payload: &controlpb.CommandResult_WifiScan{
			WifiScan: &controlpb.WifiScan{
				Fields: []*controlpb.DiagnosticField{{Key: "requested_band", Value: "all"}},
				Results: []*controlpb.WifiScanResult{{
					Ssid:            "Meraki",
					Bssid:           "6e:ef:9d:c4:8e:70",
					RssiDbm:         -68,
					Band:            "6ghz",
					FrequencyMhz:    6295,
					WifiStandard:    "unknown",
					SecurityTypes:   []string{"sae"},
					ApMldMacAddress: "6e:ef:bd:c4:8e:71",
					ApMloLinkId:     2,
					AffiliatedMloLinks: []*controlpb.MloLinkInfo{{
						ObservationFields: testObservationFields("identity", "rates"),
						LinkId:            1,
						State:             "idle",
						Band:              "5ghz",
						Channel:           149,
						RssiDbm:           -70,
						ApMacAddress:      "6e:ef:9d:c4:8e:60",
					}},
					InformationElements: []*controlpb.WifiInformationElement{
						ehtMultiLinkTestIE(),
					},
				}, {
					Ssid:            "LegacyMLO",
					Bssid:           "22:33:44:55:66:77",
					RssiDbm:         -65,
					Band:            "5ghz",
					FrequencyMhz:    5180,
					WifiStandard:    "802.11ax",
					ApMldMacAddress: "03:00:00:00:00:01",
					ApMloLinkId:     1,
				}},
			},
		},
	}

	out, err := CommandResult("agent", result, command.Options{WifiScanBrief: true, WifiScanMLO: true})
	if err != nil {
		t.Fatalf("CommandResult() error = %v", err)
	}
	for _, want := range []string{"Meraki", "MLD", "6e:ef:9d:c4:8e:70", "6e:ef:9d:c4:8e:60", "APs: shown=2 total=2", "LegacyMLO"} {
		if !strings.Contains(strings.Join(strings.Fields(out), " "), want) {
			t.Fatalf("rendered output = %q, missing %q", out, want)
		}
	}
	for _, unwanted := range []string{"STANDARD", "W7SEC", "AP_LINK", "AFFILIATED", "[A]", "[L]"} {
		if strings.Contains(out, unwanted) {
			t.Fatalf("rendered output = %q, unexpected %q", out, unwanted)
		}
	}
}

func TestRenderWifiMLOFiltersScanAndConnection(t *testing.T) {
	result := &controlpb.CommandResult{
		Status: controlpb.CommandResult_STATUS_OK,
		Payload: &controlpb.CommandResult_WifiDiagnostics{
			WifiDiagnostics: &controlpb.WifiDiagnostics{
				Status: &controlpb.WifiStatus{
					Enabled: true,
					State:   "enabled",
					Connection: &controlpb.WifiConnection{
						Ssid:            "Other",
						Bssid:           "22:22:22:22:22:22",
						WifiStandard:    "802.11be",
						ApMldMacAddress: "02:00:00:00:00:02",
						ApMloLinkId:     2,
					},
				},
				Networks: []*controlpb.NetworkDiagnostics{{
					NetworkId: "100",
					IpStatus: &controlpb.IpStatus{
						Wifi: &controlpb.WifiConnection{
							Ssid:            "Lab",
							Bssid:           "aa:bb:cc:dd:ee:ff",
							WifiStandard:    "802.11be",
							ApMldMacAddress: "02:00:00:00:00:01",
							ApMloLinkId:     1,
						},
					},
				}, {
					NetworkId: "101",
					IpStatus: &controlpb.IpStatus{
						Wifi: &controlpb.WifiConnection{
							Ssid:            "Other",
							Bssid:           "22:22:22:22:22:22",
							WifiStandard:    "802.11be",
							ApMldMacAddress: "02:00:00:00:00:02",
							ApMloLinkId:     2,
						},
					},
				}},
				Scan: &controlpb.WifiScan{
					Results: []*controlpb.WifiScanResult{{
						Ssid:            "Lab",
						Bssid:           "aa:bb:cc:dd:ee:ff",
						RssiDbm:         -45,
						Band:            "6ghz",
						FrequencyMhz:    5975,
						WifiStandard:    "802.11be",
						ApMldMacAddress: "02:00:00:00:00:01",
						ApMloLinkId:     1,
						AffiliatedMloLinks: []*controlpb.MloLinkInfo{{
							LinkId:       2,
							ApMacAddress: "aa:bb:cc:dd:ee:01",
						}},
					}, {
						Ssid:            "Other",
						Bssid:           "22:22:22:22:22:22",
						RssiDbm:         -50,
						Band:            "6ghz",
						FrequencyMhz:    6055,
						WifiStandard:    "802.11be",
						ApMldMacAddress: "02:00:00:00:00:02",
						ApMloLinkId:     2,
					}},
				},
			},
		},
	}

	out, err := CommandResult("agent", result, command.Options{
		WifiRenderMode: command.WifiRenderModeEHT,
		WifiEHTSSID:    "Lab",
	})
	if err != nil {
		t.Fatalf("CommandResult(ssid) error = %v", err)
	}
	for _, want := range []string{
		"filter: ssid=Lab",
		"filtered_results: 1",
		"Network MLO",
		"Lab",
		"aa:bb:cc:dd:ee:ff",
		"no active Wi-Fi connection",
	} {
		if !strings.Contains(strings.Join(strings.Fields(out), " "), want) {
			t.Fatalf("rendered output = %q, missing %q", out, want)
		}
	}
	if strings.Contains(out, "22:22:22:22:22:22") {
		t.Fatalf("rendered output = %q, unexpected filtered BSSID", out)
	}

	out, err = CommandResult("agent", result, command.Options{
		WifiRenderMode: command.WifiRenderModeEHT,
		WifiEHTBSSID:   "AA:BB:CC:DD:EE:01",
	})
	if err != nil {
		t.Fatalf("CommandResult(bssid) error = %v", err)
	}
	for _, want := range []string{
		"filter: bssid=AA:BB:CC:DD:EE:01",
		"filtered_results: 1",
		"aa:bb:cc:dd:ee:ff",
		"affiliated_links",
		"    [-] affiliated link",
		"Identity: type=aff link=2 ap_mac=aa:bb:cc:dd:ee:01",
	} {
		if !strings.Contains(strings.Join(strings.Fields(out), " "), strings.Join(strings.Fields(want), " ")) {
			t.Fatalf("rendered output = %q, missing %q", out, want)
		}
	}
	if strings.Contains(out, "Other") || strings.Contains(out, "22:22:22:22:22:22") {
		t.Fatalf("rendered output = %q, unexpected filtered network", out)
	}
}

func TestRenderWifiMLODetailedEHTMultiLinkSubelements(t *testing.T) {
	result := &controlpb.CommandResult{
		Status: controlpb.CommandResult_STATUS_OK,
		Payload: &controlpb.CommandResult_WifiDiagnostics{
			WifiDiagnostics: &controlpb.WifiDiagnostics{
				Status: &controlpb.WifiStatus{
					Enabled: true,
					State:   "enabled",
				},
				Scan: &controlpb.WifiScan{
					Results: []*controlpb.WifiScanResult{{
						Ssid:         "Lab",
						Bssid:        "aa:bb:cc:dd:ee:ff",
						RssiDbm:      -50,
						Band:         "6ghz",
						FrequencyMhz: 5975,
						WifiStandard: "802.11be",
						ApMloLinkId:  -1,
						InformationElements: []*controlpb.WifiInformationElement{
							detailedEHTMultiLinkTestIE(),
						},
					}},
				},
			},
		},
	}

	out, err := CommandResult("agent", result, command.Options{WifiRenderMode: command.WifiRenderModeEHT})
	if err != nil {
		t.Fatalf("CommandResult() error = %v", err)
	}
	for _, want := range []string{
		"subelement id=0 name=per_sta_profile len=22 actual=22 fragments=1 reassembled=30",
		"fragment target_id=0 target=per_sta_profile bytes=8 payload=0x0102ff046c010203",
		"profile_ie link_id=2 id=0 name=ssid len=3 actual=3 body=0x4c6162",
		"profile_ie link_id=2 id=255 ext=106 name=eht_operation len=3 actual=3 body=0x6a0102",
		"profile_ie link_id=2 id=255 ext=108 name=eht_capabilities len=4 actual=4 body=0x6c010203",
		"profile_decode link_id=2",
		"eht_operation_warnings eht_operation_too_short bytes=2 required=5",
		"eht_capabilities_warnings eht_capabilities_too_short bytes=3 required=11",
		"subelement id=221 name=vendor_specific len=6 actual=6",
		"vendor oui=00:11:22 type=7 payload_bytes=2 payload=0x99aa",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("rendered output = %q, missing %q", out, want)
		}
	}
}

func TestRenderWifiMLOUsesEHTMultiLinkElementFallback(t *testing.T) {
	result := &controlpb.CommandResult{
		Status: controlpb.CommandResult_STATUS_OK,
		Payload: &controlpb.CommandResult_WifiDiagnostics{
			WifiDiagnostics: &controlpb.WifiDiagnostics{
				Status: &controlpb.WifiStatus{
					Enabled: true,
					State:   "enabled",
					Connection: &controlpb.WifiConnection{
						Ssid:              "Lab",
						Bssid:             "aa:bb:cc:dd:ee:ff",
						ObservationFields: testObservationFields("identity"),
						WifiStandard:      "802.11ax",
						ApMloLinkId:       -1,
						InformationElements: []*controlpb.WifiInformationElement{
							detailedEHTMultiLinkTestIE(),
						},
					},
				},
				Scan: &controlpb.WifiScan{
					Results: []*controlpb.WifiScanResult{{
						Ssid:         "Lab",
						Bssid:        "aa:bb:cc:dd:ee:ff",
						RssiDbm:      -50,
						Band:         "6ghz",
						FrequencyMhz: 5975,
						WifiStandard: "802.11ax",
						ApMloLinkId:  -1,
						InformationElements: []*controlpb.WifiInformationElement{
							detailedEHTMultiLinkTestIE(),
						},
					}},
				},
			},
		},
	}

	out, err := CommandResult("agent", result, command.Options{WifiRenderMode: command.WifiRenderModeEHT})
	if err != nil {
		t.Fatalf("CommandResult() error = %v", err)
	}
	for _, want := range []string{
		"Connected MLO",
		"connected_ap_mld",
		"same_mld_results",
		"visible_links",
		"ap_mld",
		"02:00:00:00:00:01",
		"ap_link_id",
		"eht_candidates",
		"[*] Lab",
		"ap_mld=02:00:00:00:00:01 link=2 bssid=aa:bb:cc:dd:ee:ff",
		"Diagnostics / Warnings\n  Warnings: none",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("rendered output = %q, missing %q", out, want)
		}
	}
	for _, unwanted := range []string{
		"no EHT-capable scan results",
		"scan_mlo_metadata_unavailable",
	} {
		if strings.Contains(out, unwanted) {
			t.Fatalf("rendered output = %q, unexpected %q", out, unwanted)
		}
	}
}

func TestRenderWifiMLOHidesPlaceholderConnectionAndPreservesFullNearbyValues(t *testing.T) {
	result := &controlpb.CommandResult{
		Status: controlpb.CommandResult_STATUS_OK,
		Payload: &controlpb.CommandResult_WifiDiagnostics{
			WifiDiagnostics: &controlpb.WifiDiagnostics{
				Status: &controlpb.WifiStatus{
					Enabled: true,
					State:   "enabled",
					Connection: &controlpb.WifiConnection{
						Ssid:         "<unknown ssid>",
						Bssid:        "02:00:00:00:00:00",
						NetworkId:    -1,
						WifiStandard: "802.11ax",
					},
				},
				Scan: &controlpb.WifiScan{
					Results: []*controlpb.WifiScanResult{{
						Ssid:         "very-long-laboratory-network-name-that-would-wrap-the-table",
						Bssid:        "aa:bb:cc:dd:ee:ff",
						RssiDbm:      -55,
						Band:         "6GHz",
						FrequencyMhz: 6295,
						WifiStandard: "802.11be",
						ApMloLinkId:  -1,
						SecurityTypes: []string{
							"sae",
						},
					}, {
						Ssid:         "獅子丸新百合ヶ丘店",
						Bssid:        "aa:bb:cc:dd:ee:01",
						RssiDbm:      -62,
						Band:         "2.4GHz",
						FrequencyMhz: 2457,
						WifiStandard: "802.11be",
						ApMloLinkId:  -1,
						SecurityTypes: []string{
							"psk",
						},
					}},
				},
			},
		},
	}

	out, err := CommandResult("agent", result, command.Options{WifiRenderMode: command.WifiRenderModeEHT})
	if err != nil {
		t.Fatalf("CommandResult() error = %v", err)
	}
	for _, want := range []string{
		"Current AP Relation\n  Observation: no active Wi-Fi connection",
		"Connected MLO\n  Observation: no active Wi-Fi connection",
		"scan_mlo_metadata_unavailable 11be_results=2 observed_ap_mld=0 observed_link_id=0",
		"EHT Scan Links",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("rendered output = %q, missing %q", out, want)
		}
	}
	if strings.Contains(out, "[-] 獅子丸新百合ヶ丘店") {
		t.Fatalf("rendered output = %q, unexpected metadata-absent MLO marker", out)
	}
	for _, unwanted := range []string{"<unknown ssid>", "802.11ax"} {
		if strings.Contains(out, unwanted) {
			t.Fatalf("rendered output = %q, unexpected %q", out, unwanted)
		}
	}

	wrapped, err := CommandResult("agent", result, command.Options{WifiRenderMode: command.WifiRenderModeEHT}, Presentation{Width: 80})
	if err != nil {
		t.Fatal(err)
	}
	for _, ap := range result.GetWifiDiagnostics().GetScan().GetResults() {
		for _, text := range []string{out, wrapped} {
			if !strings.Contains(alignmentCompact(text), alignmentCompact(ap.Ssid)) || !strings.Contains(alignmentCompact(text), ap.Bssid) {
				t.Fatalf("nearby AP lost full identity: %s", text)
			}
		}
	}
	for line := range strings.SplitSeq(wrapped, "\n") {
		if displayWidth(line) > 80 {
			t.Fatalf("display width=%d exceeds 80: %q", displayWidth(line), line)
		}
	}
	if strings.Contains(out, "...") {
		t.Fatalf("nearby AP identity was truncated: %s", out)
	}
}

func TestAdvertisedEHTLinkIsNotAnAssociatedLink(t *testing.T) {
	ie := detailedEHTMultiLinkTestIE()
	if ids := wifiMLOLinkIDsFromElements([]*controlpb.WifiInformationElement{ie}); len(ids) == 0 {
		t.Fatalf("fixture needs an advertised link, got %v", ids)
	}
	connection := &controlpb.WifiConnection{InformationElements: []*controlpb.WifiInformationElement{ie}}
	if got := wifiMLOAssociatedLinkIDSet(connection); len(got) != 0 {
		t.Fatalf("advertised profile was promoted to associated: %v", got)
	}
}

func TestEHTScanComparisonNeedsActualUpdateAndKnownRecentAge(t *testing.T) {
	connected := &controlpb.WifiConnection{
		ApMldMacAddress:    "02:00:00:00:00:01",
		ObservationFields:  testObservationFields("ap_mld_mac_address", "associated_mlo_links"),
		AssociatedMloLinks: []*controlpb.MloLinkInfo{{LinkId: 2, ObservationFields: testObservationFields("identity")}},
	}
	scan := &controlpb.WifiScan{Fields: []*controlpb.DiagnosticField{{Key: "fresh_scan_results_updated", Value: "true"}}}
	ap := &controlpb.WifiScanResult{ApMldMacAddress: connected.ApMldMacAddress}
	if got := wifiMLOComparison(connected, scan, []*controlpb.WifiScanResult{ap}); got != "unknown_scan_age" {
		t.Fatalf("unknown boot age was considered comparable: %s", got)
	}
	age := uint64(0)
	ap.ObservationAgeMs = &age
	if got := wifiMLOComparison(connected, scan, []*controlpb.WifiScanResult{ap}); got != "updated_scan_age_le_10s" {
		t.Fatalf("known-zero boot age was discarded: %s", got)
	}
	age = 10001
	if got := wifiMLOComparison(connected, scan, []*controlpb.WifiScanResult{ap}); got != "unknown_scan_age" {
		t.Fatalf("stale scan was considered comparable: %s", got)
	}
	deleteFresh := &controlpb.WifiScan{}
	if got := wifiMLOComparison(connected, deleteFresh, []*controlpb.WifiScanResult{ap}); got != "unknown_scan_update" {
		t.Fatalf("requested scan without actual update was considered comparable: %s", got)
	}
}

func ehtMultiLinkTestIE() *controlpb.WifiInformationElement {
	return &controlpb.WifiInformationElement{
		Id:        255,
		IdExt:     107,
		ByteCount: 37,
		BytesHex:  "f00712020000000001020710328f083370056100000f72090c0200000000026400010305dd",
	}
}

func detailedEHTMultiLinkTestIE() *controlpb.WifiInformationElement {
	return &controlpb.WifiInformationElement{
		Id:        255,
		IdExt:     107,
		ByteCount: 53,
		BytesHex:  "3000090200000000010207001672090c020000000002640001030500034c6162ff036afe080102ff046c010203dd060011220799aa",
	}
}

func rnrTestIE() *controlpb.WifiInformationElement {
	return &controlpb.WifiInformationElement{
		Id:        201,
		ByteCount: 20,
		BytesHex:  "001085050aaabbccddeeff112233448015073210",
	}
}

func rmEnabledCapabilitiesTestIE() *controlpb.WifiInformationElement {
	return &controlpb.WifiInformationElement{
		Id:        70,
		ByteCount: 5,
		BytesHex:  "0000000000",
	}
}

func extendedCapabilitiesBssTransitionTestIE() *controlpb.WifiInformationElement {
	return &controlpb.WifiInformationElement{
		Id:        127,
		ByteCount: 3,
		BytesHex:  "000008",
	}
}

func mobilityDomainTestIE() *controlpb.WifiInformationElement {
	return &controlpb.WifiInformationElement{
		Id:        54,
		ByteCount: 3,
		BytesHex:  "010203",
	}
}

func multipleBSSIDTestIE() *controlpb.WifiInformationElement {
	return &controlpb.WifiInformationElement{
		Id:        71,
		ByteCount: 55,
		BytesHex:  "02003400034c61625502040353023412ff05380130016a301e0100000fac090100000fac090200000fac18000fac19c0000000000fac0c",
	}
}

func he6GhzCapabilitiesTestValue() *controlpb.WifiHe6GhzCapabilities {
	return &controlpb.WifiHe6GhzCapabilities{
		RawHex:                      "ad3e",
		Capabilities:                0x3ead,
		MinimumMpduStartSpacing:     "4us",
		MaxAmpduLengthExponent:      5,
		MaxAmpduLengthBytes:         262143,
		MaxMpduLengthBytes:          11454,
		SmPowerSave:                 "disabled",
		RdResponder:                 true,
		RxAntennaPatternConsistency: true,
		TxAntennaPatternConsistency: true,
		Features:                    []string{"max_mpdu_length_bytes=11454", "max_ampdu_length_bytes=262143"},
	}
}

func wifi7SecurityDetailsTestValue() *controlpb.WifiSecurityDetails {
	return &controlpb.WifiSecurityDetails{
		RsnPresent:                  true,
		RsnVersion:                  1,
		GroupDataCipher:             "gcmp_256",
		PairwiseCiphers:             []string{"gcmp_256"},
		AkmSuites:                   []string{"sae_gdh", "ft_sae_gdh"},
		RsnCapabilities:             0x00c0,
		RsnCapabilitiesHex:          "00c0",
		PmfCapable:                  true,
		PmfRequired:                 true,
		GroupManagementCipher:       "bip_gmac_256",
		Gcmp_256:                    true,
		SaeGdh:                      true,
		FtSaeGdh:                    true,
		Wifi7PersonalReady:          true,
		RsnxePresent:                true,
		RsnxeCapabilities:           []string{"sae_h2e", "ssid_protection"},
		ExtendedCapabilitiesPresent: true,
		ExtendedCapabilities:        []string{"bss_transition", "beacon_protection"},
		BeaconProtection:            true,
		RawRsnHex:                   "0100000fac090100000fac090200000fac18000fac19c0000000000fac0c",
		RawRsnxeHex:                 "200020",
		RawExtendedCapabilitiesHex:  "0000080000000000000010",
	}
}

func ehtCapabilitiesTestValue() *controlpb.WifiEhtCapabilities {
	return &controlpb.WifiEhtCapabilities{
		MacCapabilitiesHex: "774e",
		PhyCapabilitiesHex: "f61fffffff7ff8ff03",
		Features:           []string{"320mhz_in_6ghz", "rx_4096qam_wider_dl_ofdma"},
		Mac: &controlpb.WifiEhtMacCapabilities{
			EpcsPriorityAccess:            true,
			OmControl:                     true,
			TriggeredTxopSharingMode1:     true,
			ScsTrafficDescription:         true,
			RestrictedTwt:                 true,
			MaxMpduLengthBytes:            7991,
			LinkAdaptation:                "no_feedback",
			EhtTrs:                        true,
			TxopReturn:                    true,
			TwoBqrs:                       true,
			UnsolicitedEpcsPriorityAccess: true,
		},
		Phy: &controlpb.WifiEhtPhyCapabilities{
			Supports_320MhzIn_6Ghz:     true,
			Supports_242ToneRuGt_20Mhz: true,
			BeamformeeSs_80Mhz:         3,
			BeamformeeSs_160Mhz:        7,
			BeamformeeSs_320Mhz:        0,
			SoundingDimensions_80Mhz:   7,
			SoundingDimensions_160Mhz:  7,
			SoundingDimensions_320Mhz:  7,
			CommonNominalPacketPadding: "20us",
			MaxSupportedEhtLtf:         3,
			ExtraEhtLtfSupported:       true,
			Mcs15Supported_80Mhz:       true,
			Mcs15Supported_160Mhz:      true,
			Mcs15Supported_320Mhz:      true,
			NonOfdmaUlMuMimo_320Mhz:    true,
			MuBeamformer_320Mhz:        true,
			Rx_4096QamWiderBwDlOfdma:   true,
		},
		McsNss: []*controlpb.WifiMcsNssSupport{{
			Standard: "eht", Bandwidth: "le_80mhz", Direction: "rx", McsRange: "0-9", MaxNss: 1,
		}, {
			Standard: "eht", Bandwidth: "le_80mhz", Direction: "tx", McsRange: "0-9", MaxNss: 2,
		}},
		PpeThresholdsPresent: true,
		PpeNssCount:          2,
		PpeRuIndices:         []string{"242-tone", "4x996-tone"},
		PpeThresholdsHex:     "f10102030405060708",
	}
}

func heCapabilitiesTestValue() *controlpb.WifiHeCapabilities {
	return &controlpb.WifiHeCapabilities{
		Features: []string{"twt_responder", "punctured_sounding"},
		Mac: &controlpb.WifiHeMacCapabilities{
			TwtResponder:      true,
			OmControl:         true,
			PuncturedSounding: true,
		},
		Phy: &controlpb.WifiHePhyCapabilities{
			ChannelWidthSet:       []string{"40_80mhz_in_5ghz"},
			PreamblePuncturingRx:  []string{"preamble_puncturing_rx_80mhz_second_20mhz"},
			DcmMaxConstellationTx: "bpsk",
			DcmMaxNssTx:           1,
			DcmMaxConstellationRx: "qpsk",
			DcmMaxNssRx:           2,
			SuBeamformer:          true,
			SrpBasedSpatialReuse:  true,
			NominalPacketPadding:  "16us",
		},
	}
}

func heOperationTestValue() *controlpb.WifiHeOperation {
	return &controlpb.WifiHeOperation{
		BssColor:         17,
		BssColorDisabled: false,
		Flags:            []string{"partial_bss_color"},
	}
}

func heSpatialReuseTestValue() *controlpb.WifiHeSpatialReuseParameterSet {
	return &controlpb.WifiHeSpatialReuseParameterSet{
		SrControl:                0x08,
		Flags:                    []string{"srg_information_present"},
		SrgObssPdMinOffset:       20,
		SrgObssPdMaxOffset:       30,
		SrgBssColorBitmapHex:     "0102030405060708",
		SrgPartialBssidBitmapHex: "1112131415161718",
	}
}

func ehtOperationTestValue() *controlpb.WifiEhtOperation {
	return &controlpb.WifiEhtOperation{
		Parameters:                         0x43,
		Flags:                              []string{"disabled_subchannel_bitmap_present", "mcs15_disabled"},
		BasicMcsNssSetHex:                  "11223344",
		ChannelWidth:                       "320MHz",
		CenterFreqSegment0:                 31,
		CenterFreqSegment1:                 63,
		DisabledSubchannelBitmap:           0x0a,
		OperationInformationPresent:        true,
		DisabledSubchannelBitmapPresent:    true,
		ChannelWidthCode:                   4,
		ChannelWidthMhz:                    320,
		DisabledSubchannelBitmapHex:        "000a",
		DisabledSubchannelIndices:          []uint32{1, 3},
		Mcs15Disabled:                      true,
		GroupAddressedBuIndicationExponent: 0,
		BasicMcsNss: []*controlpb.WifiMcsNssSupport{{
			Standard: "eht", Bandwidth: "20mhz_only", Direction: "rx", McsRange: "0-7", MaxNss: 1,
		}, {
			Standard: "eht", Bandwidth: "20mhz_only", Direction: "tx", McsRange: "0-7", MaxNss: 1,
		}},
	}
}
