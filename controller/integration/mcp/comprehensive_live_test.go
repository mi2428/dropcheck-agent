//go:build e2e && mcp_live_full

package mcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const comprehensiveMCPPSKEnv = "DROPCHECK_E2E_MCP_WIFI_PSK"

// TestMCPServerCommandTransportComprehensiveLive is intentionally gated behind
// mcp_live_full because it connects, disconnects, forgets Wi-Fi, edits
// standalone config, creates a standalone archive, and clears synced archives.
//
// Example:
//
//	DROPCHECK_E2E_LIVE=1 ADB_SERIAL=<serial> \
//	  DROPCHECK_E2E_WIFI_SSID=<ssid> DROPCHECK_E2E_WIFI_PSK_ENV=<psk-env> \
//	  go test -tags 'e2e mcp_live_full' ./integration/mcp \
//	    -run TestMCPServerCommandTransportComprehensiveLive -count=1 -v
func TestMCPServerCommandTransportComprehensiveLive(t *testing.T) {
	cfg := loadLiveMCPConfig(t)
	if !cfg.live {
		t.Skipf("%s=1 not set; skipping comprehensive live MCP check", liveEnvLive)
	}
	if cfg.serial == "" {
		t.Skipf("%s or ADB_SERIAL is required", liveEnvSerial)
	}
	if cfg.ssid == "" || cfg.psk == "" {
		t.Skipf("%s and %s/%s are required", liveEnvSSID, liveEnvPSKName, liveEnvPSK)
	}

	t.Setenv(comprehensiveMCPPSKEnv, cfg.psk)
	cfg.resetLiveState()
	cfg.launchApp(t, "comprehensive MCP start", true)

	check := newComprehensiveMCPCheck(t, cfg)
	defer check.close()

	check.protocol("set_logging_level", 5*time.Second, func(ctx context.Context) error {
		return check.session.SetLoggingLevel(ctx, &mcp.SetLoggingLevelParams{Level: "debug"})
	})
	check.inventory()
	check.readResource("dropcheck://session")
	check.getPrompt("dropcheck_noc_smoke_check", map[string]string{"target": cfg.serial})
	check.getPrompt("dropcheck_connectivity_check", map[string]string{"target": cfg.serial, "essid": cfg.ssid})
	check.getPrompt("dropcheck_mlo_investigation", map[string]string{"target": cfg.serial, "essid": cfg.ssid})

	check.call("dropcheck_session_start", map[string]any{
		"adb_path":     cfg.adb,
		"serial":       cfg.serial,
		"package_name": cfg.packageName,
		"listen_addr":  "127.0.0.1:0",
	}, 45*time.Second)
	check.call("dropcheck_agents", map[string]any{}, 10*time.Second)
	check.readResource("dropcheck://agents")
	check.readResource("dropcheck://standalone/config/default")
	check.readResource("dropcheck://standalone/status/default")
	check.readResource("dropcheck://standalone/runs/default")

	check.call("dropcheck_wifi_connect", map[string]any{
		"target":         cfg.serial,
		"essid":          cfg.ssid,
		"passphrase_env": comprehensiveMCPPSKEnv,
		"timeout_ms":     45000,
	}, 70*time.Second)
	check.call("dropcheck_wifi_wait_connected", map[string]any{
		"target":            cfg.serial,
		"essid":             cfg.ssid,
		"require_ip":        true,
		"require_validated": false,
		"timeout_ms":        30000,
	}, 45*time.Second)
	check.call("dropcheck_wifi_assert", map[string]any{
		"target":            cfg.serial,
		"essid":             cfg.ssid,
		"require_ip":        true,
		"require_validated": false,
		"timeout_ms":        10000,
	}, 20*time.Second)

	check.call("dropcheck_wifi_status", targetArg(cfg.serial), 20*time.Second)
	check.call("dropcheck_ip_status", targetArg(cfg.serial), 20*time.Second)
	check.call("dropcheck_wifi_diagnostics", targetArg(cfg.serial), 30*time.Second)
	check.call("dropcheck_wifi_mlo", map[string]any{"target": cfg.serial, "fresh": true, "timeout_ms": 10000}, 30*time.Second)
	check.call("dropcheck_wifi_capabilities", targetArg(cfg.serial), 20*time.Second)
	check.call("dropcheck_wifi_scan", map[string]any{"target": cfg.serial, "band": "all", "fresh": true, "timeout_ms": 10000}, 30*time.Second)
	check.call("dropcheck_wifi_scan_detail", map[string]any{"target": cfg.serial, "scan_target": cfg.ssid, "band": "all"}, 20*time.Second)

	check.call("dropcheck_ping", map[string]any{"target": cfg.serial, "host": "1.1.1.1", "count": 1, "timeout_ms": 8000}, 20*time.Second)
	check.call("dropcheck_dns", map[string]any{"target": cfg.serial, "name": "example.com", "type": "A", "timeout_ms": 8000}, 20*time.Second)
	check.call("dropcheck_http", map[string]any{"target": cfg.serial, "url": "http://connectivitycheck.gstatic.com/generate_204", "expected_status": 204, "timeout_ms": 10000}, 20*time.Second)
	check.call("dropcheck_download", map[string]any{"target": cfg.serial, "url": "https://example.com/", "timeout_ms": 20000}, 35*time.Second)
	check.call("dropcheck_traceroute", map[string]any{"target": cfg.serial, "host": "1.1.1.1", "max_hops": 30, "timeout_ms": 30000}, 45*time.Second)
	check.call("dropcheck_path_mtu", map[string]any{"target": cfg.serial, "host": "8.8.8.8", "min_mtu": 1200, "max_mtu": 1500, "timeout_ms": 25000}, 40*time.Second)
	check.call("dropcheck_global_ip", map[string]any{"target": cfg.serial, "family": "ipv4", "timeout_ms": 10000}, 25*time.Second)
	check.call("dropcheck_wifi_monitor", map[string]any{"target": cfg.serial, "duration_ms": 1500, "interval_ms": 300}, 15*time.Second)
	check.call("dropcheck_adb_diagnostics", map[string]any{"target": cfg.serial, "kind": "cmd-wifi-status"}, 20*time.Second)

	check.call("dropcheck_command", map[string]any{"target": cfg.serial, "command": "show wifi status"}, 20*time.Second)
	check.call("dropcheck_run", map[string]any{
		"target":             cfg.serial,
		"essid":              cfg.ssid,
		"passphrase_env":     comprehensiveMCPPSKEnv,
		"connect_timeout_ms": 30000,
		"wait_timeout_ms":    30000,
		"require_ip":         true,
		"checks":             []string{"wifi_status", "ip_status", "ping", "dns", "http", "global_ip", "scan_detail"},
		"ping_host":          "1.1.1.1",
		"ping_count":         1,
		"dns_name":           "example.com",
		"dns_type":           "A",
		"http_url":           "http://connectivitycheck.gstatic.com/generate_204",
		"http_status":        204,
		"global_ip_family":   "ipv4",
	}, 2*time.Minute)

	runID := check.exerciseStandalone(cfg)
	if runID != "" {
		check.readResource("dropcheck://standalone/run/default/" + url.PathEscape(runID))
		check.call("dropcheck_standalone_run", map[string]any{"target": cfg.serial, "run_id": runID, "mark_synced": true}, 30*time.Second)
	}
	check.call("dropcheck_standalone_runs", map[string]any{"target": cfg.serial, "limit": 5, "include_synced": true}, 20*time.Second)
	check.call("dropcheck_standalone_clear_runs", map[string]any{"target": cfg.serial, "mode": "synced"}, 30*time.Second)
	check.call("dropcheck_standalone_config_edit", map[string]any{
		"target": cfg.serial,
		"edits":  []map[string]any{{"action": "delete", "path": []string{"festa", "mcp-livecheck"}}},
	}, 30*time.Second)

	check.call("dropcheck_wifi_reconnect", map[string]any{"target": cfg.serial, "timeout_ms": 30000}, 45*time.Second)
	check.call("dropcheck_wifi_cycle", map[string]any{
		"target":            cfg.serial,
		"essid":             cfg.ssid,
		"passphrase_env":    comprehensiveMCPPSKEnv,
		"timeout_ms":        30000,
		"count":             1,
		"ping_host":         "1.1.1.1",
		"http_url":          "http://connectivitycheck.gstatic.com/generate_204",
		"pause_ms":          100,
		"forget_after_each": false,
	}, 90*time.Second)
	check.call("dropcheck_wifi_reconnect", map[string]any{"target": cfg.serial, "timeout_ms": 30000}, 45*time.Second)
	check.call("dropcheck_wifi_disconnect", targetArg(cfg.serial), 20*time.Second)
	check.call("dropcheck_wifi_reconnect", map[string]any{"target": cfg.serial, "timeout_ms": 30000}, 45*time.Second)
	check.call("dropcheck_wifi_forget", map[string]any{"target": cfg.serial, "network": cfg.ssid}, 30*time.Second)
	check.call("dropcheck_wifi_connect", map[string]any{
		"target":         cfg.serial,
		"essid":          cfg.ssid,
		"passphrase_env": comprehensiveMCPPSKEnv,
		"timeout_ms":     45000,
	}, 70*time.Second)

	check.call("dropcheck_session_stop", map[string]any{}, 10*time.Second)
	check.failOnFailures()
}

