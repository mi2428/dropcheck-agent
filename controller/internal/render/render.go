package render

import (
	"encoding/json"
	"fmt"
	"strings"
	"text/tabwriter"
	"time"

	"dropcheck/controller/internal/command"
	"dropcheck/controller/internal/control"
	"dropcheck/controller/internal/controlpb"
	"dropcheck/controller/internal/pipeline"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func renderProtoMessage(message proto.Message) (string, error) {
	data, err := protojson.MarshalOptions{
		Multiline:     true,
		Indent:        "  ",
		UseProtoNames: true,
	}.Marshal(message)
	if err != nil {
		return "", err
	}
	return string(data) + "\n", nil
}

// CommandResult renders one agent command result.
//
// Text output is tailored per payload type and includes command latency. JSON
// output is the protojson representation of result without an outer agent
// envelope.
func CommandResult(agent string, result *controlpb.CommandResult, options command.Options, format pipeline.Format) (string, error) {
	if format == pipeline.FormatJSON {
		return renderProtoMessage(result)
	}
	var b strings.Builder
	if result.GetStatus() != controlpb.CommandResult_STATUS_OK {
		fmt.Fprintf(&b, "Status: %s", resultStatus(result.GetStatus()))
		if result.GetMessage() != "" {
			fmt.Fprintf(&b, "  Message: %s", result.GetMessage())
		}
		b.WriteByte('\n')
	}
	fmt.Fprintf(&b, "Latency: %dms\n", commandResultLatencyMs(result))
	switch payload := result.Payload.(type) {
	case *controlpb.CommandResult_WifiStatus:
		renderWifiStatus(&b, payload.WifiStatus)
	case *controlpb.CommandResult_ConnectWifi:
		renderConnectWifi(&b, payload.ConnectWifi)
	case *controlpb.CommandResult_IpStatus:
		renderIPStatus(&b, payload.IpStatus)
	case *controlpb.CommandResult_Ping:
		renderPing(&b, payload.Ping, result.GetStatus())
	case *controlpb.CommandResult_Traceroute:
		renderTraceroute(&b, payload.Traceroute, options, result.GetStatus())
	case *controlpb.CommandResult_PathMtu:
		renderPathMtu(&b, payload.PathMtu)
	case *controlpb.CommandResult_GlobalIp:
		renderGlobalIp(&b, payload.GlobalIp)
	case *controlpb.CommandResult_ResolveDns:
		renderResolveDNS(&b, payload.ResolveDns)
	case *controlpb.CommandResult_HttpCheck:
		renderHTTPCheck(&b, payload.HttpCheck)
	case *controlpb.CommandResult_Wget:
		renderWget(&b, payload.Wget)
	case *controlpb.CommandResult_WifiDiagnostics:
		renderWifiDiagnostics(&b, payload.WifiDiagnostics)
	case *controlpb.CommandResult_WifiScan:
		renderWifiScan(&b, payload.WifiScan)
	case *controlpb.CommandResult_WifiCapabilities:
		renderWifiCapabilities(&b, payload.WifiCapabilities)
	case *controlpb.CommandResult_WifiOperation:
		renderWifiOperation(&b, payload.WifiOperation)
	case *controlpb.CommandResult_WifiAssert:
		renderWifiAssert(&b, payload.WifiAssert)
	case *controlpb.CommandResult_WifiWatch:
		renderWifiWatch(&b, payload.WifiWatch)
	case *controlpb.CommandResult_WifiMonitor:
		renderWifiMonitor(&b, payload.WifiMonitor)
	case *controlpb.CommandResult_WifiScanDetail:
		renderWifiScanDetail(&b, payload.WifiScanDetail)
	case *controlpb.CommandResult_WifiCycle:
		renderWifiCycle(&b, payload.WifiCycle)
	default:
		if result.GetMessage() != "" {
			fmt.Fprintf(&b, "%s\n", result.GetMessage())
		}
		if b.Len() == 0 {
			fmt.Fprintf(&b, "agent=%s status=%s payload=%T\n", agent, resultStatus(result.GetStatus()), result.Payload)
		}
	}
	return b.String(), nil
}

func commandResultLatencyMs(result *controlpb.CommandResult) int64 {
	switch payload := result.Payload.(type) {
	case *controlpb.CommandResult_Ping:
		return payload.Ping.GetElapsedMs()
	case *controlpb.CommandResult_Traceroute:
		return payload.Traceroute.GetElapsedMs()
	case *controlpb.CommandResult_PathMtu:
		return payload.PathMtu.GetElapsedMs()
	case *controlpb.CommandResult_GlobalIp:
		return payload.GlobalIp.GetElapsedMs()
	case *controlpb.CommandResult_ResolveDns:
		return payload.ResolveDns.GetElapsedMs()
	case *controlpb.CommandResult_HttpCheck:
		return payload.HttpCheck.GetElapsedMs()
	case *controlpb.CommandResult_Wget:
		return payload.Wget.GetElapsedMs()
	case *controlpb.CommandResult_WifiAssert:
		return payload.WifiAssert.GetElapsedMs()
	default:
		return result.GetElapsedMs()
	}
}

// CommandResultEnvelope renders a JSON object that includes agent and
// command_id metadata around a command result.
func CommandResultEnvelope(agent string, commandID string, result *controlpb.CommandResult) (string, error) {
	data, err := protojson.MarshalOptions{
		Multiline:     true,
		Indent:        "  ",
		UseProtoNames: true,
	}.Marshal(result)
	if err != nil {
		return "", err
	}
	envelope := struct {
		Agent     string          `json:"agent"`
		CommandID string          `json:"command_id"`
		Result    json.RawMessage `json:"result"`
	}{
		Agent:     agent,
		CommandID: commandID,
		Result:    json.RawMessage(data),
	}
	wrapped, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return "", err
	}
	return string(wrapped) + "\n", nil
}

