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
	defaultADB    = "adb"
	defaultPkg    = "io.dropcheck.agent"
	defaultPSKEnv = "DROPCHECK_E2E_WIFI_PSK"

	standaloneDNSName  = "example.com"
	standalonePingHost = "1.1.1.1"
	standaloneHTTPURL  = "http://connectivitycheck.gstatic.com/generate_204"

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
		{name: "repeat_exhausted", want: "no archived ping step"},
		{name: "repeat_failed", want: "command status=STATUS_FAILED"},
		{name: "retry_exhausted", want: "no archived ping step"},
		{name: "stable_unsupported", want: "StableFor is unsupported for offline archive replay"},
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
	checks := standaloneReplayChecks()
	pingCheck := f.Ping(standalonePingHost).Count(1).Expect(ping.Received().Ge(1))
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
	case "repeat_exhausted":
		checks = []f.Check{pingCheck.Repeat(2)}
	case "repeat_failed":
		for _, step := range archive.Steps {
			if step.GetStepName() == "ping" {
				failed := proto.Clone(step).(*controlpb.StandaloneMeasurementStep)
				failed.StepIndex = 100
				failed.Result.Status = controlpb.CommandResult_STATUS_FAILED
				archive.Steps = append(archive.Steps, failed)
				break
			}
		}
		checks = []f.Check{pingCheck.Repeat(2)}
	case "retry_exhausted":
		checks = []f.Check{f.Ping(standalonePingHost).Count(1).Retry(2, 0).Expect(ping.Received().Gt(1))}
	case "stable_unsupported":
		checks = []f.Check{pingCheck.StableFor(time.Nanosecond)}
	default:
		t.Fatalf("unknown replay failure fixture %q", name)
	}
	f.Run(t, f.Plan{
		Name: "standalone-replay-failure-" + name,
		Results: []f.ResultSource{
			f.StandaloneArchive(name, archive),
		},
		Checks: checks,
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
	var err error
	if requestMode {
		_, err = shell.ParseRequestLine(parseLine)
	} else if configureMode {
		_, err = shell.ParseConfigureLine(parseLine)
	} else {
		_, err = shell.ParseLine(parseLine)
	}
	if err != nil {
		return commandResult{Output: err.Error(), Code: 1, Err: err}
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
	}
	out := commandLine
	for key, value := range replacements {
		out = strings.ReplaceAll(out, key, value)
	}
	if runner != "shell-parser" {
		switch {
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
	cfg.runCLICleanup("request", "wifi", "connect", cfg.ssid, "--passphrase", cfg.psk, "--security", "auto", "--timeout", "25000")
	cfg.runCLICleanup("request", "wifi", "wait", "connected", cfg.ssid, "--ip", "--validated", "--timeout", "30000")
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

const e2eCaseCount = 282

var e2eCaseID = regexp.MustCompile(`^E2E-[0-9]{3}$`)

func TestE2ECaseTableSchema(t *testing.T) {
	cases := loadCases(t)
	if len(cases) != e2eCaseCount {
		t.Fatalf("case count = %d, want %d", len(cases), e2eCaseCount)
	}
	titles := map[string]string{}
	previousID := ""
	for index, tc := range cases {
		// Keep surviving IDs stable when obsolete cases are removed.
		if !e2eCaseID.MatchString(tc.ID) || tc.ID <= previousID {
			t.Fatalf("case row %d has invalid or unordered ID %q after %q", index+2, tc.ID, previousID)
		}
		previousID = tc.ID
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
		if strings.Contains(commandLine, "standalone") || strings.Contains(commandLine, "show config") {
			t.Errorf("%s still references removed standalone control: %s", tc.ID, tc.Command)
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
