// Package scan provides Dropcheck Festival expectations for Wi-Fi scan checks.
package scan

import (
	"fmt"
	"slices"
	"strings"

	"dropcheck/controller/internal/controlpb"
	"dropcheck/controller/internal/festival"
	statuswifi "dropcheck/controller/internal/festival/wifi"
)

// AP is a normalized scan result for one advertised AP.
type AP struct {
	// Raw is the original protobuf scan result.
	Raw *controlpb.WifiScanResult
	// SSID is the advertised SSID.
	SSID string
	// BSSID is the AP MAC address.
	BSSID string
	// Capabilities is Android's raw capabilities string.
	Capabilities string
	// RSSIDbm is the scan RSSI.
	RSSIDbm int32
	// FrequencyMHz is the primary frequency.
	FrequencyMHz int32
	// Channel is derived from FrequencyMHz.
	Channel int32
	// Band is the normalized scan band.
	Band string
	// ChannelWidth is the normalized advertised channel width.
	ChannelWidth string
	// CenterFreq0MHz is Android's first center frequency.
	CenterFreq0MHz int32
	// CenterFreq1MHz is Android's second center frequency for 80+80MHz.
	CenterFreq1MHz int32
	// TimestampUs is Android's scan timestamp.
	TimestampUs int64
	// Standard is the normalized Wi-Fi standard, such as "be" or "ax".
	Standard string
	// RawStandard is the original Wi-Fi standard string.
	RawStandard string
	// SecurityTypes are normalized Android security type strings.
	SecurityTypes []string
	// RawSecurityTypes are the original security type strings.
	RawSecurityTypes []string
	// Passpoint reports whether this is a Passpoint AP.
	Passpoint bool
	// Responder80211Mc reports whether 802.11mc ranging is advertised.
	Responder80211Mc bool
	// Responder80211AzNtb reports whether 802.11az NTB ranging is advertised.
	Responder80211AzNtb bool
	// APMLDMacAddress is the AP MLD MAC address for Wi-Fi 7 MLO.
	APMLDMacAddress string
	// APMLOLinkID is the AP MLO link ID for Wi-Fi 7 MLO.
	APMLOLinkID int32
	// AffiliatedMLOLinks are advertised MLO links.
	AffiliatedMLOLinks []*controlpb.MloLinkInfo
	// RawText is Android's raw scan result string.
	RawText string
}

// Result is the Wi-Fi-scan-specific view passed to custom assertions.
type Result struct {
	// RawScan is the original scan payload when the command was wifi.scan.
	RawScan *controlpb.WifiScan
	// RawDetail is the original scan-detail payload when the command was wifi.scan.detail.
	RawDetail *controlpb.WifiScanDetail
	// Status is the outer command status returned by the agent.
	Status controlpb.CommandResult_Status
	// Target is the scan-detail target when present.
	Target string
	// Fields are diagnostic fields reported by the agent.
	Fields []*controlpb.DiagnosticField
	// Results are normalized scan results.
	Results []AP
	// Errors are agent-reported scan errors.
	Errors []string
}

// ResultCount matches the number of scan results.
func ResultCount() festival.OrderedMetric[int] {
	return festival.Ordered[int]("scan.result_count", func(result festival.Result) (int, bool, string) {
		scan, ok, reason := from(result)
		return len(scan.Results), ok, reason
	})
}

// ErrorCount matches the number of scan errors.
func ErrorCount() festival.OrderedMetric[int] {
	return festival.Ordered[int]("scan.error_count", func(result festival.Result) (int, bool, string) {
		scan, ok, reason := from(result)
		return len(scan.Errors), ok, reason
	})
}

// APs starts an AP selector for matching advertised scan results.
func APs() APSelector {
	return APSelector{metric: "scan.ap"}
}

// APResult is a convenience alias for APs.
func APResult() APSelector {
	return APs()
}

// Assert evaluates a custom Wi-Fi scan assertion against the typed result view.
func Assert(name string, fn func(Result) error) festival.Expectation {
	return assertion{name: name, fn: fn}
}

type assertion struct {
	name string
	fn   func(Result) error
}

func (a assertion) Evaluate(result festival.Result) []festival.Finding {
	scan, ok, reason := from(result)
	metric := "scan.assert." + a.name
	if !ok {
		return []festival.Finding{festival.Fail(metric, "<missing>", "custom assertion passed", reason)}
	}
	if err := a.fn(scan); err != nil {
		return []festival.Finding{festival.Fail(metric, "failed", "custom assertion passed", err.Error())}
	}
	return []festival.Finding{festival.Pass(metric, "passed", "custom assertion passed")}
}

