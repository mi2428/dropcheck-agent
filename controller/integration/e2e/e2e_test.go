//go:build e2e

// Package e2e runs Dropcheck's parser and real-device end-to-end matrix as Go tests.
//
// Parser and case-table consistency checks do not require a device:
//
//	cd controller
//	go test -tags e2e ./integration/e2e
//
// Full live execution requires an attached Android device and test Wi-Fi:
//
//	make e2e SERIAL=<adb-serial> SSID="<test-ssid>" PSK_ENV=DROPCHECK_E2E_WIFI_PSK
//
// MCP integration and live tests live under integration/mcp. They use the same
// live device, SSID, and PSK environment variables as this e2e matrix.
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
//	DROPCHECK_E2E_STANDALONE_UPLOAD_URL
//	                           Optional MinIO path-style bucket/prefix URL override for the
//	                           standalone upload live test. By default the test starts this
//	                           repo's docker-compose MinIO and reaches it through adb reverse.
//	                           The default MinIO path also fetches the uploaded protobuf and
//	                           evaluates it with the Dropcheck Harness.
//
// The case table is testdata/e2e_cases.tsv. The title column is included in Go
// subtest names, for example E2E-001_shell_help, so verbose output remains readable.
// Commands intentionally use placeholders such as <ssid>, <psk>, <serial>,
// <bssid> and <sync-dir> so lab secrets and machine-local paths are
// not committed. Shell commands prefixed with "request> " or "config> " are
// executed inside the corresponding interactive submode.
package e2e

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	commandparse "dropcheck/controller/internal/command"
	"dropcheck/controller/internal/controlpb"
	f "dropcheck/controller/internal/harness"
	"dropcheck/controller/internal/harness/capabilities"
	"dropcheck/controller/internal/harness/dns"
	"dropcheck/controller/internal/harness/globalip"
	"dropcheck/controller/internal/harness/ip"
	"dropcheck/controller/internal/harness/ping"
	"dropcheck/controller/internal/harness/pmtu"
	"dropcheck/controller/internal/harness/scan"
	"dropcheck/controller/internal/harness/trace"
	"dropcheck/controller/internal/harness/wifi"
	"dropcheck/controller/internal/linuxcli"
	"dropcheck/controller/internal/shell"
	"google.golang.org/protobuf/proto"
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
	envUploadURL  = "DROPCHECK_E2E_STANDALONE_UPLOAD_URL"
	defaultADB    = "adb"
	defaultPkg    = "io.dropcheck.agent"
	defaultPSKEnv = "DROPCHECK_E2E_WIFI_PSK"

	standaloneUploadFesta  = "upload-e2e"
	standaloneFailureFesta = "upload-failure-e2e"
	standaloneArchiveFesta = "archive-e2e"
	standaloneCLIFesta     = "cli-e2e"
	standaloneUploadBucket = "dropcheck"
	standaloneUploadPrefix = "e2e"
	standaloneDNSName      = "example.com"
	standalonePingHost     = "1.1.1.1"
	standaloneHTTPURL      = "http://connectivitycheck.gstatic.com/generate_204"
	defaultMinIOAPIPort    = "8080"

	harnessReplayChildEnv = "DROPCHECK_E2E_HARNESS_REPLAY_CHILD"
)

type matrixCase struct {
	ID        string
	Title     string
	Runner    string
	Command   string
	Expect    string
	Assertion string
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
	uploadURL          string
	bssid              string
	agentPref          string
	forceStopApp       bool
	launchAppActivity  bool
	launchAppEveryCase bool
	managedMinIO       bool

	vars map[string]string
}

type commandResult struct {
	Output string
	Code   int
	Err    error
}

func TestDropcheckEndToEndMatrix(t *testing.T) {
	cases := loadCases(t)
	cfg := loadConfig(t)
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
		t.Run(tc.testName(), func(t *testing.T) {
			start := time.Now()
			commandLine, missing := cfg.expand(tc.Command, tc.Runner)
			if missing != "" {
				t.Skipf("missing runtime value %s for %s", missing, tc.Command)
			}
			expect := tc.Expect
			t.Logf("START %s title=%q runner=%s expect=%s command=%s", tc.ID, tc.Title, tc.Runner, expect, redact(commandLine, cfg.psk))
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
		Checks: standaloneHarnessChecks(),
	}, f.WithADBPath(cfg.adb), f.WithSerial(cfg.serial), f.WithPackageName(cfg.packageName))
}

func TestStandaloneUploadToMinIOLive(t *testing.T) {
	cfg := loadConfig(t)
	requireLiveStandalone(t, cfg, "standalone upload E2E")
	cfg.prepareStandaloneLive(t, "standalone-upload-minio", "Shell standalone upload MinIO")
	uploadURL := cfg.ensureStandaloneUploadURL(t)
	if cfg.managedMinIO {
		cfg.clearStandaloneUploadObjects(t)
	}
	t.Cleanup(func() {
		cfg.runShellCleanup("config> set standalone disabled")
		cfg.runShellCleanup("clear standalone runs all")
		cfg.runShellCleanup("config> delete standalone upload")
		cfg.runShellCleanup("config> delete standalone festa " + standaloneUploadFesta)
	})

	ssid, psk := cfg.ssid, cfg.psk
	t.Logf("standalone upload live target=%s wifi_ssid=%q", uploadURL, ssid)

	cfg.resetStandaloneFesta(t, standaloneUploadFesta)

	commands := []string{
		"config> set standalone upload to " + quoteToken(uploadURL),
		fmt.Sprintf("config> set standalone upload via wifi essid %s passphrase %s security auto band all mac-randomization auto timeout 25000", quoteToken(ssid), quoteToken(psk)),
		fmt.Sprintf("config> set standalone festa %s interval 2s", standaloneUploadFesta),
		fmt.Sprintf("config> set standalone festa %s wifi mgmt match essid %s", standaloneUploadFesta, quoteToken(ssid)),
		fmt.Sprintf("config> set standalone festa %s wifi mgmt passphrase %s security auto", standaloneUploadFesta, quoteToken(psk)),
		fmt.Sprintf("config> set standalone festa %s wifi mgmt band all", standaloneUploadFesta),
		fmt.Sprintf("config> set standalone festa %s wifi mgmt wait ip", standaloneUploadFesta),
		fmt.Sprintf("config> set standalone festa %s wifi mgmt wait validated", standaloneUploadFesta),
		fmt.Sprintf("config> set standalone festa %s wifi mgmt timeout 25000", standaloneUploadFesta),
		fmt.Sprintf("config> set standalone festa %s check dns-main test dns name %s type A timeout 8000", standaloneUploadFesta, standaloneDNSName),
		fmt.Sprintf("config> set standalone festa %s check cloudflare test ping host %s count 1 timeout 8000", standaloneUploadFesta, standalonePingHost),
		fmt.Sprintf("config> set standalone festa %s check healthz test http url %s expected-status 204 timeout 10000", standaloneUploadFesta, standaloneHTTPURL),
		fmt.Sprintf("config> set standalone festa %s enabled", standaloneUploadFesta),
		"config> set standalone enabled",
	}
	for _, line := range commands {
		cfg.runShellLiveCommand(t, line, 90*time.Second)
	}

	status := cfg.waitStandaloneUploadSuccess(t, 2*time.Minute)
	t.Logf("standalone upload succeeded from Android perspective: %s", oneLine(redact(status, psk)))
	if !cfg.managedMinIO {
		t.Logf("skipping MinIO fetch and Harness evaluation because %s overrides the managed local MinIO target", envUploadURL)
		return
	}
	archive := cfg.fetchStandaloneUploadArchiveFromMinIO(t)
	f.Run(t, f.Plan{
		Name: "standalone-minio-eval",
		Results: []f.ResultSource{
			f.StandaloneArchiveBytes("minio-upload", archive),
		},
		Checks: standaloneHarnessChecks(),
	})
}

