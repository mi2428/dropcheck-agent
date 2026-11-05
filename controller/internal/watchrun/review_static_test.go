package watchrun

import (
	"testing"

	"dropcheck/controller/internal/control"
)

func TestStaticValidationDoesNotBindFakeAgent(t *testing.T) {
	plan := Plan{Networks: []Network{{Name: "lab", SSID: "Lab", Agent: "synthetic-a", WaitConnected: true, RequireIP: true, DisconnectAfter: true}}}
	if err := Validate(plan); err != nil {
		t.Fatalf("static validation tried to bind a live selector: %v", err)
	}
	if _, err := Compile(plan, []control.AgentInfo{{ID: "other"}}); err == nil {
		t.Fatal("execution compilation failed to bind the selected agent")
	}
}
