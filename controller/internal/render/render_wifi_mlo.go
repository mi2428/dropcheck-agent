package render

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"dropcheck/controller/internal/controlpb"
)

type wifiMLOGroup struct {
	results    []*controlpb.WifiScanResult
	displayMLD string
	bestRSSI   int32
	bands      []string
	security   []string
	standards  []string
}

type wifiMLOTableColumn struct {
	header   string
	maxWidth int
}

func renderWifiMLO(b *strings.Builder, diagnostics *controlpb.WifiDiagnostics) {
	if diagnostics == nil {
		return
	}
	status := diagnostics.GetStatus()
	current := wifiMLOCurrentConnection(status)
	scan := diagnostics.GetScan()
	candidates := wifiMLOScanCandidates(scan.GetResults())
	groups := wifiMLOGroups(candidates)

	renderWifiMLOCurrentRelation(b, current, candidates)
	renderWifiMLOConnected(b, current)
	renderWifiMLOConnectedSecurity(b, current)
	renderWifiMLOConnectedHE6GHzDetails(b, current)
	renderWifiMLOConnectedEHT(b, current)
	renderWifiMLOConnectedEHTDetails(b, current)
	renderWifiMLONetworks(b, diagnostics.GetNetworks())
	renderWifiMLOScanSummary(b, scan, candidates)
	renderWifiMLONearbyAPs(b, groups, current)
	renderWifiMLODeviceReadiness(b, diagnostics.GetCapabilities())
	renderWifiMLOCapabilities(b, diagnostics.GetCapabilities())
	renderWifiMLODiagnostics(b, status, diagnostics.GetCapabilities(), scan, current, candidates)
}

func wifiMLOCurrentConnection(status *controlpb.WifiStatus) *controlpb.WifiConnection {
	conn := status.GetConnection()
	if conn == nil {
		return nil
	}
	if !wifiMLOActiveConnection(conn) {
		return nil
	}
	return conn
}

func wifiMLOActiveConnection(conn *controlpb.WifiConnection) bool {
	if wifiMLOKnownSSID(conn.GetSsid()) || wifiMLOKnownBSSID(conn.GetBssid()) {
		return true
	}
	if conn.GetIpv4Address() != "" || wifiConnectionHasMLO(conn) {
		return true
	}
	connectedState := strings.EqualFold(conn.GetSupplicantState(), "COMPLETED") ||
		strings.EqualFold(conn.GetDetailedState(), "CONNECTED")
	return conn.GetNetworkId() >= 0 && connectedState
}

func wifiMLOKnownSSID(value string) bool {
	normalized := strings.Trim(strings.TrimSpace(value), `"`)
	return normalized != "" && !strings.EqualFold(normalized, "<unknown ssid>")
}

func wifiMLOKnownBSSID(value string) bool {
	normalized := strings.TrimSpace(value)
	return normalized != "" &&
		!strings.EqualFold(normalized, "02:00:00:00:00:00") &&
		!strings.EqualFold(normalized, "00:00:00:00:00:00")
}

func renderWifiMLOCurrentRelation(b *strings.Builder, current *controlpb.WifiConnection, candidates []*controlpb.WifiScanResult) {
	if current == nil {
		writeSection(b, "Current AP Relation")
		b.WriteString("  no active Wi-Fi connection\n")
		return
	}
	sameMLDResults := make([]*controlpb.WifiScanResult, 0)
	currentBSSIDSeen := false
	for _, result := range candidates {
		if bssidEqual(result.GetBssid(), current.GetBssid()) {
			currentBSSIDSeen = true
		}
		if wifiMLOSameMLD(current, result) {
			sameMLDResults = append(sameMLDResults, result)
		}
	}
	visibleLinks := map[int32]bool{}
	for _, result := range sameMLDResults {
		for _, id := range wifiMLOScanLinkIDs(result) {
			visibleLinks[id] = true
		}
	}
	associatedLinks := wifiMLOAssociatedLinkIDSet(current)
	missingLinks := map[int32]bool{}
	for id := range associatedLinks {
		if !visibleLinks[id] {
			missingLinks[id] = true
		}
	}
	writeKVSection(b, "Current AP Relation",
		kv("connected_bssid", empty(current.GetBssid(), "<unknown>")),
		kv("connected_ap_mld", empty(wifiMLOConnectionMLDMAC(current), "<none>")),
		kv("connected_link", wifiMLOConnectionLinkID(current)),
		kv("current_bssid_seen", currentBSSIDSeen),
		kv("same_mld_results", len(sameMLDResults)),
		kv("visible_links", wifiMLOJoinIntSet(visibleLinks, "<none>")),
		kv("associated_links", wifiMLOJoinIntSet(associatedLinks, "<none>")),
		kv("missing_associated", wifiMLOJoinIntSet(missingLinks, "<none>")),
	)
}

func renderWifiMLOConnected(b *strings.Builder, current *controlpb.WifiConnection) {
	writeSection(b, "Connected MLO")
	if current == nil {
		b.WriteString("  no active Wi-Fi connection\n")
		return
	}
	present := wifiConnectionHasMLO(current)
	writeKVRows(b,
		kv("ssid", empty(current.GetSsid(), "<hidden>")),
		kv("bssid", empty(current.GetBssid(), "<unknown>")),
		kv("standard", empty(current.GetWifiStandard(), "<unknown>")),
		kv("present", present),
		kv("ap_mld", empty(wifiMLOConnectionMLDMAC(current), "<none>")),
		kv("ap_link_id", wifiMLOConnectionLinkID(current)),
		kv("affiliated", len(current.GetAffiliatedMloLinks())),
		kv("associated", len(current.GetAssociatedMloLinks())),
	)
	renderMLOLinks(b, "Associated MLO Links", current.GetAssociatedMloLinks())
	renderMLOLinks(b, "Affiliated MLO Links", current.GetAffiliatedMloLinks())
}

func renderWifiMLOConnectedEHT(b *strings.Builder, current *controlpb.WifiConnection) {
	if current == nil {
		return
	}
	lines := formatEHTMultiLinkElements("connection", parseEHTMultiLinkElements(current.GetInformationElements()))
	if len(lines) == 0 {
		return
	}
	writeSection(b, "Connected EHT Multi-Link Elements")
	for _, line := range lines {
		fmt.Fprintf(b, "  %s\n", line)
	}
}

