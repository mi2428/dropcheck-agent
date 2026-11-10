package dropcheck_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// This maintained consumer really imports from outside the internal tree. Only
// fake processes and loopback agents are used, including successful acquisitions.
func TestExternalModuleBasicAPI(t *testing.T) {
	here, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Clean(filepath.Join(here, "../.."))
	dir := t.TempDir()
	module := fmt.Sprintf("module example.test/dropcheck-consumer\n\ngo 1.26.0\n\nrequire github.com/mi2428/dropcheck-agent/controller v0.0.0\n\nreplace github.com/mi2428/dropcheck-agent/controller => %q\n", root)
	script := `#!/bin/sh
set -eu
serial=""
if [ "${1:-}" = "-s" ]; then serial="$2"; shift 2; fi
if [ "$1" = devices ]; then printf 'fake-device device model:Fake\n'; exit 0; fi
if [ "$1" = reverse ]; then
  if [ "$2" = --remove ] && [ -e "$DROPCHECK_REMOVE_FAIL" ]; then echo 'synthetic cleanup failure' >&2; exit 9; fi
  exit 0
fi
if [ "$1" = shell ]; then
  port=""; token=""; previous=""
  for arg in "$@"; do
    if [ "$previous" = grpc_port ]; then port="$arg"; fi
    if [ "$previous" = grpc_token ]; then token="$arg"; fi
    previous="$arg"
  done
  printf '%s %s %s\n' "$port" "$token" "$serial" > "$DROPCHECK_LAUNCH.new"
  mv "$DROPCHECK_LAUNCH.new" "$DROPCHECK_LAUNCH"
  exit 0
fi
exit 10
`
	for name, text := range map[string]string{"go.mod": module, "api_test.go": externalConsumerSource, "fake-adb": script} {
		mode := os.FileMode(0o600)
		if name == "fake-adb" {
			mode = 0o700
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), mode); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	fixtureCtx, stopFixture := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { defer close(done); serveExternalFixture(fixtureCtx, dir) }()
	defer func() {
		stopFixture()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("fake agents did not stop")
		}
	}()
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, goTool, "test", "-mod=mod", "-p=1", "-count=1", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOPROXY=off", "GOSUMDB=off", "GOTOOLCHAIN=local",
		"DROPCHECK_FAKE_ADB="+filepath.Join(dir, "fake-adb"), "DROPCHECK_LAUNCH="+filepath.Join(dir, "launch"),
		"DROPCHECK_TRIGGER="+filepath.Join(dir, "replace"), "DROPCHECK_READY="+filepath.Join(dir, "ready"),
		"DROPCHECK_COMMANDS="+filepath.Join(dir, "commands"), "DROPCHECK_REMOVE_FAIL="+filepath.Join(dir, "remove-fail"))
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("external API check: %v\n%s", err, output)
	}
}

