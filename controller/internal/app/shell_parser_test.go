package app

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"dropcheck/controller/internal/controlpb"
	"dropcheck/controller/internal/linuxcli"
)

const requestModeTestPrefix = "request> "
const configureModeTestPrefix = "config> "

type helpEntry struct {
	token string
}

func parseShellLineForTest(line string) (shellCommand, error) {
	if requestLine, ok := strings.CutPrefix(line, requestModeTestPrefix); ok {
		return parseShellRequestLine(requestLine)
	}
	if configureLine, ok := strings.CutPrefix(line, configureModeTestPrefix); ok {
		return parseShellConfigureLine(configureLine)
	}
	return parseShellLine(line)
}

func shellHelpEntriesForTest(line string) []helpEntry {
	var b bytes.Buffer
	if requestLine, ok := strings.CutPrefix(line, requestModeTestPrefix); ok {
		writeShellContextHelp(&b, requestLine, &shellState{mode: shellModeRequest})
		return parseHelpEntriesForTest(b.String())
	}
	if configureLine, ok := strings.CutPrefix(line, configureModeTestPrefix); ok {
		writeShellContextHelp(&b, configureLine, &shellState{mode: shellModeConfigure})
		return parseHelpEntriesForTest(b.String())
	}
	writeShellContextHelp(&b, line)
	return parseHelpEntriesForTest(b.String())
}

func parseHelpEntriesForTest(output string) []helpEntry {
	var entries []helpEntry
	for line := range strings.SplitSeq(output, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		token := strings.TrimSpace(line)
		if strings.HasPrefix(line, "  ") {
			if len(line) >= 26 {
				token = strings.TrimSpace(line[2:26])
			} else {
				token = strings.TrimSpace(line[2:])
			}
		} else if fields := strings.Fields(line); len(fields) > 0 {
			token = fields[0]
		}
		entries = append(entries, helpEntry{token: token})
	}
	return entries
}

func completeShellLineForTest(line string, _ *shellState) []string {
	if requestLine, ok := strings.CutPrefix(line, requestModeTestPrefix); ok {
		return completeShellLine(requestLine, &shellState{mode: shellModeRequest})
	}
	if configureLine, ok := strings.CutPrefix(line, configureModeTestPrefix); ok {
		return completeShellLine(configureLine, &shellState{mode: shellModeConfigure})
	}
	return completeShellLine(line, nil)
}

func shellCompletionHintLineForTest(line string, _ *shellState) string {
	if requestLine, ok := strings.CutPrefix(line, requestModeTestPrefix); ok {
		return shellCompletionHintLine(requestLine, &shellState{mode: shellModeRequest})
	}
	if configureLine, ok := strings.CutPrefix(line, configureModeTestPrefix); ok {
		return shellCompletionHintLine(configureLine, &shellState{mode: shellModeConfigure})
	}
	return shellCompletionHintLine(line, nil)
}

func TestParseShellCommands(t *testing.T) {
	tests := []struct {
		name  string
		line  string
		kind  shellCommandKind
		label string
		hops  []string
	}{
		{
			name:  "show wifi status",
			line:  "show wifi status",
			kind:  shellAgentCommand,
			label: "wifi status",
		},
		{
			name:  "show wifi eht",
			line:  "show wifi eht",
			kind:  shellAgentCommand,
			label: "wifi eht",
		},
		{
			name:  "show wifi eht fresh",
			line:  "show wifi eht fresh",
			kind:  shellAgentCommand,
			label: "wifi eht fresh",
		},
		{
			name:  "show wifi eht fresh timeout",
			line:  "show wifi eht fresh timeout 9000",
			kind:  shellAgentCommand,
			label: "wifi eht fresh --timeout 9000",
		},
		{
			name:  "show wifi eht fresh shorthand",
			line:  "show wifi eht fresh 9000",
			kind:  shellAgentCommand,
			label: "wifi eht fresh --timeout 9000",
		},
		{
			name:  "show wifi eht ssid",
			line:  "show wifi eht ssid temp-life26",
			kind:  shellAgentCommand,
			label: "wifi eht ssid temp-life26",
		},
		{
			name:  "show wifi eht bssid",
			line:  "show wifi eht bssid aa:bb:cc:dd:ee:ff",
			kind:  shellAgentCommand,
			label: "wifi eht bssid aa:bb:cc:dd:ee:ff",
		},
		{
			name:  "show ip status",
			line:  "show ip status",
			kind:  shellAgentCommand,
			label: "ip",
		},
		{
			name:  "show wifi scan fresh",
			line:  "show wifi scan fresh 5ghz timeout 9000",
			kind:  shellAgentCommand,
			label: "wifi scan fresh 5ghz --timeout 9000",
		},
		{
			name: "show adb diagnostics full",
			line: "show adb diagnostics full",
			kind: shellADBDiagnostics,
		},
		{
			name:  "request> wifi connect",
			line:  "request> wifi connect Lab passphrase secret security wpa3 bssid aa:bb:cc:dd:ee:ff band 6ghz mac-randomization non-persistent timeout 12345",
			kind:  shellAgentCommand,
			label: "wifi connect Lab <redacted> wpa3 --bssid aa:bb:cc:dd:ee:ff --band 6ghz --mac-randomization non-persistent --timeout 12345",
		},
		{
			name:  "request> wifi connect options first",
			line:  "request> wifi connect passphrase secret security wpa3 bssid aa:bb:cc:dd:ee:ff band 6ghz mac-randomization non-persistent timeout 12345 Lab",
			kind:  shellAgentCommand,
			label: "wifi connect Lab <redacted> wpa3 --bssid aa:bb:cc:dd:ee:ff --band 6ghz --mac-randomization non-persistent --timeout 12345",
		},
		{
			name:  "request> wifi cycle",
			line:  "request> wifi cycle Lab passphrase secret count 2 ping 1.1.1.1 http https://example.test forget pause 250",
			kind:  shellAgentCommand,
			label: "wifi cycle Lab <redacted> --count 2 --ping 1.1.1.1 --http https://example.test --pause 250 --forget",
		},
		{
			name:  "request> monitor wifi",
			line:  "request> monitor wifi duration 5000 interval 250",
			kind:  shellAgentCommand,
			label: "wifi monitor 5000 250",
		},
		{
			name:  "request> ping",
			line:  "request> ping 1.1.1.1 count 5 size 64 timeout 7000",
			kind:  shellAgentCommand,
			label: "ping 1.1.1.1 5 --size 64 --timeout 7000",
		},
		{
			name:  "request ping from top level",
			line:  "request ping 1.1.1.1 count 5 size 64 timeout 7000",
			kind:  shellAgentCommand,
			label: "ping 1.1.1.1 5 --size 64 --timeout 7000",
		},
		{
			name:  "request> ping options first",
			line:  "request> ping count 5 size 64 timeout 7000 1.1.1.1",
			kind:  shellAgentCommand,
			label: "ping 1.1.1.1 5 --size 64 --timeout 7000",
		},
		{
			name:  "request> traceroute",
			line:  "request> traceroute example.test max-hops 12 via 192.0.2.1 size 80 timeout 30000",
			kind:  shellAgentCommand,
			label: "traceroute example.test 12 --via 192.0.2.1 --size 80 --timeout 30000",
			hops:  []string{"192.0.2.1"},
		},
		{
			name:  "request> traceroute options first",
			line:  "request> traceroute max-hops 12 via 192.0.2.1 size 80 timeout 30000 example.test",
			kind:  shellAgentCommand,
			label: "traceroute example.test 12 --via 192.0.2.1 --size 80 --timeout 30000",
			hops:  []string{"192.0.2.1"},
		},
		{
			name:  "path mtu",
			line:  "request> path-mtu example.test min-mtu 1200 max-mtu 1500 timeout 30000",
			kind:  shellAgentCommand,
			label: "path-mtu example.test --min-mtu 1200 --max-mtu 1500 --timeout 30000",
		},
		{
			name:  "path mtu options first",
			line:  "request> path-mtu min-mtu 1200 max-mtu 1500 timeout 30000 example.test",
			kind:  shellAgentCommand,
			label: "path-mtu example.test --min-mtu 1200 --max-mtu 1500 --timeout 30000",
		},
		{
			name:  "global ip",
			line:  "request> global-ip ipv6 timeout 7000",
			kind:  shellAgentCommand,
			label: "global-ip ipv6 --timeout 7000",
		},
		{
			name:  "global ip options first",
			line:  "request> global-ip timeout 7000 ipv6",
			kind:  shellAgentCommand,
			label: "global-ip ipv6 --timeout 7000",
		},
		{
			name:  "request> dns",
			line:  "request> dns example.test type AAAA timeout 9000",
			kind:  shellAgentCommand,
			label: "dns example.test AAAA --timeout 9000",
		},
		{
			name:  "request dns from top level",
			line:  "request dns example.test type AAAA timeout 9000",
			kind:  shellAgentCommand,
			label: "dns example.test AAAA --timeout 9000",
		},
		{
			name:  "request> download options first",
			line:  "request> download timeout 9000 https://example.test/file.bin",
			kind:  shellAgentCommand,
			label: "download https://example.test/file.bin --timeout 9000",
		},
		{
			name: "configure",
			line: "configure",
			kind: shellEnterConfigureMode,
		},
		{
			name: "show devices",
			line: "show devices",
			kind: shellShowDevices,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseShellLineForTest(tt.line)
			if err != nil {
				t.Fatalf("parseShellLineForTest() error = %v", err)
			}
			if got.kind != tt.kind {
				t.Fatalf("kind = %v, want %v", got.kind, tt.kind)
			}
			if tt.label != "" {
				assertOperationLabel(t, got.operation, tt.label)
			}
			if tt.hops != nil {
				assertTracerouteHops(t, got.operation, tt.hops)
			}
		})
	}
}

