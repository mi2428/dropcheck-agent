package watchstate

import (
	"fmt"
	"strings"
	"time"

	"dropcheck/controller/internal/watch"
)

// FailedCheckKey returns the stable identity for one finding.
func FailedCheckKey(finding watch.Finding) string {
	return strings.Join([]string{finding.Target, finding.Check, finding.Metric, finding.Expected, finding.Message}, "\x00")
}

// FailedCheckSummaryKey returns the summary key for an agent/target/finding
// tuple. Failed summaries include the agent so device-labelled TUI rows never
// merge results from different phones into one ambiguous row.
func FailedCheckSummaryKey(agent watch.AgentSnapshot, target watch.TargetSnapshot, finding watch.Finding) string {
	return FailedCheckStateKey(agent, target, finding)
}

// FailedCheckSummaryIdentity returns the stable identity for a failed summary.
func FailedCheckSummaryIdentity(item FailedCheckSummary) string {
	return FailedCheckSummaryKey(item.Agent, item.Target, item.Finding)
}

// FailedCheckSummaryIndexByIdentity finds key in rows or returns -1.
func FailedCheckSummaryIndexByIdentity(rows []FailedCheckSummary, key string) int {
	for i, row := range rows {
		if FailedCheckSummaryIdentity(row) == key {
			return i
		}
	}
	return -1
}

// FailedCheckStateKey returns the full per-agent failed-check identity.
func FailedCheckStateKey(agent watch.AgentSnapshot, target watch.TargetSnapshot, finding watch.Finding) string {
	targetName := FirstNonEmpty(target.Name, target.SSID, target.BSSID, finding.Target)
	return strings.Join([]string{AgentKey(agent), targetName, target.SSID, target.BSSID, FailedCheckKey(finding)}, "\x00")
}

// FailureHotspotSummaryIdentity returns the stable identity for a hotspot row.
func FailureHotspotSummaryIdentity(item FailureHotspotSummary) string {
	return FailureHotspotIdentity(item.Agent, item.Target, item.LatestFinding.Target)
}

// FailureHotspotSummaryIndexByIdentity finds key in hotspot rows or returns -1.
func FailureHotspotSummaryIndexByIdentity(rows []FailureHotspotSummary, key string) int {
	for i, row := range rows {
		if FailureHotspotSummaryIdentity(row) == key {
			return i
		}
	}
	return -1
}

// FailureHotspotIdentity returns the per-agent target identity used for hotspot
// aggregation.
func FailureHotspotIdentity(agent watch.AgentSnapshot, target watch.TargetSnapshot, findingTargets ...string) string {
	targetKey := FirstNonEmpty(CheckStatusTargetKey(target), FirstNonEmpty(findingTargets...), "target")
	return strings.Join([]string{RoundAgentKey(agent), targetKey}, "\x00")
}

// RoundAgentKey returns a non-empty key for aggregations that must still work
// when watch data comes from a single unnamed local agent.
func RoundAgentKey(agent watch.AgentSnapshot) string {
	if key := AgentKey(agent); key != "" {
		return key
	}
	return "all"
}

// TargetStateKey returns the map key for one configured or live target state.
func TargetStateKey(agent watch.AgentSnapshot, target watch.TargetSnapshot) string {
	targetName := FirstNonEmpty(target.Name, target.SSID, target.BSSID)
	return strings.Join([]string{AgentKey(agent), targetName, target.SSID, target.BSSID, target.Band}, "\x00")
}

// AgentKey returns the most stable available agent identity.
func AgentKey(agent watch.AgentSnapshot) string {
	return FirstNonEmpty(agent.ID, agent.ADBSerial, agent.SessionID, agent.Name)
}

// AgentLabel returns the operator-facing label for an agent snapshot.
func AgentLabel(agent watch.AgentSnapshot) string {
	return FirstNonEmpty(agent.DisplayName(), "-")
}

// SameAgent reports whether two snapshots identify the same agent.
func SameAgent(a watch.AgentSnapshot, b watch.AgentSnapshot) bool {
	return AgentKey(a) == AgentKey(b)
}

// DisplayCheckName returns an operator-facing label for internal watch steps.
func DisplayCheckName(name string) string {
	name = strings.TrimSpace(name)
	switch strings.ToLower(name) {
	case "connect":
		return "Connect"
	case "wait_connected":
		return "Wait Connected"
	case "disconnect":
		return "Disconnect"
	case "forget":
		return "Forget"
	case "target":
		return "Target"
	default:
		return name
	}
}

// CheckStatusTargetKey is the target identity used for cross-round check
// aggregation. It deliberately omits band so late events with partial target
// snapshots still land on the configured target row.
func CheckStatusTargetKey(target watch.TargetSnapshot) string {
	return FirstNonEmpty(target.Name, target.SSID, target.BSSID)
}

// CheckStatusTargetLabel returns the most useful operator-facing label for a
// target snapshot.
func CheckStatusTargetLabel(target watch.TargetSnapshot) string {
	return FirstNonEmpty(target.Name, target.SSID, target.BSSID, "-")
}

// CheckStatusTargetShortLabel returns the configured dense label for a target.
func CheckStatusTargetShortLabel(target watch.TargetSnapshot) string {
	return FirstNonEmpty(target.ShortName, target.Name, target.SSID, target.BSSID, "-")
}

