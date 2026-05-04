package mcpserver

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	dropcmd "dropcheck/controller/internal/command"
	"dropcheck/controller/internal/controlpb"
	"dropcheck/controller/internal/linuxcli"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type noArgs struct{}

type targetArgs struct {
	Target string `json:"target,omitempty" jsonschema:"agent target: number, adb serial, agent ID, or unique prefix; omit when one agent is connected"`
}

type wifiConnectArgs struct {
	Target           string `json:"target,omitempty" jsonschema:"agent target; omit when one agent is connected"`
	ESSID            string `json:"essid" jsonschema:"Wi-Fi ESSID/SSID to connect to"`
	Passphrase       string `json:"passphrase,omitempty" jsonschema:"Wi-Fi passphrase; prefer passphrase_env so secrets stay out of transcripts"`
	PassphraseEnv    string `json:"passphrase_env,omitempty" jsonschema:"environment variable containing the Wi-Fi passphrase"`
	Security         string `json:"security,omitempty" jsonschema:"auto, wpa2, wpa3, or transition"`
	BSSID            string `json:"bssid,omitempty" jsonschema:"optional AP BSSID to pin the connection"`
	Band             string `json:"band,omitempty" jsonschema:"all, 2.4ghz, 5ghz, 6ghz, or 60ghz"`
	MacRandomization string `json:"mac_randomization,omitempty" jsonschema:"auto, none, persistent, or non-persistent"`
	TimeoutMS        uint32 `json:"timeout_ms,omitempty" jsonschema:"connect timeout in milliseconds"`
}

type wifiScanArgs struct {
	Target    string `json:"target,omitempty" jsonschema:"agent target; omit when one agent is connected"`
	Band      string `json:"band,omitempty" jsonschema:"all, 2.4ghz, 5ghz, 6ghz, or 60ghz"`
	Fresh     bool   `json:"fresh,omitempty" jsonschema:"request a fresh Android scan instead of cached scan results"`
	TimeoutMS uint32 `json:"timeout_ms,omitempty" jsonschema:"fresh scan timeout in milliseconds"`
}

type wifiScanDetailArgs struct {
	TargetAgent string `json:"target,omitempty" jsonschema:"agent target; omit when one agent is connected"`
	Target      string `json:"scan_target" jsonschema:"ESSID/SSID or BSSID to inspect in scan results"`
	Band        string `json:"band,omitempty" jsonschema:"all, 2.4ghz, 5ghz, 6ghz, or 60ghz"`
}

type wifiForgetArgs struct {
	TargetAgent string `json:"target,omitempty" jsonschema:"agent target; omit when one agent is connected"`
	Network     string `json:"network" jsonschema:"ESSID/SSID, BSSID, or Android network ID to forget"`
}

type wifiReconnectArgs struct {
	Target    string `json:"target,omitempty" jsonschema:"agent target; omit when one agent is connected"`
	TimeoutMS uint32 `json:"timeout_ms,omitempty" jsonschema:"reconnect timeout in milliseconds"`
}

type wifiExpectationArgs struct {
	Target           string `json:"target,omitempty" jsonschema:"agent target; omit when one agent is connected"`
	ESSID            string `json:"essid,omitempty" jsonschema:"expected ESSID/SSID"`
	BSSID            string `json:"bssid,omitempty" jsonschema:"expected AP BSSID"`
	Security         string `json:"security,omitempty" jsonschema:"auto, wpa2, wpa3, or transition"`
	Band             string `json:"band,omitempty" jsonschema:"all, 2.4ghz, 5ghz, 6ghz, or 60ghz"`
	RequireIP        bool   `json:"require_ip,omitempty" jsonschema:"require an IP address"`
	RequireValidated bool   `json:"require_validated,omitempty" jsonschema:"require Android validated internet access"`
	TimeoutMS        uint32 `json:"timeout_ms,omitempty" jsonschema:"wait/assert timeout in milliseconds"`
}

type pingArgs struct {
	Target    string `json:"target,omitempty" jsonschema:"agent target; omit when one agent is connected"`
	Host      string `json:"host" jsonschema:"host or IP address to ping"`
	Count     uint32 `json:"count,omitempty" jsonschema:"packet count"`
	SizeBytes uint32 `json:"size_bytes,omitempty" jsonschema:"ICMP payload size in bytes"`
	TimeoutMS uint32 `json:"timeout_ms,omitempty" jsonschema:"operation timeout in milliseconds"`
}

