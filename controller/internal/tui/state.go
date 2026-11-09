package tui

import "github.com/mi2428/dropcheck-agent/controller/internal/tuirun"

func (m *model) apply(event tuirun.Event) {
	m.Apply(event)
	if event.Kind == tuirun.EventControlApplied {
		m.paused = m.State.Phase == "paused"
	}
	if event.Kind == tuirun.EventRoundStarted && !m.checkStatusPinned {
		m.checkStatusOffset = 0
	}
	if event.Kind == tuirun.EventFinding && event.Finding != nil && !m.failedCheckPinned {
		m.failedCheckCursor = m.failedCheckSummaryIndex(event.Agent, event.Target, *event.Finding)
	}
	m.updateRunQueueCursor()
	m.normalizeCursors()
}

func (m *model) pushLog(message string) {
	m.PushLog(message)
}
