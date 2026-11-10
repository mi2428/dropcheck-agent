package tuirun

import (
	"context"
	"strings"
	"testing"

	"github.com/mi2428/dropcheck-agent/controller/internal/command"
	"github.com/mi2428/dropcheck-agent/controller/internal/control"
	"github.com/mi2428/dropcheck-agent/controller/internal/controlpb"
	"github.com/mi2428/dropcheck-agent/controller/internal/runner"
)

func TestCleanupAndRotationForgetLiteralSSID(t *testing.T) {
	for _, ssid := range []string{"123", " 123 ", "Lab"} {
		for _, rotation := range []string{"none", "per_target", "per_round"} {
			t.Run(ssid+"/"+rotation, func(t *testing.T) {
				plan, err := Compile(Plan{Networks: []Network{{SSID: ssid, PSK: SecretValue("synthetic-passphrase"), Rotation: rotation, DisconnectAfter: true, ForgetAfter: true}}}, []control.AgentInfo{{ID: "a", Hello: &controlpb.AgentHello{Capabilities: []string{"wifi.forget.typed"}}}})
				if err != nil {
					t.Fatal(err)
				}
				forgot := 0
				report, err := Execute(t.Context(), plan, cleanupRunnerFunc(func(_ context.Context, op command.Operation) (runner.Result, error) {
					if forget := op.Command.GetForgetWifi(); forget != nil {
						forgot++
						if _, ok := forget.GetSelector().(*controlpb.ForgetWifi_Ssid); !ok || forget.GetSsid() != ssid || forget.GetTarget() != "" {
							t.Errorf("forget lost literal SSID %q: %v", ssid, forget)
						}
					}
					return runner.Result{Result: runtimeFixture(op)}, nil
				}), ExecuteOptions{Rounds: 1})
				want := map[string]int{"none": 1, "per_target": 2, "per_round": 3}[rotation]
				if err != nil || !report.Passed() || forgot != want {
					t.Fatalf("forget calls=%d want=%d outcome=%s err=%v", forgot, want, report.Outcome, err)
				}
			})
		}
	}
}

func TestForgetCapabilityIsCheckedBeforeOwnedConnection(t *testing.T) {
	for _, network := range []Network{
		{ForgetAfter: true},
		{Rotation: "per_target"},
		{Rotation: "per_round"},
	} {
		network.SSID, network.PSK = "123", SecretValue("synthetic-passphrase")
		plan := Plan{Networks: []Network{network}}
		if err := Validate(plan); err != nil {
			t.Fatal("device-free validation must not require an agent", err)
		}
		for _, agents := range [][]control.AgentInfo{
			{{ID: "unsupported"}},
			{{ID: "supported", Hello: &controlpb.AgentHello{Capabilities: []string{"wifi.forget.typed"}}}, {ID: "unsupported"}},
		} {
			if _, err := Compile(plan, agents); err == nil || !strings.Contains(err.Error(), "typed Wi-Fi forgetting") {
				t.Fatalf("missing capability must reject the plan before Execute: %v", err)
			}
		}
		network.Agent = "supported-serial"
		if _, err := Compile(Plan{Networks: []Network{network}}, []control.AgentInfo{
			{ID: "supported", Hello: &controlpb.AgentHello{AdbSerial: "supported-serial", Capabilities: []string{"wifi.forget.typed"}}},
			{ID: "unselected"},
		}); err != nil {
			t.Fatal("an unbound agent must not block a supported target", err)
		}
	}
}