type tracerouteArgs struct {
	Target    string   `json:"target,omitempty" jsonschema:"agent target; omit when one agent is connected"`
	Host      string   `json:"host" jsonschema:"host or IP address to trace"`
	MaxHops   uint32   `json:"max_hops,omitempty" jsonschema:"maximum hop count"`
	Via       []string `json:"via,omitempty" jsonschema:"hops that should appear in rendered validation"`
	SizeBytes uint32   `json:"size_bytes,omitempty" jsonschema:"probe payload size in bytes"`
	TimeoutMS uint32   `json:"timeout_ms,omitempty" jsonschema:"operation timeout in milliseconds"`
}

type pathMTUArgs struct {
	Target    string `json:"target,omitempty" jsonschema:"agent target; omit when one agent is connected"`
	Host      string `json:"host" jsonschema:"host or IP address to probe"`
	MinMTU    uint32 `json:"min_mtu,omitempty" jsonschema:"minimum MTU bytes"`
	MaxMTU    uint32 `json:"max_mtu,omitempty" jsonschema:"maximum MTU bytes"`
	TimeoutMS uint32 `json:"timeout_ms,omitempty" jsonschema:"operation timeout in milliseconds"`
}

type globalIPArgs struct {
	Target    string `json:"target,omitempty" jsonschema:"agent target; omit when one agent is connected"`
	Family    string `json:"family,omitempty" jsonschema:"ipv4, ipv6, or all"`
	TimeoutMS uint32 `json:"timeout_ms,omitempty" jsonschema:"operation timeout in milliseconds"`
}

type dnsArgs struct {
	Target    string `json:"target,omitempty" jsonschema:"agent target; omit when one agent is connected"`
	Name      string `json:"name" jsonschema:"DNS name to resolve"`
	Type      string `json:"type,omitempty" jsonschema:"A, AAAA, or ALL"`
	TimeoutMS uint32 `json:"timeout_ms,omitempty" jsonschema:"operation timeout in milliseconds"`
}

type httpArgs struct {
	Target         string `json:"target,omitempty" jsonschema:"agent target; omit when one agent is connected"`
	URL            string `json:"url" jsonschema:"URL to request; URLs without scheme default to https"`
	ExpectedStatus uint32 `json:"expected_status,omitempty" jsonschema:"expected HTTP status code"`
	TimeoutMS      uint32 `json:"timeout_ms,omitempty" jsonschema:"operation timeout in milliseconds"`
}

type downloadArgs struct {
	Target    string `json:"target,omitempty" jsonschema:"agent target; omit when one agent is connected"`
	URL       string `json:"url" jsonschema:"URL to download"`
	TimeoutMS uint32 `json:"timeout_ms,omitempty" jsonschema:"operation timeout in milliseconds"`
}

type standaloneRunsArgs struct {
	Target        string `json:"target,omitempty" jsonschema:"agent target; omit when one agent is connected"`
	Limit         uint32 `json:"limit,omitempty" jsonschema:"maximum number of runs to return"`
	IncludeSynced bool   `json:"include_synced,omitempty" jsonschema:"include already synced archives"`
}

type standaloneRunArgs struct {
	Target     string `json:"target,omitempty" jsonschema:"agent target; omit when one agent is connected"`
	RunID      string `json:"run_id" jsonschema:"standalone run ID"`
	MarkSynced bool   `json:"mark_synced,omitempty" jsonschema:"mark the archive as synced after fetching"`
}

type standaloneClearArgs struct {
	Target string `json:"target,omitempty" jsonschema:"agent target; omit when one agent is connected"`
	Mode   string `json:"mode,omitempty" jsonschema:"synced or all; defaults to synced"`
}

type standaloneRunOnceArgs struct {
	Target string `json:"target,omitempty" jsonschema:"agent target; omit when one agent is connected"`
	Festa  string `json:"festa,omitempty" jsonschema:"enabled standalone festa name; omit when exactly one is enabled"`
	Save   bool   `json:"save,omitempty" jsonschema:"save the generated standalone archive on the Android device"`
}

type standaloneEditInput struct {
	Action string   `json:"action,omitempty" jsonschema:"set or delete; defaults to set"`
	Path   []string `json:"path" jsonschema:"standalone config path segments"`
	Value  string   `json:"value,omitempty" jsonschema:"value for set edits"`
}

type standaloneEditArgs struct {
	Target string                `json:"target,omitempty" jsonschema:"agent target; omit when one agent is connected"`
	Edits  []standaloneEditInput `json:"edits" jsonschema:"standalone configuration edits"`
}

