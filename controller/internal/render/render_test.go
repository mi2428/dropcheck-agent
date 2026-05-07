package render

import (
	"encoding/json"
	"strings"
	"testing"

	"dropcheck/controller/internal/command"
	"dropcheck/controller/internal/controlpb"
	"dropcheck/controller/internal/pipeline"
)

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

	out, err := CommandResult("agent", result, command.Options{}, pipeline.FormatText)
	if err != nil {
		t.Fatalf("renderCommandResult() error = %v", err)
	}
	if !strings.Contains(out, "Latency: 42ms\n") {
		t.Fatalf("rendered output = %q, missing payload latency", out)
	}
}

func TestRenderConfigShowsStandaloneOnly(t *testing.T) {
	view := ConfigView{
		Standalone: &controlpb.StandaloneConfig{Enabled: true},
	}
	text, err := Config(view, pipeline.FormatText)
	if err != nil {
		t.Fatalf("Config(text) error = %v", err)
	}
	if !strings.Contains(text, "standalone {\n  enabled\n}") {
		t.Fatalf("Config(text) = %q, missing standalone block", text)
	}
	if strings.Contains(text, "controller") {
		t.Fatalf("Config(text) = %q, included controller config", text)
	}

	raw, err := Config(view, pipeline.FormatJSON)
	if err != nil {
		t.Fatalf("Config(json) error = %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("Config(json) invalid JSON: %v\n%s", err, raw)
	}
	if _, ok := got["controller"]; ok {
		t.Fatalf("Config(json) = %#v, included controller config", got)
	}
}

func TestRenderConfigShowsStandaloneUpload(t *testing.T) {
	view := ConfigView{
		Standalone: &controlpb.StandaloneConfig{
			Upload: &controlpb.StandaloneUploadConfig{
				Url: "http://192.168.50.10:8080/dropcheck/incoming",
				Wifi: &controlpb.ConnectWifi{
					Ssid:             "NOC",
					Passphrase:       "secret",
					Security:         controlpb.ConnectWifi_SECURITY_WPA3_SAE,
					Band:             controlpb.WifiBand_WIFI_BAND_6_GHZ,
					MacRandomization: controlpb.ConnectWifi_MAC_RANDOMIZATION_NON_PERSISTENT,
					TimeoutMs:        5000,
				},
			},
		},
	}
	text, err := Config(view, pipeline.FormatText)
	if err != nil {
		t.Fatalf("Config(text) error = %v", err)
	}
	for _, want := range []string{
		"upload {\n",
		"to http://192.168.50.10:8080/dropcheck/incoming\n",
		"via wifi {\n",
		"essid NOC\n",
		"passphrase <redacted>\n",
		"security wpa3\n",
		"band 6ghz\n",
		"mac-randomization non-persistent\n",
		"timeout 5s\n",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("Config(text) = %q, missing %q", text, want)
		}
	}
	if strings.Contains(text, "secret") {
		t.Fatalf("Config(text) leaked passphrase: %q", text)
	}
}

func TestRenderConfigShowsStandaloneFestaWifi(t *testing.T) {
	view := ConfigView{
		Standalone: &controlpb.StandaloneConfig{
			Festas: []*controlpb.StandaloneFesta{{
				Name:    "smoke",
				Enabled: true,
				WifiGroups: []*controlpb.StandaloneWifiGroup{{
					Name:             "mgmt",
					Essid:            "NOC",
					Passphrase:       "secret",
					Security:         controlpb.ConnectWifi_SECURITY_WPA2_WPA3_TRANSITION,
					MacRandomization: controlpb.ConnectWifi_MAC_RANDOMIZATION_PERSISTENT,
				}},
			}},
		},
	}
	text, err := Config(view, pipeline.FormatText)
	if err != nil {
		t.Fatalf("Config(text) error = %v", err)
	}
	for _, want := range []string{
		"wifi mgmt {\n",
		"match essid NOC\n",
		"passphrase <redacted>\n",
		"security transition\n",
		"mac-randomization persistent\n",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("Config(text) = %q, missing %q", text, want)
		}
	}
	if strings.Contains(text, "secret") {
		t.Fatalf("Config(text) leaked passphrase: %q", text)
	}
}

