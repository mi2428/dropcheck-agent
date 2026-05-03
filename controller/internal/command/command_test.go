package command

import (
	"slices"
	"strings"
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

func TestPathMTUOperationRejectsInvertedBounds(t *testing.T) {
	_, err := PathMTUOperation(PathMTUOptions{
		Host:   "1.1.1.1",
		MinMTU: "1600",
		MaxMTU: "1200",
	})
	if err == nil {
		t.Fatalf("PathMTUOperation() error = nil")
	}
	if !strings.Contains(err.Error(), "max-mtu must be greater than or equal to min-mtu") {
		t.Fatalf("PathMTUOperation() error = %v", err)
	}
}

func TestStandaloneSetEditsBuildConfigEdit(t *testing.T) {
	edits, err := StandaloneSetEdits([]string{"festa", "lab", "wifi-group", "office", "match", "essid", "Lab"})
	if err != nil {
		t.Fatalf("StandaloneSetEdits() error = %v", err)
	}
	op, err := StandaloneEditOperation(edits)
	if err != nil {
		t.Fatalf("StandaloneEditOperation() error = %v", err)
	}
	cmd, _, err := BuildRunCommand(op)
	if err != nil {
		t.Fatalf("BuildRunCommand() error = %v", err)
	}
	edit := cmd.GetEditStandaloneConfig().GetEdits()[0]
	if edit.GetAction() != controlpb.StandaloneEdit_ACTION_SET || strings.Join(edit.GetPath(), "/") != "festa/lab/wifi-group/office/match/essid" || edit.GetValue() != "Lab" {
		t.Fatalf("edit = %#v", edit)
	}

	retention, err := StandaloneSetEdits([]string{"retention", "7d"})
	if err != nil {
		t.Fatalf("retention edit: %v", err)
	}
	if retention[0].Value != "604800000" {
		t.Fatalf("retention = %#v", retention[0])
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
