package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mi2428/dropcheck-agent/controller/internal/controlpb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func TestTopLevelHelpOnlyAdvertisesShellAndWatch(t *testing.T) {
	help := renderTopLevelHelp()
	for _, want := range []string{"Usage:", "Global flags:", "--adb PATH", "--listen ADDR", "dropcheck [flags] shell", "dropcheck [flags] watch", "dropcheck --version"} {
		if !strings.Contains(help, want) {
			t.Fatalf("missing %q: %s", want, help)
		}
	}
	for _, removed := range []string{"dropcheck [flags] tui", "one-shot", "--format", "Common commands:", "show devices", "ping <host>", "\t"} {
		if strings.Contains(help, removed) {
			t.Fatalf("help advertises retired surface %q", removed)
		}
	}
}

func TestRemovedEntrypointsNeverStartADB(t *testing.T) {
	path := filepath.Join(t.TempDir(), "adb")
	marker := filepath.Join(t.TempDir(), "called")
	if err := os.WriteFile(path, []byte("#!/bin/sh\ntouch '"+marker+"'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"tui"}, {"tui", "missing.yml"}, {"show", "devices"}, {"ping", "example.test"}, {"--format", "json", "show", "wifi", "status"}, {"wifi", "connect", "Lab", "passphrase", "synthetic-private"}} {
		full := append([]string{"--adb", path}, args...)
		if err := runWithContext(context.Background(), full); err == nil || strings.Contains(err.Error(), "synthetic-private") {
			t.Fatalf("retired entrypoint accepted or disclosed a credential: %v", err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := runWithContext(ctx, []string{"--adb", path, "shell"}); err == nil {
		t.Fatal("canceled Shell entry started")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("invalid entry ran adb: %v", err)
	}
}

func renderTopLevelHelp() string {
	var b bytes.Buffer
	writeTopLevelHelp(&b)
	return b.String()
}

func TestParseTopLevelArgsRecognizesFlagPackageHelpForms(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"-help"}, {"-h"}} {
		_, _, err := parseTopLevelArgs(args)
		if !errors.Is(err, errHelpRequested) {
			t.Fatalf("parseTopLevelArgs(%#v) error = %v, want help", args, err)
		}
	}
}

func TestParseTopLevelArgsAcceptsSingleDashListen(t *testing.T) {
	global, rest, err := parseTopLevelArgs([]string{"-listen", "127.0.0.1:37588", "shell"})
	if err != nil || global.ListenAddr != "127.0.0.1:37588" || !slices.Equal(rest, []string{"shell"}) {
		t.Fatalf("global=%+v rest=%v err=%v", global, rest, err)
	}
}

func TestShellReturnsStartupSelectionAndCleanupErrors(t *testing.T) {
	launch := filepath.Join(t.TempDir(), "launch")
	t.Setenv("DROPCHECK_SHELL_TEST_LAUNCH", launch)
	path := fakeADB(t, `
if [ "$1" = -s ]; then shift 2; fi
if [ "$1" = reverse ]; then
  if [ "$2" = --remove ]; then echo 'synthetic shell cleanup failure' >&2; exit 9; fi
  exit 0
fi
port=""; token=""; previous=""
for arg in "$@"; do
  if [ "$previous" = grpc_port ]; then port="$arg"; fi
  if [ "$previous" = grpc_token ]; then token="$arg"; fi
  previous="$arg"
done
printf '%s %s\n' "$port" "$token" > "$DROPCHECK_SHELL_TEST_LAUNCH"
`)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		for {
			raw, err := os.ReadFile(launch)
			fields := strings.Fields(string(raw))
			if err == nil && len(fields) == 2 {
				conn, err := grpc.NewClient("127.0.0.1:"+fields[0], grpc.WithTransportCredentials(insecure.NewCredentials()))
				if err != nil {
					done <- err
					return
				}
				defer conn.Close()
				stream, err := controlpb.NewDropcheckControlClient(conn).Session(ctx)
				if err == nil {
					err = stream.Send(&controlpb.AgentFrame{SessionId: "shell-test", Body: &controlpb.AgentFrame_Hello{Hello: &controlpb.AgentHello{Token: fields[1], ControllerAgentId: "fake-device"}}})
				}
				done <- err
				<-ctx.Done()
				return
			}
			select {
			case <-ctx.Done():
				done <- ctx.Err()
				return
			case <-ticker.C:
			}
		}
	}()
	err := runShell(ctx, shellOptions{Serial: "fake-device", ADBPath: path}, []string{"--target", "missing"})
	if err == nil || !strings.Contains(err.Error(), "not connected") || !strings.Contains(err.Error(), "synthetic shell cleanup failure") {
		t.Fatalf("Shell lost selection/cleanup failure: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