// CommandError renders a command dispatch or execution error.
//
// includeAgent controls whether text and JSON output include the agent label;
// callers use this when a single command is broadcast to multiple agents.
func CommandError(agent string, commandID string, err error, format pipeline.Format, includeAgent bool) (string, error) {
	if format == pipeline.FormatJSON {
		value := map[string]any{
			"command_id": commandID,
			"error":      err.Error(),
		}
		if includeAgent {
			value["agent"] = agent
		}
		data, marshalErr := json.MarshalIndent(value, "", "  ")
		if marshalErr != nil {
			return "", marshalErr
		}
		return string(data) + "\n", nil
	}
	if includeAgent {
		return fmt.Sprintf("Agent: %s\nError: %s\n", agent, err.Error()), nil
	}
	return fmt.Sprintf("Error: %s\n", err.Error()), nil
}

// AgentListView is the renderer input for the connected-agent list.
type AgentListView struct {
	// Agents are shown in the order provided by the caller.
	Agents []control.AgentInfo
	// Selected is the selected agent ID when TargetAll is false.
	Selected string
	// TargetAll marks every row as selected for broadcast execution.
	TargetAll bool
}

// TargetView is the renderer input for the current command target.
type TargetView struct {
	// TargetAll indicates that commands will be broadcast to all agents.
	TargetAll bool
	// Selected is the selected agent ID, even if the agent has disconnected.
	Selected string
	// SelectedLabel is the cached human-readable label for Selected.
	SelectedLabel string
	// Agent is the connected agent that matches Selected, when available.
	Agent *control.AgentInfo
}

