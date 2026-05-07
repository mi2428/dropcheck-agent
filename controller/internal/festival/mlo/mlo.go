// Package mlo provides Dropcheck Festival expectations for Wi-Fi 7 MLO diagnostics.
package mlo

import (
	"fmt"
	"slices"
	"strings"

	"dropcheck/controller/internal/controlpb"
	"dropcheck/controller/internal/festival"
)

// Result is the MLO-specific view passed to custom assertions.
type Result struct {
	// Raw is the original Wi-Fi diagnostics payload for callers that need every field.
	Raw *controlpb.WifiDiagnostics
	// Status is the outer command status returned by the agent.
	Status controlpb.CommandResult_Status
	// Connection is the connected Wi-Fi status, if present.
	Connection Connection
	// ScanCandidates are cached scan results that report MLO capability or metadata.
	ScanCandidates []ScanCandidate
	// Groups are MLO scan candidates grouped by AP MLD when known, otherwise by BSSID.
	Groups []Group
}

// Connection is normalized connected MLO state.
type Connection struct {
	Raw             *controlpb.WifiConnection
	SSID            string
	BSSID           string
	Standard        string
	APMLDMacAddress string
	APMLOLinkID     int32
	AssociatedLinks []*controlpb.MloLinkInfo
	AffiliatedLinks []*controlpb.MloLinkInfo
	MLOPresent      bool
}

// ScanCandidate is one MLO-capable scan result.
type ScanCandidate struct {
	Raw             *controlpb.WifiScanResult
	SSID            string
	BSSID           string
	Standard        string
	RSSIDbm         int32
	APMLDMacAddress string
	APMLOLinkID     int32
	AffiliatedLinks []*controlpb.MloLinkInfo
	LinkIDs         []int32
}

// Group is a set of scan candidates that represent one MLO AP group.
type Group struct {
	Key             string
	APMLDMacAddress string
	SSIDs           []string
	Results         []ScanCandidate
	LinkIDs         []int32
	BestRSSIDbm     int32
}

// Connected returns matchers for connected MLO state.
func Connected() ConnectedSelector {
	return ConnectedSelector{}
}

// Scan returns matchers for MLO scan candidates.
func Scan() ScanSelector {
	return ScanSelector{}
}

// CurrentRelation returns matchers for the connected AP's relation to scan results.
func CurrentRelation() CurrentRelationSelector {
	return CurrentRelationSelector{}
}

// Metadata returns matchers for MLO metadata quality.
func Metadata() MetadataSelector {
	return MetadataSelector{}
}

// Assert evaluates a custom MLO assertion against the typed result view.
func Assert(name string, fn func(Result) error) festival.Expectation {
	return assertion{name: name, fn: fn}
}

type assertion struct {
	name string
	fn   func(Result) error
}

func (a assertion) Evaluate(result festival.Result) []festival.Finding {
	mlo, ok, reason := from(result)
	metric := "mlo.assert." + a.name
	if !ok {
		return []festival.Finding{festival.Fail(metric, "<missing>", "custom assertion passed", reason)}
	}
	if err := a.fn(mlo); err != nil {
		return []festival.Finding{festival.Fail(metric, "failed", "custom assertion passed", err.Error())}
	}
	return []festival.Finding{festival.Pass(metric, "passed", "custom assertion passed")}
}

// ConnectedSelector exposes connected MLO matchers.
type ConnectedSelector struct{}

// Present requires the connected Wi-Fi status to report MLO identity.
func (ConnectedSelector) Present() festival.Expectation {
	return boolExpectation{
		metric:   "mlo.connected.present",
		expected: true,
		observe: func(r Result) (bool, bool, string) {
			return r.Connection.MLOPresent, r.Connection.Raw != nil, "wifi diagnostics do not contain connection"
		},
	}
}

// APMLD returns matchers for the connected AP MLD MAC address.
func (ConnectedSelector) APMLD() StringField {
	return StringField{
		metric: "mlo.connected.ap_mld",
		observe: func(r Result) (string, bool, string) {
			if r.Connection.Raw == nil {
				return "", false, "wifi diagnostics do not contain connection"
			}
			if r.Connection.APMLDMacAddress == "" {
				return "", false, "ap mld mac address is empty"
			}
			return r.Connection.APMLDMacAddress, true, ""
		},
	}
}

