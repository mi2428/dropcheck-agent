package watchstate

import (
	"sort"
	"strings"
	"time"

	"dropcheck/controller/internal/watch"
)

// OutcomeEvent is the compact historical outcome used by check-status and
// timeline views.
type OutcomeEvent struct {
	When   time.Time
	Round  uint64
	Agent  watch.AgentSnapshot
	Target watch.TargetSnapshot
	Status string
}

// OutcomeBucket groups outcomes that land in the same rendered graph column.
type OutcomeBucket struct {
	OK     int
	Failed int
}

// CheckStatusAggregate is the per-check, per-target cell state before terminal
// styling is applied.
type CheckStatusAggregate struct {
	Status string
	Count  int
	Failed int
	Total  int
	Stale  bool
}

// CheckStatusAgentResult is the current or historical status for one
// agent/target/check tuple.
type CheckStatusAgentResult struct {
	Status string
	Stale  bool
}

// OutcomeEvents returns sorted pass/fail events from the retained histories.
func (s State) OutcomeEvents() []OutcomeEvent {
	events := make([]OutcomeEvent, 0, len(s.PassingChecks)+len(s.FailedChecks))
	for _, passingCheck := range s.PassingChecks {
		events = append(events, OutcomeEvent{When: passingCheck.When, Round: passingCheck.Round, Agent: passingCheck.Agent, Target: passingCheck.Target, Status: "ok"})
	}
	for _, failedCheck := range s.FailedChecks {
		events = append(events, OutcomeEvent{When: failedCheck.When, Round: failedCheck.Round, Agent: failedCheck.Agent, Target: failedCheck.Target, Status: "failed"})
	}
	sort.SliceStable(events, func(i, j int) bool {
		return events[i].When.Before(events[j].When)
	})
	return events
}

// OutcomeAgents returns the stable agent list used for multi-agent aggregates.
func (s State) OutcomeAgents(events []OutcomeEvent) []watch.AgentSnapshot {
	agents := make([]watch.AgentSnapshot, 0, Max(1, len(s.Agents)))
	seen := make(map[string]bool)
	add := func(agent watch.AgentSnapshot) {
		key := AgentKey(agent)
		if key == "" && len(agents) > 0 {
			return
		}
		if seen[key] {
			return
		}
		seen[key] = true
		agents = append(agents, agent)
	}
	if len(s.Agents) > 0 {
		for _, agent := range s.Agents {
			add(agent)
		}
	}
	for _, target := range s.Targets {
		add(target.Agent)
	}
	for _, event := range events {
		add(event.Agent)
	}
	if len(agents) == 0 {
		agents = append(agents, watch.AgentSnapshot{})
	}
	return agents
}

// CheckStatusTargets returns deduplicated targets in configured/history order.
func (s State) CheckStatusTargets() []watch.TargetSnapshot {
	targets := make([]watch.TargetSnapshot, 0, len(s.Targets))
	seen := make(map[string]bool)
	add := func(target watch.TargetSnapshot) {
		key := CheckStatusTargetKey(target)
		if key == "" || seen[key] {
			return
		}
		seen[key] = true
		targets = append(targets, target)
	}
	for _, target := range s.Targets {
		add(target.Target)
	}
	for _, passingCheck := range s.PassingChecks {
		add(passingCheck.Target)
	}
	for _, failedCheck := range s.FailedChecks {
		add(failedCheck.Target)
	}
	return targets
}

// CheckStatusChecks returns the checks that should be shown in check-status
// rows, preserving the planned step order before historical additions.
func (s State) CheckStatusChecks() []string {
	checks := make([]string, 0, 8)
	seen := make(map[string]bool)
	add := func(check string) {
		check = strings.TrimSpace(check)
		if check == "" || check == "target" || seen[check] {
			return
		}
		seen[check] = true
		checks = append(checks, check)
	}
	for _, target := range s.Targets {
		for _, step := range target.PlannedSteps {
			add(FirstNonEmpty(step.Name, step.Type))
		}
		for _, step := range target.Steps {
			add(FirstNonEmpty(step.Name, step.Type))
		}
	}
	for _, passingCheck := range s.PassingChecks {
		add(FirstNonEmpty(passingCheck.Step.Name, passingCheck.Step.Type))
	}
	for _, failedCheck := range s.FailedChecks {
		add(failedCheck.Finding.Check)
	}
	return checks
}

// FilterOutcomeEvents returns outcomes scoped to one agent and target.
func FilterOutcomeEvents(events []OutcomeEvent, agent watch.AgentSnapshot, target watch.TargetSnapshot) []OutcomeEvent {
	filtered := make([]OutcomeEvent, 0, len(events))
	targetKey := CheckStatusTargetKey(target)
	for _, event := range events {
		if AgentKey(agent) != "" && !SameAgent(event.Agent, agent) {
			continue
		}
		if targetKey != "" && CheckStatusTargetKey(event.Target) != targetKey {
			continue
		}
		filtered = append(filtered, event)
	}
	return filtered
}

// OutcomeCounts counts passing and failed events.
func OutcomeCounts(events []OutcomeEvent) (ok int, failed int) {
	for _, event := range events {
		if event.Status == "failed" {
			failed++
		} else if event.Status == "ok" {
			ok++
		}
	}
	return ok, failed
}

// OutcomeRange returns the time range covered by events.
func OutcomeRange(events []OutcomeEvent) (time.Time, time.Time) {
	if len(events) == 0 {
		now := time.Now()
		return now, now
	}
	return events[0].When, events[len(events)-1].When
}

