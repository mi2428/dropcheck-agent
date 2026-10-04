package harness

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"dropcheck/controller/internal/adb"
	"dropcheck/controller/internal/control"
	"dropcheck/controller/internal/runner"
	"dropcheck/controller/internal/session"
)

// RunOptions configure the Go-test adapter, not scenario execution semantics.
type RunOptions struct {
	Context                                   context.Context
	ADBPath, Serial, PackageName, AgentTarget string
	Runner                                    OperationRunner
	Agent                                     control.AgentInfo
}
type RunOption func(*RunOptions)

func WithContext(ctx context.Context) RunOption { return func(o *RunOptions) { o.Context = ctx } }
func WithADBPath(path string) RunOption         { return func(o *RunOptions) { o.ADBPath = path } }
func WithSerial(serial string) RunOption        { return func(o *RunOptions) { o.Serial = serial } }
func WithPackageName(name string) RunOption     { return func(o *RunOptions) { o.PackageName = name } }
func WithAgentTarget(target string) RunOption   { return func(o *RunOptions) { o.AgentTarget = target } }
func WithRunner(r OperationRunner, agent control.AgentInfo) RunOption {
	return func(o *RunOptions) { o.Runner, o.Agent = r, agent }
}

// Run is a thin testing.T adapter. All validation, attempts, evaluation and
// cleanup happen in Compile/Execute; a Go-test failure never controls the engine.
func Run(t *testing.T, plan Plan, options ...RunOption) {
	t.Helper()
	adbPath := os.Getenv("ADB")
	if adbPath == "" {
		adbPath = "adb"
	}
	packageName := os.Getenv("DROPCHECK_PACKAGE")
	if packageName == "" {
		packageName = session.DefaultPackageName
	}
	cfg := RunOptions{Context: context.Background(), ADBPath: adbPath, Serial: os.Getenv("ADB_SERIAL"), PackageName: packageName, AgentTarget: os.Getenv("DROPCHECK_AGENT")}
	for _, option := range options {
		option(&cfg)
	}
	if cfg.Context == nil {
		cfg.Context = context.Background()
	}
	r, agents := cfg.Runner, []control.AgentInfo{cfg.Agent}
	if r == nil {
		// Static validation happens before even starting the transport/session.
		if err := Validate(plan); err != nil {
			t.Fatalf("plan: %v", err)
		}
		devices, err := discoverTargets(cfg.Context, adb.Client{Path: cfg.ADBPath}, cfg.Serial)
		if err != nil {
			t.Fatalf("discover targets: %v", err)
		}
		connection, err := session.Start(cfg.Context, session.Options{ADBPath: cfg.ADBPath, Serial: cfg.Serial, PackageName: cfg.PackageName}, devices)
		if err != nil {
			t.Fatalf("start session: %v", err)
		}
		defer connection.Close()
		agents = connection.Agents
		if cfg.AgentTarget != "" {
			agent, resolveErr := connection.Server.ResolveAgent(cfg.AgentTarget)
			if resolveErr != nil {
				t.Fatalf("selected agent: %v", resolveErr)
			}
			agents = []control.AgentInfo{agent}
		} else if len(agents) != 1 {
			t.Fatal("select an agent explicitly when multiple agents are connected")
		}
		r = runner.New(connection.Server)
	}
	compiled, err := Compile(plan, agents)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	report, err := Execute(cfg.Context, compiled, r, ExecuteOptions{Rounds: 1})
	if err != nil {
		t.Errorf("execution: %v", err)
	}
	for _, result := range report.Steps {
		if result.Outcome != OutcomePass && result.Outcome != SkipOutcome {
			t.Errorf("%s: %s: %s", result.Name, result.Outcome, result.Reason)
			for _, finding := range terminalFindings(result) {
				t.Errorf("after %d attempts: %s", len(result.Attempts), finding.failureString())
			}
		}
		for _, attempt := range result.Attempts {
			t.Logf("%s repeat=%d sample=%d attempt=%d outcome=%s", result.Name, attempt.Repeat, attempt.Sample, attempt.Number, attempt.Outcome)
		}
	}
	for _, problem := range report.Problems {
		t.Errorf("%s: %s", problem.Kind, problem.Message)
	}
}

func discoverTargets(ctx context.Context, client adb.Client, serial string) ([]adb.Device, error) {
	if serial != "" {
		return []adb.Device{{Serial: serial, State: "device"}}, nil
	}
	devices, err := client.ListDevices(ctx)
	if err != nil {
		return nil, err
	}
	var ready []adb.Device
	for _, device := range devices {
		if device.State == "device" {
			ready = append(ready, device)
		}
	}
	if len(ready) == 0 {
		return nil, fmt.Errorf("no connected devices")
	}
	return ready, nil
}

func durationMS(value time.Duration) string {
	if value == 0 {
		return ""
	}
	return strconv.FormatInt(value.Milliseconds(), 10)
}
