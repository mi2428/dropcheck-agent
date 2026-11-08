package render

import (
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/mi2428/dropcheck-agent/controller/internal/controlpb"
)

func scanBSSID(value string) string {
	if len(value) != 17 || value[2] != ':' {
		return ""
	}
	mac, err := net.ParseMAC(value)
	if err != nil || len(mac) != 6 || mac[0]&1 != 0 {
		return ""
	}
	value = strings.ToLower(mac.String())
	if value == "00:00:00:00:00:00" || value == "02:00:00:00:00:00" {
		return ""
	}
	return value
}

func validScanConnections(value *controlpb.WifiScanConnections) bool {
	if value.GetObservedAtUnixMs() <= 0 || value.GetComplete() && len(value.GetErrors()) > 0 || !value.GetComplete() && len(value.GetErrors()) == 0 {
		return false
	}
	for _, bssid := range value.GetBssids() {
		if scanBSSID(bssid) == "" {
			return false
		}
	}
	return true
}

func scanEHTEvidence(ap *controlpb.WifiScanResult) string {
	switch {
	case strings.EqualFold(ap.GetWifiStandard(), "802.11be"):
		return "observed (802.11be)"
	case ap.GetEhtCapabilities() != nil && !ap.GetEhtCapabilities().GetTruncated() && ap.GetEhtCapabilities().GetMac() != nil && ap.GetEhtCapabilities().GetPhy() != nil:
		return "observed (decoded EHT Capabilities)"
	case ap.GetEhtOperation() != nil && !ap.GetEhtOperation().GetTruncated() && ap.GetEhtOperation().GetBasicMcsNssSetHex() != "":
		return "observed (decoded EHT Operation)"
	default:
		return ""
	}
}

func scanAPMark(ap *controlpb.WifiScanResult, connections *controlpb.WifiScanConnections) string {
	if key := scanBSSID(ap.GetBssid()); key != "" && validScanConnections(connections) {
		for _, bssid := range connections.GetBssids() {
			if scanBSSID(bssid) == key {
				return "*"
			}
		}
	}
	if scanEHTEvidence(ap) != "" {
		return "+"
	}
	return " "
}

func scanConnectionsSummary(value *controlpb.WifiScanConnections) string {
	if value.GetObservedAtUnixMs() == 0 {
		return "? (connection observation not captured)"
	}
	if !validScanConnections(value) {
		return "? (inconsistent connection observation)"
	}
	state := strings.Join(value.GetBssids(), ", ")
	if state == "" {
		if value.GetComplete() {
			state = "disconnected"
		} else {
			state = "unknown"
		}
	}
	result := fmt.Sprintf("%s at %s; not atomic with scan", state, time.UnixMilli(value.GetObservedAtUnixMs()).UTC().Format("2006-01-02T15:04:05.999999999Z"))
	if !value.GetComplete() {
		result += "; incomplete: " + strings.Join(value.GetErrors(), "; ")
	}
	return result
}