func TestParseShellCommandPrefixes(t *testing.T) {
	got, err := parseShellLineForTest("sho wi cap")
	if err != nil {
		t.Fatalf("parseShellLineForTest() error = %v", err)
	}
	if got.kind != shellAgentCommand {
		t.Fatalf("kind = %v, want shellAgentCommand", got.kind)
	}
	assertOperationLabel(t, got.operation, "wifi capabilities")

	if _, err := parseShellLineForTest("sho wi s"); err == nil {
		t.Fatalf("parseShellLineForTest() error = nil for ambiguous wifi command")
	}
}

func TestParseShellModesSeparateConfigureAndRequest(t *testing.T) {
	if _, err := parseShellLineForTest("set standalone enabled"); err == nil || !strings.Contains(err.Error(), `unknown command "set"`) {
		t.Fatalf("top-level set error = %v", err)
	}
	request, err := parseShellLineForTest("request ping 1.1.1.1")
	if err != nil {
		t.Fatalf("top-level request ping: %v", err)
	}
	if request.kind != shellAgentCommand {
		t.Fatalf("top-level request ping kind = %v", request.kind)
	}
	assertOperationLabel(t, request.operation, "ping 1.1.1.1")

	if _, err := parseShellLineForTest("ping 1.1.1.1 count 1"); err == nil || !strings.Contains(err.Error(), `unknown command "ping"`) {
		t.Fatalf("top-level direct ping error = %v", err)
	}

	set, err := parseShellLineForTest("config> set standalone enabled")
	if err != nil {
		t.Fatalf("config set standalone: %v", err)
	}
	if set.kind != shellAgentCommand {
		t.Fatalf("config set kind = %v", set.kind)
	}

	run, err := parseShellLineForTest("config> run request ping 1.1.1.1 count 1")
	if err != nil {
		t.Fatalf("config run request ping: %v", err)
	}
	if run.kind != shellAgentCommand {
		t.Fatalf("config run request kind = %v", run.kind)
	}
	assertOperationLabel(t, run.operation, "ping 1.1.1.1 1")

}

