//go:build harness || festival

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

	legacyEnvSSID             = "DROPCHECK_FESTIVAL_WIFI_SSID"
	legacyEnvPSK              = "DROPCHECK_FESTIVAL_WIFI_PSK"
	legacyEnvPSKName          = "DROPCHECK_FESTIVAL_WIFI_PSK_ENV"
	legacyEnvBSSID            = "DROPCHECK_FESTIVAL_WIFI_BSSID"
	legacyEnvBand             = "DROPCHECK_FESTIVAL_WIFI_BAND"
	legacyEnvRequireValidated = "DROPCHECK_FESTIVAL_REQUIRE_VALIDATED"
	legacyEnvStandard         = "DROPCHECK_FESTIVAL_WIFI_STANDARD"
	legacyEnvChannel          = "DROPCHECK_FESTIVAL_WIFI_CHANNEL"
	legacyEnvChannelWidth     = "DROPCHECK_FESTIVAL_WIFI_CHANNEL_WIDTH"
)

func TestHarnessSmoke(t *testing.T) {
	ssid := envValue(envSSID, legacyEnvSSID)
	pskEnv := firstNonEmpty(os.Getenv(envPSKName), os.Getenv(legacyEnvPSKName))
	if pskEnv == "" {
		pskEnv = firstSetEnvName(envPSK, legacyEnvPSK)
	}
	if ssid == "" || os.Getenv(pskEnv) == "" {
		t.Skipf("set %s and %s to run the Dropcheck Harness smoke test", envSSID, pskEnv)
	}
	bssid := envValue(envBSSID, legacyEnvBSSID)
	band := envValue(envBand, legacyEnvBand)
	requireValidated := envValue(envRequireValidated, legacyEnvRequireValidated) == "1"
	standard := envValue(envStandard, legacyEnvStandard)
	channel := envValue(envChannel, legacyEnvChannel)
	channelWidth := envValue(envChannelWidth, legacyEnvChannelWidth)

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

func envValue(name string, legacy string) string {
	return firstNonEmpty(os.Getenv(name), os.Getenv(legacy))
}

func firstSetEnvName(name string, legacy string) string {
	switch {
	case os.Getenv(name) != "":
		return name
	case os.Getenv(legacy) != "":
		return legacy
	default:
		return name
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
