package watchstate

import (
	"fmt"
	"strings"
	"time"

	watch "dropcheck/controller/internal/harness"
)

// Apply folds one watch event into target state, histories, and event-log
// summary fields. Cursor movement and repaint decisions stay in the TUI layer.
func (s *State) Apply(event watch.Event) {
	s.PushEventLog(event)
	if event.Kind == watch.EventLog {
		s.RecordConnectState(event)
	}
	if event.Round > s.Round {
		s.Round = event.Round
	}
	switch event.Kind {
	case watch.EventWatchStarted:
	case watch.EventRoundStarted:
		s.Round = event.Round
		s.RoundStatus = "running"
		s.Phase = fmt.Sprintf("round %d", event.Round)
		for i := range s.Targets {
			if s.MultiAgent && AgentKey(event.Agent) != "" && !SameAgent(s.Targets[i].Agent, event.Agent) {
				continue
			}
			s.Targets[i].Status = "pending"
			s.Targets[i].CurrentStep = ""
			s.Targets[i].CurrentStepID = ""
			s.Targets[i].Steps = nil
		}
	case watch.EventRoundFinished:
		if len(event.Agents) > 0 {
			s.AgentProgress = append([]watch.AgentProgress(nil), event.Agents...)
			s.RoundStatus = event.Status
			s.Phase = "idle"
		} else if !s.MultiAgent {
			s.RoundStatus = event.Status
			s.Phase = "idle"
		} else {
			s.setAgentProgress(event.Agent, event.Round, "completed", event.Status)
			s.aggregateAgents()
		}
	case watch.EventTargetStarted:
		target := s.EnsureTarget(event.Agent, event.Target)
		target.Status = FirstNonEmpty(event.Status, "running")
		s.Phase = s.EventTargetLabel(event)
		s.EventLogTarget = s.EventTargetLabel(event)
		s.EventLogStep = ""
		s.EventLogLast = "target " + FirstNonEmpty(event.Status, "started")
	case watch.EventTargetFinished:
		target := s.EnsureTarget(event.Agent, event.Target)
		target.Status = event.Status
		target.CurrentStep = ""
		if FirstNonEmpty(event.Status, target.Status) == "ok" {
			s.RecordPassingCheck(PassingCheck{
				Round:  event.Round,
				When:   EventTime(event),
				Agent:  event.Agent,
				Target: event.Target,
				Step: watch.StepSnapshot{
					Name:   "target",
					Type:   "target",
					Status: "ok",
				},
				Duration: event.Duration,
			})
		}
		s.EventLogTarget = s.EventTargetLabel(event)
		s.EventLogStep = ""
		s.EventLogLast = "target " + FirstNonEmpty(event.Status, "finished")
	case watch.EventStepStarted:
		target := s.EnsureTarget(event.Agent, event.Target)
		target.CurrentStep = event.Step.Name
		target.CurrentStepID = event.Step.ID
		target.Status = "running"
		s.Phase = s.EventTargetLabel(event) + "/" + event.Step.Name
		upsertStep(target, event.Step)
		s.EventLogTarget = s.EventTargetLabel(event)
		s.EventLogStep = event.Step.Name
		s.EventLogLast = event.Step.Name + " running"
	case watch.EventStepFinished:
		target := s.EnsureTarget(event.Agent, event.Target)
		if event.Step.Status != "running" && (event.Step.ID != "" && target.CurrentStepID == event.Step.ID || event.Step.ID == "" && target.CurrentStep == event.Step.Name) {
			target.CurrentStep = ""
			target.CurrentStepID = ""
		}
		upsertStep(target, event.Step)
		if PassingCheckEvent(event) {
			s.RecordPassingCheck(PassingCheck{
				Report:   event.Report,
				Round:    event.Round,
				When:     EventTime(event),
				Agent:    event.Agent,
				Target:   event.Target,
				Step:     event.Step,
				Duration: event.Duration,
			})
		}
		if event.Step.Status == "failed" || event.Step.Status == "missing" || event.Step.Status == "canceled" {
			target.Status = event.Step.Status
			s.RemovePassingCheckForFailedCheck(event)
		}
		s.EventLogTarget = s.EventTargetLabel(event)
		s.EventLogStep = event.Step.Name
		s.EventLogLast = event.Step.Name + " " + FirstNonEmpty(event.Step.Message, event.Step.Error, event.Step.Status, event.Status)
	case watch.EventFinding:
		if event.Finding != nil {
			s.RemovePassingCheckForFailedCheck(event)
			s.AddFailedCheck(event.Agent, event.Target, event.Round, event.Time, *event.Finding)
			target := s.EnsureTarget(event.Agent, event.Target)
			target.Status = "failed"
			event.Step.Status = "failed"
			upsertStep(target, event.Step)
			s.EventLogTarget = s.EventTargetLabel(event)
			s.EventLogStep = FirstNonEmpty(event.Step.Name, event.Finding.Check)
			s.EventLogLast = FirstNonEmpty(event.Finding.Check, event.Step.Name) + " " + FirstNonEmpty(event.Finding.Message, event.Finding.Metric+"="+event.Finding.Observed)
		}
	case watch.EventLog:
	case watch.EventControlRequested:
		if event.Status == "pause" {
			s.Phase = "pausing"
		}
	case watch.EventControlApplied:
		s.setAgentProgress(event.Agent, event.Round, event.Status, event.Status)
		s.aggregateAgents()
	case watch.EventRunFinished:
		s.Phase = event.Status
	}
}