// EventTargetLabel returns the target label used in event-log summaries.
func (s State) EventTargetLabel(event watch.Event) string {
	label := event.Target.Name
	if !s.MultiAgent {
		return label
	}
	return AgentLabel(event.Agent) + " " + label
}

// PassingCheckEvent reports whether event should be counted as a passing check.
func PassingCheckEvent(event watch.Event) bool {
	status := FirstNonEmpty(event.Step.Status, event.Status)
	if status != "ok" || event.Step.Skipped {
		return false
	}
	if event.Step.Type == "cleanup" {
		return false
	}
	return FirstNonEmpty(event.Step.Name, event.Step.Type) != ""
}

// RequiredStepFailedCheck converts failed required connection steps into
// findings.
func RequiredStepFailedCheck(event watch.Event) (watch.Finding, bool) {
	status := FirstNonEmpty(event.Step.Status, event.Status)
	if status != "failed" {
		return watch.Finding{}, false
	}
	stepType := FirstNonEmpty(event.Step.Type, event.Step.Name)
	if stepType != "connect" && stepType != "wait_connected" {
		return watch.Finding{}, false
	}
	return watch.Finding{
		Target:   FirstNonEmpty(event.Target.Name, event.Target.SSID, event.Target.BSSID),
		Check:    FirstNonEmpty(event.Step.Name, stepType),
		Metric:   "status",
		Observed: status,
		Expected: "== ok",
		Message:  FirstNonEmpty(event.Step.Message, event.Step.Error, event.Message, "step failed"),
	}, true
}

// PassingCheckKey returns the full per-agent passing-check identity.
func PassingCheckKey(agent watch.AgentSnapshot, target watch.TargetSnapshot, step watch.StepSnapshot) string {
	targetName := FirstNonEmpty(target.Name, target.SSID, target.BSSID)
	stepName := FirstNonEmpty(step.Name, step.Type)
	if targetName == "" || stepName == "" {
		return ""
	}
	return strings.Join([]string{AgentKey(agent), targetName, target.SSID, target.BSSID, stepName, step.Type}, "\x00")
}

// PassingCheckSummaryKey returns the summary identity for an agent, target, and
// step. Passing summaries include the agent so device-labelled TUI rows never
// merge results from different phones into one ambiguous row.
func PassingCheckSummaryKey(agent watch.AgentSnapshot, target watch.TargetSnapshot, step watch.StepSnapshot) string {
	return PassingCheckKey(agent, target, step)
}

// PassingCheckSummaryIdentity returns the stable identity for a passing summary.
func PassingCheckSummaryIdentity(item PassingCheckSummary) string {
	return PassingCheckSummaryKey(item.Agent, item.Target, item.Step)
}

// PassingCheckSummaryIndexByIdentity finds key in rows or returns -1.
func PassingCheckSummaryIndexByIdentity(rows []PassingCheckSummary, key string) int {
	for i, row := range rows {
		if PassingCheckSummaryIdentity(row) == key {
			return i
		}
	}
	return -1
}

// EventTime returns event.Time or wall-clock time when the event is undated.
func EventTime(event watch.Event) time.Time {
	if !event.Time.IsZero() {
		return event.Time
	}
	return time.Now()
}

// CurrentTime returns the state's pinned clock when tests or callers provide
// one, otherwise it falls back to wall-clock time for live rendering.
func (s State) CurrentTime() time.Time {
	if !s.Now.IsZero() {
		return s.Now
	}
	return time.Now()
}

// DurationLabel formats a millisecond duration for compact tables.
func DurationLabel(duration int64) string {
	if duration <= 0 {
		return "-"
	}
	if duration < 1000 {
		return fmt.Sprintf("%dms", duration)
	}
	return fmt.Sprintf("%.1fs", float64(duration)/1000)
}

// NormalizeStatus maps watch status spellings into the compact vocabulary used
// by summaries and terminal palettes.
func NormalizeStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "ok", "pass", "passed":
		return "ok"
	case "running", "run":
		return "running"
	case "failed", "fail", "failure":
		return "failed"
	case "skipped", "skip":
		return "skipped"
	default:
		return "pending"
	}
}

func (item PassingCheckSummary) avgDuration() int64 {
	if item.DurationCount <= 0 {
		return 0
	}
	return (item.DurationTotal + int64(item.DurationCount/2)) / int64(item.DurationCount)
}

// FirstNonEmpty returns the first non-blank trimmed value.
func FirstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// Max returns the larger integer.
func Max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// MaxInt64 returns the larger int64.
func MaxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// Min returns the smaller integer.
func Min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Clamp constrains value to the inclusive range low..high.
func Clamp(value, low, high int) int {
	if high < low {
		return low
	}
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

func correctedOffset(selected int, visibleRows int, totalRows int) int {
	if totalRows <= 0 {
		return 0
	}
	visibleRows = Max(1, visibleRows)
	selected = Clamp(selected, 0, totalRows-1)
	if totalRows <= visibleRows {
		return 0
	}
	start := selected - visibleRows/2
	return Clamp(start, 0, totalRows-visibleRows)
}

func stableOffset(selected int, currentOffset int, visibleRows int, totalRows int) int {
	if totalRows <= 0 {
		return 0
	}
	visibleRows = Max(1, visibleRows)
	selected = Clamp(selected, 0, totalRows-1)
	currentOffset = Clamp(currentOffset, 0, Max(0, totalRows-visibleRows))
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
