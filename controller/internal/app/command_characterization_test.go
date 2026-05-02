package app

import (
	"slices"
	"testing"

	"dropcheck/controller/internal/controlpb"
	"dropcheck/controller/internal/linuxcli"
)

func TestLinuxWifiOperationalCommands(t *testing.T) {
	tests := []struct {
		name   string
		args   []string
		assert func(*testing.T, *controlpb.RunCommand)
	}{
		{
			name: "wifi wait",
			args: []string{"wifi", "wait", "connected", "Lab", "--security", "transition", "--band", "5ghz", "--ip", "--validated", "--timeout", "9000"},
			assert: func(t *testing.T, cmd *controlpb.RunCommand) {
				wait := cmd.GetWaitWifiConnected()
				if wait == nil || wait.GetSsid() != "Lab" || wait.GetSecurity() != controlpb.ConnectWifi_SECURITY_WPA2_WPA3_TRANSITION || wait.GetBand() != controlpb.WifiBand_WIFI_BAND_5_GHZ || !wait.GetRequireIp() || !wait.GetRequireValidated() || wait.GetTimeoutMs() != 9000 {
					t.Fatalf("WaitWifiConnected = %#v", wait)
				}
			},
		},
		{
			name: "wifi assert",
			args: []string{"wifi", "assert", "--ssid", "Lab", "--bssid", "aa:bb:cc:dd:ee:ff", "--security", "wpa3", "--band", "6ghz", "--ip", "--validated", "--timeout", "8000"},
			assert: func(t *testing.T, cmd *controlpb.RunCommand) {
				assertWifi := cmd.GetAssertWifi()
				if assertWifi == nil || assertWifi.GetSsid() != "Lab" || assertWifi.GetBssid() != "aa:bb:cc:dd:ee:ff" || assertWifi.GetSecurity() != controlpb.ConnectWifi_SECURITY_WPA3_SAE || assertWifi.GetBand() != controlpb.WifiBand_WIFI_BAND_6_GHZ || !assertWifi.GetRequireIp() || !assertWifi.GetRequireValidated() || assertWifi.GetTimeoutMs() != 8000 {
					t.Fatalf("AssertWifi = %#v", assertWifi)
				}
			},
		},
		{
			name: "wifi watch interval option gets default duration",
			args: []string{"wifi", "watch", "--interval", "250"},
			assert: func(t *testing.T, cmd *controlpb.RunCommand) {
				if got := cmd.GetWatchWifi(); got == nil || got.GetDurationMs() != 10000 || got.GetIntervalMs() != 250 {
					t.Fatalf("WatchWifi = %#v", got)
				}
			},
		},
		{
			name: "wifi monitor duration interval options",
			args: []string{"wifi", "monitor", "--duration", "5000", "--interval", "250"},
			assert: func(t *testing.T, cmd *controlpb.RunCommand) {
				if got := cmd.GetMonitorWifi(); got == nil || got.GetDurationMs() != 5000 || got.GetIntervalMs() != 250 {
					t.Fatalf("MonitorWifi = %#v", got)
				}
			},
		},
		{
			name: "wifi reconnect timeout option",
			args: []string{"wifi", "reconnect", "--timeout", "9000"},
			assert: func(t *testing.T, cmd *controlpb.RunCommand) {
				if got := cmd.GetReconnectWifi(); got == nil || got.GetTimeoutMs() != 9000 {
					t.Fatalf("ReconnectWifi = %#v", got)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := linuxCommand(t, tt.args...)
			tt.assert(t, cmd)
		})
	}
}

func TestLinuxNetworkDefaultsAndNormalization(t *testing.T) {
	download := linuxCommand(t, "download", "https://example.test/file.bin")
	if got := download.GetWget(); got == nil || got.GetUrl() != "https://example.test/file.bin" || got.GetTimeoutMs() != 60000 || got.GetSelector() == nil {
		t.Fatalf("Wget = %#v", got)
	}

	http := linuxCommand(t, "http", "example.test/health")
	if got := http.GetHttpCheck(); got == nil || got.GetUrl() != "https://example.test/health" || got.GetExpectedStatus() != 200 || got.GetTimeoutMs() != 5000 || got.GetSelector() == nil {
		t.Fatalf("HttpCheck = %#v", got)
	}

	dns := linuxCommand(t, "dns", "example.test")
	if got := dns.GetResolveDns(); got == nil || got.GetName() != "example.test" || !slices.Equal(got.GetQtypes(), []controlpb.DnsRecordType{controlpb.DnsRecordType_DNS_RECORD_TYPE_A, controlpb.DnsRecordType_DNS_RECORD_TYPE_AAAA}) || got.GetTimeoutMs() != 5000 || got.GetSelector() == nil {
		t.Fatalf("ResolveDns = %#v", got)
	}
}

func linuxCommand(t *testing.T, args ...string) *controlpb.RunCommand {
	t.Helper()
	parsed, err := linuxcli.Parse(args)
	if err != nil {
		t.Fatalf("linuxcli.Parse(%#v) error = %v", args, err)
	}
	if parsed.Kind != linuxcli.AgentCommand {
		t.Fatalf("kind = %v, want AgentCommand", parsed.Kind)
	}
	cmd, _ := operationCommand(t, parsed.Operation)
	return cmd
}
