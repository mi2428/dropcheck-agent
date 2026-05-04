package command

import (
	"slices"
	"strings"
	"testing"

	"dropcheck/controller/internal/controlpb"
)

func TestInspectionOperationBuilders(t *testing.T) {
	tests := []struct {
		name      string
		op        Operation
		wantName  string
		wantLabel string
		check     func(*controlpb.RunCommand) bool
	}{
		{
			name:      "wifi diagnostics",
			op:        WifiDiagnosticsOperation(),
			wantName:  "wifi.diagnostics",
			wantLabel: "wifi diagnostics",
			check:     func(cmd *controlpb.RunCommand) bool { return cmd.GetGetWifiDiagnostics() != nil },
		},
		{
			name:      "wifi capabilities",
			op:        WifiCapabilitiesOperation(),
			wantName:  "wifi.capabilities",
			wantLabel: "wifi capabilities",
			check:     func(cmd *controlpb.RunCommand) bool { return cmd.GetGetWifiCapabilities() != nil },
		},
		{
			name:      "wifi disconnect",
			op:        WifiDisconnectOperation(),
			wantName:  "wifi.disconnect",
			wantLabel: "wifi disconnect",
			check:     func(cmd *controlpb.RunCommand) bool { return cmd.GetDisconnectWifi() != nil },
		},
		{
			name:      "ip status",
			op:        IPStatusOperation(),
			wantName:  "ip.status",
			wantLabel: "ip",
			check:     func(cmd *controlpb.RunCommand) bool { return cmd.GetGetIpStatus() != nil },
		},
		{
			name:      "standalone config",
			op:        StandaloneConfigOperation(),
			wantName:  "standalone.config",
			wantLabel: "standalone config",
			check:     func(cmd *controlpb.RunCommand) bool { return cmd.GetGetStandaloneConfig() != nil },
		},
		{
			name:      "standalone status",
			op:        StandaloneStatusOperation(),
			wantName:  "standalone.status",
			wantLabel: "standalone status",
			check:     func(cmd *controlpb.RunCommand) bool { return cmd.GetGetStandaloneStatus() != nil },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, _, err := BuildRunCommand(tt.op)
			if err != nil {
				t.Fatalf("BuildRunCommand() error = %v", err)
			}
			if tt.op.Name != tt.wantName {
				t.Fatalf("operation name = %q, want %q", tt.op.Name, tt.wantName)
			}
			if cmd.GetLabel() != tt.wantLabel {
				t.Fatalf("label = %q, want %q", cmd.GetLabel(), tt.wantLabel)
			}
			if !tt.check(cmd) {
				t.Fatalf("unexpected command payload = %#v", cmd.GetCommand())
			}
		})
	}
}