// APMLOLinkID returns matchers for the connected AP MLO link ID.
func (ConnectedSelector) APMLOLinkID() IntField {
	return IntField{
		metric: "mlo.connected.ap_mlo_link_id",
		observe: func(r Result) (int32, bool, string) {
			if r.Connection.Raw == nil {
				return 0, false, "wifi diagnostics do not contain connection"
			}
			if !r.Connection.MLOPresent {
				return 0, false, "mlo is not present"
			}
			if r.Connection.APMLOLinkID < 0 {
				return 0, false, "ap mlo link id is unknown"
			}
			return r.Connection.APMLOLinkID, true, ""
		},
	}
}

// AssociatedLinkCount matches the number of associated connected MLO links.
func (ConnectedSelector) AssociatedLinkCount() festival.OrderedMetric[int] {
	return festival.Ordered[int]("mlo.connected.associated_link_count", func(result festival.Result) (int, bool, string) {
		mlo, ok, reason := from(result)
		return len(mlo.Connection.AssociatedLinks), ok, reason
	})
}

// AffiliatedLinkCount matches the number of affiliated connected MLO links.
func (ConnectedSelector) AffiliatedLinkCount() festival.OrderedMetric[int] {
	return festival.Ordered[int]("mlo.connected.affiliated_link_count", func(result festival.Result) (int, bool, string) {
		mlo, ok, reason := from(result)
		return len(mlo.Connection.AffiliatedLinks), ok, reason
	})
}

// ScanSelector exposes scan-derived MLO matchers.
type ScanSelector struct{}

// CandidateCount matches the number of MLO-capable scan candidates.
func (ScanSelector) CandidateCount() festival.OrderedMetric[int] {
	return festival.Ordered[int]("mlo.scan.candidate_count", func(result festival.Result) (int, bool, string) {
		mlo, ok, reason := from(result)
		return len(mlo.ScanCandidates), ok, reason
	})
}

// Group starts a selector for MLO scan groups.
func (ScanSelector) Group() GroupSelector {
	return GroupSelector{}
}

// GroupSelector filters MLO scan groups by group fields.
type GroupSelector struct {
	ssid  string
	apMLD string
}

// SSID restricts matches to groups containing one SSID.
func (s GroupSelector) SSID(value string) GroupSelector {
	s.ssid = value
	return s
}

// APMLD restricts matches to one AP MLD MAC address.
func (s GroupSelector) APMLD(value string) GroupSelector {
	s.apMLD = strings.ToLower(strings.TrimSpace(value))
	return s
}

// Exists requires at least one MLO scan group to match the selector.
func (s GroupSelector) Exists() festival.Expectation {
	return groupExists{selector: s}
}

// Count matches the number of selected MLO scan groups.
func (s GroupSelector) Count() festival.OrderedMetric[int] {
	return festival.Ordered[int]("mlo.scan.group_count", func(result festival.Result) (int, bool, string) {
		mlo, ok, reason := from(result)
		if !ok {
			return 0, false, reason
		}
		return len(s.matches(mlo.Groups)), true, ""
	})
}

// LinkCount matches the largest link count among selected MLO scan groups.
func (s GroupSelector) LinkCount() festival.OrderedMetric[int] {
	return festival.Ordered[int]("mlo.scan.group_link_count", func(result festival.Result) (int, bool, string) {
		mlo, ok, reason := from(result)
		if !ok {
			return 0, false, reason
		}
		maxLinks := 0
		for _, group := range s.matches(mlo.Groups) {
			if count := len(group.LinkIDs); count > maxLinks {
				maxLinks = count
			}
		}
		return maxLinks, true, ""
	})
}

type groupExists struct {
	selector GroupSelector
}

func (e groupExists) Evaluate(result festival.Result) []festival.Finding {
	mlo, ok, reason := from(result)
	if !ok {
		return []festival.Finding{festival.Fail("mlo.scan.group", "<missing>", "exists", reason)}
	}
	matches := e.selector.matches(mlo.Groups)
	if len(matches) > 0 {
		return []festival.Finding{festival.Pass("mlo.scan.group", describeGroup(matches[0]), "exists")}
	}
	return []festival.Finding{festival.Fail("mlo.scan.group", describeGroups(mlo.Groups), "exists", "no mlo scan group matched selector")}
}

func (s GroupSelector) matches(groups []Group) []Group {
	matches := make([]Group, 0, len(groups))
	for _, group := range groups {
		if s.matchesOne(group) {
			matches = append(matches, group)
		}
	}
	return matches
}

func (s GroupSelector) matchesOne(group Group) bool {
	if s.ssid != "" && !slices.Contains(group.SSIDs, s.ssid) {
		return false
	}
	if s.apMLD != "" && strings.ToLower(strings.TrimSpace(group.APMLDMacAddress)) != s.apMLD {
		return false
	}
	return true
}

