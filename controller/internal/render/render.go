package render

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
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

// ConfigView is the persisted Agent App configuration rendered by show config.
type ConfigView struct {
	Standalone *controlpb.StandaloneConfig
}

// Config renders persisted Agent App configuration.
func Config(view ConfigView, format pipeline.Format) (string, error) {
	if format == pipeline.FormatJSON {
		return renderConfigJSON(view)
	}
	var b strings.Builder
	if view.Standalone != nil {
		renderStandaloneConfigBlock(&b, view.Standalone, 0)
	}
	return b.String(), nil
}

// ConfigEnvelope renders a multi-agent config result as a JSON object.
func ConfigEnvelope(agent string, view ConfigView) (string, error) {
	config, err := configJSONValue(view)
	if err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(map[string]any{
		"agent":  agent,
		"config": config,
	}, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data) + "\n", nil
}

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
	case *controlpb.CommandResult_WifiMonitor:
		renderWifiMonitor(&b, payload.WifiMonitor)
	case *controlpb.CommandResult_WifiScanDetail:
		renderWifiScanDetail(&b, payload.WifiScanDetail)
	case *controlpb.CommandResult_WifiCycle:
		renderWifiCycle(&b, payload.WifiCycle)
	case *controlpb.CommandResult_StandaloneConfig:
		renderStandaloneConfig(&b, payload.StandaloneConfig)
	case *controlpb.CommandResult_StandaloneStatus:
		renderStandaloneStatus(&b, payload.StandaloneStatus)
	case *controlpb.CommandResult_StandaloneRuns:
		renderStandaloneRuns(&b, payload.StandaloneRuns)
	case *controlpb.CommandResult_StandaloneRun:
		renderStandaloneRun(&b, payload.StandaloneRun)
	case *controlpb.CommandResult_StandaloneClear:
		renderStandaloneClear(&b, payload.StandaloneClear)
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
	_, _ = fmt.Fprintln(tw, "SEL\t#\tAGENT\tADB SERIAL\tDEVICE\tSDK\tAPP\tCONNECTED")
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
		_, _ = fmt.Fprintf(tw, "%s\t%d\t%s\t%s\t%s\t%d\t%s\t%s\n",
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
	fmt.Fprintf(b, "Connection: ssid=%s bssid=%s rssi=%ddBm security=%s band=%s channel=%s freq=%dMHz bandwidth=%s link=%dMbps ip=%s\n",
		conn.GetSsid(),
		empty(conn.GetBssid(), "unknown"),
		conn.GetRssiDbm(),
		empty(conn.GetSecurityType(), "unknown"),
		wifiBandFromFrequency(conn.GetFrequencyMhz()),
		wifiChannelFromFrequency(conn.GetFrequencyMhz()),
		conn.GetFrequencyMhz(),
		empty(wifiChannelWidth(conn), "unknown"),
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
		_, _ = fmt.Fprintln(tw, "HOP\tHOST\tADDRESS\tRTT\tTIMEOUT\tTARGET")
		for _, hop := range analysis.Hops {
			_, _ = fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%t\t%t\n",
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
		_, _ = fmt.Fprintln(tw, "MTU\tPAYLOAD\tPASS\tEXIT\tELAPSED")
		for _, probe := range result.GetProbes() {
			_, _ = fmt.Fprintf(tw, "%d\t%d\t%t\t%d\t%dms\n",
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
		_, _ = fmt.Fprintln(tw, "FAMILY\tIP\tGLOBAL\tSTATUS\tELAPSED\tERROR")
		for _, address := range result.GetAddresses() {
			_, _ = fmt.Fprintf(tw, "%s\t%s\t%t\t%d\t%dms\t%s\n",
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
		_, _ = fmt.Fprintln(tw, "ID\tACTIVE\tINTERFACE\tVALIDATED\tTRANSPORTS")
		for _, network := range diagnostics.GetNetworks() {
			ip := network.GetIpStatus()
			_, _ = fmt.Fprintf(tw, "%s\t%t\t%s\t%t\t%s\n",
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
	_, _ = fmt.Fprintln(tw, "SSID\tBSSID\tRSSI\tBAND\tFREQ\tSTANDARD\tSECURITY")
	for _, result := range results {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%d\t%s\t%d\t%s\t%s\n",
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
		_, _ = fmt.Fprintln(tw, "CHECK\tPASSED\tEXPECTED\tACTUAL\tMESSAGE")
		for _, check := range result.GetChecks() {
			_, _ = fmt.Fprintf(tw, "%s\t%t\t%s\t%s\t%s\n",
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
		_, _ = fmt.Fprintln(tw, "STEP\tCONNECTED\tPING\tHTTP\tELAPSED\tSSID\tERRORS")
		for _, step := range result.GetSteps() {
			ssid := ""
			if step.GetConnect() != nil {
				ssid = step.GetConnect().GetSsid()
			}
			_, _ = fmt.Fprintf(tw, "%d\t%t\t%t\t%t\t%dms\t%s\t%s\n",
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

func renderStandaloneConfig(b *strings.Builder, config *controlpb.StandaloneConfig) {
	if config == nil {
		return
	}
	renderStandaloneConfigBlock(b, config, 0)
}

func renderConfigJSON(view ConfigView) (string, error) {
	value, err := configJSONValue(view)
	if err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data) + "\n", nil
}

func configJSONValue(view ConfigView) (map[string]any, error) {
	value := make(map[string]any)
	if view.Standalone != nil {
		data, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(view.Standalone)
		if err != nil {
			return nil, err
		}
		value["standalone"] = json.RawMessage(data)
	}
	return value, nil
}

func renderStandaloneConfigBlock(b *strings.Builder, config *controlpb.StandaloneConfig, depth int) {
	if config == nil {
		return
	}
	writeConfigLine(b, depth, "standalone {")
	if config.GetEnabled() {
		writeConfigLine(b, depth+1, "enabled")
	} else {
		writeConfigLine(b, depth+1, "disabled")
	}
	if config.GetRetentionMs() != 0 {
		writeConfigLine(b, depth+1, "retention %s", formatConfigDuration(config.GetRetentionMs()))
	}
	if config.GetMaxBytes() != 0 {
		writeConfigLine(b, depth+1, "max-size %s", formatBytes(config.GetMaxBytes()))
	}
	for _, festa := range config.GetFestas() {
		writeConfigLine(b, depth+1, "festa %s {", shellQuote(festa.GetName()))
		if festa.GetEnabled() {
			writeConfigLine(b, depth+2, "enabled")
		} else {
			writeConfigLine(b, depth+2, "disabled")
		}
		if festa.GetIntervalMs() != 0 {
			writeConfigLine(b, depth+2, "interval %s", formatConfigDuration(festa.GetIntervalMs()))
		}
		for _, group := range festa.GetWifiGroups() {
			writeConfigLine(b, depth+2, "wifi-group %s {", shellQuote(group.GetName()))
			if group.GetEssid() != "" {
				writeConfigLine(b, depth+3, "match essid %s", shellQuote(group.GetEssid()))
			}
			if group.GetBssid() != "" {
				writeConfigLine(b, depth+3, "match bssid %s", shellQuote(group.GetBssid()))
			}
			if group.GetPassphrase() != "" {
				writeConfigLine(b, depth+3, "credential passphrase <redacted>")
			}
			if group.GetSecurity() != controlpb.ConnectWifi_SECURITY_UNSPECIFIED {
				writeConfigLine(b, depth+3, "security %s", standaloneSecurityName(group.GetSecurity()))
			}
			if group.GetBand() != controlpb.WifiBand_WIFI_BAND_UNSPECIFIED && group.GetBand() != controlpb.WifiBand_WIFI_BAND_ALL {
				writeConfigLine(b, depth+3, "band %s", standaloneBandName(group.GetBand()))
			}
			if group.GetRequireIp() {
				writeConfigLine(b, depth+3, "wait ip")
			}
			if group.GetRequireValidated() {
				writeConfigLine(b, depth+3, "wait validated")
			}
			if group.GetTimeoutMs() != 0 {
				writeConfigLine(b, depth+3, "timeout %s", formatConfigDuration(group.GetTimeoutMs()))
			}
			writeConfigLine(b, depth+2, "}")
		}
		checks := festa.GetChecks()
		if dns := checks.GetDns(); dns.GetEnabled() {
			writeConfigLine(b, depth+2, "check dns {")
			writeConfigLine(b, depth+3, "name %s", shellQuote(dns.GetName()))
			writeConfigLine(b, depth+3, "type %s", standaloneQTypesName(dns.GetQtypes()))
			if dns.GetTimeoutMs() != 0 {
				writeConfigLine(b, depth+3, "timeout %s", formatConfigDuration(dns.GetTimeoutMs()))
			}
			writeConfigLine(b, depth+2, "}")
		}
		if ping := checks.GetPing(); ping.GetEnabled() {
			writeConfigLine(b, depth+2, "check ping {")
			writeConfigLine(b, depth+3, "host %s", shellQuote(ping.GetHost()))
			if ping.GetCount() != 0 {
				writeConfigLine(b, depth+3, "count %d", ping.GetCount())
			}
			if ping.GetSizeBytes() != 0 {
				writeConfigLine(b, depth+3, "size %d", ping.GetSizeBytes())
			}
			if ping.GetTimeoutMs() != 0 {
				writeConfigLine(b, depth+3, "timeout %s", formatConfigDuration(ping.GetTimeoutMs()))
			}
			writeConfigLine(b, depth+2, "}")
		}
		if http := checks.GetHttp(); http.GetEnabled() {
			writeConfigLine(b, depth+2, "check http {")
			writeConfigLine(b, depth+3, "url %s", shellQuote(http.GetUrl()))
			if http.GetExpectedStatus() != 0 {
				writeConfigLine(b, depth+3, "expected-status %d", http.GetExpectedStatus())
			}
			if http.GetTimeoutMs() != 0 {
				writeConfigLine(b, depth+3, "timeout %s", formatConfigDuration(http.GetTimeoutMs()))
			}
			writeConfigLine(b, depth+2, "}")
		}
		writeConfigLine(b, depth+1, "}")
	}
	writeConfigLine(b, depth, "}")
}

func writeConfigLine(b *strings.Builder, depth int, format string, args ...any) {
	b.WriteString(strings.Repeat("  ", depth))
	fmt.Fprintf(b, format, args...)
	b.WriteByte('\n')
}

func formatConfigDuration(ms uint32) string {
	duration := time.Duration(ms) * time.Millisecond
	if duration%(24*time.Hour) == 0 {
		return fmt.Sprintf("%dd", duration/(24*time.Hour))
	}
	return duration.String()
}

func renderStandaloneStatus(b *strings.Builder, status *controlpb.StandaloneStatus) {
	if status == nil {
		return
	}
	fmt.Fprintf(b, "Standalone: enabled=%t running=%t stored=%d unsynced=%d bytes=%s\n",
		status.GetEnabled(),
		status.GetRunning(),
		status.GetStoredRuns(),
		status.GetUnsyncedRuns(),
		formatBytes(status.GetStoredBytes()),
	)
	if status.GetCurrentRunId() != "" {
		fmt.Fprintf(b, "Current run: %s\n", status.GetCurrentRunId())
	}
	if status.GetLastRunId() != "" {
		fmt.Fprintf(b, "Last run: %s started=%s finished=%s\n",
			status.GetLastRunId(),
			unixMillis(status.GetLastStartedUnixMs()),
			unixMillis(status.GetLastFinishedUnixMs()),
		)
	}
	if status.GetMessage() != "" {
		fmt.Fprintf(b, "Message: %s\n", status.GetMessage())
	}
}

func renderStandaloneRuns(b *strings.Builder, runs *controlpb.StandaloneRuns) {
	if runs == nil {
		return
	}
	fmt.Fprintf(b, "Standalone runs: returned=%d total=%d unsynced=%d\n",
		len(runs.GetRuns()),
		runs.GetTotalRuns(),
		runs.GetUnsyncedRuns(),
	)
	if len(runs.GetRuns()) == 0 {
		return
	}
	tw := tabwriter.NewWriter(b, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "RUN\tSTATUS\tSYNCED\tSTARTED\tFINISHED\tSTEPS\tFAILED\tFESTA")
	for _, run := range runs.GetRuns() {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%t\t%s\t%s\t%d\t%d\t%s\n",
			shortID(run.GetRunId()),
			empty(run.GetStatus(), "-"),
			run.GetSynced(),
			unixMillis(run.GetStartedUnixMs()),
			unixMillis(run.GetFinishedUnixMs()),
			run.GetStepCount(),
			run.GetFailedStepCount(),
			empty(run.GetFestaName(), "-"),
		)
	}
	_ = tw.Flush()
}

func renderStandaloneRun(b *strings.Builder, run *controlpb.StandaloneRunArchive) {
	if run == nil {
		return
	}
	summary := run.GetSummary()
	fmt.Fprintf(b, "Standalone run: id=%s status=%s synced=%t festa=%s steps=%d failed=%d\n",
		summary.GetRunId(),
		empty(summary.GetStatus(), "-"),
		summary.GetSynced(),
		empty(summary.GetFestaName(), "-"),
		summary.GetStepCount(),
		summary.GetFailedStepCount(),
	)
	if len(run.GetSteps()) == 0 {
		return
	}
	tw := tabwriter.NewWriter(b, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "WIFI-GROUP\tSTEP\tATTEMPT\tSTATUS\tELAPSED\tERROR")
	for _, step := range run.GetSteps() {
		status := "-"
		elapsed := int64(0)
		if step.GetResult() != nil {
			status = resultStatus(step.GetResult().GetStatus())
			elapsed = commandResultLatencyMs(step.GetResult())
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%d\t%s\t%dms\t%s\n",
			empty(step.GetWifiGroupName(), "-"),
			empty(step.GetStepName(), "-"),
			step.GetAttempt(),
			status,
			elapsed,
			step.GetError(),
		)
	}
	_ = tw.Flush()
}

func renderStandaloneClear(b *strings.Builder, result *controlpb.StandaloneClearResult) {
	if result == nil {
		return
	}
	fmt.Fprintf(b, "Standalone cleared: runs=%d bytes=%s\n", result.GetRemovedRuns(), formatBytes(result.GetRemovedBytes()))
}

func renderDiagnosticFields(b *strings.Builder, fields []*controlpb.DiagnosticField) {
	if len(fields) == 0 {
		return
	}
	tw := tabwriter.NewWriter(b, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "FIELD\tVALUE")
	for _, field := range fields {
		_, _ = fmt.Fprintf(tw, "%s\t%s\n", field.GetKey(), field.GetValue())
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

func wifiChannelFromFrequency(freq int32) string {
	channel := int32(0)
	switch {
	case freq == 2484:
		channel = 14
	case freq >= 2412 && freq <= 2472:
		channel = (freq - 2407) / 5
	case freq >= 5000 && freq <= 5895:
		channel = (freq - 5000) / 5
	case freq >= 5955 && freq <= 7115:
		channel = (freq - 5950) / 5
	}
	if channel == 0 {
		return "unknown"
	}
	return fmt.Sprint(channel)
}

var (
	wifiChannelWidthPattern = regexp.MustCompile(`(?i)(?:channel[_ ]?width|channelWidth)\s*[:=]\s*([A-Za-z0-9_./+-]+)`)
	wifiChannelWidthCore    = regexp.MustCompile(`^[0-9+]+$`)
)

func wifiChannelWidth(conn *controlpb.WifiConnection) string {
	if conn == nil {
		return ""
	}
	if width := formatWifiChannelWidth(conn.GetChannelWidth()); width != "" {
		return width
	}
	match := wifiChannelWidthPattern.FindStringSubmatch(conn.GetRaw())
	if match == nil {
		return ""
	}
	return formatWifiChannelWidth(match[1])
}

func formatWifiChannelWidth(value string) string {
	width := strings.Trim(strings.TrimSpace(value), ",;")
	if width == "" {
		return ""
	}
	normalized := strings.ToLower(width)
	normalized = strings.TrimPrefix(normalized, "channel_width_")
	normalized = strings.TrimPrefix(normalized, "width_")
	normalized = strings.ReplaceAll(normalized, "_", "")
	normalized = strings.ReplaceAll(normalized, " ", "")
	core := strings.TrimSuffix(normalized, "mhz")
	if wifiChannelWidthCore.MatchString(core) {
		return core + "MHz"
	}
	return width
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

func standaloneSecurityName(value controlpb.ConnectWifi_Security) string {
	switch value {
	case controlpb.ConnectWifi_SECURITY_WPA2_PSK:
		return "wpa2"
	case controlpb.ConnectWifi_SECURITY_WPA3_SAE:
		return "wpa3"
	case controlpb.ConnectWifi_SECURITY_WPA2_WPA3_TRANSITION:
		return "transition"
	default:
		return "auto"
	}
}

func standaloneBandName(value controlpb.WifiBand) string {
	switch value {
	case controlpb.WifiBand_WIFI_BAND_2_4_GHZ:
		return "2.4ghz"
	case controlpb.WifiBand_WIFI_BAND_5_GHZ:
		return "5ghz"
	case controlpb.WifiBand_WIFI_BAND_6_GHZ:
		return "6ghz"
	case controlpb.WifiBand_WIFI_BAND_60_GHZ:
		return "60ghz"
	default:
		return "all"
	}
}

func standaloneQTypesName(values []controlpb.DnsRecordType) string {
	hasA := false
	hasAAAA := false
	for _, value := range values {
		hasA = hasA || value == controlpb.DnsRecordType_DNS_RECORD_TYPE_A
		hasAAAA = hasAAAA || value == controlpb.DnsRecordType_DNS_RECORD_TYPE_AAAA
	}
	switch {
	case hasA && hasAAAA:
		return "ALL"
	case hasAAAA:
		return "AAAA"
	default:
		return "A"
	}
}

func shellQuote(value string) string {
	if value == "" {
		return "\"\""
	}
	if !strings.ContainsAny(value, " \t\n\"'\\") {
		return value
	}
	return strconv.Quote(value)
}

func unixMillis(ms int64) string {
	if ms == 0 {
		return "-"
	}
	return time.UnixMilli(ms).Format(time.RFC3339)
}

func formatBytes(value uint64) string {
	const unit = 1024
	if value < unit {
		return fmt.Sprintf("%dB", value)
	}
	div, exp := uint64(unit), 0
	for n := value / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(value)/float64(div), "KMGTPE"[exp])
}
