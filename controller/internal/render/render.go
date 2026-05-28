package render

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
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
	if format == pipeline.FormatSet {
		return renderConfigSet(view), nil
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
		renderGlobalIP(&b, payload.GlobalIp)
	case *controlpb.CommandResult_ResolveDns:
		renderResolveDNS(&b, payload.ResolveDns)
	case *controlpb.CommandResult_HttpCheck:
		renderHTTPCheck(&b, payload.HttpCheck)
	case *controlpb.CommandResult_Wget:
		renderWget(&b, payload.Wget)
	case *controlpb.CommandResult_WifiDiagnostics:
		if options.WifiRenderMode == command.WifiRenderModeMLO {
			renderWifiMLO(&b, payload.WifiDiagnostics, options)
		} else {
			renderWifiDiagnostics(&b, payload.WifiDiagnostics)
		}
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

type kvRow struct {
	label string
	value string
}

func kv(label string, value any) kvRow {
	return kvRow{label: label, value: fmt.Sprint(value)}
}

func writeSection(b *strings.Builder, title string) {
	if b.Len() > 0 {
		current := b.String()
		if !strings.HasSuffix(current, "\n") {
			b.WriteByte('\n')
		}
		if !strings.HasSuffix(current, "\n\n") {
			b.WriteByte('\n')
		}
	}
	b.WriteString(title)
	b.WriteByte('\n')
}

func writeBlankLine(b *strings.Builder) {
	if b.Len() == 0 {
		return
	}
	current := b.String()
	if strings.HasSuffix(current, "\n\n") {
		return
	}
	if !strings.HasSuffix(current, "\n") {
		b.WriteByte('\n')
	}
	b.WriteByte('\n')
}

func writeKVSection(b *strings.Builder, title string, rows ...kvRow) {
	writeSection(b, title)
	writeKVRows(b, rows...)
}

func writeKVRows(b *strings.Builder, rows ...kvRow) {
	tw := tabwriter.NewWriter(b, 0, 0, 2, ' ', 0)
	for _, row := range rows {
		if row.label == "" || row.value == "" {
			continue
		}
		if strings.Contains(row.value, "\n") {
			_, _ = fmt.Fprintf(tw, "  %s\n", row.label)
			for line := range strings.SplitSeq(row.value, "\n") {
				if line == "" {
					continue
				}
				_, _ = fmt.Fprintf(tw, "    %s\n", line)
			}
			continue
		}
		_, _ = fmt.Fprintf(tw, "  %s\t%s\n", row.label, row.value)
	}
	_ = tw.Flush()
}

func writeListSection(b *strings.Builder, title string, values []string) {
	if len(values) == 0 {
		return
	}
	writeSection(b, title)
	for _, value := range values {
		fmt.Fprintf(b, "  %s\n", value)
	}
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
	writeKVSection(b, "Wi-Fi",
		kv("enabled", status.GetEnabled()),
		kv("state", empty(status.GetState(), "unknown")),
		kv("active", empty(status.GetActiveNetwork(), "none")),
		kv("networks", status.GetWifiNetworkCount()),
		kv("permissions", wifiPermissionSummary(status.GetPermissions())),
	)
	var conn *controlpb.WifiConnection
	if status.GetConnection() != nil && status.GetConnection().GetSsid() != "" {
		conn = status.GetConnection()
		renderWifiConnection(b, conn)
	}
	if status.GetIpStatus() != nil {
		renderIPStatusWithOptions(b, status.GetIpStatus(), ipRenderOptions{connection: conn})
	} else if rows := wifiConnectionNetworkRows(conn); len(rows) > 0 {
		writeKVSection(b, "Network", rows...)
	}
}

func wifiPermissionSummary(permissions []string) string {
	if len(permissions) == 0 {
		return ""
	}
	granted := make([]string, 0, len(permissions))
	missing := make([]string, 0)
	for _, permission := range permissions {
		name, state, ok := strings.Cut(permission, "=")
		if !ok {
			missing = append(missing, permission)
			continue
		}
		if strings.EqualFold(state, "granted") {
			granted = append(granted, name)
		} else {
			missing = append(missing, name+"="+state)
		}
	}
	sort.Strings(granted)
	sort.Strings(missing)
	lines := make([]string, 0, 1+len(granted)+len(missing))
	if len(missing) == 0 {
		lines = append(lines, "all_granted")
		lines = append(lines, granted...)
		return multiLineValue(lines)
	}
	lines = append(lines, "missing")
	lines = append(lines, missing...)
	if len(granted) > 0 {
		lines = append(lines, "granted")
		lines = append(lines, granted...)
	}
	return multiLineValue(lines)
}

func renderWifiConnection(b *strings.Builder, conn *controlpb.WifiConnection) {
	writeKVSection(b, "Connection",
		kv("ssid", conn.GetSsid()),
		kv("bssid", empty(conn.GetBssid(), "unknown")),
		kv("security", empty(conn.GetSecurityType(), "unknown")),
		kv("standard", empty(conn.GetWifiStandard(), "unknown")),
		kv("rssi", fmt.Sprintf("%ddBm", conn.GetRssiDbm())),
		kv("signal", wifiSignalLevel(conn)),
		kv("band", wifiBandFromFrequency(conn.GetFrequencyMhz())),
		kv("channel", wifiChannelFromFrequency(conn.GetFrequencyMhz())),
		kv("frequency", fmt.Sprintf("%dMHz", conn.GetFrequencyMhz())),
		kv("bandwidth", empty(wifiChannelWidth(conn), "unknown")),
		kv("link", wifiLinkSpeed(conn)),
		kv("sta_mac", conn.GetMacAddress()),
		kv("supplicant", empty(conn.GetSupplicantState(), "unknown")),
		kv("detailed", empty(conn.GetDetailedState(), "unknown")),
	)
	renderWifiConnectionCapabilities(b, conn)
	renderWifiSecurityDetails(b, conn.GetSecurityDetails())
	renderWifiConnectionDetailedCapabilities(b, conn)
}

func wifiLinkSpeed(conn *controlpb.WifiConnection) string {
	parts := []string{fmt.Sprintf("%dMbps", conn.GetLinkSpeedMbps())}
	if conn.GetTxLinkSpeedMbps() > 0 {
		parts = append(parts, fmt.Sprintf("tx=%dMbps", conn.GetTxLinkSpeedMbps()))
	}
	if conn.GetRxLinkSpeedMbps() > 0 {
		parts = append(parts, fmt.Sprintf("rx=%dMbps", conn.GetRxLinkSpeedMbps()))
	}
	return strings.Join(parts, " ")
}

func wifiSignalLevel(conn *controlpb.WifiConnection) string {
	if conn.GetMaxSignalLevel() <= 0 && conn.GetSignalLevel() == 0 {
		return ""
	}
	return fmt.Sprintf("%d/%d", conn.GetSignalLevel(), conn.GetMaxSignalLevel())
}

func renderWifiConnectionCapabilities(b *strings.Builder, conn *controlpb.WifiConnection) {
	rows := wifiAPCapabilityRows(conn)
	if len(rows) == 0 {
		return
	}
	writeKVSection(b, "AP Capabilities", rows...)
}

func wifiAPCapabilityRows(conn *controlpb.WifiConnection) []kvRow {
	summary := newWifiAPCapabilitySummary(conn.GetInformationElements())
	rows := []kvRow{
		kv("roaming", multiLineValue(summary.roaming)),
		kv("security", multiLineValue(summary.security)),
		kv("phy", multiLineValue(summary.phy)),
		kv("operation", multiLineValue(summary.operation)),
		kv("radio", multiLineValue(summary.radio)),
		kv("qos", multiLineValue(summary.qos)),
		kv("network", multiLineValue(summary.network)),
		kv("rates", multiLineValue(summary.rates)),
	}
	if conn.GetHiddenSsid() {
		rows = append(rows, kv("hidden", true))
	}
	if conn.GetRestricted() {
		rows = append(rows, kv("restricted", true))
	}
	if conn.GetPasspointFqdn() != "" {
		rows = append(rows, kv("passpoint_fqdn", conn.GetPasspointFqdn()))
	}
	if conn.GetPasspointProviderFriendlyName() != "" {
		rows = append(rows, kv("passpoint_provider", conn.GetPasspointProviderFriendlyName()))
	}
	if conn.GetPasspointUniqueId() != "" {
		rows = append(rows, kv("passpoint_unique_id", conn.GetPasspointUniqueId()))
	}
	if maxLink := wifiMaxLink(conn); maxLink != "" {
		rows = append(rows, kv("max_link", maxLink))
	}
	if summary.vendorIE > 0 {
		rows = append(rows, kv("vendor_ie", summary.vendorIE))
	}
	if len(summary.other) > 0 {
		rows = append(rows, kv("other_ie", multiLineValue(summary.other)))
	}
	return nonEmptyKVRows(rows)
}

type wifiAPCapabilitySummary struct {
	roaming   []string
	security  []string
	phy       []string
	operation []string
	radio     []string
	qos       []string
	network   []string
	rates     []string
	other     []string
	vendorIE  int
}

func newWifiAPCapabilitySummary(elements []*controlpb.WifiInformationElement) wifiAPCapabilitySummary {
	type bucket int
	const (
		bucketRoaming bucket = iota
		bucketSecurity
		bucketPHY
		bucketOperation
		bucketRadio
		bucketQoS
		bucketNetwork
		bucketRates
		bucketOther
	)
	values := map[bucket]map[string]struct{}{}
	add := func(target bucket, name string) {
		if name == "" {
			return
		}
		if values[target] == nil {
			values[target] = map[string]struct{}{}
		}
		values[target][name] = struct{}{}
	}
	summary := wifiAPCapabilitySummary{}
	for _, element := range elements {
		switch element.GetId() {
		case 0:
			continue
		case 1:
			add(bucketRates, "supported_rates")
		case 7:
			add(bucketRadio, "country")
		case 11:
			add(bucketRadio, "bss_load")
		case 32:
			add(bucketRadio, "power_constraint")
		case 35:
			add(bucketRadio, "tpc_report")
		case 45:
			add(bucketPHY, "ht")
		case 48:
			add(bucketSecurity, "rsn")
		case 50:
			add(bucketRates, "extended_supported_rates")
		case 54:
			add(bucketRoaming, "11r")
		case 55:
			add(bucketRoaming, "fast_bss_transition")
		case 59:
			add(bucketRadio, "supported_operating_classes")
		case 61:
			add(bucketOperation, "ht_operation")
		case 70:
			add(bucketRoaming, "11k")
		case 107:
			add(bucketNetwork, "interworking")
		case 111:
			add(bucketNetwork, "roaming_consortium")
		case 127:
			if informationElementBit(element, 19) {
				add(bucketRoaming, "11v_bss_transition")
			}
			if informationElementBit(element, 84) {
				add(bucketSecurity, "beacon_protection")
			}
			if !informationElementBit(element, 19) && !informationElementBit(element, 84) {
				add(bucketOther, informationElementName(element))
			}
		case 191:
			add(bucketPHY, "vht")
		case 192:
			add(bucketOperation, "vht_operation")
		case 195:
			add(bucketRadio, "tx_power_envelope")
		case 201:
			add(bucketRoaming, "reduced_neighbor_report")
		case 221:
			summary.vendorIE++
		case 244:
			add(bucketSecurity, "rsn_extension")
		case 255:
			switch element.GetIdExt() {
			case 35:
				add(bucketPHY, "he")
			case 36:
				add(bucketOperation, "he_operation")
			case 37:
				add(bucketQoS, "uora")
			case 38:
				add(bucketQoS, "mu_edca")
			case 39:
				add(bucketQoS, "spatial_reuse")
			case 45:
				add(bucketRadio, "he_bss_load")
			case 59:
				add(bucketPHY, "he_6ghz")
			case 106:
				add(bucketOperation, "eht_operation")
			case 107:
				add(bucketOperation, "eht_multi_link")
			case 108:
				add(bucketPHY, "eht")
			default:
				add(bucketOther, informationElementExtensionName(element.GetIdExt()))
			}
		default:
			add(bucketOther, informationElementName(element))
		}
	}
	summary.roaming = sortedMapKeys(values[bucketRoaming])
	summary.security = sortedMapKeys(values[bucketSecurity])
	summary.phy = sortedMapKeys(values[bucketPHY])
	summary.operation = sortedMapKeys(values[bucketOperation])
	summary.radio = sortedMapKeys(values[bucketRadio])
	summary.qos = sortedMapKeys(values[bucketQoS])
	summary.network = sortedMapKeys(values[bucketNetwork])
	summary.rates = sortedMapKeys(values[bucketRates])
	summary.other = sortedMapKeys(values[bucketOther])
	return summary
}

func sortedMapKeys(values map[string]struct{}) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func nonEmptyKVRows(rows []kvRow) []kvRow {
	out := rows[:0]
	for _, row := range rows {
		if row.label == "" || row.value == "" {
			continue
		}
		out = append(out, row)
	}
	return out
}

func wifiMaxLink(conn *controlpb.WifiConnection) string {
	tx := conn.GetMaxSupportedTxLinkSpeedMbps()
	rx := conn.GetMaxSupportedRxLinkSpeedMbps()
	if tx <= 0 && rx <= 0 {
		return ""
	}
	return fmt.Sprintf("tx=%dMbps rx=%dMbps", tx, rx)
}

func wifiConnectionHasMLO(conn *controlpb.WifiConnection) bool {
	return conn.GetApMldMacAddress() != "" ||
		len(conn.GetAffiliatedMloLinks()) > 0 ||
		len(conn.GetAssociatedMloLinks()) > 0 ||
		wifiMLOHasElement(conn.GetInformationElements()) ||
		(strings.EqualFold(conn.GetWifiStandard(), "802.11be") && conn.GetApMloLinkId() >= 0)
}

func renderMLOLinks(b *strings.Builder, title string, links []*controlpb.MloLinkInfo) {
	if len(links) == 0 {
		return
	}
	writeSection(b, title)
	tw := tabwriter.NewWriter(b, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "ID\tSTATE\tBAND\tCHANNEL\tRSSI\tTX\tRX\tMAX_TX\tMAX_RX\tAP_MAC\tSTA_MAC")
	for _, link := range links {
		_, _ = fmt.Fprintf(tw, "%d\t%s\t%s\t%d\t%d\t%d\t%d\t%d\t%d\t%s\t%s\n",
			link.GetLinkId(),
			empty(link.GetState(), "unknown"),
			empty(link.GetBand(), "unknown"),
			link.GetChannel(),
			link.GetRssiDbm(),
			link.GetTxLinkSpeedMbps(),
			link.GetRxLinkSpeedMbps(),
			link.GetMaxSupportedTxLinkSpeedMbps(),
			link.GetMaxSupportedRxLinkSpeedMbps(),
			empty(link.GetApMacAddress(), "unknown"),
			empty(link.GetStaMacAddress(), "unknown"),
		)
	}
	_ = tw.Flush()
}

func mloLinkID(id int32) string {
	if id < 0 {
		return "<none>"
	}
	return strconv.Itoa(int(id))
}

func renderConnectWifi(b *strings.Builder, result *controlpb.ConnectWifiResult) {
	if result == nil {
		return
	}
	fmt.Fprintf(b, "Connect: ssid=%s connected=%t message=%s\n", result.GetSsid(), result.GetConnected(), empty(result.GetMessage(), "-"))
}

type ipRenderOptions struct {
	connection *controlpb.WifiConnection
}

func renderIPStatus(b *strings.Builder, status *controlpb.IpStatus) {
	renderIPStatusWithOptions(b, status, ipRenderOptions{})
}

func renderIPStatusWithOptions(b *strings.Builder, status *controlpb.IpStatus, options ipRenderOptions) {
	if status == nil {
		return
	}
	writeKVSection(b, "Network", networkRows(status, options)...)
}

func wifiConnectionNetworkRows(conn *controlpb.WifiConnection) []kvRow {
	if conn == nil {
		return nil
	}
	rows := []kvRow{
		kv("id", connectionNetworkID(conn)),
		kv("ipv4", conn.GetIpv4Address()),
	}
	return nonEmptyKVRows(rows)
}

func networkRows(status *controlpb.IpStatus, options ipRenderOptions) []kvRow {
	ipv4, ipv6, addresses := splitIPAddresses(status.GetAddresses())
	if ip := options.connection.GetIpv4Address(); ip != "" && !addressListContainsIP(ipv4, ip) {
		ipv4 = append(ipv4, ip)
	}
	capabilities := networkCapabilitiesForDetail(status.GetCapabilities())
	signalStrength, showSignalStrength := networkSignalStrengthForDetail(status, options.connection)
	rows := []kvRow{
		kv("id", empty(firstNonBlank(status.GetNetworkId(), connectionNetworkID(options.connection)), "unknown")),
		kv("transports", multiLineValue(status.GetTransports())),
		kv("interface", empty(status.GetInterfaceName(), "none")),
		kv("mtu", status.GetMtu()),
		kv("validated", status.GetValidated()),
		kv("internet", status.GetInternet()),
	}
	if len(capabilities) > 0 {
		rows = append(rows, kv("capabilities", multiLineValue(capabilities)))
	}
	if bandwidth := networkBandwidth(status); bandwidth != "" {
		rows = append(rows, kv("bandwidth", bandwidth))
	}
	if showSignalStrength {
		rows = append(rows, kv("signal_strength", signalStrength))
	}
	if status.GetNetworkSpecifier() != "" {
		rows = append(rows, kv("network_specifier", status.GetNetworkSpecifier()))
	}
	if status.GetOwnerUid() > 0 {
		rows = append(rows, kv("owner_uid", status.GetOwnerUid()))
	}
	if len(status.GetEnterpriseIds()) > 0 {
		rows = append(rows, kv("enterprise_ids", multiLineValue(status.GetEnterpriseIds())))
	}
	if len(status.GetSubscriptionIds()) > 0 {
		values := make([]string, 0, len(status.GetSubscriptionIds()))
		for _, id := range status.GetSubscriptionIds() {
			values = append(values, strconv.Itoa(int(id)))
		}
		rows = append(rows, kv("subscription_ids", multiLineValue(values)))
	}
	if status.GetDhcpServer() != "" {
		rows = append(rows, kv("dhcp_server", status.GetDhcpServer()))
	}
	if status.GetPrivateDnsActive() || status.GetPrivateDnsServerName() != "" {
		rows = append(rows, kv("private_dns", fmt.Sprintf("active=%t server=%s", status.GetPrivateDnsActive(), empty(status.GetPrivateDnsServerName(), "none"))))
	}
	if len(ipv4) > 0 {
		rows = append(rows, kv("ipv4", multiLineValue(ipv4)))
	}
	if len(ipv6) > 0 {
		rows = append(rows, kv("ipv6", multiLineValue(ipv6)))
	}
	if len(addresses) > 0 {
		rows = append(rows, kv("addresses", multiLineValue(addresses)))
	}
	if len(status.GetRoutes()) > 0 {
		rows = append(rows, kv("routes", multiLineValue(status.GetRoutes())))
	}
	if len(status.GetDnsServers()) > 0 {
		rows = append(rows, kv("dns", multiLineValue(status.GetDnsServers())))
	}
	if status.GetDomains() != "" {
		rows = append(rows, kv("domains", status.GetDomains()))
	}
	if status.GetHttpProxy() != "" {
		rows = append(rows, kv("http_proxy", status.GetHttpProxy()))
	}
	if status.GetNat64Prefix() != "" {
		rows = append(rows, kv("nat64_prefix", status.GetNat64Prefix()))
	}
	if status.GetWakeOnLanSupported() {
		rows = append(rows, kv("wake_on_lan", true))
	}
	return rows
}

func multiLineValue(values []string) string {
	nonEmpty := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		nonEmpty = append(nonEmpty, value)
	}
	return strings.Join(nonEmpty, "\n")
}

