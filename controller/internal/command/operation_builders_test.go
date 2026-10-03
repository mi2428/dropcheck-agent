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
			name:      "wifi eht",
			op:        WifiEHTOperation(),
			wantName:  "wifi.eht",
			wantLabel: "wifi eht",
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, options, err := BuildRunCommand(tt.op)
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
			if tt.wantName == "wifi.eht" && options.WifiRenderMode != WifiRenderModeEHT {
				t.Fatalf("wifi eht render mode = %q, want %q", options.WifiRenderMode, WifiRenderModeEHT)
			}
		})
	}
}

func TestWifiOperationBuildersCarryControllerContract(t *testing.T) {
	mloFresh, err := WifiEHTOperationWithOptions(WifiEHTOptions{Fresh: true, Timeout: "9000"})
	if err != nil {
		t.Fatalf("WifiEHTOperationWithOptions() error = %v", err)
	}
	mloFreshCmd, mloFreshOptions, err := BuildRunCommand(mloFresh)
	if err != nil {
		t.Fatalf("eht fresh command: %v", err)
	}
	if mloFreshCmd.GetGetWifiDiagnostics() == nil ||
		mloFreshCmd.GetLabel() != "wifi eht fresh --timeout 9000" ||
		mloFreshOptions.WifiRenderMode != WifiRenderModeEHT ||
		!mloFreshOptions.WifiEHTFreshScan ||
		mloFreshOptions.WifiEHTFreshScanTimeoutMs != 9000 {
		t.Fatalf("eht fresh command=%#v options=%#v", mloFreshCmd, mloFreshOptions)
	}
	if _, err := WifiEHTOperationWithOptions(WifiEHTOptions{Fresh: true, Timeout: "0"}); err == nil || !strings.Contains(err.Error(), "positive integer") {
		t.Fatalf("WifiEHTOperationWithOptions(timeout=0) error = %v", err)
	}
	mloFilter, err := WifiEHTOperationWithOptions(WifiEHTOptions{Fresh: true, Timeout: "9000", SSID: "temp-life26"})
	if err != nil {
		t.Fatalf("WifiEHTOperationWithOptions(ssid) error = %v", err)
	}
	mloFilterCmd, mloFilterOptions, err := BuildRunCommand(mloFilter)
	if err != nil {
		t.Fatalf("eht filtered command: %v", err)
	}
	if mloFilterCmd.GetLabel() != "wifi eht fresh --timeout 9000 ssid temp-life26" ||
		mloFilterOptions.WifiEHTSSID != "temp-life26" ||
		!mloFilterOptions.WifiEHTFreshScan {
		t.Fatalf("eht filtered command=%#v options=%#v", mloFilterCmd, mloFilterOptions)
	}
	if _, err := WifiEHTOperationWithOptions(WifiEHTOptions{SSID: "Lab", BSSID: "aa:bb:cc:dd:ee:ff"}); err == nil {
		t.Fatalf("WifiEHTOperationWithOptions(ssid+bssid) error = nil")
	}
	if _, err := WifiScanOperationWithBrief("6ghz", false, true); err == nil || !strings.Contains(err.Error(), "wifi scan brief") {
		t.Fatalf("WifiScanOperationWithBrief(mlo without brief) error = %v", err)
	}
	if _, err := WifiFreshScanOperationWithBrief("6ghz", "9000", false, true); err == nil || !strings.Contains(err.Error(), "wifi scan brief") {
		t.Fatalf("WifiFreshScanOperationWithBrief(mlo without brief) error = %v", err)
	}

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
	scanBrief, err := WifiScanOperationWithBrief("6ghz", true, false)
	if err != nil {
		t.Fatalf("WifiScanOperationWithBrief() error = %v", err)
	}
	scanBriefCmd, scanBriefOptions, err := BuildRunCommand(scanBrief)
	if err != nil {
		t.Fatalf("scan brief command: %v", err)
	}
	if scanBriefCmd.GetLabel() != "wifi scan brief 6ghz" ||
		scanBriefCmd.GetGetWifiScan().GetBand() != controlpb.WifiBand_WIFI_BAND_6_GHZ ||
		!scanBriefOptions.WifiScanBrief ||
		scanBriefOptions.WifiScanMLO {
		t.Fatalf("scan brief command=%#v options=%#v", scanBriefCmd, scanBriefOptions)
	}
	scanMLOBrief, err := WifiScanOperationWithBrief("6ghz", true, true)
	if err != nil {
		t.Fatalf("WifiScanOperationWithBrief(mlo) error = %v", err)
	}
	scanMLOBriefCmd, scanMLOBriefOptions, err := BuildRunCommand(scanMLOBrief)
	if err != nil {
		t.Fatalf("scan mlo brief command: %v", err)
	}
	if scanMLOBriefCmd.GetLabel() != "wifi scan brief mlo 6ghz" ||
		scanMLOBriefCmd.GetGetWifiScan().GetBand() != controlpb.WifiBand_WIFI_BAND_6_GHZ ||
		!scanMLOBriefOptions.WifiScanBrief ||
		!scanMLOBriefOptions.WifiScanMLO {
		t.Fatalf("scan mlo brief command=%#v options=%#v", scanMLOBriefCmd, scanMLOBriefOptions)
	}
	freshScanBrief, err := WifiFreshScanOperationWithBrief("6ghz", "9000", true, false)
	if err != nil {
		t.Fatalf("WifiFreshScanOperationWithBrief() error = %v", err)
	}
	freshScanBriefCmd, freshScanBriefOptions, err := BuildRunCommand(freshScanBrief)
	if err != nil {
		t.Fatalf("fresh scan brief command: %v", err)
	}
	if freshScanBriefCmd.GetLabel() != "wifi scan fresh brief 6ghz --timeout 9000" ||
		freshScanBriefCmd.GetGetFreshWifiScan().GetBand() != controlpb.WifiBand_WIFI_BAND_6_GHZ ||
		freshScanBriefCmd.GetGetFreshWifiScan().GetTimeoutMs() != 9000 ||
		!freshScanBriefOptions.WifiScanBrief ||
		freshScanBriefOptions.WifiScanMLO {
		t.Fatalf("fresh scan brief command=%#v options=%#v", freshScanBriefCmd, freshScanBriefOptions)
	}
	freshScanMLOBrief, err := WifiFreshScanOperationWithBrief("6ghz", "9000", true, true)
	if err != nil {
		t.Fatalf("WifiFreshScanOperationWithBrief(mlo) error = %v", err)
	}
	freshScanMLOBriefCmd, freshScanMLOBriefOptions, err := BuildRunCommand(freshScanMLOBrief)
	if err != nil {
		t.Fatalf("fresh scan mlo brief command: %v", err)
	}
	if freshScanMLOBriefCmd.GetLabel() != "wifi scan fresh brief mlo 6ghz --timeout 9000" ||
		freshScanMLOBriefCmd.GetGetFreshWifiScan().GetBand() != controlpb.WifiBand_WIFI_BAND_6_GHZ ||
		freshScanMLOBriefCmd.GetGetFreshWifiScan().GetTimeoutMs() != 9000 ||
		!freshScanMLOBriefOptions.WifiScanBrief ||
		!freshScanMLOBriefOptions.WifiScanMLO {
		t.Fatalf("fresh scan mlo brief command=%#v options=%#v", freshScanMLOBriefCmd, freshScanMLOBriefOptions)
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
	ping, err := PingOperation(PingOptions{Host: "one.one.one.one", Count: "2", Size: "64", Family: "ipv6"})
	if err != nil {
		t.Fatalf("PingOperation() error = %v", err)
	}
	pingCmd, _, err := BuildRunCommand(ping)
	if err != nil {
		t.Fatalf("ping command: %v", err)
	}
	if pingCmd.GetPing().GetTimeoutMs() != 7000 ||
		pingCmd.GetPing().GetSizeBytes() != 64 ||
		pingCmd.GetPing().GetFamily() != controlpb.IpFamily_IP_FAMILY_IPV6 {
		t.Fatalf("ping = %#v", pingCmd.GetPing())
	}

	trace, err := TracerouteOperation(TracerouteOptions{Host: "one.one.one.one", MaxHops: "4", Family: "ipv4"})
	if err != nil {
		t.Fatalf("TracerouteOperation() error = %v", err)
	}
	traceCmd, _, err := BuildRunCommand(trace)
	if err != nil {
		t.Fatalf("traceroute command: %v", err)
	}
	if traceCmd.GetTraceroute().GetMaxHops() != 4 ||
		traceCmd.GetTraceroute().GetFamily() != controlpb.IpFamily_IP_FAMILY_IPV4 {
		t.Fatalf("traceroute = %#v", traceCmd.GetTraceroute())
	}

	pathMTU, err := PathMTUOperation(PathMTUOptions{Host: "one.one.one.one", MinMTU: "1280", MaxMTU: "1500", Family: "ipv6"})
	if err != nil {
		t.Fatalf("PathMTUOperation() error = %v", err)
	}
	pathCmd, _, err := BuildRunCommand(pathMTU)
	if err != nil {
		t.Fatalf("path-mtu command: %v", err)
	}
	if pathCmd.GetPathMtu().GetMinMtuBytes() != 1280 ||
		pathCmd.GetPathMtu().GetMaxMtuBytes() != 1500 ||
		pathCmd.GetPathMtu().GetFamily() != controlpb.IpFamily_IP_FAMILY_IPV6 {
		t.Fatalf("path-mtu = %#v", pathCmd.GetPathMtu())
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
