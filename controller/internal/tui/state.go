package tui

import "dropcheck/controller/internal/watch"

func (m *model) apply(event watch.Event) {
	m.Apply(event)
	if event.Kind == watch.EventRoundStarted && !m.checkStatusPinned {
		m.checkStatusOffset = 0
	}
	if event.Kind == watch.EventFinding && event.Finding != nil && !m.failedCheckPinned {
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