func renderWifiMLOConnectedEHTDetails(b *strings.Builder, current *controlpb.WifiConnection) {
	if current == nil || !wifiMLOConnectionHasEHTDetails(current) {
		return
	}
	writeSection(b, "Connected EHT Details")
	renderWifiMLOEHTDetails(b, "connection", current.GetEhtCapabilities(), current.GetEhtOperation())
}

func renderWifiMLONetworks(b *strings.Builder, networks []*controlpb.NetworkDiagnostics) {
	rows := make([]*controlpb.NetworkDiagnostics, 0)
	for _, network := range networks {
		if wifiConnectionHasMLO(network.GetIpStatus().GetWifi()) {
			rows = append(rows, network)
		}
	}
	if len(rows) == 0 {
		return
	}
	writeSection(b, "Network MLO")
	columns := []displayTableColumn{
		{header: "NETWORK"},
		{header: "ACTIVE"},
		{header: "SSID"},
		{header: "BSSID"},
		{header: "AP_MLD"},
		{header: "AP_LINK"},
		{header: "ASSOC"},
		{header: "AFFIL"},
		{header: "STANDARD"},
	}
	tableRows := make([][]string, 0, len(rows))
	for _, network := range rows {
		conn := network.GetIpStatus().GetWifi()
		tableRows = append(tableRows, []string{
			empty(network.GetNetworkId(), "<unknown>"),
			fmt.Sprint(network.GetActive()),
			empty(conn.GetSsid(), "<hidden>"),
			empty(conn.GetBssid(), "<unknown>"),
			empty(wifiMLOConnectionMLDMAC(conn), "<none>"),
			wifiMLOConnectionLinkID(conn),
			fmt.Sprint(len(conn.GetAssociatedMloLinks())),
			fmt.Sprint(len(conn.GetAffiliatedMloLinks())),
			empty(conn.GetWifiStandard(), "<unknown>"),
		})
	}
	writeDisplayTable(b, columns, tableRows)
}

