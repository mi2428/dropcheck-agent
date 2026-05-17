package tui

import (
	"time"

	"dropcheck/controller/internal/watch"
	"dropcheck/controller/internal/watchstate"
)

func (m *model) pushEventLog(event watch.Event) {
	m.State.PushEventLog(event)
}

func (m *model) trimEventLogEntries(reference time.Time) {
	m.State.TrimEventLogEntries(reference)
}

func eventLogLine(event watch.Event) string {
	return watchstate.EventLogLine(event)
}

func logField(key string, value string) string {
	return watchstate.LogField(key, value)
}

func detailValue(value string) string {
	return watchstate.DetailValue(value)
}

func sanitizeLogText(value string) string {
	return watchstate.SanitizeLogText(value)
}
