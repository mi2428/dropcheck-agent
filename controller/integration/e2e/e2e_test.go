//go:build e2e

// Package e2e runs the merged Dropcheck Shell/CLI manual matrix as Go tests.
//
// Parser-only checks do not require a device:
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
// <bssid>, <plan>, and <sync-dir> so lab secrets and machine-local paths are
// not committed.
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
	planPath       string
	missingPlan    string
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
			cfg.runShellCleanup("set controller endpoint disabled")
			cfg.runShellCleanup("set festival standalone disabled")
			if cfg.ssid != "" && cfg.psk != "" {
				cfg.runCLICleanup("wifi", "connect", cfg.ssid, "--passphrase", cfg.psk, "--security", "auto", "--timeout", "25000")
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
		syncDir:            filepath.Join(t.TempDir(), "festival-sync"),
		live:               envBool(envLive),
		serial:             firstNonEmpty(os.Getenv(envSerial), os.Getenv("ADB_SERIAL")),
		ssid:               firstNonEmpty(os.Getenv(envSSID), os.Getenv("DROPCHECK_FESTIVAL_WIFI_SSID")),
		forceStopApp:       envBool(envForceStop),
		launchAppActivity:  envBoolDefault(envLaunchApp, true),
		launchAppEveryCase: envBoolDefault(envLaunchEach, true),
		vars:               map[string]string{},
	}
	pskEnv := firstNonEmpty(os.Getenv(envPSKName), os.Getenv("DROPCHECK_FESTIVAL_WIFI_PSK_ENV"), defaultPSKEnv)
	cfg.psk = firstNonEmpty(os.Getenv(pskEnv), os.Getenv(envPSK), os.Getenv("DROPCHECK_FESTIVAL_WIFI_PSK"))
	cfg.planPath = renderFestivalPlan(t, cfg.ssid, cfg.psk)
	cfg.missingPlan = filepath.Join(t.TempDir(), "missing-festival-plan.json")
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
			cfg.runCLICleanup("wifi", "connect", cfg.ssid, "--passphrase", cfg.psk, "--security", "auto", "--timeout", "25000")
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
	if shell.IsHelpLine(commandLine) {
		entries := shell.HelpEntries(commandLine)
		if len(entries) == 0 {
			return commandResult{Output: "help entries: <empty>", Code: 1, Err: errors.New("empty help entries")}
		}
		var b strings.Builder
		for _, entry := range entries {
			fmt.Fprintf(&b, "%s\t%s\n", entry.Token, entry.Description)
		}
		return commandResult{Output: b.String(), Code: 0}
	}
	parsed, err := shell.ParseLine(commandLine)
	if err != nil {
		return commandResult{Output: err.Error(), Code: 1, Err: err}
	}
	if parsed.Kind == shell.FestivalSync && parsed.FestivalSyncLimit != "" {
		limit, err := strconv.ParseUint(parsed.FestivalSyncLimit, 10, 32)
		if err != nil || limit == 0 {
			err := fmt.Errorf("festival sync limit must be a positive integer")
			return commandResult{Output: err.Error(), Code: 1, Err: err}
		}
	}
	return commandResult{Output: "parse ok\n", Code: 0}
}

