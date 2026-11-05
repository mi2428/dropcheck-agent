package render

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/mi2428/dropcheck-agent/controller/internal/command"
	"github.com/mi2428/dropcheck-agent/controller/internal/controlpb"
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

type wifiMLOFilter struct {
	ssid  string
	bssid string
}

func renderWifiMLO(b *strings.Builder, diagnostics *controlpb.WifiDiagnostics, options command.Options, view Presentation) {
	if diagnostics == nil {
		return
	}
	filter := wifiMLOFilter{ssid: options.WifiEHTSSID, bssid: options.WifiEHTBSSID}
	scan := diagnostics.GetScan()
	scanResults := scan.GetResults()
	candidates := wifiMLOScanCandidates(scanResults)
	status := diagnostics.GetStatus()
	current := wifiMLOCurrentConnection(status)
	groups := wifiMLOGroups(candidates)

	renderWifiMLOCurrentRelation(b, current, scan, candidates)
	renderWifiMLOConnected(b, current)
	renderWifiMLOConnectedSecurity(b, current)
	renderWifiMLOConnectedRoaming(b, current)
	renderWifiMLOConnectedBSSColoring(b, current)
	renderWifiMLOConnectedHEDetails(b, current)
	renderWifiMLOConnectedHE6GHzDetails(b, current)
	renderWifiMLOConnectedEHT(b, current)
	renderWifiMLOConnectedEHTDetails(b, current)
	renderWifiMLOConnectedEHTPuncturing(b, current)
	renderWifiMLONetworks(b, diagnostics.GetNetworks())
	renderWifiMLOScanSummary(b, scan, candidates, filter, len(scanResults))
	renderWifiMLONearbyAPs(b, groups, current, view)
	renderWifiMLODeviceReadiness(b, diagnostics.GetCapabilities())
	renderWifiMLOCapabilities(b, diagnostics.GetCapabilities())
	renderWifiMLODiagnostics(b, status, diagnostics.GetCapabilities(), scan, current, candidates)
}

func (f wifiMLOFilter) active() bool {
	return f.ssid != "" || f.bssid != ""
}

func (f wifiMLOFilter) label() string {
	switch {
	case f.ssid != "":
		return "ssid=" + f.ssid
	case f.bssid != "":
		return "bssid=" + f.bssid
	default:
		return ""
	}
}

func (f wifiMLOFilter) scanResults(results []*controlpb.WifiScanResult) []*controlpb.WifiScanResult {
	if !f.active() {
		return results
	}
	filtered := make([]*controlpb.WifiScanResult, 0, len(results))
	for _, result := range results {
		if f.matchesScan(result) {
			filtered = append(filtered, result)
		}
	}
	return filtered
}

func (f wifiMLOFilter) connection(conn *controlpb.WifiConnection) *controlpb.WifiConnection {
	if conn == nil || !f.active() || f.matchesConnection(conn) {
		return conn
	}
	return nil
}

func (f wifiMLOFilter) matchesScan(result *controlpb.WifiScanResult) bool {
	if result == nil {
		return false
	}
	if f.ssid != "" {
		return result.GetSsid() == f.ssid
	}
	if f.bssid != "" {
		if bssidEqual(result.GetBssid(), f.bssid) {
			return true
		}
		for _, link := range result.GetAffiliatedMloLinks() {
			if bssidEqual(link.GetApMacAddress(), f.bssid) {
				return true
			}
		}
	}
	return false
}

