package mcp_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"dropcheck/controller/internal/adbdiag"
	dropcmd "dropcheck/controller/internal/command"
	"dropcheck/controller/internal/controlpb"
	"dropcheck/controller/internal/mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type runRecord struct {
	target string
	op     dropcmd.Operation
}

type fakeBackend struct {
	mu      sync.Mutex
	agents  []mcpserver.Agent
	runs    []runRecord
	adbRuns []struct {
		target string
		kind   string
	}
	statusByOp  map[string]controlpb.CommandResult_Status
	started     bool
	startOpts   []mcpserver.SessionStartOptions
	stopCount   int
	runDelay    time.Duration
	inFlight    int
	maxInFlight int
}

func newFakeBackend() *fakeBackend {
	return &fakeBackend{
		agents: []mcpserver.Agent{{
			Number:       1,
			ID:           "serial-1",
			ADBSerial:    "serial-1",
			SessionID:    "session-1",
			AppVersion:   "0.1.0",
			Manufacturer: "Google",
			Model:        "Pixel",
			SDK:          36,
			Connected:    time.Unix(1700000000, 0),
		}},
		statusByOp: map[string]controlpb.CommandResult_Status{},
	}
}

func (b *fakeBackend) Info(context.Context) (mcpserver.SessionInfo, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return mcpserver.SessionInfo{Started: b.started, ListenAddr: "127.0.0.1:12345", AgentCount: len(b.agents), Agents: append([]mcpserver.Agent(nil), b.agents...), StartedAt: time.Unix(1700000001, 0)}, nil
}

func (b *fakeBackend) Start(_ context.Context, opts mcpserver.SessionStartOptions) (mcpserver.SessionInfo, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.started = true
	b.startOpts = append(b.startOpts, opts)
	return mcpserver.SessionInfo{Started: true, ListenAddr: "127.0.0.1:12345", AgentCount: len(b.agents), Agents: append([]mcpserver.Agent(nil), b.agents...), StartedAt: time.Unix(1700000001, 0)}, nil
}

func (b *fakeBackend) Stop(context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.started = false
	b.stopCount++
	return nil
}

func (b *fakeBackend) Agents(context.Context) ([]mcpserver.Agent, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]mcpserver.Agent(nil), b.agents...), nil
}

func (b *fakeBackend) Run(_ context.Context, target string, op dropcmd.Operation) (mcpserver.Execution, error) {
	cmd, _, err := dropcmd.BuildRunCommand(op)
	if err != nil {
		return mcpserver.Execution{}, err
	}
	b.mu.Lock()
	b.inFlight++
	if b.inFlight > b.maxInFlight {
		b.maxInFlight = b.inFlight
	}
	delay := b.runDelay
	b.mu.Unlock()
	if delay > 0 {
		time.Sleep(delay)
	}
	b.mu.Lock()
	b.inFlight--
	defer b.mu.Unlock()
	b.runs = append(b.runs, runRecord{target: target, op: op})
	status := controlpb.CommandResult_STATUS_OK
	if configured := b.statusByOp[op.Name]; configured != controlpb.CommandResult_STATUS_UNSPECIFIED {
		status = configured
	}
	message := "ok"
	if status != controlpb.CommandResult_STATUS_OK {
		message = "forced failure"
	}
	result := &controlpb.CommandResult{
		Status:    status,
		Message:   message,
		ElapsedMs: 12,
	}
	setPayload(result, op.Name)
	agent := b.agentForTargetLocked(target)
	return mcpserver.Execution{
		Agent:        agent,
		CommandID:    "cmd-" + op.Name,
		Operation:    op.Name,
		CommandLabel: cmd.GetLabel(),
		Result:       result,
	}, nil
}

func (b *fakeBackend) agentForTargetLocked(target string) mcpserver.Agent {
	for _, agent := range b.agents {
		if target == "" || target == agent.ID || target == agent.ADBSerial {
			return agent
		}
	}
	return b.agents[0]
}

