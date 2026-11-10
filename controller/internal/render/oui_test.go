package render

import (
	"strings"
	"testing"

	"github.com/mi2428/dropcheck-agent/controller/internal/command"
	"github.com/mi2428/dropcheck-agent/controller/internal/controlpb"
	"github.com/mi2428/dropcheck-agent/controller/internal/oui"
	"github.com/mi2428/dropcheck-agent/controller/internal/pipeline"
	"google.golang.org/protobuf/proto"
)

func TestBSSIDVendorsInScanStatusIPMLOAndSafeJSON(t *testing.T) {
	conn := &controlpb.WifiConnection{Ssid: "Lab", Bssid: "e8:0a:b9:00:00:01", ObservationFields: testObservationFields("identity")}
	status := &controlpb.WifiStatus{Connection: conn}
	scan := &controlpb.WifiScan{Results: []*controlpb.WifiScanResult{{Ssid: "Lab", Bssid: conn.Bssid}, {Ssid: "Local", Bssid: "02:00:00:00:00:01"}}}
	for _, tc := range []struct {
		result  *controlpb.CommandResult
		options command.Options
	}{
		{&controlpb.CommandResult{Payload: &controlpb.CommandResult_WifiScan{WifiScan: scan}}, command.Options{}},
		{&controlpb.CommandResult{Payload: &controlpb.CommandResult_WifiStatus{WifiStatus: status}}, command.Options{WifiRenderMode: command.WifiRenderModeStatus}},
		{&controlpb.CommandResult{Payload: &controlpb.CommandResult_WifiStatus{WifiStatus: status}}, command.Options{}},
		{&controlpb.CommandResult{Payload: &controlpb.CommandResult_IpStatus{IpStatus: &controlpb.IpStatus{Wifi: conn}}}, command.Options{}},
		{&controlpb.CommandResult{Payload: &controlpb.CommandResult_WifiDiagnostics{WifiDiagnostics: &controlpb.WifiDiagnostics{Status: status, Scan: scan}}}, command.Options{WifiRenderMode: command.WifiRenderModeEHT}},
	} {
		before := proto.Clone(tc.result)
		out, err := CommandResult("self", tc.result, tc.options, Presentation{Detail: true})
		if err != nil || !strings.Contains(out, "Cisco") {
			t.Fatalf("vendor missing for %T: %s %v", tc.result.Payload, out, err)
		}
		encoded, err := pipeline.JSON(out, true)
		if err != nil || !strings.Contains(encoded, "Cisco") || !proto.Equal(before, tc.result) {
			t.Fatal("vendor lost from JSON or mutated acquisition", err)
		}
	}
	result := &controlpb.CommandResult{Payload: &controlpb.CommandResult_WifiScan{WifiScan: scan}}
	out, err := CommandResult("self", result, command.Options{WifiScanBSSID: "02:00:00:00:00:01"})
	if err != nil || strings.Contains(out, "Cisco") || !strings.Contains(out, "local MAC") {
		t.Fatalf("filter widened vendor attribution: %s %v", out, err)
	}
	conn.ObservationFields = nil
	out, err = CommandResult("self", &controlpb.CommandResult{Payload: &controlpb.CommandResult_WifiStatus{WifiStatus: status}}, command.Options{WifiRenderMode: command.WifiRenderModeStatus})
	if err != nil || strings.Contains(out, "Cisco") {
		t.Fatal("unavailable identity acquired a vendor", out, err)
	}
}

func TestNestedScanTableReservesItsParentIndentation(t *testing.T) {
	ap := &controlpb.WifiScanResult{Ssid: "Lab", Bssid: "e8:0a:b9:00:00:01", RssiDbm: -40}
	columns := []displayTableColumn{{header: "AP"}, {header: "SSID"}, {header: "BSSID"}, {header: "dBm"}, {header: "BAND"}, {header: "CH"}, {header: "BW"}, {header: "PHY"}, {header: "SEC"}, {header: "VENDOR"}}
	row := append([]string{"1"}, adaptiveScanRow(ap)...)
	row = append(row, oui.Vendor(ap.Bssid))
	var table strings.Builder
	writeDisplayTable(&table, columns, [][]string{row})
	width := 0
	for line := range strings.SplitSeq(table.String(), "\n") {
		width = max(width, displayWidth(line))
	}
	result := &controlpb.CommandResult{Payload: &controlpb.CommandResult_WifiDiagnostics{WifiDiagnostics: &controlpb.WifiDiagnostics{Scan: &controlpb.WifiScan{Results: []*controlpb.WifiScanResult{ap}}}}}
	out, err := CommandResult("self", result, command.Options{}, Presentation{Width: width + 1})
	if err != nil {
		t.Fatal(err)
	}
	fallback := false
	for line := range strings.SplitSeq(out, "\n") {
		if displayWidth(line) > width+1 {
			t.Fatalf("nested table overflow: %q", line)
		}
		if strings.HasPrefix(strings.TrimSpace(line), "BSSID:") {
			fallback = true
		}
	}
	if !fallback {
		t.Fatalf("nested scan used its parent's two columns as table space:\n%s", out)
	}
}
