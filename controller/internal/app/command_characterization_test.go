package app

import (
	"slices"
	"testing"

	"dropcheck/controller/internal/controlpb"
)

func TestBuildCommandWifiExpectationAndMonitorCommands(t *testing.T) {
	assert := mustBuild(t, "wifi", "assert", "--ssid", "Lab", "--bssid", "aa:bb:cc:dd:ee:ff", "--security", "wpa3", "--band", "6ghz", "--ip", "--validated", "--timeout", "8000")
	assertWifi := assert.GetAssertWifi()
	if assertWifi == nil {
		t.Fatalf("AssertWifi = nil")
	}
	if assertWifi.GetSsid() != "Lab" || assertWifi.GetBssid() != "aa:bb:cc:dd:ee:ff" || assertWifi.GetSecurity() != controlpb.ConnectWifi_SECURITY_WPA3_SAE || assertWifi.GetBand() != controlpb.WifiBand_WIFI_BAND_6_GHZ || !assertWifi.GetRequireIp() || !assertWifi.GetRequireValidated() || assertWifi.GetTimeoutMs() != 8000 {
		t.Fatalf("AssertWifi = %#v", assertWifi)
	}

	watch := mustBuild(t, "wifi", "watch")
	if got := watch.GetWatchWifi(); got == nil || got.GetDurationMs() != 10000 || got.GetIntervalMs() != 1000 {
		t.Fatalf("default WatchWifi = %#v", got)
	}

	monitor := mustBuild(t, "wifi", "monitor", "5000", "250")
	if got := monitor.GetMonitorWifi(); got == nil || got.GetDurationMs() != 5000 || got.GetIntervalMs() != 250 {
		t.Fatalf("MonitorWifi = %#v", got)
	}

	reconnect := mustBuild(t, "wifi", "reconnect")
	if got := reconnect.GetReconnectWifi(); got == nil || got.GetTimeoutMs() != 30000 {
		t.Fatalf("default ReconnectWifi = %#v", got)
	}

	reconnect = mustBuild(t, "wifi", "reconnect", "9000")
	if got := reconnect.GetReconnectWifi(); got == nil || got.GetTimeoutMs() != 9000 {
		t.Fatalf("ReconnectWifi = %#v", got)
	}
}

func TestParseLinuxWifiOperationalCommands(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "wifi wait",
			args: []string{"wifi", "wait", "connected", "Lab", "--security", "transition", "--band", "5ghz", "--ip", "--validated", "--timeout", "9000"},
			want: []string{"wifi", "wait", "connected", "Lab", "--security", "transition", "--band", "5ghz", "--timeout", "9000", "--ip", "--validated"},
		},
		{
			name: "wifi assert",
			args: []string{"wifi", "assert", "--ssid", "Lab", "--bssid", "aa:bb:cc:dd:ee:ff", "--security", "wpa3", "--band", "6ghz", "--ip"},
			want: []string{"wifi", "assert", "--ssid", "Lab", "--bssid", "aa:bb:cc:dd:ee:ff", "--security", "wpa3", "--band", "6ghz", "--ip"},
		},
		{
			name: "wifi watch interval option gets default duration",
			args: []string{"wifi", "watch", "--interval", "250"},
			want: []string{"wifi", "watch", "10000", "250"},
		},
		{
			name: "wifi monitor duration interval options",
			args: []string{"wifi", "monitor", "--duration", "5000", "--interval", "250"},
			want: []string{"wifi", "monitor", "5000", "250"},
		},
		{
			name: "wifi reconnect timeout option",
			args: []string{"wifi", "reconnect", "--timeout", "9000"},
			want: []string{"wifi", "reconnect", "9000"},
		},
		{
			name: "download timeout",
			args: []string{"download", "https://example.test/file.bin", "--timeout", "9000"},
			want: []string{"download", "https://example.test/file.bin", "--timeout", "9000"},
		},
		{
			name: "http expected status timeout",
			args: []string{"http", "example.test", "--expected-status", "204", "--timeout", "7000"},
			want: []string{"http", "example.test", "204", "--timeout", "7000"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseLinuxCommand(tt.args)
			if err != nil {
				t.Fatalf("parseLinuxCommand() error = %v", err)
			}
			if got.kind != cliAgentCommand {
				t.Fatalf("kind = %v, want cliAgentCommand", got.kind)
			}
			assertOperationMatchesArgs(t, got.operation, tt.want)
		})
	}
}

func TestBuildCommandNetworkDefaultsAndNormalization(t *testing.T) {
	download := mustBuild(t, "download", "https://example.test/file.bin")
	if got := download.GetWget(); got == nil || got.GetUrl() != "https://example.test/file.bin" || got.GetTimeoutMs() != 60000 || got.GetSelector() == nil {
		t.Fatalf("Wget = %#v", got)
	}

	http := mustBuild(t, "http", "example.test/health")
	if got := http.GetHttpCheck(); got == nil || got.GetUrl() != "https://example.test/health" || got.GetExpectedStatus() != 200 || got.GetTimeoutMs() != 5000 || got.GetSelector() == nil {
		t.Fatalf("HttpCheck = %#v", got)
	}

	dns := mustBuild(t, "dns", "example.test")
	if got := dns.GetResolveDns(); got == nil || got.GetName() != "example.test" || !slices.Equal(got.GetQtypes(), []controlpb.DnsRecordType{controlpb.DnsRecordType_DNS_RECORD_TYPE_A, controlpb.DnsRecordType_DNS_RECORD_TYPE_AAAA}) || got.GetTimeoutMs() != 5000 || got.GetSelector() == nil {
		t.Fatalf("ResolveDns = %#v", got)
	}
}
