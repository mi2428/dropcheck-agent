//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"dropcheck/controller/internal/mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const e2eMCPPSKEnv = "DROPCHECK_E2E_MCP_WIFI_PSK"

func TestMCPServerAndroidLive(t *testing.T) {
	cfg := loadConfig(t)
	if !cfg.live {
		t.Skipf("%s=1 not set; skipping live MCP Android e2e", envLive)
	}
	if cfg.serial == "" {
		t.Skipf("%s or ADB_SERIAL not set; skipping live MCP Android e2e", envSerial)
	}

	cfg.resetLiveState()
	cfg.launchApp(t, "mcp suite start", true)
	t.Cleanup(func() {
		cfg.restoreWiFiConnection()
	})

	session, cleanup := connectLiveMCP(t, cfg)
	defer cleanup()
	target := cfg.serial

	t.Run("session start and agents", func(t *testing.T) {
		result, structured := callLiveMCPTool(t, session, "dropcheck_session_start", map[string]any{
			"adb_path":     cfg.adb,
			"serial":       cfg.serial,
			"package_name": cfg.packageName,
			"listen_addr":  "127.0.0.1:0",
		})
		requireMCPToolSuccess(t, result, structured, "dropcheck_session_start")
		sessionInfo := requireMCPMap(t, structured, "session")
		if agentCount := mcpNumber(sessionInfo, "agent_count"); agentCount < 1 {
			t.Fatalf("session agent_count=%v, want >= 1; structured=%v", sessionInfo["agent_count"], structured)
		}

		result, structured = callLiveMCPTool(t, session, "dropcheck_agents", map[string]any{})
		requireMCPToolSuccess(t, result, structured, "dropcheck_agents")
		agents := requireMCPSlice(t, structured, "agents")
		if len(agents) == 0 {
			t.Fatalf("agents empty; structured=%v", structured)
		}
		if !mcpAgentsIncludeTarget(agents, target) {
			t.Fatalf("agents do not include target %q: %v", target, agents)
		}
	})

	t.Run("read agent state through tools", func(t *testing.T) {
		result, structured := callLiveMCPTool(t, session, "dropcheck_wifi_status", map[string]any{
			"target": target,
		})
		requireMCPToolSuccess(t, result, structured, "dropcheck_wifi_status")
		requireMCPOperation(t, structured, "wifi.status")
		if status := structured["status"]; status != "ok" {
			t.Fatalf("wifi status=%v, want ok; structured=%v", status, structured)
		}

		result, structured = callLiveMCPTool(t, session, "dropcheck_ip_status", map[string]any{
			"target": target,
		})
		requireMCPToolSuccess(t, result, structured, "dropcheck_ip_status")
		requireMCPOperation(t, structured, "ip.status")
		if status := structured["status"]; status != "ok" {
			t.Fatalf("ip status=%v, want ok; structured=%v", status, structured)
		}
	})

	t.Run("command tool parses and dispatches to agent", func(t *testing.T) {
		result, structured := callLiveMCPTool(t, session, "dropcheck_command", map[string]any{
			"target":  target,
			"command": "show wifi status",
		})
		requireMCPToolSuccess(t, result, structured, "dropcheck_command")
		requireMCPOperation(t, structured, "wifi.status")
	})

	t.Run("dropcheck run executes connect wait checks", func(t *testing.T) {
		if cfg.ssid == "" || cfg.psk == "" {
			t.Skipf("%s/%s not set; skipping MCP dropcheck_run live sequence", envSSID, envPSK)
		}
		t.Setenv(e2eMCPPSKEnv, cfg.psk)
		result, structured := callLiveMCPTool(t, session, "dropcheck_run", map[string]any{
			"target":             target,
			"essid":              cfg.ssid,
			"passphrase_env":     e2eMCPPSKEnv,
			"security":           "auto",
			"band":               "all",
			"connect_timeout_ms": 25000,
			"wait_timeout_ms":    30000,
			"require_ip":         true,
			"checks":             []string{"wifi_status", "ip_status", "ping", "dns", "http"},
			"ping_host":          standalonePingHost,
			"ping_count":         1,
			"dns_name":           standaloneDNSName,
			"dns_type":           "A",
			"http_url":           standaloneHTTPURL,
			"http_status":        204,
		})
		requireMCPToolSuccess(t, result, structured, "dropcheck_run")
		want := []string{"wifi.connect", "wifi.wait", "wifi.status", "ip.status", "ping", "dns", "http"}
		got := mcpStepOperations(t, structured)
		if !slices.Equal(got, want) {
			t.Fatalf("dropcheck_run operations=%v, want %v; structured=%v", got, want, structured)
		}
	})

	t.Run("session stop", func(t *testing.T) {
		result, structured := callLiveMCPTool(t, session, "dropcheck_session_stop", map[string]any{})
		requireMCPToolSuccess(t, result, structured, "dropcheck_session_stop")
	})
}