func TestWifiOperationBuildersCarryControllerContract(t *testing.T) {
	forget := WifiForgetOperation("Lab")
	forgetCmd, _, err := BuildRunCommand(forget)
	if err != nil {
		t.Fatalf("forget command: %v", err)
	}
	if forgetCmd.GetForgetWifi().GetTarget() != "Lab" || forgetCmd.GetLabel() != "wifi forget Lab" {
		t.Fatalf("forget command = %#v label=%q", forgetCmd.GetForgetWifi(), forgetCmd.GetLabel())
	}

	scan, err := WifiScanOperation("5ghz")
	if err != nil {
		t.Fatalf("WifiScanOperation() error = %v", err)
	}
	scanCmd, _, err := BuildRunCommand(scan)
	if err != nil {
		t.Fatalf("scan command: %v", err)
	}
	if scanCmd.GetGetWifiScan().GetBand() != controlpb.WifiBand_WIFI_BAND_5_GHZ {
		t.Fatalf("scan band = %v", scanCmd.GetGetWifiScan().GetBand())
	}

	detail, err := WifiScanDetailOperation("Lab", "all")
	if err != nil {
		t.Fatalf("WifiScanDetailOperation() error = %v", err)
	}
	detailCmd, _, err := BuildRunCommand(detail)
	if err != nil {
		t.Fatalf("detail command: %v", err)
	}
	if detailCmd.GetGetWifiScanDetail().GetTarget() != "Lab" ||
		detailCmd.GetGetWifiScanDetail().GetBand() != controlpb.WifiBand_WIFI_BAND_ALL {
		t.Fatalf("scan detail = %#v", detailCmd.GetGetWifiScanDetail())
	}

	connect, err := WifiConnectOperation(WifiConnectOptions{
		SSID:             "Lab",
		Passphrase:       "secret",
		Security:         "wpa3",
		BSSID:            "aa:bb:cc:dd:ee:ff",
		Band:             "6ghz",
		MacRandomization: "non-persistent",
		Timeout:          "1234",
	})
	if err != nil {
		t.Fatalf("WifiConnectOperation() error = %v", err)
	}
	connectCmd, _, err := BuildRunCommand(connect)
	if err != nil {
		t.Fatalf("connect command: %v", err)
	}
	connectPayload := connectCmd.GetConnectWifi()
	if connectPayload.GetPassphrase() != "secret" ||
		connectPayload.GetSecurity() != controlpb.ConnectWifi_SECURITY_WPA3_SAE ||
		connectPayload.GetBand() != controlpb.WifiBand_WIFI_BAND_6_GHZ ||
		connectPayload.GetMacRandomization() != controlpb.ConnectWifi_MAC_RANDOMIZATION_NON_PERSISTENT ||
		connectPayload.GetTimeoutMs() != 1234 {
		t.Fatalf("connect payload = %#v", connectPayload)
	}
	if strings.Contains(connectCmd.GetLabel(), "secret") || !strings.Contains(connectCmd.GetLabel(), "<redacted>") {
		t.Fatalf("connect label did not redact passphrase: %q", connectCmd.GetLabel())
	}

	cycle, err := WifiCycleOperation(WifiCycleOptions{
		WifiConnectOptions: WifiConnectOptions{
			SSID:       "Lab",
			Passphrase: "secret",
			Timeout:    "25000",
		},
		Count:           "2",
		PingHost:        "1.1.1.1",
		HTTPURL:         "https://example.test/204",
		ForgetAfterEach: true,
		Pause:           "750",
	})
	if err != nil {
		t.Fatalf("WifiCycleOperation() error = %v", err)
	}
	cycleCmd, _, err := BuildRunCommand(cycle)
	if err != nil {
		t.Fatalf("cycle command: %v", err)
	}
	cyclePayload := cycleCmd.GetCycleWifi()
	if cyclePayload.GetCount() != 2 ||
		cyclePayload.GetPingHost() != "1.1.1.1" ||
		cyclePayload.GetHttpUrl() != "https://example.test/204" ||
		!cyclePayload.GetForgetAfterEach() ||
		cyclePayload.GetPauseMs() != 750 ||
		cyclePayload.GetConnect().GetTimeoutMs() != 25000 {
		t.Fatalf("cycle payload = %#v", cyclePayload)
	}
}

func TestNetworkProbeOperationBuildersCarryDefaultsAndNormalization(t *testing.T) {
	ping, err := PingOperation(PingOptions{Host: "1.1.1.1", Count: "2", Size: "64"})
	if err != nil {
		t.Fatalf("PingOperation() error = %v", err)
	}
	pingCmd, _, err := BuildRunCommand(ping)
	if err != nil {
		t.Fatalf("ping command: %v", err)
	}
	if pingCmd.GetPing().GetTimeoutMs() != 7000 || pingCmd.GetPing().GetSizeBytes() != 64 {
		t.Fatalf("ping = %#v", pingCmd.GetPing())
	}

	globalIP, err := GlobalIPOperation("ipv4", "9000")
	if err != nil {
		t.Fatalf("GlobalIPOperation() error = %v", err)
	}
	globalCmd, _, err := BuildRunCommand(globalIP)
	if err != nil {
		t.Fatalf("global-ip command: %v", err)
	}
	if globalCmd.GetGlobalIp().GetFamily() != controlpb.IpFamily_IP_FAMILY_IPV4 ||
		globalCmd.GetGlobalIp().GetTimeoutMs() != 9000 {
		t.Fatalf("global-ip = %#v", globalCmd.GetGlobalIp())
	}

	dns, err := DNSOperation("example.test", "AAAA", "6000")
	if err != nil {
		t.Fatalf("DNSOperation() error = %v", err)
	}
	dnsCmd, _, err := BuildRunCommand(dns)
	if err != nil {
		t.Fatalf("dns command: %v", err)
	}
	if !slices.Equal(dnsCmd.GetResolveDns().GetQtypes(), []controlpb.DnsRecordType{controlpb.DnsRecordType_DNS_RECORD_TYPE_AAAA}) ||
		dnsCmd.GetResolveDns().GetTimeoutMs() != 6000 {
		t.Fatalf("dns = %#v", dnsCmd.GetResolveDns())
	}

	http, err := HTTPOperation("example.test/health", "204", "8000")
	if err != nil {
		t.Fatalf("HTTPOperation() error = %v", err)
	}
	httpCmd, _, err := BuildRunCommand(http)
	if err != nil {
		t.Fatalf("http command: %v", err)
	}
	if httpCmd.GetHttpCheck().GetUrl() != "https://example.test/health" ||
		httpCmd.GetHttpCheck().GetExpectedStatus() != 204 ||
		httpCmd.GetHttpCheck().GetTimeoutMs() != 8000 {
		t.Fatalf("http = %#v", httpCmd.GetHttpCheck())
	}

	download, err := DownloadOperation("https://example.test/file", "")
	if err != nil {
		t.Fatalf("DownloadOperation() error = %v", err)
	}
	downloadCmd, _, err := BuildRunCommand(download)
	if err != nil {
		t.Fatalf("download command: %v", err)
	}
	if downloadCmd.GetWget().GetTimeoutMs() != 60000 {
		t.Fatalf("download = %#v", downloadCmd.GetWget())
	}
}

