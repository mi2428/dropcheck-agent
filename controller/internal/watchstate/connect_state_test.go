package watchstate

import (
	"testing"
	"time"

	"dropcheck/controller/internal/watch"
)

func TestRecordConnectStateUpdatesLatestAgentPhase(t *testing.T) {
	state := New(nil, nil, []watch.AgentSnapshot{{ID: "agent-a", Name: "pixel-a"}}, time.Time{})
	at := time.Date(2026, 5, 18, 11, 0, 0, 0, time.UTC)
	state.Apply(watch.Event{
		Time:    at,
		Kind:    watch.EventLog,
		Agent:   watch.AgentSnapshot{ID: "agent-a", Name: "pixel-a"},
		Target:  watch.TargetSnapshot{Name: "ub1(5G)"},
		Message: `wifi connect state: supplicant=FOUR_WAY_HANDSHAKE ssid="SHIZK RADIO" bssid=22:0b:8b:b6:2c:e1 ip=192.168.22.90`,
	})
	state.Apply(watch.Event{
		Time:    at.Add(time.Second),
		Kind:    watch.EventLog,
		Agent:   watch.AgentSnapshot{ID: "agent-a", Name: "pixel-a"},
		Target:  watch.TargetSnapshot{Name: "ub1(5G)"},
		Message: `wifi connect state: supplicant=COMPLETED ssid="SHIZK RADIO" bssid=22:0b:8b:b6:2c:e1 ip=192.168.22.90`,
	})

	if len(state.ConnectStates) != 1 {
		t.Fatalf("connect states = %d, want 1", len(state.ConnectStates))
	}
	got := state.ConnectStates[0]
	if got.Supplicant != "COMPLETED" || got.SSID != "SHIZK RADIO" || got.BSSID != "22:0b:8b:b6:2c:e1" || got.IP != "192.168.22.90" {
		t.Fatalf("connect state = %#v", got)
	}
}