func (cfg *e2eConfig) runShellCase(tc matrixCase, commandLine string) commandResult {
	input := commandLine + "\n"
	if commandLine != "quit" && commandLine != "exit" {
		input += "quit\n"
	}
	return cfg.runExternal(timeoutFor(tc), []string{"--serial", cfg.serial, "shell"}, input)
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
		if res.Err == nil && res.Code == 0 {
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
		strings.Contains(lower, "must ") ||
		strings.Contains(lower, "specified twice") ||
		strings.Contains(lower, "cannot ")
}

func isClearRuntimeFailure(output string) bool {
	lower := strings.ToLower(output)
	return isShellErrorOutput(output) ||
		strings.Contains(output, "STATUS_FAILED") ||
		strings.Contains(lower, "status=failed") ||
		strings.Contains(lower, "wait for android agents") ||
		strings.Contains(lower, "agent disconnected")
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
		"<plan>":                              quoteToken(cfg.planPath),
		"<missing-plan>":                      quoteToken(cfg.missingPlan),
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
	case strings.Contains(commandLine, "festival run once"), strings.Contains(commandLine, "festival standalone enabled"):
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
	if match := festivalRunID.FindStringSubmatch(output); match != nil {
		cfg.vars["run-id"] = match[1]
	}
}

var festivalRunID = regexp.MustCompile(`Dropcheck Festival run: id=([A-Za-z0-9._:-]+)`)

func (cfg *e2eConfig) restoreAfterCase(tc matrixCase, commandLine string) {
	if !cfg.live || cfg.bin == "" || cfg.serial == "" {
		return
	}
	lower := strings.ToLower(commandLine)
	switch {
	case strings.Contains(lower, "forget"):
		if cfg.ssid != "" && cfg.psk != "" {
			cfg.runCLICleanup("wifi", "connect", cfg.ssid, "--passphrase", cfg.psk, "--security", "auto", "--timeout", "25000")
		}
	case strings.Contains(lower, "set target all"):
		cfg.runShellCleanup("set target " + cfg.serial)
	case strings.Contains(lower, "set controller endpoint") && strings.Contains(lower, "enabled"):
		cfg.runShellCleanup("set controller endpoint disabled")
	case strings.Contains(lower, "set festival standalone enabled"):
		cfg.runShellCleanup("set festival standalone disabled")
	}
}

func (cfg *e2eConfig) runShellCleanup(commandLine string) {
	if cfg.bin == "" || cfg.serial == "" {
		return
	}
	_ = cfg.runExternal(20*time.Second, []string{"--serial", cfg.serial, "shell"}, commandLine+"\nquit\n")
}

func (cfg *e2eConfig) runCLICleanup(args ...string) {
	if cfg.bin == "" || cfg.serial == "" {
		return
	}
	fullArgs := append([]string{"--serial", cfg.serial}, args...)
	_ = cfg.runExternal(45*time.Second, fullArgs, "")
}

func (cfg *e2eConfig) resolveAgentPrefix() string {
	res := cfg.runExternal(25*time.Second, []string{"--serial", cfg.serial, "--format", "json", "devices"}, "")
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
	cfg.runCLICleanup("wifi", "connect", cfg.ssid, "--passphrase", cfg.psk, "--security", "auto", "--timeout", "25000")
	res := cfg.runExternal(25*time.Second, []string{"--serial", cfg.serial, "wifi", "status"}, "")
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

func renderFestivalPlan(t *testing.T, ssid string, psk string) string {
	t.Helper()
	templatePath := filepath.Join(packageDir(t), "testdata", "festival_plan.template.json")
	data, err := os.ReadFile(templatePath)
	if err != nil {
		t.Fatal(err)
	}
	if ssid == "" {
		ssid = "Test SSID"
	}
	if psk == "" {
		psk = "test-passphrase"
	}
	rendered := strings.ReplaceAll(string(data), "{{SSID}}", ssid)
	rendered = strings.ReplaceAll(rendered, "{{PSK}}", psk)
	path := filepath.Join(t.TempDir(), "festival_plan.json")
	if err := os.WriteFile(path, []byte(rendered), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
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
		"wifi scan",
		"wifi status",
		"wifi connect",
		"wifi wait",
		"wifi assert",
		"wifi reconnect",
		"wifi cycle",
		"wifi disconnect",
		"wifi forget",
		"monitor wifi",
		"wifi monitor",
		"wifi watch",
		"ping",
		"traceroute",
		"path-mtu",
		"global-ip",
		"test dns",
		"test http",
		"test download",
		"dropcheck dns",
		"dropcheck http",
		"dropcheck download",
		"dropcheck global-ip",
		"dropcheck path-mtu",
		"dropcheck traceroute",
		"dropcheck ping",
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
	if len(cases) != 355 {
		t.Fatalf("case count = %d, want 355", len(cases))
	}
	if bytes.Contains(mustReadFile(t, filepath.Join(packageDir(t), "testdata", "festival_plan.template.json")), []byte("shizkkawaii")) {
		t.Fatalf("festival plan template leaks the lab passphrase")
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
