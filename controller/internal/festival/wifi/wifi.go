// Package wifi provides Dropcheck Festival expectations for Wi-Fi status checks.
package wifi

import (
	"fmt"
	"strings"

	"dropcheck/controller/internal/controlpb"
	"dropcheck/controller/internal/festival"
)

// Result is the Wi-Fi-connection-specific view passed to custom assertions.
type Result struct {
	// RawStatus is the original Wi-Fi status payload for callers that need every field.
	RawStatus *controlpb.WifiStatus
	// Raw is the original Wi-Fi connection payload for callers that need every field.
	Raw *controlpb.WifiConnection
	// Enabled reports whether Android Wi-Fi is enabled.
	Enabled bool
	// State is Android's Wi-Fi manager state.
	State string
	// ActiveNetwork is Android's active Wi-Fi network identifier when present.
	ActiveNetwork string
	// NetworkCount is Android's configured or visible Wi-Fi network count.
	NetworkCount uint32
	// Permissions are Wi-Fi-related permissions visible to the agent.
	Permissions []string
	// SSID is the connected SSID.
	SSID string
	// BSSID is the connected BSSID.
	BSSID string
	// Standard is the normalized Wi-Fi standard, such as "be" or "ax".
	Standard string
	// RawStandard is the original standard string reported by the agent.
	RawStandard string
	// FrequencyMHz is the primary connection frequency.
	FrequencyMHz int32
	// Channel is derived from FrequencyMHz when possible.
	Channel int32
	// Band is derived from FrequencyMHz when possible.
	Band string
	// LinkSpeedMbps is Android's current link speed.
	LinkSpeedMbps int32
	// TxLinkSpeedMbps is Android's current TX link speed.
	TxLinkSpeedMbps int32
	// RxLinkSpeedMbps is Android's current RX link speed.
	RxLinkSpeedMbps int32
	// RSSIDbm is the current RSSI.
	RSSIDbm int32
	// AssociatedMLOLinks are associated Wi-Fi 7 MLO links.
	AssociatedMLOLinks []*controlpb.MloLinkInfo
	// AffiliatedMLOLinks are affiliated Wi-Fi 7 MLO links.
	AffiliatedMLOLinks []*controlpb.MloLinkInfo
	// RawText is the raw Wi-Fi connection string reported by the agent.
	RawText string
}

// Enabled matches Android's Wi-Fi enabled state.
func Enabled() festival.BoolMetric {
	return festival.Bool("wifi.enabled", func(result festival.Result) (bool, bool, string) {
		wifi, ok, reason := from(result)
		return wifi.Enabled, ok, reason
	})
}

// State matches Android's Wi-Fi manager state string.
func State() festival.OrderedMetric[string] {
	return stringMetric("wifi.state", func(result Result) (string, bool, string) {
		if result.State == "" {
			return "", false, "wifi state is empty"
		}
		return result.State, true, ""
	})
}

// SSID matches the connected SSID.
func SSID() festival.OrderedMetric[string] {
	return stringMetric("wifi.ssid", func(result Result) (string, bool, string) {
		if result.SSID == "" {
			return "", false, "ssid is empty"
		}
		return result.SSID, true, ""
	})
}

// BSSID matches the connected BSSID.
func BSSID() festival.OrderedMetric[string] {
	return stringMetric("wifi.bssid", func(result Result) (string, bool, string) {
		if result.BSSID == "" {
			return "", false, "bssid is empty"
		}
		return result.BSSID, true, ""
	})
}

// Standard matches the normalized Wi-Fi standard, such as "be" or "ax".
func Standard() festival.OrderedMetric[string] {
	return stringMetric("wifi.standard", func(result Result) (string, bool, string) {
		if result.Standard == "" {
			return "", false, "wifi standard is empty"
		}
		return result.Standard, true, ""
	})
}

// StandardName normalizes Android/protocol Wi-Fi standard strings to the short
// tokens used by Standard, such as "ax" and "be".
func StandardName(value string) string {
	return normalizeStandard(value)
}

// FrequencyMHz matches the primary connection frequency.
func FrequencyMHz() festival.OrderedMetric[int32] {
	return intMetric("wifi.frequency_mhz", func(result Result) (int32, bool, string) {
		if result.FrequencyMHz == 0 {
			return 0, false, "frequency is empty"
		}
		return result.FrequencyMHz, true, ""
	})
}

// Channel matches the Wi-Fi channel derived from the primary frequency.
func Channel() festival.OrderedMetric[int32] {
	return intMetric("wifi.channel", func(result Result) (int32, bool, string) {
		if result.Channel == 0 {
			return 0, false, "channel could not be derived from frequency"
		}
		return result.Channel, true, ""
	})
}

// Band matches the Wi-Fi band derived from the primary frequency.
func Band() festival.OrderedMetric[string] {
	return stringMetric("wifi.band", func(result Result) (string, bool, string) {
		if result.Band == "" {
			return "", false, "band could not be derived from frequency"
		}
		return result.Band, true, ""
	})
}

// LinkSpeedMbps matches Android's current link speed.
func LinkSpeedMbps() festival.OrderedMetric[int32] {
	return intMetric("wifi.link_speed_mbps", func(result Result) (int32, bool, string) {
		return result.LinkSpeedMbps, true, ""
	})
}