func TestWifiExpectationAndSamplingBuilders(t *testing.T) {
	wait, err := WifiWaitConnectedOperation("positional", WifiExpectationOptions{
		SSID:             "ignored",
		BSSID:            "aa:bb:cc:dd:ee:ff",
		Security:         "transition",
		Band:             "5ghz",
		RequireIP:        true,
		RequireValidated: true,
		Timeout:          "12000",
	})
	if err != nil {
		t.Fatalf("WifiWaitConnectedOperation() error = %v", err)
	}
	waitCmd, _, err := BuildRunCommand(wait)
	if err != nil {
		t.Fatalf("wait command: %v", err)
	}
	waitPayload := waitCmd.GetWaitWifiConnected()
	if waitPayload.GetSsid() != "positional" ||
		waitPayload.GetSecurity() != controlpb.ConnectWifi_SECURITY_WPA2_WPA3_TRANSITION ||
		waitPayload.GetBand() != controlpb.WifiBand_WIFI_BAND_5_GHZ ||
		!waitPayload.GetRequireIp() ||
		!waitPayload.GetRequireValidated() ||
		waitPayload.GetTimeoutMs() != 12000 {
		t.Fatalf("wait payload = %#v", waitPayload)
	}

	assert, err := WifiAssertOperation(WifiExpectationOptions{SSID: "Lab", Security: "wpa2", Timeout: "5000"})
	if err != nil {
		t.Fatalf("WifiAssertOperation() error = %v", err)
	}
	assertCmd, _, err := BuildRunCommand(assert)
	if err != nil {
		t.Fatalf("assert command: %v", err)
	}
	if assertCmd.GetAssertWifi().GetSecurity() != controlpb.ConnectWifi_SECURITY_WPA2_PSK ||
		assertCmd.GetAssertWifi().GetTimeoutMs() != 5000 {
		t.Fatalf("assert = %#v", assertCmd.GetAssertWifi())
	}

	monitor, err := WifiMonitorOperation("", "250")
	if err != nil {
		t.Fatalf("WifiMonitorOperation() error = %v", err)
	}
	monitorCmd, _, err := BuildRunCommand(monitor)
	if err != nil {
		t.Fatalf("monitor command: %v", err)
	}
	if monitorCmd.GetMonitorWifi().GetDurationMs() != 10000 || monitorCmd.GetMonitorWifi().GetIntervalMs() != 250 {
		t.Fatalf("monitor = %#v", monitorCmd.GetMonitorWifi())
	}

	reconnect, err := WifiReconnectOperation("")
	if err != nil {
		t.Fatalf("WifiReconnectOperation() error = %v", err)
	}
	reconnectCmd, _, err := BuildRunCommand(reconnect)
	if err != nil {
		t.Fatalf("reconnect command: %v", err)
	}
	if reconnectCmd.GetReconnectWifi().GetTimeoutMs() != 30000 {
		t.Fatalf("reconnect = %#v", reconnectCmd.GetReconnectWifi())
	}
}