// Agents renders the connected-agent list.
func Agents(view AgentListView, format pipeline.Format) (string, error) {
	if format == pipeline.FormatJSON {
		agents := view.Agents
		rows := make([]map[string]any, 0, len(agents))
		for i, info := range agents {
			hello := info.Hello
			device := hello.GetDevice()
			rows = append(rows, map[string]any{
				"number":       i + 1,
				"selected":     info.ID == view.Selected && !view.TargetAll,
				"id":           info.ID,
				"adb_serial":   hello.GetAdbSerial(),
				"session":      info.SessionID,
				"app":          hello.GetAppVersion(),
				"manufacturer": device.GetManufacturer(),
				"model":        device.GetModel(),
				"sdk":          device.GetSdk(),
				"connected":    info.Connected.Format(time.RFC3339),
			})
		}
		data, err := json.MarshalIndent(rows, "", "  ")
		if err != nil {
			return "", err
		}
		return string(data) + "\n", nil
	}
	agents := view.Agents
	if len(agents) == 0 {
		return "No agents connected\n", nil
	}
	var b strings.Builder
	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "SEL\t#\tAGENT\tADB SERIAL\tDEVICE\tSDK\tAPP\tCONNECTED")
	for i, info := range agents {
		marker := ""
		if view.TargetAll {
			marker = "all"
		} else if info.ID == view.Selected {
			marker = "*"
		}
		hello := info.Hello
		device := hello.GetDevice()
		deviceName := strings.TrimSpace(device.GetManufacturer() + " " + device.GetModel())
		fmt.Fprintf(tw, "%s\t%d\t%s\t%s\t%s\t%d\t%s\t%s\n",
			marker,
			i+1,
			shortID(info.ID),
			empty(hello.GetAdbSerial(), "unknown"),
			empty(deviceName, "unknown"),
			device.GetSdk(),
			empty(hello.GetAppVersion(), "unknown"),
			info.Connected.Format(time.RFC3339),
		)
	}
	_ = tw.Flush()
	return b.String(), nil
}

// Target renders the active target selection.
func Target(view TargetView, format pipeline.Format) (string, error) {
	if format == pipeline.FormatJSON {
		target := map[string]any{"all": view.TargetAll, "selected": view.Selected, "label": view.SelectedLabel}
		if view.Agent != nil {
			info := *view.Agent
			target["agent"] = agentDisplayName(info)
			target["id"] = info.ID
			target["adb_serial"] = info.Hello.GetAdbSerial()
			target["connected"] = true
		} else if view.Selected != "" {
			target["connected"] = false
		}
		data, err := json.MarshalIndent(target, "", "  ")
		if err != nil {
			return "", err
		}
		return string(data) + "\n", nil
	}
	if view.TargetAll {
		return "Target: all agents\n", nil
	}
	if view.Agent != nil {
		info := *view.Agent
		return fmt.Sprintf("Target: %s (id=%s adb_serial=%s)\n", agentDisplayName(info), info.ID, empty(info.Hello.GetAdbSerial(), "unknown")), nil
	}
	if view.SelectedLabel != "" {
		return fmt.Sprintf("Target: %s (disconnected)\n", view.SelectedLabel), nil
	}
	return "Target: none\n", nil
}

func renderWifiStatus(b *strings.Builder, status *controlpb.WifiStatus) {
	if status == nil {
		return
	}
	fmt.Fprintf(b, "Wi-Fi: enabled=%t state=%s active=%s networks=%d\n",
		status.GetEnabled(),
		empty(status.GetState(), "unknown"),
		empty(status.GetActiveNetwork(), "none"),
		status.GetWifiNetworkCount(),
	)
	if len(status.GetPermissions()) > 0 {
		fmt.Fprintf(b, "Permissions: %s\n", strings.Join(status.GetPermissions(), ", "))
	}
	if conn := status.GetConnection(); conn != nil && conn.GetSsid() != "" {
		renderWifiConnection(b, conn)
	}
	if status.GetIpStatus() != nil {
		renderIPStatus(b, status.GetIpStatus())
	}
}

