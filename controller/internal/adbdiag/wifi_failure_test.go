package adbdiag

import (
	"strings"
	"testing"
)

func TestParseWifiFailureCauseReportsAssociationRejectionReason(t *testing.T) {
	text := `
05-17 17:11:10.000 WifiMonitor: NETWORK_DISCONNECTION_EVENT reason=3 bssid=70:a7:41:a0:9a:6f ssid="SHIZK RADIO"
05-17 17:11:12.000 WifiMonitor: ASSOCIATION_REJECTION_EVENT timedOut=false status=37:REQUEST_DECLINED bssid=70:a7:41:a0:9a:6e ssid="SHIZK RADIO"
`
	got := ParseWifiFailureCause(text)
	for _, want := range []string{
		"association rejected",
		"status=37",
		"reason=REQUEST_DECLINED",
		"timed_out=false",
		"bssid=70:a7:41:a0:9a:6e",
		`ssid="SHIZK RADIO"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("ParseWifiFailureCause() = %q, missing %q", got, want)
		}
	}
}

func TestParseWifiFailureCauseIncludesAndroidBlocklist(t *testing.T) {
	text := `
WifiBlocklistMonitor addToBlocklist: bssid=22:0b:8b:b6:2c:e1, ssid="SHIZK RADIO", durationMs=300000, reason=REASON_FAILURE_NO_RESPONSE
`
	got := ParseWifiFailureCause(text)
	for _, want := range []string{
		"android blocklisted",
		"reason=REASON_FAILURE_NO_RESPONSE",
		"duration_ms=300000",
		"bssid=22:0b:8b:b6:2c:e1",
		`ssid="SHIZK RADIO"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("ParseWifiFailureCause() = %q, missing %q", got, want)
		}
	}
}

func TestParseWifiFailureCauseCombinesRejectionAndBlocklist(t *testing.T) {
	got := ParseWifiFailureCause(`
ASSOCIATION_REJECTION_EVENT timedOut=false status=37:REQUEST_DECLINED bssid=70:a7:41:a0:9a:6e ssid="SHIZK RADIO"
WifiBlocklistMonitor addToBlocklist: bssid=70:a7:41:a0:9a:6e, ssid="SHIZK RADIO", durationMs=300000, reason=REASON_AP_UNABLE_TO_HANDLE_NEW_STA
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
