package render

import (
	"os"
	"strings"
	"testing"

	"github.com/mi2428/dropcheck-agent/controller/internal/command"
	"github.com/mi2428/dropcheck-agent/controller/internal/controlpb"
	"github.com/mi2428/dropcheck-agent/controller/internal/pipeline"
	"google.golang.org/protobuf/proto"
)

func TestSharedScanAPMarkCases(t *testing.T) {
	data, err := os.ReadFile("../../../testdata/scan_mark_cases.tsv")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")[1:] {
		parts := strings.Split(line, "\t")
		if len(parts) != 5 {
			t.Fatal(line)
		}
		evidence, mac, connected, observation, want := parts[0], parts[1], parts[2], parts[3], parts[4]
		ap := &controlpb.WifiScanResult{Bssid: mac}
		switch evidence {
		case "be":
			ap.WifiStandard = "802.11be"
		case "cap", "truncated":
			ap.EhtCapabilities = &controlpb.WifiEhtCapabilities{Mac: &controlpb.WifiEhtMacCapabilities{}, Phy: &controlpb.WifiEhtPhyCapabilities{}, Truncated: evidence == "truncated"}
		case "empty-cap":
			ap.EhtCapabilities = &controlpb.WifiEhtCapabilities{}
		case "operation":
			ap.EhtOperation = &controlpb.WifiEhtOperation{BasicMcsNssSetHex: "00000000"}
		case "mlo-only":
			ap.ApMldMacAddress = "00:11:22:33:44:55"
		case "bandwidth-only":
			ap.Band, ap.ChannelWidth = "6ghz", "320MHz"
		case "none":
		default:
			t.Fatal(evidence)
		}
		connections := &controlpb.WifiScanConnections{Complete: observation != "partial", ObservedAtUnixMs: 1234}
		if observation == "missing-time" {
			connections.ObservedAtUnixMs = 0
		}
		if connected != "-" {
			connections.Bssids = strings.Split(connected, ",")
		}
		if observation == "partial" || observation == "contradictory" {
			connections.Errors = []string{"synthetic unavailable identity"}
		}
		if want == "blank" {
			want = " "
		}
		if got := scanAPMark(ap, connections); got != want {
			t.Errorf("%s: got %q want %q", line, got, want)
		}
	}
}

func TestRecordedScanConnectionSurvivesFilteringJSONAndDetail(t *testing.T) {
	connections := &controlpb.WifiScanConnections{ObservedAtUnixMs: 1234, Complete: true, Bssids: []string{"00:11:22:33:44:55"}}
	ap := &controlpb.WifiScanResult{Ssid: "Lab", Bssid: connections.Bssids[0], WifiStandard: "802.11be"}
	for _, result := range []*controlpb.CommandResult{
		{Status: controlpb.CommandResult_STATUS_OK, Payload: &controlpb.CommandResult_WifiScan{WifiScan: &controlpb.WifiScan{Connections: connections, Results: []*controlpb.WifiScanResult{ap}}}},
		{Status: controlpb.CommandResult_STATUS_OK, Payload: &controlpb.CommandResult_WifiScanDetail{WifiScanDetail: &controlpb.WifiScanDetail{Connections: connections, Results: []*controlpb.WifiScanResult{ap}}}},
	} {
		before := proto.Clone(result)
		out, err := CommandResult("self", result, command.Options{WifiScanBSSID: ap.Bssid})
		if err != nil || !strings.Contains(out, "\n\n[*] AP 1\n") || !strings.Contains(out, "EHT") || !strings.Contains(out, "1970-01-01T00:00:01.234Z") {
			t.Fatalf("recorded AP identity missing: %s %v", out, err)
		}
		encoded, err := pipeline.JSON(out, true)
		if err != nil || !strings.Contains(encoded, "[*] AP 1") || !proto.Equal(before, result) {
			t.Fatal("JSON lost observation or render mutated acquisition", err)
		}
	}
	if !strings.HasPrefix(scanConnectionsSummary(&controlpb.WifiScanConnections{ObservedAtUnixMs: 1234, Complete: true}), "disconnected") || !strings.Contains(scanConnectionsSummary(nil), "not captured") {
		t.Fatal("unavailable connection mistaken for disconnect")
	}
}