func TestStandaloneUploadFailureKeepsPendingRunLive(t *testing.T) {
	cfg := loadConfig(t)
	requireLiveStandalone(t, cfg, "standalone upload failure E2E")
	cfg.prepareStandaloneLive(t, "standalone-upload-failure", "Shell standalone upload failure")
	uploadURL, requests := cfg.startStandaloneUploadHTTPServer(t, http.StatusInternalServerError, "forced standalone upload failure")
	t.Cleanup(func() {
		cfg.runShellCleanup("config> set standalone disabled")
		cfg.runShellCleanup("clear standalone runs all")
		cfg.runShellCleanup("config> delete standalone upload")
		cfg.runShellCleanup("config> delete standalone festa " + standaloneFailureFesta)
	})
	cfg.resetStandaloneFesta(t, standaloneFailureFesta)

	ssid, psk := cfg.ssid, cfg.psk
	commands := []string{
		"config> set standalone upload to " + quoteToken(uploadURL),
		fmt.Sprintf("config> set standalone upload via wifi essid %s passphrase %s security auto timeout 25000", quoteToken(ssid), quoteToken(psk)),
		fmt.Sprintf("config> set standalone festa %s interval 2s", standaloneFailureFesta),
		fmt.Sprintf("config> set standalone festa %s wifi mgmt match essid %s", standaloneFailureFesta, quoteToken(ssid)),
		fmt.Sprintf("config> set standalone festa %s wifi mgmt passphrase %s security auto", standaloneFailureFesta, quoteToken(psk)),
		fmt.Sprintf("config> set standalone festa %s wifi mgmt wait ip", standaloneFailureFesta),
		fmt.Sprintf("config> set standalone festa %s wifi mgmt timeout 25000", standaloneFailureFesta),
		fmt.Sprintf("config> set standalone festa %s check cloudflare test ping host %s count 1 timeout 8000", standaloneFailureFesta, standalonePingHost),
		fmt.Sprintf("config> set standalone festa %s enabled", standaloneFailureFesta),
		"config> set standalone enabled",
	}
	for _, line := range commands {
		cfg.runShellLiveCommand(t, line, 90*time.Second)
	}

	status := cfg.waitStandaloneStatusMatch(t, standaloneUploadStopped, 2*time.Minute)
	stored, unsynced := standaloneStatusCounts(t, status)
	if requests.Load() == 0 {
		t.Fatalf("failure upload server did not receive a PUT; status=%s", oneLine(redact(status, psk)))
	}
	if stored == 0 || unsynced == 0 {
		t.Fatalf("failed standalone upload did not leave an unsynced run: stored=%d unsynced=%d status=%s", stored, unsynced, oneLine(redact(status, psk)))
	}
	runs := cfg.runShellLiveCommand(t, "show standalone runs limit 5", 35*time.Second)
	if !strings.Contains(runs, standaloneFailureFesta) || !strings.Contains(runs, "false") {
		t.Fatalf("failed upload run was not visible as unsynced: %s", oneLine(redact(runs, psk)))
	}
}

func TestStandaloneArchiveLifecycleLive(t *testing.T) {
	cfg := loadConfig(t)
	requireLiveStandalone(t, cfg, "standalone archive lifecycle E2E")
	cfg.prepareStandaloneLive(t, "standalone-archive-lifecycle", "Shell standalone archive lifecycle")
	t.Cleanup(func() {
		cfg.runShellCleanup("config> set standalone disabled")
		cfg.runShellCleanup("clear standalone runs all")
		cfg.runShellCleanup("config> delete standalone festa " + standaloneArchiveFesta)
	})
	cfg.resetStandaloneFesta(t, standaloneArchiveFesta)
	cfg.configureStandalonePingFesta(t, standaloneArchiveFesta, "mgmt")

	runOutput := cfg.runShellLiveCommand(t, "request> standalone run once festa "+standaloneArchiveFesta+" save", 90*time.Second)
	runID := requireStandaloneRunID(t, runOutput)
	detail := cfg.runShellLiveCommand(t, "show standalone run "+quoteToken(runID), 35*time.Second)
	assertStandaloneRunDetail(t, detail, runID, standaloneArchiveFesta, false)

	keepDir := filepath.Join(t.TempDir(), "keep")
	syncKeep := cfg.runShellLiveCommand(t, "sync standalone runs output "+quoteToken(keepDir)+" limit 1 keep-unsynced", 60*time.Second)
	assertSyncedArchiveFile(t, keepDir, runID, syncKeep)
	detail = cfg.runShellLiveCommand(t, "show standalone run "+quoteToken(runID), 35*time.Second)
	assertStandaloneRunDetail(t, detail, runID, standaloneArchiveFesta, false)

	markDir := filepath.Join(t.TempDir(), "mark")
	syncMark := cfg.runShellLiveCommand(t, "sync standalone runs output "+quoteToken(markDir)+" limit 1 mark-synced", 60*time.Second)
	assertSyncedArchiveFile(t, markDir, runID, syncMark)
	detail = cfg.runShellLiveCommand(t, "show standalone run "+quoteToken(runID), 35*time.Second)
	assertStandaloneRunDetail(t, detail, runID, standaloneArchiveFesta, true)
}

func TestStandaloneCLIParityLive(t *testing.T) {
	cfg := loadConfig(t)
	requireLiveStandalone(t, cfg, "standalone CLI parity E2E")
	cfg.prepareStandaloneLive(t, "standalone-cli-parity", "CLI standalone parity")
	t.Cleanup(func() {
		cfg.runShellCleanup("config> set standalone disabled")
		cfg.runShellCleanup("clear standalone runs all")
		cfg.runShellCleanup("config> delete standalone festa " + standaloneCLIFesta)
	})
	cfg.resetStandaloneFesta(t, standaloneCLIFesta)

	cfg.runCLILiveCommand(t, 45*time.Second, "configure", "set", "standalone", "festa", standaloneCLIFesta, "wifi", "mgmt", "match", "essid", cfg.ssid)
	cfg.runCLILiveCommand(t, 45*time.Second, "configure", "set", "standalone", "festa", standaloneCLIFesta, "wifi", "mgmt", "passphrase", cfg.psk, "security", "auto")
	cfg.runCLILiveCommand(t, 45*time.Second, "configure", "set", "standalone", "festa", standaloneCLIFesta, "wifi", "mgmt", "wait", "ip")
	cfg.runCLILiveCommand(t, 45*time.Second, "configure", "set", "standalone", "festa", standaloneCLIFesta, "check", "cloudflare", "test", "ping", "host", standalonePingHost, "count", "1", "timeout", "8000")
	cfg.runCLILiveCommand(t, 45*time.Second, "configure", "set", "standalone", "festa", standaloneCLIFesta, "enabled")

	runOutput := cfg.runCLILiveCommand(t, 90*time.Second, "request", "standalone", "run", "once", "--festa", standaloneCLIFesta, "--save")
	runID := requireStandaloneRunID(t, runOutput)
	status := cfg.runCLILiveCommand(t, 35*time.Second, "show", "standalone", "status")
	if !standaloneStatusRendered(status) {
		t.Fatalf("CLI standalone status did not render standalone status: %s", oneLine(redact(status, cfg.psk)))
	}
	detail := cfg.runCLILiveCommand(t, 35*time.Second, "show", "standalone", "run", runID)
	assertStandaloneRunDetail(t, detail, runID, standaloneCLIFesta, false)
}