func TestParseShellStandaloneCommands(t *testing.T) {
	status, err := parseShellLineForTest("show standalone status")
	if err != nil {
		t.Fatalf("show standalone status: %v", err)
	}
	if status.kind != shellAgentCommand {
		t.Fatalf("status kind = %v", status.kind)
	}
	cmd, _, err := buildRunCommand(status.operation)
	if err != nil {
		t.Fatalf("build status command: %v", err)
	}
	if cmd.GetGetStandaloneStatus() == nil {
		t.Fatalf("status command = %#v", cmd)
	}

	sync, err := parseShellLineForTest("sync standalone runs output out/standalone limit 10 keep-unsynced")
	if err != nil {
		t.Fatalf("sync standalone runs: %v", err)
	}
	if sync.kind != shellStandaloneSync || sync.syncOutput != "out/standalone" || sync.syncLimit != "10" || sync.syncMark {
		t.Fatalf("sync = %#v", sync)
	}

	uploadTo, err := parseShellLineForTest("config> set standalone upload to http://192.168.50.10:8080/dropcheck/incoming")
	if err != nil {
		t.Fatalf("set standalone upload to: %v", err)
	}
	cmd, _, err = buildRunCommand(uploadTo.operation)
	if err != nil {
		t.Fatalf("build upload to command: %v", err)
	}
	edits := cmd.GetEditStandaloneConfig().GetEdits()
	if len(edits) != 1 || strings.Join(edits[0].GetPath(), "/") != "upload/url" ||
		edits[0].GetValue() != "http://192.168.50.10:8080/dropcheck/incoming" {
		t.Fatalf("upload to edits = %#v", edits)
	}

	uploadWifi, err := parseShellLineForTest("config> set standalone upload via wifi essid NOC passphrase secret security wpa3 band 6ghz timeout 5s")
	if err != nil {
		t.Fatalf("set standalone upload via wifi: %v", err)
	}
	cmd, _, err = buildRunCommand(uploadWifi.operation)
	if err != nil {
		t.Fatalf("build upload wifi command: %v", err)
	}
	edits = cmd.GetEditStandaloneConfig().GetEdits()
	if len(edits) != 6 || strings.Join(edits[0].GetPath(), "/") != "upload/wifi" ||
		edits[0].GetAction() != controlpb.StandaloneEdit_ACTION_DELETE ||
		strings.Join(edits[1].GetPath(), "/") != "upload/wifi/ssid" ||
		edits[1].GetValue() != "NOC" ||
		strings.Join(edits[5].GetPath(), "/") != "upload/wifi/timeout_ms" ||
		edits[5].GetValue() != "5000" {
		t.Fatalf("upload wifi edits = %#v", edits)
	}

	wifiMatch, err := parseShellLineForTest("config> set standalone festa smoke wifi mgmt match essid NOC mac-randomization non-persistent")
	if err != nil {
		t.Fatalf("set standalone festa wifi match: %v", err)
	}
	cmd, _, err = buildRunCommand(wifiMatch.operation)
	if err != nil {
		t.Fatalf("build wifi match command: %v", err)
	}
	edits = cmd.GetEditStandaloneConfig().GetEdits()
	if len(edits) != 2 ||
		strings.Join(edits[0].GetPath(), "/") != "festa/smoke/wifi/mgmt/match/essid" ||
		edits[0].GetValue() != "NOC" ||
		strings.Join(edits[1].GetPath(), "/") != "festa/smoke/wifi/mgmt/mac_randomization" ||
		edits[1].GetValue() != "non-persistent" {
		t.Fatalf("wifi match edits = %#v", edits)
	}

	wifiPassphrase, err := parseShellLineForTest("config> set standalone festa smoke wifi mgmt passphrase secret security wpa3")
	if err != nil {
		t.Fatalf("set standalone festa wifi passphrase: %v", err)
	}
	cmd, _, err = buildRunCommand(wifiPassphrase.operation)
	if err != nil {
		t.Fatalf("build wifi passphrase command: %v", err)
	}
	edits = cmd.GetEditStandaloneConfig().GetEdits()
	if len(edits) != 2 ||
		strings.Join(edits[0].GetPath(), "/") != "festa/smoke/wifi/mgmt/passphrase" ||
		edits[0].GetValue() != "secret" ||
		strings.Join(edits[1].GetPath(), "/") != "festa/smoke/wifi/mgmt/security" ||
		edits[1].GetValue() != "wpa3" {
		t.Fatalf("wifi passphrase edits = %#v", edits)
	}

	quotedWifiMatch, err := parseShellLineForTest(`config> set standalone festa smoke wifi lab2 match essid "SHIZK RADIO MOBILE"`)
	if err != nil {
		t.Fatalf("set standalone festa quoted wifi match: %v", err)
	}
	cmd, _, err = buildRunCommand(quotedWifiMatch.operation)
	if err != nil {
		t.Fatalf("build quoted wifi match command: %v", err)
	}
	edits = cmd.GetEditStandaloneConfig().GetEdits()
	if len(edits) != 1 ||
		strings.Join(edits[0].GetPath(), "/") != "festa/smoke/wifi/lab2/match/essid" ||
		edits[0].GetValue() != "SHIZK RADIO MOBILE" {
		t.Fatalf("quoted wifi match edits = %#v", edits)
	}

	pingCheck, err := parseShellLineForTest("config> set standalone festa smoke check cloudflare test ping host 1.1.1.1 count 1 timeout 8s")
	if err != nil {
		t.Fatalf("set standalone festa named ping check: %v", err)
	}
	cmd, _, err = buildRunCommand(pingCheck.operation)
	if err != nil {
		t.Fatalf("build named ping check command: %v", err)
	}
	edits = cmd.GetEditStandaloneConfig().GetEdits()
	if len(edits) != 4 ||
		strings.Join(edits[0].GetPath(), "/") != "festa/smoke/check/cloudflare/test" ||
		edits[0].GetValue() != "ping" ||
		strings.Join(edits[1].GetPath(), "/") != "festa/smoke/check/cloudflare/host" ||
		edits[1].GetValue() != "1.1.1.1" ||
		strings.Join(edits[3].GetPath(), "/") != "festa/smoke/check/cloudflare/timeout_ms" ||
		edits[3].GetValue() != "8000" {
		t.Fatalf("named ping check edits = %#v", edits)
	}

	delWifi, err := parseShellLineForTest("config> delete standalone festa smoke wifi mgmt")
	if err != nil {
		t.Fatalf("delete standalone festa wifi: %v", err)
	}
	cmd, _, err = buildRunCommand(delWifi.operation)
	if err != nil {
		t.Fatalf("build delete wifi command: %v", err)
	}
	edits = cmd.GetEditStandaloneConfig().GetEdits()
	if len(edits) != 1 || edits[0].GetAction() != controlpb.StandaloneEdit_ACTION_DELETE || strings.Join(edits[0].GetPath(), "/") != "festa/smoke/wifi/mgmt" {
		t.Fatalf("delete wifi edits = %#v", edits)
	}

	del, err := parseShellLineForTest("config> delete standalone festa smoke")
	if err != nil {
		t.Fatalf("delete standalone festa: %v", err)
	}
	if del.kind != shellAgentCommand {
		t.Fatalf("delete kind = %v", del.kind)
	}
	cmd, _, err = buildRunCommand(del.operation)
	if err != nil {
		t.Fatalf("build delete command: %v", err)
	}
	edits = cmd.GetEditStandaloneConfig().GetEdits()
	if len(edits) != 1 || edits[0].GetAction() != controlpb.StandaloneEdit_ACTION_DELETE || strings.Join(edits[0].GetPath(), "/") != "festa/smoke" {
		t.Fatalf("delete edits = %#v", edits)
	}
}

func TestParseStandaloneSyncLimitRejectsZero(t *testing.T) {
	_, err := parseStandaloneSyncLimit("0")
	if err == nil || !strings.Contains(err.Error(), "positive integer") {
		t.Fatalf("parseStandaloneSyncLimit(0) error = %v", err)
	}
}

func TestParseShellSetEnabledWithoutRequiredValues(t *testing.T) {
	_, err := parseShellLineForTest("config> set standalone festa lab wifi office match")
	if err == nil || !strings.Contains(err.Error(), "wifi <name> match <essid|bssid> <value>") {
		t.Fatalf("set standalone wifi match error = %v", err)
	}
}

func TestShellCommandBuildsOperation(t *testing.T) {
	got, err := parseShellLineForTest("request> wifi connect Lab passphrase secret security wpa3 band 6ghz timeout 12345")
	if err != nil {
		t.Fatalf("parseShellLineForTest() error = %v", err)
	}
	if got.operation.Name != "wifi.connect" {
		t.Fatalf("operation name = %q", got.operation.Name)
	}
	cmd, _, err := buildRunCommand(got.operation)
	if err != nil {
		t.Fatalf("buildRunCommand() error = %v", err)
	}
	connect := cmd.GetConnectWifi()
	if connect == nil {
		t.Fatalf("connect command = nil")
	}
	if connect.GetSsid() != "Lab" || connect.GetPassphrase() != "secret" {
		t.Fatalf("connect credentials = %q/%q", connect.GetSsid(), connect.GetPassphrase())
	}
	if connect.GetSecurity() != controlpb.ConnectWifi_SECURITY_WPA3_SAE || connect.GetBand() != controlpb.WifiBand_WIFI_BAND_6_GHZ || connect.GetTimeoutMs() != 12345 {
		t.Fatalf("connect options = %#v", connect)
	}
}

func TestShellWifiConnectDefaultsSecurityToAuto(t *testing.T) {
	got, err := parseShellLineForTest("request> wifi connect 'Lab SSID' passphrase 'secret-passphrase'")
	if err != nil {
		t.Fatalf("parseShellLineForTest() error = %v", err)
	}
	cmd, _, err := buildRunCommand(got.operation)
	if err != nil {
		t.Fatalf("buildRunCommand() error = %v", err)
	}
	connect := cmd.GetConnectWifi()
	if connect == nil {
		t.Fatalf("connect command = nil")
	}
	if connect.GetSsid() != "Lab SSID" || connect.GetPassphrase() != "secret-passphrase" {
		t.Fatalf("connect credentials = %q/%q", connect.GetSsid(), connect.GetPassphrase())
	}
	if connect.GetSecurity() != controlpb.ConnectWifi_SECURITY_UNSPECIFIED {
		t.Fatalf("security = %v, want SECURITY_UNSPECIFIED", connect.GetSecurity())
	}
}

