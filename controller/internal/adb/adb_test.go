package adb

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
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
printf '%s\n' 'List of devices attached
emulator-5554 device product:sdk_gphone64_arm64 model:sdk_gphone64_arm64 device:emu64a transport_id:1
R5CT12345 offline usb:336592896X
malformed
'
`)

	devices, err := Client{Path: path, Serial: "ignored", Timeout: 5 * time.Second}.ListDevices(context.Background())
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

	out, err := Client{Path: path, Serial: "R5CT12345", Timeout: 5 * time.Second}.Output(context.Background(), "shell", "echo", "hello")
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

	out, err := Client{Path: path, Serial: "R5CT12345", Timeout: 5 * time.Second}.Output(context.Background(), "shell", "fail")
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

func TestClientRunSeparatesStreamsAndExitCode(t *testing.T) {
	path := fakeADB(t, `
printf 'stdout:%s\n' "$*"
echo "stderr-line" >&2
exit 3
`)

	result, err := Client{Path: path, Serial: "R5CT12345", Timeout: 5 * time.Second}.Run(context.Background(), "shell", "cmd", "wifi", "status")
	if err == nil {
		t.Fatalf("Run() error = nil")
	}
	if result.ExitCode != 3 {
		t.Fatalf("Run() exit code = %d, want 3", result.ExitCode)
	}
	if !strings.Contains(result.Stdout, "stdout:-s R5CT12345 shell cmd wifi status") {
		t.Fatalf("Run() stdout = %q", result.Stdout)
	}
	if strings.TrimSpace(result.Stderr) != "stderr-line" {
		t.Fatalf("Run() stderr = %q", result.Stderr)
	}
	if strings.Join(result.Args, " ") != "-s R5CT12345 shell cmd wifi status" {
		t.Fatalf("Run() args = %#v", result.Args)
	}
}

func TestClientSessionCommands(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "adb.log")
	t.Setenv("ADB_LOG", logPath)
	path := fakeADB(t, `
printf '%s\n' "$*" >> "$ADB_LOG"
`)

	client := Client{Path: path, Serial: "R5CT12345", Timeout: 5 * time.Second}
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

func TestStartAgentSessionRedactsTokenOnFailure(t *testing.T) {
	const token = "TEST_ONLY_TOKEN"
	for _, mode := range []string{"missing", "exit", "timeout", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "missing-adb")
			if mode != "missing" {
				body := "printf 'failure argv: %s\\n' \"$*\" >&2\nexit 7\n"
				if mode == "timeout" {
					body = "printf 'failure argv: %s\\n' \"$*\" >&2\nexec sleep 60\n"
				}
				path = fakeADB(t, body)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "canceled" {
				cancel()
			}
			timeout := 5 * time.Second
			if mode == "timeout" {
				timeout = 100 * time.Millisecond
			}
			out, err := (Client{Path: path, Timeout: timeout}).StartAgentSession(ctx, "test.agent", 43123, token, "agent-test", "serial-test")
			if err == nil {
				t.Fatal("startup unexpectedly succeeded")
			}
			if strings.Contains(out, token) || strings.Contains(err.Error(), token) {
				t.Fatal("startup output or error leaked synthetic token")
			}
			for _, want := range []string{"start-foreground-service", "--es grpc_token [REDACTED]", "--ei grpc_port 43123", "--es agent_id agent-test"} {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("error missing %q: %v", want, err)
				}
			}
			if mode == "exit" && !strings.Contains(out, "failure argv:") {
				t.Fatalf("echoed diagnostic lost: %q", out)
			}
		})
	}
}

func TestClientRunStopsAndReapsFakeProcess(t *testing.T) {
	for _, mode := range []string{"timeout", "cancel after start"} {
		t.Run(mode, func(t *testing.T) {
			ready := filepath.Join(t.TempDir(), "ready")
			t.Setenv("ADB_TEST_READY", ready)
			path := fakeADB(t, `
printf 'started\n'
: > "$ADB_TEST_READY"
exec sleep 60
`)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			timeout := 5 * time.Second
			if mode == "timeout" {
				timeout = 100 * time.Millisecond
			}
			type outcome struct {
				result Result
				err    error
			}
			done := make(chan outcome, 1)
			started := time.Now()
			go func() {
				result, err := (Client{Path: path, Timeout: timeout}).Run(ctx, "shell", "test")
				done <- outcome{result, err}
			}()
			if mode == "cancel after start" {
				startupCtx, stop := context.WithTimeout(ctx, timeout)
				defer stop()
				ticker := time.NewTicker(5 * time.Millisecond)
				defer ticker.Stop()
				for {
					if _, err := os.Stat(ready); err == nil {
						break
					} else if !errors.Is(err, os.ErrNotExist) {
						t.Fatal(err)
					}
					select {
					case <-ticker.C:
					case <-startupCtx.Done():
						cancel()
						select {
						case got := <-done:
							t.Fatalf("fake process did not start: elapsed=%v process=%v timedOut=%v exit=%d error=%v", time.Since(started), got.result.Elapsed, got.result.TimedOut, got.result.ExitCode, got.err)
						case <-time.After(time.Second):
							t.Fatal("fake process did not complete after startup cancellation")
						}
					}
				}
				cancel()
			}
			select {
			case got := <-done:
				want := context.DeadlineExceeded
				if mode == "cancel after start" {
					want = context.Canceled
					if !strings.Contains(got.result.Stdout, "started") {
						t.Fatal("cancellation did not exercise an already-started process")
					}
				}
				if !errors.Is(got.err, want) || !got.result.TimedOut || got.result.ExitCode != -1 {
					t.Fatalf("process=%v timedOut=%v exit=%d error=%v", got.result.Elapsed, got.result.TimedOut, got.result.ExitCode, got.err)
				}
				t.Logf("fake process completed and reaped: elapsed=%v timedOut=%v exit=%d", got.result.Elapsed, got.result.TimedOut, got.result.ExitCode)
			case <-time.After(time.Second):
				t.Fatal("fake process did not complete after context stop")
			}
		})
	}
}

func TestFakeADBLaunchDiagnostics(t *testing.T) {
	path := fakeADB(t, "printf 'started\\n'\n")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "shell", "test")
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	started := time.Now()
	if err := cmd.Start(); err != nil {
		t.Fatalf("fake launch: elapsed=%v context=%v error=%v", time.Since(started), ctx.Err(), err)
	}
	launchElapsed := time.Since(started)
	waitStarted := time.Now()
	err := cmd.Wait()
	waitElapsed := time.Since(waitStarted)
	t.Logf("fake launch=%v completion=%v marker=%v exit=%d context=%v", launchElapsed, waitElapsed, strings.Contains(output.String(), "started"), cmd.ProcessState.ExitCode(), ctx.Err())
	if err != nil || output.String() != "started\n" {
		t.Fatalf("fake completion: launch=%v completion=%v marker=%v context=%v error=%v", launchElapsed, waitElapsed, strings.Contains(output.String(), "started"), ctx.Err(), err)
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