func TestHarnessStandaloneResultReplayScenarios(t *testing.T) {
	archive := standaloneReplayArchiveFixture()
	f.Run(t, f.Plan{
		Name: "standalone-replay-e2e-fixture",
		Results: []f.ResultSource{
			f.StandaloneArchive("full-standalone-archive", archive),
			f.StandaloneArchiveBytes("full-standalone-archive-bytes", mustMarshalStandaloneArchive(t, archive)),
		},
		Checks: standaloneReplayChecks(),
	})

	for _, tc := range []struct {
		name string
		want string
	}{
		{name: "missing_wait", want: "wait_connected missing from standalone result"},
		{name: "failed_connect", want: "connect status=STATUS_FAILED"},
		{name: "missing_ping", want: "no archived ping step"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runHarnessReplayFailureChild(t, tc.name, tc.want)
		})
	}
}

func TestHarnessStandaloneResultReplayFailureChild(t *testing.T) {
	name := os.Getenv(harnessReplayChildEnv)
	if name == "" {
		t.Skipf("set %s to run a failing replay fixture child", harnessReplayChildEnv)
	}
	archive := standaloneReplayArchiveFixture()
	switch name {
	case "missing_wait":
		archive.Steps = removeStandaloneStep(archive.GetSteps(), "wait_connected")
	case "failed_connect":
		step := archive.GetSteps()[0]
		step.Result = &controlpb.CommandResult{
			Status:  controlpb.CommandResult_STATUS_FAILED,
			Message: "forced connect failure",
		}
	case "missing_ping":
		archive.Steps = removeStandaloneStep(archive.GetSteps(), "ping")
	default:
		t.Fatalf("unknown replay failure fixture %q", name)
	}
	f.Run(t, f.Plan{
		Name: "standalone-replay-failure-" + name,
		Results: []f.ResultSource{
			f.StandaloneArchive(name, archive),
		},
		Checks: standaloneReplayChecks(),
	})
}

func standaloneHarnessChecks() []f.Check {
	return []f.Check{
		f.DNS(standaloneDNSName).
			A().
			Timeout(8*time.Second).
			Expect(dns.AnswerCount().Ge(1), dns.Elapsed().Le(8*time.Second)),
		f.Ping(standalonePingHost).
			Count(1).
			Timeout(8 * time.Second).
			Expect(ping.Assert("payload matches request", func(result ping.Result) error {
				if result.Host != standalonePingHost {
					return fmt.Errorf("host=%s want %s", result.Host, standalonePingHost)
				}
				if result.Count != 1 {
					return fmt.Errorf("count=%d want 1", result.Count)
				}
				return nil
			})),
		f.HTTP(standaloneHTTPURL).
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

func standaloneReplayChecks() []f.Check {
	checks := []f.Check{
		f.IPStatus().
			Expect(
				ip.Validated().IsTrue(),
				ip.Internet().IsTrue(),
				ip.IPv4Address().InCIDR("192.168.10.0/24"),
				ip.MTU().Ge(1280),
			),
		f.WiFiStatus().
			Expect(
				wifi.Enabled().IsTrue(),
				wifi.SSID().Eq("Lab"),
				wifi.BSSID().Eq("aa:bb:cc:dd:ee:ff"),
				wifi.Standard().Eq("be"),
				wifi.Band().Eq("6ghz"),
			),
		f.WiFiScan().
			Fresh().
			Band("6ghz").
			Timeout(5 * time.Second).
			Expect(
				scan.APs().
					SSID("Lab").
					BSSID("aa:bb:cc:dd:ee:ff").
					Standard("be").
					Channel(37).
					Security("wpa3_sae").
					Exists(),
			),
		f.WiFiCapabilities().
			Expect(
				capabilities.Band("6ghz").Supported(),
				capabilities.Standard("be").Supported(),
				capabilities.Security("wpa3_sae").Supported(),
				capabilities.ErrorCount().Eq(0),
			),
		f.GlobalIP().
			IPv4().
			Expect(globalip.AddressCount().Ge(1)),
		f.PathMTU("8.8.8.8").
			Min(1200).
			Max(1500).
			Expect(pmtu.Discovered().IsTrue(), pmtu.PathMTU().Ge(1200)),
		f.Traceroute("8.8.8.8").
			MaxHops(30).
			Expect(trace.OutputContains("8.8.8.8")),
	}
	return append(checks, standaloneHarnessChecks()...)
}

func requireLiveStandalone(t *testing.T, cfg *e2eConfig, name string) {
	t.Helper()
	if !cfg.live {
		t.Skipf("set %s=1 to run live %s", envLive, name)
	}
	if cfg.serial == "" {
		t.Skipf("%s or ADB_SERIAL is required for %s", envSerial, name)
	}
	if cfg.ssid == "" || cfg.psk == "" {
		t.Skipf("%s and %s are required for %s", envSSID, envPSK, name)
	}
}

func (cfg *e2eConfig) prepareStandaloneLive(t *testing.T, id string, title string) {
	t.Helper()
	cfg.prepareLive(t, []matrixCase{{
		ID:      id,
		Title:   title,
		Runner:  "shell",
		Command: `request> wifi connect passphrase <psk> security auto timeout 25000 "<ssid>"`,
		Expect:  "ok",
	}})
}

func (cfg *e2eConfig) resetStandaloneFesta(t *testing.T, festa string) {
	t.Helper()
	for _, line := range []string{
		"config> set standalone disabled",
		"clear standalone runs all",
		"config> delete standalone upload",
		"config> delete standalone festa " + festa,
	} {
		cfg.runShellLiveCommand(t, line, 45*time.Second)
	}
}

func (cfg *e2eConfig) configureStandalonePingFesta(t *testing.T, festa string, group string) {
	t.Helper()
	ssid, psk := cfg.ssid, cfg.psk
	commands := []string{
		fmt.Sprintf("config> set standalone festa %s interval 2s", festa),
		fmt.Sprintf("config> set standalone festa %s wifi %s match essid %s", festa, group, quoteToken(ssid)),
		fmt.Sprintf("config> set standalone festa %s wifi %s passphrase %s security auto", festa, group, quoteToken(psk)),
		fmt.Sprintf("config> set standalone festa %s wifi %s band all", festa, group),
		fmt.Sprintf("config> set standalone festa %s wifi %s wait ip", festa, group),
		fmt.Sprintf("config> set standalone festa %s wifi %s timeout 25000", festa, group),
		fmt.Sprintf("config> set standalone festa %s check cloudflare test ping host %s count 1 timeout 8000", festa, standalonePingHost),
		fmt.Sprintf("config> set standalone festa %s enabled", festa),
	}
	for _, line := range commands {
		cfg.runShellLiveCommand(t, line, 90*time.Second)
	}
}

func (cfg *e2eConfig) runCLILiveCommand(t *testing.T, timeout time.Duration, args ...string) string {
	t.Helper()
	fullArgs := append([]string{"--serial", cfg.serial}, args...)
	res := cfg.runExternal(timeout, fullArgs, "")
	output := redact(res.Output, cfg.psk)
	if res.Err != nil || res.Code != 0 {
		t.Fatalf("live CLI command failed: args=%q rc=%d err=%v output=%s", fullArgs, res.Code, res.Err, output)
	}
	if isShellErrorOutput(res.Output) || isFailureStatusOutput(res.Output) {
		t.Fatalf("live CLI command returned failure: args=%q output=%s", fullArgs, output)
	}
	return res.Output
}

func (cfg *e2eConfig) waitStandaloneStatusMatch(t *testing.T, pattern *regexp.Regexp, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last string
	for time.Now().Before(deadline) {
		last = cfg.runShellLiveCommand(t, "show standalone status", 35*time.Second)
		if pattern.MatchString(last) {
			return last
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("standalone status did not match %s within %s; last=%s", pattern, timeout, oneLine(redact(last, cfg.psk)))
	return ""
}

func (cfg *e2eConfig) startStandaloneUploadHTTPServer(t *testing.T, status int, body string) (string, *atomic.Int32) {
	t.Helper()
	requests := &atomic.Int32{}
	listener, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatalf("listen for standalone upload failure server: %v", err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			requests.Add(1)
		}
		_, _ = io.Copy(io.Discard, r.Body)
		_ = r.Body.Close()
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})}
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Logf("standalone upload HTTP server stopped: %v", err)
		}
	}()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	})

	port := strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
	if cfg.setupADBReverse(t, port) {
		return fmt.Sprintf("http://127.0.0.1:%s/dropcheck/failure", port), requests
	}
	host := hostIPv4Address()
	if host == "" {
		t.Fatalf("adb reverse failed and no non-loopback IPv4 address was found for failure upload server")
	}
	return fmt.Sprintf("http://%s:%s/dropcheck/failure", host, port), requests
}

