package watch

import (
	"context"
	"time"
)

type EventKind string

const (
	EventWatchStarted   EventKind = "watch_started"
	EventRoundStarted   EventKind = "round_started"
	EventRoundFinished  EventKind = "round_finished"
	EventTargetStarted  EventKind = "target_started"
	EventTargetFinished EventKind = "target_finished"
	EventStepStarted    EventKind = "step_started"
	EventStepFinished   EventKind = "step_finished"
	EventFinding        EventKind = "finding"
	EventLog            EventKind = "log"
)

type Event struct {
	Time     time.Time      `json:"time"`
	Kind     EventKind      `json:"kind"`
	Plan     string         `json:"plan,omitempty"`
	Agent    AgentSnapshot  `json:"agent,omitempty"`
	Round    uint64         `json:"round,omitempty"`
	Target   TargetSnapshot `json:"target,omitempty"`
	Step     StepSnapshot   `json:"step,omitempty"`
	Finding  *Finding       `json:"finding,omitempty"`
	Status   string         `json:"status,omitempty"`
	Message  string         `json:"message,omitempty"`
	Duration int64          `json:"duration_ms,omitempty"`
}

type AgentSnapshot struct {
	ID        string `json:"id,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	Name      string `json:"name,omitempty"`
	ADBSerial string `json:"adb_serial,omitempty"`
}

func (snapshot AgentSnapshot) DisplayName() string {
	switch {
	case snapshot.Name != "":
		return snapshot.Name
	case snapshot.ADBSerial != "":
		return snapshot.ADBSerial
	case snapshot.ID != "":
		if len(snapshot.ID) > 12 {
			return snapshot.ID[:12]
		}
		return snapshot.ID
	case snapshot.SessionID != "":
		if len(snapshot.SessionID) > 12 {
			return snapshot.SessionID[:12]
		}
		return snapshot.SessionID
	default:
		return ""
	}
}

type TargetSnapshot struct {
	Name  string `json:"name,omitempty"`
	SSID  string `json:"ssid,omitempty"`
	BSSID string `json:"bssid,omitempty"`
	Band  string `json:"band,omitempty"`
}

type StepSnapshot struct {
	Name      string `json:"name,omitempty"`
	Type      string `json:"type,omitempty"`
	Operation string `json:"operation,omitempty"`
	Status    string `json:"status,omitempty"`
	Message   string `json:"message,omitempty"`
	Error     string `json:"error,omitempty"`
	Skipped   bool   `json:"skipped,omitempty"`
}

type Emitter func(Event)

type Sink interface {
	Emit(context.Context, Event) error
}

func snapshotTarget(target Target) TargetSnapshot {
	return TargetSnapshot{
		Name:  target.DisplayName(),
		SSID:  target.SSID,
		BSSID: target.BSSID,
		Band:  target.Band,
	}
}
