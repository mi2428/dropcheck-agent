package adbdiag

import (
	"strings"
	"testing"

	"dropcheck/controller/internal/pipeline"
)

func TestSpecsFullIncludesRawWifiAndConnectivityCommands(t *testing.T) {
	specs, err := Specs(KindFull)
	if err != nil {
		t.Fatalf("Specs(full) error = %v", err)
	}
	var names []string
	for _, spec := range specs {
		names = append(names, spec.Name)
	}
	joined := strings.Join(names, "\n")
	for _, want := range []string{"cmd wifi status", "dumpsys wifi", "dumpsys connectivity", "dumpsys connectivity --diag", "dumpsys connectivity trafficcontroller"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("Specs(full) names = %q, missing %q", joined, want)
		}
	}
}

func TestRenderTextIncludesCommandOutput(t *testing.T) {
	out, err := Render(Bundle{
		Agent:  "agent-1",
		Serial: "R5CT12345",
		Kind:   KindCmdWifiStatus,
		Commands: []CommandResult{{
			Name:      "cmd wifi status",
			Command:   "adb -s R5CT12345 shell cmd wifi status",
			Stdout:    "Wifi is enabled\n",
			ExitCode:  0,
			ElapsedMs: 12,
		}},
	}, pipeline.FormatText)
	if err != nil {
		t.Fatalf("Render(text) error = %v", err)
	}
	for _, want := range []string{"ADB Diagnostics", "serial", "R5CT12345", "cmd wifi status", "stdout", "Wifi is enabled"} {
		if !strings.Contains(out, want) {
			t.Fatalf("Render(text) = %q, missing %q", out, want)
		}
	}
}

func TestParseMLOSummaryExtractsCmdWifiAndDumpsysFields(t *testing.T) {
	text := `WifiInfo: SSID: "Lab" MLO Information: , Is TID-To-Link negotiation supported by the AP: false, AP MLD Address: 02:00:00:00:00:01, AP MLO Link Id: 2, AP MLO Affiliated links: [link_id=2], Vendor Data: <none>
timestamp_ms=1,wifi_link_count=1,Link Stats from link_id=2,state=2,radio_id=0,frequency_mhz=5975,beacon_rx=42,rssi_mgmt=-47,rssi=-48,channel_width=3,mlo_mode=1`

	summary := ParseMLOSummary(text)
	if summary.TIDToLinkSupported != "false" ||
		summary.APMLDAddress != "02:00:00:00:00:01" ||
		summary.APMLOLinkID != "2" ||
		summary.MLOMode != "1" ||
		summary.WifiLinkCount != "1" ||
		len(summary.LinkStats) != 1 {
		t.Fatalf("summary = %#v", summary)
	}
	link := summary.LinkStats[0]
	if link.LinkID != "2" || link.FrequencyMHz != "5975" || link.RSSI != "-48" || link.RSSIMgmt != "-47" || link.ChannelWidth != "3" {
		t.Fatalf("link = %#v", link)
	}
}

func TestRenderMLOSummary(t *testing.T) {
	out := RenderMLOSummary(MLOSummary{
		TIDToLinkSupported: "false",
		APMLDAddress:       "<none>",
		APMLOLinkID:        "<none>",
		MLOMode:            "0",
		WifiLinkCount:      "1",
		LinkStats: []MLOLinkStat{{
			LinkID:       "0",
			State:        "2",
			RadioID:      "0",
			FrequencyMHz: "5220",
			RSSI:         "-58",
			RSSIMgmt:     "-58",
			ChannelWidth: "0",
			BeaconRx:     "472263",
		}},
	})
	for _, want := range []string{
		"ADB MLO Snapshot",
		"tid_to_link",
		"false",
		"mlo_mode",
		"0",
		"wifi_link_count",
		"1",
		"ADB MLO Link Stats",
		"radio_id",
		"rssi_mgmt",
		"beacon_rx",
		"472263",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("RenderMLOSummary() = %q, missing %q", out, want)
		}
	}
	for _, unwanted := range []string{"source", "ap_mld", "ap_link_id", "MLO Links", "5220"} {
		if strings.Contains(out, unwanted) {
			t.Fatalf("RenderMLOSummary() = %q, included %q", out, unwanted)
		}
	}
}
