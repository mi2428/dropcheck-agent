package watchrun

import (
	"fmt"
	"net"
	"slices"
	"strconv"
	"strings"

	"github.com/mi2428/dropcheck-agent/controller/internal/acquire"
	"github.com/mi2428/dropcheck-agent/controller/internal/controlpb"
)

// apExpectation contains only the selectors accepted by watch YAML.
type apExpectation struct {
	ssid, bssid, band, standard, security, width, mld string
	channel, linkID                                   *int32
	count                                             int
	exact                                             bool
	countMode                                         bool
}

func compileAP(raw any) (Expectation, error) {
	fields, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("ap expectation must be a mapping")
	}
	e := apExpectation{count: 1}
	for field, value := range fields {
		text := fmt.Sprint(value)
		switch field {
		case "ssid":
			e.ssid = text
		case "bssid":
			e.bssid = strings.ToLower(text)
		case "band":
			e.band = normalizeAPBand(text)
		case "standard":
			e.standard = normalizeAPStandard(text)
		case "security":
			e.security = normalizeAPToken(text)
		case "channel_width":
			e.width = normalizeAPWidth(text)
		case "ap_mld":
			e.mld = strings.ToLower(strings.TrimSpace(text))
		case "channel", "mlo_link_id":
			n, err := strconv.ParseInt(text, 10, 32)
			if err != nil {
				return nil, err
			}
			if field == "channel" {
				e.channel = new(int32(n))
			} else {
				e.linkID = new(int32(n))
			}
		case "count", "min_count":
		default:
			return nil, fmt.Errorf("unknown AP selector %q", field)
		}
	}
	if value, ok := fields["count"]; ok {
		n, err := strconv.Atoi(fmt.Sprint(value))
		if err != nil {
			return nil, err
		}
		e.count, e.exact, e.countMode = n, true, true
	} else if value, ok := fields["min_count"]; ok {
		n, err := strconv.Atoi(fmt.Sprint(value))
		if err != nil {
			return nil, err
		}
		e.count, e.countMode = n, true
	}
	for _, address := range []string{e.bssid, e.mld} {
		if address == "" {
			continue
		}
		mac, err := net.ParseMAC(address)
		if err != nil || len(mac) != 6 {
			return nil, fmt.Errorf("invalid AP MAC address")
		}
	}
	switch e.band {
	case "", "all", "2.4ghz", "5ghz", "6ghz", "60ghz":
	default:
		return nil, fmt.Errorf("invalid AP band")
	}
	if e.channel != nil && *e.channel <= 0 || e.linkID != nil && (*e.linkID < 0 || *e.linkID > 15) {
		return nil, fmt.Errorf("invalid AP channel or MLO link ID")
	}
	return e, nil
}

func (e apExpectation) Description() string {
	op := ">="
	if e.exact {
		op = "=="
	}
	return fmt.Sprintf("scan.ap count %s %d (ssid=%s bssid=%s band=%s standard=%s security=%s width=%s mld=%s channel=%v link_id=%v)", op, e.count, e.ssid, e.bssid, e.band, e.standard, e.security, e.width, e.mld, optionalAPInt(e.channel), optionalAPInt(e.linkID))
}

func optionalAPInt(value *int32) any {
	if value == nil {
		return "any"
	}
	return *value
}

func (e apExpectation) Evaluate(result Result) []Finding {
	raw := result.Run.Raw
	metric, expected := "scan.ap", "exists"
	if e.countMode {
		metric, expected = "scan.ap_count", fmt.Sprintf(">= %d", e.count)
		if e.exact {
			expected = fmt.Sprintf("== %d", e.count)
		}
	}
	var results []*controlpb.WifiScanResult
	switch {
	case raw.GetWifiScan() != nil:
		results = raw.GetWifiScan().GetResults()
	case raw.GetWifiScanDetail() != nil:
		results = raw.GetWifiScanDetail().GetResults()
	default:
		return []Finding{MissingFinding(metric, "<missing>", expected, "wifi scan payload unavailable")}
	}
	count := 0
	for _, ap := range results {
		if e.ssid != "" && ap.GetSsid() != e.ssid || e.bssid != "" && strings.ToLower(ap.GetBssid()) != e.bssid ||
			e.band != "" && normalizeAPBand(firstNonEmpty(ap.GetBand(), bandFromFrequency(ap.GetFrequencyMhz()))) != e.band ||
			e.standard != "" && normalizeAPStandard(ap.GetWifiStandard()) != e.standard ||
			e.width != "" && normalizeAPWidth(ap.GetChannelWidth()) != e.width ||
			e.channel != nil && channelFromFrequency(ap.GetFrequencyMhz()) != *e.channel ||
			e.mld != "" && !strings.EqualFold(strings.TrimSpace(ap.GetApMldMacAddress()), e.mld) {
			continue
		}
		security := false
		for _, token := range ap.GetSecurityTypes() {
			security = security || normalizeAPToken(token) == e.security
		}
		if e.security != "" && !security || e.linkID != nil && !slices.Contains(scanLinkIDs(ap), *e.linkID) {
			continue
		}
		count++
	}
	f := Pass(metric, strconv.Itoa(count), expected)
	f.ObservedValue, f.ExpectedValue = count, e.count
	if e.exact && count != e.count || !e.exact && count < e.count {
		f.Passed, f.Message = false, "no scan result count matched selector"
	}
	return []Finding{f}
}

type mloExpectation string

func (e mloExpectation) metric() string {
	switch e {
	case "mlo_connected":
		return "mlo.connected.present"
	case "mlo_coverage":
		return "mlo.relation.associated_links_covered_by_scan"
	default:
		return "mlo.metadata.complete"
	}
}

