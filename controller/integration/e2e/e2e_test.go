//go:build e2e

// The tagged suite runs Dropcheck's real-device end-to-end matrix as Go tests.
//
// Parser and case-table consistency checks do not require a device:
//
//	cd controller
//	go test ./integration/e2e
//
// Full live execution requires an attached Android device and test Wi-Fi:
//
//	make e2e SERIAL=<adb-serial> SSID="<test-ssid>" PSK_ENV=DROPCHECK_E2E_WIFI_PSK
//
// make e2e runs go test -v -count=1. Each case prints its title, runner, command,
// result, elapsed time, per-case log path, and a short output tail.
//
// Useful environment variables:
//
//	DROPCHECK_E2E_FILTER       substring filter for case ID, title, runner, command, or assertion
//	DROPCHECK_E2E_LOG_DIR      persistent directory for per-case logs
//	DROPCHECK_E2E_BIN          prebuilt dropcheck binary to use instead of building a temp one
//	DROPCHECK_E2E_LAUNCH_APP   set to 0 to avoid bringing the Android activity to the foreground
//	DROPCHECK_E2E_LAUNCH_APP_EVERY_CASE
//	                           set to 0 to skip per-case foregrounding; defaults to 1
//	DROPCHECK_E2E_FORCE_STOP   set to 1 to force-stop the Android app before each live case
//
// The case table is testdata/e2e_cases.tsv. The title column is included in Go
// subtest names, for example E2E-001_shell_help, so verbose output remains readable.
// Commands intentionally use placeholders such as <ssid>, <psk>, <serial>,
// and <bssid> so lab secrets are not committed.
// Shell commands use the same flat grammar as one-shot argv.
package e2e

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	commandparse "dropcheck/controller/internal/command"
	f "dropcheck/controller/internal/harness"
	"dropcheck/controller/internal/harness/dns"
	"dropcheck/controller/internal/harness/ping"
)

const (
	envLive       = "DROPCHECK_E2E_LIVE"
	envSerial     = "DROPCHECK_E2E_SERIAL"
	envSSID       = "DROPCHECK_E2E_WIFI_SSID"
	envPSK        = "DROPCHECK_E2E_WIFI_PSK"
	envPSKName    = "DROPCHECK_E2E_WIFI_PSK_ENV"
	envBin        = "DROPCHECK_E2E_BIN"
	envFilter     = "DROPCHECK_E2E_FILTER"
	envLogDir     = "DROPCHECK_E2E_LOG_DIR"
	envADB        = "DROPCHECK_E2E_ADB"
	envPackage    = "DROPCHECK_E2E_PACKAGE"
	envForceStop  = "DROPCHECK_E2E_FORCE_STOP"
	envLaunchApp  = "DROPCHECK_E2E_LAUNCH_APP"
	envLaunchEach = "DROPCHECK_E2E_LAUNCH_APP_EVERY_CASE"
	defaultADB    = "adb"
	defaultPkg    = "io.dropcheck.agent"
	defaultPSKEnv = "DROPCHECK_E2E_WIFI_PSK"

	liveDNSName  = "example.com"
	livePingHost = "1.1.1.1"
	liveHTTPURL  = "http://connectivitycheck.gstatic.com/generate_204"
)

type e2eConfig struct {
	controllerRoot string
	repoRoot       string
	bin            string
	adb            string
	packageName    string
	logDir         string

	live               bool
	serial             string
	ssid               string
	psk                string
	bssid              string
	agentPref          string
	forceStopApp       bool
	launchAppActivity  bool
	launchAppEveryCase bool
}

