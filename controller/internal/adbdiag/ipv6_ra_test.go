package adbdiag

import (
	"strings"
	"testing"
)

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
	})
	for _, want := range []string{
		"ADB IPv6 RA",
		"interface",
		"wlan0",
		"default_route",
		"missing",
		"accept_ra_min_lft",
		"180",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("rendered output = %q, missing %q", out, want)
		}
	}
}
