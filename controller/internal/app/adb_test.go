package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dropcheck/controller/internal/adb"
)

func TestDiscoverADBTargetsUsesExplicitSerial(t *testing.T) {
	targets, err := discoverADBTargets(context.Background(), adb.Client{Path: filepath.Join(t.TempDir(), "missing-adb")}, "R5CT12345")
	if err != nil {
		t.Fatalf("discoverADBTargets() error = %v", err)
	}
	if len(targets) != 1 || targets[0].Serial != "R5CT12345" || targets[0].State != "device" {
		t.Fatalf("targets = %#v", targets)
	}
}

func TestDiscoverADBTargetsFiltersConnectedDevices(t *testing.T) {
	path := fakeADB(t, `
cat <<'OUT'
List of devices attached
R5CT11111 device product:one
R5CT22222 offline product:two
R5CT33333 unauthorized product:three

OUT
`)

	targets, err := discoverADBTargets(context.Background(), adb.Client{Path: path, Timeout: 5 * time.Second}, "")
	if err != nil {
		t.Fatalf("discoverADBTargets() error = %v", err)
	}
	if len(targets) != 1 || targets[0].Serial != "R5CT11111" || targets[0].State != "device" {
		t.Fatalf("targets = %#v", targets)
	}
}

func TestDiscoverADBTargetsRejectsEmptyConnectedSet(t *testing.T) {
	path := fakeADB(t, `
cat <<'OUT'
List of devices attached
R5CT22222 offline product:two

OUT
`)

	_, err := discoverADBTargets(context.Background(), adb.Client{Path: path, Timeout: 5 * time.Second}, "")
	if err == nil || !strings.Contains(err.Error(), "no connected adb devices") {
		t.Fatalf("discoverADBTargets() error = %v, want no connected devices", err)
	}
}

func fakeADB(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "adb")
	script := "#!/bin/sh\nset -eu\n" + strings.TrimLeft(body, "\n")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile(fake adb) error = %v", err)
	}
	return path
}
