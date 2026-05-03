//go:build e2e

// Package e2e runs the merged Dropcheck Shell/CLI manual matrix as Go tests.
//
// Parser and case-table consistency checks do not require a device:
//
//	cd controller
//	go test -tags e2e ./integration/e2e
//
// Full live execution requires an attached Android device and test Wi-Fi:
//
//	make e2e SERIAL=45240DLAQ007HG SSID="SHIZK RADIO" PSK_ENV=DROPCHECK_E2E_WIFI_PSK
//
// make e2e runs go test -v -count=1. Each case prints its title, runner, command,
// result, elapsed time, per-case log path, and a short output tail.
//
// Useful environment variables:
//
//	DROPCHECK_E2E_FILTER       substring filter for case ID, title, runner, mode, command, or notes
//	DROPCHECK_E2E_LOG_DIR      persistent directory for per-case logs
//	DROPCHECK_E2E_BIN          prebuilt dropcheck binary to use instead of building a temp one
//	DROPCHECK_E2E_LAUNCH_APP   set to 0 to avoid bringing the Android activity to the foreground
//	DROPCHECK_E2E_LAUNCH_APP_EVERY_CASE
//	                           set to 0 to skip per-case foregrounding; defaults to 1
//	DROPCHECK_E2E_FORCE_STOP   set to 1 to force-stop the Android app before each live case
//
// The case table is testdata/e2e_cases.tsv. The title column is included in Go
// subtest names, for example S001_help, so verbose output remains readable.
// Commands intentionally use placeholders such as <ssid>, <psk>, <serial>,
// <bssid> and <sync-dir> so lab secrets and machine-local paths are
// not committed. Shell commands prefixed with "request> " are executed inside
// the interactive request mode.
package e2e

import (
	"bytes"
	"context"
	"encoding/csv"
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
	"dropcheck/controller/internal/linuxcli"
	"dropcheck/controller/internal/shell"
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
)

type matrixCase struct {
	ID       string
	Source   string
	Title    string
	Runner   string
	Mode     string
	Command  string
	Expect   string
	Expected string
	Notes    string
}

type e2eConfig struct {
	controllerRoot string
	repoRoot       string
	bin            string
	adb            string
	packageName    string
	logDir         string
	syncDir        string

	live               bool
	serial             string
	ssid               string
	psk                string
	bssid              string
	agentPref          string
	forceStopApp       bool
	launchAppActivity  bool
	launchAppEveryCase bool

	vars map[string]string
}

type commandResult struct {
	Output string
	Code   int
	Err    error
}

func TestDropcheckShellManualMatrix(t *testing.T) {
	cases := loadCases(t)
	cfg := loadConfig(t)
	filter := os.Getenv(envFilter)
	selected := filteredCases(cases, filter)
	if cfg.live && hasLiveProcessCases(selected) {
		cfg.prepareLive(t, selected)
		t.Cleanup(func() {
			cfg.resetLiveState()
			if cfg.ssid != "" && cfg.psk != "" {
				cfg.runCLICleanup("request", "wifi", "connect", cfg.ssid, "--passphrase", cfg.psk, "--security", "auto", "--timeout", "25000")
			}
			cfg.launchApp(t, "suite cleanup", false)
		})
	}
	t.Logf("e2e cases=%d selected=%d live=%t logs=%s filter=%q serial=%q package=%q launch_app=%t launch_app_every_case=%t force_stop=%t", len(cases), len(selected), cfg.live, cfg.logDir, filter, cfg.serial, cfg.packageName, cfg.launchAppActivity, cfg.launchAppEveryCase, cfg.forceStopApp)

	for _, tc := range selected {
		tc := tc
		t.Run(tc.testName(), func(t *testing.T) {
			start := time.Now()
			commandLine, missing := cfg.expand(tc.Command, tc.Runner)
			if missing != "" {
				t.Skipf("missing runtime value %s for %s", missing, tc.Command)
			}
			expect := normalizedExpect(tc)
			t.Logf("START %s title=%q source=%s runner=%s mode=%s expect=%s command=%s", tc.ID, tc.Title, tc.Source, tc.Runner, tc.Mode, expect, redact(commandLine, cfg.psk))
			switch tc.Runner {
			case "shell-parser":
				res := runShellParser(commandLine)
				logPath := cfg.writeLog(t, tc, commandLine, res)
				t.Logf("DONE %s rc=%d err=%v elapsed=%s log=%s output_tail=%q", tc.ID, res.Code, res.Err, time.Since(start).Round(time.Millisecond), logPath, outputTail(redact(res.Output, cfg.psk)))
				assertParserResult(t, tc, expect, res)
			case "shell", "cli":
				if !cfg.live {
					t.Skipf("set %s=1, %s, %s, and %s to run live e2e cases", envLive, envSerial, envSSID, envPSK)
				}
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
				cfg.captureVars(res.Output)
				t.Logf("DONE %s rc=%d err=%v elapsed=%s log=%s output_tail=%q", tc.ID, res.Code, res.Err, time.Since(start).Round(time.Millisecond), logPath, outputTail(redact(res.Output, cfg.psk)))
				assertProcessResult(t, tc, expect, res)
				cfg.restoreAfterCase(tc, commandLine)
			default:
				t.Fatalf("unknown runner %q", tc.Runner)
			}
		})
	}
}