// EnsureTarget returns the mutable target state for agent and snapshot,
// creating it when a live event references a target that was not in the initial
// plan. Partial snapshots are merged by the stable check-status target key.
func (s *State) EnsureTarget(agent watch.AgentSnapshot, snapshot watch.TargetSnapshot) *TargetState {
	name := snapshot.Name
	if name == "" {
		name = FirstNonEmpty(snapshot.SSID, snapshot.BSSID, "target")
		snapshot.Name = name
	}
	key := TargetStateKey(agent, snapshot)
	if index, ok := s.TargetIndex[key]; ok {
		return &s.Targets[index]
	}
	if fallback := CheckStatusTargetKey(snapshot); fallback != "" {
		for i := range s.Targets {
			if !SameAgent(s.Targets[i].Agent, agent) || CheckStatusTargetKey(s.Targets[i].Target) != fallback {
				continue
			}
			s.Targets[i].Target = MergeTargetSnapshot(s.Targets[i].Target, snapshot)
			s.TargetIndex[key] = i
			return &s.Targets[i]
		}
	}
	s.TargetIndex[key] = len(s.Targets)
	s.Targets = append(s.Targets, TargetState{Agent: agent, Target: snapshot, Status: "pending", PlannedSteps: PlannedStepsForChecks(s.Checks)})
	return &s.Targets[len(s.Targets)-1]
}

// MergeTargetSnapshot fills non-empty fields from update into base.
func MergeTargetSnapshot(base watch.TargetSnapshot, update watch.TargetSnapshot) watch.TargetSnapshot {
	if update.ID != "" {
		base.ID = update.ID
	}
	if update.Name != "" {
		base.Name = update.Name
	}
	if update.ShortName != "" {
		base.ShortName = update.ShortName
	}
	if update.Agent != "" {
		base.Agent = update.Agent
	}
	if update.SSID != "" {
		base.SSID = update.SSID
	}
	if update.BSSID != "" {
		base.BSSID = update.BSSID
	}
	if update.Band != "" {
		base.Band = update.Band
	}
	return base
}

