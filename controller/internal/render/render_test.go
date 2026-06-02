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
	for line := range strings.SplitSeq(strings.TrimSpace(text), "\n") {
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
	if strings.Index(out, "\n  routes") >= strings.Index(out, "\n  dns") ||
		strings.Index(out, "\n  dns") >= strings.Index(out, "\n  domains") {
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
		"11k,11r,11v",
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

	out, err := CommandResult("agent", result, command.Options{WifiScanBrief: true}, pipeline.FormatText)
	if err != nil {
		t.Fatalf("CommandResult() error = %v", err)
	}
	for _, want := range []string{"Wi-Fi Scan", "SSID", "STANDARD", "SEC_FEATURES", "AP_MLD", "AFFILIATED", "Lab", "802.11be", "gcmp256,sae-gdh,ft-sae-gdh,h2e,ssid-prot,beacon-prot"} {
		if !strings.Contains(out, want) {
			t.Fatalf("rendered output = %q, missing %q", out, want)
		}
	}
	for _, unwanted := range []string{"Scan Affiliated MLO Links", "Scan Wi-Fi Security Details", "ready", "strict"} {
		if strings.Contains(out, unwanted) {
			t.Fatalf("rendered output = %q, unexpected %q", out, unwanted)
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

	out, err := CommandResult("agent", result, command.Options{}, pipeline.FormatText)
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
			out, err := CommandResult("agent", result, tt.options, pipeline.FormatText)
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
	lines := strings.Split(out, "\n")
	header := -1
	for i, line := range lines {
		if strings.Contains(line, "SSID") && strings.Contains(line, "BSSID") {
			header = i
			break
		}
	}
	if header < 0 {
		return nil
	}
	rows := []string{}
	for _, line := range lines[header+1:] {
		if line == "" {
			break
		}
		rows = append(rows, line)
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
						Ssid:            "Lab",
						Bssid:           "aa:bb:cc:dd:ee:ff",
						FrequencyMhz:    5975,
						WifiStandard:    "802.11be",
						ApMldMacAddress: "02:00:00:00:00:01",
						ApMloLinkId:     2,
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

	out, err := CommandResult("agent", result, command.Options{WifiRenderMode: command.WifiRenderModeEHT}, pipeline.FormatText)
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
		"Network MLO",
		"MLO Capability Signals",
		"[*] Lab",
		"type=ap ap_mld=02:00:00:00:00:01 link=2 bssid=aa:bb:cc:dd:ee:ff",
		"band=6ghz ch=5 freq=5975MHz width=80MHz eht_width=320MHz puncture=1,3 rssi=-45dBm",
		"  affiliated_links",
		"    [+] type=aff link=1 ap_mac=aa:bb:cc:dd:ee:01",
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
		"he_preamble_puncturing_rx=preamble_puncturing_rx_80mhz_second_20mhz",
		"he_punctured_sounding=true",
		"eht_disabled_subchannel_bitmap=0x000a punctured=1,3",
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
		"he_operation bss_color=17 disabled=false partial=true",
		"srg_bss_color_bitmap=0x0102030405060708",
		"COLOR",
		"17(part)",
		"Connected HE Details",
		"Scan HE Details",
		"he_mac flags twt_responder,om_control,punctured_sounding",
		"he features twt_responder,punctured_sounding",
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
		"eht features 320mhz_in_6ghz,rx_4096qam_wider_dl_ofdma",
		"max_ampdu=262143",
		"320mhz=true",
		"oper flags disabled_subchannel_bitmap_present,mcs15_disabled",
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
						LinkId:       1,
						State:        "idle",
						Band:         "5ghz",
						Channel:      44,
						RssiDbm:      -55,
						ApMacAddress: "aa:bb:cc:dd:ee:01",
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

	out, err := CommandResult("agent", result, command.Options{WifiScanBrief: true, WifiScanMLO: true}, pipeline.FormatText)
	if err != nil {
		t.Fatalf("CommandResult() error = %v", err)
	}
	for _, want := range []string{"Wi-Fi Scan", "SSID", "BSSID", "SEC_FEATURES", "AP_MLD", "Lab", "gcmp256,sae-gdh,ft-sae-gdh,h2e,ssid-prot,beacon-prot", "aa:bb:cc:dd:ee:ff", "aa:bb:cc:dd:ee:01", "affiliated_link,idle"} {
		if !strings.Contains(out, want) {
			t.Fatalf("rendered output = %q, missing %q", out, want)
		}
	}
	for _, unwanted := range []string{"GhostBE", "LegacyMLO", "Scan Affiliated MLO Links", "Scan Wi-Fi Security Details", "ready", "strict", "W7SEC", "AP_LINK", "AFFILIATED", "[A]", "[L]"} {
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
	bssidIndex := strings.Index(affiliatedLine, "aa:bb:cc:dd:ee:01")
	bandIndex := strings.Index(affiliatedLine, "5ghz")
	if bssidIndex < 0 || bandIndex < 0 || bandIndex <= bssidIndex {
		t.Fatalf("affiliated line columns = %q\n%s", affiliatedLine, out)
	}
	rssiCell := affiliatedLine[bssidIndex+len("aa:bb:cc:dd:ee:01") : bandIndex]
	if strings.TrimSpace(rssiCell) != "" {
		t.Fatalf("affiliated line RSSI = %q, want blank\n%s", affiliatedLine, out)
	}
	if strings.Contains(affiliatedLine, " - ") {
		t.Fatalf("affiliated line contains placeholder hyphen = %q\n%s", affiliatedLine, out)
	}
	parentLine := renderedLineContaining(out, "aa:bb:cc:dd:ee:ff")
	if parentLine == "" {
		t.Fatalf("rendered output missing parent line:\n%s", out)
	}
	for _, tt := range []struct {
		key  string
		want string
	}{
		{key: "mlo_results", want: "1"},
		{key: "affiliated_rows", want: "1"},
		{key: "display_rows", want: "2"},
		{key: "scan_results", want: "3"},
		{key: "scan_total", want: "3"},
	} {
		line := renderedLineContaining(out, tt.key)
		if line == "" {
			t.Fatalf("rendered output missing summary line %q:\n%s", tt.key, out)
		}
		summaryFields := strings.Fields(line)
		if len(summaryFields) == 0 || summaryFields[len(summaryFields)-1] != tt.want {
			t.Fatalf("summary line %q = %q, want value %q\n%s", tt.key, line, tt.want, out)
		}
	}
}

func TestRenderWifiScanBriefMLOGroupsSSIDByBandAndListsAPBeforeLink(t *testing.T) {
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
						LinkId:       0,
						State:        "unassociated",
						Band:         "6ghz",
						ApMacAddress: "98:8f:00:f0:75:10",
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
						LinkId:       1,
						State:        "unassociated",
						Band:         "5ghz",
						ApMacAddress: "98:8f:00:f0:74:f0",
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
						LinkId:       2,
						State:        "unassociated",
						Band:         "6ghz",
						ApMacAddress: "f0:d8:05:77:3b:0f",
					}},
				}},
			},
		},
	}

	out, err := CommandResult("agent", result, command.Options{WifiScanBrief: true, WifiScanMLO: true}, pipeline.FormatText)
	if err != nil {
		t.Fatalf("CommandResult() error = %v", err)
	}
	rows := scanTableRows(out)
	if len(rows) < 6 {
		t.Fatalf("scan rows = %#v, want at least 6 rows\n%s", rows, out)
	}
	for i, want := range []string{"98:8f:00:f0:75:10", "98:8f:00:f0:74:f0", "98:8f:00:f0:75:10", "98:8f:00:f0:74:f0", "f0:d8:05:77:3b:07", "f0:d8:05:77:3b:0f"} {
		if !strings.Contains(rows[i], want) {
			t.Fatalf("scan row %d = %q, want entry %q\n%s", i, rows[i], want, out)
		}
	}
	if strings.Count(strings.Join(rows[:4], "\n"), "hp2") != 1 {
		t.Fatalf("hp2 label should appear once per group rows=%#v\n%s", rows[:4], out)
	}
	if strings.Count(strings.Join(rows[4:6], "\n"), "cs5") != 1 {
		t.Fatalf("cs5 label should appear once per group rows=%#v\n%s", rows[4:6], out)
	}
	if !strings.Contains(rows[0], "-65") || strings.Contains(rows[2], "-65") {
		t.Fatalf("6GHz AP row should precede 6GHz link rows=%#v\n%s", rows[:4], out)
	}
	if !strings.Contains(rows[1], "-62") || strings.Contains(rows[3], "-62") {
		t.Fatalf("5GHz AP row should precede 5GHz link rows=%#v\n%s", rows[:4], out)
	}
	if !strings.Contains(rows[0], "6ghz") || !strings.Contains(rows[1], "5ghz") || !strings.Contains(rows[2], "6ghz") || !strings.Contains(rows[3], "5ghz") {
		t.Fatalf("rows not ordered by 6GHz AP, 5GHz AP, 6GHz link, 5GHz link rows=%#v\n%s", rows[:4], out)
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
						LinkId:       1,
						State:        "idle",
						Band:         "5ghz",
						Channel:      149,
						RssiDbm:      -70,
						ApMacAddress: "6e:ef:9d:c4:8e:60",
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

	out, err := CommandResult("agent", result, command.Options{WifiScanBrief: true, WifiScanMLO: true}, pipeline.FormatText)
	if err != nil {
		t.Fatalf("CommandResult() error = %v", err)
	}
	for _, want := range []string{"Meraki", "SEC_FEATURES", "6e:ef:9d:c4:8e:70", "6e:ef:9d:c4:8e:60", "mlo_results      1", "display_rows     2"} {
		if !strings.Contains(out, want) {
			t.Fatalf("rendered output = %q, missing %q", out, want)
		}
	}
	if strings.Contains(out, "LegacyMLO") {
		t.Fatalf("rendered output = %q, unexpected LegacyMLO", out)
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
	}, pipeline.FormatText)
	if err != nil {
		t.Fatalf("CommandResult(ssid) error = %v", err)
	}
	for _, want := range []string{
		"filter            ssid=Lab",
		"filtered_results  1",
		"Network MLO",
		"Lab",
		"aa:bb:cc:dd:ee:ff",
		"no active Wi-Fi connection",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("rendered output = %q, missing %q", out, want)
		}
	}
	if strings.Contains(out, "22:22:22:22:22:22") {
		t.Fatalf("rendered output = %q, unexpected filtered BSSID", out)
	}

	out, err = CommandResult("agent", result, command.Options{
		WifiRenderMode: command.WifiRenderModeEHT,
		WifiEHTBSSID:   "AA:BB:CC:DD:EE:01",
	}, pipeline.FormatText)
	if err != nil {
		t.Fatalf("CommandResult(bssid) error = %v", err)
	}
	for _, want := range []string{
		"filter            bssid=AA:BB:CC:DD:EE:01",
		"filtered_results  1",
		"aa:bb:cc:dd:ee:ff",
		"  affiliated_links",
		"    [-] type=aff link=2 ap_mac=aa:bb:cc:dd:ee:01",
	} {
		if !strings.Contains(out, want) {
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

	out, err := CommandResult("agent", result, command.Options{WifiRenderMode: command.WifiRenderModeEHT}, pipeline.FormatText)
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

	out, err := CommandResult("agent", result, command.Options{WifiRenderMode: command.WifiRenderModeEHT}, pipeline.FormatText)
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
		"Diagnostics / Warnings\n  none",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("rendered output = %q, missing %q", out, want)
		}
	}
	for _, unwanted := range []string{
		"no EHT-capable scan results",
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

	out, err := CommandResult("agent", result, command.Options{WifiRenderMode: command.WifiRenderModeEHT}, pipeline.FormatText)
	if err != nil {
		t.Fatalf("CommandResult() error = %v", err)
	}
	for _, want := range []string{
		"Current AP Relation\n  no active Wi-Fi connection",
		"Connected MLO\n  no active Wi-Fi connection",
		"scan_mlo_metadata_absent 11be_results=2 ap_mld=0 link_id=0",
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

	lines := strings.Split(out, "\n")
	for i, line := range lines {
		if line != "Nearby EHT APs" {
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