func loadCases(t *testing.T) []matrixCase {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(packageDir(t), "testdata", "e2e_cases.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	reader := csv.NewReader(strings.NewReader(string(data)))
	reader.Comma = '\t'
	reader.FieldsPerRecord = -1
	reader.LazyQuotes = true
	rows, err := reader.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) < 2 {
		t.Fatalf("no e2e cases loaded")
	}
	var cases []matrixCase
	seen := map[string]bool{}
	for i, row := range rows[1:] {
		if len(row) != 9 {
			t.Fatalf("e2e case row %d has %d fields, want 9: %#v", i+2, len(row), row)
		}
		tc := matrixCase{
			ID:       row[0],
			Source:   row[1],
			Title:    row[2],
			Runner:   row[3],
			Mode:     row[4],
			Command:  row[5],
			Expect:   row[6],
			Expected: row[7],
			Notes:    row[8],
		}
		if seen[tc.ID] {
			t.Fatalf("duplicate e2e case ID %q", tc.ID)
		}
		seen[tc.ID] = true
		cases = append(cases, tc)
	}
	return cases
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
		syncDir:            filepath.Join(t.TempDir(), "standalone-sync"),
		live:               envBool(envLive),
		serial:             firstNonEmpty(os.Getenv(envSerial), os.Getenv("ADB_SERIAL")),
		ssid:               os.Getenv(envSSID),
		forceStopApp:       envBool(envForceStop),
		launchAppActivity:  envBoolDefault(envLaunchApp, true),
		launchAppEveryCase: envBoolDefault(envLaunchEach, true),
		vars:               map[string]string{},
	}
	pskEnv := firstNonEmpty(os.Getenv(envPSKName), defaultPSKEnv)
	cfg.psk = firstNonEmpty(os.Getenv(pskEnv), os.Getenv(envPSK))
	cfg.vars["run-id"] = ""
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
			cfg.runCLICleanup("request", "wifi", "connect", cfg.ssid, "--passphrase", cfg.psk, "--security", "auto", "--timeout", "25000")
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

