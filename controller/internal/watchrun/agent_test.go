package watchrun

import (
	"testing"

	"github.com/mi2428/dropcheck-agent/controller/internal/control"
	"github.com/mi2428/dropcheck-agent/controller/internal/controlpb"
)

func TestAgentSnapshotFromInfoUsesDeviceModelAndSerialForDisplay(t *testing.T) {
	snapshot := AgentSnapshotFromInfo(control.AgentInfo{
		ID:        "agent-a",
		SessionID: "session-a",
		Hello: &controlpb.AgentHello{
			AdbSerial: "35251JEHN00258",
			Device:    &controlpb.DeviceInfo{Model: "Pixel 7a"},
		},
	})
	if got, want := snapshot.DisplayName(), "Pixel 7a (35251JEHN00258)"; got != want {
		t.Fatalf("DisplayName() = %q, want %q", got, want)
	}
	if snapshot.Name != "35251JEHN00258" || snapshot.ADBSerial != "35251JEHN00258" || snapshot.DeviceModel != "Pixel 7a" {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}

func TestBindAgentsMatchesSerial(t *testing.T) {
	agents := []control.AgentInfo{
		{ID: "agent-a", Hello: &controlpb.AgentHello{AdbSerial: "35251JEHN00258", Device: &controlpb.DeviceInfo{Model: "Pixel 7a"}}},
		{ID: "agent-b", Hello: &controlpb.AgentHello{AdbSerial: "45240DLAQ007HG", Device: &controlpb.DeviceInfo{Model: "Pixel 9"}}},
	}
	got, err := bindAgents("35251JEHN00258", agents)
	if err != nil || len(got) != 1 || got[0].ID != "agent-a" {
		t.Fatalf("bindAgents(serial) = %v, %v", got, err)
	}
	got, err = bindAgents("45240D", agents)
	if err != nil || len(got) != 1 || got[0].ID != "agent-b" {
		t.Fatalf("bindAgents(serial prefix) = %v, %v", got, err)
	}
	if _, err := bindAgents("Pixel 9", agents); err == nil {
		t.Fatal("device model must not select a target")
	}
}
