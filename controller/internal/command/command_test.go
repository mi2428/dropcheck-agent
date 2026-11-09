package command

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/mi2428/dropcheck-agent/controller/internal/controlpb"
)

func TestSplitArgs(t *testing.T) {
	tests := []struct {
		name string
		line string
		want []string
	}{
		{
			name: "plain words",
			line: "show wifi status",
			want: []string{"show", "wifi", "status"},
		},
		{
			name: "quoted ssid",
			line: `request wifi connect "Office Wi-Fi"`,
			want: []string{"request", "wifi", "connect", "Office Wi-Fi"},
		},
		{
			name: "escaped space",
			line: `show wifi scan ssid Lab\ AP band 5ghz`,
			want: []string{"show", "wifi", "scan", "ssid", "Lab AP", "band", "5ghz"},
		},
		{
			name: "single quotes",
			line: `show wifi scan ssid 'Lab AP'`,
			want: []string{"show", "wifi", "scan", "ssid", "Lab AP"},
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

func TestPathMTUBoundsMatchThePacketActuallySent(t *testing.T) {
	for _, tc := range []struct {
		family, host, minMTU, maxMTU string
		valid                        bool
	}{
		{"ipv4", "192.0.2.1", "1", "1", false},
		{"ipv4", "192.0.2.1", "28", "28", true},
		{"ipv6", "2001:db8::1", "28", "1500", false},
		{"ipv6", "2001:db8::1", "48", "48", true},
		{"", "2001:db8::1", "28", "1500", false},
		{"", "::ffff:192.0.2.1", "28", "28", true},
		{"ipv6", "example.test", "", "1200", false},
		{"ipv4", "example.test", "", "575", false},
		{"ipv6", "example.test", "1200", "1500", true},
		{"", "example.test", "", "", true},
	} {
		op, err := PathMTUOperation(PathMTUOptions{Host: tc.host, Family: tc.family, MinMTU: tc.minMTU, MaxMTU: tc.maxMTU})
		if (err == nil) != tc.valid {
			t.Fatalf("%+v: constructor error=%v", tc, err)
		}
		// Raw operations must enforce the same boundary as frontend constructors.
		minMTU, _ := strconv.ParseUint(tc.minMTU, 10, 32)
		maxMTU, _ := strconv.ParseUint(tc.maxMTU, 10, 32)
		family, _ := parseOptionalProbeFamily(tc.family)
		raw := NewOperation("path-mtu", &controlpb.RunCommand{Command: &controlpb.RunCommand_PathMtu{PathMtu: &controlpb.PathMtu{Host: tc.host, Family: family, MinMtuBytes: uint32(minMTU), MaxMtuBytes: uint32(maxMTU)}}}, Options{})
		if (ValidateOperation(raw) == nil) != tc.valid {
			t.Fatalf("%+v: raw operation validation differs", tc)
		}
		if tc.valid && ValidateOperation(op) != nil {
			t.Fatalf("%+v: valid constructor output rejected", tc)
		}
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
