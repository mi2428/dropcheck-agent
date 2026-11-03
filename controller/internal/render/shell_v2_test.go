package render

import (
	"strings"
	"testing"

	"dropcheck/controller/internal/command"
	"dropcheck/controller/internal/controlpb"
	"dropcheck/controller/internal/pipeline"
)

func TestInteractiveScanDoesNotRepeatIdentity(t *testing.T) {
	ssid := "Very long safe SSID that must never be clipped"
	mac := "02:00:00:11:22:33"
	raw := &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_OK, Payload: &controlpb.CommandResult_WifiScan{WifiScan: &controlpb.WifiScan{Results: []*controlpb.WifiScanResult{{Ssid: ssid, Bssid: mac, FrequencyMhz: 6135}}}}}
	for _, width := range []int{0, 24, 40, 80, 120} {
		out, err := CommandResult("self", raw, command.Options{}, pipeline.FormatText, Presentation{Width: width, Interactive: true, DetailAvailable: true})
		if err != nil {
			t.Fatal(err)
		}
		full := alignmentCompact(out)
		if strings.Count(full, alignmentCompact(ssid)) != 1 || strings.Count(full, mac) != 1 {
			t.Fatalf("identity duplicated/lost at %d: %s", width, out)
		}
	}
	logical, err := CommandResult("self", raw, command.Options{}, pipeline.FormatText, Presentation{Interactive: true, DetailAvailable: true})
	if err != nil {
		t.Fatal(err)
	}
	filter, err := pipeline.Parse([]string{`match "SSID *: +Very long"`})
	if err != nil {
		t.Fatal(err)
	}
	out, _ := filter.Apply(logical)
	for _, width := range []int{24, 40, 80, 120} {
		if !strings.Contains(alignmentCompact(LayoutText(out, Presentation{Width: width})), alignmentCompact(ssid)) {
			t.Fatal("filter depended on layout")
		}
	}
}

func TestDiagnosticFieldURLIsSanitizedBeforeFiltering(t *testing.T) {
	raw := &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_OK, Payload: &controlpb.CommandResult_PermissionSnapshot{PermissionSnapshot: &controlpb.PermissionSnapshot{Permissions: []*controlpb.DiagnosticField{{Key: "diagnostic_url", Value: "https://example.test/?token=synthetic-private-value"}}}}}
	out, err := CommandResult("self", raw, command.Options{}, pipeline.FormatText, Presentation{Interactive: true})
	if err != nil || strings.Contains(out, "synthetic-private-value") {
		t.Fatalf("unsafe diagnostic field: %s %v", out, err)
	}
}