type commandArgs struct {
	Command string `json:"command" jsonschema:"dropcheck CLI command without top-level adb/session flags, for example 'request wifi disconnect'"`
	Target  string `json:"target,omitempty" jsonschema:"agent target override"`
	All     bool   `json:"all,omitempty" jsonschema:"run agent commands on all connected agents"`
}

type dropcheckRunArgs struct {
	Target           string   `json:"target,omitempty" jsonschema:"agent target; omit when one agent is connected"`
	ESSID            string   `json:"essid" jsonschema:"Wi-Fi ESSID/SSID to check"`
	Passphrase       string   `json:"passphrase,omitempty" jsonschema:"Wi-Fi passphrase; prefer passphrase_env"`
	PassphraseEnv    string   `json:"passphrase_env,omitempty" jsonschema:"environment variable containing the Wi-Fi passphrase"`
	Security         string   `json:"security,omitempty" jsonschema:"auto, wpa2, wpa3, or transition"`
	BSSID            string   `json:"bssid,omitempty" jsonschema:"optional AP BSSID to pin the connection"`
	Band             string   `json:"band,omitempty" jsonschema:"all, 2.4ghz, 5ghz, 6ghz, or 60ghz"`
	MacRandomization string   `json:"mac_randomization,omitempty" jsonschema:"auto, none, persistent, or non-persistent"`
	ConnectTimeoutMS uint32   `json:"connect_timeout_ms,omitempty" jsonschema:"connect timeout in milliseconds"`
	WaitTimeoutMS    uint32   `json:"wait_timeout_ms,omitempty" jsonschema:"post-connect wait timeout in milliseconds"`
	RequireIP        *bool    `json:"require_ip,omitempty" jsonschema:"require an IP address; defaults to true"`
	RequireValidated bool     `json:"require_validated,omitempty" jsonschema:"require Android validated internet access"`
	Checks           []string `json:"checks,omitempty" jsonschema:"checks to run after connect: wifi_status, ip_status, ping, dns, http, global_ip, scan_detail"`
	PingHost         string   `json:"ping_host,omitempty" jsonschema:"ping host; defaults to 1.1.1.1"`
	PingCount        uint32   `json:"ping_count,omitempty" jsonschema:"ping packet count"`
	DNSName          string   `json:"dns_name,omitempty" jsonschema:"DNS name; defaults to example.com"`
	DNSType          string   `json:"dns_type,omitempty" jsonschema:"A, AAAA, or ALL"`
	HTTPURL          string   `json:"http_url,omitempty" jsonschema:"HTTP check URL; defaults to Android connectivity check 204 endpoint"`
	HTTPStatus       uint32   `json:"http_status,omitempty" jsonschema:"expected HTTP status; defaults to 204 for the default URL"`
	GlobalIPFamily   string   `json:"global_ip_family,omitempty" jsonschema:"ipv4, ipv6, or all"`
	DisconnectAfter  bool     `json:"disconnect_after,omitempty" jsonschema:"disconnect Wi-Fi after checks"`
	ForgetAfter      bool     `json:"forget_after,omitempty" jsonschema:"forget the ESSID after checks"`
}

