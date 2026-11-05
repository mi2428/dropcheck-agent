package adbdiag

import (
	"strings"
	"testing"
)

func TestParseWifiConnectStatusStateExtractsSupplicantPhase(t *testing.T) {
	state := ParseWifiConnectStatusState(`WifiInfo: SSID: "Example Net", BSSID: 02:00:00:00:00:11, MAC: 02:00:00:00:00:31, IP: /192.168.22.90, Security type: 4, Supplicant state: FOUR_WAY_HANDSHAKE, Wi-Fi standard: 11ax`)
	if state.Supplicant != "FOUR_WAY_HANDSHAKE" ||
		state.SSID != "Example Net" ||
		state.BSSID != "02:00:00:00:00:11" ||
		state.IP != "192.168.22.90" {
		t.Fatalf("state = %#v", state)
	}
	message := state.LogMessage()
	for _, want := range []string{
		"wifi connect state:",
		"supplicant=FOUR_WAY_HANDSHAKE",
		`ssid="Example Net"`,
		"bssid=02:00:00:00:00:11",
		"ip=192.168.22.90",
	} {
		if !strings.Contains(message, want) {
			t.Fatalf("LogMessage() = %q, missing %q", message, want)
		}
	}
}

func TestParseWifiConnectEventStatePrefersLatestSupplicantEvent(t *testing.T) {
	state := ParseWifiConnectEventState(`
WifiInfo: SSID: "Example Net", BSSID: 02:00:00:00:00:11, IP: /192.168.22.90, Supplicant state: COMPLETED
05-18 11:13:39.075 CMD_ASSOCIATED_BSSID screenOn=false, supplicantStateChangeEvents: { ASSOCIATING ASSOCIATED }
05-18 11:13:39.083 NETWORK_CONNECTION_EVENT screenOn=false, supplicantStateChangeEvents: { FOUR_WAY_HANDSHAKE }
`)
	if state.Supplicant != "FOUR_WAY_HANDSHAKE" {
		t.Fatalf("supplicant = %q, want FOUR_WAY_HANDSHAKE", state.Supplicant)
	}
}

func TestParseWifiConnectEventStateIgnoresHistoricalPlainSupplicantState(t *testing.T) {
	state := ParseWifiConnectEventState(`
05-18 11:16:22.092  1637  2020 D SupplicantStateTracker[wlan0]: Supplicant state: INTERFACE_DISABLED
WifiInfo: SSID: "Example Net", BSSID: 02:00:00:00:00:11, IP: /192.168.22.90, Supplicant state: COMPLETED
`)
	if state.Supplicant != "" {
		t.Fatalf("supplicant = %q, want empty", state.Supplicant)
	}
}