func renderWifiConnection(b *strings.Builder, conn *controlpb.WifiConnection) {
	fmt.Fprintf(b, "Connection: ssid=%s bssid=%s rssi=%ddBm security=%s band=%s freq=%dMHz link=%dMbps ip=%s\n",
		conn.GetSsid(),
		empty(conn.GetBssid(), "unknown"),
		conn.GetRssiDbm(),
		empty(conn.GetSecurityType(), "unknown"),
		wifiBandFromFrequency(conn.GetFrequencyMhz()),
		conn.GetFrequencyMhz(),
		conn.GetLinkSpeedMbps(),
		empty(conn.GetIpv4Address(), "none"),
	)
	if conn.GetWifiStandard() != "" || conn.GetSupplicantState() != "" || conn.GetDetailedState() != "" {
		fmt.Fprintf(b, "State: supplicant=%s detailed=%s standard=%s signal=%d/%d\n",
			empty(conn.GetSupplicantState(), "unknown"),
			empty(conn.GetDetailedState(), "unknown"),
			empty(conn.GetWifiStandard(), "unknown"),
			conn.GetSignalLevel(),
			conn.GetMaxSignalLevel(),
		)
	}
}

func renderConnectWifi(b *strings.Builder, result *controlpb.ConnectWifiResult) {
	if result == nil {
		return
	}
	fmt.Fprintf(b, "Connect: ssid=%s connected=%t message=%s\n", result.GetSsid(), result.GetConnected(), empty(result.GetMessage(), "-"))
	if result.GetIpStatus() != nil {
		renderIPStatus(b, result.GetIpStatus())
	}
}

func renderIPStatus(b *strings.Builder, status *controlpb.IpStatus) {
	if status == nil {
		return
	}
	fmt.Fprintf(b, "Network: id=%s transports=%s validated=%t internet=%t interface=%s mtu=%d\n",
		empty(status.GetNetworkId(), "unknown"),
		strings.Join(status.GetTransports(), ","),
		status.GetValidated(),
		status.GetInternet(),
		empty(status.GetInterfaceName(), "none"),
		status.GetMtu(),
	)
	if len(status.GetAddresses()) > 0 {
		fmt.Fprintf(b, "Addresses: %s\n", strings.Join(status.GetAddresses(), ", "))
	}
	if len(status.GetDnsServers()) > 0 {
		fmt.Fprintf(b, "DNS: %s\n", strings.Join(status.GetDnsServers(), ", "))
	}
	if status.GetDhcpServer() != "" {
		fmt.Fprintf(b, "DHCP server: %s\n", status.GetDhcpServer())
	}
	if status.GetPrivateDnsActive() || status.GetPrivateDnsServerName() != "" {
		fmt.Fprintf(b, "Private DNS: active=%t server=%s\n", status.GetPrivateDnsActive(), empty(status.GetPrivateDnsServerName(), "none"))
	}
	if status.GetWifi() != nil && status.GetWifi().GetSsid() != "" {
		renderWifiConnection(b, status.GetWifi())
	}
}

func renderPing(b *strings.Builder, result *controlpb.PingResult, status controlpb.CommandResult_Status) {
	analysis := analyzePing(result, status)
	fmt.Fprintf(b, "Ping: host=%s status=%s transmitted=%d received=%d loss=%.1f%% min/avg/max=%.2f/%.2f/%.2fms interface=%s elapsed=%dms\n",
		analysis.Host,
		analysis.Status,
		analysis.Transmitted,
		analysis.Received,
		analysis.PacketLossPercent,
		analysis.MinMs,
		analysis.AvgMs,
		analysis.MaxMs,
		empty(analysis.InterfaceName, "default"),
		analysis.ElapsedMs,
	)
	if result.GetOutput() != "" {
		fmt.Fprintf(b, "\n%s\n", strings.TrimRight(result.GetOutput(), "\n"))
	}
}

