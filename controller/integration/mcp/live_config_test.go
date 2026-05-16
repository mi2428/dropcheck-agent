//go:build e2e

package mcp_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const (
	liveEnvLive      = "DROPCHECK_E2E_LIVE"
	liveEnvSerial    = "DROPCHECK_E2E_SERIAL"
	liveEnvSSID      = "DROPCHECK_E2E_WIFI_SSID"
	liveEnvPSK       = "DROPCHECK_E2E_WIFI_PSK"
	liveEnvPSKName   = "DROPCHECK_E2E_WIFI_PSK_ENV"
	liveEnvADB       = "DROPCHECK_E2E_ADB"
	liveEnvPackage   = "DROPCHECK_E2E_PACKAGE"
	liveEnvLaunchApp = "DROPCHECK_E2E_LAUNCH_APP"
	liveDefaultADB   = "adb"
	liveDefaultPkg   = "io.dropcheck.agent"
	liveDefaultPSK   = "DROPCHECK_WIFI_PSK"
	liveDNSName      = "example.com"
	livePingHost     = "1.1.1.1"
	liveHTTPURL      = "http://connectivitycheck.gstatic.com/generate_204"
)

type liveMCPConfig struct {
	controllerRoot    string
	adb               string
	packageName       string
	live              bool
	serial            string
	ssid              string
	psk               string
	launchAppActivity bool
}

func loadLiveMCPConfig(t *testing.T) *liveMCPConfig {
	t.Helper()
	pskEnv := firstLiveNonEmpty(os.Getenv(liveEnvPSKName), liveDefaultPSK)
	return &liveMCPConfig{
		controllerRoot:    findLiveControllerRoot(t),
		adb:               liveEnvOr(liveEnvADB, liveDefaultADB),
		packageName:       liveEnvOr(liveEnvPackage, liveDefaultPkg),
		live:              liveEnvBool(liveEnvLive),
		serial:            firstLiveNonEmpty(os.Getenv(liveEnvSerial), os.Getenv("ADB_SERIAL")),
		ssid:              os.Getenv(liveEnvSSID),
		psk:               firstLiveNonEmpty(os.Getenv(pskEnv), os.Getenv(liveEnvPSK)),
		launchAppActivity: liveEnvBoolDefault(liveEnvLaunchApp, true),
	}
}

func (cfg *liveMCPConfig) resetLiveState() {
	cfg.forceStopPackage()
}

func (cfg *liveMCPConfig) restoreWiFiConnection() {
	if cfg.ssid == "" || cfg.psk == "" {
		return
	}
	_ = cfg.runDropcheck(45*time.Second, "request", "wifi", "connect", cfg.ssid, "--passphrase", cfg.psk, "--security", "auto", "--timeout", "25000")
	_ = cfg.runDropcheck(45*time.Second, "request", "wifi", "wait", "connected", cfg.ssid, "--ip", "--timeout", "30000")
}

func (cfg *liveMCPConfig) runDropcheck(timeout time.Duration, args ...string) error {
	if cfg.serial == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	fullArgs := append([]string{"run", "./cmd/dropcheck", "--serial", cfg.serial}, args...)
	cmd := exec.CommandContext(ctx, "go", fullArgs...)
	cmd.Dir = cfg.controllerRoot
	cmd.Env = os.Environ()
	return cmd.Run()
}

func (cfg *liveMCPConfig) forceStopPackage() {
	if cfg.adb == "" || cfg.serial == "" || cfg.packageName == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	_ = exec.CommandContext(ctx, cfg.adb, "-s", cfg.serial, "shell", "am", "force-stop", cfg.packageName).Run()
}

func (cfg *liveMCPConfig) launchApp(t *testing.T, reason string, wait bool) {
	t.Helper()
	if !cfg.launchAppActivity || !cfg.live || cfg.adb == "" || cfg.serial == "" || cfg.packageName == "" {
		return
	}
	args := []string{"-s", cfg.serial, "shell", "am", "start"}
	if wait {
		args = append(args, "-W")
	}
	args = append(args, "-n", cfg.packageName+"/.MainActivity", "-f", "0x34000000")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, cfg.adb, args...).CombinedOutput()
	if err != nil {
		t.Logf("launch app failed reason=%s wait=%t err=%v output=%s", reason, wait, err, strings.TrimSpace(string(out)))
		return
	}
	t.Logf("launch app reason=%s wait=%t output=%s", reason, wait, oneLiveLine(string(out)))
}

func findLiveControllerRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	for {
		if liveFileExists(filepath.Join(dir, "go.mod")) && liveDirExists(filepath.Join(dir, "cmd", "dropcheck")) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find controller root")
		}
		dir = parent
	}
}

func liveFileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func liveDirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func liveEnvBool(name string) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	return value == "1" || value == "true" || value == "yes"
}

func liveEnvBoolDefault(name string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	return liveEnvBool(name)
}

func liveEnvOr(name string, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func firstLiveNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func oneLiveLine(value string) string {
	return strings.Join(strings.Fields(value), " ")
}
