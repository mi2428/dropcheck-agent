package app

import (
	"testing"

	"dropcheck/controller/internal/control"
	"dropcheck/controller/internal/controlpb"
)

func TestPrepareCommandForAgentInjectsControllerLinkIdentity(t *testing.T) {
	cmd := &controlpb.RunCommand{
		Command: &controlpb.RunCommand_SetControllerLinkConfig{
			SetControllerLinkConfig: &controlpb.SetControllerLinkConfig{
				Config: &controlpb.ControllerLinkConfig{Enabled: true, Host: "192.168.7.1", Port: 37588},
			},
		},
	}
	state := &shellState{controllerToken: "controller-token"}
	agent := control.AgentInfo{
		ID: "agent-1",
		Hello: &controlpb.AgentHello{
			AdbSerial: "R5CT12345",
		},
	}

	prepareCommandForAgent(state, agent, cmd)

	config := cmd.GetSetControllerLinkConfig().GetConfig()
	if config.GetToken() != "controller-token" || config.GetAgentId() != "agent-1" || config.GetAdbSerial() != "R5CT12345" {
		t.Fatalf("controller link config = %#v", config)
	}
}

func TestPrepareCommandForAgentLeavesDisabledControllerLinkSecretless(t *testing.T) {
	cmd := &controlpb.RunCommand{
		Command: &controlpb.RunCommand_SetControllerLinkConfig{
			SetControllerLinkConfig: &controlpb.SetControllerLinkConfig{
				Config: &controlpb.ControllerLinkConfig{Enabled: false},
			},
		},
	}

	prepareCommandForAgent(&shellState{controllerToken: "controller-token"}, control.AgentInfo{
		ID:    "agent-1",
		Hello: &controlpb.AgentHello{AdbSerial: "R5CT12345"},
	}, cmd)

	config := cmd.GetSetControllerLinkConfig().GetConfig()
	if config.GetToken() != "" || config.GetAgentId() != "" || config.GetAdbSerial() != "" {
		t.Fatalf("disabled controller link config should not carry identity = %#v", config)
	}
}
