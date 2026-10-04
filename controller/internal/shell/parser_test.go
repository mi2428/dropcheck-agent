package shell

import (
	"strings"
	"testing"
)

func TestShellPipelineQuotingAndGrammar(t *testing.T) {
	for _, line := range []string{"show wifi scan detail 'SSID|name' | count", `show wifi scan detail "SSID|name" | display json`, `show wifi scan detail "a\\\"b"`} {
		parsed, err := ParseLine(line)
		if err != nil || parsed.Kind != AgentCommand || !strings.Contains(parsed.Operation.Command.GetGetWifiScanDetail().GetTarget(), "SSID") && !strings.Contains(line, "a\\") {
			t.Fatal(line, err)
		}
	}
	for _, line := range []string{`show wifi scan detail "unfinished`, `show wifi scan detail one\`, `show ""`, "show wifi s", "request ping host", "wifi connect Lab passphrase 00000000 passphrase 00000000", "show wifi eht brief", "check"} {
		if _, err := ParseLine(line); err == nil {
			t.Errorf("accepted %q", line)
		}
	}
	if got := CompleteLine("show wifi scan "); !strings.Contains(strings.Join(got, ","), "fresh") {
		t.Fatalf("completion %v", got)
	}
	for _, tc := range []struct{ line, want string }{{"show ", "show wifi"}, {"show wifi ", "show wifi status"}, {"show wifi scan ", "show wifi scan fresh"}} {
		if got := CompleteLine(tc.line); !strings.Contains(strings.Join(got, ","), tc.want) {
			t.Fatalf("%q: %v", tc.line, got)
		}
	}
	if got := CompleteLine("show"); !strings.Contains(strings.Join(got, ","), "show ") {
		t.Fatalf("keyword completion must advance cursor: %v", got)
	}
	if got := CompleteLine("show wifi"); !strings.Contains(strings.Join(got, ","), "show wifi ") {
		t.Fatalf("nested keyword completion must advance cursor: %v", got)
	}
	var b strings.Builder
	WriteContextHelp(&b, "show wifi ?")
	if !strings.Contains(b.String(), "status") || !strings.Contains(b.String(), "scan") {
		t.Fatalf("context help: %s", b.String())
	}
	if got := CompleteLine("show devices | dis"); !strings.Contains(strings.Join(got, ","), "show devices | display") {
		t.Fatalf("pipe completion: %v", got)
	}
	if !IsHelpLine("show devices | ?") {
		t.Fatal("pipe help not recognized")
	}
	for _, line := range []string{"show adb wifi status", "show adb connectivity --diag", "show adb cmd wifi status"} {
		parsed, err := ParseLine(line)
		if err != nil || parsed.Kind != ADBDiagnostics || parsed.ADBDiagnosticsKind == "" {
			t.Fatalf("host ADB extension %q: %+v %v", line, parsed, err)
		}
	}
}