type comprehensiveMCPCheck struct {
	t        *testing.T
	cfg      *liveMCPConfig
	ctx      context.Context
	cancel   context.CancelFunc
	session  *mcp.ClientSession
	stderr   *bytes.Buffer
	mu       sync.Mutex
	progress int
	logs     int
	seq      int
	results  []comprehensiveMCPResult
}

type comprehensiveMCPResult struct {
	name     string
	ok       bool
	note     string
	duration time.Duration
}

func newComprehensiveMCPCheck(t *testing.T, cfg *liveMCPConfig) *comprehensiveMCPCheck {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	check := &comprehensiveMCPCheck{
		t:      t,
		cfg:    cfg,
		ctx:    ctx,
		cancel: cancel,
		stderr: &bytes.Buffer{},
	}
	bin := buildLiveMCPBinary(t, cfg)
	cmd := exec.Command(bin)
	cmd.Dir = cfg.controllerRoot
	cmd.Env = append(os.Environ(), "ADB_SERIAL="+cfg.serial)
	cmd.Stderr = check.stderr
	client := mcp.NewClient(&mcp.Implementation{Name: "dropcheck-comprehensive-mcp-e2e", Version: "0.1.0"}, &mcp.ClientOptions{
		ProgressNotificationHandler: func(_ context.Context, req *mcp.ProgressNotificationClientRequest) {
			check.mu.Lock()
			check.progress++
			check.mu.Unlock()
		},
		LoggingMessageHandler: func(_ context.Context, req *mcp.LoggingMessageRequest) {
			check.mu.Lock()
			check.logs++
			check.mu.Unlock()
		},
	})
	session, err := client.Connect(ctx, &mcp.CommandTransport{
		Command:           cmd,
		TerminateDuration: 3 * time.Second,
	}, nil)
	if err != nil {
		cancel()
		t.Fatalf("connect dropcheck-mcp command transport: %v\nstderr:\n%s", err, redactE2ESecrets(cfg, check.stderr.String()))
	}
	check.session = session
	return check
}

