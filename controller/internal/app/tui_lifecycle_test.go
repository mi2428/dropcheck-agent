package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mi2428/dropcheck-agent/controller/internal/command"
	"github.com/mi2428/dropcheck-agent/controller/internal/control"
	"github.com/mi2428/dropcheck-agent/controller/internal/controlpb"
	"github.com/mi2428/dropcheck-agent/controller/internal/runner"
	"github.com/mi2428/dropcheck-agent/controller/internal/session"
	"github.com/mi2428/dropcheck-agent/controller/internal/tuiconfig"
	"github.com/mi2428/dropcheck-agent/controller/internal/tuirun"
)

func TestTUIPreflightRejectsBeforeADB(t *testing.T) {
	for name, config := range map[string]string{
		"observed active probe": "targets:\n- ssid: Lab\n  observe: true\nchecks:\n- type: ping\n  host: example.test\n",
		"invalid policy":        "targets:\n- ssid: Lab\n  passphrase: synthetic-passphrase\nchecks:\n- type: wifi_status\n  policy:\n    delay: -1ms\n",
		"invalid connection":    "targets:\n- ssid: Lab\n  passphrase: synthetic-passphrase\n  security: invalid-security\n",
	} {
		t.Run(name, func(t *testing.T) {
			plan, err := tuiconfig.Parse([]byte(config))
			if err != nil {
				t.Fatalf("fixture must reach runtime validation: %v", err)
			}
			if err := tuirun.Validate(plan); err == nil {
				t.Fatal("invalid fixture passed pure validation")
			}
			dir := t.TempDir()
			marker := filepath.Join(dir, "adb-called")
			fakeADB := filepath.Join(dir, "fake-adb")
			t.Setenv("DROPCHECK_PREFLIGHT_MARKER", marker)
			if err := os.WriteFile(fakeADB, []byte("#!/bin/sh\nprintf x >> \"$DROPCHECK_PREFLIGHT_MARKER\"\nexit 1\n"), 0o700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "tui.yml")
			if err := os.WriteFile(path, []byte(config), 0o600); err != nil {
				t.Fatal(err)
			}
			err = runTUI(context.Background(), session.Options{ADBPath: fakeADB}, []string{"-c", path, "--no-tui"})
			if err == nil || !strings.Contains(err.Error(), "validate tui config") {
				t.Fatalf("startup hid validation: %v", err)
			}
			if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("ADB was invoked: %v", err)
			}
		})
	}
}

type tuiTestRunner func(context.Context, control.AgentInfo, command.Operation) (runner.Result, error)

func (f tuiTestRunner) Run(ctx context.Context, agent control.AgentInfo, op command.Operation) (runner.Result, error) {
	return f(ctx, agent, op)
}

type tuiTestSink func(context.Context, tuirun.Event) error

func (f tuiTestSink) Emit(ctx context.Context, event tuirun.Event) error { return f(ctx, event) }

