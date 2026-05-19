package tui

import (
	"dropcheck/controller/internal/watch"
	"dropcheck/controller/internal/watchstate"
)

func (m model) currentTargetName() string {
	if target, ok := m.currentTarget(); ok {
		return m.targetLabel(target)
	}
	return "-"
}

func (m model) currentTarget() (targetState, bool) {
	return m.State.CurrentTarget()
}

func currentStepState(target targetState) (stepState, bool) {
	for _, step := range target.Steps {
		if step.Name == target.CurrentStep {
			return step, true
		}
	}
	return stepState{}, false
}

func (m model) targetFailedCheckCount(agent watch.AgentSnapshot, target string) int {
	return m.State.TargetFailedCheckCount(agent, target)
}

func (m model) passingCheckSummaries() []passingCheckSummary {
	return m.State.PassingCheckSummaries()
}

func (m model) filteredPassingCheckSummaries() []passingCheckSummary {
	return m.State.FilteredPassingCheckSummaries(m.searchQuery)
}

func (m model) failedCheckSummaries() []failedCheckSummary {
	return m.State.FailedCheckSummaries()
}

func (m model) failureHotspots() []failureHotspotSummary {
	return m.State.FailureHotspots()
}

func (m model) filteredFailureHotspots() []failureHotspotSummary {
	return m.State.FilteredFailureHotspots(m.searchQuery)
}

type failureHotspotSummaryRow struct {
	Index int
	Item  failureHotspotSummary
}

func (m model) filteredFailureHotspotRows() []failureHotspotSummary {
	return m.filteredFailureHotspots()
}

func (m model) filteredFailureHotspotRowsForAgent(agent watch.AgentSnapshot) []failureHotspotSummaryRow {
	rows := m.filteredFailureHotspotRows()
	filtered := make([]failureHotspotSummaryRow, 0, len(rows))
	for index, row := range rows {
		if sameAgent(row.Agent, agent) {
			filtered = append(filtered, failureHotspotSummaryRow{Index: index, Item: row})
		}
	}
	return filtered
}

func (m model) focusedFailureHotspotRows() []failureHotspotSummaryRow {
	if m.failureHotspotPanelsSplit() {
		key := m.currentHotspotAgentKey()
		for _, agent := range m.failureHotspotAgents() {
			if roundAgentKey(agent) == key {
				return m.filteredFailureHotspotRowsForAgent(agent)
			}
		}
		return nil
	}
	return indexedFailureHotspotRows(m.filteredFailureHotspotRows())
}

func indexedFailureHotspotRows(rows []failureHotspotSummary) []failureHotspotSummaryRow {
	indexed := make([]failureHotspotSummaryRow, 0, len(rows))
	for index, row := range rows {
		indexed = append(indexed, failureHotspotSummaryRow{Index: index, Item: row})
	}
	return indexed
}

func failureHotspotCause(finding watch.Finding) string {
	return watchstate.FailureHotspotCause(finding)
}

func (m model) filteredFailedCheckSummaries() []failedCheckSummary {
	return m.State.FilteredFailedCheckSummaries(m.searchQuery)
}

func (m model) failedCheckSummaryIndex(agent watch.AgentSnapshot, target watch.TargetSnapshot, finding watch.Finding) int {
	return m.State.FailedCheckSummaryIndex(agent, target, finding)
}
