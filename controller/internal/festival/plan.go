package festival

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"dropcheck/controller/internal/adb"
	"dropcheck/controller/internal/command"
	"dropcheck/controller/internal/control"
	"dropcheck/controller/internal/controlpb"
	"dropcheck/controller/internal/runner"
	"dropcheck/controller/internal/session"
)

// Plan is a Wi-Fi festival scenario.
type Plan struct {
	// Name optionally wraps the whole plan in a named Go subtest.
	Name string
	// Networks are visited in order. Each network gets its own subtest.
	Networks []Network
	// Checks run for every network after connect and wait-connected succeed.
	Checks []Check
}

// OperationRunner executes one operation against one agent.
type OperationRunner interface {
	Run(context.Context, control.AgentInfo, command.Operation) (runner.Result, error)
}

// RunOptions configures session startup and execution for Run.
type RunOptions struct {
	// Context is used for every festival operation. Nil uses context.Background.
	Context context.Context
	// ADBPath is the adb executable path. Empty uses ADB or "adb".
	ADBPath string
	// Serial selects one adb device. Empty uses ADB_SERIAL or all connected
	// devices for session startup, then selects the first connected agent.
	Serial string
	// PackageName is the Android package containing .AgentService.
	PackageName string
	// AgentTarget selects a connected agent by ID, prefix, or adb serial.
	AgentTarget string
	// Runner injects a preconfigured operation runner for tests or custom
	// harnesses that already manage a control session.
	Runner OperationRunner
	// Agent is the agent used with Runner.
	Agent control.AgentInfo
}

// RunOption configures Run.
type RunOption func(*RunOptions)

// WithContext sets the context used for every festival operation.
func WithContext(ctx context.Context) RunOption {
	return func(opts *RunOptions) {
		opts.Context = ctx
	}
}

// WithADBPath sets the adb executable used when Run starts a session.
func WithADBPath(path string) RunOption {
	return func(opts *RunOptions) {
		opts.ADBPath = path
	}
}

// WithSerial selects one adb serial when Run starts a session.
func WithSerial(serial string) RunOption {
	return func(opts *RunOptions) {
		opts.Serial = serial
	}
}

// WithPackageName sets the Android package containing .AgentService.
func WithPackageName(packageName string) RunOption {
	return func(opts *RunOptions) {
		opts.PackageName = packageName
	}
}

// WithAgentTarget resolves a specific connected agent after session startup.
func WithAgentTarget(target string) RunOption {
	return func(opts *RunOptions) {
		opts.AgentTarget = target
	}
}

// WithRunner injects an operation runner and agent for unit tests or advanced
// harnesses that manage sessions themselves.
func WithRunner(opRunner OperationRunner, agent control.AgentInfo) RunOption {
	return func(opts *RunOptions) {
		opts.Runner = opRunner
		opts.Agent = agent
	}
}

// Run executes plan as Go subtests.
//
// Real festival test files should normally be guarded with:
//
//	//go:build festival
func Run(t *testing.T, plan Plan, opts ...RunOption) {
	t.Helper()
	cfg := defaultRunConfig()
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.Context == nil {
		cfg.Context = context.Background()
	}
	opRunner, agent := cfg.Runner, cfg.Agent
	if opRunner == nil {
		controlSession := startSession(t, cfg)
		t.Cleanup(controlSession.Close)
		opRunner = runner.New(controlSession.Server)
		agent = selectAgent(t, controlSession.Server, controlSession.Agents, cfg.AgentTarget)
	}
	if len(plan.Networks) == 0 {
		t.Fatalf("festival plan has no networks")
	}
	if plan.Name != "" {
		t.Run(testName(plan.Name), func(t *testing.T) {
			runPlan(t, cfg.Context, opRunner, agent, plan)
		})
		return
	}
	runPlan(t, cfg.Context, opRunner, agent, plan)
}

func runPlan(t *testing.T, ctx context.Context, opRunner OperationRunner, agent control.AgentInfo, plan Plan) {
	t.Helper()
	for _, network := range plan.Networks {
		t.Run(testName(network.displayName()), func(t *testing.T) {
			runNetwork(t, ctx, opRunner, agent, network, plan.Checks)
		})
	}
}

func defaultRunConfig() RunOptions {
	adbPath := os.Getenv("ADB")
	if adbPath == "" {
		adbPath = "adb"
	}
	packageName := os.Getenv("DROPCHECK_PACKAGE")
	if packageName == "" {
		packageName = session.DefaultPackageName
	}
	return RunOptions{
		ADBPath:     adbPath,
		Serial:      os.Getenv("ADB_SERIAL"),
		PackageName: packageName,
		AgentTarget: os.Getenv("DROPCHECK_AGENT"),
	}
}

