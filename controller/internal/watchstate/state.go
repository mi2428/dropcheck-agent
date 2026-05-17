package watchstate

import (
	"fmt"
	"strings"
	"time"

	"dropcheck/controller/internal/watch"
)

// Apply folds one watch event into target state, histories, and event-log
// summary fields. Cursor movement and repaint decisions stay in the TUI layer.
func (s *State) Apply(event watch.Event) {
	s.PushEventLog(event)
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
			if s.MultiAgent && !SameAgent(s.Targets[i].Agent, event.Agent) {
				continue
			}
			s.Targets[i].Status = "pending"
			s.Targets[i].CurrentStep = ""
			s.Targets[i].Steps = nil
		}
	case watch.EventRoundFinished:
		s.RoundStatus = event.Status
		s.Phase = "idle"
	case watch.EventTargetStarted:
		target := s.EnsureTarget(event.Agent, event.Target)
		target.Status = "running"
		s.Phase = s.EventTargetLabel(event)
		s.EventLogTarget = s.EventTargetLabel(event)
		s.EventLogStep = ""
		s.EventLogLast = "target started"
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
		s.Phase = s.EventTargetLabel(event) + "/" + event.Step.Name
		upsertStep(target, event.Step)
		s.EventLogTarget = s.EventTargetLabel(event)
		s.EventLogStep = event.Step.Name
		s.EventLogLast = event.Step.Name + " running"
	case watch.EventStepFinished:
		target := s.EnsureTarget(event.Agent, event.Target)
		if event.Step.Status != "running" && target.CurrentStep == event.Step.Name {
			target.CurrentStep = ""
		}
		upsertStep(target, event.Step)
		if PassingCheckEvent(event) {
			s.RecordPassingCheck(PassingCheck{
				Round:    event.Round,
				When:     EventTime(event),
				Agent:    event.Agent,
				Target:   event.Target,
				Step:     event.Step,
				Duration: event.Duration,
			})
		}
		if finding, ok := RequiredStepFailedCheck(event); ok {
			s.AddFailedCheck(event.Agent, event.Target, event.Round, EventTime(event), finding)
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
	if update.Name != "" {
		base.Name = update.Name
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
		if target.Steps[i].Name == name {
			target.Steps[i] = StepState{Name: name, Type: snapshot.Type, Status: snapshot.Status, Message: FirstNonEmpty(snapshot.Message, snapshot.Error)}
			return
		}
	}
	target.Steps = append(target.Steps, StepState{Name: name, Type: snapshot.Type, Status: snapshot.Status, Message: FirstNonEmpty(snapshot.Message, snapshot.Error)})
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
		items = filterPassingChecksSince(items, cutoff)
	}
	if len(items) > MaxPassingCheckHistory {
		items = items[len(items)-MaxPassingCheckHistory:]
	}
	return items
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
		items = filterFailedChecksSince(items, cutoff)
	}
	if len(items) > MaxFailedCheckHistory {
		items = items[len(items)-MaxFailedCheckHistory:]
	}
	return items
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
	s.PassingChecks = filtered
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
		s.Logs = s.Logs[len(s.Logs)-VisibleEventLogLimit:]
	}
}
