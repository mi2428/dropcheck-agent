package dropcheck

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dropcheck/controller/internal/adb"
	"dropcheck/controller/internal/command"
	"dropcheck/controller/internal/control"
	"dropcheck/controller/internal/session"
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
printf '%s\n' 'List of devices attached
R5CT11111 device product:one
R5CT22222 offline product:two
R5CT33333 unauthorized product:three
'
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
printf '%s\n' 'List of devices attached
R5CT22222 offline product:two
'
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

func TestTypedForgetDoesNotReinterpretNumericSSID(t *testing.T) {
	ssid := WiFiForgetSSID("123")
	if got := ssid.Command.GetForgetWifi(); got.GetSsid() != "123" || got.GetTarget() != "" || got.GetSelector() == nil {
		t.Fatal("numeric SSID became a configuration ID")
	}
	config := WiFiForgetConfigID(0)
	if got := config.Command.GetForgetWifi(); got.GetConfigId() != 0 || got.GetSelector() == nil || got.GetTarget() != "" {
		t.Fatal("known zero configuration ID was lost")
	}
	for _, op := range []Operation{ssid, config} {
		if err := command.ValidateOperation(op); err != nil {
			t.Fatal(err)
		}
	}
	for _, op := range []Operation{WiFiForgetSSID(""), WiFiForgetConfigID(-1)} {
		if command.ValidateOperation(op) == nil {
			t.Fatal("invalid typed forget selector accepted")
		}
	}
}

func TestDevicesListsInventoryWithoutStartingAgents(t *testing.T) {
	path := fakeADB(t, `
test "$#" = 2 && test "$1" = devices && test "$2" = -l
printf '%s\n' 'List of devices attached
ready device model:Phone
offline offline
untrusted unauthorized
'
`)
	devices, err := Devices(t.Context(), path)
	if err != nil || len(devices) != 3 || devices[0].Details["model"] != "Phone" || devices[1].State != "offline" || devices[2].State != "unauthorized" {
		t.Fatalf("inventory changed or started a session: devices=%v err=%v", devices, err)
	}
}

func TestClientUsesSharedValidationBeforeDispatch(t *testing.T) {
	client := &Client{Session: &session.Session{Server: control.NewServer("synthetic", nil)}}
	op, err := Traceroute(TracerouteOptions{Host: "192.0.2.1", MaxHops: "256"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Run(t.Context(), Agent{ID: "not-connected"}, op); err == nil || !strings.Contains(err.Error(), "255") {
		t.Fatalf("shared validation did not precede transport: %v", err)
	}
}