func renderWifiMLOScanSummary(b *strings.Builder, scan *controlpb.WifiScan, candidates []*controlpb.WifiScanResult) {
	fields := wifiMLOFieldMap(scan.GetFields())
	results := 0
	errors := 0
	if scan != nil {
		results = len(scan.GetResults())
		errors = len(scan.GetErrors())
	}
	rows := []kvRow{
		kv("source", wifiMLOScanSource(fields)),
		kv("results", wifiMLOFieldOrCount(fields, "scan_result_count", results)),
		kv("total", wifiMLOFieldOrCount(fields, "scan_result_total_count", results)),
		kv("mlo_candidates", len(candidates)),
		kv("errors", errors),
	}
	for _, key := range []string{
		"requested_band",
		"wifi_enabled",
		"wifi_state",
		"scan_always_available",
		"scan_throttle_enabled",
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
	writeKVSection(b, "MLO Scan", rows...)
}

func wifiMLOScanSource(fields map[string]string) string {
	for key := range fields {
		if strings.HasPrefix(key, "fresh_scan_") {
			return "fresh"
		}
	}
	return "diagnostics"
}

func renderWifiMLONearbyAPs(b *strings.Builder, groups []wifiMLOGroup, current *controlpb.WifiConnection) {
	writeSection(b, "Nearby MLO APs")
	if len(groups) == 0 {
		b.WriteString("  no MLO-capable scan results\n")
		return
	}
	columns := []wifiMLOTableColumn{
		{header: "SSID", maxWidth: 14},
		{header: "BANDS", maxWidth: 8},
		{header: "RSSI", maxWidth: 4},
		{header: "SEC", maxWidth: 7},
		{header: "STANDARD", maxWidth: 8},
		{header: "EHT_W", maxWidth: 9},
		{header: "PUNCT", maxWidth: 7},
	}
	rows := [][]string{}
	for _, group := range groups {
		rows = append(rows, []string{
			wifiMLOJoinStrings(wifiMLOGroupSSIDs(group), "<hidden>"),
			wifiMLOJoinStrings(group.bands, "unknown"),
			fmt.Sprint(group.bestRSSI),
			wifiMLOJoinStrings(group.security, "-"),
			wifiMLOJoinStrings(group.standards, "-"),
			wifiMLOGroupEHTOperationWidths(group),
			wifiMLOGroupEHTOperationPuncturing(group),
		})
	}
	wifiMLOWriteTable(b, columns, rows)
	renderWifiMLOScanLinks(b, groups, current)
	renderWifiMLOScanSecurityDetails(b, groups)
	renderWifiMLORNRDetails(b, groups)
	renderWifiMLOMultipleBSSIDDetails(b, groups)
	renderWifiMLOScanHE6GHzDetails(b, groups)
	renderWifiMLOScanEHT(b, groups)
	renderWifiMLOScanEHTDetails(b, groups)
}

func renderWifiMLOConnectedSecurity(b *strings.Builder, current *controlpb.WifiConnection) {
	if current == nil || current.GetSecurityDetails() == nil {
		return
	}
	writeSection(b, "Connected Wi-Fi Security")
	renderWifiMLOSecurityDetails(b, "connection", current.GetSecurityDetails())
}

func renderWifiMLOScanSecurityDetails(b *strings.Builder, groups []wifiMLOGroup) {
	results := []*controlpb.WifiScanResult{}
	for _, group := range groups {
		for _, result := range group.results {
			if result.GetSecurityDetails() != nil {
				results = append(results, result)
			}
		}
	}
	if len(results) == 0 {
		return
	}
	writeSection(b, "Scan Wi-Fi 7 Security")
	for i, result := range results {
		if i > 0 {
			b.WriteByte('\n')
		}
		label := fmt.Sprintf("ap ssid=%s bssid=%s", empty(result.GetSsid(), "<hidden>"), empty(result.GetBssid(), "<unknown>"))
		renderWifiMLOSecurityDetails(b, label, result.GetSecurityDetails())
	}
}

func renderWifiMLOSecurityDetails(b *strings.Builder, label string, details *controlpb.WifiSecurityDetails) {
	fmt.Fprintf(b, "  %s\n", label)
	for _, line := range wifiSecuritySummaryLines(details) {
		fmt.Fprintf(b, "    %s\n", line)
	}
}

func renderWifiMLOConnectedHE6GHzDetails(b *strings.Builder, current *controlpb.WifiConnection) {
	if current == nil || current.GetHe_6GhzCapabilities() == nil {
		return
	}
	writeSection(b, "Connected HE 6GHz Details")
	fmt.Fprintf(b, "  connection %s\n", wifiMLOHE6GHzSummary(current.GetHe_6GhzCapabilities()))
}

func renderWifiMLOScanHE6GHzDetails(b *strings.Builder, groups []wifiMLOGroup) {
	results := []*controlpb.WifiScanResult{}
	for _, group := range groups {
		for _, result := range group.results {
			if result.GetHe_6GhzCapabilities() != nil {
				results = append(results, result)
			}
		}
	}
	if len(results) == 0 {
		return
	}
	writeSection(b, "Scan HE 6GHz Details")
	for _, result := range results {
		label := fmt.Sprintf("ap ssid=%s bssid=%s", empty(result.GetSsid(), "<hidden>"), empty(result.GetBssid(), "<unknown>"))
		fmt.Fprintf(b, "  %s %s\n", label, wifiMLOHE6GHzSummary(result.GetHe_6GhzCapabilities()))
	}
}

func renderWifiMLOScanEHT(b *strings.Builder, groups []wifiMLOGroup) {
	lines := []string{}
	for _, group := range groups {
		for _, result := range group.results {
			label := fmt.Sprintf("ap ssid=%s bssid=%s", empty(result.GetSsid(), "<hidden>"), empty(result.GetBssid(), "<unknown>"))
			lines = append(lines, formatEHTMultiLinkElements(label, parseEHTMultiLinkElements(result.GetInformationElements()))...)
		}
	}
	if len(lines) == 0 {
		return
	}
	writeSection(b, "Scan EHT Multi-Link Elements")
	for _, line := range lines {
		fmt.Fprintf(b, "  %s\n", line)
	}
}

func renderWifiMLOScanEHTDetails(b *strings.Builder, groups []wifiMLOGroup) {
	results := []*controlpb.WifiScanResult{}
	for _, group := range groups {
		for _, result := range group.results {
			if wifiMLOScanHasEHTDetails(result) {
				results = append(results, result)
			}
		}
	}
	if len(results) == 0 {
		return
	}
	writeSection(b, "Scan EHT Details")
	for i, result := range results {
		if i > 0 {
			b.WriteByte('\n')
		}
		label := fmt.Sprintf("ap ssid=%s bssid=%s", empty(result.GetSsid(), "<hidden>"), empty(result.GetBssid(), "<unknown>"))
		renderWifiMLOEHTDetails(b, label, result.GetEhtCapabilities(), result.GetEhtOperation())
	}
}

func renderWifiMLOEHTDetails(b *strings.Builder, label string, capabilities *controlpb.WifiEhtCapabilities, operation *controlpb.WifiEhtOperation) {
	fmt.Fprintf(b, "  %s\n", label)
	if capabilities != nil {
		if capabilities.GetMac() != nil {
			for _, line := range wifiMLOEHTMACSummaryLines(capabilities) {
				fmt.Fprintf(b, "    %s\n", line)
			}
		}
		if capabilities.GetPhy() != nil {
			for _, line := range wifiMLOEHTPHYSummaryLines(capabilities) {
				fmt.Fprintf(b, "    %s\n", line)
			}
		}
		if len(capabilities.GetMcsNss()) > 0 {
			wifiMLOWriteMCSNSSLines(b, "mcs_nss", capabilities.GetMcsNss())
		}
		if capabilities.GetPpeThresholdsPresent() {
			fmt.Fprintf(b, "    ppe nss=%d ru=%s hex=0x%s\n", capabilities.GetPpeNssCount(), wifiMLOJoinStrings(capabilities.GetPpeRuIndices(), "<none>"), capabilities.GetPpeThresholdsHex())
		}
		if len(capabilities.GetWarnings()) > 0 {
			fmt.Fprintf(b, "    cap_warnings %s\n", strings.Join(capabilities.GetWarnings(), ","))
		}
	}
	if operation != nil {
		for _, line := range wifiMLOEHTOperationSummaryLines(operation) {
			fmt.Fprintf(b, "    %s\n", line)
		}
		if len(operation.GetBasicMcsNss()) > 0 {
			wifiMLOWriteMCSNSSLines(b, "basic_mcs_nss", operation.GetBasicMcsNss())
		}
		if len(operation.GetWarnings()) > 0 {
			fmt.Fprintf(b, "    oper_warnings %s\n", strings.Join(operation.GetWarnings(), ","))
		}
	}
}

func wifiMLOEHTMACSummaryLines(value *controlpb.WifiEhtCapabilities) []string {
	mac := value.GetMac()
	flags := []string{}
	if mac.GetEpcsPriorityAccess() {
		flags = append(flags, "epcs")
	}
	if mac.GetOmControl() {
		flags = append(flags, "om_control")
	}
	if mac.GetRestrictedTwt() {
		flags = append(flags, "restricted_twt")
	}
	if mac.GetEhtTrs() {
		flags = append(flags, "trs")
	}
	if mac.GetTxopReturn() {
		flags = append(flags, "txop_return")
	}
	if mac.GetTwoBqrs() {
		flags = append(flags, "two_bqrs")
	}
	if mac.GetUnsolicitedEpcsPriorityAccess() {
		flags = append(flags, "unsol_epcs")
	}
	lines := []string{fmt.Sprintf("mac max_mpdu=%d max_ampdu_ext=%d link_adapt=%s", mac.GetMaxMpduLengthBytes(), mac.GetMaxAmpduLengthExponentExtension(), empty(mac.GetLinkAdaptation(), "<unknown>"))}
	lines = append(lines, wifiMLOWrappedListLines("mac flags", flags, "<none>")...)
	return lines
}

func wifiMLOHE6GHzSummary(value *controlpb.WifiHe6GhzCapabilities) string {
	flags := []string{}
	if value.GetRdResponder() {
		flags = append(flags, "rd_responder")
	}
	if value.GetRxAntennaPatternConsistency() {
		flags = append(flags, "rx_antpat")
	}
	if value.GetTxAntennaPatternConsistency() {
		flags = append(flags, "tx_antpat")
	}
	parts := []string{
		fmt.Sprintf("max_mpdu=%d", value.GetMaxMpduLengthBytes()),
		fmt.Sprintf("max_ampdu_exp=%d", value.GetMaxAmpduLengthExponent()),
		fmt.Sprintf("max_ampdu=%d", value.GetMaxAmpduLengthBytes()),
		fmt.Sprintf("min_mpdu_start=%s", empty(value.GetMinimumMpduStartSpacing(), "<unknown>")),
		fmt.Sprintf("smps=%s", empty(value.GetSmPowerSave(), "<unknown>")),
		fmt.Sprintf("flags=%s", wifiMLOJoinStrings(flags, "<none>")),
	}
	if len(value.GetWarnings()) > 0 {
		parts = append(parts, "warnings="+strings.Join(value.GetWarnings(), ","))
	}
	return strings.Join(parts, " ")
}

func wifiMLOEHTPHYSummaryLines(value *controlpb.WifiEhtCapabilities) []string {
	phy := value.GetPhy()
	muMIMO := []string{}
	if phy.GetNonOfdmaUlMuMimo_80Mhz() {
		muMIMO = append(muMIMO, "80")
	}
	if phy.GetNonOfdmaUlMuMimo_160Mhz() {
		muMIMO = append(muMIMO, "160")
	}
	if phy.GetNonOfdmaUlMuMimo_320Mhz() {
		muMIMO = append(muMIMO, "320")
	}
	muBeamformer := []string{}
	if phy.GetMuBeamformer_80Mhz() {
		muBeamformer = append(muBeamformer, "80")
	}
	if phy.GetMuBeamformer_160Mhz() {
		muBeamformer = append(muBeamformer, "160")
	}
	if phy.GetMuBeamformer_320Mhz() {
		muBeamformer = append(muBeamformer, "320")
	}
	mcs15 := []string{}
	if phy.GetMcs15Supported_80Mhz() {
		mcs15 = append(mcs15, "80")
	}
	if phy.GetMcs15Supported_160Mhz() {
		mcs15 = append(mcs15, "160")
	}
	if phy.GetMcs15Supported_320Mhz() {
		mcs15 = append(mcs15, "320")
	}
	qam := []string{}
	if phy.GetRx_1024QamWiderBwDlOfdma() {
		qam = append(qam, "1024qam_wider_dl_ofdma")
	}
	if phy.GetRx_4096QamWiderBwDlOfdma() {
		qam = append(qam, "4096qam_wider_dl_ofdma")
	}
	return []string{
		fmt.Sprintf("phy caps 320mhz=%t ru_gt20=%t ltf=max%d/extra=%t padding=%s", phy.GetSupports_320MhzIn_6Ghz(), phy.GetSupports_242ToneRuGt_20Mhz(), phy.GetMaxSupportedEhtLtf(), phy.GetExtraEhtLtfSupported(), empty(phy.GetCommonNominalPacketPadding(), "<unknown>")),
		fmt.Sprintf("phy beamformee_ss 80=%d 160=%d 320=%d", phy.GetBeamformeeSs_80Mhz(), phy.GetBeamformeeSs_160Mhz(), phy.GetBeamformeeSs_320Mhz()),
		fmt.Sprintf("phy sounding 80=%d 160=%d 320=%d", phy.GetSoundingDimensions_80Mhz(), phy.GetSoundingDimensions_160Mhz(), phy.GetSoundingDimensions_320Mhz()),
		fmt.Sprintf("phy mu_mimo=%s mu_bf=%s", wifiMLOJoinStrings(muMIMO, "<none>"), wifiMLOJoinStrings(muBeamformer, "<none>")),
		fmt.Sprintf("phy mcs15=%s qam=%s", wifiMLOJoinStrings(mcs15, "<none>"), wifiMLOJoinStrings(qam, "<none>")),
	}
}

func wifiMLOEHTOperationSummaryLines(value *controlpb.WifiEhtOperation) []string {
	lines := []string{
		fmt.Sprintf("oper op_info=%t width=%s width_mhz=%d code=%d", value.GetOperationInformationPresent(), empty(value.GetChannelWidth(), "<unknown>"), value.GetChannelWidthMhz(), value.GetChannelWidthCode()),
		fmt.Sprintf("oper ccfs0=%d ccfs1=%d mcs15_disabled=%t group_bu_exp=%d", value.GetCenterFreqSegment0(), value.GetCenterFreqSegment1(), value.GetMcs15Disabled(), value.GetGroupAddressedBuIndicationExponent()),
	}
	if value.GetDisabledSubchannelBitmapPresent() || value.GetDisabledSubchannelBitmap() != 0 {
		bitmap := value.GetDisabledSubchannelBitmapHex()
		if bitmap == "" {
			bitmap = fmt.Sprintf("%04x", value.GetDisabledSubchannelBitmap())
		}
		lines = append(lines, fmt.Sprintf("oper disabled=0x%s punctured=%s", bitmap, wifiMLOJoinUint32s(value.GetDisabledSubchannelIndices(), "<none>")))
	}
	return lines
}

func wifiMLOWriteMCSNSSLines(b *strings.Builder, label string, values []*controlpb.WifiMcsNssSupport) {
	type key struct {
		standard  string
		bandwidth string
		mcsRange  string
	}
	groups := map[key][]*controlpb.WifiMcsNssSupport{}
	order := []key{}
	for _, value := range values {
		k := key{standard: value.GetStandard(), bandwidth: value.GetBandwidth(), mcsRange: value.GetMcsRange()}
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], value)
	}
	for _, k := range order {
		var rx, tx uint32
		for _, value := range groups[k] {
			streams := value.GetMaxNss()
			if streams == 0 {
				streams = value.GetNss()
			}
			switch value.GetDirection() {
			case "rx":
				rx = streams
			case "tx":
				tx = streams
			}
		}
		fmt.Fprintf(b, "    %s %s/%s/%s rx=nss%d tx=nss%d\n", label, k.standard, k.bandwidth, k.mcsRange, rx, tx)
	}
}