func connectionNetworkID(conn *controlpb.WifiConnection) string {
	if conn == nil || conn.GetNetworkId() == 0 {
		return ""
	}
	return strconv.Itoa(int(conn.GetNetworkId()))
}

func addressListContainsIP(values []string, ip string) bool {
	for _, value := range values {
		address := strings.TrimSpace(strings.SplitN(value, "/", 2)[0])
		address = strings.SplitN(address, "%", 2)[0]
		if address == ip {
			return true
		}
	}
	return false
}

func splitIPAddresses(values []string) (ipv4 []string, ipv6 []string, other []string) {
	for _, value := range values {
		switch {
		case strings.Contains(value, ":"):
			ipv6 = append(ipv6, value)
		case strings.Contains(value, "."):
			ipv4 = append(ipv4, value)
		case value != "":
			other = append(other, value)
		}
	}
	return ipv4, ipv6, other
}

func networkBandwidth(status *controlpb.IpStatus) string {
	parts := make([]string, 0, 2)
	if status.GetDownstreamKbps() > 0 {
		parts = append(parts, fmt.Sprintf("down=%dkbps", status.GetDownstreamKbps()))
	}
	if status.GetUpstreamKbps() > 0 {
		parts = append(parts, fmt.Sprintf("up=%dkbps", status.GetUpstreamKbps()))
	}
	return strings.Join(parts, " ")
}