func (b *fakeBackend) ADBDiagnostics(_ context.Context, target string, kind string) (mcpserver.ADBDiagnostics, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.adbRuns = append(b.adbRuns, struct {
		target string
		kind   string
	}{target: target, kind: kind})
	return mcpserver.ADBDiagnostics{
		Agent: b.agents[0],
		Bundle: adbdiag.Bundle{
			Agent:  b.agents[0].ID,
			Serial: b.agents[0].ADBSerial,
			Kind:   kind,
			Commands: []adbdiag.CommandResult{{
				Name:     "cmd wifi status",
				Command:  "adb shell cmd wifi status",
				Stdout:   "Wifi is enabled\n",
				ExitCode: 0,
			}},
		},
	}, nil
}

func (b *fakeBackend) Close() error { return nil }

func setPayload(result *controlpb.CommandResult, name string) {
	switch name {
	case "wifi.connect":
		result.Payload = &controlpb.CommandResult_ConnectWifi{ConnectWifi: &controlpb.ConnectWifiResult{Ssid: "Lab", Connected: true, Message: "connected"}}
	case "wifi.wait", "wifi.assert":
		result.Payload = &controlpb.CommandResult_WifiAssert{WifiAssert: &controlpb.WifiAssertResult{Passed: true}}
	case "wifi.status":
		result.Payload = &controlpb.CommandResult_WifiStatus{WifiStatus: &controlpb.WifiStatus{Enabled: true, State: "connected"}}
	case "ip.status":
		result.Payload = &controlpb.CommandResult_IpStatus{IpStatus: &controlpb.IpStatus{InterfaceName: "wlan0", Validated: true}}
	case "ping":
		result.Payload = &controlpb.CommandResult_Ping{Ping: &controlpb.PingResult{Host: "1.1.1.1", Transmitted: 1, Received: 1}}
	case "dns":
		result.Payload = &controlpb.CommandResult_ResolveDns{ResolveDns: &controlpb.ResolveDnsResult{Name: "example.com"}}
	case "http":
		result.Payload = &controlpb.CommandResult_HttpCheck{HttpCheck: &controlpb.HttpCheckResult{Url: "http://connectivitycheck.gstatic.com/generate_204", Status: 204, ExpectedStatus: 204, Matched: true}}
	case "standalone.config":
		result.Payload = &controlpb.CommandResult_StandaloneConfig{StandaloneConfig: &controlpb.StandaloneConfig{Enabled: true}}
	case "standalone.status":
		result.Payload = &controlpb.CommandResult_StandaloneStatus{StandaloneStatus: &controlpb.StandaloneStatus{Enabled: true, StoredRuns: 1}}
	case "standalone.runs":
		result.Payload = &controlpb.CommandResult_StandaloneRuns{StandaloneRuns: &controlpb.StandaloneRuns{TotalRuns: 1}}
	case "standalone.run":
		result.Payload = &controlpb.CommandResult_StandaloneRun{StandaloneRun: &controlpb.StandaloneRunArchive{Summary: &controlpb.StandaloneRunSummary{RunId: "run-1", Status: "ok"}}}
	}
}