// OutcomeBuckets maps outcome events onto a fixed number of graph columns.
func OutcomeBuckets(events []OutcomeEvent, width int) []OutcomeBucket {
	width = Max(1, width)
	buckets := make([]OutcomeBucket, width)
	if len(events) == 0 {
		return buckets
	}
	first, last := OutcomeRange(events)
	span := last.Sub(first)
	for _, event := range events {
		index := width - 1
		if span > 0 && width > 1 {
			index = int(float64(event.When.Sub(first)) / float64(span) * float64(width-1))
		} else if width > 1 {
			index = 0
		}
		index = Clamp(index, 0, width-1)
		if event.Status == "failed" {
			buckets[index].Failed++
		} else if event.Status == "ok" {
			buckets[index].OK++
		}
	}
	return buckets
}

// CheckStatusForTarget resolves the current target-level status before falling
// back to the latest historical outcome.
func (s State) CheckStatusForTarget(agent watch.AgentSnapshot, target watch.TargetSnapshot, events []OutcomeEvent) string {
	for _, state := range s.Targets {
		if SameAgent(state.Agent, agent) && CheckStatusTargetKey(state.Target) == CheckStatusTargetKey(target) {
			status := NormalizeStatus(state.Status)
			if status == "pending" && len(events) > 0 {
				return NormalizeStatus(events[len(events)-1].Status)
			}
			return status
		}
	}
	if len(events) > 0 {
		return NormalizeStatus(events[len(events)-1].Status)
	}
	return "pending"
}

// CheckStatusTargetCell aggregates agent results into one target/check cell.
func (s State) CheckStatusTargetCell(check string, target watch.TargetSnapshot, agents []watch.AgentSnapshot) CheckStatusAggregate {
	if len(agents) == 0 {
		agents = []watch.AgentSnapshot{{}}
	}
	counts := map[string]int{}
	currentCounts := map[string]int{}
	for _, agent := range agents {
		result := s.CheckStatusAgentResult(agent, target, check)
		status := NormalizeStatus(result.Status)
		counts[status]++
		if !result.Stale {
			currentCounts[status]++
		}
	}
	total := len(agents)
	failed := counts["failed"]
	switch {
	case failed > 0:
		return CheckStatusAggregate{Status: "failed", Count: failed, Failed: failed, Total: total, Stale: currentCounts["failed"] == 0}
	case counts["running"] > 0:
		return CheckStatusAggregate{Status: "running", Count: total - counts["pending"], Total: total}
	case counts["ok"] > 0:
		return CheckStatusAggregate{Status: "ok", Count: counts["ok"], Total: total, Stale: currentCounts["ok"] == 0}
	case counts["skipped"] > 0:
		return CheckStatusAggregate{Status: "skipped", Count: counts["skipped"], Total: total, Stale: currentCounts["skipped"] == 0}
	default:
		return CheckStatusAggregate{Status: "pending", Total: total}
	}
}

// CheckStatusAgentResult returns the current round state for configured targets
// and uses stale history only when no current target state exists.
func (s State) CheckStatusAgentResult(agent watch.AgentSnapshot, target watch.TargetSnapshot, check string) CheckStatusAgentResult {
	if status, ok := s.CurrentCheckStatus(agent, target, check); ok {
		return CheckStatusAgentResult{Status: status}
	}
	if status, ok := s.HistoricalCheckStatus(agent, target, check); ok {
		return CheckStatusAgentResult{Status: status, Stale: true}
	}
	return CheckStatusAgentResult{Status: "pending"}
}

// HistoricalCheckStatus returns the newest retained pass/fail status for one
// agent, target, and check.
func (s State) HistoricalCheckStatus(agent watch.AgentSnapshot, target watch.TargetSnapshot, check string) (string, bool) {
	status := "pending"
	var seen time.Time
	for _, passingCheck := range s.PassingChecks {
		if !SameAgent(passingCheck.Agent, agent) || CheckStatusTargetKey(passingCheck.Target) != CheckStatusTargetKey(target) {
			continue
		}
		if FirstNonEmpty(passingCheck.Step.Name, passingCheck.Step.Type) != check {
			continue
		}
		if passingCheck.When.After(seen) || seen.IsZero() {
			seen = passingCheck.When
			status = "ok"
		}
	}
	for _, failedCheck := range s.FailedChecks {
		if !SameAgent(failedCheck.Agent, agent) || CheckStatusTargetKey(failedCheck.Target) != CheckStatusTargetKey(target) {
			continue
		}
		if failedCheck.Finding.Check != check {
			continue
		}
		if failedCheck.When.After(seen) || seen.IsZero() {
			seen = failedCheck.When
			status = "failed"
		}
	}
	if seen.IsZero() {
		return "", false
	}
	return status, true
}

// CurrentCheckStatus returns status from the current round target/step state.
func (s State) CurrentCheckStatus(agent watch.AgentSnapshot, target watch.TargetSnapshot, check string) (string, bool) {
	for _, state := range s.Targets {
		if !SameAgent(state.Agent, agent) || CheckStatusTargetKey(state.Target) != CheckStatusTargetKey(target) {
			continue
		}
		for _, step := range state.Steps {
			if FirstNonEmpty(step.Name, step.Type) != check {
				continue
			}
			status := NormalizeStatus(step.Status)
			if status == "pending" && state.CurrentStep == step.Name {
				status = "running"
			}
			return status, true
		}
		return "pending", true
	}
	return "", false
}
