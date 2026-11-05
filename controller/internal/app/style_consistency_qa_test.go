package app

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mi2428/dropcheck-agent/controller/internal/adbdiag"
	"github.com/mi2428/dropcheck-agent/controller/internal/controlpb"
	"github.com/mi2428/dropcheck-agent/controller/internal/render"
)

func TestStyleConsistencySupplementMetadata(t *testing.T) {
	for _, width := range []int{0, 8, 24, 40, 80, 120} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			s := commandResultSupplements{}
			s.addText(func(render.Presentation) string {
				return sourcedADBSupplement("ADB MLO Snapshot\n" + render.SourceFields([][2]string{{"mlo_mode", "0"}, {"wifi_link_count", "1"}}))
			})
			text := s.appendToText("Android result\n", render.Presentation{Width: width})
			if width != 0 && width < 80 {
				if text == "" {
					t.Fatal("supplement lost")
				}
				return
			}
			columns := map[string]int{}
			for _, label := range []string{"Source", "Received on controller", "mlo_mode", "wifi_link_count"} {
				prefix := "  " + label + ":"
				for line := range strings.SplitSeq(text, "\n") {
					if after, ok := strings.CutPrefix(line, prefix); ok {
						tail := after
						columns[label] = len(prefix) + len(tail) - len(strings.TrimLeft(tail, " "))
					}
				}
				if _, ok := columns[label]; !ok {
					t.Errorf("supplement %s must be a two-space-indented record", label)
				}
			}
			for label, column := range columns {
				if column != columns["Source"] {
					t.Errorf("supplement %s value column %d differs from Source %d", label, column, columns["Source"])
				}
			}
		})
	}
}

func TestStyleConsistencySessionAndHistoryRoles(t *testing.T) {
	s := &shellState{selected: "current", defaultPassphrase: "qa-synthetic-secret", lastWifiTestAgent: "acquired", lastWifiTest: &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_CANCELED, Payload: &controlpb.CommandResult_WifiAssert{WifiAssert: &controlpb.WifiAssertResult{}}}}
	text := renderShellSession(s)
	if strings.Contains(text, "qa-synthetic-secret") || !strings.Contains(text, "  Target:") || !strings.Contains(text, "present") {
		t.Fatal("session fields lost role or privacy")
	}
	for _, selected := range []string{"current", "other", ""} {
		s.selected = selected
		text, err := renderLastShellTest(s, render.Presentation{Width: 120, Interactive: true})
		if err != nil || !strings.Contains(text, "Historical test wifi") || !strings.Contains(text, "CANCELED") {
			t.Fatal("historical typed outcome lost", err)
		}
		found := false
		for line := range strings.SplitSeq(text, "\n") {
			if after, ok := strings.CutPrefix(line, "  Target:"); ok {
				found = strings.TrimSpace(after) == "acquired"
			}
		}
		if !found {
			t.Fatal("historical target followed current selection")
		}
	}
}

func TestStyleConsistencyRASupplementOwnedHierarchy(t *testing.T) {
	summary := adbdiag.IPv6RASummary{Interface: "wlan0", AcceptRA: "0", Advertisements: []adbdiag.IPv6RAAdvertisement{{Source: "fe80::1", Destination: "ff02::1", RouterLifetime: "0"}}}
	received := time.Date(2026, 11, 5, 1, 2, 3, 0, time.UTC)
	var supplements commandResultSupplements
	supplements.addText(func(render.Presentation) string {
		return sourcedADBSupplement(adbdiag.IPv6RASummaryText(summary), received)
	})
	for _, width := range []int{0, 8, 24, 40, 80, 120} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			text := supplements.appendToText("Android result\n", render.Presentation{Width: width})
			compact := strings.Join(strings.Fields(text), "")
			for _, fact := range []string{"Source:adb(hostsupplement)", "interface:wlan0", "accept_ra:0", "src:fe80::1", "router_lifetime:0", received.Format(time.RFC3339Nano)} {
				if !strings.Contains(compact, fact) {
					t.Errorf("RA supplement lost %q at width %d", fact, width)
				}
			}
			if width == 0 || width >= 80 {
				column := -1
				for _, label := range []string{"Source", "Received on controller", "interface", "accept_ra"} {
					found := false
					for line := range strings.SplitSeq(text, "\n") {
						if tail, ok := strings.CutPrefix(line, "  "+label+":"); ok {
							found = true
							got := 2 + len(label) + 1 + len(tail) - len(strings.TrimLeft(tail, " "))
							if column >= 0 && column != got {
								t.Errorf("RA metadata/body value columns differ: %d != %d", column, got)
							}
							column = got
						}
					}
					if !found {
						t.Errorf("missing owned RA field %s", label)
					}
				}
				if !strings.Contains(text, "  advertisements\n    ra_1\n      src:") {
					t.Error("RA supplement lost nested heading/field roles")
				}
			}
		})
	}
}