func (c *comprehensiveMCPCheck) close() {
	if c.session != nil {
		if err := c.session.Close(); err != nil && !errors.Is(err, mcp.ErrConnectionClosed) {
			c.t.Errorf("close MCP session: %v\nstderr:\n%s", err, redactE2ESecrets(c.cfg, c.stderr.String()))
		}
	}
	if c.cancel != nil {
		c.cancel()
	}
}

func (c *comprehensiveMCPCheck) inventory() {
	c.protocol("list_tools", 10*time.Second, func(ctx context.Context) error {
		res, err := c.session.ListTools(ctx, nil)
		if err != nil {
			return err
		}
		var missing []string
		for _, name := range expectedComprehensiveMCPTools() {
			if !slices.ContainsFunc(res.Tools, func(tool *mcp.Tool) bool { return tool.Name == name }) {
				missing = append(missing, name)
			}
		}
		if len(missing) > 0 {
			return fmt.Errorf("missing tools: %s", strings.Join(missing, ", "))
		}
		return nil
	})
	c.protocol("list_resources", 10*time.Second, func(ctx context.Context) error {
		res, err := c.session.ListResources(ctx, nil)
		if err != nil {
			return err
		}
		if len(res.Resources) < 2 {
			return fmt.Errorf("resources=%d, want >=2", len(res.Resources))
		}
		return nil
	})
	c.protocol("list_resource_templates", 10*time.Second, func(ctx context.Context) error {
		res, err := c.session.ListResourceTemplates(ctx, nil)
		if err != nil {
			return err
		}
		if len(res.ResourceTemplates) < 4 {
			return fmt.Errorf("resource_templates=%d, want >=4", len(res.ResourceTemplates))
		}
		return nil
	})
	c.protocol("list_prompts", 10*time.Second, func(ctx context.Context) error {
		res, err := c.session.ListPrompts(ctx, nil)
		if err != nil {
			return err
		}
		if len(res.Prompts) < 3 {
			return fmt.Errorf("prompts=%d, want >=3", len(res.Prompts))
		}
		return nil
	})
}