// CurrentRelationSelector exposes connected-vs-scan relation matchers.
type CurrentRelationSelector struct{}

// ConnectedMLDSeenInScan matches whether the connected AP MLD appears in MLO scan candidates.
func (CurrentRelationSelector) ConnectedMLDSeenInScan() festival.BoolMetric {
	return festival.Bool("mlo.relation.connected_mld_seen_in_scan", func(result festival.Result) (bool, bool, string) {
		mlo, ok, reason := from(result)
		if !ok {
			return false, false, reason
		}
		return connectedMLDSeenInScan(mlo), true, ""
	})
}

// AssociatedLinksCoveredByScan matches whether every connected associated link is visible in scan.
func (CurrentRelationSelector) AssociatedLinksCoveredByScan() festival.BoolMetric {
	return festival.Bool("mlo.relation.associated_links_covered_by_scan", func(result festival.Result) (bool, bool, string) {
		mlo, ok, reason := from(result)
		if !ok {
			return false, false, reason
		}
		return associatedLinksCoveredByScan(mlo), true, ""
	})
}

// MetadataSelector exposes MLO metadata-quality matchers.
type MetadataSelector struct{}

// Complete matches whether every MLO scan candidate has both AP MLD and link ID metadata.
func (MetadataSelector) Complete() festival.BoolMetric {
	return festival.Bool("mlo.metadata.complete", func(result festival.Result) (bool, bool, string) {
		mlo, ok, reason := from(result)
		if !ok {
			return false, false, reason
		}
		return scanMetadataComplete(mlo.ScanCandidates), true, ""
	})
}

// StringField is a string-valued MLO matcher with presence and equality checks.
type StringField struct {
	metric  string
	observe func(Result) (string, bool, string)
}

// Known requires the field to be present and non-empty.
func (f StringField) Known() festival.Expectation {
	return stringExpectation{field: f, op: "known", pass: func(got string) bool { return got != "" }}
}

// Eq requires the field to equal value. String comparisons are case-insensitive.
func (f StringField) Eq(value string) festival.Expectation {
	return stringExpectation{field: f, op: "== " + value, pass: func(got string) bool { return strings.EqualFold(got, value) }}
}

type stringExpectation struct {
	field StringField
	op    string
	pass  func(string) bool
}

func (e stringExpectation) Evaluate(result festival.Result) []festival.Finding {
	mlo, ok, reason := from(result)
	if !ok {
		return []festival.Finding{festival.Fail(e.field.metric, "<missing>", e.op, reason)}
	}
	value, ok, reason := e.field.observe(mlo)
	if !ok {
		return []festival.Finding{festival.Fail(e.field.metric, "<missing>", e.op, reason)}
	}
	if e.pass(value) {
		return []festival.Finding{festival.Pass(e.field.metric, value, e.op)}
	}
	return []festival.Finding{festival.Fail(e.field.metric, value, e.op, "constraint failed")}
}

// IntField is an int-valued MLO matcher with presence and equality checks.
type IntField struct {
	metric  string
	observe func(Result) (int32, bool, string)
}

// Known requires the field to be present.
func (f IntField) Known() festival.Expectation {
	return intExpectation{field: f, op: "known", pass: func(int32) bool { return true }}
}

// Eq requires the field to equal value.
func (f IntField) Eq(value int32) festival.Expectation {
	return intExpectation{field: f, op: fmt.Sprintf("== %d", value), pass: func(got int32) bool { return got == value }}
}

type intExpectation struct {
	field IntField
	op    string
	pass  func(int32) bool
}

func (e intExpectation) Evaluate(result festival.Result) []festival.Finding {
	mlo, ok, reason := from(result)
	if !ok {
		return []festival.Finding{festival.Fail(e.field.metric, "<missing>", e.op, reason)}
	}
	value, ok, reason := e.field.observe(mlo)
	if !ok {
		return []festival.Finding{festival.Fail(e.field.metric, "<missing>", e.op, reason)}
	}
	if e.pass(value) {
		return []festival.Finding{festival.Pass(e.field.metric, fmt.Sprint(value), e.op)}
	}
	return []festival.Finding{festival.Fail(e.field.metric, fmt.Sprint(value), e.op, "constraint failed")}
}

type boolExpectation struct {
	metric   string
	expected bool
	observe  func(Result) (bool, bool, string)
}

