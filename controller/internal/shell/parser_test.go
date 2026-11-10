package shell

import (
	"strings"
	"testing"
)

func TestShellPipelineQuotingAndGrammar(t *testing.T) {
	for _, line := range []string{"show wifi scan ssid 'SSID|name' | count", `show wifi scan ssid "SSID|name" | display json`, `show wifi scan ssid "a\\\"b"`} {
		parsed, err := ParseLine(line)
		if err != nil || parsed.Kind != AgentCommand || parsed.Operation.Command.GetGetWifiScan() == nil || parsed.Operation.Options.WifiScanSSID == "" {
			t.Fatal(line, err)
		}
	}
	for _, line := range []string{`show wifi scan ssid "unfinished`, `show wifi scan ssid one\`, `show ""`, "show wifi s", "ping host", "request wifi connect Lab passphrase 00000000 passphrase 00000000", "show wifi status eht brief", "quit", "show adb wifi status", "show adb connectivity --diag", "show wifi status | begin x"} {
		if _, err := ParseLine(line); err == nil {
			t.Errorf("accepted %q", line)
		}
	}
	for _, tc := range []struct{ line, want string }{{"show ", "show wifi"}, {"show wifi ", "show wifi status"}, {"show wifi scan ", "show wifi scan ssid"}, {"show wifi status ", "show wifi status eht"}, {"show wifi status ", "show wifi status full"}, {"request wifi scan ", "request wifi scan eht"}, {"show", "show "}, {"show wifi", "show wifi "}, {"show devices | dis", "show devices | display"}, {"show devices | trim ", "<start-column>"}} {
		if got := CompleteLine(tc.line); !strings.Contains(strings.Join(got, ","), tc.want) {
			t.Fatalf("%q: %v", tc.line, got)
		}
	}
	var b strings.Builder
	WriteContextHelp(&b, "show wifi status ?")
	if !strings.Contains(b.String(), "<Enter>") || !strings.Contains(b.String(), "eht") {
		t.Fatalf("executable parent help: %s", b.String())
	}
	if !IsHelpLine("show devices | ?") {
		t.Fatal("pipe help not recognized")
	}
	for _, line := range []string{"debug adb", "debug adb wifi", "debug adb connectivity diagnostics", "debug adb connectivity traffic timeout 1000"} {
		parsed, err := ParseLine(line)
		if err != nil || parsed.Kind != ADBDiagnostics || parsed.ADBDiagnosticsKind == "" || parsed.ADBTimeoutMs == 0 {
			t.Fatalf("host ADB extension %q: %+v %v", line, parsed, err)
		}
	}
}

func TestProfileShellUsesSharedLiteralGrammar(t *testing.T) {
	for _, tc := range []struct {
		line         string
		kind         CommandKind
		ssid, family string
		reject       bool
	}{
		{`test link ssid "Case Sensitive"`, Profile, "Case Sensitive", "ipv4", false},
		{`test link ssid "Case Sensitive" family ipv6`, Profile, "Case Sensitive", "ipv6", false},
		{`test link ssid Lab bssid 02:00:00:00:00:11`, Profile, "", "", true},
		{`test`, Help, "", "", false},
		{`test link ssid "malformed`, Profile, "", "", true},
		{`show test profiles`, Profiles, "", "", false},
		{`show test last`, LastReport, "", "", false},
		{`test wifi`, WifiTest, "", "", false},
	} {
		parsed, err := ParseLine(tc.line)
		if err != nil || parsed.Kind != tc.kind || parsed.SSID != tc.ssid || parsed.Family != tc.family || (parsed.Rejection != "") != tc.reject {
			t.Errorf("%q: %+v %v", tc.line, parsed, err)
		}
	}
	parsed, err := ParseLine(`request wifi connect "Case Sensitive" passphrase synthetic-private-psk`)
	if err != nil || parsed.Kind != Connect || parsed.ConnectOptions == nil || parsed.ConnectOptions.SSID != "Case Sensitive" || parsed.ConnectOptions.Passphrase != "synthetic-private-psk" || parsed.Operation.Command != nil {
		t.Fatal("connect must resolve the session credential only at execution", parsed, err)
	}
}

func TestRequestSubjectsAreLocalHelpAndLeavesRemainDiscoverable(t *testing.T) {
	for _, group := range []string{"request ip", "request dns", "request http"} {
		parsed, err := ParseLine(group)
		if err != nil || parsed.Kind != Help || parsed.Operation.Command != nil || parsed.HelpTopic != group {
			t.Fatalf("group must not dispatch: %s %+v %v", group, parsed, err)
		}
	}
	for _, tc := range []struct{ prefix, want string }{
		{"request pm", "request pmtu"}, {"request ip ", "request ip external"},
		{"request dns ", "request dns resolve"}, {"request http ", "request http check"},
		{"request http ", "request http download"}, {"request http check ", "<url>"},
	} {
		if got := CompleteLine(tc.prefix); !strings.Contains(strings.Join(got, ","), tc.want) {
			t.Errorf("%s completion missing %s: %v", tc.prefix, tc.want, got)
		}
	}
	for _, line := range []string{"req pm example.test", "req ip ext family ipv4", "req dn res resolve", "req ht ch download", "req ht do https://example.test/check"} {
		parsed, err := ParseLine(line)
		if err != nil || parsed.Kind != AgentCommand || parsed.Operation.Command == nil {
			t.Errorf("unique prefix failed: %s %+v %v", line, parsed, err)
		}
	}
	var help strings.Builder
	WriteContextHelp(&help, "request http check https://example.test/?token=naming-secret ?")
	if strings.Contains(help.String(), "naming-secret") || !strings.Contains(help.String(), "timeout") {
		t.Fatalf("context help lost options or exposed a value: %s", help.String())
	}
}
