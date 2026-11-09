package tui

import (
	"github.com/mi2428/dropcheck-agent/controller/internal/tuirun"
	"github.com/mi2428/dropcheck-agent/controller/internal/tuistate"
)

// History-derived rows are invalidated by Apply; clock-sensitive rows also by ticks.
type frameData struct {
	passing       []passingCheckSummary
	passingReady  bool
	failed        []failedCheckSummary
	failedReady   bool
	hotspots      []failureHotspotSummary
	hotspotsReady bool
	causes        []failureCauseSummary
	causesReady   bool
	outcomes      []outcomeEvent
	outcomesReady bool
	agents        []tuirun.AgentSnapshot
	agentsReady   bool
	targets       []tuirun.TargetSnapshot
	targetsReady  bool
	checks        []string
	checksReady   bool
}

func filterSummaryRows[T any](rows []T, query string, matches func(T, string) bool) []T {
	query = tuistate.NormalizedSearchQuery(query)
	if query == "" {
		return rows
	}
	filtered := make([]T, 0, len(rows))
	for _, row := range rows {
		if matches(row, query) {
			filtered = append(filtered, row)
		}
	}
	return filtered
}
