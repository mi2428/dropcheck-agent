package app

import (
	"strings"
	"testing"

	"github.com/mi2428/dropcheck-agent/controller/internal/controlpb"
	"github.com/mi2428/dropcheck-agent/controller/internal/render"
)

func TestProfileCatalogueFieldsRemainIndependent(t *testing.T) {
	text, err := showProfiles()
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
	encoded, _, err := captureShellLine(t, t.Context(), &shellState{}, "show test profiles | display json")
	if err != nil || !shellEnvelope(t, encoded).OK || !strings.Contains(strings.Join(shellEnvelope(t, encoded).Lines, "\n"), profileCatalogue[0].Purpose) {
		t.Fatal("Shell catalogue envelope changed", err)
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
