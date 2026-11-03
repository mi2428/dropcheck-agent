package render

import (
	"strings"
	"testing"
	"unicode"

	"dropcheck/controller/internal/command"
	"dropcheck/controller/internal/controlpb"
	"google.golang.org/protobuf/proto"
)

func alignmentCompact(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, text)
}

func TestAlignmentQAFieldsAndRawBoundaries(t *testing.T) {
	rows := [][2]string{{"短い", "first"}, {"Long label", "second"}, {"URL", "https://example.test/" + strings.Repeat("x", 80)}}
	wide := Fields(rows, Presentation{Width: 120})
	for line := range strings.SplitSeq(strings.TrimSuffix(wide, "\n"), "\n") {
		if displayColumn(line, ":  ") != 12 {
			t.Fatalf("field columns differ: %q", line)
		}
	}
	for _, width := range []int{8, 24, 40} {
		out := Fields(rows, Presentation{Width: width})
		for _, row := range rows {
			if !strings.Contains(alignmentCompact(out), row[1]) {
				t.Fatalf("field value clipped at width=%d", width)
			}
		}
	}
	narrow := Fields([][2]string{{strings.Repeat("Long label ", 8), "value"}}, Presentation{Width: 24})
	for line := range strings.SplitSeq(strings.TrimSuffix(narrow, "\n"), "\n") {
		if !strings.HasPrefix(line, "  ") {
			t.Fatalf("wrapped label escaped its indentation: %q", line)
		}
	}
	raw := "Raw:    untouched:    spacing\nhttps://example.test/a:b"
	if got := TextBlock(raw, Presentation{}); got != raw+"\n" {
		t.Fatal("raw/URL text was guessed to be structured fields", got)
	}
}

func TestAlignmentQALiteralSelectionKeepsOneDetachedContext(t *testing.T) {
	wanted := &controlpb.WifiConnection{Ssid: "Guest\t東京", Bssid: "02:00:00:11:22:01"}
	other := &controlpb.WifiConnection{Ssid: "Guest 東京", Bssid: "02:00:00:11:22:02"}
	result := &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_FAILED, Payload: &controlpb.CommandResult_WifiDiagnostics{WifiDiagnostics: &controlpb.WifiDiagnostics{
		Status: &controlpb.WifiStatus{Connection: wanted, IpStatus: &controlpb.IpStatus{Wifi: other}},
		Scan: &controlpb.WifiScan{Results: []*controlpb.WifiScanResult{{Ssid: wanted.Ssid, Bssid: wanted.Bssid}, {Ssid: other.Ssid, Bssid: other.Bssid}},
			Fields: []*controlpb.DiagnosticField{{Key: "scan_result_total_count", Value: "100"}}},
		Networks: []*controlpb.NetworkDiagnostics{{IpStatus: &controlpb.IpStatus{Wifi: wanted}}, {IpStatus: &controlpb.IpStatus{Wifi: other}}, nil},
	}}}
	before := proto.Clone(result)
	for _, identity := range []*controlpb.WifiConnection{wanted, other} {
		selected := presentationResult(result, command.Options{WifiRenderMode: command.WifiRenderModeEHT, WifiEHTSSID: identity.Ssid})
		d := selected.GetWifiDiagnostics()
		if len(d.Scan.Results) != 1 || d.Scan.Results[0].Bssid != identity.Bssid || len(d.Networks) != 1 || d.Networks[0].GetIpStatus().GetWifi().Bssid != identity.Bssid {
			t.Fatal("selection included another observation")
		}
		if identity == wanted && (d.Status.Connection == nil || d.Status.IpStatus != nil) || identity == other && (d.Status.Connection != nil || d.Status.IpStatus == nil) {
			t.Fatal("connection and independent IP context did not retain literal selection")
		}
		if selected.Status != result.Status || diagnosticFieldMap(d.Scan.Fields)["scan_result_total_count"] != "100" || !proto.Equal(result, before) {
			t.Fatal("presentation changed acquisition outcome, totals or original data")
		}
	}
}
