package shell

import (
	"strings"
	"testing"
)

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
	for _, line := range []string{"set cli default-passphrase synthetic-private-psk", "request wifi connect Lab passphrase synthetic-private-psk", "request http https://example.test/?token=secret", "help arbitrary-private-value", `show network | match private-value`, "show wifi status"} {
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