// APSelector filters scan results by AP-advertised fields.
type APSelector struct {
	metric              string
	ssid                string
	bssid               string
	standard            string
	channelWidth        string
	band                string
	channel             *int32
	security            string
	minRSSI             *int32
	capabilityFragments []string
}

// SSID restricts matches to one SSID.
func (s APSelector) SSID(value string) APSelector {
	s.ssid = value
	return s
}

// BSSID restricts matches to one BSSID.
func (s APSelector) BSSID(value string) APSelector {
	s.bssid = strings.ToLower(value)
	return s
}

// Standard restricts matches to one normalized Wi-Fi standard, such as "be".
func (s APSelector) Standard(value string) APSelector {
	s.standard = statuswifi.StandardName(value)
	return s
}

// ChannelWidth restricts matches to one normalized channel width, such as "320mhz".
func (s APSelector) ChannelWidth(value string) APSelector {
	s.channelWidth = normalizeChannelWidth(value)
	return s
}

// Band restricts matches to one Wi-Fi band, such as "6ghz".
func (s APSelector) Band(value string) APSelector {
	s.band = normalizeBand(value)
	return s
}

// Channel restricts matches to one derived Wi-Fi channel.
func (s APSelector) Channel(value int32) APSelector {
	s.channel = &value
	return s
}

// Security restricts matches to one Android security type string.
func (s APSelector) Security(value string) APSelector {
	s.security = normalizeToken(value)
	return s
}

// MinRSSI requires matched APs to have at least value dBm RSSI.
func (s APSelector) MinRSSI(value int32) APSelector {
	s.minRSSI = &value
	return s
}

// CapabilityContains requires Android's raw capabilities string to contain value.
func (s APSelector) CapabilityContains(value string) APSelector {
	if value != "" {
		s.capabilityFragments = append(s.capabilityFragments, value)
	}
	return s
}

// Exists requires at least one scan result to match the selector.
func (s APSelector) Exists() festival.Expectation {
	return apExists{selector: s}
}

// Count matches the number of scan results selected by the selector.
func (s APSelector) Count() festival.OrderedMetric[int] {
	return festival.Ordered[int](s.metric+"_count", func(result festival.Result) (int, bool, string) {
		scan, ok, reason := from(result)
		if !ok {
			return 0, false, reason
		}
		return len(s.matches(scan.Results)), true, ""
	})
}

type apExists struct {
	selector APSelector
}

func (e apExists) Evaluate(result festival.Result) []festival.Finding {
	scan, ok, reason := from(result)
	if !ok {
		return []festival.Finding{festival.Fail(e.selector.metric, "<missing>", "exists", reason)}
	}
	matches := e.selector.matches(scan.Results)
	if len(matches) > 0 {
		return []festival.Finding{festival.Pass(e.selector.metric, describeAP(matches[0]), "exists")}
	}
	return []festival.Finding{festival.Fail(e.selector.metric, describeAPs(scan.Results), "exists", "no scan result matched selector")}
}

func (s APSelector) matches(results []AP) []AP {
	matches := make([]AP, 0, len(results))
	for _, result := range results {
		if s.matchesOne(result) {
			matches = append(matches, result)
		}
	}
	return matches
}

func (s APSelector) matchesOne(result AP) bool {
	if s.ssid != "" && result.SSID != s.ssid {
		return false
	}
	if s.bssid != "" && strings.ToLower(result.BSSID) != s.bssid {
		return false
	}
	if s.standard != "" && result.Standard != s.standard {
		return false
	}
	if s.channelWidth != "" && result.ChannelWidth != s.channelWidth {
		return false
	}
	if s.band != "" && result.Band != s.band {
		return false
	}
	if s.channel != nil && result.Channel != *s.channel {
		return false
	}
	if s.security != "" && !slices.Contains(result.SecurityTypes, s.security) {
		return false
	}
	if s.minRSSI != nil && result.RSSIDbm < *s.minRSSI {
		return false
	}
	for _, fragment := range s.capabilityFragments {
		if !strings.Contains(result.Capabilities, fragment) {
			return false
		}
	}
	return true
}