func networkSignalStrengthForDetail(status *controlpb.IpStatus, connection *controlpb.WifiConnection) (int32, bool) {
	const androidSignalStrengthUnspecified = int32(-1 << 31)
	signalStrength := status.GetSignalStrength()
	if signalStrength == 0 || signalStrength == androidSignalStrengthUnspecified {
		return 0, false
	}
	if connection != nil && connection.GetRssiDbm() == signalStrength {
		return 0, false
	}
	return signalStrength, true
}

func networkCapabilitiesForDetail(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		switch strings.ToLower(value) {
		case "", "internet", "validated":
			continue
		default:
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
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

func renderGlobalIP(b *strings.Builder, result *controlpb.GlobalIpResult) {
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
		renderWifiCapabilities(b, diagnostics.GetCapabilities())
	}
	if len(diagnostics.GetNetworks()) > 0 {
		writeSection(b, "Networks")
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
		renderWifiScan(b, diagnostics.GetScan())
	}
}

func renderWifiScan(b *strings.Builder, scan *controlpb.WifiScan) {
	if scan == nil {
		return
	}
	writeKVSection(b, "Wi-Fi Scan", wifiScanSummaryRows(scan)...)
	writeBlankLine(b)
	renderScanResults(b, scan.GetResults())
	renderErrors(b, scan.GetErrors())
}

func wifiScanSummaryRows(scan *controlpb.WifiScan) []kvRow {
	fields := diagnosticFieldMap(scan.GetFields())
	rows := []kvRow{
		kv("requested_band", fields["requested_band"]),
		kv("results", firstNonBlank(fields["scan_result_count"], strconv.Itoa(len(scan.GetResults())))),
		kv("total", firstNonBlank(fields["scan_result_total_count"], strconv.Itoa(len(scan.GetResults())))),
		kv("errors", len(scan.GetErrors())),
	}
	for _, key := range []string{
		"wifi_enabled",
		"wifi_state",
		"scan_always_available",
		"scan_throttle_enabled",
		"fresh_scan_receiver_registered",
		"fresh_scan_start_scan",
		"fresh_scan_broadcast_received",
		"fresh_scan_results_updated",
		"fresh_scan_wait_completed",
		"fresh_scan_elapsed_ms",
	} {
		if value := fields[key]; value != "" {
			rows = append(rows, kv(key, value))
		}
	}
	return rows
}

func renderWifiScanDetail(b *strings.Builder, detail *controlpb.WifiScanDetail) {
	if detail == nil {
		return
	}
	writeKVSection(b, "Wi-Fi Scan Detail",
		kv("target", detail.GetTarget()),
		kv("results", firstNonBlank(diagnosticFieldMap(detail.GetFields())["scan_result_match_count"], strconv.Itoa(len(detail.GetResults())))),
		kv("total", diagnosticFieldMap(detail.GetFields())["scan_result_total_count"]),
		kv("requested_band", diagnosticFieldMap(detail.GetFields())["requested_band"]),
	)
	writeBlankLine(b)
	renderScanResults(b, detail.GetResults())
	renderErrors(b, detail.GetErrors())
}

func diagnosticFieldMap(fields []*controlpb.DiagnosticField) map[string]string {
	values := make(map[string]string, len(fields))
	for _, field := range fields {
		if field.GetKey() == "" {
			continue
		}
		values[field.GetKey()] = field.GetValue()
	}
	return values
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func renderScanResults(b *strings.Builder, results []*controlpb.WifiScanResult) {
	if len(results) == 0 {
		b.WriteString("  no results\n")
		return
	}
	columns := []displayTableColumn{
		{header: "SSID"},
		{header: "BSSID"},
		{header: "RSSI"},
		{header: "BAND"},
		{header: "FREQ"},
		{header: "STANDARD"},
		{header: "SECURITY"},
		{header: "FLAGS"},
		{header: "AP_MLD"},
		{header: "AP_LINK"},
		{header: "AFFILIATED"},
	}
	rows := make([][]string, 0, len(results))
	for _, result := range results {
		rows = append(rows, []string{
			empty(result.GetSsid(), "<hidden>"),
			empty(result.GetBssid(), "unknown"),
			strconv.Itoa(int(result.GetRssiDbm())),
			empty(result.GetBand(), wifiBandFromFrequency(result.GetFrequencyMhz())),
			strconv.Itoa(int(result.GetFrequencyMhz())),
			empty(result.GetWifiStandard(), "-"),
			empty(strings.Join(result.GetSecurityTypes(), ","), empty(result.GetCapabilities(), "-")),
			empty(strings.Join(scanConnectionCapabilityFlags(result), ","), "-"),
			empty(wifiMLOScanMLDMAC(result), "<none>"),
			scanMLOLinkID(result),
			strconv.Itoa(len(result.GetAffiliatedMloLinks())),
		})
	}
	writeDisplayTable(b, columns, rows)
	renderScanMLOLinks(b, results)
	renderScanSecurityDetails(b, results)
}

func scanConnectionCapabilityFlags(result *controlpb.WifiScanResult) []string {
	return connectionInformationElementCapabilityNames(result.GetInformationElements())
}

func renderScanMLOLinks(b *strings.Builder, results []*controlpb.WifiScanResult) {
	hasLinks := false
	for _, result := range results {
		if len(result.GetAffiliatedMloLinks()) > 0 {
			hasLinks = true
			break
		}
	}
	if !hasLinks {
		return
	}
	writeSection(b, "Scan Affiliated MLO Links")
	columns := []displayTableColumn{
		{header: "SSID"},
		{header: "BSSID"},
		{header: "AP_MLD"},
		{header: "AP_LINK"},
		{header: "ID"},
		{header: "STATE"},
		{header: "BAND"},
		{header: "CHANNEL"},
		{header: "RSSI"},
		{header: "TX"},
		{header: "RX"},
		{header: "AP_MAC"},
		{header: "STA_MAC"},
	}
	rows := [][]string{}
	for _, result := range results {
		for _, link := range result.GetAffiliatedMloLinks() {
			rows = append(rows, []string{
				empty(result.GetSsid(), "<hidden>"),
				empty(result.GetBssid(), "unknown"),
				empty(wifiMLOScanMLDMAC(result), "<none>"),
				scanMLOLinkID(result),
				strconv.Itoa(int(link.GetLinkId())),
				empty(link.GetState(), "unknown"),
				empty(link.GetBand(), "unknown"),
				strconv.Itoa(int(link.GetChannel())),
				strconv.Itoa(int(link.GetRssiDbm())),
				strconv.Itoa(int(link.GetTxLinkSpeedMbps())),
				strconv.Itoa(int(link.GetRxLinkSpeedMbps())),
				empty(link.GetApMacAddress(), "unknown"),
				empty(link.GetStaMacAddress(), "unknown"),
			})
		}
	}
	writeDisplayTable(b, columns, rows)
}

func renderScanSecurityDetails(b *strings.Builder, results []*controlpb.WifiScanResult) {
	securityResults := make([]*controlpb.WifiScanResult, 0)
	for _, result := range results {
		if result.GetSecurityDetails() != nil {
			securityResults = append(securityResults, result)
		}
	}
	if len(securityResults) == 0 {
		return
	}
	writeSection(b, "Scan Wi-Fi Security Details")
	for i, result := range securityResults {
		if i > 0 {
			b.WriteByte('\n')
		}
		label := fmt.Sprintf("ap ssid=%s bssid=%s", empty(result.GetSsid(), "<hidden>"), empty(result.GetBssid(), "<unknown>"))
		fmt.Fprintf(b, "  %s\n", label)
		for _, line := range wifiSecuritySummaryLines(result.GetSecurityDetails()) {
			fmt.Fprintf(b, "    %s\n", line)
		}
	}
}

func connectionInformationElementCapabilityNames(elements []*controlpb.WifiInformationElement) []string {
	seen := make(map[string]struct{}, len(elements))
	for _, element := range elements {
		name, ok := connectionInformationElementCapabilityName(element)
		if !ok {
			continue
		}
		seen[name] = struct{}{}
	}
	names := sortedMapKeys(seen)
	sort.Strings(names)
	return names
}

func connectionInformationElementCapabilityName(element *controlpb.WifiInformationElement) (string, bool) {
	switch element.GetId() {
	case 54:
		return "11r", true
	case 55:
		return "fast_bss_transition", true
	case 70:
		return "11k", true
	case 107:
		return "interworking", true
	case 111:
		return "roaming_consortium", true
	case 127:
		if informationElementBit(element, 19) {
			return "11v_bss_transition", true
		}
		return "", false
	case 201:
		return "reduced_neighbor_report", true
	case 255:
		switch element.GetIdExt() {
		case 107:
			return "eht_multi_link", true
		}
		return "", false
	default:
		return "", false
	}
}

func informationElementName(element *controlpb.WifiInformationElement) string {
	if element.GetId() == 255 {
		return informationElementExtensionName(element.GetIdExt())
	}
	switch element.GetId() {
	case 0:
		return "ssid"
	case 1:
		return "supported_rates"
	case 3:
		return "dsss_parameter_set"
	case 5:
		return "tim"
	case 7:
		return "country"
	case 11:
		return "bss_load"
	case 32:
		return "power_constraint"
	case 33:
		return "power_capability"
	case 35:
		return "tpc_report"
	case 36:
		return "supported_channels"
	case 42:
		return "erp"
	case 45:
		return "ht_capabilities"
	case 48:
		return "rsn"
	case 50:
		return "extended_supported_rates"
	case 54:
		return "mobility_domain_11r"
	case 55:
		return "fast_bss_transition"
	case 59:
		return "supported_operating_classes"
	case 61:
		return "ht_operation"
	case 70:
		return "rm_enabled_capabilities_11k"
	case 74:
		return "overlapping_bss_scan_parameters"
	case 107:
		return "interworking"
	case 111:
		return "roaming_consortium"
	case 127:
		return "extended_capabilities"
	case 191:
		return "vht_capabilities"
	case 192:
		return "vht_operation"
	case 195:
		return "tx_power_envelope"
	case 201:
		return "reduced_neighbor_report"
	case 221:
		return "vendor_specific"
	case 244:
		return "rsn_extension"
	default:
		return fmt.Sprintf("unknown_%d", element.GetId())
	}
}

func informationElementExtensionName(idExt int32) string {
	switch idExt {
	case 35:
		return "he_capabilities"
	case 36:
		return "he_operation"
	case 37:
		return "uora_parameter_set"
	case 38:
		return "mu_edca_parameter_set"
	case 39:
		return "spatial_reuse_parameter_set"
	case 45:
		return "he_bss_load"
	case 59:
		return "he_6ghz_capabilities"
	case 106:
		return "eht_operation"
	case 107:
		return "eht_multi_link"
	case 108:
		return "eht_capabilities"
	default:
		return fmt.Sprintf("extension_%d", idExt)
	}
}

func informationElementBit(element *controlpb.WifiInformationElement, bit int) bool {
	if bit < 0 {
		return false
	}
	bytes, err := hex.DecodeString(element.GetBytesHex())
	if err != nil {
		return false
	}
	byteIndex := bit / 8
	if byteIndex >= len(bytes) {
		return false
	}
	return bytes[byteIndex]&(1<<uint(bit%8)) != 0
}

func renderWifiConnectionDetailedCapabilities(b *strings.Builder, conn *controlpb.WifiConnection) {
	rows := wifiDetailedCapabilityRows(conn)
	if len(rows) == 0 {
		return
	}
	writeKVSection(b, "HE/EHT Details", rows...)
}

func renderWifiSecurityDetails(b *strings.Builder, details *controlpb.WifiSecurityDetails) {
	if details == nil {
		return
	}
	rows := wifiSecurityDetailRows(details)
	if len(rows) == 0 {
		return
	}
	writeKVSection(b, "Wi-Fi Security Details", rows...)
}

func wifiSecurityDetailRows(value *controlpb.WifiSecurityDetails) []kvRow {
	rows := []kvRow{}
	if value.GetRsnPresent() {
		rows = append(rows,
			kv("rsn", fmt.Sprintf("version=%d capabilities=0x%s", value.GetRsnVersion(), value.GetRsnCapabilitiesHex())),
			kv("group_data", value.GetGroupDataCipher()),
			kv("pairwise", strings.Join(value.GetPairwiseCiphers(), ",")),
			kv("akm", strings.Join(value.GetAkmSuites(), ",")),
			kv("pmf", fmt.Sprintf("capable=%t required=%t", value.GetPmfCapable(), value.GetPmfRequired())),
		)
		if value.GetGroupManagementCipher() != "" {
			rows = append(rows, kv("group_mgmt", value.GetGroupManagementCipher()))
		}
	}
	rows = append(rows, kv("wifi7", fmt.Sprintf("gcmp256=%t sae_gdh=%t ft_sae_gdh=%t beacon_protection=%t personal_ready=%t",
		value.GetGcmp_256(), value.GetSaeGdh(), value.GetFtSaeGdh(), value.GetBeaconProtection(), value.GetWifi7PersonalReady())))
	rows = append(rows, kv("wifi7_strict", wifiSecurityStrictSummary(value)))
	if value.GetRsnxePresent() {
		rows = append(rows, kv("rsnxe", strings.Join(value.GetRsnxeCapabilities(), ",")))
	}
	if value.GetExtendedCapabilitiesPresent() {
		rows = append(rows, kv("extended", strings.Join(value.GetExtendedCapabilities(), ",")))
	}
	if len(value.GetWarnings()) > 0 {
		rows = append(rows, kv("warnings", multiLineValue(value.GetWarnings())))
	}
	return nonEmptyKVRows(rows)
}

func wifiSecuritySummaryLines(value *controlpb.WifiSecurityDetails) []string {
	lines := []string{}
	if value.GetRsnPresent() {
		lines = append(lines,
			fmt.Sprintf("rsn version=%d group=%s pairwise=%s", value.GetRsnVersion(), empty(value.GetGroupDataCipher(), "<unknown>"), wifiMLOJoinStrings(value.GetPairwiseCiphers(), "<none>")),
			fmt.Sprintf("akm %s", wifiMLOJoinStrings(value.GetAkmSuites(), "<none>")),
			fmt.Sprintf("pmf capable=%t required=%t group_mgmt=%s", value.GetPmfCapable(), value.GetPmfRequired(), empty(value.GetGroupManagementCipher(), "<none>")),
		)
	}
	lines = append(lines, fmt.Sprintf("wifi7 gcmp256=%t sae_gdh=%t ft_sae_gdh=%t beacon_protection=%t personal_ready=%t",
		value.GetGcmp_256(), value.GetSaeGdh(), value.GetFtSaeGdh(), value.GetBeaconProtection(), value.GetWifi7PersonalReady()))
	lines = append(lines, "wifi7_strict "+wifiSecurityStrictSummary(value))
	if value.GetRsnxePresent() {
		lines = append(lines, "rsnxe "+wifiMLOJoinStrings(value.GetRsnxeCapabilities(), "<none>"))
	}
	if value.GetExtendedCapabilitiesPresent() {
		lines = append(lines, "extended "+wifiMLOJoinStrings(value.GetExtendedCapabilities(), "<none>"))
	}
	if len(value.GetWarnings()) > 0 {
		lines = append(lines, "warnings "+strings.Join(value.GetWarnings(), ","))
	}
	return lines
}

func wifiSecurityStrictSummary(value *controlpb.WifiSecurityDetails) string {
	pairwiseOnly := len(value.GetPairwiseCiphers()) > 0 && wifiSecurityAllIn(value.GetPairwiseCiphers(), map[string]bool{"gcmp_256": true})
	akmGdhOnly := len(value.GetAkmSuites()) > 0 && wifiSecurityAllIn(value.GetAkmSuites(), map[string]bool{
		"sae_gdh":    true,
		"ft_sae_gdh": true,
	})
	fallback := []string{}
	for _, cipher := range value.GetPairwiseCiphers() {
		if cipher != "gcmp_256" {
			fallback = append(fallback, cipher)
		}
	}
	for _, akm := range value.GetAkmSuites() {
		if akm != "sae_gdh" && akm != "ft_sae_gdh" {
			fallback = append(fallback, akm)
		}
	}
	groupMgmt256 := value.GetGroupManagementCipher() == "bip_gmac_256" || value.GetGroupManagementCipher() == "bip_cmac_256"
	strictReady := value.GetPmfRequired() && pairwiseOnly && akmGdhOnly && value.GetBeaconProtection()
	return fmt.Sprintf(
		"pairwise_gcmp256_only=%t akm_gdh_only=%t group_data_gcmp256=%t group_mgmt_256=%t fallback=%s strict_ready=%t",
		pairwiseOnly,
		akmGdhOnly,
		value.GetGroupDataCipher() == "gcmp_256",
		groupMgmt256,
		wifiMLOJoinStrings(fallback, "<none>"),
		strictReady,
	)
}

func wifiSecurityAllIn(values []string, allowed map[string]bool) bool {
	for _, value := range values {
		if !allowed[value] {
			return false
		}
	}
	return true
}

func wifiDetailedCapabilityRows(conn *controlpb.WifiConnection) []kvRow {
	rows := []kvRow{}
	if value := conn.GetHeCapabilities(); value != nil {
		rows = append(rows, kv("he_cap", wifiHECapabilitiesSummary(value)))
	}
	if value := conn.GetHeOperation(); value != nil {
		rows = append(rows, kv("he_oper", wifiHEOperationSummary(value)))
	}
	if value := conn.GetEhtCapabilities(); value != nil {
		rows = append(rows, kv("eht_cap", wifiEHTCapabilitiesSummary(value)))
	}
	if value := conn.GetEhtOperation(); value != nil {
		rows = append(rows, kv("eht_oper", wifiEHTOperationSummary(value)))
	}
	if value := conn.GetHeUoraParameterSet(); value != nil {
		rows = append(rows, kv("uora", fmt.Sprintf("eocw_min=%d eocw_max=%d%s", value.GetEocwMin(), value.GetEocwMax(), wifiTruncatedSuffix(value.GetTruncated()))))
	}
	if value := conn.GetHeMuEdcaParameterSet(); value != nil {
		rows = append(rows, kv("mu_edca", wifiHEMUEdcaSummary(value)))
	}
	if value := conn.GetHeSpatialReuseParameterSet(); value != nil {
		rows = append(rows, kv("spatial_reuse", wifiHESpatialReuseSummary(value)))
	}
	if value := conn.GetHe_6GhzCapabilities(); value != nil {
		rows = append(rows, kv("he_6ghz_cap", wifiHE6GHzCapabilitiesSummary(value)))
	}
	return rows
}

func wifiHECapabilitiesSummary(value *controlpb.WifiHeCapabilities) string {
	lines := []string{fmt.Sprintf("mac=0x%s phy=0x%s", value.GetMacCapabilitiesHex(), value.GetPhyCapabilitiesHex())}
	lines = append(lines, value.GetFeatures()...)
	lines = append(lines, wifiMCSNSSSummary(value.GetMcsNss())...)
	if value.GetPpeThresholdsPresent() {
		lines = append(lines, fmt.Sprintf("ppe nss=%d ru=%s hex=0x%s", value.GetPpeNssCount(), strings.Join(value.GetPpeRuIndices(), ","), value.GetPpeThresholdsHex()))
	}
	lines = appendWifiDecodeWarnings(lines, value.GetTruncated(), value.GetWarnings())
	return multiLineValue(lines)
}

func wifiHE6GHzCapabilitiesSummary(value *controlpb.WifiHe6GhzCapabilities) string {
	lines := []string{
		fmt.Sprintf("cap=0x%x max_mpdu=%d max_ampdu_exp=%d max_ampdu=%d", value.GetCapabilities(), value.GetMaxMpduLengthBytes(), value.GetMaxAmpduLengthExponent(), value.GetMaxAmpduLengthBytes()),
		fmt.Sprintf("min_mpdu_start=%s smps=%s", value.GetMinimumMpduStartSpacing(), value.GetSmPowerSave()),
	}
	lines = append(lines, value.GetFeatures()...)
	lines = appendWifiDecodeWarnings(lines, value.GetTruncated(), value.GetWarnings())
	return multiLineValue(lines)
}

func wifiEHTCapabilitiesSummary(value *controlpb.WifiEhtCapabilities) string {
	lines := []string{fmt.Sprintf("mac=0x%s phy=0x%s", value.GetMacCapabilitiesHex(), value.GetPhyCapabilitiesHex())}
	lines = append(lines, value.GetFeatures()...)
	lines = append(lines, wifiMCSNSSSummary(value.GetMcsNss())...)
	if value.GetPpeThresholdsPresent() {
		lines = append(lines, fmt.Sprintf("ppe nss=%d ru=%s hex=0x%s", value.GetPpeNssCount(), strings.Join(value.GetPpeRuIndices(), ","), value.GetPpeThresholdsHex()))
	}
	lines = appendWifiDecodeWarnings(lines, value.GetTruncated(), value.GetWarnings())
	return multiLineValue(lines)
}

func wifiHEOperationSummary(value *controlpb.WifiHeOperation) string {
	lines := []string{
		fmt.Sprintf("params=0x%x basic_mcs_nss=0x%s", value.GetParameters(), value.GetBasicMcsNssSetHex()),
		fmt.Sprintf("bss_color=%d disabled=%t", value.GetBssColor(), value.GetBssColorDisabled()),
	}
	if value.GetChannelWidth() != "" {
		lines = append(lines, fmt.Sprintf("width=%s primary=%d ccfs0=%d ccfs1=%d", value.GetChannelWidth(), value.GetPrimaryChannel(), value.GetCenterFreqSegment0(), value.GetCenterFreqSegment1()))
	}
	lines = append(lines, value.GetFlags()...)
	lines = appendWifiDecodeWarnings(lines, value.GetTruncated(), value.GetWarnings())
	return multiLineValue(lines)
}

func wifiEHTOperationSummary(value *controlpb.WifiEhtOperation) string {
	lines := []string{fmt.Sprintf("params=0x%x basic_mcs_nss=0x%s", value.GetParameters(), value.GetBasicMcsNssSetHex())}
	if value.GetChannelWidth() != "" {
		lines = append(lines, fmt.Sprintf("width=%s ccfs0=%d ccfs1=%d", value.GetChannelWidth(), value.GetCenterFreqSegment0(), value.GetCenterFreqSegment1()))
	}
	if value.GetDisabledSubchannelBitmap() != 0 {
		lines = append(lines, fmt.Sprintf("disabled_subchannel_bitmap=0x%x", value.GetDisabledSubchannelBitmap()))
	}
	lines = append(lines, value.GetFlags()...)
	lines = appendWifiDecodeWarnings(lines, value.GetTruncated(), value.GetWarnings())
	return multiLineValue(lines)
}

func wifiHEMUEdcaSummary(value *controlpb.WifiHeMuEdcaParameterSet) string {
	lines := []string{fmt.Sprintf("qos_info=0x%x", value.GetQosInfo())}
	for _, ac := range value.GetAc() {
		lines = append(lines, fmt.Sprintf("%s aci=%d aifsn=%d acm=%t ecw=%d/%d timer=%d", ac.GetAc(), ac.GetAci(), ac.GetAifsn(), ac.GetAcm(), ac.GetEcwMin(), ac.GetEcwMax(), ac.GetTimer()))
	}
	lines = appendWifiDecodeWarnings(lines, value.GetTruncated(), value.GetWarnings())
	return multiLineValue(lines)
}

func wifiHESpatialReuseSummary(value *controlpb.WifiHeSpatialReuseParameterSet) string {
	lines := []string{fmt.Sprintf("control=0x%x flags=%s", value.GetSrControl(), strings.Join(value.GetFlags(), ","))}
	if value.GetNonSrgObssPdMaxOffset() != 0 {
		lines = append(lines, fmt.Sprintf("non_srg_obss_pd_max_offset=%d", value.GetNonSrgObssPdMaxOffset()))
	}
	if value.GetSrgObssPdMinOffset() != 0 || value.GetSrgObssPdMaxOffset() != 0 {
		lines = append(lines, fmt.Sprintf("srg_obss_pd=%d/%d", value.GetSrgObssPdMinOffset(), value.GetSrgObssPdMaxOffset()))
	}
	if value.GetSrgBssColorBitmapHex() != "" {
		lines = append(lines, "srg_bss_color_bitmap=0x"+value.GetSrgBssColorBitmapHex())
	}
	if value.GetSrgPartialBssidBitmapHex() != "" {
		lines = append(lines, "srg_partial_bssid_bitmap=0x"+value.GetSrgPartialBssidBitmapHex())
	}
	lines = appendWifiDecodeWarnings(lines, value.GetTruncated(), value.GetWarnings())
	return multiLineValue(lines)
}

func wifiMCSNSSSummary(values []*controlpb.WifiMcsNssSupport) []string {
	type group struct {
		standard string
		width    string
		mcs      string
		items    []*controlpb.WifiMcsNssSupport
	}
	groupsByKey := map[string]*group{}
	order := []string{}
	for _, value := range values {
		key := strings.Join([]string{value.GetStandard(), value.GetBandwidth(), value.GetMcsRange()}, "/")
		if _, ok := groupsByKey[key]; !ok {
			groupsByKey[key] = &group{standard: value.GetStandard(), width: value.GetBandwidth(), mcs: value.GetMcsRange()}
			order = append(order, key)
		}
		groupsByKey[key].items = append(groupsByKey[key].items, value)
	}
	lines := []string{}
	for _, key := range order {
		group := groupsByKey[key]
		parts := make([]string, 0, len(group.items))
		for _, item := range group.items {
			nss := item.GetMaxNss()
			if nss == 0 {
				nss = item.GetNss()
			}
			parts = append(parts, fmt.Sprintf("%s=nss%d", item.GetDirection(), nss))
		}
		lines = append(lines, fmt.Sprintf("mcs_nss %s/%s/%s %s", group.standard, group.width, group.mcs, strings.Join(parts, " ")))
	}
	return lines
}

func appendWifiDecodeWarnings(lines []string, truncated bool, warnings []string) []string {
	if truncated {
		lines = append(lines, "truncated=true")
	}
	for _, warning := range warnings {
		lines = append(lines, "warning="+warning)
	}
	return lines
}

func wifiTruncatedSuffix(truncated bool) string {
	if truncated {
		return " truncated=true"
	}
	return ""
}

func scanMLOLinkID(result *controlpb.WifiScanResult) string {
	if result == nil {
		return "<none>"
	}
	if result.GetApMloLinkId() >= 0 {
		if result.GetApMldMacAddress() == "" &&
			len(result.GetAffiliatedMloLinks()) == 0 &&
			!wifiMLOHasElement(result.GetInformationElements()) &&
			!strings.EqualFold(result.GetWifiStandard(), "802.11be") {
			return "<none>"
		}
		return mloLinkID(result.GetApMloLinkId())
	}
	if id := wifiMLOCurrentLinkIDFromElements(result.GetInformationElements()); id != nil {
		return fmt.Sprint(*id)
	}
	if result.GetApMldMacAddress() == "" && len(result.GetAffiliatedMloLinks()) == 0 {
		return "<none>"
	}
	return "<none>"
}

func renderWifiCapabilities(b *strings.Builder, capabilities *controlpb.WifiCapabilities) {
	if capabilities == nil {
		return
	}
	writeSection(b, "Wi-Fi Capabilities")
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
	writeKVSection(b, "Wi-Fi Operation",
		kv("operation", empty(result.GetOperation(), "unknown")),
		kv("ok", result.GetOk()),
		kv("message", empty(result.GetMessage(), "-")),
	)
	renderDiagnosticFields(b, result.GetFields())
	renderErrors(b, result.GetErrors())
}

func renderWifiAssert(b *strings.Builder, result *controlpb.WifiAssertResult) {
	if result == nil {
		return
	}
	writeKVSection(b, "Wi-Fi Assert",
		kv("passed", result.GetPassed()),
		kv("checks", len(result.GetChecks())),
		kv("elapsed", fmt.Sprintf("%dms", result.GetElapsedMs())),
	)
	if len(result.GetChecks()) > 0 {
		writeSection(b, "Checks")
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
}

func renderWifiMonitor(b *strings.Builder, result *controlpb.WifiMonitorResult) {
	if result == nil {
		return
	}
	writeKVSection(b, "Wi-Fi Monitor", kv("events", len(result.GetEvents())), kv("errors", len(result.GetErrors())))
	if len(result.GetEvents()) > 0 {
		writeSection(b, "Events")
		tw := tabwriter.NewWriter(b, 0, 0, 2, ' ', 0)
		_, _ = fmt.Fprintln(tw, "TIME\tTYPE\tMESSAGE")
		for _, event := range result.GetEvents() {
			_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\n", unixMillis(event.GetUnixTimeMs()), empty(event.GetType(), "event"), event.GetMessage())
		}
		_ = tw.Flush()
	}
	renderErrors(b, result.GetErrors())
}

func renderWifiCycle(b *strings.Builder, result *controlpb.WifiCycleResult) {
	if result == nil {
		return
	}
	writeKVSection(b, "Wi-Fi Cycle",
		kv("requested", result.GetRequestedCount()),
		kv("completed", result.GetCompletedCount()),
		kv("passed", result.GetPassedCount()),
		kv("errors", len(result.GetErrors())),
	)
	if len(result.GetSteps()) > 0 {
		writeSection(b, "Steps")
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

func renderConfigSet(view ConfigView) string {
	var b strings.Builder
	if view.Standalone != nil {
		renderStandaloneConfigSet(&b, view.Standalone)
	}
	return b.String()
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
	if upload := config.GetUpload(); upload != nil && (upload.GetUrl() != "" || upload.GetWifi().GetSsid() != "") {
		writeConfigLine(b, depth+1, "upload {")
		if upload.GetUrl() != "" {
			writeConfigLine(b, depth+2, "to %s", shellQuote(upload.GetUrl()))
		}
		if wifi := upload.GetWifi(); wifi.GetSsid() != "" {
			writeConfigLine(b, depth+2, "via wifi {")
			writeConfigLine(b, depth+3, "essid %s", shellQuote(wifi.GetSsid()))
			if wifi.GetPassphrase() != "" {
				writeConfigLine(b, depth+3, "passphrase <redacted>")
			}
			if wifi.GetSecurity() != controlpb.ConnectWifi_SECURITY_UNSPECIFIED {
				writeConfigLine(b, depth+3, "security %s", standaloneSecurityName(wifi.GetSecurity()))
			}
			if wifi.GetBssid() != "" {
				writeConfigLine(b, depth+3, "bssid %s", shellQuote(wifi.GetBssid()))
			}
			if wifi.GetBand() != controlpb.WifiBand_WIFI_BAND_UNSPECIFIED && wifi.GetBand() != controlpb.WifiBand_WIFI_BAND_ALL {
				writeConfigLine(b, depth+3, "band %s", standaloneBandName(wifi.GetBand()))
			}
			if wifi.GetMacRandomization() != controlpb.ConnectWifi_MAC_RANDOMIZATION_UNSPECIFIED {
				writeConfigLine(b, depth+3, "mac-randomization %s", standaloneMacRandomizationName(wifi.GetMacRandomization()))
			}
			if wifi.GetTimeoutMs() != 0 {
				writeConfigLine(b, depth+3, "timeout %s", formatConfigDuration(wifi.GetTimeoutMs()))
			}
			writeConfigLine(b, depth+2, "}")
		}
		writeConfigLine(b, depth+1, "}")
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
			writeConfigLine(b, depth+2, "wifi %s {", shellQuote(group.GetName()))
			if group.GetEssid() != "" {
				writeConfigLine(b, depth+3, "match essid %s", shellQuote(group.GetEssid()))
			}
			if group.GetBssid() != "" {
				writeConfigLine(b, depth+3, "match bssid %s", shellQuote(group.GetBssid()))
			}
			if group.GetPassphrase() != "" {
				writeConfigLine(b, depth+3, "passphrase <redacted>")
			}
			if group.GetSecurity() != controlpb.ConnectWifi_SECURITY_UNSPECIFIED {
				writeConfigLine(b, depth+3, "security %s", standaloneSecurityName(group.GetSecurity()))
			}
			if group.GetBand() != controlpb.WifiBand_WIFI_BAND_UNSPECIFIED && group.GetBand() != controlpb.WifiBand_WIFI_BAND_ALL {
				writeConfigLine(b, depth+3, "band %s", standaloneBandName(group.GetBand()))
			}
			if group.GetMacRandomization() != controlpb.ConnectWifi_MAC_RANDOMIZATION_UNSPECIFIED {
				writeConfigLine(b, depth+3, "mac-randomization %s", standaloneMacRandomizationName(group.GetMacRandomization()))
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
		for _, check := range festa.GetChecks() {
			renderStandaloneCheckBlock(b, check, depth+2)
		}
		writeConfigLine(b, depth+1, "}")
	}
	writeConfigLine(b, depth, "}")
}

func renderStandaloneConfigSet(b *strings.Builder, config *controlpb.StandaloneConfig) {
	if config == nil {
		return
	}
	if config.GetEnabled() {
		writeSetLine(b, "standalone enabled")
	} else {
		writeSetLine(b, "standalone disabled")
	}
	if config.GetRetentionMs() != 0 {
		writeSetLine(b, "standalone retention %s", formatConfigDuration(config.GetRetentionMs()))
	}
	if config.GetMaxBytes() != 0 {
		writeSetLine(b, "standalone max-size %s", strconv.FormatUint(config.GetMaxBytes(), 10))
	}
	if upload := config.GetUpload(); upload != nil {
		if upload.GetUrl() != "" {
			writeSetLine(b, "standalone upload to %s", shellQuote(upload.GetUrl()))
		}
		if wifi := upload.GetWifi(); wifi != nil && wifi.GetSsid() != "" {
			renderStandaloneUploadWifiSet(b, wifi)
		}
	}
	for _, festa := range config.GetFestas() {
		renderStandaloneFestaSet(b, festa)
	}
}

func renderStandaloneUploadWifiSet(b *strings.Builder, wifi *controlpb.ConnectWifi) {
	if wifi == nil || wifi.GetSsid() == "" {
		return
	}
	var parts []string
	parts = append(parts, "essid", shellQuote(wifi.GetSsid()))
	if wifi.GetPassphrase() != "" {
		parts = append(parts, "passphrase", shellQuote(wifi.GetPassphrase()))
	}
	if wifi.GetSecurity() != controlpb.ConnectWifi_SECURITY_UNSPECIFIED {
		parts = append(parts, "security", standaloneSecurityName(wifi.GetSecurity()))
	}
	if wifi.GetBssid() != "" {
		parts = append(parts, "bssid", shellQuote(wifi.GetBssid()))
	}
	if wifi.GetBand() != controlpb.WifiBand_WIFI_BAND_UNSPECIFIED && wifi.GetBand() != controlpb.WifiBand_WIFI_BAND_ALL {
		parts = append(parts, "band", standaloneBandName(wifi.GetBand()))
	}
	if wifi.GetMacRandomization() != controlpb.ConnectWifi_MAC_RANDOMIZATION_UNSPECIFIED {
		parts = append(parts, "mac-randomization", standaloneMacRandomizationName(wifi.GetMacRandomization()))
	}
	if wifi.GetTimeoutMs() != 0 {
		parts = append(parts, "timeout", formatConfigDuration(wifi.GetTimeoutMs()))
	}
	writeSetLine(b, "standalone upload via wifi %s", strings.Join(parts, " "))
}

func renderStandaloneFestaSet(b *strings.Builder, festa *controlpb.StandaloneFesta) {
	if festa == nil {
		return
	}
	name := shellQuote(festa.GetName())
	if festa.GetEnabled() {
		writeSetLine(b, "standalone festa %s enabled", name)
	} else {
		writeSetLine(b, "standalone festa %s disabled", name)
	}
	if festa.GetIntervalMs() != 0 {
		writeSetLine(b, "standalone festa %s interval %s", name, formatConfigDuration(festa.GetIntervalMs()))
	}
	for _, group := range festa.GetWifiGroups() {
		renderStandaloneWifiGroupSet(b, name, group)
	}
	for _, check := range festa.GetChecks() {
		renderStandaloneCheckSet(b, name, check)
	}
}

func renderStandaloneWifiGroupSet(b *strings.Builder, festaName string, group *controlpb.StandaloneWifiGroup) {
	if group == nil {
		return
	}
	groupName := shellQuote(group.GetName())
	macRandomization := ""
	if group.GetMacRandomization() != controlpb.ConnectWifi_MAC_RANDOMIZATION_UNSPECIFIED {
		macRandomization = " mac-randomization " + standaloneMacRandomizationName(group.GetMacRandomization())
	}
	macRandomizationRendered := false
	if group.GetEssid() != "" {
		writeSetLine(b, "standalone festa %s wifi %s match essid %s%s", festaName, groupName, shellQuote(group.GetEssid()), macRandomization)
		macRandomizationRendered = true
	}
	if group.GetBssid() != "" {
		suffix := ""
		if !macRandomizationRendered {
			suffix = macRandomization
		}
		writeSetLine(b, "standalone festa %s wifi %s match bssid %s%s", festaName, groupName, shellQuote(group.GetBssid()), suffix)
	}
	if group.GetPassphrase() != "" {
		if group.GetSecurity() != controlpb.ConnectWifi_SECURITY_UNSPECIFIED {
			writeSetLine(b, "standalone festa %s wifi %s passphrase %s security %s", festaName, groupName, shellQuote(group.GetPassphrase()), standaloneSecurityName(group.GetSecurity()))
		} else {
			writeSetLine(b, "standalone festa %s wifi %s passphrase %s", festaName, groupName, shellQuote(group.GetPassphrase()))
		}
	}
	if group.GetBand() != controlpb.WifiBand_WIFI_BAND_UNSPECIFIED && group.GetBand() != controlpb.WifiBand_WIFI_BAND_ALL {
		writeSetLine(b, "standalone festa %s wifi %s band %s", festaName, groupName, standaloneBandName(group.GetBand()))
	}
	if group.GetRequireIp() {
		writeSetLine(b, "standalone festa %s wifi %s wait ip", festaName, groupName)
	}
	if group.GetRequireValidated() {
		writeSetLine(b, "standalone festa %s wifi %s wait validated", festaName, groupName)
	}
	if group.GetTimeoutMs() != 0 {
		writeSetLine(b, "standalone festa %s wifi %s timeout %s", festaName, groupName, formatConfigDuration(group.GetTimeoutMs()))
	}
}

func renderStandaloneCheckSet(b *strings.Builder, festaName string, check *controlpb.StandaloneCheck) {
	if check == nil {
		return
	}
	name := shellQuote(check.GetName())
	switch test := check.GetTest().(type) {
	case *controlpb.StandaloneCheck_Dns:
		dns := test.Dns
		if dns == nil {
			return
		}
		line := fmt.Sprintf("standalone festa %s check %s test dns name %s", festaName, name, shellQuote(dns.GetName()))
		if len(dns.GetQtypes()) > 0 {
			line += " type " + standaloneQTypesName(dns.GetQtypes())
		}
		if dns.GetTimeoutMs() != 0 {
			line += " timeout " + formatConfigDuration(dns.GetTimeoutMs())
		}
		writeSetLine(b, "%s", line)
	case *controlpb.StandaloneCheck_Ping:
		ping := test.Ping
		if ping == nil {
			return
		}
		line := fmt.Sprintf("standalone festa %s check %s test ping host %s", festaName, name, shellQuote(ping.GetHost()))
		if ping.GetCount() != 0 {
			line += fmt.Sprintf(" count %d", ping.GetCount())
		}
		if ping.GetSizeBytes() != 0 {
			line += fmt.Sprintf(" size %d", ping.GetSizeBytes())
		}
		if ping.GetTimeoutMs() != 0 {
			line += " timeout " + formatConfigDuration(ping.GetTimeoutMs())
		}
		writeSetLine(b, "%s", line)
	case *controlpb.StandaloneCheck_Http:
		http := test.Http
		if http == nil {
			return
		}
		line := fmt.Sprintf("standalone festa %s check %s test http url %s", festaName, name, shellQuote(http.GetUrl()))
		if http.GetExpectedStatus() != 0 {
			line += fmt.Sprintf(" expected-status %d", http.GetExpectedStatus())
		}
		if http.GetTimeoutMs() != 0 {
			line += " timeout " + formatConfigDuration(http.GetTimeoutMs())
		}
		writeSetLine(b, "%s", line)
	}
}

func renderStandaloneCheckBlock(b *strings.Builder, check *controlpb.StandaloneCheck, depth int) {
	if check == nil {
		return
	}
	switch test := check.GetTest().(type) {
	case *controlpb.StandaloneCheck_Dns:
		writeConfigLine(b, depth, "check %s {", shellQuote(check.GetName()))
		dns := test.Dns
		writeConfigLine(b, depth+1, "test dns")
		writeConfigLine(b, depth+1, "name %s", shellQuote(dns.GetName()))
		if len(dns.GetQtypes()) > 0 {
			writeConfigLine(b, depth+1, "type %s", standaloneQTypesName(dns.GetQtypes()))
		}
		if dns.GetTimeoutMs() != 0 {
			writeConfigLine(b, depth+1, "timeout %s", formatConfigDuration(dns.GetTimeoutMs()))
		}
		writeConfigLine(b, depth, "}")
	case *controlpb.StandaloneCheck_Ping:
		writeConfigLine(b, depth, "check %s {", shellQuote(check.GetName()))
		ping := test.Ping
		writeConfigLine(b, depth+1, "test ping")
		writeConfigLine(b, depth+1, "host %s", shellQuote(ping.GetHost()))
		if ping.GetCount() != 0 {
			writeConfigLine(b, depth+1, "count %d", ping.GetCount())
		}
		if ping.GetSizeBytes() != 0 {
			writeConfigLine(b, depth+1, "size %d", ping.GetSizeBytes())
		}
		if ping.GetTimeoutMs() != 0 {
			writeConfigLine(b, depth+1, "timeout %s", formatConfigDuration(ping.GetTimeoutMs()))
		}
		writeConfigLine(b, depth, "}")
	case *controlpb.StandaloneCheck_Http:
		writeConfigLine(b, depth, "check %s {", shellQuote(check.GetName()))
		http := test.Http
		writeConfigLine(b, depth+1, "test http")
		writeConfigLine(b, depth+1, "url %s", shellQuote(http.GetUrl()))
		if http.GetExpectedStatus() != 0 {
			writeConfigLine(b, depth+1, "expected-status %d", http.GetExpectedStatus())
		}
		if http.GetTimeoutMs() != 0 {
			writeConfigLine(b, depth+1, "timeout %s", formatConfigDuration(http.GetTimeoutMs()))
		}
		writeConfigLine(b, depth, "}")
	}
}

func writeConfigLine(b *strings.Builder, depth int, format string, args ...any) {
	b.WriteString(strings.Repeat("  ", depth))
	fmt.Fprintf(b, format, args...)
	b.WriteByte('\n')
}

func writeSetLine(b *strings.Builder, format string, args ...any) {
	b.WriteString("set ")
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
	writeKVSection(b, "Standalone",
		kv("enabled", status.GetEnabled()),
		kv("running", status.GetRunning()),
		kv("stored", status.GetStoredRuns()),
		kv("unsynced", status.GetUnsyncedRuns()),
		kv("bytes", formatBytes(status.GetStoredBytes())),
	)
	if status.GetCurrentRunId() != "" {
		writeKVSection(b, "Current Run", kv("id", status.GetCurrentRunId()))
	}
	if status.GetLastRunId() != "" {
		writeKVSection(b, "Last Run",
			kv("id", status.GetLastRunId()),
			kv("started", unixMillis(status.GetLastStartedUnixMs())),
			kv("finished", unixMillis(status.GetLastFinishedUnixMs())),
		)
	}
	if status.GetMessage() != "" {
		writeKVSection(b, "Message", kv("text", status.GetMessage()))
	}
}

func renderStandaloneRuns(b *strings.Builder, runs *controlpb.StandaloneRuns) {
	if runs == nil {
		return
	}
	writeKVSection(b, "Standalone Runs",
		kv("returned", len(runs.GetRuns())),
		kv("total", runs.GetTotalRuns()),
		kv("unsynced", runs.GetUnsyncedRuns()),
	)
	if len(runs.GetRuns()) == 0 {
		return
	}
	writeSection(b, "Runs")
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
	writeKVSection(b, "Standalone Run",
		kv("id", summary.GetRunId()),
		kv("status", empty(summary.GetStatus(), "-")),
		kv("synced", summary.GetSynced()),
		kv("festa", empty(summary.GetFestaName(), "-")),
		kv("steps", summary.GetStepCount()),
		kv("failed", summary.GetFailedStepCount()),
	)
	if len(run.GetSteps()) == 0 {
		return
	}
	writeSection(b, "Steps")
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
	writeKVSection(b, "Standalone Cleared",
		kv("runs", result.GetRemovedRuns()),
		kv("bytes", formatBytes(result.GetRemovedBytes())),
	)
}

func renderDiagnosticFields(b *strings.Builder, fields []*controlpb.DiagnosticField) {
	if len(fields) == 0 {
		return
	}
	writeSection(b, "Fields")
	renderDiagnosticFieldRows(b, fields)
}

func renderDiagnosticFieldRows(b *strings.Builder, fields []*controlpb.DiagnosticField) {
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
	if len(errors) == 0 {
		return
	}
	writeSection(b, "Errors")
	for _, err := range errors {
		fmt.Fprintf(b, "  %s\n", err)
	}
}

func writeList(b *strings.Builder, label string, values []string) {
	writeListSection(b, label, values)
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

func standaloneMacRandomizationName(value controlpb.ConnectWifi_MacRandomization) string {
	switch value {
	case controlpb.ConnectWifi_MAC_RANDOMIZATION_AUTO:
		return "auto"
	case controlpb.ConnectWifi_MAC_RANDOMIZATION_NONE:
		return "none"
	case controlpb.ConnectWifi_MAC_RANDOMIZATION_PERSISTENT:
		return "persistent"
	case controlpb.ConnectWifi_MAC_RANDOMIZATION_NON_PERSISTENT:
		return "non-persistent"
	default:
		return "unspecified"
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