func TestDropcheckEndToEndMatrix(t *testing.T) {
	cases := loadCases(t)
	cfg := loadConfig(t)
	if !cfg.live {
		t.Skipf("set %s=1 to run real-device E2E", envLive)
	}
	filter := os.Getenv(envFilter)
	selected := filteredCases(cases, filter)
	if cfg.live && hasLiveProcessCases(selected) {
		cfg.prepareLive(t, selected)
		t.Cleanup(func() {
			cfg.resetLiveState()
			if cfg.ssid != "" && cfg.psk != "" {
				cfg.restoreWiFiConnection()
			}
			cfg.launchApp(t, "suite cleanup", false)
		})
	}
	t.Logf("e2e cases=%d selected=%d live=%t logs=%s filter=%q serial=%q package=%q launch_app=%t launch_app_every_case=%t force_stop=%t", len(cases), len(selected), cfg.live, cfg.logDir, filter, cfg.serial, cfg.packageName, cfg.launchAppActivity, cfg.launchAppEveryCase, cfg.forceStopApp)

	for _, tc := range selected {
		if tc.Runner == "shell-parser" {
			continue // Executed once by the ordinary parser suite.
		}
		t.Run(tc.testName(), func(t *testing.T) {
			start := time.Now()
			commandLine, missing := cfg.expand(tc.Command)
			if missing != "" {
				t.Skipf("missing runtime value %s for %s", missing, tc.Command)
			}
			expect := tc.Expect
			t.Logf("START %s title=%q runner=%s expect=%s command=%s", tc.ID, tc.Title, tc.Runner, expect, redact(commandLine, cfg.psk))
			switch tc.Runner {
			case "shell", "cli":
				if cfg.serial == "" {
					t.Skipf("%s or ADB_SERIAL is required", envSerial)
				}
				if requiresWiFiSecret(commandLine) && (cfg.ssid == "" || cfg.psk == "") {
					t.Skipf("%s and %s are required for Wi-Fi live case", envSSID, envPSK)
				}
				if strings.Contains(commandLine, "<bssid>") {
					t.Skip("BSSID could not be resolved from the device")
				}
				cfg.prepareLiveCase(t, "case "+tc.ID)
				var res commandResult
				if tc.Runner == "shell" {
					res = cfg.runShellCase(tc, commandLine)
				} else {
					res = cfg.runCLICase(tc, commandLine)
				}
				logPath := cfg.writeLog(t, tc, commandLine, res)
				t.Logf("DONE %s rc=%d err=%v elapsed=%s log=%s output_tail=%q", tc.ID, res.Code, res.Err, time.Since(start).Round(time.Millisecond), logPath, outputTail(redact(res.Output, cfg.psk)))
				assertProcessResult(t, tc, expect, res)
				cfg.restoreAfterCase(tc, commandLine)
			default:
				t.Fatalf("unknown runner %q", tc.Runner)
			}
		})
	}
}

func TestHarnessLive(t *testing.T) {
	cfg := loadConfig(t)
	if !cfg.live {
		t.Skipf("set %s=1 to run live Dropcheck Harness E2E", envLive)
	}
	if cfg.serial == "" {
		t.Skipf("%s or ADB_SERIAL is required", envSerial)
	}
	if cfg.ssid == "" || cfg.psk == "" {
		t.Skipf("%s and %s are required for live Dropcheck Harness E2E", envSSID, envPSK)
	}

	f.Run(t, f.Plan{
		Name: "harness-live-e2e",
		Networks: []f.Network{
			f.WiFi("e2e-wifi").
				SSID(cfg.ssid).
				PSK(cfg.psk).
				Security("auto").
				ConnectTimeout(30 * time.Second).
				WaitTimeout(30 * time.Second).
				DisconnectAfter(false),
		},
		Checks: liveHarnessChecks(),
	}, f.WithADBPath(cfg.adb), f.WithSerial(cfg.serial), f.WithPackageName(cfg.packageName))
}

func liveHarnessChecks() []f.Check {
	return []f.Check{
		f.DNS(liveDNSName).
			A().
			Timeout(8*time.Second).
			Expect(dns.AnswerCount().Ge(1), dns.Elapsed().Le(8*time.Second)),
		f.Ping(livePingHost).
			Count(1).
			Timeout(8 * time.Second).
			Expect(ping.Assert("payload matches request", func(result ping.Result) error {
				if result.Host != livePingHost {
					return fmt.Errorf("host=%s want %s", result.Host, livePingHost)
				}
				if result.Count != 1 {
					return fmt.Errorf("count=%d want 1", result.Count)
				}
				return nil
			})),
		f.HTTP(liveHTTPURL).
			ExpectedStatus(204).
			Timeout(10 * time.Second).
			Expect(f.Assert("http matched", func(result f.Result) error {
				http := result.Run.Raw.GetHttpCheck()
				if http == nil {
					return fmt.Errorf("missing HTTP result payload")
				}
				if !http.GetMatched() {
					return fmt.Errorf("status=%d expected=%d error=%s", http.GetStatus(), http.GetExpectedStatus(), http.GetError())
				}
				return nil
			})),
	}
}