func registerTools(server *mcp.Server, backend Backend) {
	addTool[SessionStartOptions](server, "dropcheck_session_start", "Start or restart the dropcheck Android controller session over ADB.", annotations(false, new(false), true), func(ctx context.Context, in SessionStartOptions) (*mcp.CallToolResult, map[string]any, error) {
		info, err := backend.Start(ctx, in)
		if err != nil {
			return toolError(err.Error(), nil)
		}
		return toolResult(fmt.Sprintf("session started with %d agent(s)", info.AgentCount), map[string]any{"success": true, "session": info}, false)
	})

	addTool[noArgs](server, "dropcheck_session_stop", "Stop the active dropcheck controller session and remove adb reverse rules.", annotations(false, new(false), true), func(ctx context.Context, _ noArgs) (*mcp.CallToolResult, map[string]any, error) {
		if err := backend.Stop(ctx); err != nil {
			return toolError(err.Error(), nil)
		}
		return toolResult("session stopped", map[string]any{"success": true}, false)
	})

	addTool[noArgs](server, "dropcheck_agents", "List connected Android dropcheck agents.", annotations(true, nil, true), func(ctx context.Context, _ noArgs) (*mcp.CallToolResult, map[string]any, error) {
		agents, err := backend.Agents(ctx)
		if err != nil {
			return toolError(err.Error(), nil)
		}
		return toolResult(fmt.Sprintf("%d agent(s) connected", len(agents)), map[string]any{"success": true, "agents": agents}, false)
	})

	addOperationTool[targetArgs](server, backend, "dropcheck_wifi_status", "Read current Wi-Fi connection status.", annotations(true, nil, true), func(in targetArgs) (string, dropcmd.Operation, error) {
		return in.Target, dropcmd.WifiStatusOperation(), nil
	})
	addOperationTool[targetArgs](server, backend, "dropcheck_ip_status", "Read current Android network/IP status.", annotations(true, nil, true), func(in targetArgs) (string, dropcmd.Operation, error) {
		return in.Target, dropcmd.IPStatusOperation(), nil
	})
	addOperationTool[targetArgs](server, backend, "dropcheck_wifi_diagnostics", "Read Wi-Fi diagnostics, configured network diagnostics, and scan data.", annotations(true, nil, true), func(in targetArgs) (string, dropcmd.Operation, error) {
		return in.Target, dropcmd.WifiDiagnosticsOperation(), nil
	})
	addOperationTool[targetArgs](server, backend, "dropcheck_wifi_capabilities", "Read device Wi-Fi capability diagnostics.", annotations(true, nil, true), func(in targetArgs) (string, dropcmd.Operation, error) {
		return in.Target, dropcmd.WifiCapabilitiesOperation(), nil
	})

	addOperationTool[wifiScanArgs](server, backend, "dropcheck_wifi_scan", "Read cached or fresh Android Wi-Fi scan results.", annotations(true, nil, true), func(in wifiScanArgs) (string, dropcmd.Operation, error) {
		if in.Fresh {
			op, err := dropcmd.WifiFreshScanOperation(in.Band, millis(in.TimeoutMS))
			return in.Target, op, err
		}
		op, err := dropcmd.WifiScanOperation(in.Band)
		return in.Target, op, err
	})
	addOperationTool[wifiScanDetailArgs](server, backend, "dropcheck_wifi_scan_detail", "Read scan details for one ESSID/SSID or BSSID.", annotations(true, nil, true), func(in wifiScanDetailArgs) (string, dropcmd.Operation, error) {
		op, err := dropcmd.WifiScanDetailOperation(in.Target, in.Band)
		return in.TargetAgent, op, err
	})
	addOperationTool[wifiConnectArgs](server, backend, "dropcheck_wifi_connect", "Connect the Android device to a Wi-Fi ESSID/SSID.", annotations(false, new(false), false), func(in wifiConnectArgs) (string, dropcmd.Operation, error) {
		passphrase, err := passphraseValue(in.Passphrase, in.PassphraseEnv)
		if err != nil {
			return in.Target, dropcmd.Operation{}, err
		}
		op, err := dropcmd.WifiConnectOperation(dropcmd.WifiConnectOptions{
			SSID:             in.ESSID,
			Passphrase:       passphrase,
			Security:         in.Security,
			BSSID:            in.BSSID,
			Band:             in.Band,
			MacRandomization: in.MacRandomization,
			Timeout:          millis(in.TimeoutMS),
		})
		return in.Target, op, err
	})
	addOperationTool[targetArgs](server, backend, "dropcheck_wifi_disconnect", "Disconnect Wi-Fi on the Android device.", annotations(false, new(false), false), func(in targetArgs) (string, dropcmd.Operation, error) {
		return in.Target, dropcmd.WifiDisconnectOperation(), nil
	})
	addOperationTool[wifiForgetArgs](server, backend, "dropcheck_wifi_forget", "Forget a saved Wi-Fi network by ESSID/SSID, BSSID, or Android network ID.", annotations(false, new(true), false), func(in wifiForgetArgs) (string, dropcmd.Operation, error) {
		return in.TargetAgent, dropcmd.WifiForgetOperation(in.Network), nil
	})
	addOperationTool[wifiReconnectArgs](server, backend, "dropcheck_wifi_reconnect", "Request Android Wi-Fi reconnect and wait for connected state.", annotations(false, new(false), false), func(in wifiReconnectArgs) (string, dropcmd.Operation, error) {
		op, err := dropcmd.WifiReconnectOperation(millis(in.TimeoutMS))
		return in.Target, op, err
	})
	addOperationTool[wifiExpectationArgs](server, backend, "dropcheck_wifi_wait_connected", "Wait until Wi-Fi matches the requested connection state.", annotations(true, nil, true), func(in wifiExpectationArgs) (string, dropcmd.Operation, error) {
		op, err := dropcmd.WifiWaitConnectedOperation(in.ESSID, expectationOptions(in))
		return in.Target, op, err
	})
	addOperationTool[wifiExpectationArgs](server, backend, "dropcheck_wifi_assert", "Assert the current Wi-Fi state, optionally waiting up to timeout_ms.", annotations(true, nil, true), func(in wifiExpectationArgs) (string, dropcmd.Operation, error) {
		op, err := dropcmd.WifiAssertOperation(expectationOptions(in))
		return in.Target, op, err
	})

	addOperationTool[pingArgs](server, backend, "dropcheck_ping", "Run an ICMP ping from the Android device.", annotations(true, nil, true), func(in pingArgs) (string, dropcmd.Operation, error) {
		op, err := dropcmd.PingOperation(dropcmd.PingOptions{Host: in.Host, Count: number(in.Count), Size: number(in.SizeBytes), Timeout: millis(in.TimeoutMS)})
		return in.Target, op, err
	})
	addOperationTool[tracerouteArgs](server, backend, "dropcheck_traceroute", "Run traceroute from the Android device.", annotations(true, nil, true), func(in tracerouteArgs) (string, dropcmd.Operation, error) {
		op, err := dropcmd.TracerouteOperation(dropcmd.TracerouteOptions{Host: in.Host, MaxHops: number(in.MaxHops), Via: in.Via, Size: number(in.SizeBytes), Timeout: millis(in.TimeoutMS)})
		return in.Target, op, err
	})
	addOperationTool[pathMTUArgs](server, backend, "dropcheck_path_mtu", "Discover path MTU from the Android device.", annotations(true, nil, true), func(in pathMTUArgs) (string, dropcmd.Operation, error) {
		op, err := dropcmd.PathMTUOperation(dropcmd.PathMTUOptions{Host: in.Host, MinMTU: number(in.MinMTU), MaxMTU: number(in.MaxMTU), Timeout: millis(in.TimeoutMS)})
		return in.Target, op, err
	})
	addOperationTool[globalIPArgs](server, backend, "dropcheck_global_ip", "Discover the Android device's public/global IP address.", annotations(true, nil, true), func(in globalIPArgs) (string, dropcmd.Operation, error) {
		op, err := dropcmd.GlobalIPOperation(in.Family, millis(in.TimeoutMS))
		return in.Target, op, err
	})
	addOperationTool[dnsArgs](server, backend, "dropcheck_dns", "Resolve DNS from the Android device.", annotations(true, nil, true), func(in dnsArgs) (string, dropcmd.Operation, error) {
		op, err := dropcmd.DNSOperation(in.Name, in.Type, millis(in.TimeoutMS))
		return in.Target, op, err
	})
	addOperationTool[httpArgs](server, backend, "dropcheck_http", "Run an HTTP status check from the Android device.", annotations(true, nil, true), func(in httpArgs) (string, dropcmd.Operation, error) {
		op, err := dropcmd.HTTPOperation(in.URL, number(in.ExpectedStatus), millis(in.TimeoutMS))
		return in.Target, op, err
	})
	addOperationTool[downloadArgs](server, backend, "dropcheck_download", "Run an HTTP download probe from the Android device.", annotations(true, nil, true), func(in downloadArgs) (string, dropcmd.Operation, error) {
		op, err := dropcmd.DownloadOperation(in.URL, millis(in.TimeoutMS))
		return in.Target, op, err
	})

	registerStandaloneTools(server, backend)
	registerCommandTools(server, backend)
}