func TestMCPServerCommandTransportAndroidLive(t *testing.T) {
	cfg := loadConfig(t)
	if !cfg.live {
		t.Skipf("%s=1 not set; skipping live MCP command transport e2e", envLive)
	}
	if cfg.serial == "" {
		t.Skipf("%s or ADB_SERIAL not set; skipping live MCP command transport e2e", envSerial)
	}

	cfg.launchApp(t, "mcp command transport start", true)
	session, cleanup := connectLiveMCPCommand(t, cfg)
	defer cleanup()

	result, structured := callLiveMCPTool(t, session, "dropcheck_session_start", map[string]any{
		"adb_path":     cfg.adb,
		"serial":       cfg.serial,
		"package_name": cfg.packageName,
		"listen_addr":  "127.0.0.1:0",
	})
	requireMCPToolSuccess(t, result, structured, "dropcheck_session_start")

	result, structured = callLiveMCPTool(t, session, "dropcheck_agents", map[string]any{})
	requireMCPToolSuccess(t, result, structured, "dropcheck_agents")
	if agents := requireMCPSlice(t, structured, "agents"); !mcpAgentsIncludeTarget(agents, cfg.serial) {
		t.Fatalf("agents do not include target %q: %v", cfg.serial, agents)
	}

	result, structured = callLiveMCPTool(t, session, "dropcheck_wifi_status", map[string]any{
		"target": cfg.serial,
	})
	requireMCPToolSuccess(t, result, structured, "dropcheck_wifi_status")
	requireMCPOperation(t, structured, "wifi.status")

	result, structured = callLiveMCPTool(t, session, "dropcheck_session_stop", map[string]any{})
	requireMCPToolSuccess(t, result, structured, "dropcheck_session_stop")
}

func connectLiveMCP(t *testing.T, cfg *e2eConfig) (*mcp.ClientSession, func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	backend := mcpserver.NewRealBackend(mcpserver.SessionStartOptions{
		ADBPath:     cfg.adb,
		Serial:      cfg.serial,
		PackageName: cfg.packageName,
		ListenAddr:  "127.0.0.1:0",
	})
	server := mcpserver.NewServer(backend)
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- server.Run(ctx, serverTransport)
	}()
	client := mcp.NewClient(&mcp.Implementation{Name: "dropcheck-mcp-e2e", Version: "0.1.0"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		cancel()
		_ = backend.Close()
		t.Fatalf("connect MCP client: %v", err)
	}
	cleanup := func() {
		_ = session.Close()
		_ = backend.Close()
		cancel()
		select {
		case err := <-serverDone:
			if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, mcp.ErrConnectionClosed) {
				t.Fatalf("server.Run: %v", err)
			}
		case <-time.After(time.Second):
			t.Fatalf("server did not stop")
		}
	}
	return session, cleanup
}

