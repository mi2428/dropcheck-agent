package adbdiag

import (
	"strings"
	"testing"
)

const sampleIPv6RARawHex = "33330000000100090f09641a86dd6000000000583afffe800000000000000000000021320003ff02000000000000000000000000000186004042400800000000000000000000010100090f09641a030440c000001c2000000e1000000000200103e8000e21320000000000000000030440c000001c2000000e1000000000200103e8000e21320000000000000000"

func TestSummarizeIPv6DefaultRoutes(t *testing.T) {
	present, gateways := summarizeIPv6DefaultRoutes([]string{
		"0.0.0.0/0 -> 10.20.0.1 wlan0",
		"::/0 -> fe80::1 wlan0",
		"default via fe80::2 dev wlan0 proto ra",
	})
	if !present {
		t.Fatalf("present = false, want true")
	}
	if got, want := strings.Join(gateways, ","), "fe80::1,fe80::2"; got != want && got != "fe80::2,fe80::1" {
		t.Fatalf("gateways = %q, want fe80::1 and fe80::2", got)
	}
}

func TestParseIPv6RARawHex(t *testing.T) {
	ad, ok := parseIPv6RARawHex(sampleIPv6RARawHex)
	if !ok {
		t.Fatal("parseIPv6RARawHex() returned ok=false")
	}
	if ad.Source != "fe80::2132:3" {
		t.Fatalf("source = %q, want fe80::2132:3", ad.Source)
	}
	if ad.Destination != "ff02::1" {
		t.Fatalf("destination = %q, want ff02::1", ad.Destination)
	}
	if ad.RouterLifetime != "0s" {
		t.Fatalf("router lifetime = %q, want 0s", ad.RouterLifetime)
	}
	if ad.HopLimit != 64 {
		t.Fatalf("hop limit = %d, want 64", ad.HopLimit)
	}
	if ad.FlagsHex != "0x08" {
		t.Fatalf("flags = %q, want 0x08", ad.FlagsHex)
	}
	if len(ad.Prefixes) != 1 {
		t.Fatalf("prefix count = %d, want 1", len(ad.Prefixes))
	}
	if got := ad.Prefixes[0]; got.Prefix != "2001:3e8:e:2132::/64" || got.ValidLifetime != "7200s" || got.PreferredLifetime != "3600s" {
		t.Fatalf("prefix = %#v, want 2001:3e8:e:2132::/64 valid=7200s preferred=3600s", got)
	}
}

func TestParseIPv6RAAdvertisements(t *testing.T) {
	text := `
Recently active IpClient logs:
IpClient.wlan0
  IpClient.wlan0 APF dump:
    RA filters:
      Filtered:
      RA fe80::2132:3 -> ff02::1 0s 2001:3e8:e:2132::/64 7200s/3600s 2001:3e8:e:2132::/64 7200s/3600s
        Last seen 474s ago
        Last match:
          ` + sampleIPv6RARawHex + `
`
	ads := parseIPv6RAAdvertisements(text, "wlan0")
	if len(ads) != 1 {
		t.Fatalf("advertisement count = %d, want 1", len(ads))
	}
	if ads[0].LastSeen != "474s" {
		t.Fatalf("last seen = %q, want 474s", ads[0].LastSeen)
	}
	if len(ads[0].Prefixes) != 1 {
		t.Fatalf("prefix count = %d, want 1", len(ads[0].Prefixes))
	}
}

func TestRenderIPv6RASummary(t *testing.T) {
	out := RenderIPv6RASummary(IPv6RASummary{
		Interface:         "wlan0",
		DefaultRoute:      "missing",
		AcceptRA:          "2",
		AcceptRADefrtr:    "1",
		AcceptRAPinfo:     "1",
		AcceptRARtrPref:   "1",
		AcceptRAMinLft:    "180",
		AcceptRAMinHopLft: "1",
		AcceptRAFromLocal: "0",
		Advertisements: []IPv6RAAdvertisement{{
			Source:         "fe80::2132:3",
			Destination:    "ff02::1",
			LastSeen:       "474s",
			RouterLifetime: "0s",
			HopLimit:       64,
			FlagsHex:       "0x08",
			Prefixes: []IPv6RAPrefix{{
				Prefix:            "2001:3e8:e:2132::/64",
				ValidLifetime:     "7200s",
				PreferredLifetime: "3600s",
			}},
		}},
	})
	for _, want := range []string{
		"ADB IPv6 RA",
		"interface",
		"wlan0",
		"default_route",
		"missing",
		"accept_ra_min_lft",
		"180",
		"ra_1",
		"router_lifetime=0s",
		"valid=7200s",
		"preferred=3600s",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("rendered output = %q, missing %q", out, want)
		}
	}
}