func (e boolExpectation) Evaluate(result festival.Result) []festival.Finding {
	mlo, ok, reason := from(result)
	if !ok {
		return []festival.Finding{festival.Fail(e.metric, "<missing>", fmt.Sprintf("== %t", e.expected), reason)}
	}
	value, ok, reason := e.observe(mlo)
	if !ok {
		return []festival.Finding{festival.Fail(e.metric, "<missing>", fmt.Sprintf("== %t", e.expected), reason)}
	}
	if value == e.expected {
		return []festival.Finding{festival.Pass(e.metric, fmt.Sprintf("%t", value), fmt.Sprintf("== %t", e.expected))}
	}
	return []festival.Finding{festival.Fail(e.metric, fmt.Sprintf("%t", value), fmt.Sprintf("== %t", e.expected), "constraint failed")}
}

func from(result festival.Result) (Result, bool, string) {
	raw := result.Run.Raw
	if raw == nil {
		return Result{}, false, "raw result is nil"
	}
	diagnostics := raw.GetWifiDiagnostics()
	if diagnostics == nil {
		return Result{}, false, fmt.Sprintf("command payload is %T, not wifi diagnostics", raw.GetPayload())
	}
	conn := normalizeConnection(diagnostics.GetStatus().GetConnection())
	candidates := normalizeScanCandidates(diagnostics.GetScan().GetResults())
	return Result{
		Raw:            diagnostics,
		Status:         raw.GetStatus(),
		Connection:     conn,
		ScanCandidates: candidates,
		Groups:         groupCandidates(candidates),
	}, true, ""
}

func normalizeConnection(conn *controlpb.WifiConnection) Connection {
	if conn == nil {
		return Connection{}
	}
	standard := normalizeStandard(conn.GetWifiStandard())
	out := Connection{
		Raw:             conn,
		SSID:            conn.GetSsid(),
		BSSID:           conn.GetBssid(),
		Standard:        standard,
		APMLDMacAddress: conn.GetApMldMacAddress(),
		APMLOLinkID:     conn.GetApMloLinkId(),
		AssociatedLinks: conn.GetAssociatedMloLinks(),
		AffiliatedLinks: conn.GetAffiliatedMloLinks(),
	}
	out.MLOPresent = out.APMLDMacAddress != "" ||
		len(out.AssociatedLinks) > 0 ||
		len(out.AffiliatedLinks) > 0 ||
		(out.Standard == "be" && out.APMLOLinkID >= 0)
	return out
}

func normalizeScanCandidates(results []*controlpb.WifiScanResult) []ScanCandidate {
	candidates := make([]ScanCandidate, 0, len(results))
	for _, result := range results {
		candidate := normalizeScanCandidate(result)
		if scanCandidateMLOCapable(candidate) {
			candidates = append(candidates, candidate)
		}
	}
	return candidates
}

func normalizeScanCandidate(result *controlpb.WifiScanResult) ScanCandidate {
	if result == nil {
		return ScanCandidate{}
	}
	standard := normalizeStandard(result.GetWifiStandard())
	out := ScanCandidate{
		Raw:             result,
		SSID:            result.GetSsid(),
		BSSID:           result.GetBssid(),
		Standard:        standard,
		RSSIDbm:         result.GetRssiDbm(),
		APMLDMacAddress: result.GetApMldMacAddress(),
		APMLOLinkID:     result.GetApMloLinkId(),
		AffiliatedLinks: result.GetAffiliatedMloLinks(),
	}
	out.LinkIDs = scanCandidateLinkIDs(out)
	return out
}

func scanCandidateMLOCapable(candidate ScanCandidate) bool {
	return candidate.Standard == "be" ||
		candidate.APMLDMacAddress != "" ||
		len(candidate.AffiliatedLinks) > 0
}

func scanCandidateLinkIDs(candidate ScanCandidate) []int32 {
	ids := map[int32]bool{}
	if scanCandidateMLOCapable(candidate) && candidate.APMLOLinkID >= 0 {
		ids[candidate.APMLOLinkID] = true
	}
	for _, link := range candidate.AffiliatedLinks {
		if link.GetLinkId() >= 0 {
			ids[link.GetLinkId()] = true
		}
	}
	return sortedIntSet(ids)
}

