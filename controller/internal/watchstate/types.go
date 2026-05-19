package watchstate

import (
	"time"

	"dropcheck/controller/internal/watch"
)

// TargetState is the latest watch state for one agent/target pair.
type TargetState struct {
	Agent        watch.AgentSnapshot
	Target       watch.TargetSnapshot
	Status       string
	CurrentStep  string
	Steps        []StepState
	PlannedSteps []StepState
}

// StepState is the compact per-step state kept for run queue rendering.
type StepState struct {
	Name    string
	Type    string
	Status  string
	Message string
}

// PlannedStepsForTarget expands configured checks into the run queue shape shown before live events arrive.
func PlannedStepsForTarget(target watch.Target, checks []watch.Check) []StepState {
	if checks == nil {
		return nil
	}
	steps := PlannedStepsForChecks(checks)
	if target.DisconnectAfter == nil || *target.DisconnectAfter {
		steps = append(steps, StepState{Name: "disconnect", Type: "cleanup"})
	}
	if target.ForgetAfter != nil && *target.ForgetAfter {
		steps = append(steps, StepState{Name: "forget", Type: "cleanup"})
	}
	return steps
}

// PlannedStepsForChecks returns the stable required-step prefix followed by configured checks.
func PlannedStepsForChecks(checks []watch.Check) []StepState {
	if checks == nil {
		return nil
	}
	steps := []StepState{{Name: "connect", Type: "connect"}, {Name: "wait_connected", Type: "wait_connected"}}
	for _, check := range checks {
		steps = append(steps, StepState{Name: check.DisplayName(), Type: check.Type})
	}
	return steps
}

// FailedCheck records one failed expectation or required connectivity step.
type FailedCheck struct {
	Round   uint64
	When    time.Time
	Agent   watch.AgentSnapshot
	Target  watch.TargetSnapshot
	Finding watch.Finding
}

// FailedCheckSummary aggregates failures sharing one target/finding identity.
type FailedCheckSummary struct {
	Last        time.Time
	Count       int
	FailPercent int
	FailStreak  int
	Agent       watch.AgentSnapshot
	Target      watch.TargetSnapshot
	Finding     watch.Finding
}

// PassingCheck records one successful check or target completion.
type PassingCheck struct {
	Round    uint64
	When     time.Time
	Agent    watch.AgentSnapshot
	Target   watch.TargetSnapshot
	Step     watch.StepSnapshot
	Duration int64
}

// PassingCheckSummary aggregates successful checks by agent, target, and step.
type PassingCheckSummary struct {
	Last          time.Time
	Count         int
	Agent         watch.AgentSnapshot
	Target        watch.TargetSnapshot
	Step          watch.StepSnapshot
	Duration      int64
	DurationTotal int64
	DurationCount int
	MaxDuration   int64
}

// AvgDuration returns the rounded average duration in milliseconds.
func (item PassingCheckSummary) AvgDuration() int64 {
	if item.DurationCount <= 0 {
		return 0
	}
	return (item.DurationTotal + int64(item.DurationCount/2)) / int64(item.DurationCount)
}

// FailureHotspotSummary ranks targets that are failing repeatedly inside the
// retained investigation history.
type FailureHotspotSummary struct {
	Agent         watch.AgentSnapshot
	Target        watch.TargetSnapshot
	Last          time.Time
	FailCount     int
	FailRunCount  int
	RunCount      int
	FailStreak    int
	LatestCause   string
	LatestFinding watch.Finding
}

// FailureCauseTargetSummary is the per-target breakdown inside one
// cross-target failure cause.
type FailureCauseTargetSummary struct {
	Target        watch.TargetSnapshot
	Last          time.Time
	FailCount     int
	FailRunCount  int
	RunCount      int
	LatestFinding watch.Finding
}

// FailureCauseSummary ranks causes that affect multiple targets inside the
// retained investigation history.
type FailureCauseSummary struct {
	Agent              watch.AgentSnapshot
	Cause              string
	Last               time.Time
	FailCount          int
	FailRunCount       int
	RunCount           int
	TargetCount        int
	TopTarget          watch.TargetSnapshot
	TopTargetFailCount int
	LatestFinding      watch.Finding
	Targets            []FailureCauseTargetSummary
}

// EventLogEntry keeps the structured watch event beside the rendered log line for detail scoping.
type EventLogEntry struct {
	When  time.Time
	Event watch.Event
	Line  string
}

// ConnectState is the latest live association phase reported for one agent.
type ConnectState struct {
	Last       time.Time
	Agent      watch.AgentSnapshot
	Target     watch.TargetSnapshot
	Supplicant string
	SSID       string
	BSSID      string
	IP         string
}

type failureHotspotRun struct {
	Last   time.Time
	failed bool
}

type failedCheckAttempt struct {
	When       time.Time
	FailedKeys map[string]bool
}

// OccurrenceHistogram is a fixed-width time histogram for recent event graphs.
type OccurrenceHistogram struct {
	First       time.Time
	Last        time.Time
	BucketWidth time.Duration
	Counts      []int
	Max         int
	Count       int
}

const (
	SummarySparklineWindow      = 30 * time.Minute
	InvestigationHistoryWindow  = 24 * time.Hour
	FailureHotspotWindow        = InvestigationHistoryWindow
	CheckHistoryRetentionWindow = InvestigationHistoryWindow
	EventLogRetentionWindow     = InvestigationHistoryWindow
	MaxPassingCheckHistory      = 200000
	MaxFailedCheckHistory       = 100000
	MaxEventLogHistory          = 100000
	VisibleEventLogLimit        = 400
)

// State tracks watch progress and derived histories without depending on terminal UI packages.
type State struct {
	Now             time.Time
	Round           uint64
	RoundStatus     string
	Phase           string
	Targets         []TargetState
	TargetIndex     map[string]int
	Agents          []watch.AgentSnapshot
	Checks          []watch.Check
	MultiAgent      bool
	FailedChecks    []FailedCheck
	PassingChecks   []PassingCheck
	Logs            []string
	EventLogEntries []EventLogEntry
	ConnectStates   []ConnectState
	EventLogTarget  string
	EventLogStep    string
	EventLogLast    string
}

// New constructs the initial state for one watch plan and the selected agents.
func New(targets []watch.Target, checks []watch.Check, agents []watch.AgentSnapshot, now time.Time) State {
	states := make([]TargetState, 0, Max(1, len(agents))*len(targets))
	index := make(map[string]int, Max(1, len(agents))*len(targets))
	addTarget := func(agent watch.AgentSnapshot, target watch.Target) {
		snapshot := watch.TargetSnapshot{Name: target.DisplayName(), ShortName: target.ShortName, Agent: target.Agent, SSID: target.SSID, BSSID: target.BSSID, Band: target.Band}
		key := TargetStateKey(agent, snapshot)
		index[key] = len(states)
		states = append(states, TargetState{Agent: agent, Target: snapshot, Status: "pending", PlannedSteps: PlannedStepsForTarget(target, checks)})
	}
	if len(agents) == 0 {
		for _, target := range targets {
			addTarget(watch.AgentSnapshot{}, target)
		}
	} else {
		for _, target := range targets {
			for _, agent := range agents {
				if target.Agent != "" && !watch.AgentSnapshotMatches(agent, target.Agent) {
					continue
				}
				addTarget(agent, target)
			}
		}
	}
	return State{Targets: states, TargetIndex: index, Agents: append([]watch.AgentSnapshot(nil), agents...), Checks: append([]watch.Check(nil), checks...), MultiAgent: len(agents) > 1, Now: now, RoundStatus: "starting", Phase: "starting"}
}