func renderTraceroute(b *strings.Builder, result *controlpb.TracerouteResult, options command.Options, status controlpb.CommandResult_Status) {
	analysis := analyzeTraceroute(result, options.TracerouteRequiredHops, status)
	fmt.Fprintf(b, "Traceroute: host=%s status=%s hops=%d reached=%t interface=%s elapsed=%dms executable=%s\n",
		analysis.Host,
		analysis.Status,
		len(analysis.Hops),
		analysis.ReachedTarget,
		empty(analysis.InterfaceName, "default"),
		analysis.ElapsedMs,
		empty(analysis.Executable, "unknown"),
	)
	if len(analysis.RequiredHops) > 0 {
		fmt.Fprintf(b, "Required hops: passed=%t matched=%s missing=%s\n",
			analysis.RequiredHopsPassed,
			strings.Join(analysis.MatchedRequiredHops, ","),
			strings.Join(analysis.MissingRequiredHops, ","),
		)
	}
	if len(analysis.Hops) > 0 {
		tw := tabwriter.NewWriter(b, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "HOP\tHOST\tADDRESS\tRTT\tTIMEOUT\tTARGET")
		for _, hop := range analysis.Hops {
			fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%t\t%t\n",
				hop.Index,
				empty(hop.Host, "-"),
				empty(hop.Address, "-"),
				formatMillis(hop.RttMs),
				hop.TimedOut,
				hop.ReachedTarget,
			)
		}
		_ = tw.Flush()
	}
	if analysis.Error != "" {
		fmt.Fprintf(b, "Error: %s\n", analysis.Error)
	}
}

func renderPathMtu(b *strings.Builder, result *controlpb.PathMtuResult) {
	if result == nil {
		return
	}
	fmt.Fprintf(b, "Path MTU: host=%s discovered=%t mtu=%d payload=%d range=%d-%d overhead=%d interface=%s probes=%d elapsed=%dms\n",
		result.GetHost(),
		result.GetDiscovered(),
		result.GetPathMtuBytes(),
		result.GetPayloadSizeBytes(),
		result.GetMinMtuBytes(),
		result.GetMaxMtuBytes(),
		result.GetIpOverheadBytes(),
		empty(result.GetInterfaceName(), "default"),
		len(result.GetProbes()),
		result.GetElapsedMs(),
	)
	if len(result.GetProbes()) > 0 {
		tw := tabwriter.NewWriter(b, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "MTU\tPAYLOAD\tPASS\tEXIT\tELAPSED")
		for _, probe := range result.GetProbes() {
			fmt.Fprintf(tw, "%d\t%d\t%t\t%d\t%dms\n",
				probe.GetMtuBytes(),
				probe.GetPayloadSizeBytes(),
				probe.GetPassed(),
				probe.GetExitCode(),
				probe.GetElapsedMs(),
			)
		}
		_ = tw.Flush()
	}
	if result.GetError() != "" {
		fmt.Fprintf(b, "Error: %s\n", result.GetError())
	}
}

func renderGlobalIp(b *strings.Builder, result *controlpb.GlobalIpResult) {
	if result == nil {
		return
	}
	fmt.Fprintf(b, "Global IP: service=%s requested=%s interface=%s checks=%d elapsed=%dms\n",
		empty(result.GetService(), "ifconfig.me"),
		ipFamilyName(result.GetRequestedFamily()),
		empty(result.GetInterfaceName(), "default"),
		len(result.GetAddresses()),
		result.GetElapsedMs(),
	)
	if len(result.GetAddresses()) > 0 {
		tw := tabwriter.NewWriter(b, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "FAMILY\tIP\tGLOBAL\tSTATUS\tELAPSED\tERROR")
		for _, address := range result.GetAddresses() {
			fmt.Fprintf(tw, "%s\t%s\t%t\t%d\t%dms\t%s\n",
				ipFamilyName(address.GetFamily()),
				empty(address.GetIp(), "-"),
				address.GetGlobal(),
				address.GetStatus(),
				address.GetElapsedMs(),
				empty(address.GetError(), "-"),
			)
		}
		_ = tw.Flush()
	}
	if result.GetError() != "" {
		fmt.Fprintf(b, "Error: %s\n", result.GetError())
	}
}

