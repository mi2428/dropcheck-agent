package main

import (
	"slices"
	"strings"
	"testing"
	"time"

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
			got, err := splitArgs(tt.line)
			if err != nil {
				t.Fatalf("splitArgs() error = %v", err)
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("splitArgs() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestSplitArgsErrors(t *testing.T) {
	tests := []struct {
		name string
		line string
	}{
		{name: "trailing escape", line: `wifi\`},
		{name: "unterminated double quote", line: `wifi connect "ssid`},
		{name: "unterminated single quote", line: `wifi connect 'ssid`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := splitArgs(tt.line); err == nil {
				t.Fatalf("splitArgs() error = nil")
			}
		})
	}
}

func TestNormalizeShellCommandPrefixes(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "help prefix",
			args: []string{"he"},
			want: []string{"help"},
		},
		{
			name: "wifi capabilities prefix",
			args: []string{"wi", "cap"},
			want: []string{"wifi", "capabilities"},
		},
		{
			name: "http prefix disambiguates help",
			args: []string{"ht", "https://example.test"},
			want: []string{"http", "https://example.test"},
		},
		{
			name: "at target leaves command for agent dispatch",
			args: []string{"@1", "wi", "cap"},
			want: []string{"@1", "wi", "cap"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeShellCommandArgs(tt.args)
			if err != nil {
				t.Fatalf("normalizeShellCommandArgs(%v) error = %v", tt.args, err)
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("normalizeShellCommandArgs(%v) = %#v, want %#v", tt.args, got, tt.want)
			}
		})
	}

	for _, args := range [][]string{
		{"h"},
		{"d"},
		{"wifi", "s"},
	} {
		t.Run(stringsJoin(args), func(t *testing.T) {
			if _, err := normalizeShellCommandArgs(args); err == nil {
				t.Fatalf("normalizeShellCommandArgs(%v) error = nil", args)
			}
		})
	}
}

func TestEnumParsers(t *testing.T) {
	securityTests := map[string]controlpb.ConnectWifi_Security{
		"":           controlpb.ConnectWifi_SECURITY_WPA2_PSK,
		"wpa2":       controlpb.ConnectWifi_SECURITY_WPA2_PSK,
		"wpa3":       controlpb.ConnectWifi_SECURITY_WPA3_SAE,
		"transition": controlpb.ConnectWifi_SECURITY_WPA2_WPA3_TRANSITION,
	}
	for input, want := range securityTests {
		got, err := parseSecurity(input)
		if err != nil {
			t.Fatalf("parseSecurity(%q) error = %v", input, err)
		}
		if got != want {
			t.Fatalf("parseSecurity(%q) = %v, want %v", input, got, want)
		}
	}
	for _, input := range []string{"psk", "sae", "wep"} {
		if _, err := parseSecurity(input); err == nil {
			t.Fatalf("parseSecurity(%s) error = nil", input)
		}
	}

	bandTests := map[string]controlpb.WifiBand{
		"":       controlpb.WifiBand_WIFI_BAND_ALL,
		"all":    controlpb.WifiBand_WIFI_BAND_ALL,
		"2.4ghz": controlpb.WifiBand_WIFI_BAND_2_4_GHZ,
		"5ghz":   controlpb.WifiBand_WIFI_BAND_5_GHZ,
		"6ghz":   controlpb.WifiBand_WIFI_BAND_6_GHZ,
		"60ghz":  controlpb.WifiBand_WIFI_BAND_60_GHZ,
	}
	for input, want := range bandTests {
		got, err := parseWifiBand(input)
		if err != nil {
			t.Fatalf("parseWifiBand(%q) error = %v", input, err)
		}
		if got != want {
			t.Fatalf("parseWifiBand(%q) = %v, want %v", input, got, want)
		}
	}
	for _, input := range []string{"5g", "24g", "any", "7ghz"} {
		if _, err := parseWifiBand(input); err == nil {
			t.Fatalf("parseWifiBand(%s) error = nil", input)
		}
	}

	macRandomizationTests := map[string]controlpb.ConnectWifi_MacRandomization{
		"":               controlpb.ConnectWifi_MAC_RANDOMIZATION_UNSPECIFIED,
		"auto":           controlpb.ConnectWifi_MAC_RANDOMIZATION_AUTO,
		"none":           controlpb.ConnectWifi_MAC_RANDOMIZATION_NONE,
		"persistent":     controlpb.ConnectWifi_MAC_RANDOMIZATION_PERSISTENT,
		"non-persistent": controlpb.ConnectWifi_MAC_RANDOMIZATION_NON_PERSISTENT,
	}
	for input, want := range macRandomizationTests {
		got, err := parseMacRandomization(input)
		if err != nil {
			t.Fatalf("parseMacRandomization(%q) error = %v", input, err)
		}
		if got != want {
			t.Fatalf("parseMacRandomization(%q) = %v, want %v", input, got, want)
		}
	}
	for _, input := range []string{"per-test", "random", "spoofed"} {
		if _, err := parseMacRandomization(input); err == nil {
			t.Fatalf("parseMacRandomization(%s) error = nil", input)
		}
	}

	qtypes, err := parseQTypes("ALL")
	if err != nil {
		t.Fatalf("parseQTypes(ALL) error = %v", err)
	}
	wantQTypes := []controlpb.DnsRecordType{
		controlpb.DnsRecordType_DNS_RECORD_TYPE_A,
		controlpb.DnsRecordType_DNS_RECORD_TYPE_AAAA,
	}
	if !slices.Equal(qtypes, wantQTypes) {
		t.Fatalf("parseQTypes(ALL) = %v, want %v", qtypes, wantQTypes)
	}
	if _, err := parseQTypes("TXT"); err == nil {
		t.Fatalf("parseQTypes(TXT) error = nil")
	}
	if _, err := parseQTypes("A+AAAA"); err == nil {
		t.Fatalf("parseQTypes(A+AAAA) error = nil")
	}
}

