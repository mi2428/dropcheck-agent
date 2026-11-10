package tui

import (
	"strings"

	"github.com/mi2428/dropcheck-agent/controller/internal/tuirun"
	"github.com/mi2428/dropcheck-agent/controller/internal/tuistate"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func failedCheckKey(finding tuirun.Finding) string { return tuistate.FailedCheckKey(finding) }
func failedCheckSummaryKey(agent tuirun.AgentSnapshot, target tuirun.TargetSnapshot, finding tuirun.Finding) string {
	return tuistate.FailedCheckSummaryKey(agent, target, finding)
}
func failedCheckSummaryIdentity(item failedCheckSummary) string {
	return tuistate.FailedCheckSummaryIdentity(item)
}
func failedCheckSummaryIndexByIdentity(rows []failedCheckSummary, key string) int {
	return tuistate.FailedCheckSummaryIndexByIdentity(rows, key)
}
func failureHotspotSummaryIdentity(item failureHotspotSummary) string {
	return tuistate.FailureHotspotSummaryIdentity(item)
}
func failureCauseSummaryIdentity(item failureCauseSummary) string {
	return tuistate.FailureCauseSummaryIdentity(item)
}
func failureCauseIdentity(agent tuirun.AgentSnapshot, finding tuirun.Finding) string {
	return tuistate.FailureCauseIdentity(agent, tuistate.FailureHotspotCause(finding))
}
func failureHotspotIdentity(agent tuirun.AgentSnapshot, target tuirun.TargetSnapshot, findingTargets ...string) string {
	return tuistate.FailureHotspotIdentity(agent, target, findingTargets...)
}
func agentKey(agent tuirun.AgentSnapshot) string   { return tuistate.AgentKey(agent) }
func agentLabel(agent tuirun.AgentSnapshot) string { return tuistate.AgentLabel(agent) }
func sameAgent(a tuirun.AgentSnapshot, b tuirun.AgentSnapshot) bool {
	return tuistate.SameAgent(a, b)
}

func (m model) targetLabel(target targetState) string {
	return m.runQueueTargetLabel(target, m.MultiAgent)
}

func (m model) runQueueTargetLabel(target targetState, includeAgentLabel bool) string {
	label := target.Target.Name
	if !includeAgentLabel {
		return label
	}
	return agentLabel(target.Agent) + " " + label
}

func passingCheckSummaryIdentity(item passingCheckSummary) string {
	return tuistate.PassingCheckSummaryIdentity(item)
}
func passingCheckSummaryIndexByIdentity(rows []passingCheckSummary, key string) int {
	return tuistate.PassingCheckSummaryIndexByIdentity(rows, key)
}
func durationLabel(duration int64) string { return tuistate.DurationLabel(duration) }

func knownDurationLabel(duration int64, known bool) string {
	if known && duration == 0 {
		return "0ms"
	}
	return durationLabel(duration)
}

func displayCheckName(name string) string { return tuistate.DisplayCheckName(name) }

func agentDeviceLabel(agent tuirun.AgentSnapshot) string {
	if agent.DeviceModel != "" {
		return agent.DeviceModel
	}
	for _, value := range []string{agent.Name, agent.ID, agent.SessionID} {
		if value != "" && value != agent.ADBSerial {
			return value
		}
	}
	return "-"
}

func fit(value string, width int) string {
	runes := []rune(value)
	if width <= 0 || len(runes) <= width {
		return value
	}
	if width == 1 {
		return "~"
	}
	return string(runes[:width-1]) + "~"
}

func fitText(value string, width int) string { return fitANSI(sanitizeLogText(value), width) }

func fitANSI(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(value) <= width {
		return value
	}
	return ansi.Truncate(value, width, "~")
}

func padToWidth(value string, width int) string {
	if width <= 0 {
		return ""
	}
	value = fit(value, width)
	return value + strings.Repeat(" ", intMax(0, width-runeLen(value)))
}

func runeLen(value string) int                { return len([]rune(value)) }
func (m model) targetCount(status string) int { return m.TargetCount(status) }

func runQueueRowStyle(status string) lipgloss.Style {
	switch normalizeStatus(status) {
	case "failed":
		return failedStatusStyle
	case "running":
		return runningStatusStyle
	case "ok":
		return okStatusStyle
	case "skipped":
		return skippedStatusStyle
	default:
		return waitStatusStyle
	}
}

func firstNonEmpty(values ...string) string { return tuistate.FirstNonEmpty(values...) }
func intMax(a, b int) int                   { return tuistate.Max(a, b) }
func intMin(a, b int) int                   { return tuistate.Min(a, b) }
func clamp(value, low, high int) int        { return tuistate.Clamp(value, low, high) }
func correctedOffset(selected int, visibleRows int, totalRows int) int {
	if totalRows <= 0 {
		return 0
	}
	visibleRows = intMax(1, visibleRows)
	selected = clamp(selected, 0, totalRows-1)
	if totalRows <= visibleRows {
		return 0
	}
	start := selected - visibleRows/2
	return clamp(start, 0, totalRows-visibleRows)
}
func stableOffset(selected int, currentOffset int, visibleRows int, totalRows int) int {
	if totalRows <= 0 {
		return 0
	}
	visibleRows = intMax(1, visibleRows)
	selected = clamp(selected, 0, totalRows-1)
	currentOffset = clamp(currentOffset, 0, intMax(0, totalRows-visibleRows))
	if totalRows <= visibleRows {
		return 0
	}
	if selected < currentOffset {
		return selected
	}
	if selected >= currentOffset+visibleRows {
		return selected - visibleRows + 1
	}
	return currentOffset
}