func TestParseShellRejectsDuplicateOptions(t *testing.T) {
	tests := []struct {
		line string
		want string
	}{
		{line: "show standalone runs synced synced", want: "synced specified twice"},
		{line: "show standalone runs limit 1 limit 2", want: "limit specified twice"},
		{line: "show wifi scan fresh timeout 100 timeout 200", want: "timeout specified twice"},
		{line: "show wifi eht fresh timeout 100 timeout 200", want: "timeout specified twice"},
		{line: "request> wifi connect passphrase secret security auto security wpa3 Lab", want: "security specified twice"},
		{line: "request> wifi assert ip ip", want: "ip specified twice"},
		{line: "request> wifi cycle passphrase secret count 1 count 2 Lab", want: "count specified twice"},
		{line: "request> ping 1.1.1.1 count 1 count 2", want: "count specified twice"},
		{line: "request> traceroute 1.1.1.1 max-hops 1 max-hops 2", want: "max-hops specified twice"},
		{line: "request> path-mtu min-mtu 1200 min-mtu 1300 1.1.1.1", want: "min-mtu specified twice"},
		{line: "request> dns example.com type A type AAAA", want: "type specified twice"},
		{line: "request> http https://example.com expected-status 200 expected-status 204", want: "expected-status specified twice"},
		{line: "sync standalone runs mark-synced keep-unsynced", want: "mark-synced and keep-unsynced cannot be used together"},
	}
	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			_, err := parseShellLineForTest(tt.line)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("parseShellLineForTest() error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestParseShellRejectsWifiOptionsConsumingSSID(t *testing.T) {
	tests := []struct {
		line string
		want string
	}{
		{line: `request> wifi connect passphrase secret bssid "Lab SSID"`, want: "bssid requires a value before <ssid>"},
		{line: `request> wifi connect passphrase secret band "Lab SSID"`, want: "band requires a value before <ssid>"},
		{line: `request> wifi cycle passphrase secret http "Lab SSID"`, want: "http requires a value before <ssid>"},
		{line: `request> wifi cycle passphrase secret ping "Lab SSID"`, want: "ping requires a value before <ssid>"},
	}
	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			_, err := parseShellLineForTest(tt.line)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("parseShellLineForTest() error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestParseShellPipeline(t *testing.T) {
	got, err := parseShellLineForTest(`show wifi scan | match "Lab AP" | except guest | display json | count`)
	if err != nil {
		t.Fatalf("parseShellLineForTest() error = %v", err)
	}
	if !got.pipeline.displayJSON {
		t.Fatalf("displayJSON = false")
	}
	if len(got.pipeline.stages) != 3 {
		t.Fatalf("pipeline stages = %d, want 3", len(got.pipeline.stages))
	}
	text, err := got.pipeline.apply("Lab AP main\nguest Lab AP\nOther\n")
	if err != nil {
		t.Fatalf("pipeline apply error = %v", err)
	}
	if text != "Count: 1 lines\n" {
		t.Fatalf("pipeline output = %q", text)
	}

	setConfig, err := parseShellLineForTest(`show config | display set | match standalone`)
	if err != nil {
		t.Fatalf("parseShellLineForTest(show config display set) error = %v", err)
	}
	if setConfig.kind != shellShowConfig || !setConfig.pipeline.displaySet || setConfig.pipeline.format(outputText) != outputSet || len(setConfig.pipeline.stages) != 1 {
		t.Fatalf("display set pipeline = kind %v displaySet %t format %q stages %d", setConfig.kind, setConfig.pipeline.displaySet, setConfig.pipeline.format(outputText), len(setConfig.pipeline.stages))
	}

	_, err = parseShellLineForTest(`show devices | count | display json`)
	if err == nil || !strings.Contains(err.Error(), "display json must appear before count") {
		t.Fatalf("parseShellLineForTest(count then display) error = %v", err)
	}
}

func TestShellHelpAndCompletion(t *testing.T) {
	help := shellHelpEntriesForTest("show wifi ?")
	var tokens []string
	for _, entry := range help {
		tokens = append(tokens, entry.token)
	}
	for _, want := range []string{"status", "diagnostics", "eht", "scan", "capabilities"} {
		if !slices.Contains(tokens, want) {
			t.Fatalf("help tokens = %#v, missing %q", tokens, want)
		}
	}
	help = shellHelpEntriesForTest("show wifi eht ?")
	tokens = tokens[:0]
	for _, entry := range help {
		tokens = append(tokens, entry.token)
	}
	if !slices.Equal(tokens, []string{"fresh", "ssid", "bssid"}) {
		t.Fatalf("show wifi eht help tokens = %#v, want fresh/ssid/bssid", tokens)
	}

	completions := completeShellLineForTest("show wi", nil)
	if !slices.Contains(completions, "show wifi") {
		t.Fatalf("completions = %#v, missing show wifi", completions)
	}

	ipCompletions := completeShellLineForTest("show ip ", nil)
	if !slices.Contains(ipCompletions, "show ip status") {
		t.Fatalf("show ip completions = %#v, missing show ip status", ipCompletions)
	}
	ipFragments := shellCompletionFragmentsForTest("show ip ")
	if !slices.Contains(ipFragments, "status") {
		t.Fatalf("show ip completion fragments = %#v, missing status", ipFragments)
	}

	pipeCompletions := completeShellLineForTest("show wifi status | dis", nil)
	if !slices.Equal(pipeCompletions, []string{"show wifi status | display"}) {
		t.Fatalf("pipe completions = %#v, want only display", pipeCompletions)
	}
	displayCommandFragments := shellCompletionFragmentsForTest("show config | di")
	if !slices.Equal(displayCommandFragments, []string{"splay"}) {
		t.Fatalf("display command fragments = %#v, want only splay", displayCommandFragments)
	}
	for _, unexpected := range []string{"splay json", "splay set"} {
		if slices.Contains(displayCommandFragments, unexpected) {
			t.Fatalf("display command fragments = %#v, unexpectedly included %q", displayCommandFragments, unexpected)
		}
	}
	displayValueFragments := shellCompletionFragmentsForTest("show config | display ")
	for _, want := range []string{"json", "set"} {
		if !slices.Contains(displayValueFragments, want) {
			t.Fatalf("display value completions = %#v, missing %q", displayValueFragments, want)
		}
	}
	if slices.Contains(displayValueFragments, "standalone") {
		t.Fatalf("display value completions = %#v, unexpectedly included standalone", displayValueFragments)
	}
	help = shellHelpEntriesForTest("show config | display ?")
	tokens = tokens[:0]
	for _, entry := range help {
		tokens = append(tokens, entry.token)
	}
	for _, want := range []string{"json", "set"} {
		if !slices.Contains(tokens, want) {
			t.Fatalf("display value help tokens = %#v, missing %q", tokens, want)
		}
	}
	if slices.Contains(tokens, "standalone") {
		t.Fatalf("display value help tokens = %#v, unexpectedly included standalone", tokens)
	}

	if !isHelpLine("show wifi？") {
		t.Fatalf("full-width help suffix was not recognized")
	}

	help = shellHelpEntriesForTest("show wifi scan fresh timeout ?")
	tokens = tokens[:0]
	for _, entry := range help {
		tokens = append(tokens, entry.token)
	}
	if !slices.Equal(tokens, []string{"<ms>"}) {
		t.Fatalf("show wifi scan fresh timeout help tokens = %#v, want <ms>", tokens)
	}
	help = shellHelpEntriesForTest("show wifi eht fresh timeout ?")
	tokens = tokens[:0]
	for _, entry := range help {
		tokens = append(tokens, entry.token)
	}
	if !slices.Equal(tokens, []string{"<ms>"}) {
		t.Fatalf("show wifi eht fresh timeout help tokens = %#v, want <ms>", tokens)
	}

	help = shellHelpEntriesForTest("?")
	tokens = tokens[:0]
	for _, entry := range help {
		tokens = append(tokens, entry.token)
	}
	if !slices.Equal(tokens, []string{"show", "clear", "sync", "configure", "request", "help", "quit"}) {
		t.Fatalf("top-level help tokens = %#v", tokens)
	}
	for _, directRequestCommand := range []string{"wifi", "standalone", "monitor", "ping", "traceroute", "path-mtu", "global-ip", "dns", "http", "download", "exit"} {
		if slices.Contains(tokens, directRequestCommand) {
			t.Fatalf("top-level help tokens = %#v, unexpectedly included %q", tokens, directRequestCommand)
		}
	}

	topCompletions := completeShellLineForTest("p", nil)
	if slices.Contains(topCompletions, "ping") {
		t.Fatalf("top-level completions = %#v, unexpectedly included ping", topCompletions)
	}
	requestCompletions := completeShellLineForTest("request p", nil)
	if !slices.Contains(requestCompletions, "request ping") {
		t.Fatalf("request-prefixed completions = %#v, missing request ping", requestCompletions)
	}
}

func TestShellHTTPHelpAndFlexibleArgs(t *testing.T) {
	help := shellHelpEntriesForTest("request> http ?")
	var tokens []string
	for _, entry := range help {
		tokens = append(tokens, entry.token)
	}
	for _, want := range []string{"<url>", "expected-status", "timeout"} {
		if !slices.Contains(tokens, want) {
			t.Fatalf("request> http help tokens = %#v, missing %q", tokens, want)
		}
	}

	help = shellHelpEntriesForTest("request> http expected-status 301 http://www.wide.ad.jp ?")
	tokens = tokens[:0]
	for _, entry := range help {
		tokens = append(tokens, entry.token)
	}
	if slices.Contains(tokens, "expected-status") {
		t.Fatalf("terminal http help tokens = %#v, unexpectedly included expected-status", tokens)
	}
	for _, want := range []string{"timeout", "<cr>", "| display json"} {
		if !slices.Contains(tokens, want) {
			t.Fatalf("terminal http help tokens = %#v, missing %q", tokens, want)
		}
	}

	got, err := parseShellLineForTest("request> http expected-status 301 www.wide.ad.jp timeout 7000")
	if err != nil {
		t.Fatalf("parseShellLineForTest(http) error = %v", err)
	}
	if got.operation.Name != "http" {
		t.Fatalf("operation name = %q", got.operation.Name)
	}
	cmd, _, err := buildRunCommand(got.operation)
	if err != nil {
		t.Fatalf("buildRunCommand(http) error = %v", err)
	}
	http := cmd.GetHttpCheck()
	if http == nil {
		t.Fatalf("http command = nil")
	}
	if http.GetUrl() != "https://www.wide.ad.jp" || http.GetExpectedStatus() != 301 || http.GetTimeoutMs() != 7000 {
		t.Fatalf("http command = %#v", http)
	}
}

func TestShellDNSHelpAndFlexibleArgs(t *testing.T) {
	help := shellHelpEntriesForTest("request> dns ?")
	var tokens []string
	for _, entry := range help {
		tokens = append(tokens, entry.token)
	}
	for _, want := range []string{"<name>", "type", "timeout"} {
		if !slices.Contains(tokens, want) {
			t.Fatalf("request> dns help tokens = %#v, missing %q", tokens, want)
		}
	}

	help = shellHelpEntriesForTest("request> dns type a wide.ad.jp ?")
	tokens = tokens[:0]
	for _, entry := range help {
		tokens = append(tokens, entry.token)
	}
	for _, unwanted := range []string{"<name>", "type"} {
		if slices.Contains(tokens, unwanted) {
			t.Fatalf("terminal dns help tokens = %#v, unexpectedly included %q", tokens, unwanted)
		}
	}
	for _, want := range []string{"timeout", "<cr>", "| display json"} {
		if !slices.Contains(tokens, want) {
			t.Fatalf("terminal dns help tokens = %#v, missing %q", tokens, want)
		}
	}

	tests := []struct {
		line string
	}{
		{line: "request> dns type a wide.ad.jp timeout 7000"},
		{line: "request> dns wide.ad.jp type A timeout 7000"},
		{line: "request> dns wide.ad.jp a timeout 7000"},
	}
	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			got, err := parseShellLineForTest(tt.line)
			if err != nil {
				t.Fatalf("parseShellLineForTest(dns) error = %v", err)
			}
			if got.operation.Name != "dns" {
				t.Fatalf("operation name = %q", got.operation.Name)
			}
			cmd, _, err := buildRunCommand(got.operation)
			if err != nil {
				t.Fatalf("buildRunCommand(dns) error = %v", err)
			}
			resolve := cmd.GetResolveDns()
			if resolve == nil {
				t.Fatalf("dns command = nil")
			}
			if resolve.GetName() != "wide.ad.jp" || resolve.GetTimeoutMs() != 7000 || !slices.Equal(resolve.GetQtypes(), []controlpb.DnsRecordType{controlpb.DnsRecordType_DNS_RECORD_TYPE_A}) {
				t.Fatalf("dns command = %#v", resolve)
			}
		})
	}
}

