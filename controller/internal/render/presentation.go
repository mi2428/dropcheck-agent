package render

import (
	"fmt"
	"slices"
	"strings"

	"dropcheck/controller/internal/command"
	"dropcheck/controller/internal/controlpb"
	"github.com/charmbracelet/x/ansi"
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

func resultTitle(result *controlpb.CommandResult, options command.Options) string {
	switch result.Payload.(type) {
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
	for line := range strings.SplitSeq(strings.TrimSuffix(text, "\n"), "\n") {
		line = cleanDisplayCell(line)
		if width > 0 {
			line = ansi.Hardwrap(line, width, true)
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

func writeRecord(b *strings.Builder, label string, value any) {
	fmt.Fprintf(b, "%s: %s\n", label, cleanDisplayCell(fmt.Sprint(value)))
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
	writeDisplayTable(&b, columns, rows, " ")
	for line := range strings.SplitSeq(b.String(), "\n") {
		if displayWidth(line) > width {
			return false
		}
	}
	return true
}

func renderAdaptiveScan(b *strings.Builder, scan *controlpb.WifiScan, options command.Options, view Presentation) {
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
	writeRecord(b, "APs", fmt.Sprintf("shown=%d total=%s", shown, empty(fields["scan_result_total_count"], fmt.Sprint(len(scan.GetResults())))))
	if options.WifiScanMLO {
		writeRecord(b, "Filter", "mlo; shown counts filtered APs")
	}
	columns := []displayTableColumn{{header: "SSID"}, {header: "BSSID"}, {header: "dBm", numeric: true}, {header: "BAND"}, {header: "CH", numeric: true}, {header: "BW", numeric: true}, {header: "PHY"}, {header: "SEC"}}
	var rows [][]string
	for _, group := range groups {
		for _, ap := range group.results {
			rows = append(rows, adaptiveScanRow(ap))
		}
	}
	if tableFits(columns, rows, view.Width) {
		writeDisplayTable(b, columns, rows, " ")
	} else {
		for _, group := range groups {
			writeRecord(b, "SSID", group.ssid)
			var groupRows [][]string
			for _, ap := range group.results {
				groupRows = append(groupRows, adaptiveScanRow(ap)[1:])
			}
			if tableFits(columns[1:], groupRows, view.Width) {
				writeDisplayTable(b, columns[1:], groupRows, " ")
			} else {
				short := columns[2:]
				for _, ap := range group.results {
					row := adaptiveScanRow(ap)
					if tableFits(short, [][]string{row[2:]}, view.Width) {
						writeDisplayTable(b, short, [][]string{row[2:]}, " ")
						writeRecord(b, "BSSID", row[1])
					} else {
						for i, column := range columns[1:] {
							writeRecord(b, column.header, row[i+1])
						}
					}
				}
			}
		}
	}
	writeRecord(b, "Units", "RSSI=dBm; BW=MHz; band G=GHz; PHY=802.11; Tx/Rx link rates=Mbps")
	for _, group := range groups {
		for _, ap := range group.results {
			writeRecord(b, "BSSID", empty(ap.GetBssid(), "?"))
			writeRecord(b, "Observation age at collection", scanObservationAge(ap))
			writeRecord(b, "FLAGS", empty(strings.Join(scanConnectionCapabilityFlags(ap), ","), "? (availability uncollected)"))
			writeRecord(b, "AP_MLD", empty(wifiMLOScanMLDMAC(ap), "? (metadata availability uncollected)"))
			writeRecord(b, "SEC_FEATURES", scanSecurityFeatureCell(ap.GetSecurityDetails()))
			for _, link := range ap.GetAffiliatedMloLinks() {
				writeRecord(b, "Affiliated AP_MAC", empty(link.GetApMacAddress(), "?"))
				writeRecord(b, "Affiliated STA_MAC", empty(link.GetStaMacAddress(), "?"))
				writeRecord(b, "State/Band/CH", fmt.Sprintf("%s %s %d", link.GetState(), link.GetBand(), link.GetChannel()))
				writeRecord(b, "Rates", fmt.Sprintf("Tx/Rx=%d/%dMbps", link.GetTxLinkSpeedMbps(), link.GetRxLinkSpeedMbps()))
			}
		}
	}
	if !options.WifiScanBrief {
		renderScanSecurityDetails(b, scan.GetResults())
	}
	renderErrors(b, scan.GetErrors())
	writeRecord(b, "More", "show wifi scan detail BSSID (new observation)")
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
		writeRecord(b, "Band", wifiBandFromFrequency(conn.GetFrequencyMhz()))
		writeRecord(b, "CH/BW", wifiChannelFromFrequency(conn.GetFrequencyMhz())+"/"+empty(wifiChannelWidth(conn), "?"))
		writeRecord(b, "PHY", empty(conn.GetWifiStandard(), "?"))
		writeRecord(b, "RSSI", observedValue(conn.GetObservationFields(), "rssi", fmt.Sprintf("%ddBm", conn.GetRssiDbm())))
		writeRecord(b, "Security", empty(conn.GetSecurityType(), "?"))
		writeRecord(b, "Tx link rate", observedValue(conn.GetObservationFields(), "tx_link_speed_mbps", fmt.Sprintf("%dMbps", conn.GetTxLinkSpeedMbps())))
		writeRecord(b, "Rx link rate", observedValue(conn.GetObservationFields(), "rx_link_speed_mbps", fmt.Sprintf("%dMbps", conn.GetRxLinkSpeedMbps())))
		writeRecord(b, "MLD", observedValue(conn.GetObservationFields(), "ap_mld_mac_address", conn.GetApMldMacAddress()))
		writeRecord(b, "Associated", observedValue(conn.GetObservationFields(), "associated_mlo_links", fmt.Sprint(len(conn.GetAssociatedMloLinks()))))
		writeRecord(b, "Affiliated", observedValue(conn.GetObservationFields(), "affiliated_mlo_links", fmt.Sprint(len(conn.GetAffiliatedMloLinks()))))
		if view.Detail || !view.DetailAvailable {
			renderWifiConnection(b, conn)
		}
		if status.GetIpStatus() == nil && conn.GetIpv4Address() != "" {
			writeKVSection(b, "Related IP snapshot", wifiConnectionNetworkRows(conn)...)
		}
	}
	for _, permission := range status.GetPermissions() {
		if !strings.HasSuffix(permission, "=granted") {
			writeRecord(b, "Note", permission)
		}
	}
	writeRecord(b, "IP", "show ip status")
	if view.DetailAvailable && !view.Detail {
		writeRecord(b, "More", "show wifi status detail")
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
			writeDisplayTable(b, columns, rows, " ")
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
	writeSection(b, "Summary")
	writeRecord(b, "Constituent status", "? (acquisition-result contract unavailable); not an atomic snapshot")
	for _, reason := range diagnostics.GetScan().GetErrors() {
		writeRecord(b, "Scan note", reason)
	}
	for _, reason := range diagnostics.GetCapabilities().GetErrors() {
		writeRecord(b, "Device note", reason)
	}
	writeSection(b, "Wi-Fi")
	renderL2(b, diagnostics.GetStatus(), view)
	writeSection(b, "IP")
	if ip := diagnostics.GetStatus().GetIpStatus(); ip != nil {
		renderIPView(b, ip, view)
	} else {
		writeRecord(b, "Note", "? (IP payload unavailable)")
	}
	writeSection(b, "Scan summary")
	scan := diagnostics.GetScan()
	if scan != nil {
		fields := diagnosticFieldMap(scan.GetFields())
		writeRecord(b, "Data source", empty(fields["scan_source"], "?"))
		writeRecord(b, "Updated", empty(fields["fresh_scan_results_updated"], "?"))
		writeRecord(b, "APs", len(scan.GetResults()))
		writeRecord(b, "Age", scanAgeSummary(scan))
	} else {
		writeRecord(b, "Note", "? (scan payload unavailable)")
	}
	writeSection(b, "Device")
	renderDeviceCapabilities(b, diagnostics.GetCapabilities(), view)
	if view.Detail || !view.DetailAvailable {
		for _, network := range diagnostics.GetNetworks() {
			writeSection(b, "Network")
			writeRecord(b, "Network", network.GetNetworkId())
			renderIPStatus(b, network.GetIpStatus())
		}
	}
}

func renderCurrentMLO(b *strings.Builder, diagnostics *controlpb.WifiDiagnostics, options command.Options, view Presentation) {
	filter := wifiMLOFilter{ssid: options.WifiEHTSSID, bssid: options.WifiEHTBSSID}
	conn := filter.connection(diagnostics.GetStatus().GetConnection())
	if conn == nil {
		writeRecord(b, "Current", "? (current Wi-Fi payload unavailable)")
		return
	}
	writeRecord(b, "SSID", empty(conn.GetSsid(), "?"))
	writeRecord(b, "BSSID", empty(conn.GetBssid(), "?"))
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
