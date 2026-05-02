package command

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"dropcheck/controller/internal/controlpb"
)

func TestBuildCommandWithOptionsNormalizesPrefixes(t *testing.T) {
	cmd, options, err := BuildCommandWithOptions([]string{"tr", "example.test", "12", "--via", "192.0.2.1"})
	if err != nil {
		t.Fatalf("BuildCommandWithOptions() error = %v", err)
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
