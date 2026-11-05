package app

import (
	"encoding/json"
	"strings"
	"testing"

	"dropcheck/controller/internal/controlpb"
	"dropcheck/controller/internal/render"
)

func TestProfileCatalogueFieldsRemainIndependent(t *testing.T) {
	text, err := showProfiles(outputText)
	if err != nil {
		t.Fatal(err)
	}
	for _, label := range []string{"Purpose", "Prerequisites", "Family", "Cost"} {
		count := 0
		for line := range strings.SplitSeq(text, "\n") {
			if strings.HasPrefix(line, "  "+label+":") {
				count++
			}
		}
		if count != len(profileCatalogue) {
			t.Fatalf("%s is not independent in each profile: %s", label, text)
		}
	}
	encoded, err := showProfiles(outputJSON)
	var value struct {
		Checks []map[string]string `json:"checks"`
	}
	if err != nil || json.Unmarshal([]byte(encoded), &value) != nil || len(value.Checks) != len(profileCatalogue) || value.Checks[0]["purpose"] != profileCatalogue[0].Purpose {
		t.Fatal("structured catalogue changed", err)
	}
}

func TestHistoricalWifiTargetDoesNotFollowCurrentSelection(t *testing.T) {
	state := &shellState{selected: "current-device", lastWifiTestAgent: "acquired-device", lastWifiTest: &controlpb.CommandResult{
		Status: controlpb.CommandResult_STATUS_CANCELED, Payload: &controlpb.CommandResult_WifiAssert{WifiAssert: &controlpb.WifiAssertResult{}},
	}}
	for _, selected := range []string{"current-device", "another-device", ""} {
		state.selected = selected
		text, err := renderLastShellTest(state, render.Presentation{})
		if err != nil {
			t.Fatal(err)
		}
		for line := range strings.SplitSeq(text, "\n") {
			if strings.HasPrefix(line, "  Target:") && strings.TrimSpace(strings.TrimPrefix(line, "  Target:")) != "acquired-device" {
				t.Fatalf("historical target changed with selection: %q", line)
			}
		}
		if !strings.Contains(text, "acquired-device") || !strings.Contains(text, "CANCELED") {
			t.Fatalf("historical facts missing: %s", text)
		}
	}
}
