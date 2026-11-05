package tui

import "github.com/mi2428/dropcheck-agent/controller/internal/watchrun"

func (m *model) apply(event watchrun.Event) {
	m.Apply(event)
	if event.Kind == watchrun.EventControlApplied {
		m.paused = m.State.Phase == "paused"
	}
	if event.Kind == watchrun.EventRoundStarted && !m.checkStatusPinned {
		m.checkStatusOffset = 0
	}
	if event.Kind == watchrun.EventFinding && event.Finding != nil && !m.failedCheckPinned {
		m.failedCheckCursor = m.failedCheckSummaryIndex(event.Agent, event.Target, *event.Finding)
	}
	m.updateRunQueueCursor()
	m.normalizeCursors()
}

func (m *model) recordPassingCheck(passingCheck passingCheckState) {
	m.RecordPassingCheck(passingCheck)
	m.normalizeCursors()
}

func (m *model) pushLog(message string) {
	m.PushLog(message)
}
