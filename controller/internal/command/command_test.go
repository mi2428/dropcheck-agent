package command

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"dropcheck/controller/internal/controlpb"
)

func TestSplitArgs(t *testing.T) {
	tests := []struct {
		name string
		line string
		want []string
	}{
		{
			name: "plain words",
			line: "wifi status",
			want: []string{"wifi", "status"},
		},
		{
			name: "quoted ssid",
			line: `wifi connect "Office Wi-Fi" "secret pass" wpa3`,
			want: []string{"wifi", "connect", "Office Wi-Fi", "secret pass", "wpa3"},
		},
		{
			name: "escaped space",
			line: `wifi scan detail Lab\ AP 5ghz`,
			want: []string{"wifi", "scan", "detail", "Lab AP", "5ghz"},
		},
		{
			name: "single quotes",
			line: `http 'https://example.test/a b' 204`,
			want: []string{"http", "https://example.test/a b", "204"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SplitArgs(tt.line)
			if err != nil {
				t.Fatalf("SplitArgs() error = %v", err)
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("SplitArgs() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestSplitArgsErrors(t *testing.T) {
	for _, line := range []string{`wifi\`, `wifi connect "ssid`, `wifi connect 'ssid`} {
		t.Run(line, func(t *testing.T) {
			if _, err := SplitArgs(line); err == nil {
				t.Fatalf("SplitArgs() error = nil")
			}
		})
	}
}

func TestTracerouteOperationStoresRequiredHops(t *testing.T) {
	op, err := TracerouteOperation(TracerouteOptions{
		Host:    "example.test",
		MaxHops: "12",
		Via:     []string{"192.0.2.1"},
	})
	if err != nil {
		t.Fatalf("TracerouteOperation() error = %v", err)
	}
	cmd, options, err := BuildRunCommand(op)
	if err != nil {
		t.Fatalf("BuildRunCommand() error = %v", err)
	}
	trace := cmd.GetTraceroute()
	if trace == nil {
		t.Fatalf("Traceroute = nil")
	}
	if trace.GetHost() != "example.test" || trace.GetMaxHops() != 12 {
		t.Fatalf("Traceroute = %#v", trace)
	}
	if !slices.Equal(options.TracerouteRequiredHops, []string{"192.0.2.1"}) {
		t.Fatalf("TracerouteRequiredHops = %#v", options.TracerouteRequiredHops)
	}
}

func TestFestivalSetConfigOperationLoadsPlan(t *testing.T) {
	planPath := filepath.Join(t.TempDir(), "plan.json")
	if err := os.WriteFile(planPath, []byte(`{
  "name": "lab",
  "networks": [{
    "name": "lab-wifi",
    "connect": {"ssid": "Lab", "passphrase": "secret", "security": "SECURITY_WPA3_SAE"},
    "checks": [{"name": "ping", "command": {"ping": {"host": "8.8.8.8", "count": 1}}}]
  }]
}`), 0o644); err != nil {
		t.Fatal(err)
	}
	op, err := FestivalSetConfigOperation(FestivalConfigOptions{
		Enabled:   true,
		PlanPath:  planPath,
		Interval:  "30s",
		Retention: "7d",
		MaxSize:   "512m",
	})
	if err != nil {
		t.Fatalf("FestivalSetConfigOperation() error = %v", err)
	}
	cmd, _, err := BuildRunCommand(op)
	if err != nil {
		t.Fatalf("BuildRunCommand() error = %v", err)
	}
	config := cmd.GetSetFestivalConfig().GetConfig()
	if !config.GetEnabled() || config.GetIntervalMs() != 30000 || config.GetRetentionMs() != 604800000 || config.GetMaxBytes() != 512*1024*1024 {
		t.Fatalf("config = %#v", config)
	}
	if config.GetPlan().GetNetworks()[0].GetConnect().GetSsid() != "Lab" {
		t.Fatalf("plan = %#v", config.GetPlan())
	}
}

func TestWifiFreshScanOperationBuildsCommand(t *testing.T) {
	op, err := WifiFreshScanOperation("6ghz", "9000")
	if err != nil {
		t.Fatalf("WifiFreshScanOperation() error = %v", err)
	}
	if op.Name != "wifi.scan.fresh" {
		t.Fatalf("operation name = %q", op.Name)
	}
	cmd, _, err := BuildRunCommand(op)
	if err != nil {
		t.Fatalf("BuildRunCommand() error = %v", err)
	}
	scan := cmd.GetGetFreshWifiScan()
	if scan == nil || scan.GetBand() != controlpb.WifiBand_WIFI_BAND_6_GHZ || scan.GetTimeoutMs() != 9000 {
		t.Fatalf("fresh scan = %#v", scan)
	}
}

func TestControllerLinkSetConfigOperationBuildsCommand(t *testing.T) {
	op, err := ControllerLinkSetConfigOperation(ControllerLinkConfigOptions{
		Enabled:    true,
		Endpoint:   "192.168.7.1:37588",
		MinBackoff: "1s",
		MaxBackoff: "30s",
	})
	if err != nil {
		t.Fatalf("ControllerLinkSetConfigOperation() error = %v", err)
	}
	cmd, _, err := BuildRunCommand(op)
	if err != nil {
		t.Fatalf("BuildRunCommand() error = %v", err)
	}
	config := cmd.GetSetControllerLinkConfig().GetConfig()
	if !config.GetEnabled() || config.GetHost() != "192.168.7.1" || config.GetPort() != 37588 {
		t.Fatalf("config endpoint = %#v", config)
	}
	if config.GetMinBackoffMs() != 1000 || config.GetMaxBackoffMs() != 30000 {
		t.Fatalf("config backoff = %#v", config)
	}
}