func (c *comprehensiveMCPCheck) exerciseStandalone(cfg *liveMCPConfig) string {
	festa := "mcp-livecheck"
	c.call("dropcheck_standalone_config", targetArg(cfg.serial), 20*time.Second)
	c.call("dropcheck_standalone_status", targetArg(cfg.serial), 20*time.Second)
	c.call("dropcheck_standalone_runs", map[string]any{"target": cfg.serial, "limit": 5, "include_synced": true}, 20*time.Second)
	c.call("dropcheck_standalone_config_edit", map[string]any{
		"target": cfg.serial,
		"edits": []map[string]any{
			{"action": "delete", "path": []string{"festa", festa}},
			{"action": "set", "path": []string{"festa", festa, "enabled"}, "value": "true"},
			{"action": "set", "path": []string{"festa", festa, "interval_ms"}, "value": "60000"},
			{"action": "set", "path": []string{"festa", festa, "wifi", "lab", "match", "essid"}, "value": cfg.ssid},
			{"action": "set", "path": []string{"festa", festa, "wifi", "lab", "passphrase"}, "value": cfg.psk},
			{"action": "set", "path": []string{"festa", festa, "wifi", "lab", "timeout_ms"}, "value": "30000"},
			{"action": "set", "path": []string{"festa", festa, "wifi", "lab", "wait", "ip"}, "value": "true"},
			{"action": "set", "path": []string{"festa", festa, "check", "ping", "test"}, "value": "ping"},
			{"action": "set", "path": []string{"festa", festa, "check", "ping", "host"}, "value": "1.1.1.1"},
			{"action": "set", "path": []string{"festa", festa, "check", "ping", "count"}, "value": "1"},
			{"action": "set", "path": []string{"festa", festa, "check", "ping", "timeout_ms"}, "value": "8000"},
		},
	}, 30*time.Second)
	structured, ok := c.call("dropcheck_standalone_run_once", map[string]any{"target": cfg.serial, "festa": festa, "save": true}, 90*time.Second)
	if !ok {
		return ""
	}
	return nestedLiveString(structured, "result", "standalone_run", "summary", "run_id")
}

func (c *comprehensiveMCPCheck) call(name string, args map[string]any, timeout time.Duration) (map[string]any, bool) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	c.seq++
	params := &mcp.CallToolParams{Name: name, Arguments: args}
	params.SetProgressToken("comprehensive-" + strconv.Itoa(c.seq) + "-" + name)
	res, err := c.session.CallTool(ctx, params)
	duration := time.Since(start)
	if err != nil {
		c.record(name, false, err.Error(), duration)
		return nil, false
	}
	structured, err := liveStructuredMap(res)
	if err != nil {
		c.record(name, false, err.Error(), duration)
		return nil, false
	}
	if res.IsError || structured["success"] != true {
		c.record(name, false, liveToolMessage(res, structured), duration)
		return structured, false
	}
	c.record(name, true, liveOperationNote(structured), duration)
	return structured, true
}

func (c *comprehensiveMCPCheck) protocol(name string, timeout time.Duration, fn func(context.Context) error) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	err := fn(ctx)
	c.record(name, err == nil, errorString(err), time.Since(start))
}

func (c *comprehensiveMCPCheck) readResource(uri string) {
	c.protocol("read_resource "+uri, 30*time.Second, func(ctx context.Context) error {
		res, err := c.session.ReadResource(ctx, &mcp.ReadResourceParams{URI: uri})
		if err != nil {
			return err
		}
		if len(res.Contents) == 0 {
			return fmt.Errorf("empty resource")
		}
		for _, content := range res.Contents {
			if content.Text == "" {
				continue
			}
			var out map[string]any
			if err := json.Unmarshal([]byte(content.Text), &out); err != nil {
				return fmt.Errorf("resource is not JSON: %w", err)
			}
			if success, ok := out["success"].(bool); ok && !success {
				return fmt.Errorf("resource success=false: %v", out["message"])
			}
			return nil
		}
		return fmt.Errorf("resource has no text content")
	})
}

func (c *comprehensiveMCPCheck) getPrompt(name string, args map[string]string) {
	c.protocol("get_prompt "+name, 10*time.Second, func(ctx context.Context) error {
		res, err := c.session.GetPrompt(ctx, &mcp.GetPromptParams{Name: name, Arguments: args})
		if err != nil {
			return err
		}
		if len(res.Messages) == 0 {
			return fmt.Errorf("empty prompt")
		}
		return nil
	})
}