func TestStandaloneOperationBuildersAndEdits(t *testing.T) {
	list, err := StandaloneListRunsOperation(StandaloneListOptions{Limit: "5", IncludeSynced: true})
	if err != nil {
		t.Fatalf("StandaloneListRunsOperation() error = %v", err)
	}
	listCmd, _, err := BuildRunCommand(list)
	if err != nil {
		t.Fatalf("list command: %v", err)
	}
	if listCmd.GetListStandaloneRuns().GetLimit() != 5 || !listCmd.GetListStandaloneRuns().GetIncludeSynced() {
		t.Fatalf("list runs = %#v", listCmd.GetListStandaloneRuns())
	}

	run, err := StandaloneRunOperation("run-123", true)
	if err != nil {
		t.Fatalf("StandaloneRunOperation() error = %v", err)
	}
	runCmd, _, err := BuildRunCommand(run)
	if err != nil {
		t.Fatalf("run command: %v", err)
	}
	if runCmd.GetGetStandaloneRun().GetRunId() != "run-123" || !runCmd.GetGetStandaloneRun().GetMarkSynced() {
		t.Fatalf("standalone run = %#v", runCmd.GetGetStandaloneRun())
	}
	if _, err := StandaloneRunOperation(" ", false); err == nil {
		t.Fatalf("StandaloneRunOperation(blank) error = nil")
	}

	clear, err := StandaloneClearRunsOperation("")
	if err != nil {
		t.Fatalf("StandaloneClearRunsOperation() error = %v", err)
	}
	clearCmd, _, err := BuildRunCommand(clear)
	if err != nil {
		t.Fatalf("clear command: %v", err)
	}
	if !clearCmd.GetClearStandaloneRuns().GetSyncedOnly() || clearCmd.GetClearStandaloneRuns().GetAll() {
		t.Fatalf("clear synced = %#v", clearCmd.GetClearStandaloneRuns())
	}
	if _, err := StandaloneClearRunsOperation("invalid"); err == nil {
		t.Fatalf("StandaloneClearRunsOperation(invalid) error = nil")
	}

	once, err := StandaloneRunOnceOperation(StandaloneRunOptions{Festa: "smoke", Save: true})
	if err != nil {
		t.Fatalf("StandaloneRunOnceOperation() error = %v", err)
	}
	onceCmd, _, err := BuildRunCommand(once)
	if err != nil {
		t.Fatalf("run once command: %v", err)
	}
	if onceCmd.GetRunStandaloneOnce().GetFesta() != "smoke" || !onceCmd.GetRunStandaloneOnce().GetSave() {
		t.Fatalf("run once = %#v", onceCmd.GetRunStandaloneOnce())
	}

	bytesEdit, err := StandaloneSetBytesEdit([]string{"max_bytes"}, "1g", 0)
	if err != nil {
		t.Fatalf("StandaloneSetBytesEdit() error = %v", err)
	}
	if bytesEdit.Value != "1073741824" {
		t.Fatalf("bytes edit = %#v", bytesEdit)
	}

	deleteEdit := StandaloneDeleteEdit([]string{"festa", "smoke"})
	if deleteEdit.Action != "delete" || strings.Join(deleteEdit.Path, "/") != "festa/smoke" {
		t.Fatalf("delete edit = %#v", deleteEdit)
	}
}

func TestStandaloneSetAndDeleteParsersCoverChecks(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantPath string
		want     string
	}{
		{
			name:     "dns check",
			args:     []string{"festa", "smoke", "check", "dns-main", "test", "dns", "name", "example.test", "type", "AAAA", "timeout", "2s"},
			wantPath: "festa/smoke/check/dns-main/qtypes",
			want:     "AAAA",
		},
		{
			name:     "ping check",
			args:     []string{"festa", "smoke", "check", "cloudflare", "test", "ping", "host", "1.1.1.1", "count", "2", "size", "56"},
			wantPath: "festa/smoke/check/cloudflare/size_bytes",
			want:     "56",
		},
		{
			name:     "http check",
			args:     []string{"festa", "smoke", "check", "healthz", "test", "http", "url", "https://example.test", "expected-status", "204"},
			wantPath: "festa/smoke/check/healthz/expected_status",
			want:     "204",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			edits, err := StandaloneSetEdits(tt.args)
			if err != nil {
				t.Fatalf("StandaloneSetEdits() error = %v", err)
			}
			for _, edit := range edits {
				if strings.Join(edit.Path, "/") == tt.wantPath && edit.Value == tt.want {
					return
				}
			}
			t.Fatalf("edits missing %s=%s: %#v", tt.wantPath, tt.want, edits)
		})
	}

	deletes, err := StandaloneDeleteEdits([]string{"festa", "smoke", "check", "dns-main"})
	if err != nil {
		t.Fatalf("StandaloneDeleteEdits() error = %v", err)
	}
	if len(deletes) != 1 || deletes[0].Action != "delete" || strings.Join(deletes[0].Path, "/") != "festa/smoke/check/dns-main" {
		t.Fatalf("delete edits = %#v", deletes)
	}

	if _, err := StandaloneSetEdits([]string{"festa", "smoke", "check", "dns-main", "test", "dns", "name", "a", "name", "b"}); err == nil {
		t.Fatalf("duplicate standalone check key error = nil")
	}
}