func connectMCP(t *testing.T, backend *fakeBackend) (*mcp.ClientSession, func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	server := mcpserver.NewServer(backend)
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- server.Run(ctx, serverTransport)
	}()
	client := mcp.NewClient(&mcp.Implementation{Name: "dropcheck-mcp-test", Version: "0.1.0"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		cancel()
		t.Fatalf("connect MCP client: %v", err)
	}
	cleanup := func() {
		_ = session.Close()
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

func callTool(t *testing.T, session *mcp.ClientSession, name string, args map[string]any) (*mcp.CallToolResult, map[string]any) {
	t.Helper()
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool(%s): %v", name, err)
	}
	return result, structuredMap(t, result)
}

func structuredMap(t *testing.T, result *mcp.CallToolResult) map[string]any {
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

func assertContentIncludesStructuredJSON(t *testing.T, result *mcp.CallToolResult, structured map[string]any) {
	t.Helper()
	for _, content := range result.Content {
		text := content.(*mcp.TextContent).Text
		var decoded map[string]any
		if err := json.Unmarshal([]byte(text), &decoded); err != nil {
			continue
		}
		if reflect.DeepEqual(decoded, structured) {
			return
		}
	}
	t.Fatalf("content does not include structured JSON: content=%#v structured=%#v", result.Content, structured)
}

func readResourceMap(t *testing.T, session *mcp.ClientSession, uri string) map[string]any {
	t.Helper()
	result, err := session.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: uri})
	if err != nil {
		t.Fatalf("ReadResource(%s): %v", uri, err)
	}
	if len(result.Contents) != 1 {
		t.Fatalf("ReadResource(%s) contents=%d, want 1", uri, len(result.Contents))
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(result.Contents[0].Text), &out); err != nil {
		t.Fatalf("ReadResource(%s) content %q: %v", uri, result.Contents[0].Text, err)
	}
	return out
}

func outputSchemaProperties(t *testing.T, tool *mcp.Tool) map[string]any {
	t.Helper()
	data, err := json.Marshal(tool.OutputSchema)
	if err != nil {
		t.Fatalf("marshal output schema for %s: %v", tool.Name, err)
	}
	var schema map[string]any
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatalf("unmarshal output schema for %s: %v", tool.Name, err)
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("%s output schema properties=%T %[1]v", tool.Name, schema["properties"])
	}
	return properties
}