func loadConfig(t *testing.T) *e2eConfig {
	t.Helper()
	controllerRoot := findControllerRoot(t)
	logDir := os.Getenv(envLogDir)
	if logDir == "" {
		var err error
		logDir, err = os.MkdirTemp("", "dropcheck-e2e-logs-*")
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := &e2eConfig{
		controllerRoot:     controllerRoot,
		repoRoot:           filepath.Dir(controllerRoot),
		adb:                envOr(envADB, defaultADB),
		packageName:        envOr(envPackage, defaultPkg),
		logDir:             logDir,
		live:               envBool(envLive),
		serial:             firstNonEmpty(os.Getenv(envSerial), os.Getenv("ADB_SERIAL")),
		ssid:               os.Getenv(envSSID),
		forceStopApp:       envBool(envForceStop),
		launchAppActivity:  envBoolDefault(envLaunchApp, true),
		launchAppEveryCase: envBoolDefault(envLaunchEach, true),
	}
	pskEnv := firstNonEmpty(os.Getenv(envPSKName), defaultPSKEnv)
	cfg.psk = firstNonEmpty(os.Getenv(pskEnv), os.Getenv(envPSK))
	return cfg
}

func (cfg *e2eConfig) prepareLive(t *testing.T, cases []matrixCase) {
	t.Helper()
	if cfg.serial == "" {
		t.Logf("%s not set; live cases will be skipped", envSerial)
		return
	}
	cfg.bin = os.Getenv(envBin)
	if cfg.bin == "" {
		cfg.bin = filepath.Join(t.TempDir(), "dropcheck")
		t.Logf("building dropcheck binary: %s", cfg.bin)
		cmd := exec.Command("go", "build", "-o", cfg.bin, "./cmd/dropcheck")
		cmd.Dir = cfg.controllerRoot
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("build dropcheck: %v\n%s", err, out)
		}
	} else {
		t.Logf("using dropcheck binary: %s", cfg.bin)
	}
	cfg.resetLiveState()
	needsAgentPrefix, needsWiFiSetup, needsBSSID := liveSetupNeeds(cases)
	t.Logf("live setup needs: agent_prefix=%t wifi_setup=%t bssid=%t", needsAgentPrefix, needsWiFiSetup, needsBSSID)
	cfg.launchApp(t, "suite start", true)
	if needsAgentPrefix {
		cfg.agentPref = cfg.resolveAgentPrefix()
		t.Logf("resolved agent prefix: %s", cfg.agentPref)
		cfg.launchApp(t, "after agent discovery", true)
	}
	if needsWiFiSetup && cfg.ssid != "" && cfg.psk != "" {
		if needsBSSID {
			cfg.bssid = cfg.resolveBSSID()
			t.Logf("resolved bssid: %s", cfg.bssid)
			cfg.launchApp(t, "after bssid discovery", true)
		} else {
			t.Logf("ensuring Wi-Fi connection to test SSID")
			cfg.restoreWiFiConnection()
			cfg.launchApp(t, "after wifi setup", true)
		}
	}
}

func (cfg *e2eConfig) prepareLiveCase(t *testing.T, reason string) {
	t.Helper()
	if cfg.forceStopApp {
		cfg.forceStop()
		cfg.launchApp(t, reason, true)
		return
	}
	if cfg.launchAppEveryCase {
		cfg.launchApp(t, reason, true)
	}
}

func (cfg *e2eConfig) runShellCase(tc matrixCase, commandLine string) commandResult {
	input := shellInput(commandLine)
	if commandLine != "quit" && commandLine != "exit" {
		input += "quit\n"
	}
	return cfg.runExternal(timeoutFor(tc), []string{"--serial", cfg.serial, "shell"}, input)
}

func shellInput(commandLine string) string {
	return commandLine + "\n"
}

