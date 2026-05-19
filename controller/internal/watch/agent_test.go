package watch

import (
	"testing"

	"dropcheck/controller/internal/control"
	"dropcheck/controller/internal/controlpb"
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

func TestResolveAgentSnapshotMatchesSerial(t *testing.T) {
	agents := []AgentSnapshot{
		{ID: "agent-a", ADBSerial: "35251JEHN00258", DeviceModel: "Pixel 7a"},
		{ID: "agent-b", ADBSerial: "45240DLAQ007HG", DeviceModel: "Pixel 9"},
	}
	got, err := ResolveAgentSnapshot("35251JEHN00258", agents)
	if err != nil {
		t.Fatalf("ResolveAgentSnapshot(serial) error = %v", err)
	}
	if got.ID != "agent-a" {
		t.Fatalf("ResolveAgentSnapshot(serial) = %#v", got)
	}
	got, err = ResolveAgentSnapshot("45240D", agents)
	if err != nil {
		t.Fatalf("ResolveAgentSnapshot(serial prefix) error = %v", err)
	}
	if got.ID != "agent-b" {
		t.Fatalf("ResolveAgentSnapshot(serial prefix) = %#v", got)
	}
	if _, err := ResolveAgentSnapshot("Pixel 9", agents); err == nil {
		t.Fatalf("ResolveAgentSnapshot(device model) should fail")
	}
}
