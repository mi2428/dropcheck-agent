package watchrun

import (
	"strings"

	"github.com/mi2428/dropcheck-agent/controller/internal/control"
)

// AgentSnapshotFromInfo captures the stable identity and display metadata for an agent.
func AgentSnapshotFromInfo(agent control.AgentInfo) AgentSnapshot {
	serial := ""
	model := ""
	if agent.Hello != nil {
		serial = strings.TrimSpace(agent.Hello.GetAdbSerial())
		model = strings.TrimSpace(agent.Hello.GetDevice().GetModel())
	}
	name := firstNonEmpty(serial, agent.ID, agent.SessionID)
	if serial == "" && len(name) > 12 {
		name = name[:12]
	}
	return AgentSnapshot{
		ID:          agentKey(agent),
		SessionID:   agent.SessionID,
		Name:        name,
		ADBSerial:   serial,
		DeviceModel: model,
	}
}

// AgentSnapshotMatches reports whether selector exactly names the ADB serial.
func AgentSnapshotMatches(agent AgentSnapshot, selector string) bool {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return false
	}
	return strings.TrimSpace(agent.ADBSerial) == selector
}
