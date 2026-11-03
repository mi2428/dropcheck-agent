package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dropcheck/controller/internal/controlpb"
)

func TestJunosQARejectedAttemptReplacesHistoricalPass(t *testing.T) {
	for _, input := range []string{"te wi require-ip BAD", "te wi | trim 0", "te li ssid Lab family BAD", "test internet"} {
		state := &shellState{lastWifiTest: &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_OK, Payload: &controlpb.CommandResult_WifiAssert{WifiAssert: &controlpb.WifiAssertResult{Passed: true}}}}
		if _, err := runReplLine(context.Background(), state, input); err != nil {
			t.Fatal(err)
		}
		if state.lastWifiTest != nil || state.lastReport == nil || state.lastReport.Passed() {
			t.Fatal("stale PASS survived", input)
		}
	}
}

func TestPTYJunosRunningCancel(t *testing.T) {
	if os.Getenv("DROPCHECK_SHELL_PTY") != "1" {
		t.Skip("driven by tools/test_shell_history.py")
	}
	state, _, cleanup := connectedShellStateWithStream(t)
	defer cleanup()
	log := filepath.Join(t.TempDir(), "calls")
	state.adbPath = filepath.Join(t.TempDir(), "fake-adb")
	if err := os.WriteFile(state.adbPath, []byte("#!/bin/sh\nprintf 'started\\n' >> '"+log+"'\nexec sleep 30\n"), 0700); err != nil {
		t.Fatal(err)
	}
	fmt.Println("PTY_CANCEL_LOG=" + log)
	if err := replLineEditor(t.Context(), state); err != nil {
		t.Fatal(err)
	}
	calls, err := os.ReadFile(log)
	if err != nil || strings.Count(string(calls), "started") != 1 {
		t.Fatal("canceled collection continued", err)
	}
}