func registerStandaloneTools(server *mcp.Server, backend Backend) {
	addOperationTool[targetArgs](server, backend, "dropcheck_standalone_config", "Read persisted standalone dropcheck configuration.", annotations(true, nil, true), func(in targetArgs) (string, dropcmd.Operation, error) {
		return in.Target, dropcmd.StandaloneConfigOperation(), nil
	})
	addOperationTool[targetArgs](server, backend, "dropcheck_standalone_status", "Read standalone dropcheck runtime status and archive counters.", annotations(true, nil, true), func(in targetArgs) (string, dropcmd.Operation, error) {
		return in.Target, dropcmd.StandaloneStatusOperation(), nil
	})
	addOperationTool[standaloneRunsArgs](server, backend, "dropcheck_standalone_runs", "List stored standalone dropcheck run summaries.", annotations(true, nil, true), func(in standaloneRunsArgs) (string, dropcmd.Operation, error) {
		op, err := dropcmd.StandaloneListRunsOperation(dropcmd.StandaloneListOptions{Limit: number(in.Limit), IncludeSynced: in.IncludeSynced})
		return in.Target, op, err
	})
	addOperationTool[standaloneRunArgs](server, backend, "dropcheck_standalone_run", "Fetch one stored standalone dropcheck run archive.", annotations(true, nil, true), func(in standaloneRunArgs) (string, dropcmd.Operation, error) {
		op, err := dropcmd.StandaloneRunOperation(in.RunID, in.MarkSynced)
		return in.Target, op, err
	})
	addOperationTool[standaloneClearArgs](server, backend, "dropcheck_standalone_clear_runs", "Clear synced or all stored standalone dropcheck runs.", annotations(false, new(true), false), func(in standaloneClearArgs) (string, dropcmd.Operation, error) {
		op, err := dropcmd.StandaloneClearRunsOperation(in.Mode)
		return in.Target, op, err
	})
	addOperationTool[standaloneRunOnceArgs](server, backend, "dropcheck_standalone_run_once", "Run one enabled standalone dropcheck festa immediately.", annotations(false, new(false), false), func(in standaloneRunOnceArgs) (string, dropcmd.Operation, error) {
		op, err := dropcmd.StandaloneRunOnceOperation(dropcmd.StandaloneRunOptions{Festa: in.Festa, Save: in.Save})
		return in.Target, op, err
	})
	addOperationTool[standaloneEditArgs](server, backend, "dropcheck_standalone_config_edit", "Apply standalone dropcheck configuration edits.", annotations(false, new(true), false), func(in standaloneEditArgs) (string, dropcmd.Operation, error) {
		edits := make([]dropcmd.StandaloneEdit, 0, len(in.Edits))
		for _, edit := range in.Edits {
			edits = append(edits, dropcmd.StandaloneEdit{Action: edit.Action, Path: edit.Path, Value: edit.Value})
		}
		op, err := dropcmd.StandaloneEditOperation(edits)
		return in.Target, op, err
	})
}

