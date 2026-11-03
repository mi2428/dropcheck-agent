package shell

import (
	"strings"
	"testing"
)

func TestHelpSeparatesCommandSyntaxFromDescriptions(t *testing.T) {
	for _, topic := range []string{"", "show version"} {
		var out strings.Builder
		WriteHelpTopic(&out, topic)
		if !strings.Contains(out.String(), "  show version\n    Description: Show local application version\n\n") {
			t.Fatalf("syntax and description are not separate records: %q", out.String())
		}
	}
	var out strings.Builder
	WriteHelpTopic(&out, "show devices")
	if !strings.Contains(out.String(), "  show devices\n    Description: (PC-only) List connected Controller agents\n\n") {
		t.Fatalf("platform label mixed into syntax: %q", out.String())
	}
	out.Reset()
	WriteContextHelp(&out, "show wifi status ?")
	for _, want := range []string{"  <Enter>       - Execute the requested subject\n", "  eht           - Show focused EHT/MLO connection, AP and device evidence\n"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("candidate description has no delimiter: %q", out.String())
		}
	}
	for row := range strings.SplitSeq(strings.TrimSuffix(out.String(), "\n"), "\n") {
		if strings.Index(row, "  - ") != len("  capabilities") {
			t.Fatalf("candidate description is not aligned: %q", row)
		}
	}
	out.Reset()
	WriteContextHelp(&out, "request wifi connect Lab passphrase ?")
	if out.String() != "  <value>\n" {
		t.Fatalf("value hint contains an invented description or supplied value: %q", out.String())
	}
}

func TestContextHelpNeverEchoesCredentials(t *testing.T) {
	for _, line := range []string{"sho?", "show ?", "show wifi scan | ?", "request wifi connect Lab passphrase synthetic-private-psk ?", "request wifi connect Lab passphrase synthetic-private-psk?"} {
		var b strings.Builder
		WriteContextHelp(&b, line)
		if b.Len() == 0 || strings.Contains(b.String(), "synthetic-private-psk") {
			t.Fatalf("unsafe or missing help: %s", b.String())
		}
	}
	for _, line := range []string{`request wifi connect 'literal?`, `request wifi connect literal\?`} {
		if IsHelpLine(line) {
			t.Errorf("literal became help: %s", line)
		}
	}
	for _, line := range []string{"set cli default-passphrase synthetic-private-psk", "request wifi connect Lab passphrase synthetic-private-psk", "request http https://example.test/?token=secret", "help arbitrary-private-value", `show network | match private-value`, "show wifi status", "show wifi status full"} {
		if HistorySafe(line) {
			t.Errorf("secret-capable input entered history: %s", line)
		}
	}
	if !HistorySafe("show network") {
		t.Fatal("read-only history unavailable")
	}
	if !strings.Contains(strings.Join(CompleteLine("show wifi scan "), ","), "show wifi scan ssid") {
		t.Fatal("explicit scan subject missing from completion")
	}
}

func TestSessionCommandsAndRemovedPipes(t *testing.T) {
	for _, line := range []string{"request wifi connect Lab", "request wifi connect Lab passphrase test-only", "set cli default-passphrase test-only", "clear cli default-passphrase"} {
		if _, err := ParseLine(line); err != nil {
			t.Fatal(line, err)
		}
	}
	for _, line := range []string{"use Lab test-only", "show network | no-more", "show network | display set", "request wifi disconnect | display json | count"} {
		if _, err := ParseLine(line); err == nil {
			t.Fatal("accepted", line)
		}
	}
}
