package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dropcheck/controller/internal/controlpb"
)

func TestWifiSummaryUsesOneRPCAndNoADB(t *testing.T) {
	for _, line := range []string{"show wifi status", "show wifi status | display json"} {
		t.Run(line, func(t *testing.T) {
			state, stream, cleanup := connectedShellStateWithStream(t)
			defer cleanup()
			log := filepath.Join(t.TempDir(), "adb-calls")
			t.Setenv("QA_SUMMARY_ADB_LOG", log)
			state.adbPath = fakeADB(t, "printf 'called\\n' >> \"$QA_SUMMARY_ADB_LOG\"\nexit 99\n")
			sent := make(chan *controlpb.RunCommand, 1)
			go func() {
				for frame := range stream.sent {
					if cmd := frame.GetRunCommand(); cmd != nil {
						sent <- cmd
						stream.recv <- &controlpb.AgentFrame{CommandId: frame.GetCommandId(), Body: &controlpb.AgentFrame_Result{Result: &controlpb.CommandResult{
							Status: controlpb.CommandResult_STATUS_OK, Payload: &controlpb.CommandResult_WifiStatus{WifiStatus: &controlpb.WifiStatus{
								Connection: &controlpb.WifiConnection{Ssid: "summary-lab", ObservationFields: []*controlpb.DiagnosticField{{Key: "identity.state", Value: "available"}}},
								IpStatus:   &controlpb.IpStatus{InterfaceName: "wlan0", ObservationFields: []*controlpb.DiagnosticField{{Key: "link_properties.state", Value: "available"}}},
							}},
						}}}
						return
					}
				}
			}()
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			out, err := captureStdout(t, func() error { _, err := runReplLine(ctx, state, line); return err })
			if err != nil || !strings.Contains(out, "summary-lab") || !strings.Contains(out, "wlan0") || strings.Contains(out, "ADB") || strings.Count(out, "\n") > 320 {
				t.Fatalf("summary did not use the light result: %v %q", err, out)
			}
			select {
			case cmd := <-sent:
				if cmd.GetGetWifiStatus() == nil || len(stream.sent) != 0 {
					t.Fatal("summary dispatched diagnostics or additional RPCs")
				}
			default:
				t.Fatal("summary did not dispatch Wi-Fi status")
			}
			if _, err := os.Stat(log); !os.IsNotExist(err) {
				t.Fatalf("summary invoked ADB: %v", err)
			}
		})
	}
}

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