func standaloneStatusCounts(t *testing.T, status string) (stored int, unsynced int) {
	t.Helper()
	match := standaloneStatusLine.FindStringSubmatch(status)
	if match != nil {
		stored = parsePositiveInt(t, match[1], "stored")
		unsynced = parsePositiveInt(t, match[2], "unsynced")
		return stored, unsynced
	}

	storedValue, ok := standaloneSectionKVValue(status, "Standalone", "stored")
	if !ok {
		t.Fatalf("standalone stored count not found: %s", oneLine(status))
	}
	unsyncedValue, ok := standaloneSectionKVValue(status, "Standalone", "unsynced")
	if !ok {
		t.Fatalf("standalone unsynced count not found: %s", oneLine(status))
	}
	stored = parsePositiveInt(t, storedValue, "stored")
	unsynced = parsePositiveInt(t, unsyncedValue, "unsynced")
	return stored, unsynced
}

func parsePositiveInt(t *testing.T, value string, name string) int {
	t.Helper()
	parsed, err := strconv.Atoi(value)
	if err != nil {
		t.Fatalf("parse %s count %q: %v", name, value, err)
	}
	return parsed
}

func requireStandaloneRunID(t *testing.T, output string) string {
	t.Helper()
	runID, ok := standaloneRunIDFromOutput(output)
	if !ok {
		t.Fatalf("standalone run id not found in output: %s", oneLine(output))
	}
	return runID
}

func assertStandaloneRunDetail(t *testing.T, output string, runID string, festa string, synced bool) {
	t.Helper()
	if strings.Contains(output, "Standalone run: id="+runID) {
		for _, want := range []string{
			"synced=" + strconv.FormatBool(synced),
			"festa=" + festa,
			"ping",
		} {
			if !strings.Contains(output, want) {
				t.Fatalf("standalone run detail missing %q: %s", want, oneLine(output))
			}
		}
		return
	}

	for _, want := range []struct {
		key   string
		value string
	}{
		{key: "id", value: runID},
		{key: "synced", value: strconv.FormatBool(synced)},
		{key: "festa", value: festa},
	} {
		got, ok := standaloneSectionKVValue(output, "Standalone Run", want.key)
		if !ok || got != want.value {
			t.Fatalf("standalone run detail %s=%q, want %q: %s", want.key, got, want.value, oneLine(output))
		}
	}
	if !strings.Contains(output, "Steps") || !strings.Contains(output, "cloudflare") {
		t.Fatalf("standalone run detail missing cloudflare step: %s", oneLine(output))
	}
}

func standaloneRunIDFromOutput(output string) (string, bool) {
	if match := standaloneRunIDLegacy.FindStringSubmatch(output); match != nil {
		return match[1], true
	}
	return standaloneSectionKVValue(output, "Standalone Run", "id")
}

func standaloneSectionKVValue(output string, section string, key string) (string, bool) {
	inSection := false
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if !inSection {
			if trimmed == section {
				inSection = true
			}
			continue
		}
		if trimmed == "" {
			continue
		}
		if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			return "", false
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == key {
			return fields[1], true
		}
	}
	return "", false
}

func standaloneStatusRendered(output string) bool {
	if standaloneStatusLine.MatchString(output) {
		return true
	}
	if !regexp.MustCompile(`(?m)^Standalone\s*$`).MatchString(output) {
		return false
	}
	if _, ok := standaloneSectionKVValue(output, "Standalone", "enabled"); !ok {
		return false
	}
	if _, ok := standaloneSectionKVValue(output, "Standalone", "stored"); !ok {
		return false
	}
	return true
}

func assertSyncedArchiveFile(t *testing.T, outputDir string, runID string, syncOutput string) {
	t.Helper()
	var matches []string
	if err := filepath.WalkDir(outputDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".json" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(data), runID) {
			matches = append(matches, path)
		}
		return nil
	}); err != nil {
		t.Fatalf("walk synced archive dir %s: %v", outputDir, err)
	}
	if len(matches) != 1 {
		t.Fatalf("synced archive files containing %s = %d, want 1; sync output=%s", runID, len(matches), oneLine(syncOutput))
	}
}

func runHarnessReplayFailureChild(t *testing.T, name string, want string) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestHarnessStandaloneResultReplayFailureChild$", "-test.v")
	cmd.Env = append(os.Environ(), harnessReplayChildEnv+"="+name)
	cmd.Dir = packageDir(t)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("harness replay failure child %s unexpectedly passed:\n%s", name, out)
	}
	if !strings.Contains(string(out), want) {
		t.Fatalf("harness replay failure child %s output missing %q:\n%s", name, want, out)
	}
}

func mustMarshalStandaloneArchive(t *testing.T, archive *controlpb.StandaloneRunArchive) []byte {
	t.Helper()
	data, err := proto.Marshal(archive)
	if err != nil {
		t.Fatalf("marshal standalone archive fixture: %v", err)
	}
	return data
}

func removeStandaloneStep(steps []*controlpb.StandaloneMeasurementStep, name string) []*controlpb.StandaloneMeasurementStep {
	filtered := make([]*controlpb.StandaloneMeasurementStep, 0, len(steps))
	for _, step := range steps {
		if step.GetStepName() != name {
			filtered = append(filtered, step)
		}
	}
	return filtered
}

