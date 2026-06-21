package harness

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

// Plan is a Dropcheck Harness scenario.
type Plan struct {
	// Name optionally wraps the whole plan in a named Go subtest.
	Name string
	// Networks are visited in order. Each network gets its own subtest.
	Networks []Network
	// Results are saved standalone measurement archives evaluated without a
	// connected Android agent. Results and Networks are mutually exclusive.
	Results []ResultSource
	// Checks run for every network after connect and wait-connected succeed.
	Checks []Check
}

// OperationRunner executes one operation against one agent.
type OperationRunner interface {
	Run(context.Context, control.AgentInfo, command.Operation) (runner.Result, error)
}

// RunOptions configures session startup and execution for Run.
type RunOptions struct {
	// Context is used for every Dropcheck Harness operation. Nil uses
	// context.Background.
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

// WithContext sets the context used for every Dropcheck Harness operation.
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
// Real Dropcheck Harness test files should normally be guarded with:
//
//	//go:build harness
//
// The legacy `festival` build tag is still accepted for existing test files.
func Run(t *testing.T, plan Plan, opts ...RunOption) {
	t.Helper()
	cfg := defaultRunConfig()
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.Context == nil {
		cfg.Context = context.Background()
	}
	if err := validatePlan(plan); err != nil {
		t.Fatalf("Dropcheck Harness plan: %v", err)
	}
	if len(plan.Results) > 0 {
		if plan.Name != "" {
			t.Run(testName(plan.Name), func(t *testing.T) {
				runResultPlan(t, cfg.Context, plan)
			})
			return
		}
		runResultPlan(t, cfg.Context, plan)
		return
	}
	opRunner, agent := cfg.Runner, cfg.Agent
	if opRunner == nil {
		controlSession := startSession(t, cfg)
		t.Cleanup(controlSession.Close)
		opRunner = runner.New(controlSession.Server)
		agent = selectAgent(t, controlSession.Server, controlSession.Agents, cfg.AgentTarget)
	}
	if plan.Name != "" {
		t.Run(testName(plan.Name), func(t *testing.T) {
			runPlan(t, cfg.Context, opRunner, agent, plan)
		})
		return
	}
	runPlan(t, cfg.Context, opRunner, agent, plan)
}

func validatePlan(plan Plan) error {
	switch {
	case len(plan.Networks) == 0 && len(plan.Results) == 0:
		return fmt.Errorf("must set networks or results")
	case len(plan.Networks) > 0 && len(plan.Results) > 0:
		return fmt.Errorf("networks and results cannot be used together")
	default:
		return nil
	}
}

func runPlan(t *testing.T, ctx context.Context, opRunner OperationRunner, agent control.AgentInfo, plan Plan) {
	t.Helper()
	for _, network := range plan.Networks {
		t.Run(testName(network.displayName()), func(t *testing.T) {
			runNetwork(t, ctx, opRunner, agent, network, plan.Checks)
		})
	}
}

func runResultPlan(t *testing.T, ctx context.Context, plan Plan) {
	t.Helper()
	for _, source := range plan.Results {
		sourceName := resultSourceName(source)
		t.Run(testName(sourceName), func(t *testing.T) {
			t.Helper()
			if source == nil {
				t.Fatalf("result source is nil")
			}
			targets, err := source.Targets()
			if err != nil {
				t.Fatalf("load result source: %v", err)
			}
			if len(targets) == 0 {
				t.Fatalf("result source has no targets")
			}
			for _, target := range targets {
				t.Run(testName(target.displayName()), func(t *testing.T) {
					runResultTarget(t, ctx, target, plan.Checks)
				})
			}
		})
	}
}

func runResultTarget(t *testing.T, ctx context.Context, target ResultTarget, planChecks []Check) {
	t.Helper()
	if !t.Run("connect", func(t *testing.T) {
		runRecordedRequiredStep(t, target, "connect")
	}) {
		return
	}
	if !t.Run("wait_connected", func(t *testing.T) {
		runRecordedRequiredStep(t, target, "wait_connected")
	}) {
		return
	}
	network := target.syntheticNetwork()
	opRunner := archiveRunner{target: target}
	agent := target.syntheticAgent()
	for _, check := range planChecks {
		t.Run(testName(check.Name()), func(t *testing.T) {
			runCheck(t, ctx, opRunner, agent, network, check)
		})
	}
}

func runRecordedRequiredStep(t *testing.T, target ResultTarget, name string) {
	t.Helper()
	step := target.stepNamed(name)
	if step == nil {
		t.Fatalf("%s missing from standalone result", name)
	}
	if step.GetError() != "" {
		t.Fatalf("%s error=%s", name, step.GetError())
	}
	result := step.GetResult()
	if result == nil {
		t.Fatalf("%s has no command result", name)
	}
	if result.GetStatus() != controlpb.CommandResult_STATUS_OK {
		t.Fatalf("%s status=%s message=%s", name, result.GetStatus(), result.GetMessage())
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
	repeat := normalizedRepeat(step.policy.repeat)
	if repeat == 1 {
		runCheckPass(t, ctx, opRunner, agent, network, step)
		return
	}
	for index := uint32(1); index <= repeat; index++ {
		index := index
		t.Run(fmt.Sprintf("repeat_%d", index), func(t *testing.T) {
			runCheckPass(t, ctx, opRunner, agent, network, step)
		})
	}
}

func runCheckPass(t *testing.T, ctx context.Context, opRunner OperationRunner, agent control.AgentInfo, network Network, step step) {
	t.Helper()
	if step.policy.stableFor > 0 {
		runStableCheck(t, ctx, opRunner, agent, network, step)
		return
	}
	reportFailures(t, runCheckWithRetry(ctx, opRunner, agent, network, step))
}

func runStableCheck(t *testing.T, ctx context.Context, opRunner OperationRunner, agent control.AgentInfo, network Network, step step) {
	t.Helper()
	deadline := time.Now().Add(step.policy.stableFor)
	interval := step.policy.stableInterval
	if interval <= 0 {
		interval = time.Second
	}
	for sample := uint32(1); ; sample++ {
		failures := runCheckWithRetry(ctx, opRunner, agent, network, step)
		if len(failures) > 0 {
			reportFailures(t, prefixFailures(fmt.Sprintf("stable sample %d", sample), failures))
			return
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return
		}
		if interval < remaining {
			remaining = interval
		}
		if err := sleepContext(ctx, remaining); err != nil {
			t.Errorf("stable wait canceled: %v", err)
			return
		}
	}
}

func runCheckWithRetry(ctx context.Context, opRunner OperationRunner, agent control.AgentInfo, network Network, step step) []string {
	attempts := normalizedRetryAttempts(step.policy.retryAttempts)
	var lastFailures []string
	for attempt := uint32(1); attempt <= attempts; attempt++ {
		failures := executeCheckAttempt(ctx, opRunner, agent, network, step)
		if len(failures) == 0 {
			return nil
		}
		lastFailures = failures
		if attempt == attempts {
			break
		}
		if step.policy.retryDelay > 0 {
			if err := sleepContext(ctx, step.policy.retryDelay); err != nil {
				return append(lastFailures, fmt.Sprintf("retry wait canceled: %v", err))
			}
		}
	}
	if attempts > 1 {
		return prefixFailures(fmt.Sprintf("after %d attempts", attempts), lastFailures)
	}
	return lastFailures
}

func executeCheckAttempt(ctx context.Context, opRunner OperationRunner, agent control.AgentInfo, network Network, step step) []string {
	result, err := opRunner.Run(ctx, agent, step.operation)
	if err != nil {
		return []string{fmt.Sprintf("run %s: %v", step.name, err)}
	}
	var failures []string
	if result.Result.GetStatus() != controlpb.CommandResult_STATUS_OK {
		failures = append(failures, fmt.Sprintf("command status=%s message=%s", result.Result.GetStatus(), result.Result.GetMessage()))
	}
	if len(step.expectations) == 0 {
		return failures
	}
	harnessResult := Result{
		Network: network,
		Check:   step.name,
		Run: RunResult{
			CommandID: result.CommandID,
			Raw:       result.Result,
		},
	}
	for _, expectation := range step.expectations {
		for _, finding := range expectation.Evaluate(harnessResult) {
			if finding.Check == "" {
				finding.Check = step.name
			}
			if !finding.Passed {
				failures = append(failures, finding.failureString())
			}
		}
	}
	return failures
}

func normalizedRepeat(count uint32) uint32 {
	if count == 0 {
		return 1
	}
	return count
}

func normalizedRetryAttempts(attempts uint32) uint32 {
	if attempts == 0 {
		return 1
	}
	return attempts
}

func sleepContext(ctx context.Context, duration time.Duration) error {
	if duration <= 0 {
		return nil
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func prefixFailures(prefix string, failures []string) []string {
	if prefix == "" || len(failures) == 0 {
		return failures
	}
	prefixed := make([]string, 0, len(failures))
	for _, failure := range failures {
		prefixed = append(prefixed, prefix+": "+failure)
	}
	return prefixed
}

func reportFailures(t *testing.T, failures []string) {
	t.Helper()
	for _, failure := range failures {
		t.Errorf("%s", failure)
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