func renderWifiMLOScanLinks(b *strings.Builder, groups []wifiMLOGroup, current *controlpb.WifiConnection) {
	if len(groups) == 0 {
		return
	}
	writeSection(b, "MLO Scan Links")
	first := true
	for _, group := range groups {
		for _, result := range group.results {
			if !first {
				b.WriteByte('\n')
			}
			first = false
			renderWifiMLOScanLinkBlock(b, group, result, current)
			for _, link := range result.GetAffiliatedMloLinks() {
				b.WriteByte('\n')
				renderWifiMLOAffiliatedLinkBlock(b, group, result, link, current)
			}
		}
	}
}

func renderWifiMLOScanLinkBlock(b *strings.Builder, group wifiMLOGroup, result *controlpb.WifiScanResult, current *controlpb.WifiConnection) {
	fmt.Fprintf(b, "[%s] %s\n", wifiMLOBlockMark(wifiMLOResultMark(group, result, current)), empty(result.GetSsid(), "<hidden>"))
	fmt.Fprintf(b, "  ap_mld=%s link=%s bssid=%s\n", group.displayMLD, wifiMLOScanLinkID(result), empty(result.GetBssid(), "<unknown>"))
	fmt.Fprintf(b, "  band=%s ch=%s freq=%dMHz width=%s%s rssi=%ddBm\n",
		empty(result.GetBand(), wifiBandFromFrequency(result.GetFrequencyMhz())),
		wifiChannelFromFrequency(result.GetFrequencyMhz()),
		result.GetFrequencyMhz(),
		empty(wifiMLOScanChannelWidth(result.GetChannelWidth()), "<unknown>"),
		wifiMLOScanEHTOperationSuffix(result),
		result.GetRssiDbm(),
	)
	fmt.Fprintf(b, "  %s\n", wifiMLOInformationElementChecklist(result))
	fmt.Fprintf(b, "  %s\n", wifiMLOScanSDKFlags(result))
}

