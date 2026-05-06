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
	mu         sync.Mutex
	agents     []mcpserver.Agent
	runs       []runRecord
	statusByOp map[string]controlpb.CommandResult_Status
	started    bool
	startOpts  []mcpserver.SessionStartOptions
	stopCount  int
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
	return mcpserver.Execution{
		Agent:        b.agents[0],
		CommandID:    "cmd-" + op.Name,
		Operation:    op.Name,
		CommandLabel: cmd.GetLabel(),
		Result:       result,
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
		"dropcheck_session_start": {"success", "session", "error"},
		"dropcheck_agents":        {"success", "agents", "error"},
		"dropcheck_ping":          {"success", "operation", "status", "elapsed_ms", "result"},
		"dropcheck_command":       {"success", "operation", "results", "agents", "error"},
		"dropcheck_run":           {"success", "steps", "failed_step", "partial", "error"},
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

func TestFirstClassWifiToolsCoverShellOperations(t *testing.T) {
	backend := newFakeBackend()
	session, cleanup := connectMCP(t, backend)
	defer cleanup()

	if result, structured := callTool(t, session, "dropcheck_wifi_mlo", map[string]any{"target": "serial-1"}); result.IsError {
		t.Fatalf("wifi mlo IsError=true structured=%v", structured)
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

	backend.mu.Lock()
	defer backend.mu.Unlock()
	if len(backend.runs) != 3 {
		t.Fatalf("runs=%d, want 3", len(backend.runs))
	}
	if backend.runs[0].op.Name != "wifi.mlo" {
		t.Fatalf("mlo operation=%s", backend.runs[0].op.Name)
	}
	monitor := backend.runs[1].op.Command.GetMonitorWifi()
	if monitor == nil || monitor.GetDurationMs() != 15000 || monitor.GetIntervalMs() != 500 {
		t.Fatalf("MonitorWifi=%#v", monitor)
	}
	cycle := backend.runs[2].op.Command.GetCycleWifi()
	if cycle == nil {
		t.Fatalf("CycleWifi command missing")
	}
	if cycle.GetConnect().GetSsid() != "Lab" || cycle.GetConnect().GetPassphrase() != "secret" || cycle.GetConnect().GetBand() != controlpb.WifiBand_WIFI_BAND_6_GHZ {
		t.Fatalf("CycleWifi.Connect=%#v", cycle.GetConnect())
	}
	if cycle.GetCount() != 2 || cycle.GetPingHost() != "1.1.1.1" || cycle.GetHttpUrl() != "https://example.test/health" || !cycle.GetForgetAfterEach() || cycle.GetPauseMs() != 250 {
		t.Fatalf("CycleWifi=%#v", cycle)
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
