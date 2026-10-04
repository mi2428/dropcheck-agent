package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dropcheck/controller/internal/controlpb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

func TestCLICustomADBSessionAndSupplements(t *testing.T) {
	for _, tc := range []struct {
		name, supplement, invocation string
		args                         []string
	}{
		{"RA", "ADB IPv6 RA", "shell ip -6 route", []string{"show", "wifi", "status"}},
		{"MLO", "ADB MLO Snapshot", "shell cmd wifi status", []string{"show", "wifi", "eht"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts, logPath, counts := fakeCLISession(t, []string{"serial-test"})
			opts.Serial = "serial-test"
			out, err := captureStdout(t, func() error { return runCLI(context.Background(), opts, tc.args) })
			if err != nil || !strings.Contains(out, tc.supplement) {
				t.Fatalf("runCLI error = %v, supplement missing = %v", err, !strings.Contains(out, tc.supplement))
			}
			if got := <-counts; got.serial != "serial-test" || got.count != 1 {
				t.Fatalf("control operations = %v, want serial-test/1", got)
			}
			raw, err := os.ReadFile(logPath)
			if err != nil {
				t.Fatal(err)
			}
			log := string(raw)
			for _, want := range []string{"reverse tcp:", "session-start", tc.invocation, "reverse --remove"} {
				if !strings.Contains(log, want) {
					t.Fatalf("configured fake ADB did not receive %q", want)
				}
			}
			for _, line := range strings.Split(strings.TrimSpace(log), "\n") {
				if !strings.HasPrefix(line, opts.ADBPath+" serial-test ") {
					t.Fatal("session and supplement path/serial differ")
				}
			}
		})
	}
}

