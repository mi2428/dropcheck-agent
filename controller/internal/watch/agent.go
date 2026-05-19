package watch

import (
	"fmt"
	"strings"

	"dropcheck/controller/internal/control"
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
		ID:          agent.ID,
		SessionID:   agent.SessionID,
		Name:        name,
		ADBSerial:   serial,
		DeviceModel: model,
	}
}

// ResolveAgentSnapshot resolves a config agent selector against connected
// agents by ADB serial. A unique serial prefix is accepted for local
// convenience, but device model names are display-only and are not part of the
// config contract.
func ResolveAgentSnapshot(selector string, agents []AgentSnapshot) (AgentSnapshot, error) {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return AgentSnapshot{}, fmt.Errorf("agent serial is empty")
	}
	if len(agents) == 0 {
		return AgentSnapshot{}, fmt.Errorf("agent serial %q is not connected", selector)
	}
	var exact []AgentSnapshot
	for _, agent := range agents {
		if AgentSnapshotMatches(agent, selector) {
			exact = append(exact, agent)
		}
	}
	switch len(exact) {
	case 1:
		return exact[0], nil
	case 0:
	default:
		return AgentSnapshot{}, fmt.Errorf("agent serial %q is ambiguous", selector)
	}
	var prefix []AgentSnapshot
	for _, agent := range agents {
		if AgentSnapshotPrefixMatches(agent, selector) {
			prefix = append(prefix, agent)
		}
	}
	switch len(prefix) {
	case 0:
		return AgentSnapshot{}, fmt.Errorf("agent serial %q is not connected", selector)
	case 1:
		return prefix[0], nil
	default:
		return AgentSnapshot{}, fmt.Errorf("agent serial %q is ambiguous", selector)
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

// AgentSnapshotPrefixMatches reports whether selector prefixes the ADB serial.
func AgentSnapshotPrefixMatches(agent AgentSnapshot, selector string) bool {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return false
	}
	serial := strings.TrimSpace(agent.ADBSerial)
	return serial != "" && strings.HasPrefix(serial, selector)
}