func TestShellGlobalIPHelp(t *testing.T) {
	help := shellHelpEntriesForTest("request> global-ip ?")
	var tokens []string
	for _, entry := range help {
		tokens = append(tokens, entry.token)
	}
	for _, want := range []string{"ipv4", "ipv6", "all", "timeout", "<cr>", "| display json"} {
		if !slices.Contains(tokens, want) {
			t.Fatalf("request> global-ip help tokens = %#v, missing %q", tokens, want)
		}
	}

	help = shellHelpEntriesForTest("request> global-ip ipv4 ?")
	tokens = tokens[:0]
	for _, entry := range help {
		tokens = append(tokens, entry.token)
	}
	for _, unwanted := range []string{"ipv4", "ipv6", "all"} {
		if slices.Contains(tokens, unwanted) {
			t.Fatalf("request> global-ip ipv4 help tokens = %#v, unexpectedly included %q", tokens, unwanted)
		}
	}
	for _, want := range []string{"timeout", "<cr>", "| display json"} {
		if !slices.Contains(tokens, want) {
			t.Fatalf("request> global-ip ipv4 help tokens = %#v, missing %q", tokens, want)
		}
	}
}

func TestShellTerminalHelp(t *testing.T) {
	help := shellHelpEntriesForTest("show devices ?")
	var tokens []string
	for _, entry := range help {
		tokens = append(tokens, entry.token)
	}
	for _, want := range []string{"<cr>", "| display json", "| match <regex>", "| except <regex>", "| count", "| no-more"} {
		if !slices.Contains(tokens, want) {
			t.Fatalf("terminal help tokens = %#v, missing %q", tokens, want)
		}
	}
	for _, unwanted := range []string{"show", "config", "wifi"} {
		if slices.Contains(tokens, unwanted) {
			t.Fatalf("terminal help tokens = %#v, unexpectedly included %q", tokens, unwanted)
		}
	}

	help = shellHelpEntriesForTest("config> set standalone enabled ?")
	tokens = tokens[:0]
	for _, entry := range help {
		tokens = append(tokens, entry.token)
	}
	for _, want := range []string{"<cr>", "| display json", "| match <regex>", "| except <regex>", "| count", "| no-more"} {
		if !slices.Contains(tokens, want) {
			t.Fatalf("set standalone terminal help tokens = %#v, missing %q", tokens, want)
		}
	}
}

func TestShellUsagePlacesPositionalsLast(t *testing.T) {
	_, err := parseShellLineForTest("request> ping")
	if err == nil {
		t.Fatalf("parseShellLineForTest(ping) error = nil")
	}
	if !strings.Contains(err.Error(), "usage: ping [count <n>] [size <bytes>] [timeout <ms>] <host>") {
		t.Fatalf("request> ping usage = %q", err)
	}
}

