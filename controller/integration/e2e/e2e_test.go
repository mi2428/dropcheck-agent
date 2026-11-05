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
// Live commands use the same Shell grammar as the device-free parser suite.
package e2e

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/mi2428/dropcheck-agent/controller/internal/command"
	"github.com/mi2428/dropcheck-agent/controller/internal/shell"
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
	forceStopApp       bool
	launchAppActivity  bool
	launchAppEveryCase bool
}

func TestDropcheckEndToEndMatrix(t *testing.T) {
	cases := loadCases(t)
	if !envBool(envLive) {
		t.Skipf("set %s=1 to run real-device E2E", envLive)
	}
	cfg := loadConfig(t)
	filter := os.Getenv(envFilter)
	selected := filteredCases(cases, filter)
	if cfg.live && hasLiveProcessCases(selected) {
		cfg.prepareLive(t, selected)
		t.Cleanup(func() {
			cfg.resetLiveState()
			if cfg.ssid != "" && cfg.psk != "" {
				if err := cfg.restoreWiFiConnection(); err != nil {
					t.Error(err)
				}
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
			commandLine, missing, err := cfg.expand(tc.Command)
			if err != nil {
				t.Fatalf("runtime placeholder expansion: %v", err)
			}
			if missing != "" {
				t.Skipf("missing runtime value %s for %s", missing, tc.Command)
			}
			expect := tc.Expect
			t.Logf("START %s title=%q runner=%s expect=%s command=%s", tc.ID, tc.Title, tc.Runner, expect, redact(commandLine, cfg.psk))
			switch tc.Runner {
			case "shell":
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
				res := cfg.runShellCommand(timeoutFor(tc), commandLine)
				logPath := cfg.writeLog(t, tc, commandLine, res)
				t.Logf("DONE %s rc=%d err=%v elapsed=%s log=%s output_tail=%q", tc.ID, res.Code, res.Err, time.Since(start).Round(time.Millisecond), logPath, outputTail(redact(res.Output, cfg.psk)))
				assertProcessResult(t, tc, expect, res)
				cfg.restoreAfterCase(t, commandLine)
			default:
				t.Fatalf("unknown runner %q", tc.Runner)
			}
		})
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
	needsWiFiSetup, needsBSSID := liveSetupNeeds(cases)
	t.Logf("live setup needs: wifi_setup=%t bssid=%t", needsWiFiSetup, needsBSSID)
	cfg.launchApp(t, "suite start", true)
	if needsWiFiSetup && cfg.ssid != "" && cfg.psk != "" {
		if needsBSSID {
			cfg.bssid = cfg.resolveBSSID(t)
			t.Logf("resolved bssid: %s", cfg.bssid)
			cfg.launchApp(t, "after bssid discovery", true)
		} else {
			t.Logf("ensuring Wi-Fi connection to test SSID")
			if err := cfg.restoreWiFiConnection(); err != nil {
				t.Fatal(err)
			}
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

func (cfg *e2eConfig) runShellCommand(timeout time.Duration, commandLine string) commandResult {
	input := shellInput(commandLine)
	if commandLine != "exit" {
		input += "exit\n"
	}
	return cfg.runExternal(timeout, []string{"--adb", cfg.adb, "--package", cfg.packageName, "--serial", cfg.serial, "shell"}, input)
}

func shellInput(commandLine string) string {
	return commandLine + "\n"
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

func (cfg *e2eConfig) expand(commandLine string) (string, string, error) {
	lexed, err := command.Lex(commandLine, false)
	if err != nil {
		return "", "", err
	}
	replace := strings.NewReplacer("<serial>", cfg.serial, "<ssid>", cfg.ssid, "<psk>", cfg.psk, "<bssid>", cfg.bssid)
	var out strings.Builder
	previous := 0
	for _, token := range lexed.Tokens {
		if token.Pipe || !strings.Contains(token.Value, "<") {
			continue
		}
		value := replace.Replace(token.Value)
		quoted, err := quoteToken(value)
		if err != nil {
			return "", "", err
		}
		out.WriteString(commandLine[previous:token.Start])
		out.WriteString(quoted)
		previous = token.End
	}
	out.WriteString(commandLine[previous:])
	if strings.Contains(commandLine, "<bssid>") && cfg.bssid == "" {
		return "", "<bssid>", nil
	}
	return out.String(), "", nil
}

func quoteToken(value string) (string, error) {
	if !utf8.ValidString(value) || strings.ContainsAny(value, "\x00\r\n\u0085\u2028\u2029") {
		return "", errors.New("runtime value must be valid UTF-8 on one Shell input line")
	}
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value) + `"`, nil
}

func TestShellQuotePreservesUnicodeLiteral(t *testing.T) {
	for _, value := range []string{"", "plain", `quote ' " backslash \ pipe |`, "東京 e\u0301👩\u200d🔬 | \"quoted\"\\path", "tab\tvalue", "<psk> ${literal}"} {
		quoted, err := quoteToken(value)
		if err != nil {
			t.Fatal(err)
		}
		lexed, err := command.Lex(quoted, false)
		if err != nil || len(lexed.Tokens) != 1 || lexed.Tokens[0].Value != value || lexed.Tokens[0].Pipe {
			t.Fatalf("Shell literal changed: got=%+v want=%q err=%v", lexed.Tokens, value, err)
		}
	}
}

func TestShellRuntimePlaceholdersPreserveTokenBoundaries(t *testing.T) {
	cfg := &e2eConfig{serial: `serial | "\`, ssid: "東京 e\u0301👩\u200d🔬 | \"quoted\"\\<psk>", psk: "秘密 e\u0301👩\u200d🔬 \\\"'|", bssid: "aa:bb:cc:dd:ee:ff"}
	for _, template := range []string{`request wifi connect <ssid> passphrase <psk>`, `request wifi connect "<ssid>" passphrase '<psk>'`} {
		line, missing, err := cfg.expand(template)
		if err != nil || missing != "" {
			t.Fatalf("placeholder expansion failed: missing=%s err=%v", missing, err)
		}
		parsed, err := shell.ParseLine(line)
		if err != nil || parsed.ConnectOptions == nil || parsed.ConnectOptions.SSID != cfg.ssid || parsed.ConnectOptions.Passphrase != cfg.psk {
			t.Fatalf("runtime literal changed at the Shell boundary: %+v err=%v", parsed.ConnectOptions, err)
		}
	}
	line, missing, err := cfg.expand(`show devices | match <serial> | count`)
	if err != nil || missing != "" {
		t.Fatal(missing, err)
	}
	lexed, err := command.Lex(line, false)
	if err != nil || len(lexed.Tokens) != 7 || lexed.Tokens[4].Value != cfg.serial {
		t.Fatalf("placeholder became a pipeline: %+v (%v)", lexed.Tokens, err)
	}
	for _, value := range []string{"bad\nexit", "bad\rline", "bad\x00value", "bad\u0085line", "bad\u2028line", "bad\u2029line", string([]byte{0xff})} {
		cfg.ssid = value
		if _, _, err := cfg.expand(`request wifi connect "<ssid>" passphrase <psk>`); err == nil {
			t.Fatal("unrepresentable runtime placeholder was accepted")
		}
		cfg.bssid = ""
		if _, missing, err := cfg.expand(`request wifi connect <ssid> passphrase <psk> bssid <bssid>`); err == nil || missing != "" {
			t.Fatal("missing BSSID hid an invalid runtime value behind a skip")
		}
	}
}

func TestShellLiveTemplatesKeepParserExpectations(t *testing.T) {
	cfg := &e2eConfig{serial: "synthetic-serial", ssid: "東京 e\u0301👩\u200d🔬 | \"quoted\"\\path", psk: "秘密 e\u0301👩\u200d🔬 \\\"'|", bssid: "aa:bb:cc:dd:ee:ff"}
	for _, tc := range loadCases(t) {
		if tc.Runner != "shell" {
			continue
		}
		line, missing, err := cfg.expand(tc.Command)
		if err != nil || missing != "" {
			t.Fatalf("%s runtime expansion failed: missing=%s err=%v", tc.ID, missing, err)
		}
		if err := parserResultError(tc.Expect, runShellParser(line)); err != nil {
			t.Fatalf("%s runtime literal changed the parser expectation: %v", tc.ID, err)
		}
	}
}

func TestShellRestoreRejectsUnrepresentableValuesBeforeExecution(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "dispatched")
	t.Setenv("DROPCHECK_E2E_TEST_DISPATCH", marker)
	bin := filepath.Join(dir, "fake-dropcheck")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n: > \"$DROPCHECK_E2E_TEST_DISPATCH\"\nprintf '%s\\n' '{\"ok\":true,\"lines\":[]}'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"bad\nexit", "bad\rline", "bad\x00value", "bad\u0085line", "bad\u2028line", "bad\u2029line", string([]byte{0xff})} {
		for _, credential := range []bool{false, true} {
			cfg := &e2eConfig{bin: bin, repoRoot: dir, serial: "synthetic", ssid: "Lab", psk: "synthetic-secret"}
			if credential {
				cfg.psk = value
			} else {
				cfg.ssid = value
			}
			if err := cfg.restoreWiFiConnection(); err == nil || strings.Contains(err.Error(), value) {
				t.Fatal("invalid runtime value was not rejected safely before dispatch", err)
			}
			if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("invalid runtime value dispatched a command", err)
			}
		}
	}
}

func requiresWiFiSecret(commandLine string) bool {
	return strings.Contains(commandLine, "passphrase") || strings.Contains(commandLine, "wifi connect") || strings.Contains(commandLine, "wifi cycle")
}

func timeoutFor(tc matrixCase) time.Duration {
	commandLine := strings.ToLower(tc.Command)
	switch {
	case strings.Contains(commandLine, "traceroute"):
		return 90 * time.Second
	case strings.Contains(commandLine, "request pmtu"):
		return 60 * time.Second
	case strings.Contains(commandLine, "wifi cycle"):
		return 90 * time.Second
	case strings.Contains(commandLine, "request wifi scan"), strings.Contains(commandLine, "wifi connect"):
		return 45 * time.Second
	case tc.Expect == "error":
		return 20 * time.Second
	default:
		return 35 * time.Second
	}
}

func TestPMTUCommandKeepsE2ETimeout(t *testing.T) {
	for _, line := range []string{"request pmtu 1.1.1.1", "request pmtu example.test family ipv6 timeout 30000"} {
		if got := timeoutFor(matrixCase{Command: line}); got != 60*time.Second {
			t.Errorf("%s timeout = %s, want 60s", line, got)
		}
	}
}

func TestShellScanDeadlinesDistinguishFreshAndCached(t *testing.T) {
	for line, want := range map[string]time.Duration{
		"request wifi scan band all timeout 12000": 45 * time.Second,
		"request wifi scan eht timeout 12000":      45 * time.Second,
		"show wifi scan brief":                     35 * time.Second,
	} {
		if got := timeoutFor(matrixCase{Command: line}); got != want {
			t.Errorf("%s timeout=%s want=%s", line, got, want)
		}
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

func (cfg *e2eConfig) restoreAfterCase(t *testing.T, commandLine string) {
	if !cfg.live || cfg.bin == "" || cfg.serial == "" {
		return
	}
	if shouldRestoreWiFiAfter(commandLine) {
		if err := cfg.restoreWiFiConnection(); err != nil {
			t.Fatal(err)
		}
	}
}

func shouldRestoreWiFiAfter(commandLine string) bool {
	lower := strings.ToLower(commandLine)
	return strings.Contains(lower, "wifi disconnect") ||
		strings.Contains(lower, "wifi forget") ||
		strings.Contains(lower, "wifi cycle")
}

func (cfg *e2eConfig) restoreWiFiConnection() error {
	if cfg.bin == "" || cfg.serial == "" || cfg.ssid == "" || cfg.psk == "" {
		return errors.New("Wi-Fi restore requires an executable, serial, SSID and credential")
	}
	ssid, err := quoteToken(cfg.ssid)
	if err != nil {
		return err
	}
	psk, err := quoteToken(cfg.psk)
	if err != nil {
		return err
	}
	for _, line := range []string{
		"request wifi connect " + ssid + " passphrase " + psk + " security auto timeout 25000",
		"test wifi ssid " + ssid + " require-ip true require-validated true timeout 30000",
	} {
		res := cfg.runShellCommand(45*time.Second, line+" | display json")
		if !shellCommandPassed(res) {
			return fmt.Errorf("Wi-Fi restore failed: code=%d err=%v output=%s", res.Code, res.Err, redact(res.Output, cfg.psk))
		}
	}
	return nil
}

func shellCommandPassed(res commandResult) bool {
	if res.Err != nil || res.Code != 0 {
		return false
	}
	count := 0
	for line := range strings.SplitSeq(res.Output, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "{") {
			continue
		}
		var envelope struct {
			OK *bool `json:"ok"`
		}
		if err := json.Unmarshal([]byte(line), &envelope); err != nil || envelope.OK == nil || !*envelope.OK {
			return false
		}
		count++
	}
	return count == 1
}

func (cfg *e2eConfig) resetLiveState() {
	cfg.forceStopPackage()
}

func (cfg *e2eConfig) resolveBSSID(t *testing.T) string {
	if err := cfg.restoreWiFiConnection(); err != nil {
		t.Fatal(err)
	}
	res := cfg.runShellCommand(25*time.Second, "show wifi status connection")
	if match := bssidPattern.FindStringSubmatch(res.Output); match != nil {
		return strings.ToLower(match[1])
	}
	return ""
}

var bssidPattern = regexp.MustCompile(`(?i)\bbssid(?:\s*:\s*|=)([0-9a-f]{2}(?::[0-9a-f]{2}){5})`)

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
		if tc.Runner == "shell" {
			return true
		}
	}
	return false
}

func liveSetupNeeds(cases []matrixCase) (wifiSetup bool, bssid bool) {
	for _, tc := range cases {
		if tc.Runner != "shell" {
			continue
		}
		if strings.Contains(tc.Command, "<bssid>") {
			bssid = true
			wifiSetup = true
		}
		if caseNeedsWiFiSetup(tc) {
			wifiSetup = true
		}
	}
	return wifiSetup, bssid
}

func caseNeedsWiFiSetup(tc matrixCase) bool {
	commandLine := strings.ToLower(tc.Command)
	if strings.Contains(commandLine, "<ssid>") || strings.Contains(commandLine, "<psk>") || strings.Contains(commandLine, "<bssid>") {
		return true
	}
	wifiOrNetworkCommands := []string{"show wifi", "show network", "request wifi", "test wifi", "test link", "test lab", "monitor wifi", "request ping ", "request traceroute ", "request pmtu ", "request ip external", "request dns resolve ", "request http "}
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

func TestShellCleanupUsesExplicitTransportAndCurrentGrammar(t *testing.T) {
	dir := t.TempDir()
	argsPath, inputPath := filepath.Join(dir, "args"), filepath.Join(dir, "input")
	t.Setenv("DROPCHECK_E2E_TEST_ARGS", argsPath)
	t.Setenv("DROPCHECK_E2E_TEST_INPUT", inputPath)
	bin := filepath.Join(dir, "fake-dropcheck")
	fixture := `#!/bin/sh
printf '%s\n' "$@" >> "$DROPCHECK_E2E_TEST_ARGS"
while IFS= read -r line; do
  printf '%s\n' "$line" >> "$DROPCHECK_E2E_TEST_INPUT"
done
printf '%s\n' '{"ok":true,"lines":[]}'
`
	if err := os.WriteFile(bin, []byte(fixture), 0700); err != nil {
		t.Fatal(err)
	}
	cfg := &e2eConfig{bin: bin, repoRoot: dir, adb: filepath.Join(dir, "not-installed-adb"), packageName: "test.package", serial: "test-serial", ssid: "東京 e\u0301👩\u200d🔬 | \"quoted\"\\path", psk: "秘密 e\u0301👩\u200d🔬 \\\"'|"}
	if err := cfg.restoreWiFiConnection(); err != nil {
		t.Fatal(err)
	}
	args, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	wantArgs := strings.Join([]string{"--adb", cfg.adb, "--package", cfg.packageName, "--serial", cfg.serial, "shell", ""}, "\n")
	if string(args) != wantArgs+wantArgs || strings.Contains(string(args), cfg.psk) {
		t.Fatalf("cleanup escaped the explicit Shell transport: %q", args)
	}
	input, err := os.ReadFile(inputPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(input)), "\n")
	if len(lines) != 4 || lines[1] != "exit" || lines[3] != "exit" || !strings.HasPrefix(lines[0], "request wifi connect ") || !strings.HasPrefix(lines[2], "test wifi ssid ") {
		t.Fatalf("cleanup did not use connect/test/exit: %q", input)
	}
	connect, err := shell.ParseLine(lines[0])
	if err != nil || connect.ConnectOptions == nil || connect.ConnectOptions.SSID != cfg.ssid || connect.ConnectOptions.Passphrase != cfg.psk {
		t.Fatalf("restore changed the SSID/credential literal: %+v (%v)", connect.ConnectOptions, err)
	}
	wait, err := shell.ParseLine(lines[2])
	assertion := wait.Operation.Command.GetAssertWifi()
	if err != nil || wait.Rejection != "" || assertion.GetSsid() != cfg.ssid || !assertion.GetRequireIp() || !assertion.GetRequireValidated() || assertion.GetTimeoutMs() != 30000 {
		t.Fatalf("restore test changed the SSID literal: %+v (%v)", wait, err)
	}
}

func TestShellCleanupRequiresOneTypedSuccessfulEnvelope(t *testing.T) {
	if !shellCommandPassed(commandResult{Output: "startup metadata\n{\"ok\":true,\"lines\":[]}\n"}) {
		t.Fatal("typed success was rejected")
	}
	for _, res := range []commandResult{
		{}, {Output: "STATUS_OK"}, {Output: `{"lines":[]}`}, {Output: `{"ok":false,"lines":[]}`},
		{Output: `{"ok":true`, Code: 0}, {Output: "{\"ok\":true}\n{\"ok\":true}\n"},
		{Output: `{"ok":true}`, Code: 1}, {Output: `{"ok":true}`, Err: context.Canceled},
		{Output: "{\"ok\":false}\n{\"ok\":true}\n"},
	} {
		if shellCommandPassed(res) {
			t.Fatalf("missing, failed or inconsistent cleanup result passed: %+v", res)
		}
	}
}