func renderWifiMLOAffiliatedLinkBlock(b *strings.Builder, group wifiMLOGroup, result *controlpb.WifiScanResult, link *controlpb.MloLinkInfo, current *controlpb.WifiConnection) {
	fmt.Fprintf(b, "[%s] affiliated %s\n", wifiMLOBlockMark(wifiMLOLinkMark(group, link, current)), empty(result.GetSsid(), "<hidden>"))
	fmt.Fprintf(b, "  ap_mld=%s\n", group.displayMLD)
	fmt.Fprintf(b, "  link=%d parent_bssid=%s\n", link.GetLinkId(), empty(result.GetBssid(), "<unknown>"))
	fmt.Fprintf(b, "  band=%s ch=%d state=%s rssi=%ddBm tx=%d rx=%d max_tx=%d max_rx=%d ap_mac=%s\n", empty(link.GetBand(), "<unknown>"), link.GetChannel(), empty(link.GetState(), "<unknown>"), link.GetRssiDbm(), link.GetTxLinkSpeedMbps(), link.GetRxLinkSpeedMbps(), link.GetMaxSupportedTxLinkSpeedMbps(), link.GetMaxSupportedRxLinkSpeedMbps(), empty(link.GetApMacAddress(), "<unknown>"))
}

func renderWifiMLOCapabilities(b *strings.Builder, capabilities *controlpb.WifiCapabilities) {
	if capabilities == nil {
		return
	}
	rows := []kvRow{}
	if len(capabilities.GetSupportedStandards()) > 0 {
		rows = append(rows, kv("supported_standards", strings.Join(capabilities.GetSupportedStandards(), ",")))
	}
	if len(capabilities.GetUnsupportedStandards()) > 0 {
		rows = append(rows, kv("unsupported_standards", strings.Join(capabilities.GetUnsupportedStandards(), ",")))
	}
	if features := wifiMLORelevantStrings(append(capabilities.GetSupportedFeatures(), capabilities.GetUnsupportedFeatures()...)); len(features) > 0 {
		rows = append(rows, kv("mlo_features", strings.Join(features, ",")))
	}
	fieldRows := wifiMLORelevantFieldRows(capabilities.GetFields())
	if len(rows) == 0 && len(fieldRows) == 0 && len(capabilities.GetErrors()) == 0 {
		return
	}
	if len(rows) > 0 {
		writeKVSection(b, "MLO Capability Signals", rows...)
	}
	if len(fieldRows) > 0 {
		writeKVSection(b, "MLO Capability Fields", fieldRows...)
	}
	if len(capabilities.GetErrors()) > 0 {
		writeSection(b, "MLO Capability Errors")
		for _, err := range capabilities.GetErrors() {
			fmt.Fprintf(b, "  %s\n", err)
		}
	}
}

func renderWifiMLODiagnostics(b *strings.Builder, status *controlpb.WifiStatus, capabilities *controlpb.WifiCapabilities, scan *controlpb.WifiScan, current *controlpb.WifiConnection, candidates []*controlpb.WifiScanResult) {
	warnings := []string{}
	fields := wifiMLOFieldMap(scan.GetFields())
	if status == nil {
		warnings = append(warnings, "wifi_status_unavailable")
	} else {
		if !status.GetEnabled() {
			warnings = append(warnings, "wifi_disabled")
		}
		if state := status.GetState(); state != "" && state != "enabled" {
			warnings = append(warnings, "wifi_state="+state)
		}
		for _, permission := range status.GetPermissions() {
			if !strings.HasSuffix(permission, "=granted") {
				warnings = append(warnings, "permission "+permission)
			}
		}
	}
	if fields["wifi_enabled"] == "false" {
		warnings = append(warnings, "scan_wifi_enabled=false")
	}
	if fields["scan_always_available"] == "false" {
		warnings = append(warnings, "scan_always_available=false")
	}
	if fields["scan_throttle_enabled"] == "true" {
		warnings = append(warnings, "scan_throttle_enabled=true")
	}
	for _, err := range scan.GetErrors() {
		warnings = append(warnings, "scan_error="+err)
	}
	if capabilities != nil {
		if wifiMLOContainsValue(capabilities.GetUnsupportedStandards(), "802.11be") {
			warnings = append(warnings, "wifi_7_standard_unsupported")
		}
		if wifiMLOContainsValue(capabilities.GetUnsupportedFeatures(), "tid_to_link_mapping_negotiation") {
			warnings = append(warnings, "tid_to_link_mapping_negotiation_unsupported")
		}
		for _, err := range capabilities.GetErrors() {
			if wifiMLORelevantText(err) {
				warnings = append(warnings, "capability_error="+err)
			}
		}
	}
	if current != nil && !wifiConnectionHasMLO(current) {
		warnings = append(warnings, "connected_mlo_present=false")
	}
	if len(candidates) == 0 {
		warnings = append(warnings, "mlo_scan_results=0")
	}
	if current != nil && current.GetApMldMacAddress() != "" {
		seen := false
		for _, result := range candidates {
			if wifiMLOSameMLD(current, result) {
				seen = true
				break
			}
		}
		if !seen {
			warnings = append(warnings, "connected_ap_mld_not_seen_in_scan")
		}
	}
	warnings = append(warnings, wifiMLOMetadataWarnings(candidates)...)
	if current != nil {
		visibleLinks := map[int32]bool{}
		for _, result := range candidates {
			if wifiMLOSameMLD(current, result) {
				for _, id := range wifiMLOScanLinkIDs(result) {
					visibleLinks[id] = true
				}
			}
		}
		missing := map[int32]bool{}
		for id := range wifiMLOAssociatedLinkIDSet(current) {
			if !visibleLinks[id] {
				missing[id] = true
			}
		}
		if len(missing) > 0 {
			warnings = append(warnings, "associated_link_missing_from_scan ids="+wifiMLOJoinIntSet(missing, "<none>"))
		}
	}
	writeSection(b, "Diagnostics / Warnings")
	if len(warnings) == 0 {
		b.WriteString("  none\n")
		return
	}
	for _, warning := range wifiMLOUniqueStrings(warnings) {
		fmt.Fprintf(b, "  %s\n", warning)
	}
}

