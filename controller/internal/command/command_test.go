package command

import (
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
