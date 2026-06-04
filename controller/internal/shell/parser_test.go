package shell

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dropcheck/controller/internal/command"
	"dropcheck/controller/internal/controlpb"
)

func TestParseLineLocalCommandSurface(t *testing.T) {
	showDevices, err := ParseLine("show devices | display json | count")
	if err != nil {
		t.Fatalf("ParseLine(show devices) error = %v", err)
	}
	if showDevices.Kind != ShowDevices {
		t.Fatalf("show devices kind = %v, want %v", showDevices.Kind, ShowDevices)
	}
	if got := showDevices.Pipeline.StageCount(); got != 1 {
		t.Fatalf("pipeline stage count = %d, want 1", got)
	}
	if !showDevices.Pipeline.DisplayJSON() {
		t.Fatalf("pipeline should request JSON display")
	}

	showConfig, err := ParseLine("show config standalone")
	if err != nil {
		t.Fatalf("ParseLine(show config standalone) error = %v", err)
	}
	if showConfig.Kind != ShowConfig || showConfig.ConfigScope != "standalone" {
		t.Fatalf("show config = kind %v scope %q", showConfig.Kind, showConfig.ConfigScope)
	}

	showConfigSet, err := ParseLine("show config | display set | match standalone")
	if err != nil {
		t.Fatalf("ParseLine(show config display set) error = %v", err)
	}
	if showConfigSet.Kind != ShowConfig || !showConfigSet.Pipeline.DisplaySet() || showConfigSet.Pipeline.StageCount() != 1 {
		t.Fatalf("show config display set = kind %v displaySet %t stages %d", showConfigSet.Kind, showConfigSet.Pipeline.DisplaySet(), showConfigSet.Pipeline.StageCount())
	}

	sync, err := ParseLine("sync standalone runs output /tmp/dropcheck-e2e limit 2 keep-unsynced")
	if err != nil {
		t.Fatalf("ParseLine(sync standalone runs) error = %v", err)
	}
	if sync.Kind != StandaloneSync {
		t.Fatalf("sync kind = %v, want %v", sync.Kind, StandaloneSync)
	}
	if sync.StandaloneSyncOutput != "/tmp/dropcheck-e2e" || sync.StandaloneSyncLimit != "2" || sync.StandaloneSyncMark {
		t.Fatalf("sync options = output %q limit %q mark %t", sync.StandaloneSyncOutput, sync.StandaloneSyncLimit, sync.StandaloneSyncMark)
	}

	adbDiag, err := ParseLine("show adb dumpsys connectivity requests | display json")
	if err != nil {
		t.Fatalf("ParseLine(show adb dumpsys connectivity requests) error = %v", err)
	}
	if adbDiag.Kind != ADBDiagnostics || adbDiag.ADBDiagnosticsKind != "dumpsys-connectivity-requests" {
		t.Fatalf("adb diagnostics = kind %v adb kind %q", adbDiag.Kind, adbDiag.ADBDiagnosticsKind)
	}
	if !adbDiag.Pipeline.DisplayJSON() {
		t.Fatalf("adb diagnostics pipeline should request JSON display")
	}
}

func TestParseConfigureLineStandaloneLiveWatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "seed.yml")
	if err := os.WriteFile(path, []byte(strings.TrimSpace(`
version: 1
name: seed
targets:
  - name: cs1
    ssid: cs1
checks:
  - type: ping
    host: 1.1.1.1
`)+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", path, err)
	}

	cmd, err := ParseConfigureLine("set standalone live watch " + path)
	if err != nil {
		t.Fatalf("ParseConfigureLine() error = %v", err)
	}
	if cmd.Kind != AgentCommand {
		t.Fatalf("kind = %v, want AgentCommand", cmd.Kind)
	}
	run, _, err := command.BuildRunCommand(cmd.Operation)
	if err != nil {
		t.Fatalf("BuildRunCommand() error = %v", err)
	}
	edit := run.GetEditStandaloneConfig()
	if edit == nil {
		t.Fatalf("command = %T, want EditStandaloneConfig", run.GetCommand())
	}
	if got := strings.Join(edit.GetEdits()[1].GetPath(), "/"); got != "festa/live/wifi/cs1/match/essid" {
		t.Fatalf("seed path = %q, want festa/live/wifi/cs1/match/essid", got)
	}
}

