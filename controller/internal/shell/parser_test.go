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
	for _, line := range []string{"show adb", "show adb wifi", "show adb connectivity diagnostics", "show adb connectivity traffic timeout 1000"} {
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
