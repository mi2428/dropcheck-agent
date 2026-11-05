package adbdiag

import (
	"strings"
	"testing"

	"github.com/mi2428/dropcheck-agent/controller/internal/render"
)

// Only synthetic rendering is exercised; no ADB collection entry point is called.
func TestLayoutContractSyntheticMLOTable(t *testing.T) {
	for _, snapshot := range []string{"", "observed: literal"} {
		t.Run(snapshot, func(t *testing.T) {
			out := render.LayoutText(MLOSummaryText(MLOSummary{MLOMode: snapshot, LinkStats: []MLOLinkStat{
				{LinkID: "1", RadioID: "literal:radio", RSSIMgmt: "-40", BeaconRx: "0"},
				{LinkID: "2", RadioID: "other", RSSIMgmt: "-50", BeaconRx: "false"},
			}}, render.Presentation{}), render.Presentation{})
			lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
			for i, line := range lines {
				if fields := strings.Fields(line); len(fields) == 0 || fields[0] != "link_id" {
					continue
				}
				before, after := 0, 0
				for j := i - 1; j >= 0 && lines[j] == ""; j-- {
					before++
				}
				for j := i + 3; j < len(lines) && lines[j] == ""; j++ {
					after++
				}
				if before != 1 || after != 1 {
					t.Errorf("blank lines above header/below final row = %d/%d, want 1/1", before, after)
				}
				if i+2 >= len(lines) || strings.Join(strings.Fields(lines[i+1]), " ") != "1 literal:radio -40 0" || strings.Join(strings.Fields(lines[i+2]), " ") != "2 other -50 false" {
					t.Errorf("synthetic rows changed: %q", out)
				}
				return
			}
			t.Fatal("missing synthetic MLO table header")
		})
	}
}