func runShellParser(commandLine string) commandResult {
	requestLine, requestMode := requestModeCommand(commandLine)
	configureLine, configureMode := configureModeCommand(commandLine)
	parseLine := commandLine
	if requestMode {
		parseLine = requestLine
	} else if configureMode {
		parseLine = configureLine
	}
	if shell.IsHelpLine(parseLine) {
		var out bytes.Buffer
		switch {
		case requestMode:
			shell.WriteRequestContextHelp(&out, parseLine)
		case configureMode:
			shell.WriteConfigureContextHelp(&out, parseLine)
		default:
			shell.WriteContextHelp(&out, parseLine)
		}
		if strings.TrimSpace(out.String()) == "" {
			return commandResult{Output: "help output: <empty>", Code: 1, Err: errors.New("empty help output")}
		}
		return commandResult{Output: out.String(), Code: 0}
	}
	var (
		parsed shell.Command
		err    error
	)
	if requestMode {
		parsed, err = shell.ParseRequestLine(parseLine)
	} else if configureMode {
		parsed, err = shell.ParseConfigureLine(parseLine)
	} else {
		parsed, err = shell.ParseLine(parseLine)
	}
	if err != nil {
		return commandResult{Output: err.Error(), Code: 1, Err: err}
	}
	if parsed.Kind == shell.StandaloneSync && parsed.StandaloneSyncLimit != "" {
		limit, err := strconv.ParseUint(parsed.StandaloneSyncLimit, 10, 32)
		if err != nil || limit == 0 {
			err := fmt.Errorf("standalone sync limit must be a positive integer")
			return commandResult{Output: err.Error(), Code: 1, Err: err}
		}
	}
	return commandResult{Output: "parse ok\n", Code: 0}
}

func (cfg *e2eConfig) runShellCase(tc matrixCase, commandLine string) commandResult {
	input := shellInput(commandLine)
	if commandLine != "quit" && commandLine != "exit" {
		input += "quit\n"
	}
	return cfg.runExternal(timeoutFor(tc), []string{"--serial", cfg.serial, "shell"}, input)
}

func shellInput(commandLine string) string {
	if requestLine, ok := requestModeCommand(commandLine); ok {
		return "request\n" + requestLine + "\n"
	}
	if configureLine, ok := configureModeCommand(commandLine); ok {
		return "configure\n" + configureLine + "\n"
	}
	return commandLine + "\n"
}

func requestModeCommand(commandLine string) (string, bool) {
	const marker = "request> "
	if strings.HasPrefix(commandLine, marker) {
		return strings.TrimPrefix(commandLine, marker), true
	}
	return commandLine, false
}