func standaloneReplayArchiveFixture() *controlpb.StandaloneRunArchive {
	const (
		group = "lab"
		ssid  = "Lab"
	)
	selector := &controlpb.NetworkSelector{Ssid: ssid}
	steps := []*controlpb.StandaloneMeasurementStep{
		standaloneReplayStep(1, group, 1, "connect", &controlpb.RunCommand{
			Label: "standalone connect lab",
			Command: &controlpb.RunCommand_ConnectWifi{ConnectWifi: &controlpb.ConnectWifi{
				Ssid:       ssid,
				Passphrase: "secret",
				Security:   controlpb.ConnectWifi_SECURITY_WPA2_PSK,
				Band:       controlpb.WifiBand_WIFI_BAND_5_GHZ,
				TimeoutMs:  35000,
			}},
		}, &controlpb.CommandResult{
			Status:  controlpb.CommandResult_STATUS_OK,
			Message: "connected",
			Payload: &controlpb.CommandResult_ConnectWifi{ConnectWifi: &controlpb.ConnectWifiResult{
				Ssid:      ssid,
				Connected: true,
			}},
		}),
		standaloneReplayStep(1, group, 2, "wait_connected", &controlpb.RunCommand{
			Label: "standalone wait lab",
			Command: &controlpb.RunCommand_WaitWifiConnected{WaitWifiConnected: &controlpb.WaitWifiConnected{
				Ssid:             ssid,
				Security:         controlpb.ConnectWifi_SECURITY_WPA2_PSK,
				Band:             controlpb.WifiBand_WIFI_BAND_5_GHZ,
				RequireIp:        true,
				RequireValidated: true,
				TimeoutMs:        35000,
			}},
		}, &controlpb.CommandResult{
			Status:  controlpb.CommandResult_STATUS_OK,
			Message: "connected",
			Payload: &controlpb.CommandResult_WifiAssert{WifiAssert: &controlpb.WifiAssertResult{
				Passed: true,
			}},
		}),
		standaloneReplayStep(1, group, 3, "ip", &controlpb.RunCommand{
			Label:   "standalone ip status",
			Command: &controlpb.RunCommand_GetIpStatus{GetIpStatus: &controlpb.GetIpStatus{Selector: selector}},
		}, standaloneReplayResult("ip.status")),
		standaloneReplayStep(1, group, 4, "wifi", &controlpb.RunCommand{
			Label:   "standalone wifi status",
			Command: &controlpb.RunCommand_GetWifiStatus{GetWifiStatus: &controlpb.GetWifiStatus{}},
		}, standaloneReplayResult("wifi.status")),
		standaloneReplayStep(1, group, 5, "wifi_scan", &controlpb.RunCommand{
			Label: "standalone wifi scan fresh",
			Command: &controlpb.RunCommand_GetFreshWifiScan{GetFreshWifiScan: &controlpb.GetFreshWifiScan{
				Band:      controlpb.WifiBand_WIFI_BAND_6_GHZ,
				TimeoutMs: 5000,
			}},
		}, standaloneReplayResult("wifi.scan.fresh")),
		standaloneReplayStep(1, group, 6, "wifi_capabilities", &controlpb.RunCommand{
			Label:   "standalone wifi capabilities",
			Command: &controlpb.RunCommand_GetWifiCapabilities{GetWifiCapabilities: &controlpb.GetWifiCapabilities{}},
		}, standaloneReplayResult("wifi.capabilities")),
		standaloneReplayStep(1, group, 7, "global_ip", &controlpb.RunCommand{
			Label: "standalone global-ip",
			Command: &controlpb.RunCommand_GlobalIp{GlobalIp: &controlpb.GlobalIp{
				Family:    controlpb.IpFamily_IP_FAMILY_IPV4,
				TimeoutMs: 10000,
				Selector:  selector,
			}},
		}, standaloneReplayResult("global-ip")),
		standaloneReplayStep(1, group, 8, "path_mtu", &controlpb.RunCommand{
			Label: "standalone path-mtu 8.8.8.8",
			Command: &controlpb.RunCommand_PathMtu{PathMtu: &controlpb.PathMtu{
				Host:        "8.8.8.8",
				TimeoutMs:   20000,
				Selector:    selector,
				MinMtuBytes: 1200,
				MaxMtuBytes: 1500,
			}},
		}, standaloneReplayResult("path-mtu")),
		standaloneReplayStep(1, group, 9, "traceroute", &controlpb.RunCommand{
			Label: "standalone traceroute 8.8.8.8",
			Command: &controlpb.RunCommand_Traceroute{Traceroute: &controlpb.Traceroute{
				Host:      "8.8.8.8",
				MaxHops:   30,
				TimeoutMs: 30000,
				Selector:  selector,
			}},
		}, standaloneReplayResult("traceroute")),
		standaloneReplayStep(1, group, 10, "dns", &controlpb.RunCommand{
			Label: "standalone dns " + standaloneDNSName,
			Command: &controlpb.RunCommand_ResolveDns{ResolveDns: &controlpb.ResolveDns{
				Name:      standaloneDNSName,
				Qtypes:    []controlpb.DnsRecordType{controlpb.DnsRecordType_DNS_RECORD_TYPE_A},
				TimeoutMs: 8000,
				Selector:  selector,
			}},
		}, standaloneReplayResult("dns")),
		standaloneReplayStep(1, group, 11, "ping", &controlpb.RunCommand{
			Label: "standalone ping " + standalonePingHost,
			Command: &controlpb.RunCommand_Ping{Ping: &controlpb.Ping{
				Host:      standalonePingHost,
				Count:     1,
				TimeoutMs: 8000,
				Selector:  selector,
			}},
		}, standaloneReplayResult("ping")),
		standaloneReplayStep(1, group, 12, "http", &controlpb.RunCommand{
			Label: "standalone http " + standaloneHTTPURL,
			Command: &controlpb.RunCommand_HttpCheck{HttpCheck: &controlpb.HttpCheck{
				Url:            standaloneHTTPURL,
				ExpectedStatus: 204,
				TimeoutMs:      10000,
				Selector:       selector,
			}},
		}, standaloneReplayResult("http")),
	}
	return &controlpb.StandaloneRunArchive{
		Summary: &controlpb.StandaloneRunSummary{
			RunId:           "replay-run-1",
			FestaName:       "replay",
			Status:          "ok",
			WifiGroupCount:  1,
			StepCount:       uint32(len(steps)),
			FailedStepCount: 0,
		},
		Festa: &controlpb.StandaloneFesta{
			Name: "replay",
			WifiGroups: []*controlpb.StandaloneWifiGroup{{
				Name:             group,
				Essid:            ssid,
				Passphrase:       "secret",
				Security:         controlpb.ConnectWifi_SECURITY_WPA2_PSK,
				Band:             controlpb.WifiBand_WIFI_BAND_5_GHZ,
				RequireIp:        true,
				RequireValidated: true,
			}},
		},
		Steps: steps,
		Device: &controlpb.DeviceInfo{
			Manufacturer: "Dropcheck",
			Model:        "Replay",
		},
	}
}

