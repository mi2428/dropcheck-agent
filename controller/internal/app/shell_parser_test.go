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
			name:  "show wifi scan brief",
			line:  "show wifi scan brief 5ghz",
			kind:  shellAgentCommand,
			label: "wifi scan brief 5ghz",
		},
		{
			name:  "show wifi scan brief mlo",
			line:  "show wifi scan brief mlo 5ghz",
			kind:  shellAgentCommand,
			label: "wifi scan brief mlo 5ghz",
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

	run, err := parseShellLineForTest("config> run request ping 1.1.1.1 count 1")
	if err != nil {
		t.Fatalf("config run request ping: %v", err)
	}
	if run.kind != shellAgentCommand {
		t.Fatalf("config run request kind = %v", run.kind)
	}
	assertOperationLabel(t, run.operation, "ping 1.1.1.1 1")

}

func TestParseShellRejectsStandaloneCommands(t *testing.T) {
	for _, line := range []string{
		"show config", "show config standalone", "show standalone status",
		"show standalone runs", "show standalone run test-run",
		"clear standalone runs all", "sync standalone runs",
		"config> show", "config> set standalone enabled", "config> delete standalone",
		"config> set standalone live watch missing.yml", "request> standalone run once",
		"config> run show standalone status", "request standalone run once",
		"sho sta status", "config> s sta enabled", "request> sta run once",
	} {
		if _, err := parseShellLineForTest(line); err == nil {
			t.Fatalf("removed command accepted: %q", line)
		}
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

	help = shellHelpEntriesForTest("show wifi scan ?")
	tokens = tokens[:0]
	for _, entry := range help {
		tokens = append(tokens, entry.token)
	}
	for _, want := range []string{"brief", "fresh", "detail", "all", "2.4ghz", "5ghz", "6ghz", "60ghz", "<cr>"} {
		if !slices.Contains(tokens, want) {
			t.Fatalf("show wifi scan help tokens = %#v, missing %q", tokens, want)
		}
	}
	if slices.Contains(tokens, "mlo") {
		t.Fatalf("show wifi scan help tokens = %#v, unexpectedly included mlo", tokens)
	}

	help = shellHelpEntriesForTest("show wifi scan brief ?")
	tokens = tokens[:0]
	for _, entry := range help {
		tokens = append(tokens, entry.token)
	}
	for _, want := range []string{"mlo", "all", "2.4ghz", "5ghz", "6ghz", "60ghz", "<cr>"} {
		if !slices.Contains(tokens, want) {
			t.Fatalf("show wifi scan brief help tokens = %#v, missing %q", tokens, want)
		}
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
	displayCommandFragments := shellCompletionFragmentsForTest("show devices | di")
	if !slices.Equal(displayCommandFragments, []string{"splay"}) {
		t.Fatalf("display command fragments = %#v, want only splay", displayCommandFragments)
	}
	for _, unexpected := range []string{"splay json", "splay set"} {
		if slices.Contains(displayCommandFragments, unexpected) {
			t.Fatalf("display command fragments = %#v, unexpectedly included %q", displayCommandFragments, unexpected)
		}
	}
	displayValueFragments := shellCompletionFragmentsForTest("show devices | display ")
	for _, want := range []string{"json", "set"} {
		if !slices.Contains(displayValueFragments, want) {
			t.Fatalf("display value completions = %#v, missing %q", displayValueFragments, want)
		}
	}
	if slices.Contains(displayValueFragments, "standalone") {
		t.Fatalf("display value completions = %#v, unexpectedly included standalone", displayValueFragments)
	}
	help = shellHelpEntriesForTest("show devices | display ?")
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
	if !slices.Equal(tokens, []string{"show", "configure", "request", "help", "quit"}) {
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

	help = shellHelpEntriesForTest("config> run show devices ?")
	tokens = tokens[:0]
	for _, entry := range help {
		tokens = append(tokens, entry.token)
	}
	for _, want := range []string{"<cr>", "| display json", "| match <regex>", "| except <regex>", "| count", "| no-more"} {
		if !slices.Contains(tokens, want) {
			t.Fatalf("configure run terminal help tokens = %#v, missing %q", tokens, want)
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

	line = []rune("run request ping ?")
	out.Reset()
	newLine, _, ok = handleShellHelpKey(&out, line, len(line), '?', &shellState{mode: shellModeConfigure})
	if !ok {
		t.Fatalf("configure help key ok = false")
	}
	if got := string(newLine); got != "run request ping " {
		t.Fatalf("configure help new line = %q", got)
	}
	for _, want := range []string{"<host>", "count", "size", "timeout"} {
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
	line := []rune("run request ping cou")
	completions, offset = configureCompleter.Do(line, len(line))
	if offset != len([]rune("cou")) {
		t.Fatalf("configure completion offset = %d, want 3", offset)
	}
	if len(completions) != 1 || string(completions[0]) != "nt " {
		t.Fatalf("configure completion = %#v, want nt plus a space", completions)
	}
}

func TestShellReadlineCompletionsAreSingleTokens(t *testing.T) {
	lines := []string{
		"",
		"s",
		"show ",
		"show wifi ",
		"show wifi eht ",
		"show wifi scan ",
		"show adb ",
		"show adb dumpsys ",
		"request ",
		"request wi",
		"request wifi ",
		"request wifi connect ",
		"request wifi wait connected ",
		"request ping ",
		"request dns example.test ",
		"show devices | ",
		"show devices | di",
		"show devices | display ",
		"show devices | display j",
		"show devices | display s",
		"show devices | ma",
		"show devices | match ",
		"show devices | ex",
		"show devices | except ",
		"config> ",
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

func TestEHTHelpCompletionOnlyOffersAcceptedSyntax(t *testing.T) {
	for _, line := range []string{"show wifi eht ", "sh wi e ", "config> run show wifi eht ", "config> run sh wi e "} {
		t.Run(line, func(t *testing.T) {
			want := []string{"fresh", "ssid", "bssid"}
			if got := shellCompletionFragmentsForTest(line); !slices.Equal(got, want) {
				t.Fatalf("completion tokens = %v, want %v", got, want)
			}
			var help []string
			for _, entry := range shellHelpEntriesForTest(line + "?") {
				help = append(help, entry.token)
			}
			if !slices.Equal(help, want) {
				t.Fatalf("help tokens = %v, want %v", help, want)
			}
			for _, token := range want {
				candidate := line + token
				switch token {
				case "ssid":
					candidate += ` "  MiXeD | SSID\\Tail  "`
				case "bssid":
					candidate += " aa:bb:cc:dd:ee:ff"
				}
				parsed, err := parseShellLineForTest(candidate)
				if err != nil || parsed.operation.Command == nil || parsed.operation.Command.GetGetWifiDiagnostics() == nil {
					t.Fatalf("advertised completion did not parse: %v", err)
				}
				if token == "ssid" && parsed.operation.Options.WifiEHTSSID != `  MiXeD | SSID\Tail  ` {
					t.Fatal("quoted literal was normalized")
				}
			}
			if _, err := parseShellLineForTest(line + "brief"); err == nil {
				t.Fatal("rejected EHT brief became accepted")
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
			line: "show wifi scan ",
			want: []string{"brief", "fresh", "detail", "all", "2.4ghz", "5ghz", "6ghz", "60ghz"},
		},
		{
			line: "show wifi scan fresh ",
			want: []string{"brief", "all", "2.4ghz", "5ghz", "6ghz", "60ghz", "timeout"},
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
			line: "show wifi scan brief ",
			want: []string{"mlo", "all", "2.4ghz", "5ghz", "6ghz", "60ghz"},
		},
		{
			line: "show wifi scan fresh brief ",
			want: []string{"mlo", "all", "2.4ghz", "5ghz", "6ghz", "60ghz", "timeout"},
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

func TestRemovedStandaloneHelpAndCompletion(t *testing.T) {
	for _, line := range []string{"?", "show ?", "config> ?", "config> run ?", "request> ?", "show standalone ?", "config> set standalone ?", "request> standalone ?"} {
		for _, entry := range shellHelpEntriesForTest(line) {
			if strings.Contains(entry.token, "standalone") || entry.token == "clear" || entry.token == "sync" || entry.token == "set" || entry.token == "delete" || entry.token == "config" {
				t.Fatalf("help for %q advertises removed token %q", line, entry.token)
			}
		}
	}
	for _, line := range []string{"show sta", "show config ", "clear ", "sync ", "config> set ", "config> delete ", "request> sta", "config> run show sta"} {
		if got := completeShellLineForTest(line, nil); len(got) != 0 {
			t.Fatalf("removed command completion for %q: %v", line, got)
		}
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
			name:  "wifi scan brief flags",
			args:  []string{"show", "wifi", "scan", "fresh", "--brief", "--band", "5ghz", "--timeout", "9000"},
			label: "wifi scan fresh brief 5ghz --timeout 9000",
		},
		{
			name:  "wifi scan brief mlo flags",
			args:  []string{"show", "wifi", "scan", "fresh", "--brief", "--mlo", "--band", "5ghz", "--timeout", "9000"},
			label: "wifi scan fresh brief mlo 5ghz --timeout 9000",
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