func (cfg *e2eConfig) runCLICase(tc matrixCase, commandLine string) commandResult {
	args, err := commandparse.SplitArgs(commandLine)
	if err != nil {
		return commandResult{Output: err.Error(), Code: 1, Err: err}
	}
	if len(args) > 0 && args[0] == "dropcheck" {
		args = args[1:]
	}
	if !hasSerialArg(args) {
		args = append([]string{"--serial", cfg.serial}, args...)
	}
	return cfg.runExternal(timeoutFor(tc), args, "")
}

func (cfg *e2eConfig) runExternal(timeout time.Duration, args []string, stdin string) commandResult {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, cfg.bin, args...)
	cmd.Dir = cfg.repoRoot
	cmd.Env = os.Environ()
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		code = 1
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.ExitCode()
		}
		if ctx.Err() != nil {
			err = ctx.Err()
		}
	}
	return commandResult{Output: string(out), Code: code, Err: err}
}

func assertProcessResult(t *testing.T, tc matrixCase, expect string, res commandResult) {
	t.Helper()
	failOnPanic(t, res.Output)
	switch expect {
	case "help":
		if !strings.Contains(res.Output, "commands:") && !strings.Contains(res.Output, "<cr>") {
			t.Fatalf("%s expected help output, rc=%d err=%v output=%s", tc.ID, res.Code, res.Err, res.Output)
		}
	case "ok":
		if res.Err != nil || res.Code != 0 {
			t.Fatalf("%s expected success, rc=%d err=%v output=%s", tc.ID, res.Code, res.Err, res.Output)
		}
	case "error":
		if tc.Runner == "cli" {
			if res.Code == 0 {
				t.Fatalf("%s expected CLI error, rc=0 output=%s", tc.ID, res.Output)
			}
			return
		}
		if !isShellErrorOutput(res.Output) {
			t.Fatalf("%s expected shell error output, rc=%d err=%v output=%s", tc.ID, res.Code, res.Err, res.Output)
		}
	case "ok_or_clear":
		if res.Err == nil && res.Code == 0 && !isFailureStatusOutput(res.Output) {
			return
		}
		if !isClearRuntimeFailure(res.Output) {
			t.Fatalf("%s expected success or clear runtime failure, rc=%d err=%v output=%s", tc.ID, res.Code, res.Err, res.Output)
		}
	case "advisory":
	default:
		t.Fatalf("%s has unknown expectation %q", tc.ID, expect)
	}
}

func failOnPanic(t *testing.T, output string) {
	t.Helper()
	lower := strings.ToLower(output)
	if strings.Contains(lower, "panic:") || strings.Contains(lower, "fatal error:") {
		t.Fatalf("process produced panic/fatal output:\n%s", output)
	}
}

func isShellErrorOutput(output string) bool {
	lower := strings.ToLower(output)
	return strings.Contains(output, "Error:") ||
		strings.Contains(lower, "usage:") ||
		strings.Contains(lower, "unknown ") ||
		strings.Contains(lower, "requires ") ||
		strings.Contains(lower, "is required") ||
		strings.Contains(lower, "must ") ||
		strings.Contains(lower, "specified twice") ||
		strings.Contains(lower, "cannot ") ||
		strings.Contains(lower, "error parsing regexp") ||
		strings.Contains(lower, "unsupported ") ||
		strings.Contains(lower, "unexpected ") ||
		strings.Contains(lower, "status: failed")
}

func isClearRuntimeFailure(output string) bool {
	lower := strings.ToLower(output)
	return strings.Contains(lower, "wait for android agents") ||
		strings.Contains(lower, "agent disconnected") ||
		strings.Contains(lower, "network not available") ||
		strings.Contains(lower, "did not match requested") ||
		strings.Contains(lower, "timeout") ||
		strings.Contains(lower, "timed out") ||
		strings.Contains(lower, "no matching") ||
		strings.Contains(lower, "no_dns_address_for_family") ||
		strings.Contains(lower, "dns resolution failed") ||
		strings.Contains(lower, "one_or_more_families_failed") ||
		strings.Contains(lower, "not a device owner, profile owner, system app, or privileged app") ||
		strings.Contains(lower, "unsupported") ||
		strings.Contains(lower, "connectivity completed with failed checks")
}