const externalConsumerSource = `package consumer
import (
    "context"
    "encoding/json"
    "errors"
    "os"
    "path/filepath"
    "reflect"
    "strings"
    "sync"
    "testing"
    "time"
    "github.com/mi2428/dropcheck-agent/controller/pkg/dropcheck"
)

func parts(value []dropcheck.Part) []string {
    var ids []string
    for _, part := range value { ids = append(ids, part.CommandID) }
    return ids
}
func findings(value []dropcheck.Finding) bool {
    for _, f := range value { if !f.Passed || f.Missing { return false } }
    return true
}
func ping(value *dropcheck.PingResult) bool { return value != nil && value.Received != nil && *value.Received == 0 }
func waitFile(t *testing.T, path string) {
    t.Helper()
    deadline := time.NewTimer(5*time.Second); defer deadline.Stop()
    ticker := time.NewTicker(5*time.Millisecond); defer ticker.Stop()
    for {
        if _, err := os.Stat(path); err == nil { return } else if !errors.Is(err, os.ErrNotExist) { t.Fatal(err) }
        select { case <-deadline.C: t.Fatal("fake marker timeout"); case <-ticker.C: }
    }
}
func write(t *testing.T, path string) { t.Helper(); if err := os.WriteFile(path, []byte("1"), 0600); err != nil { t.Fatal(err) } }
func TestPublicSurfaceAndPayloads(t *testing.T) {
    for _, typ := range []reflect.Type{reflect.TypeOf(dropcheck.Client{}), reflect.TypeOf(dropcheck.Operation{})} {
        for i := 0; i < typ.NumField(); i++ { if typ.Field(i).IsExported() { t.Fatalf("exported implementation member: %s", typ.Field(i).Name) } }
    }
    for _, name := range []string{"Hello", "Token", "Server", "Session"} {
        if _, ok := reflect.TypeOf(dropcheck.Agent{}).FieldByName(name); ok { t.Fatal("agent exposes transport credentials") }
    }
    zero := uint32(0)
    raw := &dropcheck.CommandResult{Status: dropcheck.StatusOK, Payload: &dropcheck.PingPayload{Ping: &dropcheck.PingResult{Received: &zero}}}
    var result dropcheck.Result = dropcheck.Result{Raw: raw, Parts: []dropcheck.Part{{Raw: raw}}, Findings: []dropcheck.Finding{{Passed: true}}}
    if !ping(result.Raw.GetPing()) || !findings(result.Findings) || len(parts(result.Parts)) != 1 { t.Fatal("nameable payload processing failed") }
    observedFalse := false
    trace := &dropcheck.CommandResult{Status: dropcheck.StatusOK, Payload: &dropcheck.TraceroutePayload{Traceroute: &dropcheck.TracerouteResult{ReachedTarget: &observedFalse}}}
    if trace.GetTraceroute().ReachedTarget == nil || trace.GetTraceroute().GetReachedTarget() { t.Fatal("known false lost") }
    _ = dropcheck.DNSAnswer{Type: dropcheck.DNSRecordA, Address: "192.0.2.1"}
    _ = dropcheck.GlobalIPAddress{Family: dropcheck.IPFamilyV4}
    if _, err := dropcheck.Ping(dropcheck.PingOptions{Host: "192.0.2.1", Count: 1, Timeout: time.Second}); err != nil { t.Fatal(err) }
    if _, err := dropcheck.IPStatus().WithNetwork(dropcheck.NetworkSelector{SSID: " 123 日本語 ", NetworkID: 42}); err != nil { t.Fatal(err) }
    if _, err := dropcheck.WiFiForgetSSID("123"); err != nil { t.Fatal(err) }
    if _, err := dropcheck.WiFiForgetConfigID(0); err != nil { t.Fatal(err) }
    if _, err := dropcheck.WiFiConnect(dropcheck.WiFiConnectOptions{SSID: "Lab", Passphrase: "short"}); err == nil { t.Fatal("invalid credential accepted") }
    var absent *dropcheck.Client
    if _, err := absent.Run(context.Background(), dropcheck.Agent{}, dropcheck.WiFiStatus()); !errors.Is(err, dropcheck.ErrClientNotOpen) { t.Fatal(err) }
    if err := absent.Close(); err != nil { t.Fatal(err) }
    ctx, cancel := context.WithCancel(context.Background()); cancel()
    if client, err := dropcheck.Open(ctx, dropcheck.Options{ADBPath: "must-not-execute"}); client != nil || !errors.Is(err, context.Canceled) { t.Fatal("pre-canceled Open") }
    if devices, err := dropcheck.Devices(ctx, "must-not-execute"); devices != nil || !errors.Is(err, context.Canceled) { t.Fatal("pre-canceled Devices") }
}

func TestFakeBackedLifecycle(t *testing.T) {
    ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second); defer cancel()
    var mu sync.Mutex
    var events []dropcheck.LogEvent
    client, err := dropcheck.Open(ctx, dropcheck.Options{Serial: "fake-device", ADBPath: os.Getenv("DROPCHECK_FAKE_ADB"), OnLog: func(event dropcheck.LogEvent) { mu.Lock(); events = append(events, event); mu.Unlock() }})
    if err != nil { t.Fatal(err) }
    defer func() { _ = client.Close() }()
    agents := client.Agents()
    if len(agents) != 1 { t.Fatal("no fake agent") }
    agent := agents[0]
    serialized, err := json.Marshal(agent); if err != nil { t.Fatal(err) }
    if strings.Contains(string(serialized), "token") || strings.Contains(string(serialized), "Hello") { t.Fatal("credential-bearing agent") }
    agent.Capabilities[0] = "wifi.forget.typed"
    agent.ID, agent.SessionID, agent.Device.Model = "edited", "edited", "edited"
    if got := client.Agents()[0]; got.ID != "fake-device" || got.Device.Model != "Fake" || got.Capabilities[0] != "wifi.status" { t.Fatal("snapshot mutation changed server state") }
    forget, _ := dropcheck.WiFiForgetConfigID(0)
    if _, err := client.Run(ctx, agent, forget); err == nil || !strings.Contains(err.Error(), "typed") { t.Fatal("forged capability authorized dispatch") }
    result, err := client.Run(ctx, agent, dropcheck.WiFiStatus())
    if err != nil || result.Raw.GetStatus() != dropcheck.StatusOK || result.Raw.GetWifiStatus().GetState() != "connected" || !findings(result.Findings) || len(result.Parts) != 1 || result.Parts[0].SessionID != agents[0].SessionID || result.Parts[0].CommandID == "" || result.Parts[0].Ended.Before(result.Parts[0].Started) { t.Fatalf("successful Run: %v %+v", err, result) }
    result.Raw.GetWifiStatus().State = "mutated"
    if result.Parts[0].Raw.GetWifiStatus().State != "connected" { t.Fatal("part payload aliases Raw") }
    result, err = client.Run(ctx, agent, dropcheck.WiFiStatus())
    if err != nil || result.Raw.GetWifiStatus().State != "connected" { t.Fatal("result mutation damaged later Run") }
    probe, _ := dropcheck.Ping(dropcheck.PingOptions{Host: "192.0.2.1"})
    result, err = client.Run(ctx, agent, probe)
    if err != nil || result.Raw.GetStatus() != dropcheck.StatusFailed { t.Fatal("agent failure confused with transport error") }
    trace, _ := dropcheck.Traceroute(dropcheck.TracerouteOptions{Host: "192.0.2.1"})
    result, err = client.Run(ctx, agent, trace)
    if err != nil || len(result.Findings) != 1 || !result.Findings[0].Missing { t.Fatal("missing successful payload not surfaced") }
    if _, err := client.Run(ctx, agent, dropcheck.Operation{}); err == nil { t.Fatal("zero operation dispatched") }
    mu.Lock()
    for _, level := range []dropcheck.LogLevel{dropcheck.LogDebug, dropcheck.LogInfo, dropcheck.LogWarn, dropcheck.LogError} {
        seen := false; for _, event := range events { if event.Level == level { seen = true } }; if !seen { t.Errorf("missing callback level %v", level) }
    }
    mu.Unlock()
    write(t, os.Getenv("DROPCHECK_TRIGGER")); waitFile(t, os.Getenv("DROPCHECK_READY"))
    if _, err := client.Run(ctx, agents[0], dropcheck.WiFiStatus()); err == nil { t.Fatal("stale snapshot silently repinned") }
    if _, err := client.Run(ctx, client.Agents()[0], forget); err != nil { t.Fatal(err) }
    // Close interrupts an in-flight acquisition and retains cleanup failures.
    blocking, _ := dropcheck.Download("https://example.test/never", time.Second)
    runDone := make(chan error, 1)
    go func() { _, err := client.Run(ctx, client.Agents()[0], blocking); runDone <- err }()
    waitFile(t, os.Getenv("DROPCHECK_COMMANDS")+".blocked")
    write(t, os.Getenv("DROPCHECK_REMOVE_FAIL"))
    closeErr := client.Close()
    if closeErr == nil || !strings.Contains(closeErr.Error(), "fake-device") || client.Close() != closeErr { t.Fatal("cleanup failure lost") }
    select { case err := <-runDone: if !errors.Is(err, dropcheck.ErrClientClosed) { t.Fatal(err) }; case <-time.After(3*time.Second): t.Fatal("Run survived Close") }
    if _, err := client.Run(ctx, agent, dropcheck.WiFiStatus()); !errors.Is(err, dropcheck.ErrClientClosed) { t.Fatal(err) }
    if len(client.Agents()) != 0 { t.Fatal("closed client exposes current agents") }
    if err := os.Remove(os.Getenv("DROPCHECK_REMOVE_FAIL")); err != nil { t.Fatal(err) }
    if err := os.Remove(os.Getenv("DROPCHECK_TRIGGER")); err != nil { t.Fatal(err) }
    second, err := dropcheck.Open(ctx, dropcheck.Options{Serial: "fake-device", ADBPath: os.Getenv("DROPCHECK_FAKE_ADB"), OnLog: func(dropcheck.LogEvent) {}})
    if err != nil { t.Fatal(err) }
    if _, err := second.Run(ctx, agents[0], dropcheck.WiFiStatus()); err == nil { t.Fatal("foreign snapshot accepted") }
    if err := second.Close(); err != nil { t.Fatal(err) }; if err := second.Close(); err != nil { t.Fatal(err) }
}

func TestMidFlightPublicCancellation(t *testing.T) {
    for _, inventory := range []bool{false, true} {
        dir := t.TempDir(); marker := filepath.Join(dir, "ready"); path := filepath.Join(dir, "adb")
        if err := os.WriteFile(path, []byte("#!/bin/sh\n: > '"+marker+"'\nexec sleep 60\n"), 0700); err != nil { t.Fatal(err) }
        ctx, cancel := context.WithCancel(context.Background())
        done := make(chan error, 1)
        go func() { if inventory { _, err := dropcheck.Devices(ctx, path); done <- err } else { _, err := dropcheck.Open(ctx, dropcheck.Options{ADBPath: path}); done <- err } }()
        waitFile(t, marker); cancel()
        select { case err := <-done: if !errors.Is(err, context.Canceled) { t.Fatal(err) }; case <-time.After(3*time.Second): t.Fatal("canceled process stuck") }
    }
}
`
