package render

import (
	"fmt"
	"sort"
	"strings"
	"text/tabwriter"
	"unicode"

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
	renderWifiMLONetworks(b, diagnostics.GetNetworks())
	renderWifiMLOScanSummary(b, scan, candidates)
	renderWifiMLONearbyAPs(b, groups, current)
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
		kv("connected_ap_mld", empty(current.GetApMldMacAddress(), "<none>")),
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
		kv("ap_mld", empty(current.GetApMldMacAddress(), "<none>")),
		kv("ap_link_id", wifiMLOConnectionLinkID(current)),
		kv("affiliated", len(current.GetAffiliatedMloLinks())),
		kv("associated", len(current.GetAssociatedMloLinks())),
	)
	renderMLOLinks(b, "Associated MLO Links", current.GetAssociatedMloLinks())
	renderMLOLinks(b, "Affiliated MLO Links", current.GetAffiliatedMloLinks())
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
	tw := tabwriter.NewWriter(b, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "NETWORK\tACTIVE\tSSID\tBSSID\tAP_MLD\tAP_LINK\tASSOC\tAFFIL\tSTANDARD")
	for _, network := range rows {
		conn := network.GetIpStatus().GetWifi()
		_, _ = fmt.Fprintf(tw, "%s\t%t\t%s\t%s\t%s\t%s\t%d\t%d\t%s\n",
			empty(network.GetNetworkId(), "<unknown>"),
			network.GetActive(),
			empty(conn.GetSsid(), "<hidden>"),
			empty(conn.GetBssid(), "<unknown>"),
			empty(conn.GetApMldMacAddress(), "<none>"),
			wifiMLOConnectionLinkID(conn),
			len(conn.GetAssociatedMloLinks()),
			len(conn.GetAffiliatedMloLinks()),
			empty(conn.GetWifiStandard(), "<unknown>"),
		)
	}
	_ = tw.Flush()
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
		kv("source", "diagnostics"),
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

func renderWifiMLONearbyAPs(b *strings.Builder, groups []wifiMLOGroup, current *controlpb.WifiConnection) {
	writeSection(b, "Nearby MLO APs")
	if len(groups) == 0 {
		b.WriteString("  no MLO-capable scan results\n")
		return
	}
	columns := []wifiMLOTableColumn{
		{header: "MARK", maxWidth: 4},
		{header: "SSID", maxWidth: 24},
		{header: "BANDS", maxWidth: 18},
		{header: "RSSI", maxWidth: 4},
		{header: "SECURITY", maxWidth: 12},
		{header: "STANDARD", maxWidth: 8},
	}
	rows := [][]string{}
	for _, group := range groups {
		rows = append(rows, []string{
			wifiMLOGroupMark(group, current),
			wifiMLOJoinStrings(wifiMLOGroupSSIDs(group), "<hidden>"),
			wifiMLOJoinStrings(group.bands, "unknown"),
			fmt.Sprint(group.bestRSSI),
			wifiMLOJoinStrings(group.security, "-"),
			wifiMLOJoinStrings(group.standards, "-"),
		})
	}
	wifiMLOWriteTable(b, columns, rows)
	renderWifiMLOScanLinks(b, groups, current)
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
	fmt.Fprintf(b, "  band=%s ch=%s freq=%dMHz width=%s rssi=%ddBm\n",
		empty(result.GetBand(), wifiBandFromFrequency(result.GetFrequencyMhz())),
		wifiChannelFromFrequency(result.GetFrequencyMhz()),
		result.GetFrequencyMhz(),
		empty(wifiMLOScanChannelWidth(result.GetChannelWidth()), "<unknown>"),
		result.GetRssiDbm(),
	)
}