// TxLinkSpeedMbps matches Android's current TX link speed.
func TxLinkSpeedMbps() festival.OrderedMetric[int32] {
	return intMetric("wifi.tx_link_speed_mbps", func(result Result) (int32, bool, string) {
		return result.TxLinkSpeedMbps, true, ""
	})
}

// RxLinkSpeedMbps matches Android's current RX link speed.
func RxLinkSpeedMbps() festival.OrderedMetric[int32] {
	return intMetric("wifi.rx_link_speed_mbps", func(result Result) (int32, bool, string) {
		return result.RxLinkSpeedMbps, true, ""
	})
}

// RSSIDbm matches the current RSSI.
func RSSIDbm() festival.OrderedMetric[int32] {
	return intMetric("wifi.rssi_dbm", func(result Result) (int32, bool, string) {
		return result.RSSIDbm, true, ""
	})
}

// AssociatedMLOLinkCount matches the number of associated MLO links.
func AssociatedMLOLinkCount() festival.OrderedMetric[int] {
	return festival.Ordered[int]("wifi.associated_mlo_link_count", func(result festival.Result) (int, bool, string) {
		wifi, ok, reason := from(result)
		return len(wifi.AssociatedMLOLinks), ok, reason
	})
}

// RawContains requires the raw Wi-Fi connection text to contain value.
func RawContains(value string) festival.Expectation {
	return rawContains{value: value}
}

// Assert evaluates a custom Wi-Fi assertion against the typed result view.
func Assert(name string, fn func(Result) error) festival.Expectation {
	return assertion{name: name, fn: fn}
}

type assertion struct {
	name string
	fn   func(Result) error
}

func (a assertion) Evaluate(result festival.Result) []festival.Finding {
	wifi, ok, reason := from(result)
	metric := "wifi.assert." + a.name
	if !ok {
		return []festival.Finding{festival.Fail(metric, "<missing>", "custom assertion passed", reason)}
	}
	if err := a.fn(wifi); err != nil {
		return []festival.Finding{festival.Fail(metric, "failed", "custom assertion passed", err.Error())}
	}
	return []festival.Finding{festival.Pass(metric, "passed", "custom assertion passed")}
}

type rawContains struct {
	value string
}

func (e rawContains) Evaluate(result festival.Result) []festival.Finding {
	wifi, ok, reason := from(result)
	if !ok {
		return []festival.Finding{festival.Fail("wifi.raw", "<missing>", "contains "+e.value, reason)}
	}
	if strings.Contains(wifi.RawText, e.value) {
		return []festival.Finding{festival.Pass("wifi.raw", e.value, "contains "+e.value)}
	}
	return []festival.Finding{festival.Fail("wifi.raw", wifi.RawText, "contains "+e.value, "value not found")}
}

func stringMetric(name string, observe func(Result) (string, bool, string)) festival.OrderedMetric[string] {
	return festival.Ordered[string](name, func(result festival.Result) (string, bool, string) {
		wifi, ok, reason := from(result)
		if !ok {
			return "", false, reason
		}
		return observe(wifi)
	})
}

func intMetric(name string, observe func(Result) (int32, bool, string)) festival.OrderedMetric[int32] {
	return festival.Ordered[int32](name, func(result festival.Result) (int32, bool, string) {
		wifi, ok, reason := from(result)
		if !ok {
			return 0, false, reason
		}
		return observe(wifi)
	})
}

func from(result festival.Result) (Result, bool, string) {
	raw := result.Run.Raw
	if raw == nil {
		return Result{}, false, "raw result is nil"
	}
	status := raw.GetWifiStatus()
	if status == nil {
		return Result{}, false, fmt.Sprintf("command payload is %T, not wifi status", raw.GetPayload())
	}
	conn := status.GetConnection()
	if conn == nil {
		return Result{}, false, "wifi status does not contain connection"
	}
	frequency := conn.GetFrequencyMhz()
	return Result{
		RawStatus:          status,
		Raw:                conn,
		Enabled:            status.GetEnabled(),
		State:              status.GetState(),
		ActiveNetwork:      status.GetActiveNetwork(),
		NetworkCount:       status.GetWifiNetworkCount(),
		Permissions:        status.GetPermissions(),
		SSID:               conn.GetSsid(),
		BSSID:              conn.GetBssid(),
		Standard:           normalizeStandard(conn.GetWifiStandard()),
		RawStandard:        conn.GetWifiStandard(),
		FrequencyMHz:       frequency,
		Channel:            channelFromFrequency(frequency),
		Band:               bandFromFrequency(frequency),
		LinkSpeedMbps:      conn.GetLinkSpeedMbps(),
		TxLinkSpeedMbps:    conn.GetTxLinkSpeedMbps(),
		RxLinkSpeedMbps:    conn.GetRxLinkSpeedMbps(),
		RSSIDbm:            conn.GetRssiDbm(),
		AssociatedMLOLinks: conn.GetAssociatedMloLinks(),
		AffiliatedMLOLinks: conn.GetAffiliatedMloLinks(),
		RawText:            conn.GetRaw(),
	}, true, ""
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