func TestShellImmediateHelpKey(t *testing.T) {
	line := []rune("show wifi ?")
	var out bytes.Buffer
	newLine, newPos, ok := handleShellHelpKey(&out, line, len(line), '?')
	if !ok {
		t.Fatalf("handleShellHelpKey ok = false")
	}
	if got := string(newLine); got != "show wifi " {
		t.Fatalf("new line = %q, want %q", got, "show wifi ")
	}
	if newPos != len([]rune("show wifi ")) {
		t.Fatalf("new pos = %d, want %d", newPos, len([]rune("show wifi ")))
	}
	for _, want := range []string{"status", "diagnostics", "eht", "scan", "capabilities"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("help output = %q, missing %q", out.String(), want)
		}
	}

	line = []rune("wifi？")
	out.Reset()
	newLine, _, ok = handleShellHelpKey(&out, line, len(line), '？', &shellState{mode: shellModeRequest})
	if !ok {
		t.Fatalf("handleShellHelpKey full-width ok = false")
	}
	if got := string(newLine); got != "wifi" {
		t.Fatalf("full-width new line = %q, want %q", got, "wifi")
	}
	if !strings.Contains(out.String(), "connect") {
		t.Fatalf("full-width help output = %q, missing connect", out.String())
	}

	line = []rune("set standalone festa smoke check cloudflare test ping ?")
	out.Reset()
	newLine, _, ok = handleShellHelpKey(&out, line, len(line), '?', &shellState{mode: shellModeConfigure})
	if !ok {
		t.Fatalf("configure help key ok = false")
	}
	if got := string(newLine); got != "set standalone festa smoke check cloudflare test ping " {
		t.Fatalf("configure help new line = %q", got)
	}
	for _, want := range []string{"host", "count", "size", "timeout"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("configure help output = %q, missing %q", out.String(), want)
		}
	}
	for _, unexpected := range []string{"<name>", "upload"} {
		if strings.Contains(out.String(), unexpected) {
			t.Fatalf("configure help output = %q, unexpectedly included %q", out.String(), unexpected)
		}
	}
}

func TestShellReadlineCompleter(t *testing.T) {
	completer := shellReadlineCompleter{}
	completions, offset := completer.Do([]rune("show wi"), len([]rune("show wi")))
	if offset != len([]rune("wi")) {
		t.Fatalf("offset = %d, want %d", offset, len([]rune("wi")))
	}
	if len(completions) != 1 || string(completions[0]) != "fi " {
		t.Fatalf("completions = %#v, want fi plus a space", completions)
	}

	requestCompleter := shellReadlineCompleter{state: &shellState{mode: shellModeRequest}}
	completions, offset = requestCompleter.Do([]rune("global-ip"), len([]rune("global-ip")))
	if offset != len([]rune("global-ip")) {
		t.Fatalf("request> global-ip offset = %d, want %d", offset, len([]rune("global-ip")))
	}
	if len(completions) != 1 || string(completions[0]) != " " {
		t.Fatalf("request> global-ip exact completions = %#v, want a space", completions)
	}

	completionStrings := shellCompletionFragmentsForTest("request> global-ip ")
	for _, want := range []string{"ipv4", "ipv6", "all", "timeout"} {
		if !slices.Contains(completionStrings, want) {
			t.Fatalf("request> global-ip option completions = %#v, missing %q", completionStrings, want)
		}
	}

	completions, _ = requestCompleter.Do([]rune("global-ip ipv4 "), len([]rune("global-ip ipv4 ")))
	if !slices.ContainsFunc(completions, func(candidate []rune) bool {
		return strings.TrimSpace(string(candidate)) == "timeout"
	}) {
		t.Fatalf("request> global-ip completions = %#v, missing timeout", completions)
	}
	for _, unexpected := range []string{"ipv4", "ipv6", "all"} {
		if slices.ContainsFunc(completions, func(candidate []rune) bool {
			return strings.TrimSpace(string(candidate)) == unexpected
		}) {
			t.Fatalf("request> global-ip completions = %#v, unexpectedly included %q", completions, unexpected)
		}
	}

	completions, offset = requestCompleter.Do([]rune("ping count "), len([]rune("ping count ")))
	if offset != 0 {
		t.Fatalf("placeholder offset = %d, want 0", offset)
	}
	if len(completions) != 0 {
		t.Fatalf("placeholder completions = %#v, want no selectable candidates", completions)
	}
	if got := shellCompletionHintLineForTest("request> ping count ", nil); got != "<n>" {
		t.Fatalf("placeholder hint = %q, want <n>", got)
	}

	completions, offset = completer.Do([]rune("ping count "), len([]rune("ping count ")))
	if offset != 0 {
		t.Fatalf("top-level direct ping offset = %d, want 0", offset)
	}
	if len(completions) != 0 {
		t.Fatalf("top-level direct ping completions = %#v, want no selectable candidates", completions)
	}
	if got := shellCompletionHintLineForTest("ping count ", nil); got != "" {
		t.Fatalf("top-level direct ping hint = %q, want empty", got)
	}

	completions, _ = requestCompleter.Do([]rune("http expected-status "), len([]rune("http expected-status ")))
	if len(completions) != 0 {
		t.Fatalf("placeholder completions = %#v, want no selectable candidates", completions)
	}
	if got := shellCompletionHintLineForTest("request> http expected-status ", nil); got != "<code>" {
		t.Fatalf("placeholder hint = %q, want <code>", got)
	}

	configureCompleter := shellReadlineCompleter{state: &shellState{mode: shellModeConfigure}}
	line := []rune("set standalone festa smoke check cloudflare test ping h")
	completions, offset = configureCompleter.Do(line, len(line))
	if offset != len([]rune("h")) {
		t.Fatalf("configure completion offset = %d, want 1", offset)
	}
	if len(completions) != 1 || string(completions[0]) != "ost " {
		t.Fatalf("configure completion = %#v, want ost plus a space", completions)
	}
}

func TestShellReadlineCompletionsAreSingleTokens(t *testing.T) {
	lines := []string{
		"",
		"s",
		"show ",
		"show c",
		"show config ",
		"show wifi ",
		"show wifi eht ",
		"show wifi scan ",
		"show adb ",
		"show adb dumpsys ",
		"clear ",
		"sync ",
		"sync standalone runs ",
		"request ",
		"request wi",
		"request wifi ",
		"request wifi connect ",
		"request wifi wait connected ",
		"request ping ",
		"request dns example.test ",
		"show config | ",
		"show config | di",
		"show config | display ",
		"show config | display j",
		"show config | display s",
		"show config | ma",
		"show config | match ",
		"show config | ex",
		"show config | except ",
		"config> ",
		"config> s",
		"config> show ",
		"config> set ",
		"config> set standalone ",
		"config> set standalone u",
		"config> set standalone upload ",
		"config> set standalone upload v",
		"config> set standalone upload via ",
		"config> set standalone upload via wifi ",
		"config> set standalone festa smoke ",
		"config> set standalone festa smoke wifi ",
		"config> set standalone festa smoke wifi mgmt ",
		"config> set standalone festa smoke wifi mgmt match ",
		"config> set standalone festa smoke check ",
		"config> set standalone festa smoke check dns-main ",
		"config> set standalone festa smoke check dns-main test ",
		"config> set standalone festa smoke check dns-main test dns ",
		"config> delete ",
		"config> run ",
		"config> run sh",
		"config> run show ",
		"config> run request ",
		"config> run request wi",
		"request> ",
		"request> wi",
		"request> wifi ",
		"request> wifi connect ",
		"request> wifi wait ",
		"request> wifi wait connected ",
		"request> standalone ",
		"request> standalone run ",
		"request> standalone run once ",
		"request> monitor ",
		"request> monitor wifi ",
		"request> ping ",
		"request> traceroute ",
		"request> path-mtu ",
		"request> global-ip ",
		"request> dns ",
		"request> http ",
		"request> download ",
	}
	for _, line := range lines {
		t.Run(line, func(t *testing.T) {
			completions := shellCompletionFragmentsForTest(line)
			for _, completion := range completions {
				if completion == "" {
					continue
				}
				if strings.ContainsAny(completion, " \t\r\n") {
					t.Fatalf("completion fragment %q contains whitespace; completions = %#v", completion, completions)
				}
			}
		})
	}
}

