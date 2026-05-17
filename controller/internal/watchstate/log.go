package watchstate

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"dropcheck/controller/internal/watch"
)

// PushEventLog stores a sanitized, human-readable log line while preserving the
// structured event for detail-panel scoping.
func (s *State) PushEventLog(event watch.Event) {
	line := strings.TrimSpace(SanitizeLogText(EventLogLine(event)))
	if line == "" {
		return
	}
	when := event.Time
	if when.IsZero() {
		when = time.Now()
	}
	formattedLine := when.Format("15:04:05") + " " + line
	s.PushVisibleLog(formattedLine)
	s.EventLogEntries = append(s.EventLogEntries, EventLogEntry{When: when, Event: event, Line: formattedLine})
	s.TrimEventLogEntries(when)
}

// TrimEventLogEntries bounds structured log history relative to reference.
func (s *State) TrimEventLogEntries(reference time.Time) {
	if reference.IsZero() {
		return
	}
	cutoff := reference.Add(-EventLogRetentionWindow)
	filtered := s.EventLogEntries[:0]
	for _, entry := range s.EventLogEntries {
		if entry.When.IsZero() || !entry.When.Before(cutoff) {
			filtered = append(filtered, entry)
		}
	}
	s.EventLogEntries = filtered
	if len(s.EventLogEntries) > MaxEventLogHistory {
		s.EventLogEntries = s.EventLogEntries[len(s.EventLogEntries)-MaxEventLogHistory:]
	}
}

// EventLogLine renders one watch event as stable key=value text.
func EventLogLine(event watch.Event) string {
	if event.Kind == watch.EventLog && event.Message != "" {
		return SanitizeLogText(event.Message)
	}
	fields := []string{LogField("kind", string(event.Kind))}
	add := func(key string, value string) {
		if field := LogField(key, value); field != "" {
			fields = append(fields, field)
		}
	}
	if event.Round > 0 {
		fields = append(fields, fmt.Sprintf("round=%d", event.Round))
	}
	if event.Status != "" {
		add("status", event.Status)
	}
	if event.Agent.DisplayName() != "" {
		add("agent", event.Agent.DisplayName())
	}
	if event.Target.Name != "" {
		add("target", event.Target.Name)
	}
	if event.Target.SSID != "" {
		add("ssid", event.Target.SSID)
	}
	if event.Target.BSSID != "" {
		add("bssid", event.Target.BSSID)
	}
	if event.Target.Band != "" {
		add("band", event.Target.Band)
	}
	if event.Step.Name != "" {
		add("step", event.Step.Name)
	}
	if event.Step.Type != "" {
		add("type", event.Step.Type)
	}
	if event.Step.Operation != "" {
		add("op", event.Step.Operation)
	}
	if event.Step.Status != "" && event.Step.Status != event.Status {
		add("step_status", event.Step.Status)
	}
	if event.Step.Skipped {
		fields = append(fields, "skipped=true")
	}
	if event.Duration > 0 {
		fields = append(fields, fmt.Sprintf("duration_ms=%d", event.Duration))
	}
	if event.Finding != nil {
		add("check", event.Finding.Check)
		add("metric", event.Finding.Metric)
		add("observed", event.Finding.Observed)
		add("expected", event.Finding.Expected)
		if event.Finding.Message != "" {
			add("finding_msg", event.Finding.Message)
		}
	}
	message := FirstNonEmpty(event.Message, event.Step.Message, event.Step.Error)
	if message != "" {
		add("msg", message)
	}
	return strings.Join(fields, " ")
}

// LogField returns one sanitized key=value field for event logs.
func LogField(key string, value string) string {
	value = strings.TrimSpace(SanitizeLogText(value))
	if value == "" {
		return ""
	}
	if strings.ContainsAny(value, " \t\n\r\"") {
		value = strconv.Quote(value)
	}
	return key + "=" + value
}

// DetailValue returns a sanitized value suitable for dense detail rows.
func DetailValue(value string) string {
	value = strings.TrimSpace(SanitizeLogText(value))
	if strings.ContainsAny(value, " \t\n\"") || strings.HasPrefix(value, "=") {
		return strconv.Quote(value)
	}
	return value
}

// SanitizeLogText escapes control characters while preserving printable text.
func SanitizeLogText(value string) string {
	if value == "" {
		return ""
	}
	var b strings.Builder
	changed := false
	for _, r := range value {
		switch r {
		case '\n':
			b.WriteString(`\n`)
			changed = true
		case '\r':
			b.WriteString(`\r`)
			changed = true
		case '\t':
			b.WriteString(`\t`)
			changed = true
		case '\x1b':
			b.WriteString(`\x1b`)
			changed = true
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&b, "\\x%02x", r)
				changed = true
				continue
			}
			b.WriteRune(r)
		}
	}
	if !changed {
		return value
	}
	return b.String()
}