func registerCommandTools(server *mcp.Server, backend Backend) {
	addTool[commandArgs](server, "dropcheck_command", "Execute a dropcheck CLI-shaped command through MCP. This parses dropcheck grammar, not OS shell commands.", annotations(false, new(false), false), func(ctx context.Context, in commandArgs) (*mcp.CallToolResult, map[string]any, error) {
		args, err := dropcmd.SplitArgs(in.Command)
		if err != nil {
			return toolError(err.Error(), map[string]any{"command": in.Command})
		}
		if len(args) > 0 && args[0] == "dropcheck" {
			args = args[1:]
		}
		cliOpts, rest, err := linuxcli.ExtractOptions(args)
		if err != nil {
			return toolError(err.Error(), map[string]any{"command": in.Command})
		}
		parsed, err := linuxcli.Parse(rest)
		if err != nil {
			return toolError(err.Error(), map[string]any{"command": in.Command})
		}
		target := in.Target
		if target == "" {
			target = cliOpts.Target
		}
		all := in.All || cliOpts.All
		switch parsed.Kind {
		case linuxcli.Devices:
			agents, err := backend.Agents(ctx)
			if err != nil {
				return toolError(err.Error(), map[string]any{"command": in.Command})
			}
			return toolResult(fmt.Sprintf("%d agent(s) connected", len(agents)), map[string]any{"success": true, "agents": agents}, false)
		case linuxcli.Config:
			if parsed.ConfigScope != "" && parsed.ConfigScope != "all" && parsed.ConfigScope != "standalone" {
				return toolError("unsupported config scope "+parsed.ConfigScope, map[string]any{"command": in.Command})
			}
			return runOperationMaybeAll(ctx, backend, target, all, dropcmd.StandaloneConfigOperation())
		case linuxcli.AgentCommand:
			return runOperationMaybeAll(ctx, backend, target, all, parsed.Operation)
		case linuxcli.StandaloneSync:
			return toolError("sync standalone runs writes host files and is not exposed through MCP; use dropcheck_standalone_runs and dropcheck_standalone_run", map[string]any{"command": in.Command})
		default:
			return toolError("unsupported dropcheck command", map[string]any{"command": in.Command})
		}
	})

	addTool[dropcheckRunArgs](server, "dropcheck_run", "Connect to an ESSID and run a complete connectivity check sequence from the Android device.", annotations(false, new(false), false), func(ctx context.Context, in dropcheckRunArgs) (*mcp.CallToolResult, map[string]any, error) {
		return runDropcheck(ctx, backend, in)
	})
}