func (e mloExpectation) Description() string { return e.metric() + " == true" }

func (e mloExpectation) Evaluate(result Result) []Finding {
	d := result.Run.Raw.GetWifiDiagnostics()
	missing := func(reason string) []Finding {
		return []Finding{MissingFinding(e.metric(), "<missing>", "== true", reason)}
	}
	if d == nil {
		return missing("wifi diagnostics payload unavailable")
	}
	conn := d.GetStatus().GetConnection()
	present := conn.GetApMldMacAddress() != "" || len(conn.GetAssociatedMloLinks()) > 0 || len(conn.GetAffiliatedMloLinks()) > 0
	var candidates []*controlpb.WifiScanResult
	for _, ap := range d.GetScan().GetResults() {
		if scanMLOCapable(ap) {
			candidates = append(candidates, ap)
		}
	}
	value := false
	switch e {
	case "mlo_connected":
		if conn == nil {
			return missing("wifi diagnostics do not contain connection")
		}
		if !present {
			if known, reason := acquire.Availability(conn.ObservationFields, "ap_mld_mac_address"); !known {
				return missing(reason)
			}
		}
		value = present
	case "mlo_coverage":
		if conn == nil || d.GetScan() == nil {
			return missing("connection/scan relation unavailable")
		}
		if known, reason := acquire.Availability(conn.ObservationFields, "associated_mlo_links"); !known {
			return missing(reason)
		}
		want := linkIDs(conn.GetAssociatedMloLinks())
		if present && conn.GetApMloLinkId() >= 0 {
			want = append(want, conn.GetApMloLinkId())
		}
		var seen []int32
		mld := strings.TrimSpace(conn.GetApMldMacAddress())
		for _, ap := range candidates {
			if strings.EqualFold(ap.GetApMldMacAddress(), mld) {
				seen = append(seen, scanLinkIDs(ap)...)
			}
		}
		value = mld != "" && len(want) > 0
		for _, id := range want {
			value = value && slices.Contains(seen, id)
		}
	case "mlo_metadata_complete":
		if d.GetScan() == nil {
			return missing("scan metadata unavailable")
		}
		value = len(candidates) > 0
		for _, ap := range candidates {
			if known, reason := acquire.Availability(ap.ObservationFields, "ap_mlo_link_id"); !known {
				return missing(reason)
			}
			value = value && ap.GetApMldMacAddress() != "" && len(scanLinkIDs(ap)) > 0
		}
	}
	f := Pass(e.metric(), strconv.FormatBool(value), "== true")
	f.ObservedValue, f.ExpectedValue = value, true
	if !value {
		f.Passed, f.Message = false, "constraint failed"
	}
	return []Finding{f}
}

func scanMLOCapable(ap *controlpb.WifiScanResult) bool {
	return normalizeAPStandard(ap.GetWifiStandard()) == "be" || ap.GetApMldMacAddress() != "" || len(ap.GetAffiliatedMloLinks()) > 0
}

func scanLinkIDs(ap *controlpb.WifiScanResult) []int32 {
	ids := linkIDs(ap.GetAffiliatedMloLinks())
	if scanMLOCapable(ap) && ap.GetApMloLinkId() >= 0 {
		ids = append(ids, ap.GetApMloLinkId())
	}
	return ids
}

func linkIDs(links []*controlpb.MloLinkInfo) []int32 {
	var ids []int32
	for _, link := range links {
		if link.GetLinkId() >= 0 {
			ids = append(ids, link.GetLinkId())
		}
	}
	return ids
}

func normalizeAPStandard(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	for _, prefix := range []string{"wifi_standard_", "standard_", "ieee80211", "ieee802.11", "802.11", "11", "wifi ", "wi-fi "} {
		value = strings.TrimPrefix(value, prefix)
	}
	value = strings.ReplaceAll(value, " ", "")
	switch value {
	case "6", "6e", "he":
		return "ax"
	case "7", "eht":
		return "be"
	default:
		return value
	}
}

func normalizeAPBand(value string) string {
	value = strings.NewReplacer(" ", "", "_", "", "-", "").Replace(strings.ToLower(strings.TrimSpace(value)))
	switch value {
	case "2.4", "24", "2.4g", "24g", "2.4ghz", "24ghz":
		return "2.4ghz"
	case "5", "5g", "5ghz":
		return "5ghz"
	case "6", "6g", "6ghz":
		return "6ghz"
	case "60", "60g", "60ghz":
		return "60ghz"
	default:
		return value
	}
}

func normalizeAPToken(value string) string {
	return strings.NewReplacer("-", "_", " ", "_").Replace(strings.ToLower(strings.TrimSpace(value)))
}

func normalizeAPWidth(value string) string {
	value = strings.Trim(strings.ToLower(strings.TrimSpace(value)), ",;")
	value = strings.TrimPrefix(strings.TrimPrefix(value, "channel_width_"), "width_")
	value = strings.NewReplacer(" ", "", "_", "").Replace(value)
	if value != "" && !strings.HasSuffix(value, "mhz") {
		value += "mhz"
	}
	return value
}

func channelFromFrequency(frequency int32) int32 {
	switch {
	case frequency == 2484:
		return 14
	case frequency >= 2412 && frequency <= 2472:
		return (frequency - 2407) / 5
	case frequency >= 5000 && frequency <= 5895:
		return (frequency - 5000) / 5
	case frequency >= 5955 && frequency <= 7115:
		return (frequency - 5950) / 5
	default:
		return 0
	}
}
