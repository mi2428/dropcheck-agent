package command

import (
	"strings"
	"testing"

	"github.com/mi2428/dropcheck-agent/controller/internal/controlpb"
)

func TestConnectCredentialsAtBuilderAndDispatchBoundary(t *testing.T) {
	for _, secret := range []string{"", "short", "1234567", "1234567\n", "1234567\x00", "1234567\x7f", "1234567\u0085", "1234567\xff", strings.Repeat("z", 64), strings.Repeat("a", 65)} {
		for _, security := range []string{"", "auto", "wpa2", "wpa3", "transition"} {
			opts := WifiConnectOptions{SSID: " 123 日本語 ", BSSID: "aa:bb:cc:dd:ee:ff", Security: security, Passphrase: secret}
			if _, err := WifiConnectOperation(opts); err == nil {
				t.Fatal("connect builder accepted invalid credential")
			}
			if _, err := WifiCycleOperation(WifiCycleOptions{WifiConnectOptions: opts}); err == nil {
				t.Fatal("cycle builder accepted invalid credential")
			}
			wire := &controlpb.ConnectWifi{Ssid: opts.SSID, Passphrase: secret, Security: controlpb.ConnectWifi_SECURITY_WPA2_PSK}
			for _, cmd := range []*controlpb.RunCommand{
				{Command: &controlpb.RunCommand_ConnectWifi{ConnectWifi: wire}},
				{Command: &controlpb.RunCommand_CycleWifi{CycleWifi: &controlpb.CycleWifi{Connect: wire}}},
			} {
				if ValidateOperation(NewOperation("synthetic", cmd, Options{})) == nil {
					t.Fatal("dispatch boundary accepted invalid credential")
				}
			}
		}
	}
	for _, secret := range []string{"12345678", "        ", strings.Repeat("a", 63), strings.Repeat("日", 20), strings.Repeat("aB09", 16)} {
		for _, security := range []string{"", "auto", "wpa2", "wpa3", "transition"} {
			opts := WifiConnectOptions{SSID: " 123 日本語 ", Security: security, Passphrase: secret}
			op, err := WifiConnectOperation(opts)
			if err != nil || ValidateOperation(op) != nil || op.Command.GetConnectWifi().Ssid != opts.SSID {
				t.Fatal("valid credential or literal SSID changed")
			}
			cycle, err := WifiCycleOperation(WifiCycleOptions{WifiConnectOptions: opts})
			if err != nil || ValidateOperation(cycle) != nil {
				t.Fatal("valid cycle rejected")
			}
		}
	}
	// Shell defers missing credentials to its explicit/default credential resolver.
	parsed, err := parseInteractiveTokens([]string{"request", "wifi", "connect", "Lab"})
	if err != nil || parsed.Connect == nil || parsed.Connect.Passphrase != "" {
		t.Fatal("Shell credential resolution moved into preflight")
	}
}