func (c *comprehensiveMCPCheck) record(name string, ok bool, note string, duration time.Duration) {
	c.results = append(c.results, comprehensiveMCPResult{
		name:     name,
		ok:       ok,
		note:     redactE2ESecrets(c.cfg, note),
		duration: duration,
	})
}

func (c *comprehensiveMCPCheck) failOnFailures() {
	failures := 0
	for _, result := range c.results {
		status := "OK"
		if !result.ok {
			status = "FAIL"
			failures++
		}
		note := result.note
		if note == "" {
			note = "-"
		}
		c.t.Logf("%-4s %-58s %8s %s", status, result.name, result.duration.Round(time.Millisecond), note)
	}
	c.t.Logf("MCP comprehensive live summary: checks=%d failures=%d progress_notifications=%d log_messages=%d", len(c.results), failures, c.progress, c.logs)
	if stderr := strings.TrimSpace(redactE2ESecrets(c.cfg, c.stderr.String())); stderr != "" {
		c.t.Logf("dropcheck-mcp stderr:\n%s", stderr)
	}
	if failures > 0 {
		c.t.Fatalf("comprehensive MCP live check failed: %d/%d", failures, len(c.results))
	}
}

func expectedComprehensiveMCPTools() []string {
	return []string{
		"dropcheck_adb_diagnostics",
		"dropcheck_agents",
		"dropcheck_command",
		"dropcheck_dns",
		"dropcheck_download",
		"dropcheck_global_ip",
		"dropcheck_http",
		"dropcheck_ip_status",
		"dropcheck_path_mtu",
		"dropcheck_ping",
		"dropcheck_run",
		"dropcheck_session_start",
		"dropcheck_session_stop",
		"dropcheck_standalone_clear_runs",
		"dropcheck_standalone_config",
		"dropcheck_standalone_config_edit",
		"dropcheck_standalone_run",
		"dropcheck_standalone_run_once",
		"dropcheck_standalone_runs",
		"dropcheck_standalone_status",
		"dropcheck_traceroute",
		"dropcheck_wifi_assert",
		"dropcheck_wifi_capabilities",
		"dropcheck_wifi_connect",
		"dropcheck_wifi_cycle",
		"dropcheck_wifi_diagnostics",
		"dropcheck_wifi_disconnect",
		"dropcheck_wifi_forget",
		"dropcheck_wifi_mlo",
		"dropcheck_wifi_monitor",
		"dropcheck_wifi_reconnect",
		"dropcheck_wifi_scan",
		"dropcheck_wifi_scan_detail",
		"dropcheck_wifi_status",
		"dropcheck_wifi_wait_connected",
	}
}

func liveStructuredMap(result *mcp.CallToolResult) (map[string]any, error) {
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("decode structured content: %w", err)
	}
	return out, nil
}

func targetArg(serial string) map[string]any {
	return map[string]any{"target": serial}
}

func liveOperationNote(m map[string]any) string {
	var parts []string
	if operation, _ := m["operation"].(string); operation != "" {
		parts = append(parts, operation)
	}
	if status, _ := m["status"].(string); status != "" {
		parts = append(parts, status)
	}
	if message, _ := m["message"].(string); message != "" {
		parts = append(parts, message)
	}
	if len(parts) == 0 {
		return "ok"
	}
	return strings.Join(parts, ": ")
}

func liveToolMessage(result *mcp.CallToolResult, structured map[string]any) string {
	if msg, _ := structured["error"].(string); msg != "" {
		return msg
	}
	if msg, _ := structured["message"].(string); msg != "" {
		return msg
	}
	var texts []string
	for _, content := range result.Content {
		if text, ok := content.(*mcp.TextContent); ok && text.Text != "" {
			texts = append(texts, text.Text)
		}
	}
	if len(texts) > 0 {
		return strings.Join(texts, " ")
	}
	return "tool returned failure"
}

func nestedLiveString(m map[string]any, path ...string) string {
	var current any = m
	for _, key := range path {
		object, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current = object[key]
	}
	if value, ok := current.(string); ok {
		return value
	}
	return ""
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func redactE2ESecrets(cfg *liveMCPConfig, value string) string {
	for _, secret := range []string{cfg.psk, os.Getenv(comprehensiveMCPPSKEnv)} {
		if secret != "" {
			value = strings.ReplaceAll(value, secret, "[REDACTED]")
		}
	}
	return value
}
