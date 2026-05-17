package tui

import (
	"time"

	"dropcheck/controller/internal/watch"
)

func (m *model) apply(event watch.Event) {
	m.State.Apply(event)
	if event.Kind == watch.EventFinding && event.Finding != nil {
		m.failedCheckCursor = m.failedCheckSummaryIndex(event.Agent, event.Target, *event.Finding)
	}
	m.updateRunQueueCursor()
	m.normalizeCursors()
}

func (m *model) addFailedCheck(agent watch.AgentSnapshot, target watch.TargetSnapshot, round uint64, when time.Time, finding watch.Finding) {
	m.State.AddFailedCheck(agent, target, round, when, finding)
	m.failedCheckCursor = m.failedCheckSummaryIndex(agent, target, finding)
	m.normalizeCursors()
}

func (m *model) recordPassingCheck(passingCheck passingCheckState) {
	m.State.RecordPassingCheck(passingCheck)
	m.normalizeCursors()
}

func (m *model) pushLog(message string) {
	m.State.PushLog(message)
}

func (m *model) pushVisibleLog(line string) {
	m.State.PushVisibleLog(line)
}
