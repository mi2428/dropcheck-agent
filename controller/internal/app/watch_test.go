package app

import (
	"strings"
	"testing"
	"time"

	"dropcheck/controller/internal/control"
	"dropcheck/controller/internal/controlpb"
	"dropcheck/controller/internal/watch"
)

func TestWatchSessionLogEventFormatsWarnForTUI(t *testing.T) {
	at := time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC)
	event, ok := watchSessionLogEvent(control.LogEvent{
		AgentID:   "agent-a",
		SessionID: "session-a",
		CommandID: "cmd-1",
		Level:     controlpb.CommandLog_LEVEL_WARN,
		Message:   "traceroute binary not available",
		Time:      at,
	})
	if !ok {
		t.Fatal("watchSessionLogEvent filtered warn log")
	}
	if event.Kind != watch.EventLog || !event.Time.Equal(at) {
		t.Fatalf("event metadata = %#v", event)
	}
	if event.Agent.ID != "agent-a" || event.Agent.SessionID != "session-a" {
		t.Fatalf("agent snapshot = %#v", event.Agent)
	}
	if want := "[warn agent=agent-a command=cmd-1] traceroute binary not available"; !strings.Contains(event.Message, want) {
		t.Fatalf("message = %q, want %q", event.Message, want)
	}
}

func TestWatchSessionLogEventFiltersDebugForTUI(t *testing.T) {
	_, ok := watchSessionLogEvent(control.LogEvent{
		AgentID: "agent-a",
		Level:   controlpb.CommandLog_LEVEL_DEBUG,
		Message: "debug detail",
	})
	if ok {
		t.Fatal("debug log should be filtered")
	}
}
