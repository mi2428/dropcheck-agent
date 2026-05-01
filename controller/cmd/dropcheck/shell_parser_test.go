package main

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"dropcheck/controller/internal/controlpb"
)

func TestParseShellCommands(t *testing.T) {
	tests := []struct {
		name string
		line string
		kind shellCommandKind
		args []string
	}{
		{
			name: "show wifi status",
			line: "show wifi status",
			kind: shellAgentCommand,
			args: []string{"wifi", "status"},
		},
		{
			name: "show wifi scan fresh",
			line: "show wifi scan fresh 5ghz timeout 9000",
			kind: shellAgentCommand,
			args: []string{"wifi", "scan", "fresh", "5ghz", "--timeout", "9000"},
		},
		{
			name: "request wifi connect",
			line: "request wifi connect Lab passphrase secret security wpa3 bssid aa:bb:cc:dd:ee:ff band 6ghz mac-randomization non-persistent timeout 12345",
			kind: shellAgentCommand,
			args: []string{"wifi", "connect", "Lab", "secret", "wpa3", "--bssid", "aa:bb:cc:dd:ee:ff", "--band", "6ghz", "--mac-randomization", "non-persistent", "--timeout", "12345"},
		},
		{
			name: "request wifi cycle",
			line: "request wifi cycle Lab passphrase secret count 2 ping 1.1.1.1 http https://example.test forget pause 250",
			kind: shellAgentCommand,
			args: []string{"wifi", "cycle", "Lab", "secret", "--count", "2", "--ping", "1.1.1.1", "--http", "https://example.test", "--pause", "250", "--forget"},
		},
		{
			name: "monitor wifi",
			line: "monitor wifi duration 5000 interval 250",
			kind: shellAgentCommand,
			args: []string{"wifi", "monitor", "5000", "250"},
		},
		{
			name: "ping",
			line: "ping 1.1.1.1 count 5 size 64 timeout 7000",
			kind: shellAgentCommand,
			args: []string{"ping", "1.1.1.1", "5", "--size", "64", "--timeout", "7000"},
		},
		{
			name: "traceroute",
			line: "traceroute example.test max-hops 12 via 192.0.2.1 size 80 timeout 30000",
			kind: shellAgentCommand,
			args: []string{"traceroute", "example.test", "12", "--via", "192.0.2.1", "--size", "80", "--timeout", "30000"},
		},
		{
			name: "path mtu",
			line: "path-mtu example.test min-mtu 1200 max-mtu 1500 timeout 30000",
			kind: shellAgentCommand,
			args: []string{"path-mtu", "example.test", "--min-mtu", "1200", "--max-mtu", "1500", "--timeout", "30000"},
		},
		{
			name: "test dns",
			line: "test dns example.test type AAAA timeout 9000",
			kind: shellAgentCommand,
			args: []string{"dns", "example.test", "AAAA", "--timeout", "9000"},
		},
		{
			name: "set target all",
			line: "set target all",
			kind: shellSetTarget,
		},
		{
			name: "show devices",
			line: "show devices",
			kind: shellShowDevices,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseShellLine(tt.line)
			if err != nil {
				t.Fatalf("parseShellLine() error = %v", err)
			}
			if got.kind != tt.kind {
				t.Fatalf("kind = %v, want %v", got.kind, tt.kind)
			}
			if !slices.Equal(got.operation.legacyCommandArgs(), tt.args) {
				t.Fatalf("operation legacy args = %#v, want %#v", got.operation.legacyCommandArgs(), tt.args)
			}
		})
	}
}