func standaloneReplayStep(groupIndex uint32, groupName string, stepIndex uint32, name string, command *controlpb.RunCommand, result *controlpb.CommandResult) *controlpb.StandaloneMeasurementStep {
	return &controlpb.StandaloneMeasurementStep{
		WifiGroupIndex: groupIndex,
		WifiGroupName:  groupName,
		StepIndex:      stepIndex,
		StepName:       name,
		Attempt:        1,
		Command:        command,
		Result:         result,
	}
}

func standaloneReplayResult(name string) *controlpb.CommandResult {
	result := &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_OK}
	switch name {
	case "ip.status":
		result.Payload = &controlpb.CommandResult_IpStatus{IpStatus: &controlpb.IpStatus{
			NetworkId:         "100",
			Transports:        []string{"wifi"},
			Validated:         true,
			Internet:          true,
			InterfaceName:     "wlan0",
			Mtu:               1500,
			Addresses:         []string{"192.168.10.23/24", "fe80::123/64"},
			DnsServers:        []string{"192.168.10.1"},
			DhcpServer:        "192.168.10.1",
			Routes:            []string{"0.0.0.0/0 -> 192.168.10.1 wlan0"},
			Capabilities:      []string{"internet", "validated"},
			RawLinkProperties: "LinkProperties{LinkAddresses: [192.168.10.23/24]}",
		}}
	case "wifi.status":
		result.Payload = &controlpb.CommandResult_WifiStatus{WifiStatus: &controlpb.WifiStatus{
			Enabled: true,
			State:   "enabled",
			Connection: &controlpb.WifiConnection{
				Ssid:            "Lab",
				Bssid:           "aa:bb:cc:dd:ee:ff",
				RssiDbm:         -45,
				FrequencyMhz:    6135,
				LinkSpeedMbps:   2401,
				TxLinkSpeedMbps: 2401,
				RxLinkSpeedMbps: 2401,
				WifiStandard:    "802.11be",
				ChannelWidth:    "160MHz",
				SecurityType:    "wpa3_sae",
			},
		}}
	case "wifi.scan.fresh":
		result.Payload = &controlpb.CommandResult_WifiScan{WifiScan: &controlpb.WifiScan{
			Results: []*controlpb.WifiScanResult{{
				Ssid:          "Lab",
				Bssid:         "aa:bb:cc:dd:ee:ff",
				Capabilities:  "[RSN-SAE-CCMP][EHT][ESS]",
				RssiDbm:       -41,
				FrequencyMhz:  6135,
				Band:          "6GHz",
				ChannelWidth:  "320MHz",
				WifiStandard:  "802.11be",
				SecurityTypes: []string{"wpa3_sae"},
			}},
		}}
	case "wifi.capabilities":
		result.Payload = &controlpb.CommandResult_WifiCapabilities{WifiCapabilities: &controlpb.WifiCapabilities{
			SupportedBands:         []string{"2.4GHz", "5GHz", "6GHz"},
			SupportedStandards:     []string{"802.11ax", "802.11be"},
			SupportedSecurityModes: []string{"wpa3_sae"},
		}}
	case "global-ip":
		result.Payload = &controlpb.CommandResult_GlobalIp{GlobalIp: &controlpb.GlobalIpResult{
			RequestedFamily: controlpb.IpFamily_IP_FAMILY_IPV4,
			ElapsedMs:       100,
			Addresses: []*controlpb.GlobalIpAddress{{
				Family: controlpb.IpFamily_IP_FAMILY_IPV4,
				Ip:     "203.0.113.10",
				Global: true,
				Status: 200,
			}},
		}}
	case "path-mtu":
		result.Payload = &controlpb.CommandResult_PathMtu{PathMtu: &controlpb.PathMtuResult{
			Host:         "8.8.8.8",
			Discovered:   true,
			PathMtuBytes: 1400,
			Probes: []*controlpb.PathMtuProbe{{
				MtuBytes: 1400,
				Passed:   true,
			}},
		}}
	case "traceroute":
		result.Payload = &controlpb.CommandResult_Traceroute{Traceroute: &controlpb.TracerouteResult{
			Host:      "8.8.8.8",
			MaxHops:   30,
			Output:    "1 192.0.2.1 1.0 ms\n2 8.8.8.8 5.0 ms\n",
			ElapsedMs: 500,
		}}
	case "dns":
		result.Payload = &controlpb.CommandResult_ResolveDns{ResolveDns: &controlpb.ResolveDnsResult{
			Name:      standaloneDNSName,
			ElapsedMs: 80,
			Answers: []*controlpb.DnsAnswer{{
				Type:    controlpb.DnsRecordType_DNS_RECORD_TYPE_A,
				Address: "93.184.216.34",
			}},
		}}
	case "ping":
		result.Payload = &controlpb.CommandResult_Ping{Ping: &controlpb.PingResult{
			Host:              standalonePingHost,
			Count:             1,
			Transmitted:       1,
			Received:          1,
			PacketLossPercent: 0,
			MinMs:             10,
			AvgMs:             20,
			MaxMs:             30,
			ElapsedMs:         80,
		}}
	case "http":
		result.Payload = &controlpb.CommandResult_HttpCheck{HttpCheck: &controlpb.HttpCheckResult{
			Url:            standaloneHTTPURL,
			Status:         204,
			ExpectedStatus: 204,
			Matched:        true,
			ElapsedMs:      100,
		}}
	default:
		result.Message = name
	}
	return result
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
		if len(row) != 6 {
			t.Fatalf("e2e case row %d has %d fields, want 6: %#v", i+2, len(row), row)
		}
		tc := matrixCase{
			ID:        row[0],
			Title:     row[1],
			Runner:    row[2],
			Command:   row[3],
			Expect:    row[4],
			Assertion: row[5],
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
		uploadURL:          os.Getenv(envUploadURL),
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
	if after, ok := strings.CutPrefix(commandLine, marker); ok {
		return after, true
	}
	return commandLine, false
}