func TestShellOptionCompletion(t *testing.T) {
	tests := []struct {
		line string
		want []string
	}{
		{
			line: "show wifi scan fresh ",
			want: []string{"all", "2.4ghz", "5ghz", "6ghz", "60ghz", "timeout"},
		},
		{
			line: "show wifi eht ",
			want: []string{"fresh", "ssid", "bssid"},
		},
		{
			line: "show wifi eht fresh ",
			want: []string{"timeout", "ssid", "bssid"},
		},
		{
			line: "show wifi scan detail Lab ",
			want: []string{"all", "2.4ghz", "5ghz", "6ghz", "60ghz"},
		},
		{
			line: "request> wifi connect Lab ",
			want: []string{"passphrase", "security", "bssid", "band", "mac-randomization", "timeout"},
		},
		{
			line: "request> wifi connect Lab security ",
			want: []string{"auto", "wpa2", "wpa3", "transition"},
		},
		{
			line: "request> wifi connect Lab band ",
			want: []string{"all", "2.4ghz", "5ghz", "6ghz", "60ghz"},
		},
		{
			line: "request> wifi connect Lab mac-randomization ",
			want: []string{"auto", "none", "persistent", "non-persistent"},
		},
		{
			line: "config> set standalone festa smoke ",
			want: []string{"enabled", "disabled", "interval", "wifi", "check"},
		},
		{
			line: "config> set standalone festa smoke wifi mgmt ",
			want: []string{"match", "passphrase", "band", "wait", "timeout"},
		},
		{
			line: "config> set standalone festa smoke wifi mgmt match ",
			want: []string{"essid", "bssid"},
		},
		{
			line: "config> set standalone festa smoke wifi mgmt match essid Lab mac-randomization ",
			want: []string{"auto", "none", "persistent", "non-persistent"},
		},
		{
			line: "config> set standalone festa smoke wifi mgmt passphrase secret ",
			want: []string{"security"},
		},
		{
			line: "config> set standalone festa smoke wifi mgmt passphrase secret security ",
			want: []string{"auto", "wpa2", "wpa3", "transition"},
		},
		{
			line: "config> set standalone festa smoke wifi mgmt wait ",
			want: []string{"ip", "validated"},
		},
		{
			line: "config> set standalone festa smoke check cloudflare ",
			want: []string{"test"},
		},
		{
			line: "config> set standalone festa smoke check cloudflare test ",
			want: []string{"ping", "dns", "http"},
		},
		{
			line: "config> set standalone festa smoke check dns-main test dns ",
			want: []string{"name", "type", "timeout"},
		},
		{
			line: "config> set standalone festa smoke check dns-main test dns name example.test ",
			want: []string{"type", "timeout"},
		},
		{
			line: "config> set standalone festa smoke check dns-main test dns type ",
			want: []string{"A", "AAAA", "ALL"},
		},
		{
			line: "config> set standalone festa smoke check cloudflare test ping ",
			want: []string{"host", "count", "size", "timeout"},
		},
		{
			line: "config> set standalone festa smoke check cloudflare test ping host 1.1.1.1 ",
			want: []string{"count", "size", "timeout"},
		},
		{
			line: "config> set standalone festa smoke check healthz test http ",
			want: []string{"url", "expected-status", "timeout"},
		},
		{
			line: "config> set standalone festa smoke check healthz test http url https://example.test ",
			want: []string{"expected-status", "timeout"},
		},
		{
			line: "request> wifi wait connected security ",
			want: []string{"wpa2", "wpa3", "transition"},
		},
		{
			line: "request> monitor wifi ",
			want: []string{"duration", "interval"},
		},
		{
			line: "request> ping ",
			want: []string{"count", "size", "timeout"},
		},
		{
			line: "request ping ",
			want: []string{"count", "size", "timeout"},
		},
		{
			line: "request> ping example.test ",
			want: []string{"count", "size", "timeout"},
		},
		{
			line: "request> traceroute example.test ",
			want: []string{"max-hops", "via", "size", "timeout"},
		},
		{
			line: "request> path-mtu example.test ",
			want: []string{"min-mtu", "max-mtu", "timeout"},
		},
		{
			line: "request> global-ip ",
			want: []string{"ipv4", "ipv6", "all", "timeout"},
		},
		{
			line: "request> dns example.test ",
			want: []string{"type", "timeout"},
		},
		{
			line: "request dns example.test ",
			want: []string{"type", "timeout"},
		},
		{
			line: "request> dns example.test type ",
			want: []string{"A", "AAAA", "ALL"},
		},
		{
			line: "request> http example.test ",
			want: []string{"expected-status", "timeout"},
		},
		{
			line: "request> download example.test ",
			want: []string{"timeout"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			completions := shellCompletionFragmentsForTest(tt.line)
			for _, want := range tt.want {
				if !slices.Contains(completions, want) {
					t.Fatalf("completions = %#v, missing %q", completions, want)
				}
			}
		})
	}
}

func TestConfigureStandaloneDeepHelp(t *testing.T) {
	tests := []struct {
		line       string
		want       []string
		unexpected []string
	}{
		{
			line: "config> set standalone festa ?",
			want: []string{"<name>"},
		},
		{
			line: "config> set standalone festa smoke ?",
			want: []string{"enabled", "disabled", "interval", "wifi", "check"},
		},
		{
			line: "config> set standalone festa smoke wifi ?",
			want: []string{"<name>"},
		},
		{
			line: "config> set standalone festa smoke wifi mgmt ?",
			want: []string{"match", "passphrase", "band", "wait", "timeout"},
		},
		{
			line: "config> set standalone festa smoke wifi mgmt match ?",
			want: []string{"essid", "bssid"},
		},
		{
			line: `config> set standalone festa smoke wifi mgmt match essid "SHIZK RADIO" ?`,
			want: []string{"mac-randomization", "<cr>"},
		},
		{
			line: "config> set standalone festa smoke wifi mgmt wait ?",
			want: []string{"ip", "validated"},
		},
		{
			line: "config> set standalone festa smoke check ?",
			want: []string{"<name>"},
		},
		{
			line: "config> set standalone festa smoke check cloudflare ?",
			want: []string{"test"},
		},
		{
			line: "config> set standalone festa smoke check cloudflare test ?",
			want: []string{"ping", "dns", "http"},
		},
		{
			line: "config> set standalone festa smoke check cloudflare test ping ?",
			want: []string{"host", "count", "size", "timeout"},
		},
		{
			line: "config> set standalone festa smoke check cloudflare test ping host 1.1.1.1 ?",
			want: []string{"count", "size", "timeout", "<cr>"},
		},
		{
			line: "config> set standalone festa smoke check dns-main test dns type ?",
			want: []string{"A", "AAAA", "ALL"},
		},
		{
			line: "config> set standalone festa smoke check healthz test http url https://example.test ?",
			want: []string{"expected-status", "timeout", "<cr>"},
		},
		{
			line:       "config> set standalone festa smoke check cloudflare test ping ?",
			unexpected: []string{"upload", "festa"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			help := shellHelpEntriesForTest(tt.line)
			var tokens []string
			for _, entry := range help {
				tokens = append(tokens, entry.token)
			}
			for _, want := range tt.want {
				if !slices.Contains(tokens, want) {
					t.Fatalf("help tokens = %#v, missing %q", tokens, want)
				}
			}
			for _, unexpected := range tt.unexpected {
				if slices.Contains(tokens, unexpected) {
					t.Fatalf("help tokens = %#v, unexpectedly included %q", tokens, unexpected)
				}
			}
		})
	}
}

