package linuxcli

import (
	"strings"
	"testing"

	cmdop "dropcheck/controller/internal/command"
	"dropcheck/controller/internal/controlpb"
	"dropcheck/controller/internal/pipeline"
)

func TestExtractOptions(t *testing.T) {
	opts, rest, err := ExtractOptions([]string{"--format=json", "--target", "pixel", "show", "devices"})
	if err != nil {
		t.Fatalf("ExtractOptions() error = %v", err)
	}
	if opts.Format != pipeline.FormatJSON || opts.Target != "pixel" || opts.All {
		t.Fatalf("options = %#v", opts)
	}
	if got, want := strings.Join(rest, " "), "show devices"; got != want {
		t.Fatalf("rest = %q, want %q", got, want)
	}

	opts, rest, err = ExtractOptions([]string{"--all", "--", "--target", "literal"})
	if err != nil {
		t.Fatalf("ExtractOptions(-- sentinel) error = %v", err)
	}
	if !opts.All {
		t.Fatalf("options = %#v, want all target", opts)
	}
	if got, want := strings.Join(rest, " "), "--target literal"; got != want {
		t.Fatalf("rest = %q, want %q", got, want)
	}

	if _, _, err := ExtractOptions([]string{"--all", "--target", "pixel"}); err == nil {
		t.Fatalf("ExtractOptions(conflicting target) error = nil")
	}
	if _, _, err := ExtractOptions([]string{"--format", "xml"}); err == nil {
		t.Fatalf("ExtractOptions(invalid format) error = nil")
	}
}

func TestParseTopLevelCommands(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		check func(*testing.T, Command)
	}{
		{
			name: "devices",
			args: []string{"show", "devices"},
			check: func(t *testing.T, cmd Command) {
				t.Helper()
				if cmd.Kind != Devices {
					t.Fatalf("kind = %v, want Devices", cmd.Kind)
				}
			},
		},
		{
			name: "sync standalone runs",
			args: []string{"sync", "standalone", "runs", "--output", "out", "--limit=5", "--keep-unsynced"},
			check: func(t *testing.T, cmd Command) {
				t.Helper()
				if cmd.Kind != StandaloneSync {
					t.Fatalf("kind = %v, want StandaloneSync", cmd.Kind)
				}
				if cmd.StandaloneSyncOutput != "out" || cmd.StandaloneSyncLimit != "5" || cmd.StandaloneSyncMark {
					t.Fatalf("standalone sync = %#v", cmd)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, err := Parse(tt.args)
			if err != nil {
				t.Fatalf("Parse(%q) error = %v", strings.Join(tt.args, " "), err)
			}
			tt.check(t, cmd)
		})
	}
}

