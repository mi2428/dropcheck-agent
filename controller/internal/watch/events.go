package watch

import (
	"context"
	"strings"
	"time"
)

// EventKind identifies the type of one watch event.
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

// Event is one structured state transition, finding, or log item emitted by a watch run.
type Event struct {
	Time     time.Time      `json:"time"`
	Kind     EventKind      `json:"kind"`
	Plan     string         `json:"plan,omitempty"`
	Agent    AgentSnapshot  `json:"agent"`
	Round    uint64         `json:"round,omitempty"`
	Target   TargetSnapshot `json:"target"`
	Step     StepSnapshot   `json:"step"`
	Finding  *Finding       `json:"finding,omitempty"`
	Status   string         `json:"status,omitempty"`
	Message  string         `json:"message,omitempty"`
	Duration int64          `json:"duration_ms,omitempty"`
}

// AgentSnapshot captures the stable agent identity and display metadata stored with events.
type AgentSnapshot struct {
	ID          string `json:"id,omitempty"`
	SessionID   string `json:"session_id,omitempty"`
	Name        string `json:"name,omitempty"`
	ADBSerial   string `json:"adb_serial,omitempty"`
	DeviceModel string `json:"device_model,omitempty"`
}

// DisplayName returns the most useful operator-facing label for the agent.
func (snapshot AgentSnapshot) DisplayName() string {
	model := strings.TrimSpace(snapshot.DeviceModel)
	serial := strings.TrimSpace(snapshot.ADBSerial)
	switch {
	case model != "" && serial != "":
		return model + " (" + serial + ")"
	case model != "":
		return model
	case snapshot.Name != "":
		return snapshot.Name
	case serial != "":
		return serial
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

// TargetSnapshot captures the stable target identity stored with events.
type TargetSnapshot struct {
	Name      string `json:"name,omitempty"`
	ShortName string `json:"short_name,omitempty"`
	Agent     string `json:"agent,omitempty"`
	SSID      string `json:"ssid,omitempty"`
	BSSID     string `json:"bssid,omitempty"`
	Band      string `json:"band,omitempty"`
}

// StepSnapshot captures one watch step state stored with events.
type StepSnapshot struct {
	Name      string `json:"name,omitempty"`
	Type      string `json:"type,omitempty"`
	Operation string `json:"operation,omitempty"`
	Status    string `json:"status,omitempty"`
	Message   string `json:"message,omitempty"`
	Error     string `json:"error,omitempty"`
	Skipped   bool   `json:"skipped,omitempty"`
}

// Emitter receives watch events without returning delivery errors.
type Emitter func(Event)

// Sink receives watch events and may report delivery errors.
type Sink interface {
	Emit(context.Context, Event) error
}

func snapshotTarget(target Target) TargetSnapshot {
	return TargetSnapshot{
		Name:      target.DisplayName(),
		ShortName: target.ShortName,
		Agent:     target.Agent,
		SSID:      target.SSID,
		BSSID:     target.BSSID,
		Band:      target.Band,
	}
}