func addTool[In any](server *mcp.Server, name string, description string, toolAnnotations *mcp.ToolAnnotations, handler func(context.Context, In) (*mcp.CallToolResult, map[string]any, error)) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        name,
		Title:       strings.TrimPrefix(name, "dropcheck_"),
		Description: description,
		Annotations: toolAnnotations,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, map[string]any, error) {
		return handler(ctx, in)
	})
}

func addOperationTool[In any](server *mcp.Server, backend Backend, name string, description string, toolAnnotations *mcp.ToolAnnotations, build func(In) (string, dropcmd.Operation, error)) {
	addTool[In](server, name, description, toolAnnotations, func(ctx context.Context, in In) (*mcp.CallToolResult, map[string]any, error) {
		target, op, err := build(in)
		if err != nil {
			return toolError(err.Error(), map[string]any{"tool": name})
		}
		exec, err := backend.Run(ctx, target, op)
		if err != nil {
			return toolError(err.Error(), map[string]any{"tool": name, "operation": op.Name, "target": target})
		}
		return executionToolResult(exec)
	})
}

func runOperationMaybeAll(ctx context.Context, backend Backend, target string, all bool, op dropcmd.Operation) (*mcp.CallToolResult, map[string]any, error) {
	if all || target == "all" {
		agents, err := backend.Agents(ctx)
		if err != nil {
			return toolError(err.Error(), map[string]any{"operation": op.Name})
		}
		if len(agents) == 0 {
			return toolError("no Android agents connected", map[string]any{"operation": op.Name})
		}
		execs := make([]Execution, 0, len(agents))
		for _, agent := range agents {
			exec, err := backend.Run(ctx, agent.ID, op)
			if err != nil {
				return toolError(err.Error(), map[string]any{"operation": op.Name, "target": agent.ID})
			}
			execs = append(execs, exec)
		}
		return executionsToolResult(execs)
	}
	exec, err := backend.Run(ctx, target, op)
	if err != nil {
		return toolError(err.Error(), map[string]any{"operation": op.Name, "target": target})
	}
	return executionToolResult(exec)
}

func runDropcheck(ctx context.Context, backend Backend, in dropcheckRunArgs) (*mcp.CallToolResult, map[string]any, error) {
	passphrase, err := passphraseValue(in.Passphrase, in.PassphraseEnv)
	if err != nil {
		return toolError(err.Error(), map[string]any{"essid": in.ESSID})
	}
	requireIP := true
	if in.RequireIP != nil {
		requireIP = *in.RequireIP
	}
	var execs []Execution
	runStep := func(op dropcmd.Operation) (bool, error) {
		exec, err := backend.Run(ctx, in.Target, op)
		if err != nil {
			return false, err
		}
		execs = append(execs, exec)
		return exec.Result != nil && exec.Result.GetStatus() == controlpb.CommandResult_STATUS_OK, nil
	}
	connect, err := dropcmd.WifiConnectOperation(dropcmd.WifiConnectOptions{
		SSID:             in.ESSID,
		Passphrase:       passphrase,
		Security:         in.Security,
		BSSID:            in.BSSID,
		Band:             in.Band,
		MacRandomization: in.MacRandomization,
		Timeout:          millis(in.ConnectTimeoutMS),
	})
	if err != nil {
		return toolError(err.Error(), map[string]any{"essid": in.ESSID})
	}
	ok, err := runStep(connect)
	if err != nil {
		return dropcheckPartialError(err.Error(), execs)
	}
	if ok {
		wait, err := dropcmd.WifiWaitConnectedOperation(in.ESSID, dropcmd.WifiExpectationOptions{
			BSSID:            in.BSSID,
			Security:         in.Security,
			Band:             in.Band,
			RequireIP:        requireIP,
			RequireValidated: in.RequireValidated,
			Timeout:          millis(in.WaitTimeoutMS),
		})
		if err != nil {
			return toolError(err.Error(), map[string]any{"essid": in.ESSID})
		}
		ok, err = runStep(wait)
		if err != nil {
			return dropcheckPartialError(err.Error(), execs)
		}
	}
	if ok {
		for _, check := range normalizedChecks(in.Checks) {
			op, err := dropcheckCheckOperation(check, in)
			if err != nil {
				return dropcheckPartialError(err.Error(), execs)
			}
			ok, err = runStep(op)
			if err != nil {
				return dropcheckPartialError(err.Error(), execs)
			}
			if !ok {
				break
			}
		}
	}
	if in.DisconnectAfter {
		_, err = runStep(dropcmd.WifiDisconnectOperation())
		if err != nil {
			return dropcheckPartialError(err.Error(), execs)
		}
	}
	if in.ForgetAfter {
		_, err = runStep(dropcmd.WifiForgetOperation(in.ESSID))
		if err != nil {
			return dropcheckPartialError(err.Error(), execs)
		}
	}
	return dropcheckRunResult(execs)
}

