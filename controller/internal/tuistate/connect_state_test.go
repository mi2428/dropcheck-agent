package tuistate

import (
	"testing"
	"time"

	"github.com/mi2428/dropcheck-agent/controller/internal/tuirun"
)

func TestRecordConnectStateUpdatesLatestAgentPhase(t *testing.T) {
	state := New(nil, nil, []tuirun.AgentSnapshot{{ID: "agent-a", Name: "pixel-a"}}, time.Time{})
	at := time.Date(2026, 5, 18, 11, 0, 0, 0, time.UTC)
	state.Apply(tuirun.Event{
		Time:    at,
		Kind:    tuirun.EventLog,
		Agent:   tuirun.AgentSnapshot{ID: "agent-a", Name: "pixel-a"},
		Target:  tuirun.TargetSnapshot{Name: "ub1(5G)"},
		Message: `wifi connect state: supplicant=FOUR_WAY_HANDSHAKE ssid="Example Net" bssid=02:00:00:00:00:11 ip=192.0.2.90`,
	})
	state.Apply(tuirun.Event{
		Time:    at.Add(time.Second),
		Kind:    tuirun.EventLog,
		Agent:   tuirun.AgentSnapshot{ID: "agent-a", Name: "pixel-a"},
		Target:  tuirun.TargetSnapshot{Name: "ub1(5G)"},
		Message: `wifi connect state: supplicant=COMPLETED ssid="Example Net" bssid=02:00:00:00:00:11 ip=192.0.2.90`,
	})

	if len(state.ConnectStates) != 1 {
		t.Fatalf("connect states = %d, want 1", len(state.ConnectStates))
	}
	got := state.ConnectStates[0]
	if got.Supplicant != "COMPLETED" || got.SSID != "Example Net" || got.BSSID != "02:00:00:00:00:11" || got.IP != "192.0.2.90" {
		t.Fatalf("connect state = %#v", got)
	}
}
