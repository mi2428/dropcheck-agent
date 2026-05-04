//go:build festival

package festival

import (
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	f "dropcheck/controller/internal/festival"
	"dropcheck/controller/internal/festival/capabilities"
	"dropcheck/controller/internal/festival/dns"
	"dropcheck/controller/internal/festival/globalip"
	"dropcheck/controller/internal/festival/ip"
	"dropcheck/controller/internal/festival/ping"
	"dropcheck/controller/internal/festival/pmtu"
	"dropcheck/controller/internal/festival/scan"
	"dropcheck/controller/internal/festival/trace"
	"dropcheck/controller/internal/festival/wifi"
)

const (
	envSSID             = "DROPCHECK_FESTIVAL_WIFI_SSID"
	envPSK              = "DROPCHECK_FESTIVAL_WIFI_PSK"
	envPSKName          = "DROPCHECK_FESTIVAL_WIFI_PSK_ENV"
	envBSSID            = "DROPCHECK_FESTIVAL_WIFI_BSSID"
	envBand             = "DROPCHECK_FESTIVAL_WIFI_BAND"
	envRequireValidated = "DROPCHECK_FESTIVAL_REQUIRE_VALIDATED"
	envStandard         = "DROPCHECK_FESTIVAL_WIFI_STANDARD"
	envChannel          = "DROPCHECK_FESTIVAL_WIFI_CHANNEL"
	envChannelWidth     = "DROPCHECK_FESTIVAL_WIFI_CHANNEL_WIDTH"
)

func TestFestivalSmoke(t *testing.T) {
	ssid := os.Getenv(envSSID)
	pskEnv := os.Getenv(envPSKName)
	if pskEnv == "" {
		pskEnv = envPSK
	}
	if ssid == "" || os.Getenv(pskEnv) == "" {
		t.Skipf("set %s and %s to run the Dropcheck Festival smoke test", envSSID, pskEnv)
	}
	network := f.WiFi("smoke-wifi").
		SSID(ssid).
		PSKEnv(pskEnv).
		BSSID(os.Getenv(envBSSID)).
		Band(os.Getenv(envBand)).
		RequireValidated(os.Getenv(envRequireValidated) == "1")

	ipExpect := []f.Expectation{
		ip.AddressCount().Ge(1),
		ip.MTU().Ge(1280),
	}
	wifiExpect := []f.Expectation{
		wifi.Enabled().IsTrue(),
		wifi.SSID().Eq(ssid),
	}
	ap := scan.APs().SSID(ssid)
	capabilityExpect := []f.Expectation{capabilities.ErrorCount().Eq(0)}
	if bssid := os.Getenv(envBSSID); bssid != "" {
		wifiExpect = append(wifiExpect, wifi.BSSID().Eq(bssid))
		ap = ap.BSSID(bssid)
	}
	if standard := os.Getenv(envStandard); standard != "" {
		wifiExpect = append(wifiExpect, wifi.Standard().Eq(wifi.StandardName(standard)))
		ap = ap.Standard(standard)
		capabilityExpect = append(capabilityExpect, capabilities.Standard(standard).Supported())
	}
	if channel := os.Getenv(envChannel); channel != "" {
		value, err := strconv.ParseInt(channel, 10, 32)
		if err != nil {
			t.Fatalf("%s: %v", envChannel, err)
		}
		wifiExpect = append(wifiExpect, wifi.Channel().Eq(int32(value)))
		ap = ap.Channel(int32(value))
	}
	if width := os.Getenv(envChannelWidth); width != "" {
		ap = ap.ChannelWidth(width)
	}

	f.Run(t, f.Plan{
		Networks: []f.Network{network},
		Checks: []f.Check{
			f.IPStatus().
				Expect(ipExpect...),
			f.WiFiStatus().
				Expect(wifiExpect...),
			f.WiFiScan().
				Fresh().
				Band(os.Getenv(envBand)).
				Expect(ap.Exists()),
			f.WiFiCapabilities().
				Expect(capabilityExpect...),
			f.Ping("8.8.8.8").
				Count(5).
				Expect(
					ping.Received().Ge(1),
					ping.LossPercent().Le(100),
					ping.AvgLatency().Gt(0).Le(2*time.Second),
				),
			f.DNS("example.com").
				A().
				Expect(
					dns.AnswerCount().Ge(1),
					dns.Elapsed().Le(5*time.Second),
				),
			f.GlobalIP().
				IPv4().
				Expect(
					globalip.AddressCount().Ge(1),
				),
			f.PathMTU("8.8.8.8").
				Min(1200).
				Expect(
					pmtu.Discovered().IsTrue(),
					pmtu.PathMTU().Ge(1200),
				),
			f.Traceroute("8.8.8.8").
				MaxHops(30).
				Expect(
					trace.Elapsed().Le(30*time.Second),
					trace.Assert("has output", func(r trace.Result) error {
						if r.Output == "" {
							return fmt.Errorf("empty traceroute output")
						}
						return nil
					}),
				),
		},
	})
}