func wifiMLOMetadataWarnings(candidates []*controlpb.WifiScanResult) []string {
	beResults := make([]*controlpb.WifiScanResult, 0, len(candidates))
	for _, result := range candidates {
		if strings.EqualFold(result.GetWifiStandard(), "802.11be") {
			beResults = append(beResults, result)
		}
	}
	if len(beResults) == 0 {
		return nil
	}

	apMLDSeen := 0
	linkIDSeen := 0
	withoutMetadata := 0
	for _, result := range beResults {
		if result.GetApMldMacAddress() != "" || wifiMLOMLDMACFromElements(result.GetInformationElements()) != "" {
			apMLDSeen++
		}
		if result.GetApMloLinkId() >= 0 || wifiMLOCurrentLinkIDFromElements(result.GetInformationElements()) != nil {
			linkIDSeen++
		}
		if !wifiMLOScanHasMetadata(result) && result.GetApMloLinkId() < 0 {
			withoutMetadata++
		}
	}
	switch {
	case apMLDSeen == 0 && linkIDSeen == 0:
		return []string{fmt.Sprintf("scan_mlo_metadata_absent 11be_results=%d ap_mld=0 link_id=0", len(beResults))}
	case withoutMetadata > 0:
		return []string{fmt.Sprintf("scan_mlo_metadata_partial missing=%d 11be_results=%d", withoutMetadata, len(beResults))}
	default:
		return nil
	}
}

func wifiMLOScanCandidates(results []*controlpb.WifiScanResult) []*controlpb.WifiScanResult {
	candidates := make([]*controlpb.WifiScanResult, 0, len(results))
	for _, result := range results {
		if wifiMLOCapableScanCandidate(result) {
			candidates = append(candidates, result)
		}
	}
	return candidates
}

func wifiMLOCapableScanCandidate(result *controlpb.WifiScanResult) bool {
	return wifiMLOScanHasMetadata(result) ||
		strings.EqualFold(result.GetWifiStandard(), "802.11be")
}

func wifiMLOScanHasMetadata(result *controlpb.WifiScanResult) bool {
	return result.GetApMldMacAddress() != "" ||
		len(result.GetAffiliatedMloLinks()) > 0 ||
		wifiMLOHasElement(result.GetInformationElements())
}

func wifiMLOConnectionHasEHTDetails(conn *controlpb.WifiConnection) bool {
	return conn.GetEhtCapabilities() != nil || conn.GetEhtOperation() != nil
}

func wifiMLOScanHasEHTDetails(result *controlpb.WifiScanResult) bool {
	return result.GetEhtCapabilities() != nil || result.GetEhtOperation() != nil
}

func wifiMLOGroups(results []*controlpb.WifiScanResult) []wifiMLOGroup {
	byKey := map[string][]*controlpb.WifiScanResult{}
	order := []string{}
	for _, result := range results {
		key := wifiMLOGroupKey(result)
		if _, ok := byKey[key]; !ok {
			order = append(order, key)
		}
		byKey[key] = append(byKey[key], result)
	}
	groups := make([]wifiMLOGroup, 0, len(byKey))
	for _, key := range order {
		groups = append(groups, newWifiMLOGroup(byKey[key]))
	}
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].bestRSSI != groups[j].bestRSSI {
			return groups[i].bestRSSI > groups[j].bestRSSI
		}
		return groups[i].displayMLD < groups[j].displayMLD
	})
	return groups
}

func wifiMLOGroupKey(result *controlpb.WifiScanResult) string {
	if mld := strings.ToLower(strings.TrimSpace(wifiMLOScanMLDMAC(result))); mld != "" {
		return "mld:" + mld
	}
	if bssid := strings.ToLower(strings.TrimSpace(result.GetBssid())); bssid != "" {
		return "bssid:" + bssid
	}
	return fmt.Sprintf("unknown:%s:%d", result.GetSsid(), result.GetFrequencyMhz())
}

func newWifiMLOGroup(results []*controlpb.WifiScanResult) wifiMLOGroup {
	group := wifiMLOGroup{results: results, displayMLD: "<unknown>"}
	firstRSSI := true
	for _, result := range results {
		if group.displayMLD == "<unknown>" && wifiMLOScanMLDMAC(result) != "" {
			group.displayMLD = wifiMLOScanMLDMAC(result)
		}
		if firstRSSI || result.GetRssiDbm() > group.bestRSSI {
			group.bestRSSI = result.GetRssiDbm()
			firstRSSI = false
		}
		group.bands = append(group.bands, empty(result.GetBand(), wifiBandFromFrequency(result.GetFrequencyMhz())))
		for _, link := range result.GetAffiliatedMloLinks() {
			if link.GetBand() != "" {
				group.bands = append(group.bands, link.GetBand())
			}
		}
		group.security = append(group.security, wifiMLOScanSecurity(result))
		group.standards = append(group.standards, result.GetWifiStandard())
	}
	group.bands = wifiMLOUniqueStrings(group.bands)
	group.security = wifiMLOUniqueStrings(group.security)
	group.standards = wifiMLOUniqueStrings(group.standards)
	return group
}

func wifiMLOSameMLD(conn *controlpb.WifiConnection, result *controlpb.WifiScanResult) bool {
	currentMLD := strings.TrimSpace(wifiMLOConnectionMLDMAC(conn))
	if currentMLD != "" && strings.EqualFold(currentMLD, strings.TrimSpace(wifiMLOScanMLDMAC(result))) {
		return true
	}
	return bssidEqual(conn.GetBssid(), result.GetBssid())
}

