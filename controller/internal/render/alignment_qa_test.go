package render

import (
	"testing"

	"dropcheck/controller/internal/command"
	"dropcheck/controller/internal/controlpb"
	"google.golang.org/protobuf/proto"
)

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