func renderResolveDNS(b *strings.Builder, result *controlpb.ResolveDnsResult) {
	if result == nil {
		return
	}
	if result.GetError() != "" {
		fmt.Fprintf(b, "DNS: name=%s error=%s elapsed=%dms\n", result.GetName(), result.GetError(), result.GetElapsedMs())
		return
	}
	fmt.Fprintf(b, "DNS: name=%s elapsed=%dms answers=%d\n", result.GetName(), result.GetElapsedMs(), len(result.GetAnswers()))
	for _, answer := range result.GetAnswers() {
		fmt.Fprintf(b, "  %s %s\n", dnsTypeName(answer.GetType()), answer.GetAddress())
	}
}

func renderHTTPCheck(b *strings.Builder, result *controlpb.HttpCheckResult) {
	if result == nil {
		return
	}
	if result.GetError() != "" {
		fmt.Fprintf(b, "HTTP: url=%s error=%s elapsed=%dms\n", result.GetUrl(), result.GetError(), result.GetElapsedMs())
		return
	}
	fmt.Fprintf(b, "HTTP: url=%s status=%d expected=%d matched=%t elapsed=%dms\n",
		result.GetUrl(),
		result.GetStatus(),
		result.GetExpectedStatus(),
		result.GetMatched(),
		result.GetElapsedMs(),
	)
}

func renderWget(b *strings.Builder, result *controlpb.WgetResult) {
	if result == nil {
		return
	}
	if result.GetError() != "" {
		fmt.Fprintf(b, "Download: url=%s error=%s elapsed=%dms\n", result.GetUrl(), result.GetError(), result.GetElapsedMs())
		return
	}
	fmt.Fprintf(b, "Download: url=%s status=%d bytes=%d content_length=%d type=%s throughput=%.0fbps interface=%s elapsed=%dms\n",
		result.GetUrl(),
		result.GetStatus(),
		result.GetBytesRead(),
		result.GetContentLength(),
		empty(result.GetContentType(), "unknown"),
		result.GetThroughputBps(),
		empty(result.GetInterfaceName(), "default"),
		result.GetElapsedMs(),
	)
}

func renderWifiDiagnostics(b *strings.Builder, diagnostics *controlpb.WifiDiagnostics) {
	if diagnostics == nil {
		return
	}
	renderWifiStatus(b, diagnostics.GetStatus())
	if diagnostics.GetCapabilities() != nil {
		b.WriteByte('\n')
		renderWifiCapabilities(b, diagnostics.GetCapabilities())
	}
	if len(diagnostics.GetNetworks()) > 0 {
		b.WriteString("\nNetworks:\n")
		tw := tabwriter.NewWriter(b, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "ID\tACTIVE\tINTERFACE\tVALIDATED\tTRANSPORTS")
		for _, network := range diagnostics.GetNetworks() {
			ip := network.GetIpStatus()
			fmt.Fprintf(tw, "%s\t%t\t%s\t%t\t%s\n",
				empty(network.GetNetworkId(), "unknown"),
				network.GetActive(),
				empty(ip.GetInterfaceName(), "none"),
				ip.GetValidated(),
				strings.Join(ip.GetTransports(), ","),
			)
		}
		_ = tw.Flush()
	}
	if diagnostics.GetScan() != nil {
		b.WriteByte('\n')
		renderWifiScan(b, diagnostics.GetScan())
	}
}

func renderWifiScan(b *strings.Builder, scan *controlpb.WifiScan) {
	if scan == nil {
		return
	}
	renderDiagnosticFields(b, scan.GetFields())
	renderScanResults(b, scan.GetResults())
	renderErrors(b, scan.GetErrors())
}

