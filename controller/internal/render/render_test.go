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

func TestRenderWifiRequestResultsDoNotAutoShowState(t *testing.T) {
	status := &controlpb.WifiStatus{
		Enabled: true,
		State:   "enabled",
		Connection: &controlpb.WifiConnection{
			Ssid:        "AutoShowSSID",
			Bssid:       "aa:bb:cc:dd:ee:ff",
			Ipv4Address: "192.0.2.10",
		},
		IpStatus: &controlpb.IpStatus{
			InterfaceName: "wlan0",
			Addresses:     []string{"192.0.2.10/24"},
		},
	}
	tests := []struct {
		name string
		want string
		res  *controlpb.CommandResult
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
			name: "operation",
			want: "Wi-Fi Operation",
			res: &controlpb.CommandResult{Payload: &controlpb.CommandResult_WifiOperation{WifiOperation: &controlpb.WifiOperationResult{
				Operation: "reconnect",
				Ok:        true,
				Message:   "reconnected",
				Status:    status,
			}}},
		},
		{
			name: "assert",
			want: "Wi-Fi Assert",
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
			out, err := CommandResult("agent", tt.res, command.Options{}, pipeline.FormatText)
			if err != nil {
				t.Fatalf("CommandResult() error = %v", err)
			}
			if !strings.Contains(out, tt.want) {
				t.Fatalf("rendered output = %q, missing %q", out, tt.want)
			}
			for _, unwanted := range []string{"Wi-Fi\n", "Network\n", "AutoShowSSID", "192.0.2.10"} {
				if strings.Contains(out, unwanted) {
					t.Fatalf("rendered output = %q, unexpectedly included %q", out, unwanted)
				}
			}
		})
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