func isFailureStatusOutput(output string) bool {
	lower := strings.ToLower(output)
	return strings.Contains(output, "STATUS_FAILED") ||
		strings.Contains(lower, "status=failed") ||
		strings.Contains(lower, "status: failed")
}

func (cfg *e2eConfig) expand(commandLine string) (string, string) {
	serial := firstNonEmpty(cfg.serial, "SERIAL")
	serialPrefix := serial
	if len(serialPrefix) > 12 {
		serialPrefix = serialPrefix[:12]
	}
	ssid := firstNonEmpty(cfg.ssid, "Test SSID")
	psk := firstNonEmpty(cfg.psk, "test-passphrase")
	bssid := firstNonEmpty(cfg.bssid, "aa:bb:cc:dd:ee:ff")
	agentPrefix := firstNonEmpty(cfg.agentPref, serialPrefix)
	replacements := map[string]string{
		"<serial>":                            serial,
		"<serial-prefix>":                     serialPrefix,
		"<agent-id-prefix-from-show-devices>": agentPrefix,
		"<ssid>":                              ssid,
		"<psk>":                               quoteToken(psk),
		"<bssid>":                             bssid,
	}
	out := commandLine
	for key, value := range replacements {
		out = strings.ReplaceAll(out, key, value)
	}
	if strings.Contains(commandLine, "<bssid>") && cfg.bssid == "" {
		return out, "<bssid>"
	}
	return out, ""
}

func quoteToken(value string) string {
	if value == "" {
		return `""`
	}
	if strings.ContainsAny(value, " \t\r\n\"'\\|&;$<>") {
		return strconv.Quote(value)
	}
	return value
}

func requiresWiFiSecret(commandLine string) bool {
	return strings.Contains(commandLine, "passphrase") || strings.Contains(commandLine, "wifi connect") || strings.Contains(commandLine, "wifi cycle")
}

func timeoutFor(tc matrixCase) time.Duration {
	commandLine := strings.ToLower(tc.Command)
	switch {
	case strings.Contains(commandLine, "traceroute"):
		return 90 * time.Second
	case strings.Contains(commandLine, "path-mtu"):
		return 60 * time.Second
	case strings.Contains(commandLine, "wifi cycle"):
		return 90 * time.Second
	case strings.Contains(commandLine, "scan fresh"), strings.Contains(commandLine, "wifi connect"):
		return 45 * time.Second
	case tc.Expect == "error":
		return 20 * time.Second
	default:
		return 35 * time.Second
	}
}

func (cfg *e2eConfig) writeLog(t *testing.T, tc matrixCase, commandLine string, res commandResult) string {
	t.Helper()
	var b strings.Builder
	fmt.Fprintf(&b, "ID: %s\nTitle: %s\nRunner: %s\nExpect: %s\n", tc.ID, tc.Title, tc.Runner, tc.Expect)
	fmt.Fprintf(&b, "Command: %s\nAssertion: %s\n\n", redact(commandLine, cfg.psk), tc.Assertion)
	fmt.Fprintf(&b, "ExitCode: %d\nError: %v\n\n", res.Code, res.Err)
	b.WriteString(redact(res.Output, cfg.psk))
	path := filepath.Join(cfg.logDir, tc.ID+".log")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("write e2e log: %v", err)
	}
	return path
}

func redact(value string, secret string) string {
	if secret == "" {
		return value
	}
	return strings.ReplaceAll(value, secret, "<redacted>")
}

func (cfg *e2eConfig) restoreAfterCase(tc matrixCase, commandLine string) {
	if !cfg.live || cfg.bin == "" || cfg.serial == "" {
		return
	}
	if shouldRestoreWiFiAfter(commandLine) {
		cfg.restoreWiFiConnection()
	}
}

func shouldRestoreWiFiAfter(commandLine string) bool {
	lower := strings.ToLower(commandLine)
	return strings.Contains(lower, "wifi disconnect") ||
		strings.Contains(lower, "wifi forget") ||
		strings.Contains(lower, "wifi cycle")
}