func configureModeCommand(commandLine string) (string, bool) {
	const marker = "config> "
	if after, ok := strings.CutPrefix(commandLine, marker); ok {
		return after, true
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

func (cfg *e2eConfig) runShellLiveCommand(t *testing.T, commandLine string, timeout time.Duration) string {
	t.Helper()
	res := cfg.runExternal(timeout, []string{"--serial", cfg.serial, "shell"}, shellInput(commandLine)+"quit\n")
	output := redact(res.Output, cfg.psk)
	if res.Err != nil || res.Code != 0 {
		t.Fatalf("live shell command failed: command=%s rc=%d err=%v output=%s", redact(commandLine, cfg.psk), res.Code, res.Err, output)
	}
	if isShellErrorOutput(res.Output) || isFailureStatusOutput(res.Output) {
		t.Fatalf("live shell command returned failure: command=%s output=%s", redact(commandLine, cfg.psk), output)
	}
	return res.Output
}

func (cfg *e2eConfig) waitStandaloneUploadSuccess(t *testing.T, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last string
	for time.Now().Before(deadline) {
		last = cfg.runShellLiveCommand(t, "show standalone status", 35*time.Second)
		if standaloneUploadSuccess.MatchString(last) {
			return last
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("standalone upload did not report HTTP 20x success within %s; last status=%s", timeout, oneLine(redact(last, cfg.psk)))
	return ""
}

func (cfg *e2eConfig) ensureStandaloneUploadURL(t *testing.T) string {
	t.Helper()
	if cfg.uploadURL != "" {
		return strings.TrimRight(cfg.uploadURL, "/")
	}
	port := envOr("MINIO_API_PORT", defaultMinIOAPIPort)
	cfg.startMinIO(t, port)
	cfg.managedMinIO = true
	waitHTTPReady(t, fmt.Sprintf("http://127.0.0.1:%s/minio/health/ready", port), 90*time.Second)

	if cfg.setupADBReverse(t, port) {
		cfg.uploadURL = fmt.Sprintf("http://127.0.0.1:%s/%s/%s", port, standaloneUploadBucket, standaloneUploadPrefix)
		return cfg.uploadURL
	}
	host := hostIPv4Address()
	if host == "" {
		t.Fatalf("adb reverse failed and no non-loopback IPv4 address was found; set %s explicitly", envUploadURL)
	}
	cfg.uploadURL = fmt.Sprintf("http://%s:%s/%s/%s", host, port, standaloneUploadBucket, standaloneUploadPrefix)
	return cfg.uploadURL
}

func (cfg *e2eConfig) startMinIO(t *testing.T, port string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "compose", "-f", "docker-compose.test.yml", "up", "-d", "minio", "minio-init")
	cmd.Dir = cfg.repoRoot
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	if err != nil {
		t.Fatalf("start MinIO on host port %s: %v\n%s", port, err, out)
	}
	t.Logf("MinIO started for standalone upload E2E on host port %s", port)
}

func (cfg *e2eConfig) clearStandaloneUploadObjects(t *testing.T) {
	t.Helper()
	script := minIOAliasScript() + fmt.Sprintf(`
mc rm --recursive --force "local/${MINIO_BUCKET:-%s}/%s" >/dev/null 2>&1 || true
`, standaloneUploadBucket, standaloneUploadPrefix)
	cfg.runMinIOClient(t, "", script)
}

func (cfg *e2eConfig) fetchStandaloneUploadArchiveFromMinIO(t *testing.T) []byte {
	t.Helper()
	tmpRoot := filepath.Join(cfg.repoRoot, ".tmp")
	if err := os.MkdirAll(tmpRoot, 0o755); err != nil {
		t.Fatalf("create MinIO fetch temp root: %v", err)
	}
	outDir, err := os.MkdirTemp(tmpRoot, "e2e-minio-")
	if err != nil {
		t.Fatalf("create MinIO fetch temp dir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(outDir)
	})
	script := minIOAliasScript() + fmt.Sprintf(`
object="$(mc find "local/${MINIO_BUCKET:-%s}/%s" --name "*.pb" 2>/dev/null | sort | tail -n 1 || true)"
if [ -z "$object" ]; then
  echo "no standalone protobuf objects found under %s" >&2
  exit 1
fi
mc cp "$object" /out/standalone.pb >/dev/null
printf 'object=%%s\n' "$object"
`, standaloneUploadBucket, standaloneUploadPrefix, standaloneUploadPrefix)
	out := cfg.runMinIOClient(t, outDir, script)
	path := filepath.Join(outDir, "standalone.pb")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fetched standalone archive %s: %v; mc output=%s", path, err, oneLine(out))
	}
	if len(data) == 0 {
		t.Fatalf("fetched standalone archive is empty; mc output=%s", oneLine(out))
	}
	t.Logf("fetched standalone archive from MinIO: bytes=%d %s", len(data), oneLine(out))
	return data
}

func minIOAliasScript() string {
	return `set -eu
mc alias set local http://minio:9000 "${MINIO_ROOT_USER:-dropcheck}" "${MINIO_ROOT_PASSWORD:-dropcheck-secret}" >/dev/null
`
}

func (cfg *e2eConfig) runMinIOClient(t *testing.T, outputDir string, script string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	args := []string{"compose", "-f", "docker-compose.test.yml", "run", "--rm", "-T", "--entrypoint", "/bin/sh"}
	if outputDir != "" {
		args = append(args, "-v", outputDir+":/out")
	}
	args = append(args, "minio-init", "-c", script)
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Dir = cfg.repoRoot
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	if err != nil {
		t.Fatalf("run MinIO client command: %v\n%s", err, out)
	}
	return string(out)
}

func waitHTTPReady(t *testing.T, url string, timeout time.Duration) {
	t.Helper()
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(timeout)
	var last string
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if resp != nil {
			last = resp.Status
			_ = resp.Body.Close()
		}
		if err == nil && resp != nil && resp.StatusCode >= 200 && resp.StatusCode <= 299 {
			return
		}
		if err != nil {
			last = err.Error()
		}
		time.Sleep(1 * time.Second)
	}
	t.Fatalf("MinIO health endpoint %s was not ready within %s; last=%s", url, timeout, last)
}

func (cfg *e2eConfig) setupADBReverse(t *testing.T, port string) bool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	spec := "tcp:" + port
	cmd := exec.CommandContext(ctx, cfg.adb, "-s", cfg.serial, "reverse", spec, spec)
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	if err != nil {
		t.Logf("adb reverse %s failed: %v output=%s", spec, err, oneLine(string(out)))
		return false
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = exec.CommandContext(ctx, cfg.adb, "-s", cfg.serial, "reverse", "--remove", spec).Run()
	})
	return true
}

func hostIPv4Address() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, preferPrivate := range []bool{true, false} {
		for _, iface := range ifaces {
			if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
				continue
			}
			addrs, err := iface.Addrs()
			if err != nil {
				continue
			}
			for _, addr := range addrs {
				ip := ipv4FromAddr(addr)
				if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
					continue
				}
				if preferPrivate && !ip.IsPrivate() {
					continue
				}
				return ip.String()
			}
		}
	}
	return ""
}