func TestRenderConfigShowsStandaloneFestaChecks(t *testing.T) {
	view := ConfigView{
		Standalone: &controlpb.StandaloneConfig{
			Festas: []*controlpb.StandaloneFesta{{
				Name: "smoke",
				Checks: []*controlpb.StandaloneCheck{
					{
						Name: "cloudflare",
						Test: &controlpb.StandaloneCheck_Ping{Ping: &controlpb.StandalonePingCheck{
							Host:      "1.1.1.1",
							Count:     1,
							TimeoutMs: 8000,
						}},
					},
					{
						Name: "dns-main",
						Test: &controlpb.StandaloneCheck_Dns{Dns: &controlpb.StandaloneDnsCheck{
							Name:   "example.test",
							Qtypes: []controlpb.DnsRecordType{controlpb.DnsRecordType_DNS_RECORD_TYPE_AAAA},
						}},
					},
				},
			}},
		},
	}
	text, err := Config(view, pipeline.FormatText)
	if err != nil {
		t.Fatalf("Config(text) error = %v", err)
	}
	for _, want := range []string{
		"check cloudflare {\n",
		"test ping\n",
		"host 1.1.1.1\n",
		"count 1\n",
		"timeout 8s\n",
		"check dns-main {\n",
		"test dns\n",
		"name example.test\n",
		"type AAAA\n",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("Config(text) = %q, missing %q", text, want)
		}
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

	out, err := CommandResult("agent", result, command.Options{}, pipeline.FormatText)
	if err != nil {
		t.Fatalf("renderCommandResult() error = %v", err)
	}
	if !strings.Contains(out, "Latency: 7ms\n") {
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

	out, err := CommandResult("agent", result, command.Options{}, pipeline.FormatText)
	if err != nil {
		t.Fatalf("renderCommandResult() error = %v", err)
	}
	for _, want := range []string{"Connection\n", "channel", "40", "bandwidth", "80MHz"} {
		if !strings.Contains(out, want) {
			t.Fatalf("rendered output = %q, missing %q", out, want)
		}
	}
	if strings.Count(out, "\nConnection\n") != 1 {
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
					Ssid:            "Lab",
					Bssid:           "aa:bb:cc:dd:ee:ff",
					FrequencyMhz:    5975,
					LinkSpeedMbps:   1200,
					TxLinkSpeedMbps: 900,
					RxLinkSpeedMbps: 1200,
					MacAddress:      "02:00:00:00:00:09",
					WifiStandard:    "802.11be",
					SecurityType:    "wpa3_sae",
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
					NetworkId:        "102",
					Transports:       []string{"wifi"},
					Capabilities:     []string{"internet", "validated", "not_roaming", "not_metered"},
					DownstreamKbps:   1200000,
					UpstreamKbps:     600000,
					SignalStrength:   -52,
					RawCapabilities:  "raw_caps",
					Addresses:        []string{"192.0.2.10/24", "2001:db8::10/64", "2001:db8::11/64"},
					DnsServers:       []string{"192.0.2.1", "1.1.1.1"},
					DhcpServer:       "192.0.2.254",
					Routes:           []string{"0.0.0.0/0 -> 192.0.2.1 wlan0", "192.0.2.0/24 -> 0.0.0.0 wlan0"},
					PrivateDnsActive: true,
				},
			},
		},
	}

	out, err := CommandResult("agent", result, command.Options{}, pipeline.FormatText)
	if err != nil {
		t.Fatalf("renderCommandResult() error = %v", err)
	}
	for _, want := range []string{
		"AP Capabilities",
		"permissions\n    all_granted\n    ACCESS_FINE_LOCATION\n    NEARBY_WIFI_DEVICES",
		"roaming",
		"11r",
		"11k",
		"11v_bss_transition",
		"phy",
		"eht",
		"roaming\n    11k\n    11r\n    11v_bss_transition",
		"link",
		"tx=900Mbps rx=1200Mbps",
		"sta_mac",
		"02:00:00:00:00:09",
		"Network",
		"not_metered",
		"capabilities\n    not_metered\n    not_roaming",
		"bandwidth",
		"down=1200000kbps up=600000kbps",
		"signal_strength",
		"-52",
		"ipv4",
		"192.0.2.10/24",
		"ipv6\n    2001:db8::10/64\n    2001:db8::11/64",
		"dns",
		"192.0.2.1",
		"dns\n    192.0.2.1\n    1.1.1.1",
		"dhcp_server",
		"192.0.2.254",
		"private_dns",
		"active=true server=none",
		"routes",
		"0.0.0.0/0 -> 192.0.2.1 wlan0",
		"routes\n    0.0.0.0/0 -> 192.0.2.1 wlan0\n    192.0.2.0/24 -> 0.0.0.0 wlan0",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("rendered output = %q, missing %q", out, want)
		}
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
		"internet,validated,not_metered",
		"all_granted ACCESS_FINE_LOCATION,NEARBY_WIFI_DEVICES",
		"not_metered,not_roaming",
		"192.0.2.1,1.1.1.1",
		"0.0.0.0/0 -> 192.0.2.1 wlan0 | 192.0.2.0/24 -> 0.0.0.0 wlan0",
		"11k,11r,11v_bss_transition",
		"security=wpa3_sae",
	} {
		if strings.Contains(out, unwanted) {
			t.Fatalf("rendered output = %q, included unwanted %q", out, unwanted)
		}
	}
	if !(strings.Index(out, "11k") < strings.Index(out, "11r") &&
		strings.Index(out, "11r") < strings.Index(out, "11v_bss_transition")) {
		t.Fatalf("rendered output = %q, capability rows are not sorted", out)
	}
}