func dropcheckCheckOperation(check string, in dropcheckRunArgs) (dropcmd.Operation, error) {
	switch check {
	case "wifi_status":
		return dropcmd.WifiStatusOperation(), nil
	case "ip_status":
		return dropcmd.IPStatusOperation(), nil
	case "ping":
		host := in.PingHost
		if host == "" {
			host = "1.1.1.1"
		}
		return dropcmd.PingOperation(dropcmd.PingOptions{Host: host, Count: number(in.PingCount)})
	case "dns":
		name := in.DNSName
		if name == "" {
			name = "example.com"
		}
		return dropcmd.DNSOperation(name, in.DNSType, "")
	case "http":
		url := in.HTTPURL
		status := number(in.HTTPStatus)
		if url == "" {
			url = "http://connectivitycheck.gstatic.com/generate_204"
			if status == "" {
				status = "204"
			}
		}
		return dropcmd.HTTPOperation(url, status, "")
	case "global_ip":
		family := in.GlobalIPFamily
		if family == "" {
			family = "ipv4"
		}
		return dropcmd.GlobalIPOperation(family, "")
	case "scan_detail":
		return dropcmd.WifiScanDetailOperation(in.ESSID, in.Band)
	default:
		return dropcmd.Operation{}, fmt.Errorf("unsupported dropcheck check %q", check)
	}
}

func dropcheckRunResult(execs []Execution) (*mcp.CallToolResult, map[string]any, error) {
	steps := make([]map[string]any, 0, len(execs))
	success := true
	var failedStep string
	for _, exec := range execs {
		step, ok, _, err := executionMap(exec)
		if err != nil {
			return toolError(err.Error(), map[string]any{"operation": exec.Operation})
		}
		steps = append(steps, step)
		if !ok && failedStep == "" {
			success = false
			failedStep = exec.Operation
		}
	}
	out := map[string]any{
		"success": success,
		"steps":   steps,
	}
	if failedStep != "" {
		out["failed_step"] = failedStep
	}
	text := fmt.Sprintf("dropcheck completed: %d step(s)", len(steps))
	if !success {
		text = "dropcheck failed at " + failedStep
	}
	return toolResult(text, out, !success)
}

func dropcheckPartialError(message string, execs []Execution) (*mcp.CallToolResult, map[string]any, error) {
	_, partial, _ := dropcheckRunResult(execs)
	out := map[string]any{"success": false, "error": message}
	if partial != nil {
		out["partial"] = partial
	}
	return toolResult(message, out, true)
}

func expectationOptions(in wifiExpectationArgs) dropcmd.WifiExpectationOptions {
	return dropcmd.WifiExpectationOptions{
		SSID:             in.ESSID,
		BSSID:            in.BSSID,
		Security:         in.Security,
		Band:             in.Band,
		RequireIP:        in.RequireIP,
		RequireValidated: in.RequireValidated,
		Timeout:          millis(in.TimeoutMS),
	}
}

func passphraseValue(passphrase, envName string) (string, error) {
	if passphrase != "" {
		return passphrase, nil
	}
	if envName == "" {
		return "", fmt.Errorf("passphrase or passphrase_env is required")
	}
	value, ok := os.LookupEnv(envName)
	if !ok || value == "" {
		return "", fmt.Errorf("environment variable %s is not set or empty", envName)
	}
	return value, nil
}

func normalizedChecks(checks []string) []string {
	if len(checks) == 0 {
		return []string{"wifi_status", "ip_status", "ping", "dns", "http"}
	}
	out := make([]string, 0, len(checks))
	for _, check := range checks {
		check = strings.ToLower(strings.TrimSpace(check))
		check = strings.ReplaceAll(check, "-", "_")
		if check != "" {
			out = append(out, check)
		}
	}
	return out
}

func millis(value uint32) string {
	return number(value)
}

func number(value uint32) string {
	if value == 0 {
		return ""
	}
	return strconv.FormatUint(uint64(value), 10)
}