func TestShellCommandBuildsOperation(t *testing.T) {
	got, err := parseShellLine("request wifi connect Lab passphrase secret security wpa3 band 6ghz timeout 12345")
	if err != nil {
		t.Fatalf("parseShellLine() error = %v", err)
	}
	if got.operation.Name != "wifi.connect" {
		t.Fatalf("operation name = %q", got.operation.Name)
	}
	for key, want := range map[string]string{
		"ssid":       "Lab",
		"passphrase": "secret",
		"security":   "wpa3",
		"band":       "6ghz",
		"timeout":    "12345",
	} {
		if got.operation.Args[key] != want {
			t.Fatalf("operation arg %s = %q, want %q", key, got.operation.Args[key], want)
		}
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
}

func TestParseShellPipeline(t *testing.T) {
	got, err := parseShellLine(`show wifi scan | match "Lab AP" | except guest | display json | count`)
	if err != nil {
		t.Fatalf("parseShellLine() error = %v", err)
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
}

func TestShellHelpAndCompletion(t *testing.T) {
	help := shellHelpEntries("show wifi ?")
	var tokens []string
	for _, entry := range help {
		tokens = append(tokens, entry.token)
	}
	for _, want := range []string{"status", "diagnostics", "scan", "capabilities"} {
		if !slices.Contains(tokens, want) {
			t.Fatalf("help tokens = %#v, missing %q", tokens, want)
		}
	}

	completions := completeShellLine("show wi", nil)
	if !slices.Contains(completions, "show wifi") {
		t.Fatalf("completions = %#v, missing show wifi", completions)
	}

	pipeCompletions := completeShellLine("show wifi status | dis", nil)
	if !slices.Contains(pipeCompletions, "show wifi status | display json") {
		t.Fatalf("pipe completions = %#v, missing display json", pipeCompletions)
	}

	if !isHelpLine("show wifi？") {
		t.Fatalf("full-width help suffix was not recognized")
	}
}

func TestShellHTTPHelpAndFlexibleArgs(t *testing.T) {
	help := shellHelpEntries("test http ?")
	var tokens []string
	for _, entry := range help {
		tokens = append(tokens, entry.token)
	}
	for _, want := range []string{"<url>", "expected-status", "timeout"} {
		if !slices.Contains(tokens, want) {
			t.Fatalf("test http help tokens = %#v, missing %q", tokens, want)
		}
	}

	help = shellHelpEntries("test http expected-status 301 http://www.wide.ad.jp ?")
	tokens = tokens[:0]
	for _, entry := range help {
		tokens = append(tokens, entry.token)
	}
	if slices.Contains(tokens, "expected-status") {
		t.Fatalf("terminal test http help tokens = %#v, unexpectedly included expected-status", tokens)
	}
	for _, want := range []string{"timeout", "<cr>", "| display json"} {
		if !slices.Contains(tokens, want) {
			t.Fatalf("terminal test http help tokens = %#v, missing %q", tokens, want)
		}
	}

	got, err := parseShellLine("test http expected-status 301 www.wide.ad.jp timeout 7000")
	if err != nil {
		t.Fatalf("parseShellLine(test http) error = %v", err)
	}
	if got.operation.Name != "http" {
		t.Fatalf("operation name = %q", got.operation.Name)
	}
	if got.operation.Args["url"] != "https://www.wide.ad.jp" || got.operation.Args["expected-status"] != "301" || got.operation.Args["timeout"] != "7000" {
		t.Fatalf("operation args = %#v", got.operation.Args)
	}
	cmd, _, err := buildRunCommand(got.operation)
	if err != nil {
		t.Fatalf("buildRunCommand(test http) error = %v", err)
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
	help := shellHelpEntries("test dns ?")
	var tokens []string
	for _, entry := range help {
		tokens = append(tokens, entry.token)
	}
	for _, want := range []string{"<name>", "type", "timeout"} {
		if !slices.Contains(tokens, want) {
			t.Fatalf("test dns help tokens = %#v, missing %q", tokens, want)
		}
	}

	help = shellHelpEntries("test dns type a wide.ad.jp ?")
	tokens = tokens[:0]
	for _, entry := range help {
		tokens = append(tokens, entry.token)
	}
	for _, unwanted := range []string{"<name>", "type"} {
		if slices.Contains(tokens, unwanted) {
			t.Fatalf("terminal test dns help tokens = %#v, unexpectedly included %q", tokens, unwanted)
		}
	}
	for _, want := range []string{"timeout", "<cr>", "| display json"} {
		if !slices.Contains(tokens, want) {
			t.Fatalf("terminal test dns help tokens = %#v, missing %q", tokens, want)
		}
	}

	tests := []struct {
		line string
	}{
		{line: "test dns type a wide.ad.jp timeout 7000"},
		{line: "test dns wide.ad.jp type A timeout 7000"},
		{line: "test dns wide.ad.jp a timeout 7000"},
	}
	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			got, err := parseShellLine(tt.line)
			if err != nil {
				t.Fatalf("parseShellLine(test dns) error = %v", err)
			}
			if got.operation.Name != "dns" {
				t.Fatalf("operation name = %q", got.operation.Name)
			}
			if got.operation.Args["name"] != "wide.ad.jp" || got.operation.Args["type"] != "A" || got.operation.Args["timeout"] != "7000" {
				t.Fatalf("operation args = %#v", got.operation.Args)
			}
			cmd, _, err := buildRunCommand(got.operation)
			if err != nil {
				t.Fatalf("buildRunCommand(test dns) error = %v", err)
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

func TestShellTerminalHelp(t *testing.T) {
	help := shellHelpEntries("show target ?")
	var tokens []string
	for _, entry := range help {
		tokens = append(tokens, entry.token)
	}
	for _, want := range []string{"<cr>", "| display json", "| match <regex>", "| except <regex>", "| count", "| no-more"} {
		if !slices.Contains(tokens, want) {
			t.Fatalf("terminal help tokens = %#v, missing %q", tokens, want)
		}
	}
	for _, unwanted := range []string{"show", "devices", "wifi"} {
		if slices.Contains(tokens, unwanted) {
			t.Fatalf("terminal help tokens = %#v, unexpectedly included %q", tokens, unwanted)
		}
	}

	help = shellHelpEntries("set target all ?")
	tokens = tokens[:0]
	for _, entry := range help {
		tokens = append(tokens, entry.token)
	}
	if !slices.Equal(tokens, []string{"<cr>"}) {
		t.Fatalf("set target terminal help tokens = %#v, want <cr> only", tokens)
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
	for _, want := range []string{"status", "diagnostics", "scan", "capabilities"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("help output = %q, missing %q", out.String(), want)
		}
	}

	line = []rune("request wifi？")
	out.Reset()
	newLine, _, ok = handleShellHelpKey(&out, line, len(line), '？')
	if !ok {
		t.Fatalf("handleShellHelpKey full-width ok = false")
	}
	if got := string(newLine); got != "request wifi" {
		t.Fatalf("full-width new line = %q, want %q", got, "request wifi")
	}
	if !strings.Contains(out.String(), "connect") {
		t.Fatalf("full-width help output = %q, missing connect", out.String())
	}
}

func TestShellReadlineCompleter(t *testing.T) {
	completer := shellReadlineCompleter{}
	completions, offset := completer.Do([]rune("show wi"), len([]rune("show wi")))
	if offset != len([]rune("wi")) {
		t.Fatalf("offset = %d, want %d", offset, len([]rune("wi")))
	}
	if len(completions) != 1 || string(completions[0]) != "fi" {
		t.Fatalf("completions = %#v, want fi", completions)
	}
}

func TestParseShellRejectsLinuxShapeInShell(t *testing.T) {
	if _, err := parseShellLine("wifi status"); err == nil {
		t.Fatalf("parseShellLine(wifi status) error = nil")
	}
	if _, err := parseShellLine("devices"); err == nil {
		t.Fatalf("parseShellLine(devices) error = nil")
	}
}

func TestParseLinuxCommands(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "wifi connect flags",
			args: []string{"wifi", "connect", "Lab", "--passphrase", "secret", "--security", "wpa3", "--band", "6ghz"},
			want: []string{"wifi", "connect", "Lab", "secret", "wpa3", "--band", "6ghz"},
		},
		{
			name: "wifi scan fresh flags",
			args: []string{"wifi", "scan", "fresh", "--band", "5ghz", "--timeout", "9000"},
			want: []string{"wifi", "scan", "fresh", "5ghz", "--timeout", "9000"},
		},
		{
			name: "ping flags",
			args: []string{"ping", "1.1.1.1", "--count", "5", "--size", "64", "--timeout", "7000"},
			want: []string{"ping", "1.1.1.1", "5", "--size", "64", "--timeout", "7000"},
		},
		{
			name: "path mtu flags",
			args: []string{"path-mtu", "example.test", "--min-mtu", "1200", "--max-mtu", "1500", "--timeout", "30000"},
			want: []string{"path-mtu", "example.test", "--min-mtu", "1200", "--max-mtu", "1500", "--timeout", "30000"},
		},
		{
			name: "dns flags",
			args: []string{"dns", "example.test", "--type", "AAAA", "--timeout", "9000"},
			want: []string{"dns", "example.test", "AAAA", "--timeout", "9000"},
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
			if !slices.Equal(got.operation.legacyCommandArgs(), tt.want) {
				t.Fatalf("operation legacy args = %#v, want %#v", got.operation.legacyCommandArgs(), tt.want)
			}
		})
	}
}

func TestLinuxCommandBuildsOperation(t *testing.T) {
	got, err := parseLinuxCommand([]string{"ping", "1.1.1.1", "--count", "5", "--size", "64"})
	if err != nil {
		t.Fatalf("parseLinuxCommand() error = %v", err)
	}
	if got.operation.Name != "ping" {
		t.Fatalf("operation name = %q", got.operation.Name)
	}
	if got.operation.Args["host"] != "1.1.1.1" || got.operation.Args["count"] != "5" || got.operation.Args["size"] != "64" {
		t.Fatalf("operation args = %#v", got.operation.Args)
	}
	cmd, _, err := buildRunCommand(got.operation)
	if err != nil {
		t.Fatalf("buildRunCommand() error = %v", err)
	}
	if ping := cmd.GetPing(); ping == nil || ping.GetHost() != "1.1.1.1" || ping.GetCount() != 5 || ping.GetSizeBytes() != 64 {
		t.Fatalf("ping command = %#v", cmd.GetPing())
	}
}

func TestExtractCLIOptionsAndTopLevel(t *testing.T) {
	global, rest, err := parseTopLevelArgs([]string{"--serial", "abc", "--format", "json", "devices"})
	if err != nil {
		t.Fatalf("parseTopLevelArgs() error = %v", err)
	}
	if global.serial != "abc" {
		t.Fatalf("serial = %q", global.serial)
	}
	if !slices.Equal(rest, []string{"--format", "json", "devices"}) {
		t.Fatalf("rest = %#v", rest)
	}

	opts, commandArgs, err := extractCLIOptions(rest)
	if err != nil {
		t.Fatalf("extractCLIOptions() error = %v", err)
	}
	if opts.format != outputJSON {
		t.Fatalf("format = %q", opts.format)
	}
	if !slices.Equal(commandArgs, []string{"devices"}) {
		t.Fatalf("commandArgs = %#v", commandArgs)
	}
}
