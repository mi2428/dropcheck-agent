package render

import (
	"strings"
	"testing"

	"github.com/mi2428/dropcheck-agent/controller/internal/command"
	"github.com/mi2428/dropcheck-agent/controller/internal/controlpb"
	"github.com/mi2428/dropcheck-agent/controller/internal/pipeline"
)

func TestAdversarialQAScanDetailPreservesSafelyCollectedVenue(t *testing.T) {
	value := &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_OK, Payload: &controlpb.CommandResult_WifiScanDetail{WifiScanDetail: &controlpb.WifiScanDetail{Target: "fixture", Results: []*controlpb.WifiScanResult{{Ssid: "fixture", Bssid: "02:00:00:11:22:33", OperatorFriendlyName: "qa-operator-name", VenueName: "qa-venue-name"}}}}}
	out, err := CommandResult("self", value, command.Options{Detail: true}, pipeline.FormatText, Presentation{Interactive: true, Detail: true, DetailAvailable: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"qa-operator-name", "qa-venue-name"} {
		if !strings.Contains(out, field) {
			t.Errorf("safe collected field is unreachable in scan detail: %s", field)
		}
	}
}
