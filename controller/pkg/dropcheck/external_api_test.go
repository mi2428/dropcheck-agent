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

// Compile and exercise the API from a separate module outside the internal tree.
func TestExternalModuleBasicAPI(t *testing.T) {
	here, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Clean(filepath.Join(here, "../.."))
	dir := t.TempDir()
	module := fmt.Sprintf("module example.test/dropcheck-consumer\n\ngo 1.26.0\n\nrequire dropcheck/controller v0.0.0\n\nreplace dropcheck/controller => %q\n", root)
	source := `package consumer
import (
    "context"
    "errors"
    "testing"
    "dropcheck/controller/pkg/dropcheck"
)
func TestBasicOperations(t *testing.T) {
    ctx, cancel := context.WithCancel(context.Background())
    cancel()
    options := dropcheck.Options{Serial: "synthetic", ADBPath: "must-not-execute", OnLog: func(dropcheck.LogEvent) {}}
    if client, err := dropcheck.Open(ctx, options); !errors.Is(err, context.Canceled) || client != nil {
        t.Fatalf("canceled Open: client=%v err=%v", client, err)
    }
    if devices, err := dropcheck.Devices(ctx, "must-not-execute"); !errors.Is(err, context.Canceled) || devices != nil {
        t.Fatalf("canceled inventory: devices=%v err=%v", devices, err)
    }
    var _ []dropcheck.Device
    op := dropcheck.WiFiStatus()
    var client *dropcheck.Client
    result, err := client.Run(ctx, dropcheck.Agent{ID: "synthetic"}, op)
    if err == nil || result.Raw != nil { t.Fatal("unconnected execution accepted") }
    var _ dropcheck.Result = result
    selector := &dropcheck.NetworkSelector{Ssid: "literal: SSID", NetworkId: "42"}
    probe, err := dropcheck.Ping(dropcheck.PingOptions{Host: "192.0.2.1", Count: "1"})
    if err != nil { t.Fatal(err) }
    probe.Command.GetPing().Selector = selector
    if probe.Command.GetPing().GetSelector().GetNetworkId() != "42" { t.Fatal("typed selector unavailable") }
    raw := &dropcheck.CommandResult{Status: dropcheck.StatusCanceled}
    if raw.GetStatus() != dropcheck.StatusCanceled { t.Fatal("typed outcome unavailable") }
}
`
	for name, text := range map[string]string{"go.mod": module, "api_test.go": source} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, goTool, "test", "-mod=mod", "-p=1", "-count=1", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOPROXY=off", "GOSUMDB=off", "GOTOOLCHAIN=local")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("external API check: %v\n%s", err, output)
	}
}