func TestRenderConfigDisplaySet(t *testing.T) {
	view := ConfigView{
		Standalone: &controlpb.StandaloneConfig{
			Enabled:     true,
			RetentionMs: 7 * 24 * 60 * 60 * 1000,
			MaxBytes:    512 * 1024 * 1024,
			Upload: &controlpb.StandaloneUploadConfig{
				Url: "http://192.168.50.10:8080/dropcheck/incoming",
				Wifi: &controlpb.ConnectWifi{
					Ssid:             "NOC",
					Passphrase:       "upload-secret",
					Security:         controlpb.ConnectWifi_SECURITY_WPA3_SAE,
					Bssid:            "aa:bb:cc:dd:ee:ff",
					Band:             controlpb.WifiBand_WIFI_BAND_6_GHZ,
					MacRandomization: controlpb.ConnectWifi_MAC_RANDOMIZATION_NON_PERSISTENT,
					TimeoutMs:        5000,
				},
			},
			Festas: []*controlpb.StandaloneFesta{{
				Name:       "smoke",
				Enabled:    true,
				IntervalMs: 30000,
				WifiGroups: []*controlpb.StandaloneWifiGroup{{
					Name:             "mgmt",
					Essid:            "Lab SSID",
					Passphrase:       "wifi secret",
					Security:         controlpb.ConnectWifi_SECURITY_WPA2_WPA3_TRANSITION,
					Band:             controlpb.WifiBand_WIFI_BAND_5_GHZ,
					RequireIp:        true,
					RequireValidated: true,
					TimeoutMs:        35000,
					MacRandomization: controlpb.ConnectWifi_MAC_RANDOMIZATION_PERSISTENT,
				}},
				Checks: []*controlpb.StandaloneCheck{
					{
						Name: "cloudflare",
						Test: &controlpb.StandaloneCheck_Ping{Ping: &controlpb.StandalonePingCheck{
							Host:      "1.1.1.1",
							Count:     1,
							SizeBytes: 64,
							TimeoutMs: 8000,
						}},
					},
					{
						Name: "dns-main",
						Test: &controlpb.StandaloneCheck_Dns{Dns: &controlpb.StandaloneDnsCheck{
							Name:      "example.test",
							Qtypes:    []controlpb.DnsRecordType{controlpb.DnsRecordType_DNS_RECORD_TYPE_AAAA},
							TimeoutMs: 3000,
						}},
					},
					{
						Name: "health",
						Test: &controlpb.StandaloneCheck_Http{Http: &controlpb.StandaloneHttpCheck{
							Url:            "https://example.test/health",
							ExpectedStatus: 204,
							TimeoutMs:      4000,
						}},
					},
				},
			}},
		},
	}
	text, err := Config(view, pipeline.FormatSet)
	if err != nil {
		t.Fatalf("Config(display set) error = %v", err)
	}
	for _, want := range []string{
		"set standalone enabled\n",
		"set standalone retention 7d\n",
		"set standalone max-size 536870912\n",
		"set standalone upload to http://192.168.50.10:8080/dropcheck/incoming\n",
		"set standalone upload via wifi essid NOC passphrase upload-secret security wpa3 bssid aa:bb:cc:dd:ee:ff band 6ghz mac-randomization non-persistent timeout 5s\n",
		"set standalone festa smoke enabled\n",
		"set standalone festa smoke interval 30s\n",
		"set standalone festa smoke wifi mgmt match essid \"Lab SSID\" mac-randomization persistent\n",
		"set standalone festa smoke wifi mgmt passphrase \"wifi secret\" security transition\n",
		"set standalone festa smoke wifi mgmt band 5ghz\n",
		"set standalone festa smoke wifi mgmt wait ip\n",
		"set standalone festa smoke wifi mgmt wait validated\n",
		"set standalone festa smoke wifi mgmt timeout 35s\n",
		"set standalone festa smoke check cloudflare test ping host 1.1.1.1 count 1 size 64 timeout 8s\n",
		"set standalone festa smoke check dns-main test dns name example.test type AAAA timeout 3s\n",
		"set standalone festa smoke check health test http url https://example.test/health expected-status 204 timeout 4s\n",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("Config(display set) = %q, missing %q", text, want)
		}
	}
	if strings.Contains(text, "<redacted>") {
		t.Fatalf("Config(display set) should emit copyable commands, got %q", text)
	}
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		args, err := command.SplitArgs(line)
		if err != nil {
			t.Fatalf("SplitArgs(%q) error = %v", line, err)
		}
		if len(args) < 3 || args[0] != "set" || args[1] != "standalone" {
			t.Fatalf("display set line is not a standalone set command: %q", line)
		}
		if _, err := command.StandaloneSetEdits(args[2:]); err != nil {
			t.Fatalf("display set line does not parse: %q: %v", line, err)
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
					Domains:          "local",
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
		"domains",
		"local",
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
	if !(strings.Index(out, "\n  routes") < strings.Index(out, "\n  dns") &&
		strings.Index(out, "\n  dns") < strings.Index(out, "\n  domains")) {
		t.Fatalf("network rows are not ordered routes -> dns -> domains:\n%s", out)
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

	out, err := CommandResult("agent", result, command.Options{}, pipeline.FormatText)
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

func TestRenderWifiStatusOmitsMLOFields(t *testing.T) {
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
	for _, unwanted := range []string{
		"\nMLO\n",
		"\nMLO Links\n",
		"ap_mld",
		"ap_link_id",
		"02:00:00:00:00:01",
	} {
		if strings.Contains(out, unwanted) {
			t.Fatalf("rendered output = %q, included %q", out, unwanted)
		}
	}
}

func TestScanMLOLinkIDTreatsZeroAsPresentFor11be(t *testing.T) {
	got := scanMLOLinkID(&controlpb.WifiScanResult{
		WifiStandard: "802.11be",
		ApMloLinkId:  0,
	})
	if got != "0" {
		t.Fatalf("scanMLOLinkID() = %q, want 0", got)
	}
	got = scanMLOLinkID(&controlpb.WifiScanResult{
		ApMloLinkId: 0,
	})
	if got != "<none>" {
		t.Fatalf("scanMLOLinkID() without 11be = %q, want <none>", got)
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
						He_6GhzCapabilities: he6GhzCapabilitiesTestValue(),
						EhtCapabilities:     ehtCapabilitiesTestValue(),
						EhtOperation:        ehtOperationTestValue(),
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
						He_6GhzCapabilities: he6GhzCapabilitiesTestValue(),
						EhtCapabilities:     ehtCapabilitiesTestValue(),
						EhtOperation:        ehtOperationTestValue(),
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
		"EHT_W",
		"PUNCT",
		"MLO Scan Links",
		"Network MLO",
		"MLO Capability Signals",
		"[*] Lab",
		"ap_mld=02:00:00:00:00:01 link=2 bssid=aa:bb:cc:dd:ee:ff",
		"band=6ghz ch=5 freq=5975MHz width=80MHz eht_width=320MHz puncture=1,3 rssi=-45dBm",
		"[+] affiliated Lab",
		"Connected EHT Multi-Link Elements",
		"ml_control raw=0x0030 type=basic(0) presence=link_id_info,bss_parameters_change_count bytes=28",
		"common_info len=9 mld_mac=02:00:00:00:00:01 link_id=2 bss_param_change_count=7",
		"per_link link_id=2 control=0x0972 complete=true",
		"sta_info_len=12 sta_mac=02:00:00:00:00:02 beacon_interval_tu=100",
		"dtim=1/3",
		"Scan EHT Multi-Link Elements",
		"Connected HE 6GHz Details",
		"Scan HE 6GHz Details",
		"Connected EHT Details",
		"max_mpdu=7991",
		"max_ampdu=262143",
		"320mhz=true",
		"width_mhz=320",
		"disabled=0x000a",
		"punctured=1,3",
		"Scan EHT Details",
		"tid_to_link_mapping_negotiation",
		"Diagnostics / Warnings",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("rendered output = %q, missing %q", out, want)
		}
	}
	for _, line := range scanEHTDetailLines(out) {
		if len(line) > 92 {
			t.Fatalf("Scan EHT detail line too long (%d): %s\n%s", len(line), line, out)
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

	out, err := CommandResult("agent", result, command.Options{WifiRenderMode: command.WifiRenderModeMLO}, pipeline.FormatText)
	if err != nil {
		t.Fatalf("CommandResult() error = %v", err)
	}
	for _, want := range []string{
		"subelement id=0 name=per_sta_profile len=22 actual=22 fragments=1 reassembled=30",
		"fragment target_id=0 target=per_sta_profile bytes=8 payload=0x0102ff046c010203",
		"profile_ie link_id=2 id=0 name=ssid len=3 actual=3 body=0x4c6162",
		"profile_ie link_id=2 id=255 ext=106 name=eht_operation len=3 actual=3 body=0x6a0102",
		"profile_ie link_id=2 id=255 ext=108 name=eht_capabilities len=4 actual=4 body=0x6c010203",
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
						Ssid:         "Lab",
						Bssid:        "aa:bb:cc:dd:ee:ff",
						WifiStandard: "802.11ax",
						ApMloLinkId:  -1,
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

	out, err := CommandResult("agent", result, command.Options{WifiRenderMode: command.WifiRenderModeMLO}, pipeline.FormatText)
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
		"mlo_candidates",
		"[*] Lab",
		"ap_mld=02:00:00:00:00:01 link=2 bssid=aa:bb:cc:dd:ee:ff",
		"Diagnostics / Warnings\n  none",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("rendered output = %q, missing %q", out, want)
		}
	}
	for _, unwanted := range []string{
		"no MLO-capable scan results",
		"scan_mlo_metadata_absent",
	} {
		if strings.Contains(out, unwanted) {
			t.Fatalf("rendered output = %q, unexpected %q", out, unwanted)
		}
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

func detailedEHTMultiLinkTestIE() *controlpb.WifiInformationElement {
	return &controlpb.WifiInformationElement{
		Id:        255,
		IdExt:     107,
		ByteCount: 53,
		BytesHex:  "3000090200000000010207001672090c020000000002640001030500034c6162ff036afe080102ff046c010203dd060011220799aa",
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

func ehtCapabilitiesTestValue() *controlpb.WifiEhtCapabilities {
	return &controlpb.WifiEhtCapabilities{
		MacCapabilitiesHex: "774e",
		PhyCapabilitiesHex: "f61fffffff7ff8ff03",
		Mac: &controlpb.WifiEhtMacCapabilities{
			EpcsPriorityAccess:            true,
			OmControl:                     true,
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

func scanEHTDetailLines(rendered string) []string {
	lines := strings.Split(rendered, "\n")
	out := []string{}
	inSection := false
	for _, line := range lines {
		switch line {
		case "Scan EHT Details":
			inSection = true
			continue
		case "Diagnostics / Warnings", "MLO Capability Signals":
			inSection = false
		}
		if inSection && strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	return out
}
