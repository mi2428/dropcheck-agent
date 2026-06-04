package standaloneseed

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dropcheck/controller/internal/command"
	"dropcheck/controller/internal/controlpb"
)

func TestLiveWatchEdits(t *testing.T) {
	t.Setenv("LIVE_PSK", "topsecret")
	current := writeWatchFile(t, "current.yml", `
version: 1
name: current
defaults:
  security: wpa3
  connect_timeout: 12s
targets:
  - name: cs1
    ssid: cs1
    mac_randomization: persistent
  - name: cs2
    ssid: cs2
    bssid: aa:bb:cc:dd:ee:ff
    passphrase_env: LIVE_PSK
    band: 6ghz
checks:
  - type: ping
    host: 1.1.1.1
`)
	legacy := writeWatchFile(t, "legacy.yml", `
version: 1
name: legacy
targets:
  - name: cs1-legacy
    ssid: cs1-legacy
checks:
  - type: dns
    query: example.com
`)

	edits, err := LiveWatchEdits([]string{current, legacy})
	if err != nil {
		t.Fatalf("LiveWatchEdits() error = %v", err)
	}
	if len(edits) == 0 {
		t.Fatalf("LiveWatchEdits() returned no edits")
	}
	if edits[0].Action != "delete" || strings.Join(edits[0].Path, "/") != "festa/live" {
		t.Fatalf("first edit = %#v, want delete festa/live", edits[0])
	}

	got := make(map[string]string)
	for _, edit := range edits[1:] {
		got[strings.Join(edit.Path, "/")] = edit.Value
	}
	if got["festa/live/wifi/cs1/match/essid"] != "cs1" {
		t.Fatalf("cs1 essid = %q", got["festa/live/wifi/cs1/match/essid"])
	}
	if got["festa/live/wifi/cs1/security"] != "wpa3" {
		t.Fatalf("cs1 security = %q", got["festa/live/wifi/cs1/security"])
	}
	if got["festa/live/wifi/cs1/mac_randomization"] != "persistent" {
		t.Fatalf("cs1 mac randomization = %q", got["festa/live/wifi/cs1/mac_randomization"])
	}
	if got["festa/live/wifi/cs1/timeout_ms"] != "12000" {
		t.Fatalf("cs1 timeout = %q", got["festa/live/wifi/cs1/timeout_ms"])
	}
	if got["festa/live/wifi/cs2/passphrase"] != "topsecret" {
		t.Fatalf("cs2 passphrase = %q", got["festa/live/wifi/cs2/passphrase"])
	}
	if got["festa/live/wifi/cs2/match/bssid"] != "aa:bb:cc:dd:ee:ff" {
		t.Fatalf("cs2 bssid = %q", got["festa/live/wifi/cs2/match/bssid"])
	}
	if got["festa/live/wifi/cs2/band"] != "6ghz" {
		t.Fatalf("cs2 band = %q", got["festa/live/wifi/cs2/band"])
	}
	if got["festa/live/wifi/cs1-legacy/match/essid"] != "cs1-legacy" {
		t.Fatalf("legacy essid = %q", got["festa/live/wifi/cs1-legacy/match/essid"])
	}
}

func TestLiveWatchEditsRejectsDuplicateNames(t *testing.T) {
	first := writeWatchFile(t, "first.yml", `
version: 1
name: first
targets:
  - name: cs1
    ssid: cs1
checks:
  - type: ping
    host: 1.1.1.1
`)
	second := writeWatchFile(t, "second.yml", `
version: 1
name: second
targets:
  - name: cs1
    ssid: cs1-duplicate
checks:
  - type: ping
    host: 1.1.1.1
`)

	_, err := LiveWatchEdits([]string{first, second})
	if err == nil || !strings.Contains(err.Error(), `duplicate live wifi name "cs1"`) {
		t.Fatalf("LiveWatchEdits() error = %v", err)
	}
}

func TestOperationFromSetArgsBuildsEditOperation(t *testing.T) {
	path := writeWatchFile(t, "seed.yml", `
version: 1
name: seed
targets:
  - name: cs1
    ssid: cs1
checks:
  - type: ping
    host: 1.1.1.1
`)

	op, matched, err := OperationFromSetArgs([]string{"live", "watch", path})
	if err != nil {
		t.Fatalf("OperationFromSetArgs() error = %v", err)
	}
	if !matched {
		t.Fatalf("OperationFromSetArgs() matched = false")
	}
	cmd, _, err := command.BuildRunCommand(op)
	if err != nil {
		t.Fatalf("BuildRunCommand() error = %v", err)
	}
	edit := cmd.GetEditStandaloneConfig()
	if edit == nil {
		t.Fatalf("command = %T, want EditStandaloneConfig", cmd.GetCommand())
	}
	edits := edit.GetEdits()
	if len(edits) != 2 {
		t.Fatalf("edits = %#v, want delete+set", edits)
	}
	if edits[0].GetAction() != controlpb.StandaloneEdit_ACTION_DELETE || strings.Join(edits[0].GetPath(), "/") != "festa/live" {
		t.Fatalf("delete edit = %#v", edits[0])
	}
	if strings.Join(edits[1].GetPath(), "/") != "festa/live/wifi/cs1/match/essid" || edits[1].GetValue() != "cs1" {
		t.Fatalf("seed edit = %#v", edits[1])
	}
}

func writeWatchFile(t *testing.T, name string, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(strings.TrimSpace(body)+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", path, err)
	}
	return path
}
