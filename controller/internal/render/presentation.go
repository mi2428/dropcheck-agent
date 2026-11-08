package render

import (
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/mi2428/dropcheck-agent/controller/internal/command"
	"github.com/mi2428/dropcheck-agent/controller/internal/controlpb"
	"github.com/mi2428/dropcheck-agent/controller/internal/oui"
)

// Presentation is local display state, never an acquisition or evaluation option.
// Width zero selects lossless records for pipes and unknown terminal widths.
type Presentation struct {
	Width  int
	Detail bool
	// RequestedFresh is supplied by the adapter, not inferred from source/update data.
	RequestedFresh *bool
	// DetailAvailable must be set only after the adapter exposes the detail command.
	// Until then, existing detail data stays reachable in the default result.
	DetailAvailable bool
}

// TextBlock sanitizes a sourced host supplement with the same width budget.
func TextBlock(text string, view Presentation) string {
	return wrapPresentation(safePresentationText(text), view.Width)
}

// LayoutText only reflows values already sanitized at their typed/source boundary.
func LayoutText(text string, view Presentation) string {
	if text == "" {
		return ""
	}
	return wrapPresentation(text, view.Width)
}

func resultTitle(result *controlpb.CommandResult, options command.Options) string {
	switch result.Payload.(type) {
	case *controlpb.CommandResult_NetworkSnapshot:
		return "Android Networks"
	case *controlpb.CommandResult_PermissionSnapshot:
		return "Permissions"
	case *controlpb.CommandResult_WifiStatus:
		return "Wi-Fi status"
	case *controlpb.CommandResult_IpStatus:
		return "IP status"
	case *controlpb.CommandResult_WifiScan:
		return "Wi-Fi scan"
	case *controlpb.CommandResult_WifiScanDetail:
		return "Wi-Fi scan detail"
	case *controlpb.CommandResult_WifiCapabilities:
		return "Wi-Fi capabilities"
	case *controlpb.CommandResult_WifiDiagnostics:
		if options.WifiRenderMode == command.WifiRenderModeFull {
			return "Wi-Fi status"
		}
		if options.WifiRenderMode == command.WifiRenderModeEHT {
			return "Wi-Fi EHT"
		}
		return "Wi-Fi diagnostics"
	case *controlpb.CommandResult_Ping:
		return "Ping"
	case *controlpb.CommandResult_ResolveDns:
		return "DNS"
	case *controlpb.CommandResult_HttpCheck:
		return "HTTP"
	case *controlpb.CommandResult_Traceroute:
		return "Traceroute"
	case *controlpb.CommandResult_PathMtu:
		return "Path MTU"
	case *controlpb.CommandResult_Wget:
		return "Download"
	case *controlpb.CommandResult_GlobalIp:
		return "Global IP"
	default:
		return "Command"
	}
}

func wrapPresentation(text string, width int) string {
	var b strings.Builder
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	for i := 0; i < len(lines); {
		if _, _, field := strings.Cut(lines[i], "\t"); field {
			end, labelWidth := i, 0
			for end < len(lines) {
				label, _, ok := strings.Cut(lines[end], "\t")
				if !ok {
					break
				}
				labelWidth = max(labelWidth, displayWidth(strings.TrimRight(label, " ")))
				end++
			}
			indent := 4
			for _, line := range lines[i:end] {
				label, value, _ := strings.Cut(line, "\t")
				label = strings.TrimRight(label, " ")
				continuation := label == "" && strings.HasPrefix(value, "\t")
				if continuation {
					value = strings.TrimPrefix(value, "\t")
				} else {
					indent = len(label) - len(strings.TrimLeft(label, " ")) + 4
				}
				prefix := "  " + padDisplayEnd(label+":", labelWidth+1) + " "
				if width > 0 && displayWidth(prefix)+8 > width {
					if !continuation {
						writeFieldLabel(&b, "  "+label, width)
					}
					prefix = strings.Repeat(" ", min(indent, max(0, width-2)))
				} else if continuation {
					prefix = strings.Repeat(" ", displayWidth(prefix))
				}
				if width > 0 {
					value = ansi.Hardwrap(value, max(1, width-displayWidth(prefix)), true)
				}
				for part := range strings.SplitSeq(value, "\n") {
					b.WriteString(prefix + part + "\n")
					prefix = strings.Repeat(" ", displayWidth(prefix))
				}
			}
			i = end
			continue
		}
		line := lines[i]
		line = cleanDisplayCell(line)
		if width > 0 {
			indent := line[:len(line)-len(strings.TrimLeft(line, " "))]
			if len(indent) > 0 && len(indent) < width {
				wrapped := ansi.Hardwrap(line[len(indent):], width-len(indent), true)
				line = indent + strings.ReplaceAll(wrapped, "\n", "\n"+indent)
			} else {
				line = ansi.Hardwrap(line, width, true)
			}
		}
		b.WriteString(line)
		b.WriteByte('\n')
		i++
	}
	return b.String()
}