// AddFailedCheck appends one failed finding and enforces the bounded failure
// history retention policy.
func (s *State) AddFailedCheck(agent watch.AgentSnapshot, target watch.TargetSnapshot, round uint64, when time.Time, finding watch.Finding) {
	if when.IsZero() {
		when = time.Now()
	}
	if target.Name == "" {
		target.Name = FirstNonEmpty(target.SSID, target.BSSID, finding.Target, "target")
	}
	s.FailedChecks = append(s.FailedChecks, FailedCheck{Round: round, When: when, Agent: agent, Target: target, Finding: finding})
	s.TrimFailedChecks(when)
}

func upsertStep(target *TargetState, snapshot watch.StepSnapshot) {
	name := snapshot.Name
	if name == "" {
		name = snapshot.Type
	}
	for i := range target.Steps {
		if snapshot.ID != "" && target.Steps[i].ID == snapshot.ID || snapshot.ID == "" && target.Steps[i].Name == name {
			target.Steps[i] = StepState{ID: snapshot.ID, Name: name, Type: snapshot.Type, Status: snapshot.Status, Message: FirstNonEmpty(snapshot.Message, snapshot.Error)}
			return
		}
	}
	target.Steps = append(target.Steps, StepState{ID: snapshot.ID, Name: name, Type: snapshot.Type, Status: snapshot.Status, Message: FirstNonEmpty(snapshot.Message, snapshot.Error)})
}

func (s *State) setAgentProgress(agent watch.AgentSnapshot, round uint64, state, status string) {
	for i := range s.AgentProgress {
		if SameAgent(s.AgentProgress[i].Agent, agent) {
			s.AgentProgress[i].Round, s.AgentProgress[i].State, s.AgentProgress[i].Phase = round, state, status
			return
		}
	}
	s.AgentProgress = append(s.AgentProgress, watch.AgentProgress{Agent: agent, Round: round, State: state, Phase: status})
}

func (s *State) aggregateAgents() {
	completed, paused := 0, 0
	failed := false
	for _, agent := range s.Agents {
		for _, progress := range s.AgentProgress {
			if !SameAgent(agent, progress.Agent) || progress.Round != s.Round {
				continue
			}
			if progress.State == "completed" {
				completed++
				if progress.Phase == "failed" || progress.Phase == "missing" {
					failed = true
				}
			}
			if progress.State == "paused" {
				paused++
			}
		}
	}
	switch {
	case completed == len(s.Agents):
		s.Phase = "idle"
		s.RoundStatus = "ok"
		if failed {
			s.RoundStatus = "failed"
		}
	case paused+completed == len(s.Agents):
		s.Phase = "paused"
	default:
		s.Phase = "multi-agent running"
		s.RoundStatus = "running"
	}
}

// RecordPassingCheck appends one successful check and enforces the bounded
// passing history retention policy.
func (s *State) RecordPassingCheck(passingCheck PassingCheck) {
	if passingCheck.When.IsZero() {
		passingCheck.When = time.Now()
	}
	if passingCheck.Target.Name == "" {
		passingCheck.Target.Name = FirstNonEmpty(passingCheck.Target.SSID, passingCheck.Target.BSSID, "target")
	}
	if passingCheck.Step.Name == "" {
		passingCheck.Step.Name = FirstNonEmpty(passingCheck.Step.Type, "step")
	}
	s.PassingChecks = append(s.PassingChecks, passingCheck)
	s.TrimPassingChecks(passingCheck.When)
}

// TrimPassingChecks drops old passing history relative to reference.
func (s *State) TrimPassingChecks(reference time.Time) {
	s.PassingChecks = trimPassingCheckHistory(s.PassingChecks, reference)
}

// TrimFailedChecks drops old failure history relative to reference.
func (s *State) TrimFailedChecks(reference time.Time) {
	s.FailedChecks = trimFailedCheckHistory(s.FailedChecks, reference)
}