func TestToolsListIncludesDropcheckOperations(t *testing.T) {
	session, cleanup := connectMCP(t, newFakeBackend())
	defer cleanup()

	list, err := session.ListTools(context.Background(), &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	var names []string
	for _, tool := range list.Tools {
		names = append(names, tool.Name)
	}
	for _, name := range []string{
		"dropcheck_session_start",
		"dropcheck_agents",
		"dropcheck_wifi_connect",
		"dropcheck_wifi_cycle",
		"dropcheck_wifi_disconnect",
		"dropcheck_wifi_forget",
		"dropcheck_wifi_mlo",
		"dropcheck_wifi_monitor",
		"dropcheck_ping",
		"dropcheck_adb_diagnostics",
		"dropcheck_standalone_run_once",
		"dropcheck_command",
		"dropcheck_run",
	} {
		if !slices.Contains(names, name) {
			t.Fatalf("tool %s not listed; got %v", name, names)
		}
	}
}

func TestToolOutputSchemasDescribeStructuredContent(t *testing.T) {
	session, cleanup := connectMCP(t, newFakeBackend())
	defer cleanup()

	list, err := session.ListTools(context.Background(), &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	tools := map[string]*mcp.Tool{}
	for _, tool := range list.Tools {
		tools[tool.Name] = tool
	}
	for name, fields := range map[string][]string{
		"dropcheck_session_start":   {"success", "session", "error"},
		"dropcheck_agents":          {"success", "agents", "error"},
		"dropcheck_ping":            {"success", "operation", "status", "elapsed_ms", "result"},
		"dropcheck_adb_diagnostics": {"success", "agent", "diagnostics", "error"},
		"dropcheck_command":         {"success", "operation", "results", "agents", "error"},
		"dropcheck_run":             {"success", "steps", "failed_step", "partial", "error"},
	} {
		tool := tools[name]
		if tool == nil {
			t.Fatalf("tool %s not listed", name)
		}
		properties := outputSchemaProperties(t, tool)
		for _, field := range fields {
			if _, ok := properties[field]; !ok {
				t.Fatalf("%s output schema missing %s: %v", name, field, properties)
			}
		}
	}
	resultProperties := func(toolName string) map[string]any {
		t.Helper()
		result, ok := outputSchemaProperties(t, tools[toolName])["result"].(map[string]any)
		if !ok {
			t.Fatalf("%s result schema missing", toolName)
		}
		properties, ok := result["properties"].(map[string]any)
		if !ok {
			t.Fatalf("%s result properties missing: %v", toolName, result)
		}
		return properties
	}
	for name, payload := range map[string]string{
		"dropcheck_wifi_status":    "wifi_status",
		"dropcheck_ip_status":      "ip_status",
		"dropcheck_ping":           "ping",
		"dropcheck_dns":            "resolve_dns",
		"dropcheck_http":           "http_check",
		"dropcheck_standalone_run": "standalone_run",
	} {
		if _, ok := resultProperties(name)[payload]; !ok {
			t.Fatalf("%s output schema missing result.%s", name, payload)
		}
	}
}

func TestToolAnnotationsReflectExternalAndDestructiveBehavior(t *testing.T) {
	session, cleanup := connectMCP(t, newFakeBackend())
	defer cleanup()

	list, err := session.ListTools(context.Background(), &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	tools := map[string]*mcp.Tool{}
	for _, tool := range list.Tools {
		tools[tool.Name] = tool
	}
	for _, name := range []string{"dropcheck_ping", "dropcheck_wifi_forget", "dropcheck_wifi_cycle", "dropcheck_command", "dropcheck_run"} {
		tool := tools[name]
		if tool == nil {
			t.Fatalf("tool %s not listed", name)
		}
		if tool.Annotations == nil || tool.Annotations.OpenWorldHint == nil || !*tool.Annotations.OpenWorldHint {
			t.Fatalf("%s OpenWorldHint=%v, want true", name, tool.Annotations)
		}
	}
	if !tools["dropcheck_ping"].Annotations.ReadOnlyHint {
		t.Fatalf("dropcheck_ping ReadOnlyHint=false")
	}
	for _, name := range []string{"dropcheck_wifi_forget", "dropcheck_wifi_cycle", "dropcheck_command", "dropcheck_run"} {
		hint := tools[name].Annotations.DestructiveHint
		if hint == nil || !*hint {
			t.Fatalf("%s DestructiveHint=%v, want true", name, hint)
		}
	}
	standaloneRun := tools["dropcheck_standalone_run"]
	if standaloneRun == nil {
		t.Fatalf("dropcheck_standalone_run not listed")
	}
	if standaloneRun.Annotations.ReadOnlyHint {
		t.Fatalf("dropcheck_standalone_run ReadOnlyHint=true, want false because mark_synced can update state")
	}
	if hint := standaloneRun.Annotations.DestructiveHint; hint == nil || *hint {
		t.Fatalf("dropcheck_standalone_run DestructiveHint=%v, want false", hint)
	}
}

func TestResourcesExposeSessionAgentsAndStandaloneState(t *testing.T) {
	backend := newFakeBackend()
	session, cleanup := connectMCP(t, backend)
	defer cleanup()

	resources, err := session.ListResources(context.Background(), &mcp.ListResourcesParams{})
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	var resourceURIs []string
	for _, resource := range resources.Resources {
		resourceURIs = append(resourceURIs, resource.URI)
	}
	for _, uri := range []string{"dropcheck://session", "dropcheck://agents"} {
		if !slices.Contains(resourceURIs, uri) {
			t.Fatalf("resource %s not listed; got %v", uri, resourceURIs)
		}
	}

	templates, err := session.ListResourceTemplates(context.Background(), &mcp.ListResourceTemplatesParams{})
	if err != nil {
		t.Fatalf("ListResourceTemplates: %v", err)
	}
	var templateURIs []string
	for _, template := range templates.ResourceTemplates {
		templateURIs = append(templateURIs, template.URITemplate)
	}
	for _, uri := range []string{
		"dropcheck://standalone/config/{target}",
		"dropcheck://standalone/status/{target}",
		"dropcheck://standalone/runs/{target}",
		"dropcheck://standalone/run/{target}/{run_id}",
	} {
		if !slices.Contains(templateURIs, uri) {
			t.Fatalf("resource template %s not listed; got %v", uri, templateURIs)
		}
	}

	sessionResource := readResourceMap(t, session, "dropcheck://session")
	if sessionResource["success"] != true {
		t.Fatalf("session resource=%v", sessionResource)
	}
	agentsResource := readResourceMap(t, session, "dropcheck://agents")
	if agents, ok := agentsResource["agents"].([]any); !ok || len(agents) != 1 {
		t.Fatalf("agents resource=%v", agentsResource)
	}
	configResource := readResourceMap(t, session, "dropcheck://standalone/config/default")
	if configResource["operation"] != "standalone.config" {
		t.Fatalf("standalone config resource=%v", configResource)
	}
	runResource := readResourceMap(t, session, "dropcheck://standalone/run/serial-1/run-1")
	if runResource["operation"] != "standalone.run" {
		t.Fatalf("standalone run resource=%v", runResource)
	}
}

func TestPromptsDescribeDropcheckWorkflows(t *testing.T) {
	session, cleanup := connectMCP(t, newFakeBackend())
	defer cleanup()

	prompts, err := session.ListPrompts(context.Background(), &mcp.ListPromptsParams{})
	if err != nil {
		t.Fatalf("ListPrompts: %v", err)
	}
	var names []string
	for _, prompt := range prompts.Prompts {
		names = append(names, prompt.Name)
	}
	for _, name := range []string{"dropcheck_connectivity_check", "dropcheck_mlo_investigation", "dropcheck_noc_smoke_check"} {
		if !slices.Contains(names, name) {
			t.Fatalf("prompt %s not listed; got %v", name, names)
		}
	}

	prompt, err := session.GetPrompt(context.Background(), &mcp.GetPromptParams{
		Name: "dropcheck_mlo_investigation",
		Arguments: map[string]string{
			"target": "serial-1",
			"essid":  "Lab",
		},
	})
	if err != nil {
		t.Fatalf("GetPrompt: %v", err)
	}
	if len(prompt.Messages) != 1 {
		t.Fatalf("prompt messages=%d, want 1", len(prompt.Messages))
	}
	text := prompt.Messages[0].Content.(*mcp.TextContent).Text
	for _, want := range []string{"dropcheck_wifi_mlo", "fresh=true", "dropcheck_adb_diagnostics", "Lab"} {
		if !strings.Contains(text, want) {
			t.Fatalf("prompt text missing %q: %s", want, text)
		}
	}
}

func TestFirstClassWifiToolsCoverShellOperations(t *testing.T) {
	backend := newFakeBackend()
	session, cleanup := connectMCP(t, backend)
	defer cleanup()

	if result, structured := callTool(t, session, "dropcheck_wifi_mlo", map[string]any{"target": "serial-1"}); result.IsError {
		t.Fatalf("wifi mlo IsError=true structured=%v", structured)
	}
	if result, structured := callTool(t, session, "dropcheck_wifi_mlo", map[string]any{
		"target":     "serial-1",
		"fresh":      true,
		"timeout_ms": float64(9000),
	}); result.IsError {
		t.Fatalf("wifi mlo fresh IsError=true structured=%v", structured)
	}
	if result, structured := callTool(t, session, "dropcheck_wifi_monitor", map[string]any{
		"target":      "serial-1",
		"duration_ms": float64(15000),
		"interval_ms": float64(500),
	}); result.IsError {
		t.Fatalf("wifi monitor IsError=true structured=%v", structured)
	}
	if result, structured := callTool(t, session, "dropcheck_wifi_cycle", map[string]any{
		"target":            "serial-1",
		"essid":             "Lab",
		"passphrase":        "secret",
		"security":          "wpa3",
		"band":              "6ghz",
		"count":             float64(2),
		"ping_host":         "1.1.1.1",
		"http_url":          "https://example.test/health",
		"forget_after_each": true,
		"pause_ms":          float64(250),
	}); result.IsError {
		t.Fatalf("wifi cycle IsError=true structured=%v", structured)
	}
	if result, structured := callTool(t, session, "dropcheck_wifi_wait_connected", map[string]any{
		"target":            "serial-1",
		"essid":             "Lab",
		"bssid":             "aa:bb:cc:dd:ee:ff",
		"security":          "wpa3",
		"band":              "6ghz",
		"require_ip":        true,
		"require_validated": true,
		"timeout_ms":        float64(12000),
	}); result.IsError {
		t.Fatalf("wifi wait IsError=true structured=%v", structured)
	}
	if result, structured := callTool(t, session, "dropcheck_wifi_assert", map[string]any{
		"target":     "serial-1",
		"essid":      "Lab",
		"require_ip": true,
		"timeout_ms": float64(5000),
	}); result.IsError {
		t.Fatalf("wifi assert IsError=true structured=%v", structured)
	}

	backend.mu.Lock()
	defer backend.mu.Unlock()
	if len(backend.runs) != 6 {
		t.Fatalf("runs=%d, want 6", len(backend.runs))
	}
	if backend.runs[0].op.Name != "wifi.mlo" {
		t.Fatalf("mlo operation=%s", backend.runs[0].op.Name)
	}
	if backend.runs[1].op.Name != "wifi.mlo" || !backend.runs[1].op.Options.WifiMLOFreshScan || backend.runs[1].op.Options.WifiMLOFreshScanTimeoutMs != 9000 {
		t.Fatalf("mlo fresh operation=%s options=%#v", backend.runs[1].op.Name, backend.runs[1].op.Options)
	}
	monitor := backend.runs[2].op.Command.GetMonitorWifi()
	if monitor == nil || monitor.GetDurationMs() != 15000 || monitor.GetIntervalMs() != 500 {
		t.Fatalf("MonitorWifi=%#v", monitor)
	}
	cycle := backend.runs[3].op.Command.GetCycleWifi()
	if cycle == nil {
		t.Fatalf("CycleWifi command missing")
	}
	if cycle.GetConnect().GetSsid() != "Lab" || cycle.GetConnect().GetPassphrase() != "secret" || cycle.GetConnect().GetBand() != controlpb.WifiBand_WIFI_BAND_6_GHZ {
		t.Fatalf("CycleWifi.Connect=%#v", cycle.GetConnect())
	}
	if cycle.GetCount() != 2 || cycle.GetPingHost() != "1.1.1.1" || cycle.GetHttpUrl() != "https://example.test/health" || !cycle.GetForgetAfterEach() || cycle.GetPauseMs() != 250 {
		t.Fatalf("CycleWifi=%#v", cycle)
	}
	wait := backend.runs[4].op.Command.GetWaitWifiConnected()
	if wait == nil ||
		wait.GetSsid() != "Lab" ||
		wait.GetBssid() != "aa:bb:cc:dd:ee:ff" ||
		wait.GetSecurity() != controlpb.ConnectWifi_SECURITY_WPA3_SAE ||
		wait.GetBand() != controlpb.WifiBand_WIFI_BAND_6_GHZ ||
		!wait.GetRequireIp() ||
		!wait.GetRequireValidated() ||
		wait.GetTimeoutMs() != 12000 {
		t.Fatalf("WaitWifiConnected=%#v", wait)
	}
	assert := backend.runs[5].op.Command.GetAssertWifi()
	if assert == nil ||
		assert.GetSsid() != "Lab" ||
		!assert.GetRequireIp() ||
		assert.GetTimeoutMs() != 5000 {
		t.Fatalf("AssertWifi=%#v", assert)
	}
}

func TestWifiConnectUsesPassphraseEnvAndRedactsOutput(t *testing.T) {
	backend := newFakeBackend()
	session, cleanup := connectMCP(t, backend)
	defer cleanup()
	t.Setenv("DROPCHECK_TEST_PSK", "super-secret")

	result, structured := callTool(t, session, "dropcheck_wifi_connect", map[string]any{
		"target":         "serial-1",
		"essid":          "Lab",
		"passphrase_env": "DROPCHECK_TEST_PSK",
		"security":       "auto",
		"band":           "5ghz",
		"timeout_ms":     25000,
	})
	if result.IsError {
		t.Fatalf("wifi connect IsError=true structured=%v", structured)
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if len(backend.runs) != 1 {
		t.Fatalf("runs=%d, want 1", len(backend.runs))
	}
	run := backend.runs[0]
	if run.target != "serial-1" {
		t.Fatalf("target=%q", run.target)
	}
	connect := run.op.Command.GetConnectWifi()
	if connect == nil {
		t.Fatalf("ConnectWifi command missing")
	}
	if connect.GetSsid() != "Lab" || connect.GetPassphrase() != "super-secret" || connect.GetBand() != controlpb.WifiBand_WIFI_BAND_5_GHZ || connect.GetTimeoutMs() != 25000 {
		t.Fatalf("ConnectWifi=%#v", connect)
	}
	encoded, _ := json.Marshal(structured)
	if strings.Contains(string(encoded), "super-secret") {
		t.Fatalf("structured output leaked passphrase: %s", encoded)
	}
	assertContentIncludesStructuredJSON(t, result, structured)
	for _, content := range result.Content {
		if text := content.(*mcp.TextContent).Text; strings.Contains(text, "super-secret") {
			t.Fatalf("content leaked passphrase: %s", text)
		}
	}
}

func TestADBDiagnosticsToolCollectsHostDiagnostics(t *testing.T) {
	backend := newFakeBackend()
	session, cleanup := connectMCP(t, backend)
	defer cleanup()

	result, structured := callTool(t, session, "dropcheck_adb_diagnostics", map[string]any{
		"target": "serial-1",
		"kind":   "cmd-wifi-status",
	})
	if result.IsError {
		t.Fatalf("adb diagnostics IsError=true structured=%v", structured)
	}
	diagnostics, ok := structured["diagnostics"].(map[string]any)
	if !ok {
		t.Fatalf("diagnostics=%T %[1]v", structured["diagnostics"])
	}
	if diagnostics["kind"] != "cmd-wifi-status" {
		t.Fatalf("diagnostics.kind=%v structured=%v", diagnostics["kind"], structured)
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if len(backend.adbRuns) != 1 || backend.adbRuns[0].target != "serial-1" || backend.adbRuns[0].kind != "cmd-wifi-status" {
		t.Fatalf("adbRuns=%#v", backend.adbRuns)
	}
}

func TestDropcheckRunSequencesConnectWaitChecksAndCleanup(t *testing.T) {
	backend := newFakeBackend()
	session, cleanup := connectMCP(t, backend)
	defer cleanup()

	result, structured := callTool(t, session, "dropcheck_run", map[string]any{
		"target":           "serial-1",
		"essid":            "Lab",
		"passphrase":       "secret",
		"security":         "auto",
		"band":             "5ghz",
		"checks":           []string{"wifi_status", "ip_status", "ping", "dns", "http"},
		"ping_host":        "1.1.1.1",
		"dns_name":         "example.com",
		"disconnect_after": true,
		"forget_after":     true,
	})
	if result.IsError {
		t.Fatalf("dropcheck_run IsError=true structured=%v", structured)
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	var names []string
	for _, run := range backend.runs {
		names = append(names, run.op.Name)
	}
	want := []string{"wifi.connect", "wifi.wait", "wifi.status", "ip.status", "ping", "dns", "http", "wifi.disconnect", "wifi.forget"}
	if !slices.Equal(names, want) {
		t.Fatalf("operations=%v, want %v", names, want)
	}
	wait := backend.runs[1].op.Command.GetWaitWifiConnected()
	if wait == nil || wait.GetSsid() != "Lab" || !wait.GetRequireIp() || wait.GetBand() != controlpb.WifiBand_WIFI_BAND_5_GHZ {
		t.Fatalf("WaitWifiConnected=%#v", wait)
	}
	steps, ok := structured["steps"].([]any)
	if !ok || len(steps) != len(want) {
		t.Fatalf("steps=%T %[1]v", structured["steps"])
	}
}

func TestFailedAgentStatusBecomesToolError(t *testing.T) {
	backend := newFakeBackend()
	backend.statusByOp["ping"] = controlpb.CommandResult_STATUS_FAILED
	session, cleanup := connectMCP(t, backend)
	defer cleanup()

	result, structured := callTool(t, session, "dropcheck_ping", map[string]any{
		"target": "serial-1",
		"host":   "1.1.1.1",
		"count":  1,
	})
	if !result.IsError {
		t.Fatalf("IsError=false structured=%v", structured)
	}
	if structured["success"] != false || structured["status"] != "failed" {
		t.Fatalf("structured=%v", structured)
	}
	assertContentIncludesStructuredJSON(t, result, structured)
}

func TestDropcheckCommandParsesCLIGrammar(t *testing.T) {
	backend := newFakeBackend()
	session, cleanup := connectMCP(t, backend)
	defer cleanup()

	result, structured := callTool(t, session, "dropcheck_command", map[string]any{
		"target":  "serial-1",
		"command": "request wifi forget Lab",
	})
	if result.IsError {
		t.Fatalf("dropcheck_command IsError=true structured=%v", structured)
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if len(backend.runs) != 1 {
		t.Fatalf("runs=%d, want 1", len(backend.runs))
	}
	if backend.runs[0].op.Name != "wifi.forget" {
		t.Fatalf("operation=%s", backend.runs[0].op.Name)
	}
	forget := backend.runs[0].op.Command.GetForgetWifi()
	if forget == nil || forget.GetTarget() != "Lab" {
		t.Fatalf("ForgetWifi=%#v", forget)
	}
}

func TestDropcheckCommandRunsAllAgentsConcurrently(t *testing.T) {
	backend := newFakeBackend()
	backend.agents = append(backend.agents, mcpserver.Agent{
		Number:       2,
		ID:           "serial-2",
		ADBSerial:    "serial-2",
		SessionID:    "session-2",
		AppVersion:   "0.1.0",
		Manufacturer: "Google",
		Model:        "Pixel",
		SDK:          36,
		Connected:    time.Unix(1700000002, 0),
	})
	backend.runDelay = 50 * time.Millisecond
	session, cleanup := connectMCP(t, backend)
	defer cleanup()

	result, structured := callTool(t, session, "dropcheck_command", map[string]any{
		"all":     true,
		"command": "request ping 1.1.1.1",
	})
	if result.IsError {
		t.Fatalf("dropcheck_command IsError=true structured=%v", structured)
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if len(backend.runs) != 2 {
		t.Fatalf("runs=%d, want 2", len(backend.runs))
	}
	if backend.maxInFlight < 2 {
		t.Fatalf("maxInFlight=%d, want concurrent runs", backend.maxInFlight)
	}
}

func TestSessionStartAndStopTools(t *testing.T) {
	backend := newFakeBackend()
	session, cleanup := connectMCP(t, backend)
	defer cleanup()

	result, structured := callTool(t, session, "dropcheck_session_start", map[string]any{
		"adb_path":     "adb-test",
		"serial":       "serial-1",
		"package_name": "io.dropcheck.agent.test",
		"listen_addr":  "127.0.0.1:0",
	})
	if result.IsError {
		t.Fatalf("session_start IsError=true structured=%v", structured)
	}
	_, structured = callTool(t, session, "dropcheck_session_stop", map[string]any{})
	if structured["success"] != true {
		t.Fatalf("stop structured=%v", structured)
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if len(backend.startOpts) != 1 || backend.startOpts[0].ADBPath != "adb-test" || backend.stopCount != 1 {
		t.Fatalf("startOpts=%v stopCount=%d", backend.startOpts, backend.stopCount)
	}
}
