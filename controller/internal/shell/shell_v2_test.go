package shell

import (
	"strings"
	"testing"
)

func TestContextHelpNeverEchoesCredentials(t *testing.T) {
	for _, line := range []string{"sho?", "show ?", "show wifi scan | ?", "wifi connect Lab passphrase synthetic-private-psk ?", "wifi connect Lab passphrase synthetic-private-psk?"} {
		var b strings.Builder
		WriteContextHelp(&b, line)
		if b.Len() == 0 || strings.Contains(b.String(), "synthetic-private-psk") {
			t.Fatalf("unsafe or missing help: %s", b.String())
		}
	}
	for _, line := range []string{`wifi connect 'literal?`, `wifi connect literal\?`} {
		if IsHelpLine(line) {
			t.Errorf("literal became help: %s", line)
		}
	}
	for _, line := range []string{"set default passphrase synthetic-private-psk", "wifi connect Lab passphrase synthetic-private-psk", "use Lab", "http https://example.test/?token=secret", "help arbitrary-private-value", `show networks | match private-value`} {
		if HistorySafe(line) {
			t.Errorf("secret-capable input entered history: %s", line)
		}
	}
	if !HistorySafe("show networks") {
		t.Fatal("read-only history unavailable")
	}
	if !strings.Contains(strings.Join(CompleteLine("show wifi scan "), ","), "show wifi scan detail") {
		t.Fatal("nested scan detail missing from completion")
	}
}

func TestSessionCommandsAndRemovedPipes(t *testing.T) {
	for _, line := range []string{"use Lab", "use Lab passphrase test-only", "set default passphrase test-only", "clear default passphrase"} {
		if _, err := ParseLine(line); err != nil {
			t.Fatal(line, err)
		}
	}
	for _, line := range []string{"use Lab test-only", "show networks | no-more", "show networks | display set", "wifi disconnect | display json | count"} {
		if _, err := ParseLine(line); err == nil {
			t.Fatal("accepted", line)
		}
	}
}
