package session

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dropcheck/controller/internal/adb"
	"dropcheck/controller/internal/controlpb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func TestStartRetriesADBReversePortCollisionOnEphemeralListen(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "adb.log")
	countPath := filepath.Join(tmp, "reverse.count")
	sessionPath := filepath.Join(tmp, "agent.session")
	t.Setenv("ADB_LOG", logPath)
	t.Setenv("ADB_COUNT", countPath)
	t.Setenv("ADB_SESSION", sessionPath)
	path := fakeADB(t, `
printf '%s\n' "$*" >> "$ADB_LOG"
serial=""
if [ "${1:-}" = "-s" ]; then
  serial="$2"
  shift 2
fi
if [ "${1:-}" = "reverse" ] && [ "${2:-}" != "--remove" ]; then
  count=0
  if [ -f "$ADB_COUNT" ]; then
    count=$(cat "$ADB_COUNT")
  fi
  count=$((count + 1))
  printf '%s\n' "$count" > "$ADB_COUNT"
  if [ "$count" -eq 1 ]; then
    echo "adb: error: cannot bind listener: Address already in use" >&2
    exit 1
  fi
fi
if [ "${1:-}" = "shell" ] && [ "${2:-}" = "am" ]; then
  port=""
  token=""
  prev=""
  for arg in "$@"; do
    if [ "$prev" = "grpc_port" ]; then port="$arg"; fi
    if [ "$prev" = "grpc_token" ]; then token="$arg"; fi
    prev="$arg"
  done
  printf '%s %s %s\n' "$port" "$token" "$serial" > "$ADB_SESSION"
fi
`)
	cancelAgent, agentErr := connectStartedAgent(t, sessionPath)
	defer cancelAgent()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	session, err := Start(ctx, Options{
		ADBPath: path,
	}, []adb.Device{{Serial: "R5CT12345", State: "device"}})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(session.Close)
	if err := <-agentErr; err != nil {
		t.Fatalf("agent connect error = %v", err)
	}

	lines := readADBLog(t, logPath)
	reverses := matchingLines(lines, " reverse tcp:")
	starts := matchingLines(lines, "shell am start-foreground-service")
	if len(reverses) != 2 {
		t.Fatalf("reverse commands = %#v, want 2 attempts", reverses)
	}
	if len(starts) != 1 {
		t.Fatalf("start commands = %#v, want 1", starts)
	}
}

func TestStartDoesNotRetryADBReverseCollisionOnFixedListenAddr(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "adb.log")
	t.Setenv("ADB_LOG", logPath)
	path := fakeADB(t, `
printf '%s\n' "$*" >> "$ADB_LOG"
if [ "${1:-}" = "-s" ]; then
  shift 2
fi
if [ "${1:-}" = "reverse" ] && [ "${2:-}" != "--remove" ]; then
  echo "adb: error: cannot bind listener: Address already in use" >&2
  exit 1
fi
`)

	session, err := Start(context.Background(), Options{
		ADBPath:    path,
		ListenAddr: freeTCPAddr(t),
	}, []adb.Device{{Serial: "R5CT12345", State: "device"}})
	if session != nil {
		t.Cleanup(session.Close)
	}
	if err == nil {
		t.Fatal("Start() error = nil, want adb reverse failure")
	}
	if !isADBReversePortCollision(err) {
		t.Fatalf("Start() error = %v, want reverse collision", err)
	}

	reverses := matchingLines(readADBLog(t, logPath), " reverse tcp:")
	if len(reverses) != 1 {
		t.Fatalf("reverse commands = %#v, want no retry for fixed port", reverses)
	}
}

func connectStartedAgent(t *testing.T, path string) (func(), <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		port, token, serial, err := waitAgentSessionFile(ctx, path)
		if err != nil {
			errCh <- err
			return
		}
		conn, err := grpc.NewClient("127.0.0.1:"+port, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			errCh <- err
			return
		}
		defer conn.Close()
		stream, err := controlpb.NewDropcheckControlClient(conn).Session(ctx)
		if err != nil {
			errCh <- err
			return
		}
		if err := stream.Send(&controlpb.AgentFrame{
			Seq:       1,
			SessionId: "test-session",
			Body: &controlpb.AgentFrame_Hello{
				Hello: &controlpb.AgentHello{
					Token:             token,
					ControllerAgentId: serial,
					AdbSerial:         serial,
				},
			},
		}); err != nil {
			errCh <- err
			return
		}
		errCh <- nil
		<-ctx.Done()
	}()
	return cancel, errCh
}

func waitAgentSessionFile(ctx context.Context, path string) (string, string, string, error) {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		raw, err := os.ReadFile(path)
		if err == nil {
			fields := strings.Fields(string(raw))
			if len(fields) >= 3 {
				return fields[0], fields[1], fields[2], nil
			}
		}
		select {
		case <-ctx.Done():
			return "", "", "", ctx.Err()
		case <-ticker.C:
		}
	}
}

func readADBLog(t *testing.T, path string) []string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
}

func matchingLines(lines []string, needle string) []string {
	var matches []string
	for _, line := range lines {
		if strings.Contains(line, needle) {
			matches = append(matches, line)
		}
	}
	return matches
}

func freeTCPAddr(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	return addr
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