func renderWifiMLOAffiliatedLinkBlock(b *strings.Builder, group wifiMLOGroup, result *controlpb.WifiScanResult, link *controlpb.MloLinkInfo, current *controlpb.WifiConnection) {
	fmt.Fprintf(b, "[%s] affiliated %s\n", wifiMLOBlockMark(wifiMLOLinkMark(group, link, current)), empty(result.GetSsid(), "<hidden>"))
	fmt.Fprintf(b, "  ap_mld=%s\n", group.displayMLD)
	fmt.Fprintf(b, "  link=%d parent_bssid=%s\n", link.GetLinkId(), empty(result.GetBssid(), "<unknown>"))
	fmt.Fprintf(b, "  band=%s ch=%d state=%s rssi=%ddBm ap_mac=%s\n", empty(link.GetBand(), "<unknown>"), link.GetChannel(), empty(link.GetState(), "<unknown>"), link.GetRssiDbm(), empty(link.GetApMacAddress(), "<unknown>"))
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
		if result.GetApMldMacAddress() != "" {
			apMLDSeen++
		}
		if result.GetApMloLinkId() >= 0 {
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
		len(result.GetAffiliatedMloLinks()) > 0
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
	if mld := strings.ToLower(strings.TrimSpace(result.GetApMldMacAddress())); mld != "" {
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
		if group.displayMLD == "<unknown>" && result.GetApMldMacAddress() != "" {
			group.displayMLD = result.GetApMldMacAddress()
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
	currentMLD := strings.TrimSpace(conn.GetApMldMacAddress())
	if currentMLD != "" && strings.EqualFold(currentMLD, strings.TrimSpace(result.GetApMldMacAddress())) {
		return true
	}
	return bssidEqual(conn.GetBssid(), result.GetBssid())
}

func bssidEqual(left string, right string) bool {
	return left != "" && right != "" && strings.EqualFold(left, right)
}

func wifiMLOGroupMark(group wifiMLOGroup, current *controlpb.WifiConnection) string {
	if current == nil {
		return ""
	}
	for _, result := range group.results {
		if bssidEqual(result.GetBssid(), current.GetBssid()) {
			return "*"
		}
	}
	for _, result := range group.results {
		if wifiMLOSameMLD(current, result) {
			return "+"
		}
	}
	return ""
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
	if wifiConnectionHasMLO(conn) && conn.GetApMloLinkId() >= 0 {
		return fmt.Sprint(conn.GetApMloLinkId())
	}
	return "<none>"
}

func wifiMLOScanLinkID(result *controlpb.WifiScanResult) string {
	explicitMLO := wifiMLOScanHasMetadata(result)
	switch {
	case (explicitMLO || strings.EqualFold(result.GetWifiStandard(), "802.11be")) && result.GetApMloLinkId() >= 0:
		return fmt.Sprint(result.GetApMloLinkId())
	case strings.EqualFold(result.GetWifiStandard(), "802.11be"):
		return "<unknown>"
	default:
		return "<none>"
	}
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
	return ids
}

func wifiMLOScanSecurity(result *controlpb.WifiScanResult) string {
	if values := result.GetSecurityTypes(); len(values) > 0 {
		return strings.Join(values, ",")
	}
	return result.GetCapabilities()
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
	preparedHeaders := make([]string, len(columns))
	for i, column := range columns {
		preparedHeaders[i] = wifiMLOFitCell(column.header, column.maxWidth)
	}
	preparedRows := make([][]string, 0, len(rows))
	for _, row := range rows {
		prepared := make([]string, len(columns))
		for i, column := range columns {
			value := ""
			if i < len(row) {
				value = row[i]
			}
			prepared[i] = wifiMLOFitCell(value, column.maxWidth)
		}
		preparedRows = append(preparedRows, prepared)
	}

	widths := make([]int, len(columns))
	for i := range columns {
		widths[i] = wifiMLODisplayWidth(preparedHeaders[i])
		for _, row := range preparedRows {
			if width := wifiMLODisplayWidth(row[i]); width > widths[i] {
				widths[i] = width
			}
		}
	}

	wifiMLOWriteTableRow(b, preparedHeaders, widths)
	for _, row := range preparedRows {
		wifiMLOWriteTableRow(b, row, widths)
	}
}

func wifiMLOWriteTableRow(b *strings.Builder, row []string, widths []int) {
	for i, value := range row {
		if i > 0 {
			b.WriteString("  ")
		}
		b.WriteString(wifiMLOPadDisplayEnd(value, widths[i]))
	}
	b.WriteByte('\n')
}

func wifiMLOFitCell(value string, maxWidth int) string {
	cleaned := strings.ReplaceAll(value, "\t", " ")
	if maxWidth <= 0 || wifiMLODisplayWidth(cleaned) <= maxWidth {
		return cleaned
	}
	if maxWidth <= 3 {
		return strings.Repeat(".", maxWidth)
	}

	suffix := "..."
	targetWidth := maxWidth - wifiMLODisplayWidth(suffix)
	var out strings.Builder
	width := 0
	for _, r := range cleaned {
		runeWidth := wifiMLORuneDisplayWidth(r)
		if width+runeWidth > targetWidth {
			break
		}
		out.WriteRune(r)
		width += runeWidth
	}
	out.WriteString(suffix)
	return out.String()
}

func wifiMLOPadDisplayEnd(value string, width int) string {
	padding := width - wifiMLODisplayWidth(value)
	if padding <= 0 {
		return value
	}
	return value + strings.Repeat(" ", padding)
}

func wifiMLODisplayWidth(value string) int {
	width := 0
	for _, r := range value {
		width += wifiMLORuneDisplayWidth(r)
	}
	return width
}

func wifiMLORuneDisplayWidth(r rune) int {
	if unicode.IsControl(r) || unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || unicode.Is(unicode.Mc, r) {
		return 0
	}
	if unicode.In(r,
		unicode.Han,
		unicode.Hiragana,
		unicode.Katakana,
		unicode.Hangul,
	) || (r >= 0xff01 && r <= 0xff60) || (r >= 0xffe0 && r <= 0xffe6) {
		return 2
	}
	return 1
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

func wifiMLOJoinIntSet(values map[int32]bool, emptyValue string) string {
	return wifiMLOJoinInts(wifiMLOSortedIntSet(values), emptyValue)
}

func wifiMLOSortedIntSet(values map[int32]bool) []int32 {
	out := make([]int32, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
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
