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