func TestRenderWifiStatusPreservesConnectionNetworkFallback(t *testing.T) {
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

	out, err := CommandResult("agent", result, command.Options{}, pipeline.FormatText)
	if err != nil {
		t.Fatalf("renderCommandResult() error = %v", err)
	}
	for _, want := range []string{"Network\n", "id", "119", "ipv4", "192.0.2.10"} {
		if !strings.Contains(out, want) {
			t.Fatalf("rendered output = %q, missing %q", out, want)
		}
	}
	if strings.Contains(out, "\n  ip ") {
		t.Fatalf("rendered output = %q, included stale connection ip row", out)
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

	out, err := CommandResult("agent", result, command.Options{}, pipeline.FormatText)
	if err != nil {
		t.Fatalf("renderCommandResult() error = %v", err)
	}
	if strings.Contains(out, "signal_strength") {
		t.Fatalf("rendered output = %q, included duplicate signal_strength", out)
	}
	if !strings.Contains(out, "not_metered") {
		t.Fatalf("rendered output = %q, missing non-duplicate network capability", out)
	}
}

func TestRenderWifiStatusShowsMLOFields(t *testing.T) {
	result := &controlpb.CommandResult{
		Status: controlpb.CommandResult_STATUS_OK,
		Payload: &controlpb.CommandResult_WifiStatus{
			WifiStatus: &controlpb.WifiStatus{
				Enabled: true,
				Connection: &controlpb.WifiConnection{
					Ssid:            "Lab",
					Bssid:           "aa:bb:cc:dd:ee:ff",
					FrequencyMhz:    5975,
					ApMldMacAddress: "02:00:00:00:00:01",
					ApMloLinkId:     2,
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

	out, err := CommandResult("agent", result, command.Options{}, pipeline.FormatText)
	if err != nil {
		t.Fatalf("renderCommandResult() error = %v", err)
	}
	for _, want := range []string{
		"MLO\n",
		"ap_mld",
		"02:00:00:00:00:01",
		"ap_link_id",
		"MLO Links",
		"associated",
		"active",
		"1200",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("rendered output = %q, missing %q", out, want)
		}
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
					RssiDbm:         -48,
					Band:            "6ghz",
					FrequencyMhz:    5975,
					WifiStandard:    "802.11be",
					SecurityTypes:   []string{"wpa3_sae"},
					ApMldMacAddress: "02:00:00:00:00:01",
					ApMloLinkId:     2,
					AffiliatedMloLinks: []*controlpb.MloLinkInfo{{
						LinkId:          2,
						State:           "active",
						Band:            "6ghz",
						Channel:         5,
						RssiDbm:         -48,
						TxLinkSpeedMbps: 1200,
						RxLinkSpeedMbps: 1200,
						ApMacAddress:    "02:00:00:00:00:02",
						StaMacAddress:   "02:00:00:00:00:03",
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

	out, err := CommandResult("agent", result, command.Options{}, pipeline.FormatText)
	if err != nil {
		t.Fatalf("renderCommandResult() error = %v", err)
	}
	for _, want := range []string{
		"requested_band",
		"results",
		"total",
		"AP_MLD",
		"AP_LINK",
		"AFFILIATED",
		"FLAGS",
		"02:00:00:00:00:01",
		"Scan Affiliated MLO Links",
		"active",
		"1200",
		"<none>",
		"11k,11r,11v_bss_transition",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("rendered output = %q, missing %q", out, want)
		}
	}
	for _, unwanted := range []string{
		"ANDROID_CAPABILITIES",
		"[RSN-SAE-CCMP][ESS]",
		"he_capabilities",
		"Scan Connection Capabilities",
		"Scan Information Elements",
		"000008",
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
		"\nFields\n",
		"\nScan Results\n",
	} {
		if strings.Contains(out, unwanted) {
			t.Fatalf("rendered output = %q, included unwanted heading %q", out, unwanted)
		}
	}
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
						Ssid:            "Lab",
						Bssid:           "aa:bb:cc:dd:ee:ff",
						FrequencyMhz:    5975,
						WifiStandard:    "802.11be",
						ApMldMacAddress: "02:00:00:00:00:01",
						ApMloLinkId:     2,
						InformationElements: []*controlpb.WifiInformationElement{
							ehtMultiLinkTestIE(),
						},
						AssociatedMloLinks: []*controlpb.MloLinkInfo{{
							LinkId:       2,
							State:        "active",
							Band:         "6ghz",
							Channel:      5,
							RssiDbm:      -45,
							ApMacAddress: "aa:bb:cc:dd:ee:ff",
						}},
					},
				},
				Capabilities: &controlpb.WifiCapabilities{
					SupportedStandards: []string{"802.11ax", "802.11be"},
					SupportedFeatures:  []string{"tid_to_link_mapping_negotiation"},
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
						Ssid:            "Lab",
						Bssid:           "aa:bb:cc:dd:ee:ff",
						RssiDbm:         -45,
						Band:            "6ghz",
						FrequencyMhz:    5975,
						ChannelWidth:    "CHANNEL_WIDTH_80MHZ",
						WifiStandard:    "802.11be",
						ApMldMacAddress: "02:00:00:00:00:01",
						ApMloLinkId:     2,
						InformationElements: []*controlpb.WifiInformationElement{
							ehtMultiLinkTestIE(),
						},
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

	out, err := CommandResult("agent", result, command.Options{WifiRenderMode: command.WifiRenderModeMLO}, pipeline.FormatText)
	if err != nil {
		t.Fatalf("CommandResult() error = %v", err)
	}
	for _, want := range []string{
		"Current AP Relation",
		"Connected MLO",
		"MLO Scan",
		"Nearby MLO APs",
		"MLO Scan Links",
		"Network MLO",
		"MLO Capability Signals",
		"[*] Lab",
		"ap_mld=02:00:00:00:00:01 link=2 bssid=aa:bb:cc:dd:ee:ff",
		"band=6ghz ch=5 freq=5975MHz width=80MHz rssi=-45dBm",
		"[+] affiliated Lab",
		"Connected EHT Multi-Link Elements",
		"ml_control raw=0x0030 type=basic(0) presence=link_id_info,bss_parameters_change_count bytes=28",
		"common_info len=9 mld_mac=02:00:00:00:00:01 link_id=2 bss_param_change_count=7",
		"per_link link_id=2 control=0x0972 complete=true",
		"sta_info_len=12 sta_mac=02:00:00:00:00:02 beacon_interval_tu=100",
		"dtim=1/3",
		"Scan EHT Multi-Link Elements",
		"tid_to_link_mapping_negotiation",
		"Diagnostics / Warnings",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("rendered output = %q, missing %q", out, want)
		}
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

func TestRenderWifiMLOHidesPlaceholderConnectionAndCapsNearbyTable(t *testing.T) {
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

	out, err := CommandResult("agent", result, command.Options{WifiRenderMode: command.WifiRenderModeMLO}, pipeline.FormatText)
	if err != nil {
		t.Fatalf("CommandResult() error = %v", err)
	}
	for _, want := range []string{
		"Current AP Relation\n  no active Wi-Fi connection",
		"Connected MLO\n  no active Wi-Fi connection",
		"scan_mlo_metadata_absent 11be_results=2 ap_mld=0 link_id=0",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("rendered output = %q, missing %q", out, want)
		}
	}
	for _, unwanted := range []string{"<unknown ssid>", "802.11ax"} {
		if strings.Contains(out, unwanted) {
			t.Fatalf("rendered output = %q, unexpected %q", out, unwanted)
		}
	}

	lines := strings.Split(out, "\n")
	for i, line := range lines {
		if line != "Nearby MLO APs" {
			continue
		}
		for _, tableLine := range lines[i+1:] {
			if tableLine == "" {
				break
			}
			if wifiMLODisplayWidth(tableLine) > 80 {
				t.Fatalf("table line display width=%d exceeds 80: %q\n%s", wifiMLODisplayWidth(tableLine), tableLine, out)
			}
		}
		break
	}
	if !strings.Contains(out, "...") {
		t.Fatalf("rendered output = %q, missing truncated table cell", out)
	}
}

func ehtMultiLinkTestIE() *controlpb.WifiInformationElement {
	return &controlpb.WifiInformationElement{
		Id:        255,
		IdExt:     107,
		ByteCount: 28,
		BytesHex:  "3000090200000000010207000f72090c0200000000026400010305dd",
	}
}