func TestTUIAbsentConsumerHasBoundedShutdownAndDurableFinalDelivery(t *testing.T) {
	for _, failDelivery := range []bool{false, true} {
		t.Run(map[bool]string{false: "durable", true: "delivery failure"}[failDelivery], func(t *testing.T) {
			const targets = 128
			plan := tuirun.Plan{}
			for range targets {
				plan.Networks = append(plan.Networks, tuirun.Network{SSID: "Lab", PSK: tuirun.SecretValue("synthetic-passphrase"), DisconnectAfter: true})
			}
			compiled, err := tuirun.Compile(plan, []control.AgentInfo{{ID: "agent"}})
			if err != nil {
				t.Fatal(err)
			}
			pipe := newTUIEventPipe(1)
			defer pipe.StopConsumer()
			blocked := make(chan struct{})
			failure := errors.New("synthetic terminal sink failure")
			observer := tuiTestSink(func(_ context.Context, event tuirun.Event) error {
				if event.Kind == tuirun.EventRoundStarted {
					close(blocked)
				}
				if failDelivery && event.Kind == tuirun.EventRunEnding {
					return failure
				}
				return nil
			})
			connects, cleanups := 0, 0
			r := tuiTestRunner(func(_ context.Context, _ control.AgentInfo, op command.Operation) (runner.Result, error) {
				raw := &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_OK}
				if op.Name == "wifi.connect" {
					connects++
					raw.Payload = &controlpb.CommandResult_ConnectWifi{ConnectWifi: &controlpb.ConnectWifiResult{Connected: true}}
				} else if op.Name == "wifi.disconnect" {
					cleanups++
					raw.Payload = &controlpb.CommandResult_WifiOperation{WifiOperation: &controlpb.WifiOperationResult{Ok: true}}
				}
				return runner.Result{Result: raw}, nil
			})
			var output bytes.Buffer
			done := make(chan error, 1)
			go func() {
				_, err := tuirun.Execute(context.Background(), compiled, r, tuirun.ExecuteOptions{Rounds: 1, Sinks: []tuirun.Sink{tuirun.NewJSONLWriter(&output), observer, tuirun.ChannelSink{C: pipe.C, Done: pipe.done}}})
				done <- err
			}()
			select {
			case <-blocked:
			case <-time.After(5 * time.Second):
				t.Fatal("producer did not fill the unread UI buffer")
			}
			// No UI read occurs, before or after detachment.
			pipe.StopConsumer()
			pipe.StopConsumer()
			select {
			case err := <-done:
				if failDelivery != errors.Is(err, failure) || !failDelivery && err != nil {
					t.Fatalf("delivery error was hidden: %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("unread UI accumulated per-event shutdown waits")
			}
			pipe.Close()
			if connects != targets || cleanups != targets || !strings.Contains(output.String(), `"kind":"run_ending"`) {
				t.Fatalf("cleanup or final JSONL lost: connects=%d cleanups=%d", connects, cleanups)
			}
		})
	}
}

func TestCanceledTUIDiagnosticsDoNotStartADB(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "called")
	path := filepath.Join(dir, "fake-adb")
	t.Setenv("DROPCHECK_DIAGNOSTIC_MARKER", marker)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf x >> \"$DROPCHECK_DIAGNOSTIC_MARKER\"\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := tuiOperationRunner{adbPath: path}
	agent := control.AgentInfo{Hello: &controlpb.AgentHello{AdbSerial: "synthetic-device"}}
	cause := tuirun.FailureCauseContext{Step: tuirun.StepSnapshot{Type: "connect"}}
	if r.FailureCause(ctx, agent, cause) != "" || r.ObserveFailureCause(ctx, agent, cause, func(string) { t.Error("late diagnostic") }) != nil {
		t.Fatal("canceled diagnostic hook started")
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("canceled hook invoked ADB: %v", err)
	}
}

func TestTUICanceledUnreadConsumerKeepsOwnedCleanupAndFinalJSONL(t *testing.T) {
	plan := tuirun.Plan{}
	for range 128 {
		plan.Networks = append(plan.Networks, tuirun.Network{SSID: "Lab", PSK: tuirun.SecretValue("synthetic-passphrase"), DisconnectAfter: true})
	}
	compiled, err := tuirun.Compile(plan, []control.AgentInfo{{ID: "agent"}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pipe := newTUIEventPipe(8)
	defer pipe.StopConsumer()
	blocked := make(chan struct{})
	connects, cleanups := 0, 0
	r := tuiTestRunner(func(_ context.Context, _ control.AgentInfo, op command.Operation) (runner.Result, error) {
		raw := &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_OK}
		if op.Name == "wifi.connect" {
			connects++
			raw.Payload = &controlpb.CommandResult_ConnectWifi{ConnectWifi: &controlpb.ConnectWifiResult{Connected: true}}
		} else {
			cleanups++
			raw.Payload = &controlpb.CommandResult_WifiOperation{WifiOperation: &controlpb.WifiOperationResult{Ok: true}}
		}
		return runner.Result{Result: raw}, nil
	})
	observer := tuiTestSink(func(_ context.Context, event tuirun.Event) error {
		if event.Kind == tuirun.EventTargetFinished && event.Target.ID == "target/0" {
			close(blocked)
		}
		return nil
	})
	var output bytes.Buffer
	done := make(chan error, 1)
	go func() {
		_, err := tuirun.Execute(ctx, compiled, r, tuirun.ExecuteOptions{Rounds: 1, Sinks: []tuirun.Sink{tuirun.NewJSONLWriter(&output), observer, tuirun.ChannelSink{C: pipe.C, Done: pipe.done}}})
		done <- err
	}()
	select {
	case <-blocked:
	case <-time.After(5 * time.Second):
		t.Fatal("producer did not reach its acquired target's full UI buffer")
	}
	cancel()
	pipe.StopConsumer()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("canceled target events accumulated UI delivery waits")
	}
	pipe.Close()
	if connects != 1 || cleanups != 1 || !strings.Contains(output.String(), `"kind":"run_ending"`) {
		t.Fatalf("cancel lost owned cleanup or final JSONL: connects=%d cleanups=%d", connects, cleanups)
	}
}