func trimPassingCheckHistory(items []PassingCheck, reference time.Time) []PassingCheck {
	if len(items) == 0 {
		return items
	}
	if reference.IsZero() {
		reference = latestPassingCheckTime(items)
	}
	if !reference.IsZero() {
		cutoff := reference.Add(-CheckHistoryRetentionWindow)
		before := len(items)
		filtered := filterPassingChecksSince(items, cutoff)
		clear(filtered[len(filtered):before])
		items = filtered
	}
	if len(items) > MaxPassingCheckHistory {
		drop := len(items) - MaxPassingCheckHistory
		clear(items[:drop])
		items = items[drop:]
	}
	return compactHistory(items)
}

func trimFailedCheckHistory(items []FailedCheck, reference time.Time) []FailedCheck {
	if len(items) == 0 {
		return items
	}
	if reference.IsZero() {
		reference = latestFailedCheckTime(items)
	}
	if !reference.IsZero() {
		cutoff := reference.Add(-CheckHistoryRetentionWindow)
		before := len(items)
		filtered := filterFailedChecksSince(items, cutoff)
		clear(filtered[len(filtered):before])
		items = filtered
	}
	if len(items) > MaxFailedCheckHistory {
		drop := len(items) - MaxFailedCheckHistory
		clear(items[:drop])
		items = items[drop:]
	}
	return compactHistory(items)
}

func filterPassingChecksSince(items []PassingCheck, cutoff time.Time) []PassingCheck {
	filtered := items[:0]
	for _, item := range items {
		if item.When.IsZero() || item.When.Before(cutoff) {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

func filterFailedChecksSince(items []FailedCheck, cutoff time.Time) []FailedCheck {
	filtered := items[:0]
	for _, item := range items {
		if item.When.IsZero() || item.When.Before(cutoff) {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

func latestPassingCheckTime(items []PassingCheck) time.Time {
	var latest time.Time
	for _, item := range items {
		if item.When.After(latest) {
			latest = item.When
		}
	}
	return latest
}

func latestFailedCheckTime(items []FailedCheck) time.Time {
	var latest time.Time
	for _, item := range items {
		if item.When.After(latest) {
			latest = item.When
		}
	}
	return latest
}

func compactHistory[T any](items []T) []T {
	if len(items) == 0 {
		return nil
	}
	if cap(items) <= Max(1024, len(items)*2) {
		return items
	}
	compacted := make([]T, len(items))
	copy(compacted, items)
	return compacted
}

// RemovePassingCheckForFailedCheck removes an optimistic same-round pass when a
// later finding reports that the same step actually failed.
func (s *State) RemovePassingCheckForFailedCheck(event watch.Event) {
	key := PassingCheckKey(event.Agent, event.Target, event.Step)
	if key == "" {
		return
	}
	filtered := s.PassingChecks[:0]
	for _, passingCheck := range s.PassingChecks {
		if passingCheck.Round == event.Round && PassingCheckKey(passingCheck.Agent, passingCheck.Target, passingCheck.Step) == key {
			continue
		}
		filtered = append(filtered, passingCheck)
	}
	clear(filtered[len(filtered):len(s.PassingChecks)])
	s.PassingChecks = compactHistory(filtered)
}

// PushLog appends a controller-generated log line that does not have a
// structured watch.Event.
func (s *State) PushLog(message string) {
	message = strings.TrimSpace(SanitizeLogText(message))
	if message == "" {
		return
	}
	when := time.Now()
	line := when.Format("15:04:05") + " " + message
	s.PushVisibleLog(line)
	s.EventLogEntries = append(s.EventLogEntries, EventLogEntry{When: when, Line: line})
	s.TrimEventLogEntries(when)
}

// PushVisibleLog appends a line to the bounded visible event log.
func (s *State) PushVisibleLog(line string) {
	s.Logs = append(s.Logs, line)
	if len(s.Logs) > VisibleEventLogLimit {
		drop := len(s.Logs) - VisibleEventLogLimit
		clear(s.Logs[:drop])
		s.Logs = compactHistory(s.Logs[drop:])
	}
}