func bssidEqual(left string, right string) bool {
	return left != "" && right != "" && strings.EqualFold(left, right)
}

func wifiMLOResultMark(group wifiMLOGroup, result *controlpb.WifiScanResult, current *controlpb.WifiConnection) string {
	if current == nil {
		return ""
	}
	if bssidEqual(result.GetBssid(), current.GetBssid()) {
		return "*"
	}
	for _, candidate := range group.results {
		if wifiMLOSameMLD(current, candidate) {
			return "+"
		}
	}
	return ""
}

func wifiMLOLinkMark(group wifiMLOGroup, link *controlpb.MloLinkInfo, current *controlpb.WifiConnection) string {
	if current == nil {
		return ""
	}
	if bssidEqual(link.GetApMacAddress(), current.GetBssid()) {
		return "*"
	}
	for _, result := range group.results {
		if wifiMLOSameMLD(current, result) {
			return "+"
		}
	}
	return ""
}

func wifiMLOBlockMark(mark string) string {
	if mark == "" {
		return "-"
	}
	return mark
}

func wifiMLOConnectionLinkID(conn *controlpb.WifiConnection) string {
	if !wifiConnectionHasMLO(conn) {
		return "<none>"
	}
	if conn.GetApMloLinkId() >= 0 {
		return fmt.Sprint(conn.GetApMloLinkId())
	}
	if id := wifiMLOCurrentLinkIDFromElements(conn.GetInformationElements()); id != nil {
		return fmt.Sprint(*id)
	}
	return "<none>"
}

func wifiMLOScanLinkID(result *controlpb.WifiScanResult) string {
	explicitMLO := wifiMLOScanHasMetadata(result)
	elementLinkID := wifiMLOCurrentLinkIDFromElements(result.GetInformationElements())
	switch {
	case (explicitMLO || strings.EqualFold(result.GetWifiStandard(), "802.11be")) && result.GetApMloLinkId() >= 0:
		return fmt.Sprint(result.GetApMloLinkId())
	case elementLinkID != nil:
		return fmt.Sprint(*elementLinkID)
	case explicitMLO || strings.EqualFold(result.GetWifiStandard(), "802.11be"):
		return "<unknown>"
	default:
		return "<none>"
	}
}

func wifiMLOGroupEHTOperationWidths(group wifiMLOGroup) string {
	values := make([]string, 0, len(group.results))
	for _, result := range group.results {
		values = append(values, wifiMLOScanEHTOperationWidth(result))
	}
	return wifiMLOJoinStrings(values, "-")
}

func wifiMLOGroupEHTOperationPuncturing(group wifiMLOGroup) string {
	values := make([]string, 0, len(group.results))
	for _, result := range group.results {
		values = append(values, wifiMLOScanEHTOperationPuncturing(result))
	}
	return wifiMLOJoinStrings(values, "-")
}

func wifiMLOScanEHTOperationSuffix(result *controlpb.WifiScanResult) string {
	width := wifiMLOScanEHTOperationWidth(result)
	puncturing := wifiMLOScanEHTOperationPuncturing(result)
	if width == "" && puncturing == "" {
		return ""
	}
	return fmt.Sprintf(" eht_width=%s puncture=%s", empty(width, "<unknown>"), empty(puncturing, "-"))
}

func wifiMLOScanEHTOperationWidth(result *controlpb.WifiScanResult) string {
	operation := result.GetEhtOperation()
	if operation == nil {
		return ""
	}
	if !operation.GetOperationInformationPresent() {
		return "<unknown>"
	}
	if operation.GetChannelWidthMhz() > 0 {
		return fmt.Sprintf("%dMHz", operation.GetChannelWidthMhz())
	}
	return operation.GetChannelWidth()
}

func wifiMLOScanEHTOperationPuncturing(result *controlpb.WifiScanResult) string {
	operation := result.GetEhtOperation()
	if operation == nil {
		return ""
	}
	if !operation.GetDisabledSubchannelBitmapPresent() && operation.GetDisabledSubchannelBitmap() == 0 {
		return ""
	}
	if len(operation.GetDisabledSubchannelIndices()) == 0 {
		return "none"
	}
	return wifiMLOJoinUint32s(operation.GetDisabledSubchannelIndices(), "<none>")
}

func wifiMLOScanLinkIDs(result *controlpb.WifiScanResult) []int32 {
	ids := map[int32]bool{}
	if wifiMLOCapableScanCandidate(result) && result.GetApMloLinkId() >= 0 {
		ids[result.GetApMloLinkId()] = true
	}
	for _, link := range result.GetAffiliatedMloLinks() {
		if link.GetLinkId() >= 0 {
			ids[link.GetLinkId()] = true
		}
	}
	for _, id := range wifiMLOLinkIDsFromElements(result.GetInformationElements()) {
		ids[id] = true
	}
	return wifiMLOSortedIntSet(ids)
}

func wifiMLOAssociatedLinkIDSet(conn *controlpb.WifiConnection) map[int32]bool {
	ids := map[int32]bool{}
	if wifiConnectionHasMLO(conn) && conn.GetApMloLinkId() >= 0 {
		ids[conn.GetApMloLinkId()] = true
	}
	for _, link := range conn.GetAssociatedMloLinks() {
		if link.GetLinkId() >= 0 {
			ids[link.GetLinkId()] = true
		}
	}
	for _, id := range wifiMLOLinkIDsFromElements(conn.GetInformationElements()) {
		ids[id] = true
	}
	return ids
}

func wifiMLOScanMLDMAC(result *controlpb.WifiScanResult) string {
	return firstNonEmpty(result.GetApMldMacAddress(), wifiMLOMLDMACFromElements(result.GetInformationElements()))
}