func ipv4FromAddr(addr net.Addr) net.IP {
	switch v := addr.(type) {
	case *net.IPNet:
		return v.IP.To4()
	case *net.IPAddr:
		return v.IP.To4()
	default:
		return nil
	}
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
		strings.Contains(lower, "outside uint32 millisecond range") ||
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

func (cfg *e2eConfig) captureVars(output string) {
	if cfg.vars == nil {
		cfg.vars = map[string]string{}
	}
	if runID, ok := standaloneRunIDFromOutput(output); ok {
		cfg.vars["run-id"] = runID
	}
}

var (
	standaloneRunIDLegacy   = regexp.MustCompile(`Standalone run:\s*id=([A-Za-z0-9._:-]+)`)
	standaloneUploadSuccess = regexp.MustCompile(`standalone upload completed: uploaded=[1-9][0-9]* last_http_status=2[0-9][0-9]`)
	standaloneUploadStopped = regexp.MustCompile(`standalone upload stopped: .*status=500`)
	standaloneStatusLine    = regexp.MustCompile(`Standalone: enabled=\S+ running=\S+ stored=([0-9]+) unsynced=([0-9]+)`)
)

func (cfg *e2eConfig) restoreAfterCase(tc matrixCase, commandLine string) {
	if !cfg.live || cfg.bin == "" || cfg.serial == "" {
		return
	}
	lower := strings.ToLower(commandLine)
	switch {
	case shouldRestoreWiFiAfter(commandLine):
		cfg.restoreWiFiConnection()
	case strings.Contains(lower, "set standalone enabled"):
		cfg.runShellCleanup("config> set standalone disabled")
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
	cfg.runCLICleanup("request", "wifi", "connect", cfg.ssid, "--passphrase", cfg.psk, "--security", "auto", "--timeout", "25000")
	cfg.runCLICleanup("request", "wifi", "wait", "connected", cfg.ssid, "--ip", "--validated", "--timeout", "30000")
}

func (cfg *e2eConfig) runShellCleanup(commandLine string) {
	if cfg.bin == "" || cfg.serial == "" {
		return
	}
	_ = cfg.runExternal(20*time.Second, []string{"--serial", cfg.serial, "shell"}, shellInput(commandLine)+"quit\n")
}

func (cfg *e2eConfig) resetLiveState() {
	cfg.forceStopPackage()
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
		"dropcheck show wifi",
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
		strings.Contains(strings.ToLower(tc.Runner), filter) ||
		strings.Contains(strings.ToLower(tc.Command), filter) ||
		strings.Contains(strings.ToLower(tc.Assertion), filter)
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

const e2eCaseCount = 358

var e2eCaseID = regexp.MustCompile(`^E2E-[0-9]{3}$`)

func TestE2ECaseTableSchema(t *testing.T) {
	cases := loadCases(t)
	if len(cases) != e2eCaseCount {
		t.Fatalf("case count = %d, want %d", len(cases), e2eCaseCount)
	}
	titles := map[string]string{}
	for index, tc := range cases {
		wantID := fmt.Sprintf("E2E-%03d", index+1)
		if tc.ID != wantID || !e2eCaseID.MatchString(tc.ID) {
			t.Fatalf("case row %d has ID %q, want %q", index+2, tc.ID, wantID)
		}
		if strings.TrimSpace(tc.Title) == "" {
			t.Fatalf("%s has an empty test title", tc.ID)
		}
		if !titleMatchesRunner(tc) {
			t.Fatalf("%s title %q does not match runner %q", tc.ID, tc.Title, tc.Runner)
		}
		if previousID, ok := titles[tc.Title]; ok {
			t.Fatalf("%s duplicates title %q from %s", tc.ID, tc.Title, previousID)
		}
		titles[tc.Title] = tc.ID
		if containsStaleCaseLanguage(tc) {
			t.Fatalf("%s contains stale case-management language", tc.ID)
		}
	}
}

func titleMatchesRunner(tc matrixCase) bool {
	switch tc.Runner {
	case "shell":
		return strings.HasPrefix(tc.Title, "Shell ")
	case "shell-parser":
		return strings.HasPrefix(tc.Title, "Parser ")
	case "cli":
		return strings.HasPrefix(tc.Title, "CLI ")
	default:
		return false
	}
}

func containsStaleCaseLanguage(tc matrixCase) bool {
	text := strings.ToLower(strings.Join([]string{tc.ID, tc.Title, tc.Runner, tc.Command, tc.Expect, tc.Assertion}, "\n"))
	staleTerms := []string{
		"test2",
		"mer" + "ged",
		"manual" + " matrix",
		"resolved" + " anom" + "aly",
		"regression" + ":",
		"cur" + "rently",
		"decide" + " whether",
		"documented" + " as",
		"last" + "-wins",
	}
	for _, term := range staleTerms {
		if strings.Contains(text, term) {
			return true
		}
	}
	return false
}

func TestE2ECaseTableParsesShellAndCLIExpectations(t *testing.T) {
	cases := loadCases(t)
	for _, tc := range cases {
		if tc.Runner != "shell" && tc.Runner != "cli" {
			continue
		}
		commandLine := expandParserPlaceholders(tc.Command)
		var res commandResult
		switch tc.Runner {
		case "shell":
			res = runShellParser(commandLine)
		case "cli":
			res = runCLIParser(commandLine)
		}
		if tc.Expect == "error" {
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
	if !isShellErrorOutput("retention_ms is outside uint32 millisecond range") {
		t.Fatalf("retention range validation must count as a shell error")
	}
}

func TestStandaloneTextParsersHandleKVRenderer(t *testing.T) {
	status := `Standalone
  enabled   true
  running   false
  stored    2
  unsynced  1

Message
  text  standalone upload stopped: run_id=run-1 status=500
`
	stored, unsynced := standaloneStatusCounts(t, status)
	if stored != 2 || unsynced != 1 {
		t.Fatalf("standaloneStatusCounts() = %d, %d; want 2, 1", stored, unsynced)
	}
	if !standaloneStatusRendered(status) {
		t.Fatalf("standaloneStatusRendered() = false")
	}

	run := `Standalone Run
  id      1778057585851-78844897-37d9-407e-bdca-dba4fd737d9c
  status  ok
  synced  false
  festa   archive-e2e
  steps   3
  failed  0

Steps
WIFI-GROUP  STEP            ATTEMPT  STATUS  ELAPSED  ERROR
mgmt        connect         1        ok      37ms
mgmt        wait_connected  1        ok      20ms
mgmt        cloudflare      1        ok      107ms
`
	runID := requireStandaloneRunID(t, run)
	if runID != "1778057585851-78844897-37d9-407e-bdca-dba4fd737d9c" {
		t.Fatalf("requireStandaloneRunID() = %q", runID)
	}
	assertStandaloneRunDetail(t, run, runID, "archive-e2e", false)
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
		{name: "shell standalone config", runner: "shell", text: "show config standalone"},
		{name: "shell standalone status", runner: "shell", text: "show standalone status"},
		{name: "shell standalone runs", runner: "shell", text: "show standalone runs"},
		{name: "shell standalone run detail parser", runner: "shell-parser", text: "show standalone run"},
		{name: "shell standalone clear", runner: "shell", text: "clear standalone runs"},
		{name: "shell standalone upload target parser", runner: "shell-parser", text: "config> set standalone upload to"},
		{name: "shell standalone upload wifi parser", runner: "shell-parser", text: "config> set standalone upload via wifi"},
		{name: "shell standalone delete parser", runner: "shell-parser", text: "config> delete standalone"},
		{name: "shell standalone run once", runner: "shell", text: "request> standalone run once"},
		{name: "shell standalone sync", runner: "shell", text: "sync standalone runs"},
		{name: "shell wifi status", runner: "shell", text: "show wifi status"},
		{name: "shell ip status", runner: "shell", text: "show ip status"},
		{name: "shell wifi diagnostics", runner: "shell", text: "show wifi diagnostics"},
		{name: "shell wifi eht", runner: "shell", text: "show wifi eht"},
		{name: "shell wifi eht fresh", runner: "shell", text: "show wifi eht fresh"},
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
		{name: "cli wifi eht", runner: "cli", text: "dropcheck show wifi eht"},
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
		{name: "cli standalone status", runner: "cli", text: "dropcheck show standalone status"},
		{name: "cli standalone run detail", runner: "cli", text: "dropcheck show standalone run"},
		{name: "cli standalone run once", runner: "cli", text: "dropcheck request standalone run once"},
		{name: "cli standalone sync", runner: "cli", text: "dropcheck sync standalone runs"},
		{name: "cli standalone configure", runner: "cli", text: "dropcheck configure set standalone"},
		{name: "cli standalone upload configure", runner: "cli", text: "dropcheck configure set standalone upload"},
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
		default:
			return append([]string(nil), args[i:]...), nil
		}
	}
	return nil, nil
}