func (cfg *e2eConfig) restoreWiFiConnection() {
	if cfg.ssid == "" || cfg.psk == "" {
		return
	}
	cfg.runCLICleanup("wifi", "connect", cfg.ssid, "passphrase", cfg.psk, "security", "auto", "timeout", "25000")
	cfg.runCLICleanup("wifi", "wait", "connected", "ssid", cfg.ssid, "require-ip", "true", "require-validated", "true", "timeout", "30000")
}

func (cfg *e2eConfig) resetLiveState() {
	cfg.forceStopPackage()
}

func (cfg *e2eConfig) runCLICleanup(args ...string) {
	if cfg.bin == "" || cfg.serial == "" {
		return
	}
	fullArgs := append([]string{"--serial", cfg.serial}, args...)
	_ = cfg.runExternal(45*time.Second, fullArgs, "")
}

func (cfg *e2eConfig) resolveAgentPrefix() string {
	res := cfg.runExternal(25*time.Second, []string{"--serial", cfg.serial, "--format", "json", "show", "devices"}, "")
	if match := agentID.FindStringSubmatch(res.Output); match != nil {
		value := match[1]
		if len(value) > 8 {
			return value[:8]
		}
		return value
	}
	if len(cfg.serial) > 12 {
		return cfg.serial[:12]
	}
	return cfg.serial
}

func (cfg *e2eConfig) resolveBSSID() string {
	cfg.restoreWiFiConnection()
	res := cfg.runExternal(25*time.Second, []string{"--serial", cfg.serial, "show", "wifi", "status"}, "")
	if match := bssidPattern.FindStringSubmatch(res.Output); match != nil {
		return strings.ToLower(match[1])
	}
	return ""
}

var (
	agentID      = regexp.MustCompile(`"id"\s*:\s*"([^"]+)"`)
	bssidPattern = regexp.MustCompile(`(?i)bssid=([0-9a-f]{2}(?::[0-9a-f]{2}){5})`)
)

func (cfg *e2eConfig) forceStop() {
	if !cfg.forceStopApp || cfg.adb == "" || cfg.serial == "" || cfg.packageName == "" {
		return
	}
	cfg.forceStopPackage()
}

func (cfg *e2eConfig) forceStopPackage() {
	if cfg.adb == "" || cfg.serial == "" || cfg.packageName == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, cfg.adb, "-s", cfg.serial, "shell", "am", "force-stop", cfg.packageName)
	_ = cmd.Run()
}

func (cfg *e2eConfig) launchApp(t *testing.T, reason string, wait bool) {
	t.Helper()
	if !cfg.launchAppActivity || !cfg.live || cfg.adb == "" || cfg.serial == "" || cfg.packageName == "" {
		return
	}
	args := []string{"am", "start"}
	if wait {
		args = append(args, "-W")
	}
	component := cfg.packageName + "/.MainActivity"
	args = append(args, "-n", component, "-f", "0x34000000")
	out, err := cfg.adbShell(10*time.Second, args...)
	if err != nil {
		t.Logf("launch app failed reason=%s wait=%t component=%s err=%v output=%s", reason, wait, component, err, strings.TrimSpace(out))
		return
	}
	t.Logf("launch app reason=%s wait=%t component=%s output=%s", reason, wait, component, oneLine(out))
}

func (cfg *e2eConfig) adbShell(timeout time.Duration, args ...string) (string, error) {
	if cfg.adb == "" || cfg.serial == "" {
		return "", errors.New("adb or serial is empty")
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	fullArgs := append([]string{"-s", cfg.serial, "shell"}, args...)
	cmd := exec.CommandContext(ctx, cfg.adb, fullArgs...)
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return string(out), ctx.Err()
	}
	return string(out), err
}

func packageDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Dir(file)
}

func findControllerRoot(t *testing.T) string {
	t.Helper()
	dir := packageDir(t)
	for {
		if fileExists(filepath.Join(dir, "go.mod")) && dirExists(filepath.Join(dir, "cmd", "dropcheck")) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find controller root")
		}
		dir = parent
	}
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func hasSerialArg(args []string) bool {
	for _, arg := range args {
		if arg == "--serial" || strings.HasPrefix(arg, "--serial=") {
			return true
		}
	}
	return false
}