func connectLiveMCPCommand(t *testing.T, cfg *e2eConfig) (*mcp.ClientSession, func()) {
	t.Helper()
	bin := buildLiveMCPBinary(t, cfg)
	ctx, cancel := context.WithCancel(context.Background())
	stderr := &bytes.Buffer{}
	cmd := exec.Command(bin)
	cmd.Dir = cfg.controllerRoot
	cmd.Env = append(os.Environ(),
		"ADB_SERIAL="+cfg.serial,
	)
	cmd.Stderr = stderr
	client := mcp.NewClient(&mcp.Implementation{Name: "dropcheck-mcp-command-e2e", Version: "0.1.0"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{
		Command:           cmd,
		TerminateDuration: 3 * time.Second,
	}, nil)
	if err != nil {
		cancel()
		t.Fatalf("connect dropcheck-mcp command transport: %v\nstderr:\n%s", err, stderr.String())
	}
	cleanup := func() {
		if err := session.Close(); err != nil {
			t.Fatalf("close dropcheck-mcp command transport: %v\nstderr:\n%s", err, stderr.String())
		}
		cancel()
	}
	return session, cleanup
}

func buildLiveMCPBinary(t *testing.T, cfg *e2eConfig) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "dropcheck-mcp")
	t.Logf("building dropcheck-mcp binary: %s", bin)
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/dropcheck-mcp")
	cmd.Dir = cfg.controllerRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build dropcheck-mcp: %v\n%s", err, out)
	}
	return bin
}

func callLiveMCPTool(t *testing.T, session *mcp.ClientSession, name string, args map[string]any) (*mcp.CallToolResult, map[string]any) {
	t.Helper()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool(%s): %v", name, err)
	}
	return result, liveMCPStructuredMap(t, result)
}

func liveMCPStructuredMap(t *testing.T, result *mcp.CallToolResult) map[string]any {
	t.Helper()
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal structured content %s: %v", data, err)
	}
	return out
}

func requireMCPToolSuccess(t *testing.T, result *mcp.CallToolResult, structured map[string]any, name string) {
	t.Helper()
	if result.IsError {
		t.Fatalf("%s IsError=true structured=%v", name, structured)
	}
	if structured["success"] != true {
		t.Fatalf("%s success=%v, want true; structured=%v", name, structured["success"], structured)
	}
}

func requireMCPOperation(t *testing.T, structured map[string]any, want string) {
	t.Helper()
	if got := structured["operation"]; got != want {
		t.Fatalf("operation=%v, want %s; structured=%v", got, want, structured)
	}
}

func requireMCPMap(t *testing.T, structured map[string]any, key string) map[string]any {
	t.Helper()
	value, ok := structured[key].(map[string]any)
	if !ok {
		t.Fatalf("%s=%T %v, want object; structured=%v", key, structured[key], structured[key], structured)
	}
	return value
}

func requireMCPSlice(t *testing.T, structured map[string]any, key string) []any {
	t.Helper()
	value, ok := structured[key].([]any)
	if !ok {
		t.Fatalf("%s=%T %v, want array; structured=%v", key, structured[key], structured[key], structured)
	}
	return value
}

func mcpNumber(structured map[string]any, key string) float64 {
	switch value := structured[key].(type) {
	case float64:
		return value
	case int:
		return float64(value)
	case json.Number:
		n, _ := value.Float64()
		return n
	default:
		return 0
	}
}

func mcpAgentsIncludeTarget(agents []any, target string) bool {
	for _, raw := range agents {
		agent, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		for _, key := range []string{"id", "adb_serial"} {
			if value, ok := agent[key].(string); ok && value == target {
				return true
			}
		}
	}
	return false
}

func mcpStepOperations(t *testing.T, structured map[string]any) []string {
	t.Helper()
	steps := requireMCPSlice(t, structured, "steps")
	operations := make([]string, 0, len(steps))
	for i, raw := range steps {
		step, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("steps[%d]=%T %v, want object", i, raw, raw)
		}
		operation, ok := step["operation"].(string)
		if !ok || operation == "" {
			t.Fatalf("steps[%d].operation=%T %v, want non-empty string", i, step["operation"], step["operation"])
		}
		operations = append(operations, operation)
	}
	return operations
}
