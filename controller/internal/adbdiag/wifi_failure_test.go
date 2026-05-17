package adbdiag

import (
	"strings"
	"testing"
)

func TestParseWifiFailureCauseReportsAssociationRejectionReason(t *testing.T) {
	text := `
05-17 17:11:10.000 WifiMonitor: NETWORK_DISCONNECTION_EVENT reason=3 bssid=02:00:00:00:00:21 ssid="Example Net"
05-17 17:11:12.000 WifiMonitor: ASSOCIATION_REJECTION_EVENT timedOut=false status=37:REQUEST_DECLINED bssid=02:00:00:00:00:20 ssid="Example Net"
`
	got := ParseWifiFailureCause(text)
	for _, want := range []string{
		"association rejected",
		"status=37",
		"reason=REQUEST_DECLINED",
		"timed_out=false",
		"bssid=02:00:00:00:00:20",
		`ssid="Example Net"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("ParseWifiFailureCause() = %q, missing %q", got, want)
		}
	}
}

func TestParseWifiFailureCauseIncludesAndroidBlocklist(t *testing.T) {
	text := `
WifiBlocklistMonitor addToBlocklist: bssid=02:00:00:00:00:11, ssid="Example Net", durationMs=300000, reason=REASON_FAILURE_NO_RESPONSE
`
	got := ParseWifiFailureCause(text)
	for _, want := range []string{
		"android blocklisted",
		"reason=REASON_FAILURE_NO_RESPONSE",
		"duration_ms=300000",
		"bssid=02:00:00:00:00:11",
		`ssid="Example Net"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("ParseWifiFailureCause() = %q, missing %q", got, want)
		}
	}
}

func TestParseWifiFailureCauseCombinesRejectionAndBlocklist(t *testing.T) {
	got := ParseWifiFailureCause(`
ASSOCIATION_REJECTION_EVENT timedOut=false status=37:REQUEST_DECLINED bssid=02:00:00:00:00:20 ssid="Example Net"
WifiBlocklistMonitor addToBlocklist: bssid=02:00:00:00:00:20, ssid="Example Net", durationMs=300000, reason=REASON_AP_UNABLE_TO_HANDLE_NEW_STA
`)
	for _, want := range []string{
		"association rejected status=37 reason=REQUEST_DECLINED",
		"android blocklisted reason=REASON_AP_UNABLE_TO_HANDLE_NEW_STA",
		";",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("ParseWifiFailureCause() = %q, missing %q", got, want)
		}
	}
}

func TestParseWifiFailureCauseFallsBackToDisconnect(t *testing.T) {
	got := ParseWifiFailureCause(`NETWORK_DISCONNECTION_EVENT reason=8 bssid=aa:bb:cc:dd:ee:ff ssid=Lab`)
	for _, want := range []string{"disconnected", "reason=8", "bssid=aa:bb:cc:dd:ee:ff", "ssid=Lab"} {
		if !strings.Contains(got, want) {
			t.Fatalf("ParseWifiFailureCause() = %q, missing %q", got, want)
		}
	}
}
