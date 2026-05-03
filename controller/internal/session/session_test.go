package session

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dropcheck/controller/internal/adb"
)

func TestStartRetriesADBReversePortCollisionOnEphemeralListen(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "adb.log")
	countPath := filepath.Join(tmp, "reverse.count")
	t.Setenv("ADB_LOG", logPath)
	t.Setenv("ADB_COUNT", countPath)
	path := fakeADB(t, `
printf '%s\n' "$*" >> "$ADB_LOG"
if [ "${1:-}" = "-s" ]; then
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
`)

	session, err := Start(context.Background(), Options{
		ADBPath: path,
		NoADB:   true,
	}, []adb.Device{{Serial: "R5CT12345", State: "device"}})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(session.Close)

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
		NoADB:      true,
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