func renderWifiScanDetail(b *strings.Builder, detail *controlpb.WifiScanDetail) {
	if detail == nil {
		return
	}
	fmt.Fprintf(b, "Scan detail: target=%s results=%d\n", detail.GetTarget(), len(detail.GetResults()))
	renderDiagnosticFields(b, detail.GetFields())
	renderScanResults(b, detail.GetResults())
	renderErrors(b, detail.GetErrors())
}

func renderScanResults(b *strings.Builder, results []*controlpb.WifiScanResult) {
	if len(results) == 0 {
		b.WriteString("Scan: no results\n")
		return
	}
	tw := tabwriter.NewWriter(b, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "SSID\tBSSID\tRSSI\tBAND\tFREQ\tSTANDARD\tSECURITY")
	for _, result := range results {
		fmt.Fprintf(tw, "%s\t%s\t%d\t%s\t%d\t%s\t%s\n",
			empty(result.GetSsid(), "<hidden>"),
			empty(result.GetBssid(), "unknown"),
			result.GetRssiDbm(),
			empty(result.GetBand(), wifiBandFromFrequency(result.GetFrequencyMhz())),
			result.GetFrequencyMhz(),
			empty(result.GetWifiStandard(), "-"),
			empty(strings.Join(result.GetSecurityTypes(), ","), empty(result.GetCapabilities(), "-")),
		)
	}
	_ = tw.Flush()
}

func renderWifiCapabilities(b *strings.Builder, capabilities *controlpb.WifiCapabilities) {
	if capabilities == nil {
		return
	}
	renderDiagnosticFields(b, capabilities.GetFields())
	writeList(b, "Supported bands", capabilities.GetSupportedBands())
	writeList(b, "Unsupported bands", capabilities.GetUnsupportedBands())
	writeList(b, "Supported standards", capabilities.GetSupportedStandards())
	writeList(b, "Supported security", capabilities.GetSupportedSecurityModes())
	writeList(b, "Supported features", capabilities.GetSupportedFeatures())
	renderErrors(b, capabilities.GetErrors())
}

func renderWifiOperation(b *strings.Builder, result *controlpb.WifiOperationResult) {
	if result == nil {
		return
	}
	fmt.Fprintf(b, "Wi-Fi operation: operation=%s ok=%t message=%s\n", empty(result.GetOperation(), "unknown"), result.GetOk(), empty(result.GetMessage(), "-"))
	renderDiagnosticFields(b, result.GetFields())
	renderErrors(b, result.GetErrors())
	if result.GetStatus() != nil {
		renderWifiStatus(b, result.GetStatus())
	}
}

func renderWifiAssert(b *strings.Builder, result *controlpb.WifiAssertResult) {
	if result == nil {
		return
	}
	fmt.Fprintf(b, "Wi-Fi assert: passed=%t checks=%d elapsed=%dms\n", result.GetPassed(), len(result.GetChecks()), result.GetElapsedMs())
	if len(result.GetChecks()) > 0 {
		tw := tabwriter.NewWriter(b, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "CHECK\tPASSED\tEXPECTED\tACTUAL\tMESSAGE")
		for _, check := range result.GetChecks() {
			fmt.Fprintf(tw, "%s\t%t\t%s\t%s\t%s\n",
				check.GetKey(),
				check.GetPassed(),
				empty(check.GetExpected(), "-"),
				empty(check.GetActual(), "-"),
				empty(check.GetMessage(), "-"),
			)
		}
		_ = tw.Flush()
	}
	renderErrors(b, result.GetErrors())
	if result.GetStatus() != nil {
		renderWifiStatus(b, result.GetStatus())
	}
}

func renderWifiWatch(b *strings.Builder, result *controlpb.WifiWatchResult) {
	if result == nil {
		return
	}
	fmt.Fprintf(b, "Wi-Fi watch: samples=%d errors=%d\n", len(result.GetSamples()), len(result.GetErrors()))
	for _, sample := range result.GetSamples() {
		fmt.Fprintf(b, "%s ", unixMillis(sample.GetUnixTimeMs()))
		if sample.GetStatus() != nil {
			conn := sample.GetStatus().GetConnection()
			fmt.Fprintf(b, "state=%s ssid=%s rssi=%ddBm ip=%s\n",
				empty(sample.GetStatus().GetState(), "unknown"),
				empty(conn.GetSsid(), "none"),
				conn.GetRssiDbm(),
				empty(conn.GetIpv4Address(), "none"),
			)
		}
	}
	renderErrors(b, result.GetErrors())
}