func configureModeCommand(commandLine string) (string, bool) {
	const marker = "config> "
	if strings.HasPrefix(commandLine, marker) {
		return strings.TrimPrefix(commandLine, marker), true
	}
	return commandLine, false
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

func assertParserResult(t *testing.T, tc matrixCase, expect string, res commandResult) {
	t.Helper()
	failOnPanic(t, res.Output)
	switch expect {
	case "help":
		if res.Err != nil || strings.TrimSpace(res.Output) == "" {
			t.Fatalf("%s expected help entries, got err=%v output=%q", tc.ID, res.Err, res.Output)
		}
	case "ok", "ok_or_clear":
		if res.Err != nil {
			t.Fatalf("%s expected parse ok, got %v", tc.ID, res.Err)
		}
	case "error":
		if res.Err == nil {
			t.Fatalf("%s expected parse error, got ok", tc.ID)
		}
	case "advisory":
	default:
		t.Fatalf("%s has unknown expectation %q", tc.ID, expect)
	}
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

func normalizedExpect(tc matrixCase) string {
	switch tc.ID {
	case "T2-FEST-006", "T2-FEST-012", "T2-FEST-022", "T2-FEST-023", "T2-NET-004", "T2-PIPE-002", "T2-PIPE-006", "T2-PIPE-009":
		return "error"
	case "T2-WEXP-015", "T2-WEXP-016", "T2-WSCAN-012":
		return "ok"
	default:
		return tc.Expect
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
		strings.Contains(lower, "controller endpoint is disabled or incomplete") ||
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

func (cfg *e2eConfig) expand(commandLine string, runner string) (string, string) {
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
		"<sync-dir>":                          quoteToken(cfg.syncDir),
	}
	if runID := cfg.vars["run-id"]; runID != "" {
		replacements["<run-id>"] = runID
	}
	out := commandLine
	for key, value := range replacements {
		out = strings.ReplaceAll(out, key, value)
	}
	if runner != "shell-parser" {
		switch {
		case strings.Contains(out, "<run-id>"):
			return out, "<run-id>"
		case strings.Contains(commandLine, "<bssid>") && cfg.bssid == "":
			return out, "<bssid>"
		}
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
	return strings.Contains(commandLine, "passphrase") || strings.Contains(commandLine, "--passphrase") || strings.Contains(commandLine, "wifi connect") || strings.Contains(commandLine, "wifi cycle")
}

func timeoutFor(tc matrixCase) time.Duration {
	commandLine := strings.ToLower(tc.Command)
	switch {
	case tc.Runner == "shell-parser":
		return 5 * time.Second
	case strings.Contains(commandLine, "traceroute"):
		return 90 * time.Second
	case strings.Contains(commandLine, "path-mtu"):
		return 60 * time.Second
	case strings.Contains(commandLine, "standalone run once"), strings.Contains(commandLine, "set standalone enabled"):
		return 90 * time.Second
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
	fmt.Fprintf(&b, "ID: %s\nTitle: %s\nSource: %s\nRunner: %s\nMode: %s\nExpect: %s\n", tc.ID, tc.Title, tc.Source, tc.Runner, tc.Mode, normalizedExpect(tc))
	fmt.Fprintf(&b, "Command: %s\nExpected: %s\nNotes: %s\n\n", redact(commandLine, cfg.psk), tc.Expected, tc.Notes)
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

func (cfg *e2eConfig) captureVars(output string) {
	if cfg.vars == nil {
		cfg.vars = map[string]string{}
	}
	if match := standaloneRunID.FindStringSubmatch(output); match != nil {
		cfg.vars["run-id"] = match[1]
	}
}

var standaloneRunID = regexp.MustCompile(`Standalone run: id=([A-Za-z0-9._:-]+)`)

func (cfg *e2eConfig) restoreAfterCase(tc matrixCase, commandLine string) {
	if !cfg.live || cfg.bin == "" || cfg.serial == "" {
		return
	}
	lower := strings.ToLower(commandLine)
	switch {
	case strings.Contains(lower, "forget"):
		if cfg.ssid != "" && cfg.psk != "" {
			cfg.runCLICleanup("request", "wifi", "connect", cfg.ssid, "--passphrase", cfg.psk, "--security", "auto", "--timeout", "25000")
		}
	case strings.Contains(lower, "set controller endpoint") && strings.Contains(lower, "enabled"):
		cfg.forceStopPackage()
		cfg.runShellCleanup("config> set controller endpoint disabled")
		cfg.forceStopPackage()
	case strings.Contains(lower, "set standalone enabled"):
		cfg.runShellCleanup("config> set standalone disabled")
	}
}

func (cfg *e2eConfig) runShellCleanup(commandLine string) {
	if cfg.bin == "" || cfg.serial == "" {
		return
	}
	_ = cfg.runExternal(20*time.Second, []string{"--serial", cfg.serial, "shell"}, shellInput(commandLine)+"quit\n")
}

func (cfg *e2eConfig) resetLiveState() {
	cfg.forceStopPackage()
	cfg.runShellCleanup("config> set controller endpoint disabled")
	cfg.runShellCleanup("config> set standalone disabled")
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
	cfg.runCLICleanup("request", "wifi", "connect", cfg.ssid, "--passphrase", cfg.psk, "--security", "auto", "--timeout", "25000")
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
	wifiOrNetworkCommands := []string{
		"show wifi",
		"show ip",
		"request wifi connect",
		"request wifi wait",
		"request wifi assert",
		"request wifi reconnect",
		"request wifi cycle",
		"request wifi disconnect",
		"request wifi forget",
		"request> wifi connect",
		"request> wifi wait",
		"request> wifi assert",
		"request> wifi reconnect",
		"request> wifi cycle",
		"request> wifi disconnect",
		"request> wifi forget",
		"monitor wifi",
		"request> monitor wifi",
		"request ping",
		"request traceroute",
		"request path-mtu",
		"request global-ip",
		"request dns",
		"request http",
		"request download",
		"request> ping",
		"request> traceroute",
		"request> path-mtu",
		"request> global-ip",
		"request> dns",
		"request> http",
		"request> download",
		"dropcheck request ping",
		"dropcheck show ip",
		"dropcheck request traceroute",
		"dropcheck request path-mtu",
		"dropcheck request global-ip",
		"dropcheck request dns",
		"dropcheck request http",
		"dropcheck request download",
	}
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
		strings.Contains(strings.ToLower(tc.Source), filter) ||
		strings.Contains(strings.ToLower(tc.Runner), filter) ||
		strings.Contains(strings.ToLower(tc.Mode), filter) ||
		strings.Contains(strings.ToLower(tc.Command), filter) ||
		strings.Contains(strings.ToLower(tc.Notes), filter)
}

func (tc matrixCase) testName() string {
	slug := slugForTestName(tc.Title)
	if slug == "" {
		return tc.ID
	}
	return tc.ID + "_" + slug
}

func slugForTestName(value string) string {
	value = strings.ToLower(value)
	var b strings.Builder
	lastUnderscore := false
	for _, r := range value {
		isWord := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if isWord {
			b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore && b.Len() > 0 {
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	slug := strings.Trim(b.String(), "_")
	if len(slug) > 72 {
		slug = strings.TrimRight(slug[:72], "_")
	}
	return slug
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

func TestE2ECaseTableIsMerged(t *testing.T) {
	cases := loadCases(t)
	var testCount, test2Count int
	for _, tc := range cases {
		switch tc.Source {
		case "TEST":
			testCount++
		case "TEST2":
			test2Count++
		default:
			t.Fatalf("unknown source %q for %s", tc.Source, tc.ID)
		}
		if strings.Contains(tc.Command, "shizkkawaii") {
			t.Fatalf("%s leaks the lab passphrase in the case table", tc.ID)
		}
		if strings.TrimSpace(tc.Title) == "" {
			t.Fatalf("%s has an empty test title", tc.ID)
		}
	}
	if testCount == 0 || test2Count == 0 {
		t.Fatalf("merged table must include both TEST and TEST2 cases, got TEST=%d TEST2=%d", testCount, test2Count)
	}
	if len(cases) != 342 {
		t.Fatalf("case count = %d, want 342", len(cases))
	}
}

func TestE2ECaseTableParsesShellAndCLIExpectations(t *testing.T) {
	cases := loadCases(t)
	for _, tc := range cases {
		if tc.Runner != "shell" && tc.Runner != "cli" {
			continue
		}
		expect := normalizedExpect(tc)
		commandLine := expandParserPlaceholders(tc.Command)
		var res commandResult
		switch tc.Runner {
		case "shell":
			res = runShellParser(commandLine)
		case "cli":
			res = runCLIParser(commandLine)
		}
		if expect == "error" {
			if res.Err == nil {
				t.Errorf("%s expected parser error for %s command %q", tc.ID, tc.Runner, tc.Command)
			}
			continue
		}
		if res.Err != nil {
			t.Errorf("%s expected parser success for %s command %q: %v", tc.ID, tc.Runner, tc.Command, res.Err)
		}
	}
}

func TestE2EFailureClassifiers(t *testing.T) {
	configFailure := "Status: failed  Message: festa enabled must be true or false"
	if !isFailureStatusOutput(configFailure) {
		t.Fatalf("config failure status was not detected")
	}
	if isClearRuntimeFailure(configFailure) {
		t.Fatalf("config validation failure must not count as a clear runtime failure")
	}
	if !isClearRuntimeFailure("Status: failed  Message: network not available for ping") {
		t.Fatalf("network runtime failure was not accepted")
	}
	if !isClearRuntimeFailure("Status: failed  Message: wifi addNetworkPrivileged failed: SecurityException:Caller is not a device owner, profile owner, system app, or privileged app") {
		t.Fatalf("platform Wi-Fi provisioning limits must count as clear runtime failures")
	}
	if !isShellErrorOutput("match regex: error parsing regexp: missing closing ]: `[`") {
		t.Fatalf("regexp parse errors must count as shell errors")
	}
}

func TestE2ECaseTableCoversControllerCommandSurface(t *testing.T) {
	cases := loadCases(t)
	required := []struct {
		name   string
		runner string
		text   string
	}{
		{name: "shell help", runner: "shell", text: "help"},
		{name: "shell show devices", runner: "shell", text: "show devices"},
		{name: "shell pipeline", runner: "shell", text: "| match"},
		{name: "shell show controller config", runner: "shell", text: "show config controller endpoint"},
		{name: "shell show controller link", runner: "shell", text: "show controller link"},
		{name: "shell set controller endpoint", runner: "shell", text: "set controller endpoint"},
		{name: "shell controller reconnect", runner: "shell", text: "request> controller reconnect"},
		{name: "shell standalone config", runner: "shell", text: "show config standalone"},
		{name: "shell standalone status", runner: "shell", text: "show standalone status"},
		{name: "shell standalone runs", runner: "shell", text: "show standalone runs"},
		{name: "shell standalone run detail parser", runner: "shell-parser", text: "show standalone run"},
		{name: "shell standalone clear", runner: "shell", text: "clear standalone runs"},
		{name: "shell standalone delete parser", runner: "shell-parser", text: "config> delete standalone"},
		{name: "shell standalone run once", runner: "shell", text: "request> standalone run once"},
		{name: "shell standalone sync", runner: "shell", text: "sync standalone runs"},
		{name: "shell wifi status", runner: "shell", text: "show wifi status"},
		{name: "shell ip status", runner: "shell", text: "show ip status"},
		{name: "shell wifi diagnostics", runner: "shell", text: "show wifi diagnostics"},
		{name: "shell wifi capabilities", runner: "shell", text: "show wifi capabilities"},
		{name: "shell wifi scan", runner: "shell", text: "show wifi scan"},
		{name: "shell wifi fresh scan", runner: "shell", text: "show wifi scan fresh"},
		{name: "shell wifi scan detail", runner: "shell", text: "show wifi scan detail"},
		{name: "shell wifi connect", runner: "shell", text: "request> wifi connect"},
		{name: "shell wifi wait", runner: "shell", text: "request> wifi wait"},
		{name: "shell wifi assert", runner: "shell", text: "request> wifi assert"},
		{name: "shell wifi reconnect", runner: "shell", text: "request> wifi reconnect"},
		{name: "shell wifi monitor", runner: "shell", text: "request> monitor wifi"},
		{name: "shell wifi cycle", runner: "shell", text: "request> wifi cycle"},
		{name: "shell wifi disconnect", runner: "shell", text: "request> wifi disconnect"},
		{name: "shell wifi forget", runner: "shell", text: "request> wifi forget"},
		{name: "shell ping", runner: "shell", text: "request> ping"},
		{name: "shell traceroute", runner: "shell", text: "request> traceroute"},
		{name: "shell path mtu", runner: "shell", text: "request> path-mtu"},
		{name: "shell global ip", runner: "shell", text: "request> global-ip"},
		{name: "shell dns", runner: "shell", text: "request> dns"},
		{name: "shell http", runner: "shell", text: "request> http"},
		{name: "shell download", runner: "shell", text: "request> download"},
		{name: "cli show devices", runner: "cli", text: "dropcheck --serial"},
		{name: "cli ip status", runner: "cli", text: "dropcheck show ip status"},
		{name: "cli wifi scan", runner: "cli", text: "dropcheck show wifi scan"},
		{name: "cli wifi connect", runner: "cli", text: "dropcheck request wifi connect"},
		{name: "cli wifi wait", runner: "cli", text: "dropcheck request wifi wait"},
		{name: "cli wifi assert", runner: "cli", text: "dropcheck request wifi assert"},
		{name: "cli wifi monitor", runner: "cli", text: "dropcheck request monitor wifi"},
		{name: "cli wifi reconnect", runner: "cli", text: "dropcheck request wifi reconnect"},
		{name: "cli wifi cycle", runner: "cli", text: "dropcheck request wifi cycle"},
		{name: "cli ping", runner: "cli", text: "dropcheck request ping"},
		{name: "cli traceroute", runner: "cli", text: "dropcheck request traceroute"},
		{name: "cli path mtu", runner: "cli", text: "dropcheck request path-mtu"},
		{name: "cli global ip", runner: "cli", text: "dropcheck request global-ip"},
		{name: "cli dns", runner: "cli", text: "dropcheck request dns"},
		{name: "cli http", runner: "cli", text: "dropcheck request http"},
		{name: "cli download", runner: "cli", text: "dropcheck request download"},
		{name: "cli standalone runs", runner: "cli", text: "dropcheck show standalone runs"},
		{name: "cli standalone sync", runner: "cli", text: "dropcheck sync standalone runs"},
		{name: "cli controller configure", runner: "cli", text: "dropcheck configure set controller endpoint"},
		{name: "cli standalone configure", runner: "cli", text: "dropcheck configure set standalone"},
		{name: "cli standalone delete", runner: "cli", text: "dropcheck configure delete standalone"},
	}
	for _, want := range required {
		if !e2eTableHasCommand(cases, want.runner, want.text) {
			t.Errorf("missing E2E coverage for %s: runner=%s command contains %q", want.name, want.runner, want.text)
		}
	}
	for _, tc := range cases {
		commandLine := e2eComparableCommand(tc.Command)
		if strings.Contains(commandLine, "wifi watch") || strings.Contains(commandLine, "watch wifi") {
			t.Errorf("%s still references removed wifi watch command: %s", tc.ID, tc.Command)
		}
	}
}

func e2eTableHasCommand(cases []matrixCase, runner string, text string) bool {
	needle := e2eComparableCommand(text)
	for _, tc := range cases {
		if tc.Runner == runner && strings.Contains(e2eComparableCommand(tc.Command), needle) {
			return true
		}
	}
	return false
}

func e2eComparableCommand(value string) string {
	return strings.Join(strings.Fields(strings.ToLower(value)), " ")
}

func expandParserPlaceholders(commandLine string) string {
	return strings.NewReplacer(
		"<serial>", "SERIAL",
		"<ssid>", "Lab",
		"<psk>", "secret",
		"<bssid>", "00:11:22:33:44:55",
		"<sync-dir>", "/tmp/dropcheck-e2e",
	).Replace(commandLine)
}

func runCLIParser(commandLine string) commandResult {
	args, err := commandparse.SplitArgs(commandLine)
	if err != nil {
		return commandResult{Output: err.Error(), Code: 1, Err: err}
	}
	if len(args) > 0 && args[0] == "dropcheck" {
		args = args[1:]
	}
	args, err = stripAppFlags(args)
	if err != nil {
		return commandResult{Output: err.Error(), Code: 1, Err: err}
	}
	_, args, err = linuxcli.ExtractOptions(args)
	if err != nil {
		return commandResult{Output: err.Error(), Code: 1, Err: err}
	}
	if _, err := linuxcli.Parse(args); err != nil {
		return commandResult{Output: err.Error(), Code: 1, Err: err}
	}
	return commandResult{Output: "parse ok\n", Code: 0}
}

func stripAppFlags(args []string) ([]string, error) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			return append([]string(nil), args[i+1:]...), nil
		}
		if !strings.HasPrefix(arg, "-") {
			return append([]string(nil), args[i:]...), nil
		}
		name, _, hasValue := strings.Cut(arg, "=")
		switch name {
		case "--adb", "-adb", "--serial", "-serial", "--package", "-package", "--listen":
			if !hasValue {
				if i+1 >= len(args) {
					return nil, fmt.Errorf("%s requires a value", name)
				}
				i++
			}
		case "--no-adb":
			if hasValue {
				return nil, fmt.Errorf("%s does not take a value", name)
			}
		default:
			return append([]string(nil), args[i:]...), nil
		}
	}
	return nil, nil
}