func (f wifiMLOFilter) matchesConnection(conn *controlpb.WifiConnection) bool {
	if conn == nil {
		return false
	}
	if f.ssid != "" {
		return conn.GetSsid() == f.ssid
	}
	if f.bssid != "" {
		if bssidEqual(conn.GetBssid(), f.bssid) {
			return true
		}
		for _, link := range append(conn.GetAssociatedMloLinks(), conn.GetAffiliatedMloLinks()...) {
			if bssidEqual(link.GetApMacAddress(), f.bssid) {
				return true
			}
		}
	}
	return false
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

func renderWifiMLOCurrentRelation(b *strings.Builder, current *controlpb.WifiConnection, scan *controlpb.WifiScan, candidates []*controlpb.WifiScanResult) {
	if current == nil {
		writeSection(b, "Current AP Relation")
		writeRecord(b, "Observation", "no active Wi-Fi connection")
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
	comparison := wifiMLOComparison(current, scan, candidates)
	missingText := "<unknown>"
	if comparison == "updated_scan_age_le_10s" {
		missingText = wifiMLOJoinIntSet(missingLinks)
	}
	writeKVSection(b, "Current AP Relation",
		kv("connected_bssid", empty(current.GetBssid(), "<unknown>")),
		kv("connected_ap_mld", empty(wifiMLOConnectionMLDMAC(current), "<none>")),
		kv("connected_link", wifiMLOConnectionLinkID(current)),
		kv("current_bssid_seen", currentBSSIDSeen),
		kv("same_mld_results", len(sameMLDResults)),
		kv("visible_links", wifiMLOJoinIntSet(visibleLinks)),
		kv("associated_links", wifiMLOJoinIntSet(associatedLinks)),
		kv("scan_comparison", comparison),
		kv("missing_associated", missingText),
	)
}

// Only a successful update with known, recent per-AP boot-clock ages supports
// a point-in-time scan comparison. It is not a continuity or roam guarantee.
func wifiMLOComparison(current *controlpb.WifiConnection, scan *controlpb.WifiScan, candidates []*controlpb.WifiScanResult) string {
	fields := diagnosticFieldMap(current.GetObservationFields())
	if fields["ap_mld_mac_address.state"] != "available" || fields["associated_mlo_links.state"] != "available" || !wifiMLOKnownBSSID(current.GetApMldMacAddress()) {
		return "unknown_connection_identity"
	}
	if wifiMLOFieldMap(scan.GetFields())["fresh_scan_results_updated"] != "true" || len(scan.GetErrors()) > 0 {
		return "unknown_scan_update"
	}
	for _, candidate := range candidates {
		if wifiMLOSameMLD(current, candidate) && (candidate.ObservationAgeMs == nil || candidate.GetObservationAgeMs() > 10000) {
			return "unknown_scan_age"
		}
	}
	return "updated_scan_age_le_10s"
}

func renderWifiMLOConnected(b *strings.Builder, current *controlpb.WifiConnection) {
	writeSection(b, "Connected MLO")
	if current == nil {
		writeRecord(b, "Observation", "no active Wi-Fi connection")
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
	writeRecord(b, "Elements", strings.Join(lines, "\n"))
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
		wifi := network.GetIpStatus().GetWifi()
		if wifiConnectionHasMLO(wifi) {
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

func renderWifiMLOScanSummary(b *strings.Builder, scan *controlpb.WifiScan, candidates []*controlpb.WifiScanResult, filter wifiMLOFilter, filteredResults int) {
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
		kv("eht_candidates", len(candidates)),
		kv("errors", errors),
	}
	if filter.active() {
		rows = append(rows,
			kv("filter", filter.label()),
			kv("filtered_results", filteredResults),
		)
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
	writeKVSection(b, "EHT Scan", rows...)
}

func wifiMLOScanSource(fields map[string]string) string {
	if source := fields["scan_source"]; source != "" {
		return source
	}
	for key := range fields {
		if strings.HasPrefix(key, "fresh_scan_") {
			return "fresh"
		}
	}
	return "diagnostics"
}

func renderWifiMLONearbyAPs(b *strings.Builder, groups []wifiMLOGroup, current *controlpb.WifiConnection, view Presentation) {
	writeSection(b, "Nearby EHT APs")
	if len(groups) == 0 {
		writeRecord(b, "Observation", "no EHT-capable scan results")
		return
	}
	renderWifiMLONearbyAPTable(b, groups, view)
	renderWifiMLOScanLinks(b, groups, current)
	renderWifiMLOScanSecurityDetails(b, groups)
	renderWifiMLOScanRoamingDetails(b, groups)
	renderWifiMLOScanBSSColoring(b, groups)
	renderWifiMLORNRDetails(b, groups)
	renderWifiMLOMultipleBSSIDDetails(b, groups)
	renderWifiMLOScanHEDetails(b, groups)
	renderWifiMLOScanHE6GHzDetails(b, groups)
	renderWifiMLOScanEHT(b, groups)
	renderWifiMLOScanEHTPuncturing(b, groups)
	renderWifiMLOScanEHTDetails(b, groups)
}

func renderWifiMLONearbyAPTable(b *strings.Builder, groups []wifiMLOGroup, view Presentation) {
	columns := []wifiMLOTableColumn{
		{header: "SSID", maxWidth: 14},
		{header: "BANDS", maxWidth: 8},
		{header: "RSSI", maxWidth: 4},
		{header: "SEC", maxWidth: 7},
		{header: "STANDARD", maxWidth: 8},
		{header: "COLOR", maxWidth: 8},
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
			wifiMLOGroupBSSColors(group),
			wifiMLOGroupEHTOperationWidths(group),
			wifiMLOGroupEHTOperationPuncturing(group),
		})
	}
	if view.Interactive {
		fullColumns := make([]displayTableColumn, len(columns))
		for i, col := range columns {
			fullColumns[i] = displayTableColumn{header: col.header}
		}
		if tableFits(fullColumns, rows, view.Width) {
			writeDisplayTable(b, fullColumns, rows)
		} else {
			for _, row := range rows {
				for i, column := range fullColumns {
					writeRecord(b, column.header, row[i])
				}
			}
		}
	} else {
		wifiMLOWriteTable(b, columns, rows)
	}
}

func renderWifiMLOConnectedSecurity(b *strings.Builder, current *controlpb.WifiConnection) {
	if current == nil || current.GetSecurityDetails() == nil {
		return
	}
	writeSection(b, "Connected Wi-Fi Security")
	renderWifiMLOSecurityDetails(b, "connection", current.GetSecurityDetails())
}

func renderWifiMLOConnectedRoaming(b *strings.Builder, current *controlpb.WifiConnection) {
	if current == nil {
		return
	}
	lines := wifiMLORoamingSummaryLines(current.GetInformationElements(), current.GetSecurityDetails())
	if len(lines) == 0 {
		return
	}
	writeSection(b, "Connected Roaming / Transition")
	fmt.Fprintf(b, "  connection\n")
	wifiMLOWriteDetailRows(b, kv("Roaming", strings.Join(lines, "\n")))
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

func renderWifiMLOScanRoamingDetails(b *strings.Builder, groups []wifiMLOGroup) {
	type entry struct {
		label string
		lines []string
	}
	entries := []entry{}
	for _, group := range groups {
		for _, result := range group.results {
			lines := wifiMLORoamingSummaryLines(result.GetInformationElements(), result.GetSecurityDetails())
			if len(lines) == 0 {
				continue
			}
			entries = append(entries, entry{
				label: fmt.Sprintf("ap ssid=%s bssid=%s", empty(result.GetSsid(), "<hidden>"), empty(result.GetBssid(), "<unknown>")),
				lines: lines,
			})
		}
	}
	if len(entries) == 0 {
		return
	}
	writeSection(b, "Scan Roaming / Transition")
	for i, entry := range entries {
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(b, "  %s\n", entry.label)
		wifiMLOWriteDetailRows(b, kv("Roaming", strings.Join(entry.lines, "\n")))
	}
}

func renderWifiMLOSecurityDetails(b *strings.Builder, label string, details *controlpb.WifiSecurityDetails) {
	fmt.Fprintf(b, "  %s\n", label)
	wifiMLOWriteDetailRows(b, kv("Security", strings.Join(wifiSecuritySummaryLines(details), "\n")))
}

func renderWifiMLOConnectedHEDetails(b *strings.Builder, current *controlpb.WifiConnection) {
	if current == nil || !wifiMLOConnectionHasHEDetails(current) {
		return
	}
	writeSection(b, "Connected HE Details")
	renderWifiMLOHEDetails(b, "connection", current.GetHeCapabilities(), current.GetHeOperation(), current.GetHeUoraParameterSet(), current.GetHeMuEdcaParameterSet())
}

func renderWifiMLOScanHEDetails(b *strings.Builder, groups []wifiMLOGroup) {
	results := []*controlpb.WifiScanResult{}
	for _, group := range groups {
		for _, result := range group.results {
			if wifiMLOScanHasHEDetails(result) {
				results = append(results, result)
			}
		}
	}
	if len(results) == 0 {
		return
	}
	writeSection(b, "Scan HE Details")
	for i, result := range results {
		if i > 0 {
			b.WriteByte('\n')
		}
		label := fmt.Sprintf("ap ssid=%s bssid=%s", empty(result.GetSsid(), "<hidden>"), empty(result.GetBssid(), "<unknown>"))
		renderWifiMLOHEDetails(b, label, result.GetHeCapabilities(), result.GetHeOperation(), result.GetHeUoraParameterSet(), result.GetHeMuEdcaParameterSet())
	}
}

func renderWifiMLOHEDetails(b *strings.Builder, label string, capabilities *controlpb.WifiHeCapabilities, operation *controlpb.WifiHeOperation, uora *controlpb.WifiHeUoraParameterSet, muEdca *controlpb.WifiHeMuEdcaParameterSet) {
	fmt.Fprintf(b, "  %s\n", label)
	if capabilities != nil {
		if capabilities.GetMac() != nil {
			wifiMLOWriteDetailRows(b, wifiMLOHEMACSummaryLines(capabilities)...)
		}
		if capabilities.GetPhy() != nil {
			wifiMLOWriteDetailRows(b, wifiMLOHEPHYSummaryLines(capabilities)...)
		}
		if len(capabilities.GetFeatures()) > 0 {
			wifiMLOWriteDetailRows(b, wifiMLOWrappedListLines("he features", capabilities.GetFeatures())...)
		}
		if len(capabilities.GetMcsNss()) > 0 {
			wifiMLOWriteMCSNSSLines(b, "he_mcs_nss", capabilities.GetMcsNss())
		}
		if capabilities.GetPpeThresholdsPresent() {
			wifiMLOWriteDetailRows(b, kv("he_ppe", fmt.Sprintf("nss=%d ru=%s hex=0x%s", capabilities.GetPpeNssCount(), wifiMLOJoinStrings(capabilities.GetPpeRuIndices(), "<none>"), capabilities.GetPpeThresholdsHex())))
		}
		if len(capabilities.GetWarnings()) > 0 {
			wifiMLOWriteDetailRows(b, kv("he_cap_warnings", strings.Join(capabilities.GetWarnings(), ",")))
		}
	}
	if operation != nil {
		wifiMLOWriteDetailRows(b, wifiMLOHEOperationSummaryLines(operation)...)
	}
	if uora != nil {
		wifiMLOWriteDetailRows(b, kv("he_uora", fmt.Sprintf("eocw_min=%d eocw_max=%d", uora.GetEocwMin(), uora.GetEocwMax())))
		if len(uora.GetWarnings()) > 0 {
			wifiMLOWriteDetailRows(b, kv("he_uora_warnings", strings.Join(uora.GetWarnings(), ",")))
		}
	}
	if muEdca != nil {
		wifiMLOWriteDetailRows(b, kv("he_mu_edca", fmt.Sprintf("qos_info=0x%x", muEdca.GetQosInfo())))
		for _, ac := range muEdca.GetAc() {
			wifiMLOWriteDetailRows(b, kv("he_mu_edca_ac "+ac.GetAc(), fmt.Sprintf("aci=%d aifsn=%d acm=%t ecw=%d/%d timer=%d", ac.GetAci(), ac.GetAifsn(), ac.GetAcm(), ac.GetEcwMin(), ac.GetEcwMax(), ac.GetTimer())))
		}
		if len(muEdca.GetWarnings()) > 0 {
			wifiMLOWriteDetailRows(b, kv("he_mu_edca_warnings", strings.Join(muEdca.GetWarnings(), ",")))
		}
	}
}

func wifiMLOHEMACSummaryLines(value *controlpb.WifiHeCapabilities) []kvRow {
	mac := value.GetMac()
	flags := []string{}
	if mac.GetTwtRequester() {
		flags = append(flags, "twt_requester")
	}
	if mac.GetTwtResponder() {
		flags = append(flags, "twt_responder")
	}
	if mac.GetOmControl() {
		flags = append(flags, "om_control")
	}
	if mac.GetOfdmaRandomAccess() {
		flags = append(flags, "ofdma_ra")
	}
	if mac.GetSrpResponder() {
		flags = append(flags, "srp_responder")
	}
	if mac.GetUl_2X996ToneRu() {
		flags = append(flags, "ul_2x996")
	}
	if mac.GetPuncturedSounding() {
		flags = append(flags, "punctured_sounding")
	}
	if mac.GetHtVhtTriggerFrameRx() {
		flags = append(flags, "ht_vht_trigger_rx")
	}
	lines := []kvRow{
		kv("he_mac", fmt.Sprintf("link_adapt=%s max_ampdu_ext=%d multi_tid_rx_qos=%d multi_tid_tx_qos=%d", empty(mac.GetLinkAdaptation(), "<unknown>"), mac.GetMaxAmpduLengthExponentExtension(), mac.GetMultiTidAggregationRxQos(), mac.GetMultiTidAggregationTxQos())),
	}
	lines = append(lines, wifiMLOWrappedListLines("he_mac flags", flags)...)
	return lines
}

func wifiMLOHEPHYSummaryLines(value *controlpb.WifiHeCapabilities) []kvRow {
	phy := value.GetPhy()
	return []kvRow{
		kv("he_phy", fmt.Sprintf("widths=%s puncture_rx=%s dcm_tx=%s/nss%d dcm_rx=%s/nss%d",
			wifiMLOJoinStrings(phy.GetChannelWidthSet(), "<none>"),
			wifiMLOJoinStrings(phy.GetPreamblePuncturingRx(), "<none>"),
			empty(phy.GetDcmMaxConstellationTx(), "<unknown>"),
			phy.GetDcmMaxNssTx(),
			empty(phy.GetDcmMaxConstellationRx(), "<unknown>"),
			phy.GetDcmMaxNssRx(),
		)),
		kv("he_phy bf", fmt.Sprintf("su_bfer=%t su_bfee=%t mu_bfer=%t bfee_sts<=80=%d bfee_sts>80=%d", phy.GetSuBeamformer(), phy.GetSuBeamformee(), phy.GetMuBeamformer(), phy.GetBeamformeeStsUnder_80Mhz(), phy.GetBeamformeeStsAbove_80Mhz())),
		kv("he_phy spatial_reuse", fmt.Sprintf("spatial_reuse=%t partial_bw_dl_mu_mimo=%t max_nc=%d padding=%s", phy.GetSrpBasedSpatialReuse(), phy.GetPartialBwDlMuMimo(), phy.GetMaxNc(), empty(phy.GetNominalPacketPadding(), "<unknown>"))),
	}
}

func wifiMLOHEOperationSummaryLines(value *controlpb.WifiHeOperation) []kvRow {
	lines := []kvRow{
		kv("he_oper", fmt.Sprintf("params=0x%x basic_mcs_nss=0x%s bss_color=%d disabled=%t", value.GetParameters(), value.GetBasicMcsNssSetHex(), value.GetBssColor(), value.GetBssColorDisabled())),
	}
	lines = append(lines, wifiMLOWrappedListLines("he_oper flags", value.GetFlags())...)
	if value.GetChannelWidth() != "" || value.GetPrimaryChannel() != 0 || value.GetCenterFreqSegment0() != 0 || value.GetCenterFreqSegment1() != 0 {
		lines = append(lines, kv("he_oper_6ghz", fmt.Sprintf("width=%s primary=%d ccfs0=%d ccfs1=%d", empty(value.GetChannelWidth(), "<unknown>"), value.GetPrimaryChannel(), value.GetCenterFreqSegment0(), value.GetCenterFreqSegment1())))
	}
	if value.GetTruncated() {
		lines = append(lines, kv("he_oper_truncated", true))
	}
	if len(value.GetWarnings()) > 0 {
		lines = append(lines, kv("he_oper_warnings", strings.Join(value.GetWarnings(), ",")))
	}
	return lines
}

func renderWifiMLOConnectedHE6GHzDetails(b *strings.Builder, current *controlpb.WifiConnection) {
	if current == nil || current.GetHe_6GhzCapabilities() == nil {
		return
	}
	writeSection(b, "Connected HE 6GHz Details")
	fmt.Fprintln(b, "  connection")
	wifiMLOWriteDetailRows(b, kv("he_6ghz", wifiMLOHE6GHzSummary(current.GetHe_6GhzCapabilities())))
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
		fmt.Fprintf(b, "  %s\n", label)
		wifiMLOWriteDetailRows(b, kv("he_6ghz", wifiMLOHE6GHzSummary(result.GetHe_6GhzCapabilities())))
	}
}

func renderWifiMLOConnectedBSSColoring(b *strings.Builder, current *controlpb.WifiConnection) {
	if current == nil || !wifiMLOHasBSSColoringContext(current.GetHeOperation(), current.GetHeSpatialReuseParameterSet()) {
		return
	}
	writeSection(b, "Connected BSS Coloring")
	renderWifiMLOBSSColoring(b, "connection", current.GetHeOperation(), current.GetHeSpatialReuseParameterSet())
}

func renderWifiMLOScanBSSColoring(b *strings.Builder, groups []wifiMLOGroup) {
	results := []*controlpb.WifiScanResult{}
	for _, group := range groups {
		for _, result := range group.results {
			if wifiMLOHasBSSColoringContext(result.GetHeOperation(), result.GetHeSpatialReuseParameterSet()) {
				results = append(results, result)
			}
		}
	}
	if len(results) == 0 {
		return
	}
	writeSection(b, "Scan BSS Coloring")
	for i, result := range results {
		if i > 0 {
			b.WriteByte('\n')
		}
		label := fmt.Sprintf("ap ssid=%s bssid=%s", empty(result.GetSsid(), "<hidden>"), empty(result.GetBssid(), "<unknown>"))
		renderWifiMLOBSSColoring(b, label, result.GetHeOperation(), result.GetHeSpatialReuseParameterSet())
	}
}

func renderWifiMLOBSSColoring(b *strings.Builder, label string, operation *controlpb.WifiHeOperation, spatialReuse *controlpb.WifiHeSpatialReuseParameterSet) {
	fmt.Fprintf(b, "  %s\n", label)
	wifiMLOWriteDetailRows(b, wifiMLOBSSColoringSummaryLines(operation, spatialReuse)...)
}

func wifiMLOHasBSSColoringContext(operation *controlpb.WifiHeOperation, spatialReuse *controlpb.WifiHeSpatialReuseParameterSet) bool {
	return operation != nil || spatialReuse != nil
}

func wifiMLOBSSColoringSummaryLines(operation *controlpb.WifiHeOperation, spatialReuse *controlpb.WifiHeSpatialReuseParameterSet) []kvRow {
	lines := []kvRow{}
	if operation != nil {
		lines = append(lines, kv("he_operation", fmt.Sprintf(
			"bss_color=%d disabled=%t partial=%t",
			operation.GetBssColor(),
			operation.GetBssColorDisabled(),
			wifiMLOContainsValue(operation.GetFlags(), "partial_bss_color"),
		)))
		if len(operation.GetWarnings()) > 0 {
			lines = append(lines, kv("he_operation_warnings", strings.Join(operation.GetWarnings(), ",")))
		}
	}
	if spatialReuse != nil {
		lines = append(lines, kv("spatial_reuse", fmt.Sprintf("control=0x%x flags=%s", spatialReuse.GetSrControl(), wifiMLOJoinStrings(spatialReuse.GetFlags(), "<none>"))))
		if spatialReuse.GetNonSrgObssPdMaxOffset() != 0 {
			lines = append(lines, kv("non_srg_obss_pd_max_offset", spatialReuse.GetNonSrgObssPdMaxOffset()))
		}
		if spatialReuse.GetSrgObssPdMinOffset() != 0 || spatialReuse.GetSrgObssPdMaxOffset() != 0 {
			lines = append(lines, kv("srg_obss_pd", fmt.Sprintf("%d/%d", spatialReuse.GetSrgObssPdMinOffset(), spatialReuse.GetSrgObssPdMaxOffset())))
		}
		if spatialReuse.GetSrgBssColorBitmapHex() != "" {
			lines = append(lines, kv("srg_bss_color_bitmap", "0x"+spatialReuse.GetSrgBssColorBitmapHex()))
		}
		if spatialReuse.GetSrgPartialBssidBitmapHex() != "" {
			lines = append(lines, kv("srg_partial_bssid_bitmap", "0x"+spatialReuse.GetSrgPartialBssidBitmapHex()))
		}
		if spatialReuse.GetTruncated() {
			lines = append(lines, kv("spatial_reuse_truncated", true))
		}
		if len(spatialReuse.GetWarnings()) > 0 {
			lines = append(lines, kv("spatial_reuse_warnings", strings.Join(spatialReuse.GetWarnings(), ",")))
		}
	}
	return lines
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
	writeRecord(b, "Elements", strings.Join(lines, "\n"))
}

func renderWifiMLOConnectedEHTPuncturing(b *strings.Builder, current *controlpb.WifiConnection) {
	if current == nil || !wifiMLOHasEHTPuncturingContext(current.GetHeCapabilities(), current.GetEhtOperation()) {
		return
	}
	writeSection(b, "Connected EHT Puncturing")
	renderWifiMLOEHTPuncturing(b, "connection", current.GetHeCapabilities(), current.GetEhtOperation())
}

func renderWifiMLOScanEHTPuncturing(b *strings.Builder, groups []wifiMLOGroup) {
	results := []*controlpb.WifiScanResult{}
	for _, group := range groups {
		for _, result := range group.results {
			if wifiMLOHasEHTPuncturingContext(result.GetHeCapabilities(), result.GetEhtOperation()) {
				results = append(results, result)
			}
		}
	}
	if len(results) == 0 {
		return
	}
	writeSection(b, "Scan EHT Puncturing")
	for i, result := range results {
		if i > 0 {
			b.WriteByte('\n')
		}
		label := fmt.Sprintf("ap ssid=%s bssid=%s", empty(result.GetSsid(), "<hidden>"), empty(result.GetBssid(), "<unknown>"))
		renderWifiMLOEHTPuncturing(b, label, result.GetHeCapabilities(), result.GetEhtOperation())
	}
}

func renderWifiMLOEHTPuncturing(b *strings.Builder, label string, he *controlpb.WifiHeCapabilities, operation *controlpb.WifiEhtOperation) {
	fmt.Fprintf(b, "  %s\n", label)
	wifiMLOWriteDetailRows(b, wifiMLOEHTPuncturingSummaryLines(he, operation)...)
}

func wifiMLOHasEHTPuncturingContext(he *controlpb.WifiHeCapabilities, operation *controlpb.WifiEhtOperation) bool {
	return he != nil || operation != nil
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
			wifiMLOWriteDetailRows(b, wifiMLOEHTMACSummaryLines(capabilities)...)
		}
		if capabilities.GetPhy() != nil {
			wifiMLOWriteDetailRows(b, wifiMLOEHTPHYSummaryLines(capabilities)...)
		}
		if len(capabilities.GetFeatures()) > 0 {
			wifiMLOWriteDetailRows(b, wifiMLOWrappedListLines("eht features", capabilities.GetFeatures())...)
		}
		if len(capabilities.GetMcsNss()) > 0 {
			wifiMLOWriteMCSNSSLines(b, "mcs_nss", capabilities.GetMcsNss())
		}
		if capabilities.GetPpeThresholdsPresent() {
			wifiMLOWriteDetailRows(b, kv("ppe", fmt.Sprintf("nss=%d ru=%s hex=0x%s", capabilities.GetPpeNssCount(), wifiMLOJoinStrings(capabilities.GetPpeRuIndices(), "<none>"), capabilities.GetPpeThresholdsHex())))
		}
		if len(capabilities.GetWarnings()) > 0 {
			wifiMLOWriteDetailRows(b, kv("cap_warnings", strings.Join(capabilities.GetWarnings(), ",")))
		}
	}
	if operation != nil {
		wifiMLOWriteDetailRows(b, wifiMLOEHTOperationSummaryLines(operation)...)
		if len(operation.GetBasicMcsNss()) > 0 {
			wifiMLOWriteMCSNSSLines(b, "basic_mcs_nss", operation.GetBasicMcsNss())
		}
		if len(operation.GetWarnings()) > 0 {
			wifiMLOWriteDetailRows(b, kv("oper_warnings", strings.Join(operation.GetWarnings(), ",")))
		}
	}
}

func wifiMLOEHTMACSummaryLines(value *controlpb.WifiEhtCapabilities) []kvRow {
	mac := value.GetMac()
	flags := []string{}
	if mac.GetEpcsPriorityAccess() {
		flags = append(flags, "epcs")
	}
	if mac.GetOmControl() {
		flags = append(flags, "om_control")
	}
	if mac.GetTriggeredTxopSharingMode1() {
		flags = append(flags, "triggered_txop_mode1")
	}
	if mac.GetTriggeredTxopSharingMode2() {
		flags = append(flags, "triggered_txop_mode2")
	}
	if mac.GetRestrictedTwt() {
		flags = append(flags, "restricted_twt")
	}
	if mac.GetScsTrafficDescription() {
		flags = append(flags, "scs_traffic_description")
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
	lines := []kvRow{kv("mac", fmt.Sprintf("max_mpdu=%d max_ampdu_ext=%d link_adapt=%s", mac.GetMaxMpduLengthBytes(), mac.GetMaxAmpduLengthExponentExtension(), empty(mac.GetLinkAdaptation(), "<unknown>")))}
	lines = append(lines, wifiMLOWrappedListLines("mac flags", flags)...)
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

func wifiMLOEHTPHYSummaryLines(value *controlpb.WifiEhtCapabilities) []kvRow {
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
	return []kvRow{
		kv("phy caps", fmt.Sprintf("320mhz=%t ru_gt20=%t ltf=max%d/extra=%t padding=%s", phy.GetSupports_320MhzIn_6Ghz(), phy.GetSupports_242ToneRuGt_20Mhz(), phy.GetMaxSupportedEhtLtf(), phy.GetExtraEhtLtfSupported(), empty(phy.GetCommonNominalPacketPadding(), "<unknown>"))),
		kv("phy beamformee_ss", fmt.Sprintf("80=%d 160=%d 320=%d", phy.GetBeamformeeSs_80Mhz(), phy.GetBeamformeeSs_160Mhz(), phy.GetBeamformeeSs_320Mhz())),
		kv("phy sounding", fmt.Sprintf("80=%d 160=%d 320=%d", phy.GetSoundingDimensions_80Mhz(), phy.GetSoundingDimensions_160Mhz(), phy.GetSoundingDimensions_320Mhz())),
		kv("phy multi-user", fmt.Sprintf("mu_mimo=%s mu_bf=%s", wifiMLOJoinStrings(muMIMO, "<none>"), wifiMLOJoinStrings(muBeamformer, "<none>"))),
		kv("phy modulation", fmt.Sprintf("mcs15=%s qam=%s", wifiMLOJoinStrings(mcs15, "<none>"), wifiMLOJoinStrings(qam, "<none>"))),
	}
}

func wifiMLOEHTOperationSummaryLines(value *controlpb.WifiEhtOperation) []kvRow {
	lines := []kvRow{
		kv("oper", fmt.Sprintf("op_info=%t width=%s width_mhz=%d code=%d", value.GetOperationInformationPresent(), empty(value.GetChannelWidth(), "<unknown>"), value.GetChannelWidthMhz(), value.GetChannelWidthCode())),
		kv("oper channels", fmt.Sprintf("ccfs0=%d ccfs1=%d mcs15_disabled=%t group_bu_exp=%d", value.GetCenterFreqSegment0(), value.GetCenterFreqSegment1(), value.GetMcs15Disabled(), value.GetGroupAddressedBuIndicationExponent())),
	}
	lines = append(lines, wifiMLOWrappedListLines("oper flags", value.GetFlags())...)
	if value.GetDisabledSubchannelBitmapPresent() || value.GetDisabledSubchannelBitmap() != 0 {
		bitmap := value.GetDisabledSubchannelBitmapHex()
		if bitmap == "" {
			bitmap = fmt.Sprintf("%04x", value.GetDisabledSubchannelBitmap())
		}
		lines = append(lines, kv("oper disabled", fmt.Sprintf("disabled=0x%s punctured=%s", bitmap, wifiMLOJoinUint32s(value.GetDisabledSubchannelIndices(), "<none>"))))
	}
	return lines
}

func wifiMLOEHTPuncturingSummaryLines(he *controlpb.WifiHeCapabilities, operation *controlpb.WifiEhtOperation) []kvRow {
	lines := []kvRow{
		kv("he_preamble_puncturing_rx", wifiMLOHEPreamblePuncturingRX(he)),
		kv("he_punctured_sounding", wifiMLOHEPuncturedSounding(he)),
		kv("eht_disabled_subchannel_bitmap", wifiMLOEHTDisabledSubchannelBitmap(operation)),
	}
	return lines
}

func wifiMLOHEPreamblePuncturingRX(he *controlpb.WifiHeCapabilities) string {
	if he == nil || he.GetPhy() == nil {
		return "<unknown>"
	}
	return wifiMLOJoinStrings(he.GetPhy().GetPreamblePuncturingRx(), "<none>")
}

func wifiMLOHEPuncturedSounding(he *controlpb.WifiHeCapabilities) string {
	if he == nil || he.GetMac() == nil {
		return "<unknown>"
	}
	return fmt.Sprint(he.GetMac().GetPuncturedSounding())
}

func wifiMLOEHTDisabledSubchannelBitmap(operation *controlpb.WifiEhtOperation) string {
	if operation == nil {
		return "<unknown>"
	}
	if !operation.GetDisabledSubchannelBitmapPresent() && operation.GetDisabledSubchannelBitmap() == 0 {
		return "absent"
	}
	bitmap := operation.GetDisabledSubchannelBitmapHex()
	if bitmap == "" {
		bitmap = fmt.Sprintf("%04x", operation.GetDisabledSubchannelBitmap())
	}
	return fmt.Sprintf("0x%s punctured=%s", bitmap, wifiMLOJoinUint32s(operation.GetDisabledSubchannelIndices(), "<none>"))
}

func wifiMLOGroupBSSColors(group wifiMLOGroup) string {
	values := []string{}
	for _, result := range group.results {
		values = append(values, wifiMLOHEOperationBSSColorValue(result.GetHeOperation()))
	}
	return wifiMLOJoinStrings(values, "-")
}

func wifiMLOHEOperationBSSColorValue(operation *controlpb.WifiHeOperation) string {
	if operation == nil {
		return ""
	}
	if operation.GetBssColorDisabled() {
		return fmt.Sprintf("%d(off)", operation.GetBssColor())
	}
	if wifiMLOContainsValue(operation.GetFlags(), "partial_bss_color") {
		return fmt.Sprintf("%d(part)", operation.GetBssColor())
	}
	return fmt.Sprint(operation.GetBssColor())
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
		wifiMLOWriteDetailRows(b, kv(fmt.Sprintf("%s %s/%s/%s", label, k.standard, k.bandwidth, k.mcsRange), fmt.Sprintf("rx=nss%d tx=nss%d", rx, tx)))
	}
}

func renderWifiMLOScanLinks(b *strings.Builder, groups []wifiMLOGroup, current *controlpb.WifiConnection) {
	if len(groups) == 0 {
		return
	}
	writeSection(b, "EHT Scan Links")
	first := true
	for _, group := range groups {
		for _, result := range group.results {
			if !first {
				b.WriteByte('\n')
			}
			first = false
			renderWifiMLOScanLinkBlock(b, group, result, current)
			renderWifiMLOAffiliatedLinkBlocks(b, group, result, current)
		}
	}
}

func renderWifiMLOScanLinkBlock(b *strings.Builder, group wifiMLOGroup, result *controlpb.WifiScanResult, current *controlpb.WifiConnection) {
	fmt.Fprintf(b, "[%s] %s\n", wifiMLOBlockMark(wifiMLOResultMark(group, result, current)), empty(result.GetSsid(), "<hidden>"))
	writeRecord(b, "Identity", fmt.Sprintf("type=ap ap_mld=%s link=%s bssid=%s", group.displayMLD, wifiMLOScanLinkID(result), empty(result.GetBssid(), "<unknown>")))
	writeRecord(b, "Radio", fmt.Sprintf("band=%s ch=%s freq=%dMHz width=%s%s rssi=%ddBm",
		empty(result.GetBand(), wifiBandFromFrequency(result.GetFrequencyMhz())),
		wifiChannelFromFrequency(result.GetFrequencyMhz()),
		result.GetFrequencyMhz(),
		empty(wifiMLOScanChannelWidth(result.GetChannelWidth()), "<unknown>"),
		wifiMLOScanEHTOperationSuffix(result),
		result.GetRssiDbm(),
	))
	writeRecord(b, "Information elements", wifiMLOInformationElementChecklist(result))
	writeRecord(b, "SDK flags", wifiMLOScanSDKFlags(result))
}

func renderWifiMLOAffiliatedLinkBlocks(b *strings.Builder, group wifiMLOGroup, result *controlpb.WifiScanResult, current *controlpb.WifiConnection) {
	if len(result.GetAffiliatedMloLinks()) == 0 {
		return
	}
	fmt.Fprintf(b, "  affiliated_links\n")
	for _, link := range result.GetAffiliatedMloLinks() {
		renderWifiMLOAffiliatedLinkBlock(b, group, result, link, current)
	}
}

func renderWifiMLOAffiliatedLinkBlock(b *strings.Builder, group wifiMLOGroup, result *controlpb.WifiScanResult, link *controlpb.MloLinkInfo, current *controlpb.WifiConnection) {
	fmt.Fprintf(b, "    [%s] affiliated link\n", wifiMLOBlockMark(wifiMLOLinkMark(group, link, current)))
	writeRecord(b, "    Identity", fmt.Sprintf("type=aff link=%d ap_mac=%s", link.GetLinkId(), empty(link.GetApMacAddress(), "<unknown>")))
	writeRecord(b, "    Radio", fmt.Sprintf("band=%s ch=%d state=%s rssi=%ddBm tx=%d rx=%d max_tx=%d max_rx=%d parent_bssid=%s", empty(link.GetBand(), "<unknown>"), link.GetChannel(), empty(link.GetState(), "<unknown>"), link.GetRssiDbm(), link.GetTxLinkSpeedMbps(), link.GetRxLinkSpeedMbps(), link.GetMaxSupportedTxLinkSpeedMbps(), link.GetMaxSupportedRxLinkSpeedMbps(), empty(result.GetBssid(), "<unknown>")))
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
			writeRecord(b, "Error", err)
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
		warnings = append(warnings, "eht_scan_results=0")
	}
	if current != nil && wifiMLOComparison(current, scan, candidates) == "updated_scan_age_le_10s" {
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
	if current != nil && wifiMLOComparison(current, scan, candidates) == "updated_scan_age_le_10s" {
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
			warnings = append(warnings, "associated_link_missing_from_scan ids="+wifiMLOJoinIntSet(missing))
		}
	}
	writeSection(b, "Diagnostics / Warnings")
	if len(warnings) == 0 {
		writeRecord(b, "Warnings", "none")
		return
	}
	for _, warning := range wifiMLOUniqueStrings(warnings) {
		writeRecord(b, "Warning", warning)
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
		if (diagnosticFieldMap(result.GetObservationFields())["ap_mlo_link_id.state"] == "available" && result.GetApMloLinkId() >= 0) || wifiMLOCurrentLinkIDFromElements(result.GetInformationElements()) != nil {
			linkIDSeen++
		}
		if !wifiMLOScanHasMetadata(result) {
			withoutMetadata++
		}
	}
	switch {
	case apMLDSeen == 0 && linkIDSeen == 0:
		return []string{fmt.Sprintf("scan_mlo_metadata_unavailable 11be_results=%d observed_ap_mld=0 observed_link_id=0", len(beResults))}
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
	return (diagnosticFieldMap(result.GetObservationFields())["ap_mlo_link_id.state"] == "available" && result.GetApMloLinkId() >= 0) ||
		result.GetApMldMacAddress() != "" ||
		len(result.GetAffiliatedMloLinks()) > 0 ||
		wifiMLOHasElement(result.GetInformationElements())
}

func wifiMLOConnectionHasEHTDetails(conn *controlpb.WifiConnection) bool {
	return conn.GetEhtCapabilities() != nil || conn.GetEhtOperation() != nil
}

func wifiMLOScanHasEHTDetails(result *controlpb.WifiScanResult) bool {
	return result.GetEhtCapabilities() != nil || result.GetEhtOperation() != nil
}

func wifiMLOConnectionHasHEDetails(conn *controlpb.WifiConnection) bool {
	return conn.GetHeCapabilities() != nil ||
		conn.GetHeOperation() != nil ||
		conn.GetHeUoraParameterSet() != nil ||
		conn.GetHeMuEdcaParameterSet() != nil
}

func wifiMLOScanHasHEDetails(result *controlpb.WifiScanResult) bool {
	return result.GetHeCapabilities() != nil ||
		result.GetHeOperation() != nil ||
		result.GetHeUoraParameterSet() != nil ||
		result.GetHeMuEdcaParameterSet() != nil
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
	return wifiMLOKnownBSSID(left) && wifiMLOKnownBSSID(right) && strings.EqualFold(left, right)
}

func wifiMLOResultMark(group wifiMLOGroup, result *controlpb.WifiScanResult, current *controlpb.WifiConnection) string {
	if current == nil {
		if wifiMLOScanHasMetadata(result) {
			return "-"
		}
		return ""
	}
	if diagnosticFieldMap(current.GetObservationFields())["identity.state"] == "available" && bssidEqual(result.GetBssid(), current.GetBssid()) {
		return "*"
	}
	for _, candidate := range group.results {
		if wifiMLOSameMLD(current, candidate) {
			return "+"
		}
	}
	if wifiMLOScanHasMetadata(result) {
		return "-"
	}
	return ""
}

func wifiMLOLinkMark(group wifiMLOGroup, link *controlpb.MloLinkInfo, current *controlpb.WifiConnection) string {
	if current == nil {
		return "-"
	}
	if diagnosticFieldMap(current.GetObservationFields())["identity.state"] == "available" && bssidEqual(link.GetApMacAddress(), current.GetBssid()) {
		return "*"
	}
	for _, result := range group.results {
		if wifiMLOSameMLD(current, result) {
			return "+"
		}
	}
	return "-"
}

func wifiMLOBlockMark(mark string) string {
	if mark == "" {
		return " "
	}
	return mark
}

func wifiMLOConnectionLinkID(conn *controlpb.WifiConnection) string {
	if diagnosticFieldMap(conn.GetObservationFields())["ap_mlo_link_id.state"] != "" {
		return observedValue(conn.GetObservationFields(), "ap_mlo_link_id", fmt.Sprint(conn.GetApMloLinkId()))
	}
	if id := wifiMLOCurrentLinkIDFromElements(conn.GetInformationElements()); id != nil {
		return fmt.Sprint(*id)
	}
	return "? (link ID presence unavailable)"
}

func wifiMLOScanLinkID(result *controlpb.WifiScanResult) string {
	return scanMLOLinkID(result)
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
	if diagnosticFieldMap(result.GetObservationFields())["ap_mlo_link_id.state"] == "available" && result.GetApMloLinkId() >= 0 {
		ids[result.GetApMloLinkId()] = true
	}
	for _, link := range result.GetAffiliatedMloLinks() {
		if diagnosticFieldMap(link.GetObservationFields())["identity.state"] == "available" && link.GetLinkId() >= 0 {
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
	if diagnosticFieldMap(conn.GetObservationFields())["ap_mlo_link_id.state"] == "available" && conn.GetApMloLinkId() >= 0 {
		ids[conn.GetApMloLinkId()] = true
	}
	for _, link := range conn.GetAssociatedMloLinks() {
		if diagnosticFieldMap(link.GetObservationFields())["identity.state"] == "available" && link.GetLinkId() >= 0 {
			ids[link.GetLinkId()] = true
		}
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

func wifiMLOWrappedListLines(label string, values []string) []kvRow {
	return []kvRow{kv(label, wifiMLOJoinStrings(wifiMLOUniqueStrings(values), "<none>"))}
}

func wifiMLOWriteDetailRows(b *strings.Builder, rows ...kvRow) {
	for _, row := range rows {
		writeRecord(b, "  "+row.label, row.value)
	}
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
		displayColumns[i] = displayTableColumn{header: column.header, maxWidth: column.maxWidth}
	}
	writeDisplayTable(b, displayColumns, rows)
}

func wifiMLODisplayWidth(value string) int {
	return displayWidth(value)
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

func wifiMLOJoinIntSet(values map[int32]bool) string {
	return wifiMLOJoinInts(wifiMLOSortedIntSet(values), "<none>")
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
		key := value
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, value)
	}
	return out
}