func from(result festival.Result) (Result, bool, string) {
	raw := result.Run.Raw
	if raw == nil {
		return Result{}, false, "raw result is nil"
	}
	if scan := raw.GetWifiScan(); scan != nil {
		return Result{
			RawScan: scan,
			Status:  raw.GetStatus(),
			Fields:  scan.GetFields(),
			Results: normalizeResults(scan.GetResults()),
			Errors:  scan.GetErrors(),
		}, true, ""
	}
	if detail := raw.GetWifiScanDetail(); detail != nil {
		return Result{
			RawDetail: detail,
			Status:    raw.GetStatus(),
			Target:    detail.GetTarget(),
			Fields:    detail.GetFields(),
			Results:   normalizeResults(detail.GetResults()),
			Errors:    detail.GetErrors(),
		}, true, ""
	}
	return Result{}, false, fmt.Sprintf("command payload is %T, not wifi scan", raw.GetPayload())
}

func normalizeResults(values []*controlpb.WifiScanResult) []AP {
	results := make([]AP, 0, len(values))
	for _, value := range values {
		frequency := value.GetFrequencyMhz()
		securityTypes := value.GetSecurityTypes()
		normalizedSecurity := make([]string, 0, len(securityTypes))
		for _, security := range securityTypes {
			normalizedSecurity = append(normalizedSecurity, normalizeToken(security))
		}
		results = append(results, AP{
			Raw:                 value,
			SSID:                value.GetSsid(),
			BSSID:               value.GetBssid(),
			Capabilities:        value.GetCapabilities(),
			RSSIDbm:             value.GetRssiDbm(),
			FrequencyMHz:        frequency,
			Channel:             channelFromFrequency(frequency),
			Band:                normalizeBand(firstNonEmpty(value.GetBand(), bandFromFrequency(frequency))),
			ChannelWidth:        normalizeChannelWidth(value.GetChannelWidth()),
			CenterFreq0MHz:      value.GetCenterFreq0Mhz(),
			CenterFreq1MHz:      value.GetCenterFreq1Mhz(),
			TimestampUs:         value.GetTimestampUs(),
			Standard:            statuswifi.StandardName(value.GetWifiStandard()),
			RawStandard:         value.GetWifiStandard(),
			SecurityTypes:       normalizedSecurity,
			RawSecurityTypes:    append([]string(nil), securityTypes...),
			Passpoint:           value.GetPasspoint(),
			Responder80211Mc:    value.GetResponder_80211Mc(),
			Responder80211AzNtb: value.GetResponder_80211AzNtb(),
			APMLDMacAddress:     value.GetApMldMacAddress(),
			APMLOLinkID:         value.GetApMloLinkId(),
			AffiliatedMLOLinks:  value.GetAffiliatedMloLinks(),
			RawText:             value.GetRaw(),
		})
	}
	return results
}

func describeAP(value AP) string {
	return fmt.Sprintf("ssid=%s bssid=%s standard=%s channel=%d width=%s rssi=%ddBm", value.SSID, value.BSSID, value.Standard, value.Channel, value.ChannelWidth, value.RSSIDbm)
}

func describeAPs(values []AP) string {
	if len(values) == 0 {
		return "<none>"
	}
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, describeAP(value))
	}
	return strings.Join(parts, "; ")
}

func normalizeBand(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.ReplaceAll(normalized, " ", "")
	normalized = strings.ReplaceAll(normalized, "_", "")
	normalized = strings.ReplaceAll(normalized, "-", "")
	switch normalized {
	case "2.4", "24", "2.4g", "24g", "2.4ghz", "24ghz":
		return "2.4ghz"
	case "5", "5g", "5ghz":
		return "5ghz"
	case "6", "6g", "6ghz":
		return "6ghz"
	case "60", "60g", "60ghz":
		return "60ghz"
	default:
		return normalized
	}
}

func normalizeToken(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.ReplaceAll(normalized, "-", "_")
	normalized = strings.ReplaceAll(normalized, " ", "_")
	return normalized
}

func normalizeChannelWidth(value string) string {
	width := strings.ToLower(strings.TrimSpace(value))
	width = strings.Trim(width, ",;")
	width = strings.TrimPrefix(width, "channel_width_")
	width = strings.TrimPrefix(width, "width_")
	width = strings.ReplaceAll(width, " ", "")
	width = strings.ReplaceAll(width, "_", "")
	if !strings.HasSuffix(width, "mhz") && width != "" {
		width += "mhz"
	}
	return width
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

func bandFromFrequency(frequency int32) string {
	switch {
	case frequency >= 2400 && frequency < 2500:
		return "2.4ghz"
	case frequency >= 4900 && frequency < 5900:
		return "5ghz"
	case frequency >= 5925 && frequency < 7125:
		return "6ghz"
	case frequency >= 57000 && frequency < 71000:
		return "60ghz"
	default:
		return ""
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