func startSession(t *testing.T, cfg RunOptions) *session.Session {
	t.Helper()
	targets, err := discoverTargets(cfg.Context, adb.Client{Path: cfg.ADBPath}, cfg.Serial)
	if err != nil {
		t.Fatalf("discover adb targets: %v", err)
	}
	controlSession, err := session.Start(cfg.Context, session.Options{
		ADBPath:     cfg.ADBPath,
		Serial:      cfg.Serial,
		PackageName: cfg.PackageName,
	}, targets)
	if err != nil {
		t.Fatalf("start dropcheck session: %v", err)
	}
	return controlSession
}

func discoverTargets(ctx context.Context, client adb.Client, serial string) ([]adb.Device, error) {
	if serial != "" {
		return []adb.Device{{Serial: serial, State: "device"}}, nil
	}
	devices, err := client.ListDevices(ctx)
	if err != nil {
		return nil, err
	}
	var targets []adb.Device
	for _, device := range devices {
		if device.State == "device" {
			targets = append(targets, device)
		}
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("no connected adb devices; connect a device or pass ADB_SERIAL")
	}
	return targets, nil
}

func selectAgent(t *testing.T, server *control.Server, agents []control.AgentInfo, target string) control.AgentInfo {
	t.Helper()
	if target != "" {
		info, err := server.ResolveAgent(target)
		if err != nil {
			t.Fatalf("resolve agent %q: %v", target, err)
		}
		return info
	}
	if len(agents) == 0 {
		t.Fatalf("no Android agents connected")
	}
	return agents[0]
}

func runNetwork(t *testing.T, ctx context.Context, opRunner OperationRunner, agent control.AgentInfo, network Network, planChecks []Check) {
	t.Helper()
	connect := network.connectOperation(t)
	if network.disconnectAfter {
		t.Cleanup(func() {
			_, _ = opRunner.Run(context.Background(), agent, command.WifiDisconnectOperation())
		})
	}
	if network.forgetAfter {
		t.Cleanup(func() {
			target := network.ssid
			if target == "" {
				target = network.bssid
			}
			_, _ = opRunner.Run(context.Background(), agent, command.WifiForgetOperation(target))
		})
	}
	// Connection and post-connect wait are prerequisites for every check in the
	// network subtest. t.Run isolates their failure output, but the returned bool
	// is still needed to stop later checks from running on the wrong network.
	if !t.Run("connect", func(t *testing.T) {
		runRequiredOperation(t, ctx, opRunner, agent, connect)
	}) {
		return
	}
	if network.waitConnected {
		wait := network.waitOperation(t)
		if !t.Run("wait_connected", func(t *testing.T) {
			runRequiredOperation(t, ctx, opRunner, agent, wait)
		}) {
			return
		}
	}
	checks := append([]Check{}, planChecks...)
	checks = append(checks, network.checks...)
	for _, check := range checks {
		t.Run(testName(check.Name()), func(t *testing.T) {
			runCheck(t, ctx, opRunner, agent, network, check)
		})
	}
}

func runRequiredOperation(t *testing.T, ctx context.Context, opRunner OperationRunner, agent control.AgentInfo, op command.Operation) {
	t.Helper()
	result, err := opRunner.Run(ctx, agent, op)
	if err != nil {
		t.Fatalf("%s failed: %v", op.Name, err)
	}
	if result.Result.GetStatus() != controlpb.CommandResult_STATUS_OK {
		t.Fatalf("%s status=%s message=%s", op.Name, result.Result.GetStatus(), result.Result.GetMessage())
	}
}

func runCheck(t *testing.T, ctx context.Context, opRunner OperationRunner, agent control.AgentInfo, network Network, check Check) {
	t.Helper()
	step, err := check.build()
	if err != nil {
		t.Fatalf("build check: %v", err)
	}
	result, err := opRunner.Run(ctx, agent, step.operation)
	if err != nil {
		t.Fatalf("run %s: %v", step.name, err)
	}
	if result.Result.GetStatus() != controlpb.CommandResult_STATUS_OK {
		t.Errorf("command status=%s message=%s", result.Result.GetStatus(), result.Result.GetMessage())
	}
	if len(step.expectations) == 0 {
		return
	}
	festivalResult := Result{
		Network: network,
		Check:   step.name,
		Run: RunResult{
			CommandID: result.CommandID,
			Raw:       result.Result,
		},
	}
	for _, expectation := range step.expectations {
		for _, finding := range expectation.Evaluate(festivalResult) {
			if finding.Check == "" {
				finding.Check = step.name
			}
			if !finding.Passed {
				t.Errorf("%s", finding.failureString())
			}
		}
	}
}

func testName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unnamed"
	}
	replacer := strings.NewReplacer(" ", "_", "/", "_", "\\", "_", ":", "_")
	return replacer.Replace(value)
}

func durationMS(value time.Duration) string {
	if value <= 0 {
		return ""
	}
	return strconv.FormatInt(value.Milliseconds(), 10)
}
