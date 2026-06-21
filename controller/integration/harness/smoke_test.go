//go:build harness

package harness

import (
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	h "dropcheck/controller/internal/harness"
	"dropcheck/controller/internal/harness/capabilities"
	"dropcheck/controller/internal/harness/dns"
	"dropcheck/controller/internal/harness/globalip"
	"dropcheck/controller/internal/harness/ip"
	"dropcheck/controller/internal/harness/ping"
	"dropcheck/controller/internal/harness/pmtu"
	"dropcheck/controller/internal/harness/scan"
	"dropcheck/controller/internal/harness/trace"
	"dropcheck/controller/internal/harness/wifi"
)

const (
	envSSID             = "DROPCHECK_HARNESS_WIFI_SSID"
	envPSK              = "DROPCHECK_HARNESS_WIFI_PSK"
	envPSKName          = "DROPCHECK_HARNESS_WIFI_PSK_ENV"
	envBSSID            = "DROPCHECK_HARNESS_WIFI_BSSID"
	envBand             = "DROPCHECK_HARNESS_WIFI_BAND"
	envRequireValidated = "DROPCHECK_HARNESS_REQUIRE_VALIDATED"
	envStandard         = "DROPCHECK_HARNESS_WIFI_STANDARD"
	envChannel          = "DROPCHECK_HARNESS_WIFI_CHANNEL"
	envChannelWidth     = "DROPCHECK_HARNESS_WIFI_CHANNEL_WIDTH"
)

func TestHarnessSmoke(t *testing.T) {
	ssid := os.Getenv(envSSID)
	pskEnv := os.Getenv(envPSKName)
	if pskEnv == "" {
		pskEnv = envPSK
	}
	if ssid == "" || os.Getenv(pskEnv) == "" {
		t.Skipf("set %s and %s to run the Dropcheck Harness smoke test", envSSID, pskEnv)
	}
	bssid := os.Getenv(envBSSID)
	band := os.Getenv(envBand)
	requireValidated := os.Getenv(envRequireValidated) == "1"
	standard := os.Getenv(envStandard)
	channel := os.Getenv(envChannel)
	channelWidth := os.Getenv(envChannelWidth)

	network := h.WiFi("smoke-wifi").
		SSID(ssid).
		PSKEnv(pskEnv).
		BSSID(bssid).
		Band(band).
		RequireValidated(requireValidated)

	ipExpect := []h.Expectation{
		ip.AddressCount().Ge(1),
		ip.MTU().Ge(1280),
	}
	wifiExpect := []h.Expectation{
		wifi.Enabled().IsTrue(),
		wifi.SSID().Eq(ssid),
	}
	ap := scan.APs().SSID(ssid)
	capabilityExpect := []h.Expectation{capabilities.ErrorCount().Eq(0)}
	if bssid != "" {
		wifiExpect = append(wifiExpect, wifi.BSSID().Eq(bssid))
		ap = ap.BSSID(bssid)
	}
	if standard != "" {
		wifiExpect = append(wifiExpect, wifi.Standard().Eq(wifi.StandardName(standard)))
		ap = ap.Standard(standard)
		capabilityExpect = append(capabilityExpect, capabilities.Standard(standard).Supported())
	}
	if channel != "" {
		value, err := strconv.ParseInt(channel, 10, 32)
		if err != nil {
			t.Fatalf("%s: %v", envChannel, err)
		}
		wifiExpect = append(wifiExpect, wifi.Channel().Eq(int32(value)))
		ap = ap.Channel(int32(value))
	}
	if channelWidth != "" {
		ap = ap.ChannelWidth(channelWidth)
	}

	h.Run(t, h.Plan{
		Networks: []h.Network{network},
		Checks: []h.Check{
			h.IPStatus().
				Expect(ipExpect...),
			h.WiFiStatus().
				Expect(wifiExpect...),
			h.WiFiScan().
				Fresh().
				Band(band).
				Expect(ap.Exists()),
			h.WiFiCapabilities().
				Expect(capabilityExpect...),
			h.Ping("8.8.8.8").
				Count(5).
				Expect(
					ping.Received().Ge(1),
					ping.LossPercent().Le(100),
					ping.AvgLatency().Gt(0).Le(2*time.Second),
				),
			h.DNS("example.com").
				A().
				Expect(
					dns.AnswerCount().Ge(1),
					dns.Elapsed().Le(5*time.Second),
				),
			h.GlobalIP().
				IPv4().
				Expect(
					globalip.AddressCount().Ge(1),
				),
			h.PathMTU("8.8.8.8").
				Min(1200).
				Expect(
					pmtu.Discovered().IsTrue(),
					pmtu.PathMTU().Ge(1200),
				),
			h.Traceroute("8.8.8.8").
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
