package watchstate

import (
	"sort"
	"strings"
	"time"

	"github.com/mi2428/dropcheck-agent/controller/internal/watchrun"
)

// OutcomeEvent is the compact historical outcome used by check-status and
// timeline views.
type OutcomeEvent struct {
	When   time.Time
	Round  uint64
	Agent  watchrun.AgentSnapshot
	Target watchrun.TargetSnapshot
	Status string
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
func (s State) OutcomeAgents(events []OutcomeEvent) []watchrun.AgentSnapshot {
	agents := make([]watchrun.AgentSnapshot, 0, Max(1, len(s.Agents)))
	seen := make(map[string]bool)
	add := func(agent watchrun.AgentSnapshot) {
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
		agents = append(agents, watchrun.AgentSnapshot{})
	}
	return agents
}

// CheckStatusTargets returns deduplicated targets in configured/history order.
func (s State) CheckStatusTargets() []watchrun.TargetSnapshot {
	targets := make([]watchrun.TargetSnapshot, 0, len(s.Targets))
	seen := make(map[string]bool)
	add := func(target watchrun.TargetSnapshot) {
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
			add(FirstNonEmpty(step.ID, step.Name, step.Type))
		}
		for _, step := range target.Steps {
			add(FirstNonEmpty(step.ID, step.Name, step.Type))
		}
	}
	for _, passingCheck := range s.PassingChecks {
		add(FirstNonEmpty(passingCheck.Step.ID, passingCheck.Step.Name, passingCheck.Step.Type))
	}
	for _, failedCheck := range s.FailedChecks {
		add(FirstNonEmpty(failedCheck.Finding.CheckID, failedCheck.Finding.Check))
	}
	return checks
}

// OutcomeCounts counts passing and failed events.
func OutcomeCounts(events []OutcomeEvent) (ok int, failed int) {
	for _, event := range events {
		switch event.Status {
		case "failed":
			failed++
		case "ok":
			ok++
		}
	}
	return ok, failed
}

// CheckStatusForTarget resolves the current target-level status before falling
// back to the latest historical outcome.
func (s State) CheckStatusForTarget(agent watchrun.AgentSnapshot, target watchrun.TargetSnapshot, events []OutcomeEvent) string {
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
func (s State) CheckStatusTargetCell(check string, target watchrun.TargetSnapshot, agents []watchrun.AgentSnapshot) CheckStatusAggregate {
	if len(agents) == 0 {
		agents = []watchrun.AgentSnapshot{{}}
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
	case counts["missing"] > 0:
		return CheckStatusAggregate{Status: "missing", Count: counts["missing"], Total: total}
	case counts["canceled"] > 0:
		return CheckStatusAggregate{Status: "canceled", Count: counts["canceled"], Total: total}
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
// and falls back to stale history until the current round emits this check.
func (s State) CheckStatusAgentResult(agent watchrun.AgentSnapshot, target watchrun.TargetSnapshot, check string) CheckStatusAgentResult {
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
func (s State) HistoricalCheckStatus(agent watchrun.AgentSnapshot, target watchrun.TargetSnapshot, check string) (string, bool) {
	status := "pending"
	var seen time.Time
	for _, passingCheck := range s.PassingChecks {
		if !SameAgent(passingCheck.Agent, agent) || CheckStatusTargetKey(passingCheck.Target) != CheckStatusTargetKey(target) {
			continue
		}
		if FirstNonEmpty(passingCheck.Step.ID, passingCheck.Step.Name, passingCheck.Step.Type) != check {
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
		if FirstNonEmpty(failedCheck.Finding.CheckID, failedCheck.Finding.Check) != check {
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
//
// Returning "not found" when the target exists but has not emitted this step in
// the current round lets the dashboard retain the most recent completed result
// until fresh data for that check arrives.
func (s State) CurrentCheckStatus(agent watchrun.AgentSnapshot, target watchrun.TargetSnapshot, check string) (string, bool) {
	for _, state := range s.Targets {
		if !SameAgent(state.Agent, agent) || CheckStatusTargetKey(state.Target) != CheckStatusTargetKey(target) {
			continue
		}
		for _, step := range state.Steps {
			if FirstNonEmpty(step.ID, step.Name, step.Type) != check {
				continue
			}
			status := NormalizeStatus(step.Status)
			if status == "pending" && (step.ID != "" && state.CurrentStepID == step.ID || step.ID == "" && state.CurrentStepID == "" && state.CurrentStep == step.Name) {
				status = "running"
			}
			return status, true
		}
		if state.Target.ID != "" {
			return "pending", true
		}
		return "", false
	}
	return "", false
}

func (s State) CheckLabel(id string) string {
	for _, check := range s.Checks {
		if check.ID == id {
			return DisplayCheckName(check.Name)
		}
	}
	for _, target := range s.Targets {
		for _, step := range append(append([]StepState(nil), target.PlannedSteps...), target.Steps...) {
			if step.ID == id {
				return DisplayCheckName(step.Name)
			}
		}
	}
	return DisplayCheckName(id)
}
