package tuirun

import (
	"fmt"
	"testing"

	"github.com/mi2428/dropcheck-agent/controller/internal/controlpb"
)

func TestExpectationMappingsRejectConflictingOrNonScalarValues(t *testing.T) {
	for _, values := range []map[string]any{
		{"addresses": map[string]any{"contains": []any{"192.0.2.1/24"}, "exact": []any{"192.0.2.99/24"}}},
		{"addresses": map[string]any{"cidr": "192.0.2.0/24", "cidrs": "198.51.100.0/24"}},
		{"error": map[string]any{"exact": nil}},
		{"ssid": map[string]any{"contains": map[string]any{"nested": "Lab"}}},
		{"ap": map[string]any{"ssid": nil}},
		{"ap": map[string]any{"ssid": []any{"Lab"}}},
		{"ap": map[string]any{"count": 0, "min_count": 1}},
	} {
		if _, err := CompileExpectations(values); err == nil {
			t.Errorf("invalid expectation mapping accepted: %#v", values)
		}
	}
	for _, value := range []any{"<nil>", false, 42, int64(42), uint64(42), float64(4.2), []any{}} {
		if _, err := CompileExpectations(map[string]any{"error": map[string]any{"exact": value}}); err != nil {
			t.Errorf("valid scalar or empty set rejected: %#v: %v", value, err)
		}
	}
	for _, value := range []any{" Lab ", 123, false} {
		expect, err := CompileExpectations(map[string]any{"ap": map[string]any{"ssid": value}})
		if err != nil {
			t.Fatal(err)
		}
		raw := &controlpb.CommandResult{Payload: &controlpb.CommandResult_WifiScan{WifiScan: &controlpb.WifiScan{
			Results: []*controlpb.WifiScanResult{{Ssid: fmt.Sprint(value)}},
		}}}
		if finding := expect[0].Evaluate(Result{Run: RunResult{Raw: raw}})[0]; !finding.Passed || finding.Missing {
			t.Errorf("literal AP selector changed: %#v: %+v", value, finding)
		}
	}
}