func renderWifiMonitor(b *strings.Builder, result *controlpb.WifiMonitorResult) {
	if result == nil {
		return
	}
	fmt.Fprintf(b, "Wi-Fi monitor: events=%d errors=%d\n", len(result.GetEvents()), len(result.GetErrors()))
	for _, event := range result.GetEvents() {
		fmt.Fprintf(b, "%s %-12s %s\n", unixMillis(event.GetUnixTimeMs()), empty(event.GetType(), "event"), event.GetMessage())
	}
	renderErrors(b, result.GetErrors())
}

func renderWifiCycle(b *strings.Builder, result *controlpb.WifiCycleResult) {
	if result == nil {
		return
	}
	fmt.Fprintf(b, "Wi-Fi cycle: requested=%d completed=%d passed=%d errors=%d\n",
		result.GetRequestedCount(),
		result.GetCompletedCount(),
		result.GetPassedCount(),
		len(result.GetErrors()),
	)
	if len(result.GetSteps()) > 0 {
		tw := tabwriter.NewWriter(b, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "STEP\tCONNECTED\tPING\tHTTP\tELAPSED\tSSID\tERRORS")
		for _, step := range result.GetSteps() {
			ssid := ""
			if step.GetConnect() != nil {
				ssid = step.GetConnect().GetSsid()
			}
			fmt.Fprintf(tw, "%d\t%t\t%t\t%t\t%dms\t%s\t%s\n",
				step.GetIndex(),
				step.GetConnected(),
				step.GetPingOk(),
				step.GetHttpOk(),
				step.GetElapsedMs(),
				empty(ssid, "-"),
				strings.Join(step.GetErrors(), ","),
			)
		}
		_ = tw.Flush()
	}
	renderErrors(b, result.GetErrors())
}

func renderDiagnosticFields(b *strings.Builder, fields []*controlpb.DiagnosticField) {
	if len(fields) == 0 {
		return
	}
	tw := tabwriter.NewWriter(b, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "FIELD\tVALUE")
	for _, field := range fields {
		fmt.Fprintf(tw, "%s\t%s\n", field.GetKey(), field.GetValue())
	}
	_ = tw.Flush()
}

func renderErrors(b *strings.Builder, errors []string) {
	for _, err := range errors {
		fmt.Fprintf(b, "Error: %s\n", err)
	}
}

func writeList(b *strings.Builder, label string, values []string) {
	if len(values) == 0 {
		return
	}
	fmt.Fprintf(b, "%s: %s\n", label, strings.Join(values, ", "))
}

func wifiBandFromFrequency(freq int32) string {
	switch {
	case freq >= 2400 && freq < 2500:
		return "2.4ghz"
	case freq >= 4900 && freq < 5900:
		return "5ghz"
	case freq >= 5925 && freq < 7125:
		return "6ghz"
	case freq >= 57000 && freq < 71000:
		return "60ghz"
	default:
		return "unknown"
	}
}

func ipFamilyName(value controlpb.IpFamily) string {
	switch value {
	case controlpb.IpFamily_IP_FAMILY_IPV4:
		return "ipv4"
	case controlpb.IpFamily_IP_FAMILY_IPV6:
		return "ipv6"
	case controlpb.IpFamily_IP_FAMILY_ALL:
		return "all"
	default:
		return "unspecified"
	}
}

func unixMillis(ms int64) string {
	if ms == 0 {
		return "-"
	}
	return time.UnixMilli(ms).Format(time.RFC3339)
}