func TestBuildCommandWifiConnect(t *testing.T) {
	cmd := mustBuild(t, "wifi", "connect", "Lab", "pass", "wpa3", "--bssid", "aa:bb:cc:dd:ee:ff", "--band", "6ghz", "--mac-randomization", "non-persistent", "--timeout", "12345")
	connect := cmd.GetConnectWifi()
	if connect == nil {
		t.Fatalf("GetConnectWifi() = nil")
	}
	if connect.GetSsid() != "Lab" || connect.GetPassphrase() != "pass" {
		t.Fatalf("connect credentials = %q/%q", connect.GetSsid(), connect.GetPassphrase())
	}
	if connect.GetSecurity() != controlpb.ConnectWifi_SECURITY_WPA3_SAE {
		t.Fatalf("security = %v", connect.GetSecurity())
	}
	if connect.GetBssid() != "aa:bb:cc:dd:ee:ff" {
		t.Fatalf("bssid = %q", connect.GetBssid())
	}
	if connect.GetBand() != controlpb.WifiBand_WIFI_BAND_6_GHZ {
		t.Fatalf("band = %v", connect.GetBand())
	}
	if connect.GetMacRandomization() != controlpb.ConnectWifi_MAC_RANDOMIZATION_NON_PERSISTENT {
		t.Fatalf("mac randomization = %v", connect.GetMacRandomization())
	}
	if connect.GetTimeoutMs() != 12345 {
		t.Fatalf("timeout = %d", connect.GetTimeoutMs())
	}
}