func TestParseAgentCommandSurface(t *testing.T) {
	tests := []struct {
		name         string
		args         []string
		check        func(*testing.T, *controlpb.RunCommand)
		checkOptions func(*testing.T, cmdop.Options)
	}{
		{
			name: "ip status",
			args: []string{"show", "ip", "status"},
			check: func(t *testing.T, run *controlpb.RunCommand) {
				t.Helper()
				if run.GetGetIpStatus() == nil {
					t.Fatalf("command = %T, want GetIpStatus", run.GetCommand())
				}
			},
		},
		{
			name: "wifi eht",
			args: []string{"show", "wifi", "eht"},
			check: func(t *testing.T, run *controlpb.RunCommand) {
				t.Helper()
				if run.GetGetWifiDiagnostics() == nil {
					t.Fatalf("command = %T, want GetWifiDiagnostics", run.GetCommand())
				}
			},
			checkOptions: func(t *testing.T, options cmdop.Options) {
				t.Helper()
				if options.WifiRenderMode != cmdop.WifiRenderModeEHT {
					t.Fatalf("wifi render mode = %q, want %q", options.WifiRenderMode, cmdop.WifiRenderModeEHT)
				}
			},
		},
		{
			name: "wifi eht ssid",
			args: []string{"show", "wifi", "eht", "ssid", "temp-life26"},
			check: func(t *testing.T, run *controlpb.RunCommand) {
				t.Helper()
				if run.GetGetWifiDiagnostics() == nil {
					t.Fatalf("command = %T, want GetWifiDiagnostics", run.GetCommand())
				}
				if run.GetLabel() != "wifi eht ssid temp-life26" {
					t.Fatalf("label = %q, want wifi eht ssid temp-life26", run.GetLabel())
				}
			},
			checkOptions: func(t *testing.T, options cmdop.Options) {
				t.Helper()
				if options.WifiEHTSSID != "temp-life26" {
					t.Fatalf("ssid filter = %q, want temp-life26", options.WifiEHTSSID)
				}
			},
		},
		{
			name: "wifi eht bssid",
			args: []string{"show", "wifi", "eht", "--bssid", "aa:bb:cc:dd:ee:ff"},
			check: func(t *testing.T, run *controlpb.RunCommand) {
				t.Helper()
				if run.GetGetWifiDiagnostics() == nil {
					t.Fatalf("command = %T, want GetWifiDiagnostics", run.GetCommand())
				}
				if run.GetLabel() != "wifi eht bssid aa:bb:cc:dd:ee:ff" {
					t.Fatalf("label = %q, want wifi eht bssid aa:bb:cc:dd:ee:ff", run.GetLabel())
				}
			},
			checkOptions: func(t *testing.T, options cmdop.Options) {
				t.Helper()
				if options.WifiEHTBSSID != "aa:bb:cc:dd:ee:ff" {
					t.Fatalf("bssid filter = %q, want aa:bb:cc:dd:ee:ff", options.WifiEHTBSSID)
				}
			},
		},
		{
			name: "wifi eht fresh ssid",
			args: []string{"show", "wifi", "eht", "fresh", "--timeout", "9000", "--ssid", "temp-life26"},
			check: func(t *testing.T, run *controlpb.RunCommand) {
				t.Helper()
				if run.GetGetWifiDiagnostics() == nil {
					t.Fatalf("command = %T, want GetWifiDiagnostics", run.GetCommand())
				}
				if run.GetLabel() != "wifi eht fresh --timeout 9000 ssid temp-life26" {
					t.Fatalf("label = %q, want wifi eht fresh --timeout 9000 ssid temp-life26", run.GetLabel())
				}
			},
			checkOptions: func(t *testing.T, options cmdop.Options) {
				t.Helper()
				if options.WifiEHTSSID != "temp-life26" || !options.WifiEHTFreshScan || options.WifiEHTFreshScanTimeoutMs != 9000 {
					t.Fatalf("wifi eht options = %#v", options)
				}
			},
		},
		{
			name: "fresh wifi scan",
			args: []string{"show", "wifi", "scan", "fresh", "5ghz", "--timeout", "8000"},
			check: func(t *testing.T, run *controlpb.RunCommand) {
				t.Helper()
				scan := run.GetGetFreshWifiScan()
				if scan == nil {
					t.Fatalf("command = %T, want GetFreshWifiScan", run.GetCommand())
				}
				if scan.GetBand() != controlpb.WifiBand_WIFI_BAND_5_GHZ || scan.GetTimeoutMs() != 8000 {
					t.Fatalf("fresh scan = %#v", scan)
				}
			},
		},
		{
			name: "standalone runs",
			args: []string{"show", "standalone", "runs", "--limit", "5", "--synced"},
			check: func(t *testing.T, run *controlpb.RunCommand) {
				t.Helper()
				list := run.GetListStandaloneRuns()
				if list == nil {
					t.Fatalf("command = %T, want ListStandaloneRuns", run.GetCommand())
				}
				if list.GetLimit() != 5 || !list.GetIncludeSynced() {
					t.Fatalf("list standalone = %#v", list)
				}
			},
		},
		{
			name: "standalone run",
			args: []string{"show", "standalone", "run", "run-123"},
			check: func(t *testing.T, run *controlpb.RunCommand) {
				t.Helper()
				get := run.GetGetStandaloneRun()
				if get == nil {
					t.Fatalf("command = %T, want GetStandaloneRun", run.GetCommand())
				}
				if get.GetRunId() != "run-123" || get.GetMarkSynced() {
					t.Fatalf("get standalone run = %#v", get)
				}
			},
		},
		{
			name: "wifi connect",
			args: []string{"request", "wifi", "connect", "Lab", "--passphrase", "secret", "--security", "wpa3", "--band", "6ghz", "--timeout", "25000"},
			check: func(t *testing.T, run *controlpb.RunCommand) {
				t.Helper()
				connect := run.GetConnectWifi()
				if connect == nil {
					t.Fatalf("command = %T, want ConnectWifi", run.GetCommand())
				}
				if connect.GetSsid() != "Lab" || connect.GetPassphrase() != "secret" {
					t.Fatalf("connect identity = %#v", connect)
				}
				if connect.GetSecurity() != controlpb.ConnectWifi_SECURITY_WPA3_SAE ||
					connect.GetBand() != controlpb.WifiBand_WIFI_BAND_6_GHZ ||
					connect.GetTimeoutMs() != 25000 {
					t.Fatalf("connect options = %#v", connect)
				}
			},
		},
		{
			name: "wifi wait",
			args: []string{"request", "wifi", "wait", "connected", "Lab", "--ip", "--validated", "--timeout", "12000"},
			check: func(t *testing.T, run *controlpb.RunCommand) {
				t.Helper()
				wait := run.GetWaitWifiConnected()
				if wait == nil {
					t.Fatalf("command = %T, want WaitWifiConnected", run.GetCommand())
				}
				if wait.GetSsid() != "Lab" || !wait.GetRequireIp() || !wait.GetRequireValidated() || wait.GetTimeoutMs() != 12000 {
					t.Fatalf("wait = %#v", wait)
				}
			},
		},
		{
			name: "wifi assert",
			args: []string{"request", "wifi", "assert", "--ssid", "Lab", "--security", "wpa2", "--band", "5ghz", "--ip", "--timeout", "5000"},
			check: func(t *testing.T, run *controlpb.RunCommand) {
				t.Helper()
				assert := run.GetAssertWifi()
				if assert == nil {
					t.Fatalf("command = %T, want AssertWifi", run.GetCommand())
				}
				if assert.GetSsid() != "Lab" || assert.GetSecurity() != controlpb.ConnectWifi_SECURITY_WPA2_PSK ||
					assert.GetBand() != controlpb.WifiBand_WIFI_BAND_5_GHZ || !assert.GetRequireIp() || assert.GetTimeoutMs() != 5000 {
					t.Fatalf("assert = %#v", assert)
				}
			},
		},
		{
			name: "wifi reconnect",
			args: []string{"request", "wifi", "reconnect", "--timeout", "12000"},
			check: func(t *testing.T, run *controlpb.RunCommand) {
				t.Helper()
				reconnect := run.GetReconnectWifi()
				if reconnect == nil {
					t.Fatalf("command = %T, want ReconnectWifi", run.GetCommand())
				}
				if reconnect.GetTimeoutMs() != 12000 {
					t.Fatalf("reconnect = %#v", reconnect)
				}
			},
		},
		{
			name: "monitor wifi",
			args: []string{"request", "monitor", "wifi", "--duration", "1500", "--interval", "500"},
			check: func(t *testing.T, run *controlpb.RunCommand) {
				t.Helper()
				monitor := run.GetMonitorWifi()
				if monitor == nil {
					t.Fatalf("command = %T, want MonitorWifi", run.GetCommand())
				}
				if monitor.GetDurationMs() != 1500 || monitor.GetIntervalMs() != 500 {
					t.Fatalf("monitor = %#v", monitor)
				}
			},
		},
		{
			name: "ping",
			args: []string{"request", "ping", "1.1.1.1", "2", "--timeout", "8000"},
			check: func(t *testing.T, run *controlpb.RunCommand) {
				t.Helper()
				ping := run.GetPing()
				if ping == nil {
					t.Fatalf("command = %T, want Ping", run.GetCommand())
				}
				if ping.GetHost() != "1.1.1.1" || ping.GetCount() != 2 || ping.GetTimeoutMs() != 8000 {
					t.Fatalf("ping = %#v", ping)
				}
			},
		},
		{
			name: "traceroute",
			args: []string{"request", "traceroute", "1.1.1.1", "--max-hops", "30", "--via", "10.0.0.1", "--via", "10.0.0.2", "--timeout", "60000"},
			check: func(t *testing.T, run *controlpb.RunCommand) {
				t.Helper()
				trace := run.GetTraceroute()
				if trace == nil {
					t.Fatalf("command = %T, want Traceroute", run.GetCommand())
				}
				if trace.GetHost() != "1.1.1.1" || trace.GetMaxHops() != 30 || trace.GetTimeoutMs() != 60000 {
					t.Fatalf("traceroute = %#v", trace)
				}
			},
			checkOptions: func(t *testing.T, opts cmdop.Options) {
				t.Helper()
				if got, want := strings.Join(opts.TracerouteRequiredHops, ","), "10.0.0.1,10.0.0.2"; got != want {
					t.Fatalf("traceroute required hops = %q, want %q", got, want)
				}
			},
		},
		{
			name: "path mtu",
			args: []string{"request", "path-mtu", "1.1.1.1", "--min-mtu", "576", "--max-mtu", "1200", "--timeout", "20000"},
			check: func(t *testing.T, run *controlpb.RunCommand) {
				t.Helper()
				mtu := run.GetPathMtu()
				if mtu == nil {
					t.Fatalf("command = %T, want PathMtu", run.GetCommand())
				}
				if mtu.GetHost() != "1.1.1.1" || mtu.GetMinMtuBytes() != 576 || mtu.GetMaxMtuBytes() != 1200 || mtu.GetTimeoutMs() != 20000 {
					t.Fatalf("path mtu = %#v", mtu)
				}
			},
		},
		{
			name: "global ip",
			args: []string{"request", "global-ip", "--family", "ipv6", "--timeout", "10000"},
			check: func(t *testing.T, run *controlpb.RunCommand) {
				t.Helper()
				global := run.GetGlobalIp()
				if global == nil {
					t.Fatalf("command = %T, want GlobalIp", run.GetCommand())
				}
				if global.GetFamily() != controlpb.IpFamily_IP_FAMILY_IPV6 || global.GetTimeoutMs() != 10000 {
					t.Fatalf("global ip = %#v", global)
				}
			},
		},
		{
			name: "dns",
			args: []string{"request", "dns", "example.com", "AAAA", "--timeout", "8000"},
			check: func(t *testing.T, run *controlpb.RunCommand) {
				t.Helper()
				dns := run.GetResolveDns()
				if dns == nil {
					t.Fatalf("command = %T, want ResolveDns", run.GetCommand())
				}
				if dns.GetName() != "example.com" || dns.GetTimeoutMs() != 8000 ||
					len(dns.GetQtypes()) != 1 || dns.GetQtypes()[0] != controlpb.DnsRecordType_DNS_RECORD_TYPE_AAAA {
					t.Fatalf("dns = %#v", dns)
				}
			},
		},
		{
			name: "http",
			args: []string{"request", "http", "https://example.com", "--expected-status", "204", "--timeout", "10000"},
			check: func(t *testing.T, run *controlpb.RunCommand) {
				t.Helper()
				http := run.GetHttpCheck()
				if http == nil {
					t.Fatalf("command = %T, want HttpCheck", run.GetCommand())
				}
				if http.GetUrl() != "https://example.com" || http.GetExpectedStatus() != 204 || http.GetTimeoutMs() != 10000 {
					t.Fatalf("http = %#v", http)
				}
			},
		},
		{
			name: "download",
			args: []string{"request", "download", "https://example.com/file", "--timeout", "15000"},
			check: func(t *testing.T, run *controlpb.RunCommand) {
				t.Helper()
				wget := run.GetWget()
				if wget == nil {
					t.Fatalf("command = %T, want Wget", run.GetCommand())
				}
				if wget.GetUrl() != "https://example.com/file" || wget.GetTimeoutMs() != 15000 {
					t.Fatalf("download = %#v", wget)
				}
			},
		},
		{
			name: "standalone run once",
			args: []string{"request", "standalone", "run", "once", "--festa", "smoke", "--save"},
			check: func(t *testing.T, run *controlpb.RunCommand) {
				t.Helper()
				once := run.GetRunStandaloneOnce()
				if once == nil {
					t.Fatalf("command = %T, want RunStandaloneOnce", run.GetCommand())
				}
				if once.GetFesta() != "smoke" || !once.GetSave() {
					t.Fatalf("run standalone once = %#v", once)
				}
			},
		},
		{
			name: "clear standalone runs",
			args: []string{"clear", "standalone", "runs", "all"},
			check: func(t *testing.T, run *controlpb.RunCommand) {
				t.Helper()
				clear := run.GetClearStandaloneRuns()
				if clear == nil {
					t.Fatalf("command = %T, want ClearStandaloneRuns", run.GetCommand())
				}
				if !clear.GetAll() || clear.GetSyncedOnly() {
					t.Fatalf("clear standalone = %#v", clear)
				}
			},
		},
		{
			name: "delete standalone festa",
			args: []string{"configure", "delete", "standalone", "festa", "smoke"},
			check: func(t *testing.T, run *controlpb.RunCommand) {
				t.Helper()
				edit := run.GetEditStandaloneConfig()
				if edit == nil {
					t.Fatalf("command = %T, want EditStandaloneConfig", run.GetCommand())
				}
				edits := edit.GetEdits()
				if len(edits) != 1 || edits[0].GetAction() != controlpb.StandaloneEdit_ACTION_DELETE ||
					strings.Join(edits[0].GetPath(), ".") != "festa.smoke" {
					t.Fatalf("standalone delete edits = %#v", edits)
				}
			},
		},
		{
			name: "set standalone upload wifi",
			args: []string{"configure", "set", "standalone", "upload", "via", "wifi", "essid", "NOC", "passphrase", "secret", "security", "wpa3", "band", "6ghz", "timeout", "5s"},
			check: func(t *testing.T, run *controlpb.RunCommand) {
				t.Helper()
				edit := run.GetEditStandaloneConfig()
				if edit == nil {
					t.Fatalf("command = %T, want EditStandaloneConfig", run.GetCommand())
				}
				edits := edit.GetEdits()
				if len(edits) != 6 ||
					strings.Join(edits[0].GetPath(), ".") != "upload.wifi" ||
					edits[0].GetAction() != controlpb.StandaloneEdit_ACTION_DELETE ||
					strings.Join(edits[1].GetPath(), ".") != "upload.wifi.ssid" ||
					edits[1].GetValue() != "NOC" ||
					strings.Join(edits[5].GetPath(), ".") != "upload.wifi.timeout_ms" ||
					edits[5].GetValue() != "5000" {
					t.Fatalf("standalone upload wifi edits = %#v", edits)
				}
			},
		},
		{
			name: "set standalone festa wifi match",
			args: []string{"configure", "set", "standalone", "festa", "smoke", "wifi", "mgmt", "match", "essid", "NOC", "mac-randomization", "auto"},
			check: func(t *testing.T, run *controlpb.RunCommand) {
				t.Helper()
				edit := run.GetEditStandaloneConfig()
				if edit == nil {
					t.Fatalf("command = %T, want EditStandaloneConfig", run.GetCommand())
				}
				edits := edit.GetEdits()
				if len(edits) != 2 ||
					strings.Join(edits[0].GetPath(), ".") != "festa.smoke.wifi.mgmt.match.essid" ||
					edits[0].GetValue() != "NOC" ||
					strings.Join(edits[1].GetPath(), ".") != "festa.smoke.wifi.mgmt.mac_randomization" ||
					edits[1].GetValue() != "auto" {
					t.Fatalf("standalone festa wifi match edits = %#v", edits)
				}
			},
		},
		{
			name: "set standalone festa wifi passphrase",
			args: []string{"configure", "set", "standalone", "festa", "smoke", "wifi", "mgmt", "passphrase", "secret", "security", "transition"},
			check: func(t *testing.T, run *controlpb.RunCommand) {
				t.Helper()
				edit := run.GetEditStandaloneConfig()
				if edit == nil {
					t.Fatalf("command = %T, want EditStandaloneConfig", run.GetCommand())
				}
				edits := edit.GetEdits()
				if len(edits) != 2 ||
					strings.Join(edits[0].GetPath(), ".") != "festa.smoke.wifi.mgmt.passphrase" ||
					edits[0].GetValue() != "secret" ||
					strings.Join(edits[1].GetPath(), ".") != "festa.smoke.wifi.mgmt.security" ||
					edits[1].GetValue() != "transition" {
					t.Fatalf("standalone festa wifi passphrase edits = %#v", edits)
				}
			},
		},
		{
			name: "set standalone festa named ping check",
			args: []string{"configure", "set", "standalone", "festa", "smoke", "check", "cloudflare", "test", "ping", "host", "1.1.1.1", "count", "1", "timeout", "8s"},
			check: func(t *testing.T, run *controlpb.RunCommand) {
				t.Helper()
				edit := run.GetEditStandaloneConfig()
				if edit == nil {
					t.Fatalf("command = %T, want EditStandaloneConfig", run.GetCommand())
				}
				edits := edit.GetEdits()
				if len(edits) != 4 ||
					strings.Join(edits[0].GetPath(), ".") != "festa.smoke.check.cloudflare.test" ||
					edits[0].GetValue() != "ping" ||
					strings.Join(edits[1].GetPath(), ".") != "festa.smoke.check.cloudflare.host" ||
					edits[1].GetValue() != "1.1.1.1" ||
					strings.Join(edits[3].GetPath(), ".") != "festa.smoke.check.cloudflare.timeout_ms" ||
					edits[3].GetValue() != "8000" {
					t.Fatalf("standalone named ping check edits = %#v", edits)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, err := Parse(tt.args)
			if err != nil {
				t.Fatalf("Parse(%q) error = %v", strings.Join(tt.args, " "), err)
			}
			if cmd.Kind != AgentCommand {
				t.Fatalf("kind = %v, want AgentCommand", cmd.Kind)
			}
			run, opts, err := cmdop.BuildRunCommand(cmd.Operation)
			if err != nil {
				t.Fatalf("BuildRunCommand() error = %v", err)
			}
			tt.check(t, run)
			if tt.checkOptions != nil {
				tt.checkOptions(t, opts)
			}
		})
	}
}

func TestParseRejectsInvalidCLICommands(t *testing.T) {
	tests := [][]string{
		{"show", "ip"},
		{"show", "wifi", "mlo"},
		{"show", "wifi", "scan", "--timeout", "8000"},
		{"request", "wifi", "connect", "Lab"},
		{"request", "dns", "example.com", "--type", "A", "--type", "AAAA"},
		{"sync", "standalone", "runs", "--mark-synced", "--keep-unsynced"},
	}
	for _, args := range tests {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			if _, err := Parse(args); err == nil {
				t.Fatalf("Parse(%q) error = nil", strings.Join(args, " "))
			}
		})
	}
}
