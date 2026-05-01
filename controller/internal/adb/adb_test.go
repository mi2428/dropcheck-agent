package adb

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestListDevicesParsesLongOutput(t *testing.T) {
	path := fakeADB(t, `
if [ "$1" = "-s" ]; then
  echo "ListDevices should not pass a serial" >&2
  exit 9
fi
cat <<'OUT'
List of devices attached
emulator-5554 device product:sdk_gphone64_arm64 model:sdk_gphone64_arm64 device:emu64a transport_id:1
R5CT12345 offline usb:336592896X
malformed

OUT
`)

	devices, err := Client{Path: path, Serial: "ignored", Timeout: time.Second}.ListDevices(context.Background())
	if err != nil {
		t.Fatalf("ListDevices() error = %v", err)
	}
	if len(devices) != 2 {
		t.Fatalf("devices len = %d, want 2: %#v", len(devices), devices)
	}
	first := devices[0]
	if first.Serial != "emulator-5554" || first.State != "device" {
		t.Fatalf("first device = %#v", first)
	}
	if first.Details["product"] != "sdk_gphone64_arm64" || first.Details["model"] != "sdk_gphone64_arm64" || first.Details["transport_id"] != "1" {
		t.Fatalf("first details = %#v", first.Details)
	}
	if first.Raw != "emulator-5554 device product:sdk_gphone64_arm64 model:sdk_gphone64_arm64 device:emu64a transport_id:1" {
		t.Fatalf("first raw = %q", first.Raw)
	}
	if devices[1].Serial != "R5CT12345" || devices[1].State != "offline" || devices[1].Details["usb"] != "336592896X" {
		t.Fatalf("second device = %#v", devices[1])
	}
}

func TestClientOutputAddsSerialAndCombinesOutput(t *testing.T) {
	path := fakeADB(t, `
printf 'args:%s\n' "$*"
echo "stderr-line" >&2
`)

	out, err := Client{Path: path, Serial: "R5CT12345", Timeout: time.Second}.Output(context.Background(), "shell", "echo", "hello")
	if err != nil {
		t.Fatalf("Output() error = %v", err)
	}
	if !strings.Contains(out, "args:-s R5CT12345 shell echo hello") {
		t.Fatalf("Output() = %q, missing argv", out)
	}
	if !strings.Contains(out, "stderr-line") {
		t.Fatalf("Output() = %q, missing stderr", out)
	}
}

func TestClientOutputErrorIncludesADBCommandAndMessage(t *testing.T) {
	path := fakeADB(t, `
echo "permission denied" >&2
exit 7
`)

	out, err := Client{Path: path, Serial: "R5CT12345", Timeout: time.Second}.Output(context.Background(), "shell", "fail")
	if err == nil {
		t.Fatalf("Output() error = nil")
	}
	if strings.TrimSpace(out) != "permission denied" {
		t.Fatalf("Output() out = %q", out)
	}
	if got := err.Error(); !strings.Contains(got, "adb -s R5CT12345 shell fail: permission denied") {
		t.Fatalf("Output() error = %q", got)
	}
}

func TestClientSessionCommands(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "adb.log")
	t.Setenv("ADB_LOG", logPath)
	path := fakeADB(t, `
printf '%s\n' "$*" >> "$ADB_LOG"
`)

	client := Client{Path: path, Serial: "R5CT12345", Timeout: time.Second}
	ctx := context.Background()
	if err := client.Reverse(ctx, 43123); err != nil {
		t.Fatalf("Reverse() error = %v", err)
	}
	if err := client.RemoveReverse(ctx, 43123); err != nil {
		t.Fatalf("RemoveReverse() error = %v", err)
	}
	out, err := client.StartAgentSession(ctx, "io.dropcheck.agent", 43123, "token", "agent-1", "R5CT12345")
	if err != nil {
		t.Fatalf("StartAgentSession() error = %v out=%q", err, out)
	}

	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", logPath, err)
	}
	got := strings.Split(strings.TrimSpace(string(raw)), "\n")
	want := []string{
		"-s R5CT12345 reverse tcp:43123 tcp:43123",
		"-s R5CT12345 reverse --remove tcp:43123",
		"-s R5CT12345 shell am start-foreground-service -n io.dropcheck.agent/.AgentService -a io.dropcheck.agent.action.GRPC_SESSION --es grpc_host 127.0.0.1 --ei grpc_port 43123 --es grpc_token token --es agent_id agent-1 --es adb_serial R5CT12345",
	}
	if len(got) != len(want) {
		t.Fatalf("logged commands = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("logged command %d = %q, want %q", i, got[i], want[i])
		}
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