func envBool(name string) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	return value == "1" || value == "true" || value == "yes"
}

func envBoolDefault(name string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	return envBool(name)
}

func envOr(name string, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func filteredCases(cases []matrixCase, filter string) []matrixCase {
	if filter == "" {
		return cases
	}
	var selected []matrixCase
	for _, tc := range cases {
		if caseMatchesFilter(tc, filter) {
			selected = append(selected, tc)
		}
	}
	return selected
}

func hasLiveProcessCases(cases []matrixCase) bool {
	for _, tc := range cases {
		if tc.Runner == "shell" || tc.Runner == "cli" {
			return true
		}
	}
	return false
}

func liveSetupNeeds(cases []matrixCase) (agentPrefix bool, wifiSetup bool, bssid bool) {
	for _, tc := range cases {
		if tc.Runner != "shell" && tc.Runner != "cli" {
			continue
		}
		if strings.Contains(tc.Command, "<agent-id-prefix-from-show-devices>") {
			agentPrefix = true
		}
		if strings.Contains(tc.Command, "<bssid>") {
			bssid = true
			wifiSetup = true
		}
		if caseNeedsWiFiSetup(tc) {
			wifiSetup = true
		}
	}
	return agentPrefix, wifiSetup, bssid
}

func caseNeedsWiFiSetup(tc matrixCase) bool {
	commandLine := strings.ToLower(tc.Command)
	if strings.Contains(commandLine, "<ssid>") || strings.Contains(commandLine, "<psk>") || strings.Contains(commandLine, "<bssid>") {
		return true
	}
	wifiOrNetworkCommands := []string{"show wifi", "show ip", "wifi connect", "wifi wait", "wifi assert", "wifi reconnect", "wifi cycle", "wifi disconnect", "wifi forget", "wifi monitor", "ping ", "traceroute ", "path-mtu ", "global-ip", "dns ", "http ", "download "}
	for _, needle := range wifiOrNetworkCommands {
		if strings.Contains(commandLine, needle) {
			return true
		}
	}
	return false
}

func caseMatchesFilter(tc matrixCase, filter string) bool {
	filter = strings.ToLower(filter)
	return strings.Contains(strings.ToLower(tc.ID), filter) ||
		strings.Contains(strings.ToLower(tc.Title), filter) ||
		strings.Contains(strings.ToLower(tc.Runner), filter) ||
		strings.Contains(strings.ToLower(tc.Command), filter) ||
		strings.Contains(strings.ToLower(tc.Assertion), filter)
}

func outputTail(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	const max = 900
	if len(value) <= max {
		return value
	}
	return value[len(value)-max:]
}

func oneLine(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Join(strings.Fields(value), " ")
	return value
}

func TestE2EFailureClassifiers(t *testing.T) {
	configFailure := "Status: failed  Message: wifi ssid is required"
	if !isFailureStatusOutput(configFailure) {
		t.Fatalf("config failure status was not detected")
	}
	if isClearRuntimeFailure(configFailure) {
		t.Fatalf("config validation failure must not count as a clear runtime failure")
	}
	if !isClearRuntimeFailure("Status: failed  Message: network not available for ping") {
		t.Fatalf("network runtime failure was not accepted")
	}
	if !isClearRuntimeFailure("Status: failed  Message: global IP check failed\nError: one_or_more_families_failed\nipv6    -   false   0       0ms      no_dns_address_for_family") {
		t.Fatalf("missing address family for global IP must count as a clear runtime failure")
	}
	if !isClearRuntimeFailure("Status: failed  Message: dns resolution failed\nDNS: name=example.com elapsed=13ms answers=0") {
		t.Fatalf("DNS runtime resolution failures must count as clear runtime failures")
	}
	if !isClearRuntimeFailure("Status: failed  Message: wifi addNetworkPrivileged failed: SecurityException:Caller is not a device owner, profile owner, system app, or privileged app") {
		t.Fatalf("platform Wi-Fi provisioning limits must count as clear runtime failures")
	}
	if !isShellErrorOutput("match regex: error parsing regexp: missing closing ]: `[`") {
		t.Fatalf("regexp parse errors must count as shell errors")
	}
}
