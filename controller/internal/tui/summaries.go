package tui

import (
	"github.com/mi2428/dropcheck-agent/controller/internal/tuirun"
	"github.com/mi2428/dropcheck-agent/controller/internal/tuistate"
)

func (m model) currentTargetName() string {
	if target, ok := m.currentTarget(); ok {
		return m.targetLabel(target)
	}
	return "-"
}

func (m model) currentTarget() (targetState, bool) {
	return m.CurrentTarget()
}

func currentStepState(target targetState) (stepState, bool) {
	for _, step := range target.Steps {
		if target.CurrentStepID != "" && step.ID == target.CurrentStepID || target.CurrentStepID == "" && step.ID == "" && step.Name == target.CurrentStep {
			return step, true
		}
	}
	return stepState{}, false
}

func (m model) targetFailedCheckCount(agent tuirun.AgentSnapshot, target tuirun.TargetSnapshot) int {
	return m.TargetFailedCheckCount(agent, target)
}

func (m model) filteredPassingCheckSummaries() []passingCheckSummary {
	return m.FilteredPassingCheckSummaries(m.panelFilterQuery(focusPassingChecks))
}

func (m model) filteredFailureHotspots() []failureHotspotSummary {
	return m.FilteredFailureHotspots(m.panelFilterQuery(focusFailureHotspots))
}

func (m model) filteredFailureCauses() []failureCauseSummary {
	return m.FilteredFailureCauses(m.panelFilterQuery(focusFailureHotspots))
}

type failureHotspotSummaryRow struct {
	Index int
	Item  failureHotspotSummary
}

type failureCauseSummaryRow struct {
	Index int
	Item  failureCauseSummary
}

type failureAnalysisRow struct {
	Index   int
	Mode    failureHotspotMode
	Hotspot failureHotspotSummary
	Cause   failureCauseSummary
}

func (m model) filteredFailureHotspotRows() []failureHotspotSummary {
	return m.filteredFailureHotspots()
}

func (m model) filteredFailureCauseRows() []failureCauseSummary {
	return m.filteredFailureCauses()
}

func (m model) filteredFailureHotspotRowsForAgent(agent tuirun.AgentSnapshot) []failureHotspotSummaryRow {
	rows := m.filteredFailureHotspotRows()
	filtered := make([]failureHotspotSummaryRow, 0, len(rows))
	for index, row := range rows {
		if sameAgent(row.Agent, agent) {
			filtered = append(filtered, failureHotspotSummaryRow{Index: index, Item: row})
		}
	}
	return filtered
}

func (m model) filteredFailureCauseRowsForAgent(agent tuirun.AgentSnapshot) []failureCauseSummaryRow {
	rows := m.filteredFailureCauseRows()
	filtered := make([]failureCauseSummaryRow, 0, len(rows))
	for index, row := range rows {
		if sameAgent(row.Agent, agent) {
			filtered = append(filtered, failureCauseSummaryRow{Index: index, Item: row})
		}
	}
	return filtered
}

func (m model) focusedFailureHotspotRows() []failureAnalysisRow {
	return m.focusedFailureAnalysisRowsForMode(m.failureHotspotMode)
}

func (m model) focusedFailureAnalysisRowsForMode(mode failureHotspotMode) []failureAnalysisRow {
	if m.failureHotspotPanelsSplit() {
		key := m.currentHotspotAgentKey()
		for _, agent := range m.failureHotspotAgents() {
			if roundAgentKey(agent) == key {
				return m.failureAnalysisRowsForAgent(mode, agent)
			}
		}
		return nil
	}
	return m.indexedFailureAnalysisRows(mode)
}

func (m model) failureAnalysisRowsForAgent(mode failureHotspotMode, agent tuirun.AgentSnapshot) []failureAnalysisRow {
	if mode == failureHotspotModeTargets {
		rows := m.filteredFailureHotspotRowsForAgent(agent)
		indexed := make([]failureAnalysisRow, 0, len(rows))
		for _, row := range rows {
			indexed = append(indexed, failureAnalysisRow{Index: row.Index, Mode: mode, Hotspot: row.Item})
		}
		return indexed
	}
	rows := m.filteredFailureCauseRowsForAgent(agent)
	indexed := make([]failureAnalysisRow, 0, len(rows))
	for _, row := range rows {
		indexed = append(indexed, failureAnalysisRow{Index: row.Index, Mode: mode, Cause: row.Item})
	}
	return indexed
}

func (m model) indexedFailureAnalysisRows(mode failureHotspotMode) []failureAnalysisRow {
	if mode == failureHotspotModeTargets {
		rows := indexedFailureHotspotRows(m.filteredFailureHotspotRows())
		indexed := make([]failureAnalysisRow, 0, len(rows))
		for _, row := range rows {
			indexed = append(indexed, failureAnalysisRow{Index: row.Index, Mode: mode, Hotspot: row.Item})
		}
		return indexed
	}
	rows := indexedFailureCauseRows(m.filteredFailureCauseRows())
	indexed := make([]failureAnalysisRow, 0, len(rows))
	for _, row := range rows {
		indexed = append(indexed, failureAnalysisRow{Index: row.Index, Mode: mode, Cause: row.Item})
	}
	return indexed
}

func indexedFailureHotspotRows(rows []failureHotspotSummary) []failureHotspotSummaryRow {
	indexed := make([]failureHotspotSummaryRow, 0, len(rows))
	for index, row := range rows {
		indexed = append(indexed, failureHotspotSummaryRow{Index: index, Item: row})
	}
	return indexed
}

func indexedFailureCauseRows(rows []failureCauseSummary) []failureCauseSummaryRow {
	indexed := make([]failureCauseSummaryRow, 0, len(rows))
	for index, row := range rows {
		indexed = append(indexed, failureCauseSummaryRow{Index: index, Item: row})
	}
	return indexed
}

func failureAnalysisRowIdentity(row failureAnalysisRow) string {
	if row.Mode == failureHotspotModeTargets {
		return "target:" + failureHotspotSummaryIdentity(row.Hotspot)
	}
	return "cause:" + failureCauseSummaryIdentity(row.Cause)
}

func failureAnalysisRowAgent(row failureAnalysisRow) tuirun.AgentSnapshot {
	if row.Mode == failureHotspotModeTargets {
		return row.Hotspot.Agent
	}
	return row.Cause.Agent
}

func (m model) failureAnalysisRowByIdentity(mode failureHotspotMode, key string) (failureAnalysisRow, bool) {
	for _, row := range m.indexedFailureAnalysisRows(mode) {
		if failureAnalysisRowIdentity(row) == key {
			return row, true
		}
	}
	return failureAnalysisRow{}, false
}

func failureHotspotCause(finding tuirun.Finding) string {
	return tuistate.FailureHotspotCause(finding)
}

func (m model) filteredFailedCheckSummaries() []failedCheckSummary {
	return m.FilteredFailedCheckSummaries(m.panelFilterQuery(focusFailedChecks))
}

func (m model) failedCheckSummaryIndex(agent tuirun.AgentSnapshot, target tuirun.TargetSnapshot, finding tuirun.Finding) int {
	return m.FailedCheckSummaryIndex(agent, target, finding)
}