func TestCLIMultipleAgentsRequireExplicitTargetOrBroadcast(t *testing.T) {
	for _, tc := range []struct {
		name  string
		flags []string
		want  int
	}{
		{"ambiguous", nil, 0},
		{"target", []string{"--target", "serial-b"}, 1},
		{"broadcast", []string{"--all"}, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts, _, counts := fakeCLISession(t, []string{"serial-a", "serial-b"})
			args := append(append([]string(nil), tc.flags...), "request", "ping", "example.test", "--count", "1")
			_, err := captureStdout(t, func() error { return runCLI(context.Background(), opts, args) })
			if tc.want == 0 {
				if err == nil || !strings.Contains(err.Error(), "multiple") {
					t.Fatalf("ambiguous CLI error = %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			total := 0
			for range 2 {
				got := <-counts
				total += got.count
				if tc.name == "target" && got.serial == "serial-a" && got.count != 0 {
					t.Fatal("explicit target dispatched to serial-a")
				}
			}
			if total != tc.want {
				t.Fatalf("control operations = %d, want %d", total, tc.want)
			}
		})
	}
}

func TestCLIFreshEHTFailureUsesStrictOneShotPath(t *testing.T) {
	for _, format := range []string{"text", "json"} {
		t.Run(format, func(t *testing.T) {
			opts, _, counts := fakeCLISession(t, []string{"serial-test"})
			opts.Serial = "serial-test"
			out, err := captureStdout(t, func() error {
				return runCLI(context.Background(), opts, []string{"--format", format, "show", "wifi", "eht", "fresh"})
			})
			if err == nil || !strings.Contains(err.Error(), "FAILED") || !strings.Contains(out, "fresh scan incomplete") || !strings.Contains(out, "cached (refresh failed)") {
				t.Fatalf("strict one-shot error = %v, output = %s", err, out)
			}
			if got := <-counts; got.count != 2 {
				t.Fatalf("composite operations = %v, want 2", got)
			}
		})
	}
}

type fakeCLICommandCount struct {
	serial string
	count  int
}

// Only these fake executables are reachable; no installed adb or device is used.
func fakeCLISession(t *testing.T, serials []string) (shellOptions, string, <-chan fakeCLICommandCount) {
	t.Helper()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "adb.log")
	t.Setenv("CLI_ADB_DIR", dir)
	t.Setenv("CLI_ADB_LOG", logPath)
	t.Setenv("CLI_ADB_SERIALS", strings.Join(serials, " "))
	defaultADB := fakeADB(t, `exit 99`)
	t.Setenv("PATH", filepath.Dir(defaultADB))
	path := fakeADB(t, `
umask 077
serial=""
if [ "${1:-}" = "-s" ]; then serial="$2"; shift 2; fi
if [ "${1:-}" = "devices" ]; then
  printf 'List of devices attached\n'
  for item in $CLI_ADB_SERIALS; do printf '%s device\n' "$item"; done
  exit 0
fi
if [ "${1:-}" = "shell" ] && [ "${2:-}" = "am" ]; then
  port=""; token=""; prev=""
  for arg in "$@"; do
    if [ "$prev" = "grpc_port" ]; then port="$arg"; fi
    if [ "$prev" = "grpc_token" ]; then token="$arg"; fi
    prev="$arg"
  done
  printf '%s %s session-start\n' "$0" "$serial" >> "$CLI_ADB_LOG"
  printf '%s %s %s\n' "$port" "$token" "$serial" > "$CLI_ADB_DIR/$serial"
  exit 0
fi
printf '%s %s %s\n' "$0" "$serial" "$*" >> "$CLI_ADB_LOG"
case "$*" in
  'shell ip -6 route show table all') printf 'default via fe80::1 dev wlan0\n' ;;
  'shell cmd wifi status') printf 'Is TID-To-Link negotiation supported by the AP: true\n' ;;
esac
`)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	counts := make(chan fakeCLICommandCount, len(serials))
	done := make(chan error, len(serials))
	for _, serial := range serials {
		go func() {
			count := 0
			defer func() { counts <- fakeCLICommandCount{serial: serial, count: count} }()
			ticker := time.NewTicker(10 * time.Millisecond)
			defer ticker.Stop()
			var fields []string
			for len(fields) != 3 {
				raw, _ := os.ReadFile(filepath.Join(dir, serial))
				fields = strings.Fields(string(raw))
				if len(fields) == 3 {
					break
				}
				select {
				case <-ctx.Done():
					done <- ctx.Err()
					return
				case <-ticker.C:
				}
			}
			conn, err := grpc.NewClient("127.0.0.1:"+fields[0], grpc.WithTransportCredentials(insecure.NewCredentials()))
			if err != nil {
				done <- err
				return
			}
			defer func() { _ = conn.Close() }()
			stream, err := controlpb.NewDropcheckControlClient(conn).Session(ctx)
			if err != nil {
				done <- err
				return
			}
			err = stream.Send(&controlpb.AgentFrame{SessionId: "fake-" + serial, Body: &controlpb.AgentFrame_Hello{Hello: &controlpb.AgentHello{
				Token: fields[1], ControllerAgentId: serial, AdbSerial: serial,
			}}})
			for err == nil {
				var frame *controlpb.ControllerFrame
				frame, err = stream.Recv()
				if err != nil {
					break
				}
				cmd := frame.GetRunCommand()
				if cmd == nil {
					continue
				}
				count++
				result := &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_OK}
				switch {
				case cmd.GetGetFreshWifiScan() != nil:
					result.Status = controlpb.CommandResult_STATUS_FAILED
					result.Message = "fresh scan incomplete"
					result.Payload = &controlpb.CommandResult_WifiScan{WifiScan: &controlpb.WifiScan{Results: []*controlpb.WifiScanResult{{Ssid: "reference", WifiStandard: "802.11be"}}}}
				case cmd.GetGetWifiStatus() != nil:
					result.Payload = &controlpb.CommandResult_WifiStatus{WifiStatus: &controlpb.WifiStatus{IpStatus: &controlpb.IpStatus{InterfaceName: "wlan0"}}}
				case cmd.GetGetWifiDiagnostics() != nil:
					result.Payload = &controlpb.CommandResult_WifiDiagnostics{WifiDiagnostics: &controlpb.WifiDiagnostics{Status: &controlpb.WifiStatus{}}}
				case cmd.GetPing() != nil:
					result.Payload = &controlpb.CommandResult_Ping{Ping: &controlpb.PingResult{Host: "example.test"}}
				default:
					err = fmt.Errorf("unexpected fake control operation")
					continue
				}
				err = stream.Send(&controlpb.AgentFrame{CommandId: frame.GetCommandId(), Body: &controlpb.AgentFrame_Result{Result: result}})
			}
			// Session.Close stops the loopback server after every CLI call.
			if err != nil && !errors.Is(err, io.EOF) && status.Code(err) != codes.Unavailable && status.Code(err) != codes.Canceled {
				done <- err
			} else {
				done <- nil
			}
		}()
	}
	t.Cleanup(func() {
		cancel()
		for range serials {
			if err := <-done; err != nil {
				t.Errorf("fake session: %v", err)
			}
		}
	})
	return shellOptions{ADBPath: path}, logPath, counts
}