func groupCandidates(candidates []ScanCandidate) []Group {
	byKey := map[string][]ScanCandidate{}
	for _, candidate := range candidates {
		byKey[groupKey(candidate)] = append(byKey[groupKey(candidate)], candidate)
	}
	groups := make([]Group, 0, len(byKey))
	for key, values := range byKey {
		group := Group{Key: key, Results: values, BestRSSIDbm: -127}
		ssids := map[string]bool{}
		linkIDs := map[int32]bool{}
		for _, value := range values {
			if group.APMLDMacAddress == "" && value.APMLDMacAddress != "" {
				group.APMLDMacAddress = value.APMLDMacAddress
			}
			if value.SSID != "" {
				ssids[value.SSID] = true
			}
			for _, id := range value.LinkIDs {
				linkIDs[id] = true
			}
			if value.RSSIDbm > group.BestRSSIDbm {
				group.BestRSSIDbm = value.RSSIDbm
			}
		}
		for ssid := range ssids {
			group.SSIDs = append(group.SSIDs, ssid)
		}
		slices.Sort(group.SSIDs)
		group.LinkIDs = sortedIntSet(linkIDs)
		groups = append(groups, group)
	}
	slices.SortFunc(groups, func(a, b Group) int {
		return strings.Compare(a.Key, b.Key)
	})
	return groups
}

func groupKey(candidate ScanCandidate) string {
	if candidate.APMLDMacAddress != "" {
		return "mld:" + strings.ToLower(strings.TrimSpace(candidate.APMLDMacAddress))
	}
	if candidate.BSSID != "" {
		return "bssid:" + strings.ToLower(strings.TrimSpace(candidate.BSSID))
	}
	return "ssid:" + candidate.SSID
}

func connectedMLDSeenInScan(result Result) bool {
	mld := strings.TrimSpace(result.Connection.APMLDMacAddress)
	if mld == "" {
		return false
	}
	for _, candidate := range result.ScanCandidates {
		if strings.EqualFold(candidate.APMLDMacAddress, mld) {
			return true
		}
	}
	return false
}

func associatedLinksCoveredByScan(result Result) bool {
	mld := strings.TrimSpace(result.Connection.APMLDMacAddress)
	if mld == "" {
		return false
	}
	want := connectionAssociatedLinkIDs(result.Connection)
	if len(want) == 0 {
		return false
	}
	seen := map[int32]bool{}
	for _, candidate := range result.ScanCandidates {
		if !strings.EqualFold(candidate.APMLDMacAddress, mld) {
			continue
		}
		for _, id := range candidate.LinkIDs {
			seen[id] = true
		}
	}
	for _, id := range want {
		if !seen[id] {
			return false
		}
	}
	return true
}

func connectionAssociatedLinkIDs(conn Connection) []int32 {
	ids := map[int32]bool{}
	if conn.MLOPresent && conn.APMLOLinkID >= 0 {
		ids[conn.APMLOLinkID] = true
	}
	for _, link := range conn.AssociatedLinks {
		if link.GetLinkId() >= 0 {
			ids[link.GetLinkId()] = true
		}
	}
	return sortedIntSet(ids)
}

func scanMetadataComplete(candidates []ScanCandidate) bool {
	if len(candidates) == 0 {
		return false
	}
	for _, candidate := range candidates {
		if candidate.APMLDMacAddress == "" || len(candidate.LinkIDs) == 0 {
			return false
		}
	}
	return true
}

func sortedIntSet(values map[int32]bool) []int32 {
	out := make([]int32, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	slices.Sort(out)
	return out
}

func describeGroup(group Group) string {
	return fmt.Sprintf("ap_mld=%s ssids=%s links=%v results=%d best_rssi=%ddBm", empty(group.APMLDMacAddress, "<unknown>"), strings.Join(group.SSIDs, ","), group.LinkIDs, len(group.Results), group.BestRSSIDbm)
}

func describeGroups(groups []Group) string {
	if len(groups) == 0 {
		return "<none>"
	}
	parts := make([]string, 0, len(groups))
	for _, group := range groups {
		parts = append(parts, describeGroup(group))
	}
	return strings.Join(parts, "; ")
}

func normalizeStandard(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.TrimPrefix(normalized, "wifi_standard_")
	normalized = strings.TrimPrefix(normalized, "standard_")
	normalized = strings.TrimPrefix(normalized, "ieee80211")
	normalized = strings.TrimPrefix(normalized, "ieee802.11")
	normalized = strings.TrimPrefix(normalized, "802.11")
	normalized = strings.TrimPrefix(normalized, "11")
	normalized = strings.TrimPrefix(normalized, "wifi ")
	normalized = strings.TrimPrefix(normalized, "wi-fi ")
	normalized = strings.ReplaceAll(normalized, " ", "")
	switch normalized {
	case "6", "6e", "he":
		return "ax"
	case "7", "eht":
		return "be"
	default:
		return normalized
	}
}

func empty(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