func writeRecord(b *strings.Builder, label string, value any) {
	// Tabs are owned field boundaries, never guessed from raw text or URL colons.
	prefix := strings.TrimRight(cleanDisplayCell(label), " ") + "\t"
	for line := range strings.SplitSeq(fmt.Sprint(value), "\n") {
		fmt.Fprintf(b, "%s%s\n", prefix, cleanDisplayCell(line))
		// A second owned tab distinguishes a value continuation from an empty key.
		prefix = "\t\t"
	}
}

// writeNested indents exactly the content scoped by its caller.
func writeNested(b *strings.Builder, text string) {
	text = strings.TrimSuffix(text, "\n")
	if text == "" {
		return
	}
	for line := range strings.SplitAfterSeq(text, "\n") {
		line = strings.TrimSuffix(line, "\n")
		if line == "" {
			b.WriteByte('\n')
			continue
		}
		if !strings.HasPrefix(line, "\t\t") {
			b.WriteString("  ")
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
}

func writeFieldLabel(b *strings.Builder, label string, width int) {
	wrapped := wrapPresentation(label, max(1, width-1))
	b.WriteString(strings.TrimSuffix(wrapped, "\n") + ":\n")
}

// Fields formats explicitly typed local records without parsing arbitrary text.
func Fields(rows [][2]string, view Presentation) string {
	var b strings.Builder
	for _, row := range rows {
		writeRecord(&b, row[0], row[1])
	}
	return wrapPresentation(b.String(), view.Width)
}

// SourceFields sanitizes free-form host facts before encoding owned logical fields.
// Compose these records before calling LayoutText; raw text is never parsed as fields.
func SourceFields(rows [][2]string) string {
	var b strings.Builder
	for _, row := range rows {
		writeRecord(&b, row[0], safePresentationField(row[0], row[1]))
	}
	return b.String()
}

// Table shares display-cell spacing and narrow record layout with sourced tables.
func Table(headers []string, rows [][]string, view Presentation) string {
	columns := make([]displayTableColumn, len(headers))
	for i, header := range headers {
		columns[i].header = cleanDisplayCell(header)
	}
	safe := make([][]string, len(rows))
	for i, row := range rows {
		safe[i] = make([]string, len(row))
		for j, value := range row {
			label := ""
			if j < len(headers) {
				label = headers[j]
			}
			safe[i][j] = safePresentationField(label, value)
		}
	}
	var b strings.Builder
	writeDisplayTable(&b, columns, safe, view.Width)
	return LayoutText(b.String(), view)
}

// Exact dictionary only: unknown modes/enum values retain their original spelling.
func compactSecuritySet(values []string) string {
	if len(values) == 0 {
		return "?"
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		switch value {
		case "wpa2_psk":
			value = "psk"
		case "wpa3_sae":
			value = "sae"
		case "eap_wpa3_enterprise":
			value = "wpa3-eap"
		case "wpa2_wpa3_personal", "wpa2_psk+wpa3_sae":
			value = "psk+sae"
		}
		out = append(out, value)
	}
	return strings.Join(out, "+")
}

func compactBand(value string) string {
	switch strings.ToLower(value) {
	case "2.4ghz":
		return "2.4G"
	case "5ghz":
		return "5G"
	case "6ghz":
		return "6G"
	case "60ghz":
		return "60G"
	case "", "unknown":
		return "?"
	default:
		return value
	}
}

func compactPHY(value string) string {
	switch value {
	case "802.11a", "802.11b", "802.11g", "802.11n", "802.11ac", "802.11ax", "802.11be":
		return strings.TrimPrefix(value, "802.11")
	case "":
		return "?"
	default:
		return value
	}
}

func tableFits(columns []displayTableColumn, rows [][]string, width int) bool {
	if width <= 0 {
		return false
	}
	var b strings.Builder
	writeDisplayTable(&b, columns, rows)
	for line := range strings.SplitSeq(b.String(), "\n") {
		if displayWidth(line) > width {
			return false
		}
	}
	return true
}

func renderAdaptiveScan(b *strings.Builder, scan *controlpb.WifiScan, options command.Options, view Presentation) {
	total := len(scan.GetResults())
	fields := diagnosticFieldMap(scan.GetFields())
	requested := "? (request metadata unavailable)"
	if view.RequestedFresh != nil {
		if *view.RequestedFresh {
			requested = "fresh"
		} else {
			requested = "cached"
		}
	}
	writeRecord(b, "Requested", requested)
	writeRecord(b, "Data source", empty(fields["scan_source"], "? (source metadata unavailable)"))
	writeRecord(b, "Updated", empty(fields["fresh_scan_results_updated"], "?"))
	writeRecord(b, "Band", empty(fields["requested_band"], "?"))
	writeRecord(b, "Update elapsed", empty(fields["fresh_scan_elapsed_ms"], "?")+"ms")
	groups := scanResultGroups(scan.GetResults(), options.WifiScanMLO)
	shown := 0
	for _, group := range groups {
		shown += len(group.results)
	}
	writeRecord(b, "APs", fmt.Sprintf("shown=%d total=%s", shown, empty(fields["scan_result_total_count"], fmt.Sprint(total))))
	writeRecord(b, "Connections at collection", scanConnectionsSummary(scan.GetConnections()))
	if options.WifiScanMLO {
		writeRecord(b, "Filter", "mlo; shown counts filtered APs")
	}
	columns := []displayTableColumn{{header: "SSID"}, {header: "BSSID"}, {header: "dBm", numeric: true}, {header: "BAND"}, {header: "CH", numeric: true}, {header: "BW", numeric: true}, {header: "PHY"}, {header: "SEC"}}
	if options.WifiScanBrief {
		columns = append(columns, displayTableColumn{header: "AGE(s)"})
	} else {
		columns = append(columns, displayTableColumn{header: "VENDOR"})
	}
	var rows [][]string
	for _, group := range groups {
		for _, ap := range group.results {
			row := adaptiveScanRow(ap)
			if options.WifiScanBrief {
				row = append(row, scanAgeSeconds(ap))
			} else {
				row = append(row, oui.Vendor(ap.GetBssid()))
			}
			rows = append(rows, row)
		}
	}
	renderScanRows(b, groups, columns, rows, scan.GetConnections(), options, view)
	if !options.WifiScanBrief {
		fieldsWritten := false
		for _, field := range scan.GetFields() {
			if !slices.Contains([]string{"scan_source", "fresh_scan_results_updated", "requested_band", "fresh_scan_elapsed_ms", "scan_result_total_count", "scan_result_count"}, field.GetKey()) {
				if !fieldsWritten {
					writeSection(b, "Scan fields")
					fieldsWritten = true
				}
				writeRecord(b, field.GetKey(), field.GetValue())
			}
		}
	}
	renderErrors(b, scan.GetErrors())
	if options.WifiScanBrief {
		writeRecord(b, "Full", "show wifi scan (new passive observation)")
	}
}

func renderScanRows(b *strings.Builder, groups []scanResultGroup, columns []displayTableColumn, rows [][]string, connections *controlpb.WifiScanConnections, options command.Options, view Presentation) {
	// Row numbers connect supplemental fields without repeating SSID/BSSID values.
	indexedColumns := append([]displayTableColumn{{header: "AP", numeric: true}}, columns...)
	indexedRows := make([][]string, 0, len(rows))
	for i, row := range rows {
		indexedRows = append(indexedRows, append([]string{fmt.Sprint(i + 1)}, row...))
	}
	table := tableFits(indexedColumns, indexedRows, view.Width)
	if table {
		writeDisplayTable(b, indexedColumns, indexedRows)
	}
	if !options.WifiScanBrief || options.WifiScanMLO {
		writeBlankLine(b)
		writeRecord(b, "AP display", fmt.Sprintf("shown=%d total=%d", len(rows), len(rows)))
		writeRecord(b, "Marks", "[*]=connected at collection; [+]=EHT observed; [ ]=neither mark; absence is not proof of non-support")
	}
	index := 0
	for _, group := range groups {
		if options.WifiScanBrief && !options.WifiScanMLO {
			if table {
				continue
			}
			groupColumns := append([]displayTableColumn{{header: "AP", numeric: true}}, columns[1:]...)
			var groupRows [][]string
			for i := range group.results {
				groupRows = append(groupRows, append([]string{fmt.Sprint(index + i + 1)}, rows[index+i][1:]...))
			}
			if tableFits(groupColumns, groupRows, view.Width) {
				writeRecord(b, "SSID", group.ssid)
				writeDisplayTable(b, groupColumns, groupRows)
				index += len(group.results)
				continue
			}
		}
		for _, ap := range group.results {
			writeSection(b, fmt.Sprintf("[%s] AP %d", scanAPMark(ap, connections), index+1))
			var details strings.Builder
			detail := &details
			if table {
				writeRecord(detail, "SSID", scanDisplaySSID(ap))
				writeRecord(detail, "BSSID", empty(ap.GetBssid(), "?"))
			}
			if !table {
				for col, column := range columns {
					writeRecord(detail, column.header, rows[index][col])
				}
			}
			if !options.WifiScanBrief {
				writeRecord(detail, "EHT", empty(scanEHTEvidence(ap), "not observed"))
				writeRecord(detail, "Observation age at collection", scanObservationAge(ap))
				writeRecord(detail, "BSSID vendor", oui.Vendor(ap.GetBssid()))
			}
			if !options.WifiScanBrief || options.WifiScanMLO {
				writeRecord(detail, "MLD", empty(wifiMLOScanMLDMAC(ap), "? (metadata availability uncollected)"))
				renderMLOLinks(detail, "Affiliated", ap.GetAffiliatedMloLinks(), true)
			}
			if !options.WifiScanBrief {
				writeRecord(detail, "Frequency MHz", ap.GetFrequencyMhz())
				writeRecord(detail, "Centers MHz", fmt.Sprintf("%d/%d", ap.GetCenterFreq0Mhz(), ap.GetCenterFreq1Mhz()))
				writeRecord(detail, "FLAGS", empty(strings.Join(scanConnectionCapabilityFlags(ap), ","), "? (availability uncollected)"))
				renderWifiSecurityDetails(detail, ap.GetSecurityDetails())
				renderFullScanEvidence(detail, ap)
			}
			writeNested(b, details.String())
			index++
		}
	}
	writeBlankLine(b)
	writeRecord(b, "Units", "RSSI=dBm; BW=MHz; band G=GHz; PHY=802.11")
	if options.WifiScanBrief {
		writeRecord(b, "AGE(s)", "seconds at collection; ?=timestamp unavailable; wpa3-eap=eap_wpa3_enterprise")
	}
}

func adaptiveScanRow(ap *controlpb.WifiScanResult) []string {
	return []string{scanDisplaySSID(ap), empty(ap.GetBssid(), "?"), fmt.Sprint(ap.GetRssiDbm()), compactBand(empty(ap.GetBand(), wifiBandFromFrequency(ap.GetFrequencyMhz()))), wifiChannelFromFrequency(ap.GetFrequencyMhz()), empty(strings.TrimSuffix(ap.GetChannelWidth(), "MHz"), "?"), compactPHY(ap.GetWifiStandard()), compactSecuritySet(ap.GetSecurityTypes())}
}

func renderL2(b *strings.Builder, status *controlpb.WifiStatus, view Presentation) {
	if status == nil {
		writeRecord(b, "Note", "? (Wi-Fi payload missing)")
		return
	}
	radio := "off"
	if status.GetEnabled() {
		radio = "on"
	}
	writeRecord(b, "Radio", observedValue(status.GetObservationFields(), "radio", radio))
	writeRecord(b, "Wi-Fi state", status.GetState())
	writeRecord(b, "Active Network", empty(status.GetActiveNetwork(), "none"))
	writeRecord(b, "Wi-Fi Networks", status.GetWifiNetworkCount())
	conn := status.GetConnection()
	if conn == nil {
		link := observedValue(status.GetObservationFields(), "connection", "")
		if link == "none" {
			link = "disconnected"
			writeRecord(b, "SSID/BSSID", "none")
		}
		writeRecord(b, "Link", link)
	} else {
		writeRecord(b, "Link", observedValue(status.GetObservationFields(), "connection", empty(conn.GetDetailedState(), empty(conn.GetSupplicantState(), "?"))))
		ssid, bssid := conn.GetSsid(), conn.GetBssid()
		if ssid == "" || strings.EqualFold(ssid, "<unknown ssid>") {
			ssid = "? (identity unavailable)"
		}
		if !wifiMLOKnownBSSID(bssid) {
			bssid = "?"
		}
		if strings.EqualFold(conn.GetDetailedState(), "DISCONNECTED") {
			ssid, bssid = "none", "none"
		}
		writeRecord(b, "SSID", observedValue(conn.GetObservationFields(), "identity", ssid))
		writeRecord(b, "BSSID", observedValue(conn.GetObservationFields(), "identity", bssid))
		writeRecord(b, "BSSID vendor", observedValue(conn.GetObservationFields(), "identity", oui.Vendor(bssid)))
		writeRecord(b, "Band", wifiBandFromFrequency(conn.GetFrequencyMhz()))
		writeRecord(b, "CH/BW", wifiChannelFromFrequency(conn.GetFrequencyMhz())+"/"+empty(wifiChannelWidth(conn), "?"))
		writeRecord(b, "PHY", empty(conn.GetWifiStandard(), "?"))
		writeRecord(b, "RSSI", observedValue(conn.GetObservationFields(), "rssi", fmt.Sprintf("%ddBm", conn.GetRssiDbm())))
		writeRecord(b, "Security", empty(conn.GetSecurityType(), "?"))
		if conn.GetSecurityDetails().GetRsnPresent() {
			writeRecord(b, "PMF", fmt.Sprintf("capable=%t required=%t", conn.GetSecurityDetails().GetPmfCapable(), conn.GetSecurityDetails().GetPmfRequired()))
		}
		writeRecord(b, "Tx link rate", observedValue(conn.GetObservationFields(), "tx_link_speed_mbps", fmt.Sprintf("%dMbps", conn.GetTxLinkSpeedMbps())))
		writeRecord(b, "Rx link rate", observedValue(conn.GetObservationFields(), "rx_link_speed_mbps", fmt.Sprintf("%dMbps", conn.GetRxLinkSpeedMbps())))
		writeRecord(b, "MLD", observedValue(conn.GetObservationFields(), "ap_mld_mac_address", conn.GetApMldMacAddress()))
		writeRecord(b, "Associated", observedValue(conn.GetObservationFields(), "associated_mlo_links", fmt.Sprint(len(conn.GetAssociatedMloLinks()))))
		writeRecord(b, "Affiliated", observedValue(conn.GetObservationFields(), "affiliated_mlo_links", fmt.Sprint(len(conn.GetAffiliatedMloLinks()))))
		if view.Detail || !view.DetailAvailable {
			var details strings.Builder
			renderConnectionExtra(&details, conn)
			writeNested(b, details.String())
		}
	}
	for _, permission := range status.GetPermissions() {
		writeRecord(b, "Permission", permission)
	}
	if view.Detail || !view.DetailAvailable {
		renderDiagnosticFields(b, status.GetObservationFields())
	}
}

// The overview already owns identity, radio, rates and MLO counts.
func renderConnectionExtra(b *strings.Builder, conn *controlpb.WifiConnection) {
	writeSection(b, "Connection detail")
	writeRecord(b, "Network configuration ID", conn.GetNetworkId())
	writeRecord(b, "Signal level", wifiSignalLevel(conn))
	writeRecord(b, "Frequency MHz", conn.GetFrequencyMhz())
	writeRecord(b, "Link speed Mbps", conn.GetLinkSpeedMbps())
	writeRecord(b, "STA MAC", conn.GetMacAddress())
	writeRecord(b, "Supplicant state", conn.GetSupplicantState())
	writeRecord(b, "Hidden SSID", conn.GetHiddenSsid())
	writeRecord(b, "Restricted", conn.GetRestricted())
	writeRecord(b, "Subscription ID", conn.GetSubscriptionId())
	writeRecord(b, "Applicable redactions", conn.GetApplicableRedactions())
	writeRecord(b, "AP link ID", observedValue(conn.GetObservationFields(), "ap_mlo_link_id", fmt.Sprint(conn.GetApMloLinkId())))
	capabilityRows := slices.DeleteFunc(wifiAPCapabilityRows(conn), func(row kvRow) bool { return row.label == "hidden" || row.label == "restricted" })
	writeKVSection(b, "AP Capabilities", capabilityRows...)
	renderWifiSecurityDetails(b, conn.GetSecurityDetails())
	renderWifiConnectionDetailedCapabilities(b, conn)
	renderMLOLinks(b, "Associated links", conn.GetAssociatedMloLinks(), true)
	renderMLOLinks(b, "Affiliated links", conn.GetAffiliatedMloLinks(), true)
	writeRecord(b, "WifiInfo IPv4 (separate observation)", conn.GetIpv4Address())
	renderInformationElements(b, conn.GetInformationElements())
	renderDiagnosticFields(b, conn.GetObservationFields())
}

func renderWifiSnapshot(b *strings.Builder, status *controlpb.WifiStatus, view Presentation) {
	if status == nil {
		writeRecord(b, "Observation", "? (snapshot unavailable)")
		return
	}
	renderL2(b, status, view)
	if status.GetIpStatus() != nil {
		writeSection(b, "Related IP snapshot")
		renderIPView(b, status.GetIpStatus(), view)
	}
}

func renderDeviceCapabilities(b *strings.Builder, capabilities *controlpb.WifiCapabilities, view Presentation) {
	if capabilities == nil {
		writeRecord(b, "Note", "? (device capability payload missing)")
		return
	}
	writeRecord(b, "Scope", "device")
	groups := []struct {
		name                          string
		known, supported, unsupported []string
	}{
		{"Band", []string{"2.4ghz", "5ghz", "6ghz", "60ghz"}, capabilities.GetSupportedBands(), capabilities.GetUnsupportedBands()},
		{"PHY", []string{"802.11n", "802.11ac", "802.11ax", "802.11be"}, capabilities.GetSupportedStandards(), capabilities.GetUnsupportedStandards()},
		{"Security", []string{"wpa2_psk", "wpa3_sae", "wpa2_wpa3_personal", "owe"}, capabilities.GetSupportedSecurityModes(), capabilities.GetUnsupportedSecurityModes()},
		{"Features", []string{"mlo"}, capabilities.GetSupportedFeatures(), capabilities.GetUnsupportedFeatures()},
	}
	for _, group := range groups {
		label := func(value string) string {
			switch group.name {
			case "Band":
				return compactBand(value)
			case "PHY":
				return compactPHY(value)
			case "Security":
				if value == "wpa2_psk" || value == "psk" {
					return "WPA2-PSK"
				}
				return compactSecuritySet([]string{value})
			default:
				return value
			}
		}
		for i := range group.known {
			group.known[i] = label(group.known[i])
		}
		group.supported = slices.Clone(group.supported)
		group.unsupported = slices.Clone(group.unsupported)
		for i := range group.supported {
			group.supported[i] = label(group.supported[i])
		}
		for i := range group.unsupported {
			group.unsupported[i] = label(group.unsupported[i])
		}
		values := slices.Clone(group.known)
		for _, value := range append(slices.Clone(group.supported), group.unsupported...) {
			if !slices.Contains(values, value) {
				values = append(values, value)
			}
		}
		var rows [][]string
		for _, value := range values {
			support := "?"
			if slices.Contains(group.supported, value) {
				support = "yes"
			}
			if slices.Contains(group.unsupported, value) {
				support = "no"
			}
			if slices.Contains(group.supported, value) && slices.Contains(group.unsupported, value) {
				support = "? (conflicting lists)"
			}
			rows = append(rows, []string{value, support})
		}
		columns := []displayTableColumn{{header: group.name}, {header: "Support"}}
		if tableFits(columns, rows, view.Width) {
			writeDisplayTable(b, columns, rows)
		} else {
			writeSection(b, group.name)
			for _, row := range rows {
				writeRecord(b, row[0], row[1])
			}
		}
	}
	if view.Detail || !view.DetailAvailable {
		renderDiagnosticFields(b, capabilities.GetFields())
	}
	renderErrors(b, capabilities.GetErrors())
}

func renderDiagnosticBundle(b *strings.Builder, diagnostics *controlpb.WifiDiagnostics, options command.Options, view Presentation) {
	if diagnostics == nil {
		writeRecord(b, "Note", "? (diagnostics payload unavailable)")
		return
	}
	childView := view
	if childView.Width > 0 {
		childView.Width = max(1, childView.Width-2)
	}
	writeSection(b, "Summary")
	var summary strings.Builder
	writeRecord(&summary, "Constituent status", "? (acquisition-result contract unavailable); not an atomic snapshot")
	writeNested(b, summary.String())
	writeSection(b, "Wi-Fi")
	var wifi strings.Builder
	renderL2(&wifi, diagnostics.GetStatus(), childView)
	writeNested(b, wifi.String())
	writeSection(b, "IP")
	if ip := diagnostics.GetStatus().GetIpStatus(); ip != nil {
		var network strings.Builder
		renderIPView(&network, ip, childView)
		writeNested(b, network.String())
	} else {
		var missing strings.Builder
		writeRecord(&missing, "Note", "? (IP payload unavailable)")
		writeNested(b, missing.String())
	}
	writeSection(b, "Scan summary")
	scan := diagnostics.GetScan()
	if scan != nil {
		cached := false
		scanView := childView
		scanView.RequestedFresh = &cached
		var scanOutput strings.Builder
		renderAdaptiveScan(&scanOutput, scan, options, scanView)
		writeNested(b, scanOutput.String())
	} else {
		var missing strings.Builder
		writeRecord(&missing, "Note", "? (scan payload unavailable)")
		writeNested(b, missing.String())
	}
	writeSection(b, "Device")
	var device strings.Builder
	renderDeviceCapabilities(&device, diagnostics.GetCapabilities(), childView)
	writeNested(b, device.String())
	if view.Detail || !view.DetailAvailable {
		for i, network := range diagnostics.GetNetworks() {
			writeSection(b, fmt.Sprintf("Network %d", i+1))
			var body strings.Builder
			writeRecord(&body, "Active/default at bundle enumeration", network.GetActive())
			renderIPView(&body, network.GetIpStatus(), childView)
			writeNested(b, body.String())
		}
	}
	current := wifiMLOCurrentConnection(diagnostics.GetStatus())
	candidates := wifiMLOScanCandidates(diagnostics.GetScan().GetResults())
	writeSection(b, "EHT/MLO interpretation (references the observations above)")
	var interpretation strings.Builder
	renderWifiMLOCurrentRelation(&interpretation, current, diagnostics.GetScan(), candidates)
	renderWifiMLOConnectedEHT(&interpretation, current)
	renderWifiMLOScanEHT(&interpretation, wifiMLOGroups(candidates))
	renderWifiMLODeviceReadiness(&interpretation, diagnostics.GetCapabilities())
	renderWifiMLODiagnostics(&interpretation, diagnostics.GetStatus(), diagnostics.GetCapabilities(), diagnostics.GetScan(), current, candidates)
	writeNested(b, interpretation.String())
}

func renderCurrentMLO(b *strings.Builder, diagnostics *controlpb.WifiDiagnostics, options command.Options, view Presentation) {
	conn := diagnostics.GetStatus().GetConnection()
	if conn == nil {
		writeRecord(b, "Current", "? (current Wi-Fi payload unavailable)")
		return
	}
	writeRecord(b, "SSID", empty(conn.GetSsid(), "?"))
	writeRecord(b, "BSSID", empty(conn.GetBssid(), "?"))
	writeRecord(b, "BSSID vendor", observedValue(conn.GetObservationFields(), "identity", oui.Vendor(conn.GetBssid())))
	writeRecord(b, "MLD", observedValue(conn.GetObservationFields(), "ap_mld_mac_address", wifiMLOConnectionMLDMAC(conn)))
	writeRecord(b, "PHY", empty(conn.GetWifiStandard(), "?"))
	writeRecord(b, "Associated", observedValue(conn.GetObservationFields(), "associated_mlo_links", fmt.Sprint(len(conn.GetAssociatedMloLinks()))))
	writeRecord(b, "Affiliated", observedValue(conn.GetObservationFields(), "affiliated_mlo_links", fmt.Sprint(len(conn.GetAffiliatedMloLinks()))))
	for _, section := range []struct {
		title string
		links []*controlpb.MloLinkInfo
	}{{"Associated", conn.GetAssociatedMloLinks()}, {"Affiliated", conn.GetAffiliatedMloLinks()}} {
		writeSection(b, section.title)
		for _, link := range section.links {
			writeRecord(b, "AP MAC", observedValue(link.GetObservationFields(), "identity", link.GetApMacAddress()))
			writeRecord(b, "AP MAC vendor", observedValue(link.GetObservationFields(), "identity", oui.Vendor(link.GetApMacAddress())))
			writeRecord(b, "State", observedValue(link.GetObservationFields(), "identity", link.GetState()))
			writeRecord(b, "Radio", observedValue(link.GetObservationFields(), "identity", fmt.Sprintf("%s CH=%d", link.GetBand(), link.GetChannel())))
			writeRecord(b, "RSSI", observedValue(link.GetObservationFields(), "rates", fmt.Sprintf("%ddBm", link.GetRssiDbm())))
			writeRecord(b, "Tx/Rx", observedValue(link.GetObservationFields(), "rates", fmt.Sprintf("%d/%dMbps", link.GetTxLinkSpeedMbps(), link.GetRxLinkSpeedMbps())))
		}
	}
	fields := diagnosticFieldMap(diagnostics.GetScan().GetFields())
	writeRecord(b, "Scan source", empty(fields["scan_source"], "?"))
	writeRecord(b, "Scan age", scanAgeSummary(diagnostics.GetScan()))
	writeRecord(b, "Coverage", "? (comparable relation metadata required)")
	renderErrors(b, diagnostics.GetScan().GetErrors())
	writeRecord(b, "More", "show wifi eht detail")
}
