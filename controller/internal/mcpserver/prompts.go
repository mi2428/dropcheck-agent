package mcpserver

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func registerPrompts(server *mcp.Server) {
	server.AddPrompt(&mcp.Prompt{
		Name:        "dropcheck_connectivity_check",
		Title:       "Dropcheck Connectivity Check",
		Description: "Plan and run an Android-side Wi-Fi connectivity check with dropcheck_run.",
		Arguments: []*mcp.PromptArgument{
			{Name: "essid", Description: "Wi-Fi ESSID/SSID to validate", Required: true},
			{Name: "target", Description: "Agent target, serial, or default", Required: false},
			{Name: "ping_host", Description: "Ping host to probe", Required: false},
			{Name: "dns_name", Description: "DNS name to resolve", Required: false},
			{Name: "http_url", Description: "HTTP URL to check", Required: false},
		},
	}, connectivityPrompt)

	server.AddPrompt(&mcp.Prompt{
		Name:        "dropcheck_mlo_investigation",
		Title:       "Dropcheck MLO Investigation",
		Description: "Investigate Wi-Fi 7 MLO state using fresh scans, MLO diagnostics, and adb snapshots.",
		Arguments: []*mcp.PromptArgument{
			{Name: "target", Description: "Agent target, serial, or default", Required: false},
			{Name: "essid", Description: "ESSID/SSID to focus on", Required: false},
		},
	}, mloInvestigationPrompt)

	server.AddPrompt(&mcp.Prompt{
		Name:        "dropcheck_noc_smoke_check",
		Title:       "Dropcheck NOC Smoke Check",
		Description: "Run a conservative NOC-facing smoke check across agent, Wi-Fi, IP, and probe state.",
		Arguments: []*mcp.PromptArgument{
			{Name: "target", Description: "Agent target, serial, all, or default", Required: false},
		},
	}, nocSmokePrompt)
}

func connectivityPrompt(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	args := req.Params.Arguments
	essid := strings.TrimSpace(args["essid"])
	target := defaultPromptArg(args["target"], "default")
	pingHost := defaultPromptArg(args["ping_host"], "1.1.1.1")
	dnsName := defaultPromptArg(args["dns_name"], "example.com")
	httpURL := defaultPromptArg(args["http_url"], "http://connectivitycheck.gstatic.com/generate_204")
	return promptText("Run a dropcheck Wi-Fi connectivity validation.",
		fmt.Sprintf(`Use the Dropcheck MCP tools to validate ESSID %q on target %q.

Start or inspect the session first, list agents if the target is ambiguous, then run dropcheck_run with passphrase_env when credentials are needed. Use these defaults unless the user overrides them:
- ping_host: %s
- dns_name: %s
- http_url: %s

Report the failed_step when dropcheck_run fails, and include the key structured fields from wifi.status, ip.status, ping, dns, and http results. Do not echo passphrases.`, essid, target, pingHost, dnsName, httpURL))
}

func mloInvestigationPrompt(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	args := req.Params.Arguments
	target := defaultPromptArg(args["target"], "default")
	essid := strings.TrimSpace(args["essid"])
	focus := "the current connection"
	if essid != "" {
		focus = fmt.Sprintf("ESSID %q", essid)
	}
	return promptText("Investigate Dropcheck Wi-Fi MLO state.",
		fmt.Sprintf(`Use Dropcheck MCP tools to investigate MLO for %s on target %q.

Recommended sequence:
1. dropcheck_wifi_mlo with fresh=true and timeout_ms=10000.
2. dropcheck_wifi_scan with fresh=true, band="all", timeout_ms=10000.
3. dropcheck_wifi_scan_detail for the ESSID or BSSID when a focus target is known.
4. dropcheck_adb_diagnostics with kind="cmd-wifi-status"; use kind="dumpsys-wifi" only when raw framework state is needed.

Summarize associated and affiliated links, AP MLD address, link IDs, bands, RSSI, EHT/HE capability evidence, and any disagreement between agent diagnostics and adb diagnostics.`, focus, target))
}

func nocSmokePrompt(_ context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	target := defaultPromptArg(req.Params.Arguments["target"], "default")
	return promptText("Run a Dropcheck NOC smoke check.",
		fmt.Sprintf(`Use Dropcheck MCP tools to run a read-only smoke check for target %q.

Recommended sequence:
1. dropcheck_agents.
2. dropcheck_wifi_status.
3. dropcheck_ip_status.
4. dropcheck_ping with host="1.1.1.1" and count=3.
5. dropcheck_dns with name="example.com" and type="A".
6. dropcheck_http with url="http://connectivitycheck.gstatic.com/generate_204" and expected_status=204.

Stop on hard tool errors. For failed agent statuses, report status, message, elapsed_ms, and the smallest result fields that explain the failure.`, target))
}

func promptText(description string, text string) (*mcp.GetPromptResult, error) {
	return &mcp.GetPromptResult{
		Description: description,
		Messages: []*mcp.PromptMessage{{
			Role:    "user",
			Content: &mcp.TextContent{Text: text},
		}},
	}, nil
}

func defaultPromptArg(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}
