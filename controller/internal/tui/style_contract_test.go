package tui

import (
	"strings"
	"testing"

	"dropcheck/controller/internal/controlpb"
	"dropcheck/controller/internal/harness"
	"github.com/charmbracelet/x/ansi"
)

func TestStyleHeadingRolesAndSharedReportDetail(t *testing.T) {
	for _, title := range []string{"logs", "causes", "failure history", "IPv6 RA"} {
		if got := detailSectionTitle(detailSection{Title: title}); got == "" || strings.HasSuffix(got, ":") {
			t.Fatalf("section heading has scalar syntax: %q", got)
		}
	}
	scope := harness.Scope{AgentID: "acquired-id", TargetID: "ssid-scope"}
	result := &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_CANCELED, Message: "partial acquisition; password=style-secret"}
	report := harness.Report{Outcome: harness.CanceledOutcome, Agents: []harness.AgentProgress{{Agent: harness.AgentSnapshot{ID: scope.AgentID, Name: "acquired-device"}}},
		Steps: []harness.StepReport{{Scope: scope, Attempts: []harness.Attempt{{Scope: scope, Outcome: harness.CanceledOutcome, Result: harness.OperationResult{Raw: result}}}}}}
	for _, width := range []int{8, 24, 40, 80} {
		lines := workflowReportLines(report, 0, true, width)
		text := strings.Join(lines, "\n")
		compact := strings.Join(strings.Fields(text), "")
		if !strings.Contains(compact, "Source:androidTarget:acquired-device") || !strings.Contains(compact, "Status:canceled") || strings.Contains(compact, "style-secret") {
			t.Fatal("detail did not use the safe shared record renderer")
		}
		for _, line := range lines {
			if ansi.StringWidth(line) > width {
				t.Fatalf("detail clipped rather than reflowed at width %d: %q", width, line)
			}
		}
	}
}