func TestParseLineAgentCommandSurface(t *testing.T) {
	tests := []struct {
		name      string
		line      string
		parse     func(string) (Command, error)
		operation string
		check     func(*testing.T, *controlpb.RunCommand)
	}{
		{
			name:      "show wifi status",
			line:      "show wifi status",
			operation: "wifi.status",
			check: func(t *testing.T, cmd *controlpb.RunCommand) {
				t.Helper()
				if cmd.GetGetWifiStatus() == nil {
					t.Fatalf("GetWifiStatus not set")
				}
			},
		},
		{
			name:      "show wifi eht",
			line:      "show wifi eht",
			operation: "wifi.eht",
			check: func(t *testing.T, cmd *controlpb.RunCommand) {
				t.Helper()
				if cmd.GetGetWifiDiagnostics() == nil {
					t.Fatalf("GetWifiDiagnostics not set")
				}
			},
		},
		{
			name:      "show wifi eht fresh",
			line:      "show wifi eht fresh timeout 9000",
			operation: "wifi.eht",
			check: func(t *testing.T, cmd *controlpb.RunCommand) {
				t.Helper()
				if cmd.GetGetWifiDiagnostics() == nil || cmd.GetLabel() != "wifi eht fresh --timeout 9000" {
					t.Fatalf("wifi eht fresh command = %#v", cmd)
				}
			},
		},
		{
			name:      "show wifi eht ssid",
			line:      "show wifi eht ssid temp-life26",
			operation: "wifi.eht",
			check: func(t *testing.T, cmd *controlpb.RunCommand) {
				t.Helper()
				if cmd.GetGetWifiDiagnostics() == nil || cmd.GetLabel() != "wifi eht ssid temp-life26" {
					t.Fatalf("wifi eht ssid command = %#v", cmd)
				}
			},
		},
		{
			name:      "show wifi eht bssid",
			line:      "show wifi eht bssid aa:bb:cc:dd:ee:ff",
			operation: "wifi.eht",
			check: func(t *testing.T, cmd *controlpb.RunCommand) {
				t.Helper()
				if cmd.GetGetWifiDiagnostics() == nil || cmd.GetLabel() != "wifi eht bssid aa:bb:cc:dd:ee:ff" {
					t.Fatalf("wifi eht bssid command = %#v", cmd)
				}
			},
		},
		{
			name:      "show ip status",
			line:      "show ip status",
			operation: "ip.status",
			check: func(t *testing.T, cmd *controlpb.RunCommand) {
				t.Helper()
				if cmd.GetGetIpStatus() == nil {
					t.Fatalf("GetIpStatus not set")
				}
			},
		},
		{
			name:      "show wifi fresh scan",
			line:      "show wifi scan fresh 5ghz timeout 9000",
			operation: "wifi.scan.fresh",
			check: func(t *testing.T, cmd *controlpb.RunCommand) {
				t.Helper()
				scan := cmd.GetGetFreshWifiScan()
				if scan == nil || scan.GetBand() != controlpb.WifiBand_WIFI_BAND_5_GHZ || scan.GetTimeoutMs() != 9000 {
					t.Fatalf("fresh scan = %#v", scan)
				}
			},
		},
		{
			name:      "show wifi brief scan",
			line:      "show wifi scan brief 5ghz",
			operation: "wifi.scan",
			check: func(t *testing.T, cmd *controlpb.RunCommand) {
				t.Helper()
				scan := cmd.GetGetWifiScan()
				if scan == nil || scan.GetBand() != controlpb.WifiBand_WIFI_BAND_5_GHZ || cmd.GetLabel() != "wifi scan brief 5ghz" {
					t.Fatalf("brief scan = %#v", cmd)
				}
			},
		},
		{
			name:      "show wifi brief mlo scan",
			line:      "show wifi scan brief mlo 5ghz",
			operation: "wifi.scan",
			check: func(t *testing.T, cmd *controlpb.RunCommand) {
				t.Helper()
				scan := cmd.GetGetWifiScan()
				if scan == nil || scan.GetBand() != controlpb.WifiBand_WIFI_BAND_5_GHZ || cmd.GetLabel() != "wifi scan brief mlo 5ghz" {
					t.Fatalf("brief mlo scan = %#v", cmd)
				}
			},
		},
		{
			name:      "configure standalone",
			line:      "set standalone enabled",
			parse:     ParseConfigureLine,
			operation: "standalone.config.edit",
			check: func(t *testing.T, cmd *controlpb.RunCommand) {
				t.Helper()
				edit := cmd.GetEditStandaloneConfig().GetEdits()[0]
				if edit.GetAction() != controlpb.StandaloneEdit_ACTION_SET || strings.Join(edit.GetPath(), ".") != "enabled" || edit.GetValue() != "true" {
					t.Fatalf("standalone enabled edit not set")
				}
			},
		},
		{
			name:      "standalone run once",
			line:      "standalone run once festa smoke save",
			parse:     ParseRequestLine,
			operation: "standalone.run.once",
			check: func(t *testing.T, cmd *controlpb.RunCommand) {
				t.Helper()
				run := cmd.GetRunStandaloneOnce()
				if run == nil || run.GetFesta() != "smoke" || !run.GetSave() {
					t.Fatalf("standalone run once = %#v", run)
				}
			},
		},
		{
			name:      "wifi connect",
			line:      "wifi connect passphrase secret security wpa3 band 6ghz timeout 12345 Lab",
			parse:     ParseRequestLine,
			operation: "wifi.connect",
			check: func(t *testing.T, cmd *controlpb.RunCommand) {
				t.Helper()
				connect := cmd.GetConnectWifi()
				if connect == nil || connect.GetSsid() != "Lab" || connect.GetSecurity() != controlpb.ConnectWifi_SECURITY_WPA3_SAE || connect.GetBand() != controlpb.WifiBand_WIFI_BAND_6_GHZ {
					t.Fatalf("connect = %#v", connect)
				}
			},
		},
		{
			name:      "wifi monitor",
			line:      "monitor wifi interval 250",
			parse:     ParseRequestLine,
			operation: "wifi.monitor",
			check: func(t *testing.T, cmd *controlpb.RunCommand) {
				t.Helper()
				monitor := cmd.GetMonitorWifi()
				if monitor == nil || monitor.GetDurationMs() != 10000 || monitor.GetIntervalMs() != 250 {
					t.Fatalf("monitor = %#v", monitor)
				}
			},
		},
		{
			name:      "wifi wait",
			line:      "wifi wait connected Lab ip validated timeout 12000",
			parse:     ParseRequestLine,
			operation: "wifi.wait",
			check: func(t *testing.T, cmd *controlpb.RunCommand) {
				t.Helper()
				wait := cmd.GetWaitWifiConnected()
				if wait == nil || wait.GetSsid() != "Lab" || !wait.GetRequireIp() || !wait.GetRequireValidated() || wait.GetTimeoutMs() != 12000 {
					t.Fatalf("wifi wait = %#v", wait)
				}
			},
		},
		{
			name:      "wifi assert",
			line:      "wifi assert ssid Lab ip timeout 5000",
			parse:     ParseRequestLine,
			operation: "wifi.assert",
			check: func(t *testing.T, cmd *controlpb.RunCommand) {
				t.Helper()
				assert := cmd.GetAssertWifi()
				if assert == nil || assert.GetSsid() != "Lab" || !assert.GetRequireIp() || assert.GetTimeoutMs() != 5000 {
					t.Fatalf("wifi assert = %#v", assert)
				}
			},
		},
		{
			name:      "wifi reconnect",
			line:      "wifi reconnect timeout 10000",
			parse:     ParseRequestLine,
			operation: "wifi.reconnect",
			check: func(t *testing.T, cmd *controlpb.RunCommand) {
				t.Helper()
				if got := cmd.GetReconnectWifi().GetTimeoutMs(); got != 10000 {
					t.Fatalf("reconnect timeout = %d, want 10000", got)
				}
			},
		},
		{
			name:      "wifi cycle",
			line:      "wifi cycle Lab passphrase secret count 2 ping 1.1.1.1 http https://example.test forget pause 250",
			parse:     ParseRequestLine,
			operation: "wifi.cycle",
			check: func(t *testing.T, cmd *controlpb.RunCommand) {
				t.Helper()
				cycle := cmd.GetCycleWifi()
				if cycle == nil || cycle.GetConnect().GetSsid() != "Lab" || cycle.GetCount() != 2 || cycle.GetPingHost() != "1.1.1.1" || !cycle.GetForgetAfterEach() {
					t.Fatalf("wifi cycle = %#v", cycle)
				}
			},
		},
		{
			name:      "ping",
			line:      "ping count 5 size 64 timeout 7000 1.1.1.1",
			parse:     ParseRequestLine,
			operation: "ping",
			check: func(t *testing.T, cmd *controlpb.RunCommand) {
				t.Helper()
				ping := cmd.GetPing()
				if ping == nil || ping.GetHost() != "1.1.1.1" || ping.GetCount() != 5 || ping.GetSizeBytes() != 64 {
					t.Fatalf("ping = %#v", ping)
				}
			},
		},
		{
			name:      "traceroute",
			line:      "traceroute 1.1.1.1 max-hops 12 via 192.0.2.1 size 80 timeout 30000",
			parse:     ParseRequestLine,
			operation: "traceroute",
			check: func(t *testing.T, cmd *controlpb.RunCommand) {
				t.Helper()
				trace := cmd.GetTraceroute()
				if trace == nil || trace.GetHost() != "1.1.1.1" || trace.GetMaxHops() != 12 || trace.GetSizeBytes() != 80 {
					t.Fatalf("traceroute = %#v", trace)
				}
			},
		},
		{
			name:      "path mtu",
			line:      "path-mtu min-mtu 1200 max-mtu 1500 timeout 30000 1.1.1.1",
			parse:     ParseRequestLine,
			operation: "path-mtu",
			check: func(t *testing.T, cmd *controlpb.RunCommand) {
				t.Helper()
				pmtu := cmd.GetPathMtu()
				if pmtu == nil || pmtu.GetHost() != "1.1.1.1" || pmtu.GetMinMtuBytes() != 1200 || pmtu.GetMaxMtuBytes() != 1500 {
					t.Fatalf("path mtu = %#v", pmtu)
				}
			},
		},
		{
			name:      "global ip",
			line:      "global-ip family ipv4 timeout 7000",
			parse:     ParseRequestLine,
			operation: "global-ip",
			check: func(t *testing.T, cmd *controlpb.RunCommand) {
				t.Helper()
				global := cmd.GetGlobalIp()
				if global == nil || global.GetFamily() != controlpb.IpFamily_IP_FAMILY_IPV4 || global.GetTimeoutMs() != 7000 {
					t.Fatalf("global ip = %#v", global)
				}
			},
		},
		{
			name:      "dns",
			line:      "dns example.com A timeout 7000",
			parse:     ParseRequestLine,
			operation: "dns",
			check: func(t *testing.T, cmd *controlpb.RunCommand) {
				t.Helper()
				dns := cmd.GetResolveDns()
				if dns == nil || dns.GetName() != "example.com" || len(dns.GetQtypes()) != 1 || dns.GetQtypes()[0] != controlpb.DnsRecordType_DNS_RECORD_TYPE_A {
					t.Fatalf("dns = %#v", dns)
				}
			},
		},
		{
			name:      "download",
			line:      "download timeout 30000 https://example.com/",
			parse:     ParseRequestLine,
			operation: "download",
			check: func(t *testing.T, cmd *controlpb.RunCommand) {
				t.Helper()
				wget := cmd.GetWget()
				if wget == nil || wget.GetUrl() != "https://example.com/" || wget.GetTimeoutMs() != 30000 {
					t.Fatalf("download = %#v", wget)
				}
			},
		},
		{
			name:      "http",
			line:      "http expected-status 204 timeout 7000 https://www.google.com/generate_204",
			parse:     ParseRequestLine,
			operation: "http",
			check: func(t *testing.T, cmd *controlpb.RunCommand) {
				t.Helper()
				http := cmd.GetHttpCheck()
				if http == nil || http.GetExpectedStatus() != 204 || http.GetTimeoutMs() != 7000 {
					t.Fatalf("http = %#v", http)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parse := tt.parse
			if parse == nil {
				parse = ParseLine
			}
			got, err := parse(tt.line)
			if err != nil {
				t.Fatalf("parse error = %v", err)
			}
			cmd := agentCommand(t, got, tt.operation)
			tt.check(t, cmd)
		})
	}
}

func TestParseLineRejectsDeadAndInvalidCommandForms(t *testing.T) {
	tests := []struct {
		name  string
		line  string
		parse func(string) (Command, error)
		want  string
	}{
		{
			name:  "removed wifi watch command",
			line:  "wifi watch duration 1000",
			parse: ParseRequestLine,
			want:  "unknown wifi command",
		},
		{
			name:  "duplicate connect option",
			line:  "wifi connect Lab passphrase secret passphrase other",
			parse: ParseRequestLine,
			want:  "passphrase specified twice",
		},
		{
			name: "ambiguous show wifi prefix",
			line: "show wifi s",
			want: "ambiguous show wifi command",
		},
		{
			name: "removed show wifi mlo command",
			line: "show wifi mlo",
			want: "unknown show wifi command",
		},
		{
			name: "removed show wifi eht brief command",
			line: "show wifi eht brief",
			want: "usage: show wifi eht [fresh [timeout <ms>]] [ssid <ssid>|bssid <bssid>]",
		},
		{
			name: "show wifi scan mlo requires brief",
			line: "show wifi scan mlo",
			want: "mlo is supported only with wifi scan brief",
		},
		{
			name:  "bad monitor duration",
			line:  "monitor wifi duration 0",
			parse: ParseRequestLine,
			want:  "duration_ms must be a positive integer",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parse := tt.parse
			if parse == nil {
				parse = ParseLine
			}
			_, err := parse(tt.line)
			if err == nil {
				t.Fatalf("parse succeeded, want error containing %q", tt.want)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %q, want substring %q", err.Error(), tt.want)
			}
		})
	}
}

func agentCommand(t *testing.T, got Command, operation string) *controlpb.RunCommand {
	t.Helper()
	if got.Kind != AgentCommand {
		t.Fatalf("kind = %v, want %v", got.Kind, AgentCommand)
	}
	if got.Operation.Name != operation {
		t.Fatalf("operation = %q, want %q", got.Operation.Name, operation)
	}
	if got.Operation.Command == nil {
		t.Fatalf("operation %q has nil command", operation)
	}
	return got.Operation.Command
}