func TestShellPlaceholderCompletionHints(t *testing.T) {
	tests := []struct {
		line string
		want string
	}{
		{line: "request> ping count ", want: "<n>"},
		{line: "request ping count ", want: "<n>"},
		{line: "show wifi scan fresh timeout ", want: "<ms>"},
		{line: "show wifi eht fresh timeout ", want: "<ms>"},
		{line: "request> ping count 5 size 64 timeout 7000 ", want: "<host>"},
		{line: "request ping count 5 size 64 timeout 7000 ", want: "<host>"},
		{line: "request> traceroute via ", want: "<host_or_ip>"},
		{line: "request> path-mtu min-mtu ", want: "<bytes>"},
		{line: "request> global-ip timeout ", want: "<ms>"},
		{line: "request> http expected-status ", want: "<code>"},
		{line: "request> download timeout ", want: "<ms>"},
		{line: "config> set standalone festa smoke interval ", want: "<duration>"},
		{line: "config> set standalone festa smoke wifi mgmt match essid ", want: "<essid>"},
		{line: "config> set standalone festa smoke check ", want: "<name>"},
		{line: "config> set standalone festa smoke check cloudflare test ping host ", want: "<host>"},
		{line: "config> set standalone festa smoke check cloudflare test ping count ", want: "<n>"},
		{line: "config> set standalone festa smoke check cloudflare test ping size ", want: "<bytes>"},
		{line: "config> set standalone festa smoke check healthz test http expected-status ", want: "<code>"},
	}

	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			if got := shellCompletionFragmentsForTest(tt.line); len(got) != 0 {
				t.Fatalf("selectable completions = %#v, want none", got)
			}
			if got := shellCompletionHintLineForTest(tt.line, nil); got != tt.want {
				t.Fatalf("hint = %q, want %q", got, tt.want)
			}
		})
	}
}

func shellCompletionFragmentsForTest(line string) []string {
	mode := shellModeOperational
	if strings.HasPrefix(line, requestModeTestPrefix) {
		mode = shellModeRequest
		line = strings.TrimPrefix(line, requestModeTestPrefix)
	} else if strings.HasPrefix(line, configureModeTestPrefix) {
		mode = shellModeConfigure
		line = strings.TrimPrefix(line, configureModeTestPrefix)
	}
	completer := shellReadlineCompleter{state: &shellState{mode: mode}}
	completions, _ := completer.Do([]rune(line), len([]rune(line)))
	return shellCompletionStrings(completions)
}

func shellCompletionStrings(completions [][]rune) []string {
	out := make([]string, 0, len(completions))
	for _, completion := range completions {
		out = append(out, strings.TrimSuffix(string(completion), " "))
	}
	return out
}

func TestParseShellRejectsLinuxShapeInShell(t *testing.T) {
	for _, line := range []string{
		"wifi status",
		"standalone run once",
		"monitor wifi",
		"ping 1.1.1.1",
		"traceroute 1.1.1.1",
		"path-mtu 1.1.1.1",
		"global-ip",
		"dns example.com",
		"http example.com",
		"download https://example.com",
	} {
		if _, err := parseShellLineForTest(line); err == nil {
			t.Fatalf("parseShellLineForTest(%q) error = nil", line)
		}
	}
	if _, err := parseShellLineForTest("devices"); err == nil {
		t.Fatalf("parseShellLineForTest(devices) error = nil")
	}
}

func TestParseLinuxCommands(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		label string
	}{
		{
			name:  "wifi connect flags",
			args:  []string{"request", "wifi", "connect", "Lab", "--passphrase", "secret", "--security", "wpa3", "--band", "6ghz"},
			label: "wifi connect Lab <redacted> wpa3 --band 6ghz",
		},
		{
			name:  "wifi scan fresh flags",
			args:  []string{"show", "wifi", "scan", "fresh", "--band", "5ghz", "--timeout", "9000"},
			label: "wifi scan fresh 5ghz --timeout 9000",
		},
		{
			name:  "ip status",
			args:  []string{"show", "ip", "status"},
			label: "ip",
		},
		{
			name:  "request> ping flags",
			args:  []string{"request", "ping", "1.1.1.1", "--count", "5", "--size", "64", "--timeout", "7000"},
			label: "ping 1.1.1.1 5 --size 64 --timeout 7000",
		},
		{
			name:  "path mtu flags",
			args:  []string{"request", "path-mtu", "example.test", "--min-mtu", "1200", "--max-mtu", "1500", "--timeout", "30000"},
			label: "path-mtu example.test --min-mtu 1200 --max-mtu 1500 --timeout 30000",
		},
		{
			name:  "global ip flags",
			args:  []string{"request", "global-ip", "--family", "ipv4", "--timeout", "7000"},
			label: "global-ip ipv4 --timeout 7000",
		},
		{
			name:  "dns flags",
			args:  []string{"request", "dns", "example.test", "--type", "AAAA", "--timeout", "9000"},
			label: "dns example.test AAAA --timeout 9000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := linuxcli.Parse(tt.args)
			if err != nil {
				t.Fatalf("linuxcli.Parse() error = %v", err)
			}
			if got.Kind != linuxcli.AgentCommand {
				t.Fatalf("kind = %v, want AgentCommand", got.Kind)
			}
			assertOperationLabel(t, got.Operation, tt.label)
		})
	}
}

func TestParseLinuxWifiWaitUsesDashSSID(t *testing.T) {
	got, err := linuxcli.Parse([]string{"request", "wifi", "wait", "connected", "--ssid", "Lab", "--ip", "--timeout", "12000"})
	if err != nil {
		t.Fatalf("linuxcli.Parse() error = %v", err)
	}
	cmd, _ := operationCommand(t, got.Operation)
	wait := cmd.GetWaitWifiConnected()
	if wait == nil {
		t.Fatalf("wait command = nil")
	}
	if wait.GetSsid() != "Lab" || !wait.GetRequireIp() || wait.GetTimeoutMs() != 12000 {
		t.Fatalf("wait command = %#v", wait)
	}
	assertOperationLabel(t, got.Operation, "wifi wait connected Lab --timeout 12000 --ip")
}

func TestParseLinuxWifiConnectRejectsExtraPositionalWithPassphraseFlag(t *testing.T) {
	_, err := linuxcli.Parse([]string{"request", "wifi", "connect", "Lab", "--passphrase", "secret", "extra"})
	if err == nil || !strings.Contains(err.Error(), "too many positional arguments for request wifi connect") {
		t.Fatalf("linuxcli.Parse() error = %v", err)
	}
}

func TestLinuxCommandBuildsOperation(t *testing.T) {
	got, err := linuxcli.Parse([]string{"request", "ping", "1.1.1.1", "--count", "5", "--size", "64"})
	if err != nil {
		t.Fatalf("linuxcli.Parse() error = %v", err)
	}
	if got.Operation.Name != "ping" {
		t.Fatalf("operation name = %q", got.Operation.Name)
	}
	cmd, _ := operationCommand(t, got.Operation)
	if ping := cmd.GetPing(); ping == nil || ping.GetHost() != "1.1.1.1" || ping.GetCount() != 5 || ping.GetSizeBytes() != 64 {
		t.Fatalf("ping command = %#v", cmd.GetPing())
	}
}

func TestExtractCLIOptionsAndTopLevel(t *testing.T) {
	global, rest, err := parseTopLevelArgs([]string{"--serial", "abc", "--listen", "127.0.0.1:37588", "--format", "json", "show", "devices"})
	if err != nil {
		t.Fatalf("parseTopLevelArgs() error = %v", err)
	}
	if global.Serial != "abc" {
		t.Fatalf("serial = %q", global.Serial)
	}
	if global.ListenAddr != "127.0.0.1:37588" {
		t.Fatalf("listen = %q", global.ListenAddr)
	}
	if !slices.Equal(rest, []string{"--format", "json", "show", "devices"}) {
		t.Fatalf("rest = %#v", rest)
	}

	opts, cliArgs, err := linuxcli.ExtractOptions(rest)
	if err != nil {
		t.Fatalf("linuxcli.ExtractOptions() error = %v", err)
	}
	if opts.Format != outputJSON {
		t.Fatalf("format = %q", opts.Format)
	}
	if !slices.Equal(cliArgs, []string{"show", "devices"}) {
		t.Fatalf("cliArgs = %#v", cliArgs)
	}
}
