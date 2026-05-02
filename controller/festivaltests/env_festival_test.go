//go:build festival

package festivaltests

import (
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"dropcheck/controller/internal/festival"
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

func TestEnvFestival(t *testing.T) {
	ssid := os.Getenv("FESTIVAL_WIFI_SSID")
	pskEnv := os.Getenv("FESTIVAL_WIFI_PSK_ENV")
	if pskEnv == "" {
		pskEnv = "FESTIVAL_WIFI_PSK"
	}
	if ssid == "" || os.Getenv(pskEnv) == "" {
		t.Skipf("set FESTIVAL_WIFI_SSID and %s to run the festival scenario", pskEnv)
	}
	network := festival.WiFi("env-wifi").
		SSID(ssid).
		PSKEnv(pskEnv).
		BSSID(os.Getenv("FESTIVAL_WIFI_BSSID")).
		Band(os.Getenv("FESTIVAL_WIFI_BAND")).
		RequireValidated(os.Getenv("FESTIVAL_REQUIRE_VALIDATED") == "1")

	ipExpect := []festival.Expectation{
		ip.AddressCount().Ge(1),
		ip.MTU().Ge(1280),
	}
	wifiExpect := []festival.Expectation{
		wifi.Enabled().IsTrue(),
		wifi.SSID().Eq(ssid),
	}
	ap := scan.APs().SSID(ssid)
	capabilityExpect := []festival.Expectation{capabilities.ErrorCount().Eq(0)}
	if bssid := os.Getenv("FESTIVAL_WIFI_BSSID"); bssid != "" {
		wifiExpect = append(wifiExpect, wifi.BSSID().Eq(bssid))
		ap = ap.BSSID(bssid)
	}
	if standard := os.Getenv("FESTIVAL_WIFI_STANDARD"); standard != "" {
		wifiExpect = append(wifiExpect, wifi.Standard().Eq(wifi.StandardName(standard)))
		ap = ap.Standard(standard)
		capabilityExpect = append(capabilityExpect, capabilities.Standard(standard).Supported())
	}
	if channel := os.Getenv("FESTIVAL_WIFI_CHANNEL"); channel != "" {
		value, err := strconv.ParseInt(channel, 10, 32)
		if err != nil {
			t.Fatalf("FESTIVAL_WIFI_CHANNEL: %v", err)
		}
		wifiExpect = append(wifiExpect, wifi.Channel().Eq(int32(value)))
		ap = ap.Channel(int32(value))
	}
	if width := os.Getenv("FESTIVAL_WIFI_CHANNEL_WIDTH"); width != "" {
		wifiExpect = append(wifiExpect, wifi.ChannelWidth().Eq(wifi.ChannelWidthName(width)))
		ap = ap.ChannelWidth(width)
	}

	festival.Run(t, festival.Plan{
		Networks: []festival.Network{network},
		Checks: []festival.Check{
			festival.IPStatus().
				Expect(ipExpect...),
			festival.WiFiStatus().
				Expect(wifiExpect...),
			festival.WiFiScan().
				Fresh().
				Band(os.Getenv("FESTIVAL_WIFI_BAND")).
				Expect(ap.Exists()),
			festival.WiFiCapabilities().
				Expect(capabilityExpect...),
			festival.Ping("8.8.8.8").
				Count(5).
				Expect(
					ping.Received().Ge(1),
					ping.LossPercent().Le(100),
					ping.AvgLatency().Gt(0).Le(2*time.Second),
				),
			festival.DNS("example.com").
				A().
				Expect(
					dns.AnswerCount().Ge(1),
					dns.Elapsed().Le(5*time.Second),
				),
			festival.GlobalIP().
				IPv4().
				Expect(
					globalip.AddressCount().Ge(1),
				),
			festival.PathMTU("8.8.8.8").
				Min(1200).
				Expect(
					pmtu.Discovered().IsTrue(),
					pmtu.PathMTU().Ge(1200),
				),
			festival.Traceroute("8.8.8.8").
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