func TestBuildCommandWifiSubcommands(t *testing.T) {
	tests := []struct {
		name   string
		args   []string
		assert func(*testing.T, *controlpb.RunCommand)
	}{
		{
			name: "fresh scan",
			args: []string{"wifi", "scan", "fresh", "5ghz", "--timeout", "9000"},
			assert: func(t *testing.T, cmd *controlpb.RunCommand) {
				scan := cmd.GetGetFreshWifiScan()
				if scan == nil || scan.GetBand() != controlpb.WifiBand_WIFI_BAND_5_GHZ || scan.GetTimeoutMs() != 9000 {
					t.Fatalf("fresh scan = %#v", scan)
				}
			},
		},
		{
			name: "scan detail",
			args: []string{"wifi", "scan", "detail", "aa:bb:cc:dd:ee:ff", "2.4ghz"},
			assert: func(t *testing.T, cmd *controlpb.RunCommand) {
				detail := cmd.GetGetWifiScanDetail()
				if detail == nil || detail.GetTarget() != "aa:bb:cc:dd:ee:ff" || detail.GetBand() != controlpb.WifiBand_WIFI_BAND_2_4_GHZ {
					t.Fatalf("scan detail = %#v", detail)
				}
			},
		},
		{
			name: "wait connected",
			args: []string{"wifi", "wait", "connected", "Lab", "--security", "transition", "--band", "5ghz", "--ip", "--validated", "--timeout", "3000"},
			assert: func(t *testing.T, cmd *controlpb.RunCommand) {
				wait := cmd.GetWaitWifiConnected()
				if wait == nil || wait.GetSsid() != "Lab" || !wait.GetRequireIp() || !wait.GetRequireValidated() {
					t.Fatalf("wait = %#v", wait)
				}
				if wait.GetSecurity() != controlpb.ConnectWifi_SECURITY_WPA2_WPA3_TRANSITION || wait.GetBand() != controlpb.WifiBand_WIFI_BAND_5_GHZ {
					t.Fatalf("wait security/band = %v/%v", wait.GetSecurity(), wait.GetBand())
				}
			},
		},
		{
			name: "cycle",
			args: []string{"wifi", "cycle", "Lab", "pass", "--count", "2", "--mac-randomization", "non-persistent", "--ping", "1.1.1.1", "--http", "https://example.test", "--forget", "--pause", "250"},
			assert: func(t *testing.T, cmd *controlpb.RunCommand) {
				cycle := cmd.GetCycleWifi()
				if cycle == nil || cycle.GetCount() != 2 || cycle.GetPingHost() != "1.1.1.1" || cycle.GetHttpUrl() != "https://example.test" || !cycle.GetForgetAfterEach() {
					t.Fatalf("cycle = %#v", cycle)
				}
				if cycle.GetConnect().GetSsid() != "Lab" || cycle.GetPauseMs() != 250 {
					t.Fatalf("cycle connect/pause = %#v/%d", cycle.GetConnect(), cycle.GetPauseMs())
				}
				if cycle.GetConnect().GetMacRandomization() != controlpb.ConnectWifi_MAC_RANDOMIZATION_NON_PERSISTENT {
					t.Fatalf("cycle mac randomization = %v", cycle.GetConnect().GetMacRandomization())
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := mustBuild(t, tt.args...)
			tt.assert(t, cmd)
		})
	}
}

func TestBuildCommandUniquePrefixes(t *testing.T) {
	tests := []struct {
		name   string
		args   []string
		assert func(*testing.T, *controlpb.RunCommand)
	}{
		{
			name: "wifi status",
			args: []string{"wi", "stat"},
			assert: func(t *testing.T, cmd *controlpb.RunCommand) {
				if cmd.GetGetWifiStatus() == nil || cmd.GetLabel() != "wifi status" {
					t.Fatalf("status command = %#v label=%q", cmd.GetGetWifiStatus(), cmd.GetLabel())
				}
			},
		},
		{
			name: "wifi capabilities",
			args: []string{"wi", "cap"},
			assert: func(t *testing.T, cmd *controlpb.RunCommand) {
				if cmd.GetGetWifiCapabilities() == nil || cmd.GetLabel() != "wifi capabilities" {
					t.Fatalf("capabilities command = %#v label=%q", cmd.GetGetWifiCapabilities(), cmd.GetLabel())
				}
			},
		},
		{
			name: "wifi scan fresh",
			args: []string{"wi", "sc", "fr", "5ghz", "--timeout", "9000"},
			assert: func(t *testing.T, cmd *controlpb.RunCommand) {
				scan := cmd.GetGetFreshWifiScan()
				if scan == nil || scan.GetBand() != controlpb.WifiBand_WIFI_BAND_5_GHZ || scan.GetTimeoutMs() != 9000 {
					t.Fatalf("fresh scan = %#v", scan)
				}
			},
		},
		{
			name: "wifi wait connected",
			args: []string{"wi", "wai", "c", "Lab", "--ip"},
			assert: func(t *testing.T, cmd *controlpb.RunCommand) {
				wait := cmd.GetWaitWifiConnected()
				if wait == nil || wait.GetSsid() != "Lab" || !wait.GetRequireIp() {
					t.Fatalf("wait = %#v", wait)
				}
			},
		},
		{
			name: "traceroute",
			args: []string{"tr", "example.test"},
			assert: func(t *testing.T, cmd *controlpb.RunCommand) {
				if trace := cmd.GetTraceroute(); trace == nil || trace.GetHost() != "example.test" {
					t.Fatalf("traceroute = %#v", trace)
				}
			},
		},
		{
			name: "path mtu",
			args: []string{"path", "example.test"},
			assert: func(t *testing.T, cmd *controlpb.RunCommand) {
				if pathMtu := cmd.GetPathMtu(); pathMtu == nil || pathMtu.GetHost() != "example.test" {
					t.Fatalf("path-mtu = %#v", pathMtu)
				}
			},
		},
		{
			name: "global ip",
			args: []string{"gl"},
			assert: func(t *testing.T, cmd *controlpb.RunCommand) {
				if global := cmd.GetGlobalIp(); global == nil || global.GetFamily() != controlpb.IpFamily_IP_FAMILY_ALL {
					t.Fatalf("global-ip = %#v", global)
				}
			},
		},
		{
			name: "download",
			args: []string{"do", "https://example.test/file"},
			assert: func(t *testing.T, cmd *controlpb.RunCommand) {
				if wget := cmd.GetWget(); wget == nil || wget.GetUrl() != "https://example.test/file" {
					t.Fatalf("download = %#v", wget)
				}
			},
		},
		{
			name: "dns",
			args: []string{"dn", "example.test"},
			assert: func(t *testing.T, cmd *controlpb.RunCommand) {
				if dns := cmd.GetResolveDns(); dns == nil || dns.GetName() != "example.test" {
					t.Fatalf("dns = %#v", dns)
				}
			},
		},
		{
			name: "http",
			args: []string{"ht", "https://example.test"},
			assert: func(t *testing.T, cmd *controlpb.RunCommand) {
				if http := cmd.GetHttpCheck(); http == nil || http.GetUrl() != "https://example.test" {
					t.Fatalf("http = %#v", http)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := mustBuild(t, tt.args...)
			tt.assert(t, cmd)
		})
	}
}

func TestBuildCommandNetworkChecks(t *testing.T) {
	tests := []struct {
		name   string
		args   []string
		assert func(*testing.T, *controlpb.RunCommand)
	}{
		{
			name: "ip",
			args: []string{"ip"},
			assert: func(t *testing.T, cmd *controlpb.RunCommand) {
				if cmd.GetGetIpStatus() == nil || cmd.GetGetIpStatus().GetSelector() == nil {
					t.Fatalf("ip command = %#v", cmd.GetGetIpStatus())
				}
			},
		},
		{
			name: "ping size timeout",
			args: []string{"ping", "example.test", "5", "--size", "128", "--timeout", "7000"},
			assert: func(t *testing.T, cmd *controlpb.RunCommand) {
				ping := cmd.GetPing()
				if ping == nil || ping.GetHost() != "example.test" || ping.GetCount() != 5 || ping.GetSizeBytes() != 128 || ping.GetTimeoutMs() != 7000 {
					t.Fatalf("ping = %#v", ping)
				}
				assertEmptySelector(t, ping.GetSelector())
			},
		},
		{
			name: "traceroute",
			args: []string{"traceroute", "example.test", "12", "--via", "192.0.2.1", "--via", "gateway.example", "--size", "80", "--timeout", "30000"},
			assert: func(t *testing.T, cmd *controlpb.RunCommand) {
				trace := cmd.GetTraceroute()
				if trace == nil || trace.GetMaxHops() != 12 || trace.GetSizeBytes() != 80 || trace.GetTimeoutMs() != 30000 {
					t.Fatalf("traceroute = %#v", trace)
				}
				assertEmptySelector(t, trace.GetSelector())
			},
		},
		{
			name: "path mtu",
			args: []string{"path-mtu", "example.test", "--min-mtu", "1200", "--max-mtu", "1500", "--timeout", "30000"},
			assert: func(t *testing.T, cmd *controlpb.RunCommand) {
				pathMtu := cmd.GetPathMtu()
				if pathMtu == nil || pathMtu.GetHost() != "example.test" || pathMtu.GetMinMtuBytes() != 1200 || pathMtu.GetMaxMtuBytes() != 1500 || pathMtu.GetTimeoutMs() != 30000 {
					t.Fatalf("path-mtu = %#v", pathMtu)
				}
				assertEmptySelector(t, pathMtu.GetSelector())
			},
		},
		{
			name: "global ip",
			args: []string{"global-ip", "ipv6", "--timeout", "7000"},
			assert: func(t *testing.T, cmd *controlpb.RunCommand) {
				global := cmd.GetGlobalIp()
				if global == nil || global.GetFamily() != controlpb.IpFamily_IP_FAMILY_IPV6 || global.GetTimeoutMs() != 7000 {
					t.Fatalf("global-ip = %#v", global)
				}
				assertEmptySelector(t, global.GetSelector())
			},
		},
		{
			name: "download",
			args: []string{"download", "https://example.test/file", "--timeout", "8000"},
			assert: func(t *testing.T, cmd *controlpb.RunCommand) {
				wget := cmd.GetWget()
				if wget == nil || wget.GetUrl() != "https://example.test/file" || wget.GetTimeoutMs() != 8000 {
					t.Fatalf("wget = %#v", wget)
				}
				assertEmptySelector(t, wget.GetSelector())
			},
		},
		{
			name: "dns aaaa",
			args: []string{"dns", "example.test", "AAAA", "--timeout", "9000"},
			assert: func(t *testing.T, cmd *controlpb.RunCommand) {
				resolve := cmd.GetResolveDns()
				if resolve == nil || resolve.GetName() != "example.test" || !slices.Equal(resolve.GetQtypes(), []controlpb.DnsRecordType{controlpb.DnsRecordType_DNS_RECORD_TYPE_AAAA}) {
					t.Fatalf("resolve = %#v", resolve)
				}
				if resolve.GetTimeoutMs() != 9000 {
					t.Fatalf("resolve timeout = %d", resolve.GetTimeoutMs())
				}
				assertEmptySelector(t, resolve.GetSelector())
			},
		},
		{
			name: "http expected",
			args: []string{"http", "https://example.test/health", "204"},
			assert: func(t *testing.T, cmd *controlpb.RunCommand) {
				http := cmd.GetHttpCheck()
				if http == nil || http.GetExpectedStatus() != 204 || http.GetTimeoutMs() != 5000 {
					t.Fatalf("http = %#v", http)
				}
				assertEmptySelector(t, http.GetSelector())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := mustBuild(t, tt.args...)
			tt.assert(t, cmd)
		})
	}
}

func TestBuildCommandWithOptionsTracerouteVia(t *testing.T) {
	cmd, options, err := buildCommandWithOptions([]string{"tr", "example.test", "12", "--via", "192.0.2.1", "--via", "gateway.example"})
	if err != nil {
		t.Fatalf("buildCommandWithOptions() error = %v", err)
	}
	if cmd.GetTraceroute() == nil {
		t.Fatalf("traceroute command = nil")
	}
	if !slices.Equal(options.TracerouteRequiredHops, []string{"192.0.2.1", "gateway.example"}) {
		t.Fatalf("traceroute required hops = %#v", options.TracerouteRequiredHops)
	}
}

func TestBuildCommandRejectsInvalidInput(t *testing.T) {
	tests := [][]string{
		{"wifi"},
		{"wifi", "connect", "ssid"},
		{"wifi", "connect", "ssid", "pass", "--timeout"},
		{"wifi", "connect", "ssid", "pass", "--security", "wpa3"},
		{"wifi", "connect", "ssid", "pass", "--randomize-mac"},
		{"wifi", "connect", "ssid", "pass", "--mac-randomization", "spoofed"},
		{"wifi", "scan", "7ghz"},
		{"wifi", "s"},
		{"wifi", "d"},
		{"wifi", "assert", "ssid", "Lab"},
		{"wifi", "assert", "ip"},
		{"wifi", "cycle", "ssid", "pass", "--randomize-mac"},
		{"d", "example.test"},
		{"ping"},
		{"ping", "host", "2", "64"},
		{"ping", "host", "-s", "64"},
		{"ping", "host", "--bad"},
		{"ip", "wifi"},
		{"ping", "host", "--network", "wifi"},
		{"download", "https://example.test", "--iface", "wlan0"},
		{"ping", "host", "--ssid", "Lab"},
		{"traceroute", "host", "--via"},
		{"traceroute", "host", "--require-hop", "gw"},
		{"traceroute", "host", "-s", "64"},
		{"path-mtu"},
		{"path-mtu", "host", "--size", "1500"},
		{"path-mtu", "host", "--min-mtu"},
		{"global-ip", "bogus"},
		{"global-ip", "ipv4", "--family", "ipv6"},
		{"global-ip", "--timeout"},
		{"http", "https://example.test", "--expected", "204"},
		{"http", "https://example.test", "0"},
		{"wget", "https://example.test"},
		{"resolve", "example.test", "TXT"},
		{"dns", "example.test", "A+AAAA"},
		{"dns", "resolve", "example.test"},
	}

	for _, args := range tests {
		t.Run(stringsJoin(args), func(t *testing.T) {
			if _, err := buildCommand(args); err == nil {
				t.Fatalf("buildCommand(%v) error = nil", args)
			}
		})
	}
}

func TestTimeoutFor(t *testing.T) {
	if got := timeoutFor(mustBuild(t, "wifi", "scan", "fresh", "--timeout", "9000")); got != 14*time.Second {
		t.Fatalf("fresh scan timeout = %v", got)
	}
	if got := timeoutFor(mustBuild(t, "ping", "example.test", "2")); got != 10*time.Second {
		t.Fatalf("ping timeout = %v", got)
	}
	if got := timeoutFor(mustBuild(t, "path-mtu", "example.test", "--timeout", "7000")); got != 10*time.Second {
		t.Fatalf("path-mtu timeout = %v", got)
	}
	if got := timeoutFor(mustBuild(t, "global-ip", "ipv4", "--timeout", "7000")); got != 10*time.Second {
		t.Fatalf("global-ip ipv4 timeout = %v", got)
	}
	if got := timeoutFor(mustBuild(t, "global-ip", "--timeout", "7000")); got != 17*time.Second {
		t.Fatalf("global-ip all timeout = %v", got)
	}
	if got := timeoutFor(mustBuild(t, "wifi", "cycle", "Lab", "pass", "--count", "2", "--timeout", "1000", "--pause", "250")); got != 32500*time.Millisecond {
		t.Fatalf("cycle timeout = %v", got)
	}
}

func TestRedactedCommand(t *testing.T) {
	connect := mustBuild(t, "wifi", "connect", "Lab", "super-secret")
	if strings.Contains(connect.GetLabel(), "super-secret") {
		t.Fatalf("connect label contains secret: %q", connect.GetLabel())
	}
	connect.Label = "wifi connect Lab super-secret"
	redacted := redactedCommand(connect)
	if got := redacted.GetConnectWifi().GetPassphrase(); got != "<redacted>" {
		t.Fatalf("redacted connect passphrase = %q", got)
	}
	if strings.Contains(redacted.GetLabel(), "super-secret") {
		t.Fatalf("redacted connect label contains secret: %q", redacted.GetLabel())
	}
	if got := connect.GetConnectWifi().GetPassphrase(); got != "super-secret" {
		t.Fatalf("original connect passphrase mutated to %q", got)
	}

	cycle := mustBuild(t, "wifi", "cycle", "Lab", "super-secret")
	if strings.Contains(cycle.GetLabel(), "super-secret") {
		t.Fatalf("cycle label contains secret: %q", cycle.GetLabel())
	}
	cycle.Label = "wifi cycle Lab super-secret"
	redacted = redactedCommand(cycle)
	if got := redacted.GetCycleWifi().GetConnect().GetPassphrase(); got != "<redacted>" {
		t.Fatalf("redacted cycle passphrase = %q", got)
	}
	if strings.Contains(redacted.GetLabel(), "super-secret") {
		t.Fatalf("redacted cycle label contains secret: %q", redacted.GetLabel())
	}
	if got := cycle.GetCycleWifi().GetConnect().GetPassphrase(); got != "super-secret" {
		t.Fatalf("original cycle passphrase mutated to %q", got)
	}
}

func mustBuild(t *testing.T, args ...string) *controlpb.RunCommand {
	t.Helper()
	cmd, err := buildCommand(args)
	if err != nil {
		t.Fatalf("buildCommand(%v) error = %v", args, err)
	}
	return cmd
}

func assertEmptySelector(t *testing.T, selector *controlpb.NetworkSelector) {
	t.Helper()
	if selector == nil {
		t.Fatalf("selector = nil")
	}
	if selector.GetSsid() != "" {
		t.Fatalf("selector ssid = %q", selector.GetSsid())
	}
}

func stringsJoin(values []string) string {
	return strings.Join(values, " ")
}
