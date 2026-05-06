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
	if !strings.Contains(out, "channel=40") || !strings.Contains(out, "bandwidth=80MHz") {
		t.Fatalf("rendered output = %q, missing channel or bandwidth", out)
	}
	if strings.Count(out, "Connection: ssid=Lab") != 1 {
		t.Fatalf("rendered output = %q, duplicated wifi connection", out)
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
		"MLO: ap_mld=02:00:00:00:00:01 ap_link_id=2 affiliated=0 associated=1",
		"Associated MLO links:",
		"active",
		"1200",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("rendered output = %q, missing %q", out, want)
		}
	}
}