func wifiMLOConnectionMLDMAC(conn *controlpb.WifiConnection) string {
	return firstNonEmpty(conn.GetApMldMacAddress(), wifiMLOMLDMACFromElements(conn.GetInformationElements()))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func wifiMLOHasElement(elements []*controlpb.WifiInformationElement) bool {
	return len(parseEHTMultiLinkElements(elements)) > 0
}

func wifiMLOMLDMACFromElements(elements []*controlpb.WifiInformationElement) string {
	for _, element := range parseEHTMultiLinkElements(elements) {
		if element.commonInfo != nil && element.commonInfo.mldMACAddress != "" {
			return element.commonInfo.mldMACAddress
		}
	}
	return ""
}

func wifiMLOCurrentLinkIDFromElements(elements []*controlpb.WifiInformationElement) *int32 {
	for _, element := range parseEHTMultiLinkElements(elements) {
		if element.commonInfo != nil && element.commonInfo.linkID != nil {
			value := int32(*element.commonInfo.linkID)
			return &value
		}
	}
	return nil
}

func wifiMLOLinkIDsFromElements(elements []*controlpb.WifiInformationElement) []int32 {
	ids := map[int32]bool{}
	for _, element := range parseEHTMultiLinkElements(elements) {
		if element.commonInfo != nil && element.commonInfo.linkID != nil {
			ids[int32(*element.commonInfo.linkID)] = true
		}
		for _, subelement := range element.subelements {
			if subelement.perSTAProfile != nil {
				ids[int32(subelement.perSTAProfile.linkID)] = true
			}
		}
	}
	return wifiMLOSortedIntSet(ids)
}

func wifiMLOScanSecurity(result *controlpb.WifiScanResult) string {
	if values := result.GetSecurityTypes(); len(values) > 0 {
		return strings.Join(values, ",")
	}
	return result.GetCapabilities()
}

func wifiMLOWrappedListLines(label string, values []string, emptyValue string) []string {
	values = wifiMLOUniqueStrings(values)
	if len(values) == 0 {
		return []string{label + " " + emptyValue}
	}
	lines := []string{}
	current := ""
	for _, value := range values {
		next := value
		if current != "" {
			next = current + "," + value
		}
		if len(next) > 56 && current != "" {
			lines = append(lines, label+" "+current)
			current = value
		} else {
			current = next
		}
	}
	if current != "" {
		lines = append(lines, label+" "+current)
	}
	return lines
}

func wifiMLOGroupSSIDs(group wifiMLOGroup) []string {
	values := make([]string, 0, len(group.results))
	for _, result := range group.results {
		values = append(values, empty(result.GetSsid(), "<hidden>"))
	}
	return wifiMLOUniqueStrings(values)
}

func wifiMLOFieldMap(fields []*controlpb.DiagnosticField) map[string]string {
	out := map[string]string{}
	for _, field := range fields {
		out[field.GetKey()] = field.GetValue()
	}
	return out
}

func wifiMLOFieldOrCount(fields map[string]string, key string, fallback int) string {
	if value := fields[key]; value != "" {
		return value
	}
	return fmt.Sprint(fallback)
}

func wifiMLORelevantFieldRows(fields []*controlpb.DiagnosticField) []kvRow {
	rows := []kvRow{}
	for _, field := range fields {
		if wifiMLORelevantText(field.GetKey()) || wifiMLORelevantText(field.GetValue()) {
			rows = append(rows, kv(field.GetKey(), field.GetValue()))
		}
	}
	return rows
}

func wifiMLORelevantStrings(values []string) []string {
	out := []string{}
	for _, value := range values {
		if wifiMLORelevantText(value) {
			out = append(out, value)
		}
	}
	return wifiMLOUniqueStrings(out)
}

func wifiMLORelevantText(value string) bool {
	lower := strings.ToLower(value)
	for _, token := range []string{"mlo", "mld", "multi-link", "tid_to_link", "tid-to-link", "802.11be", "wifi_7", "wifi7", "wi-fi 7"} {
		if strings.Contains(lower, token) {
			return true
		}
	}
	return false
}

func wifiMLOContainsValue(values []string, want string) bool {
	for _, value := range values {
		if strings.EqualFold(value, want) {
			return true
		}
	}
	return false
}

func wifiMLOScanChannelWidth(value string) string {
	trimmed := strings.Trim(strings.TrimSpace(value), ",;")
	if trimmed == "" {
		return ""
	}
	normalized := strings.ToLower(trimmed)
	normalized = strings.TrimPrefix(normalized, "channel_width_")
	normalized = strings.TrimPrefix(normalized, "width_")
	normalized = strings.ReplaceAll(normalized, "_", "")
	normalized = strings.ReplaceAll(normalized, " ", "")
	core := strings.TrimSuffix(normalized, "mhz")
	if core != "" {
		for _, r := range core {
			if (r < '0' || r > '9') && r != '+' {
				return trimmed
			}
		}
		return core + "MHz"
	}
	return trimmed
}

func wifiMLOWriteTable(b *strings.Builder, columns []wifiMLOTableColumn, rows [][]string) {
	displayColumns := make([]displayTableColumn, len(columns))
	for i, column := range columns {
		displayColumns[i] = displayTableColumn{
			header:   column.header,
			maxWidth: column.maxWidth,
		}
	}
	writeDisplayTable(b, displayColumns, rows)
}

func wifiMLOFitCell(value string, maxWidth int) string {
	return fitDisplayCell(value, maxWidth)
}

func wifiMLOPadDisplayEnd(value string, width int) string {
	return padDisplayEnd(value, width)
}

func wifiMLODisplayWidth(value string) int {
	return displayWidth(value)
}

func wifiMLORuneDisplayWidth(r rune) int {
	return runeDisplayWidth(r)
}

func wifiMLOJoinStrings(values []string, emptyValue string) string {
	values = wifiMLOUniqueStrings(values)
	filtered := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" {
			filtered = append(filtered, value)
		}
	}
	if len(filtered) == 0 {
		return emptyValue
	}
	return strings.Join(filtered, ",")
}

func wifiMLOJoinInts(values []int32, emptyValue string) string {
	if len(values) == 0 {
		return emptyValue
	}
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, fmt.Sprint(value))
	}
	return strings.Join(parts, ",")
}

func wifiMLOJoinUint32s(values []uint32, emptyValue string) string {
	if len(values) == 0 {
		return emptyValue
	}
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, fmt.Sprint(value))
	}
	return strings.Join(parts, ",")
}

func wifiMLOJoinIntSet(values map[int32]bool, emptyValue string) string {
	return wifiMLOJoinInts(wifiMLOSortedIntSet(values), emptyValue)
}

func wifiMLOSortedIntSet(values map[int32]bool) []int32 {
	out := make([]int32, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	slices.Sort(out)
	return out
}

func wifiMLOUniqueStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, value)
	}
	return out
}
