package watchstate

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"dropcheck/controller/internal/watch"
)

// CurrentTarget returns the active target, preferring a target with a running
// step over a target that is only marked running.
func (s State) CurrentTarget() (TargetState, bool) {
	for _, target := range s.Targets {
		if target.CurrentStep != "" {
			return target, true
		}
	}
	for _, target := range s.Targets {
		if target.Status == "running" {
			return target, true
		}
	}
	return TargetState{}, false
}

func currentStepState(target TargetState) (StepState, bool) {
	for _, step := range target.Steps {
		if step.Name == target.CurrentStep {
			return step, true
		}
	}
	return StepState{}, false
}

// TargetFailedCheckCount counts failed checks for one target label and agent.
func (s State) TargetFailedCheckCount(agent watch.AgentSnapshot, target string) int {
	count := 0
	for _, item := range s.FailedChecks {
		if SameAgent(item.Agent, agent) && item.Finding.Target == target {
			count++
		}
	}
	return count
}

// TargetCount counts current targets with the exact normalized status used by
// the watch event stream.
func (s State) TargetCount(status string) int {
	count := 0
	for _, target := range s.Targets {
		if target.Status == status {
			count++
		}
	}
	return count
}

// PassingCheckSummaries groups passing history by target and step, newest
// summary first.
func (s State) PassingCheckSummaries() []PassingCheckSummary {
	rows := make([]PassingCheckSummary, 0, len(s.PassingChecks))
	index := make(map[string]int, len(s.PassingChecks))
	for _, item := range s.PassingChecks {
		key := PassingCheckSummaryKey(item.Agent, item.Target, item.Step)
		if pos, ok := index[key]; ok {
			rows[pos].Last = item.When
			rows[pos].Count++
			rows[pos].Agent = item.Agent
			rows[pos].Target = item.Target
			rows[pos].Step = item.Step
			rows[pos].Duration = item.Duration
			if item.Duration > 0 {
				rows[pos].DurationTotal += item.Duration
				rows[pos].DurationCount++
				rows[pos].MaxDuration = MaxInt64(rows[pos].MaxDuration, item.Duration)
			}
			continue
		}
		index[key] = len(rows)
		row := PassingCheckSummary{
			Last:     item.When,
			Count:    1,
			Agent:    item.Agent,
			Target:   item.Target,
			Step:     item.Step,
			Duration: item.Duration,
		}
		if item.Duration > 0 {
			row.DurationTotal = item.Duration
			row.DurationCount = 1
			row.MaxDuration = item.Duration
		}
		rows = append(rows, row)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		return rows[i].Last.After(rows[j].Last)
	})
	return rows
}

// FilteredPassingCheckSummaries returns passing summaries that match queryValue.
func (s State) FilteredPassingCheckSummaries(queryValue string) []PassingCheckSummary {
	rows := s.PassingCheckSummaries()
	query := NormalizedSearchQuery(queryValue)
	if query == "" {
		return rows
	}
	filtered := rows[:0]
	for _, row := range rows {
		if PassingCheckSummaryMatches(row, query) {
			filtered = append(filtered, row)
		}
	}
	return filtered
}

// FailedCheckSummaries groups failure history by target and finding identity,
// newest summary first.
func (s State) FailedCheckSummaries() []FailedCheckSummary {
	rows := make([]FailedCheckSummary, 0, len(s.FailedChecks))
	index := make(map[string]int, len(s.FailedChecks))
	for _, item := range s.FailedChecks {
		finding := item.Finding
		key := FailedCheckSummaryKey(item.Target, finding)
		if pos, ok := index[key]; ok {
			rows[pos].Last = item.When
			rows[pos].Count++
			rows[pos].Target = item.Target
			rows[pos].Finding = finding
			continue
		}
		index[key] = len(rows)
		rows = append(rows, FailedCheckSummary{Last: item.When, Count: 1, Target: item.Target, Finding: finding})
	}
	s.DecorateFailedCheckSummaries(rows)
	sort.SliceStable(rows, func(i, j int) bool {
		return rows[i].Last.After(rows[j].Last)
	})
	return rows
}

// DecorateFailedCheckSummaries adds failure percentage and streak metadata to
// rows using both passing and failed attempts.
func (s State) DecorateFailedCheckSummaries(rows []FailedCheckSummary) {
	if len(rows) == 0 {
		return
	}
	attemptsByGroup := s.failedCheckAttemptsByGroup()
	for i := range rows {
		key := FailedCheckSummaryIdentity(rows[i])
		attempts := attemptsByGroup[FailedCheckAttemptGroupKey(rows[i].Target, rows[i].Finding.Target, rows[i].Finding.Check)]
		failedAttempts := 0
		for _, attempt := range attempts {
			if attempt.FailedKeys[key] {
				failedAttempts++
			}
		}
		if len(attempts) > 0 {
			rows[i].FailPercent = failedAttempts * 100 / len(attempts)
		}
		rows[i].FailStreak = FailedCheckStreak(attempts, key)
	}
}

func (s State) failedCheckAttemptsByGroup() map[string][]failedCheckAttempt {
	type attemptIndex struct {
		group string
		index int
	}
	groups := map[string][]failedCheckAttempt{}
	indexes := map[string]attemptIndex{}
	upsert := func(group string, attemptKey string, at time.Time, failed bool, failedKey string) {
		if group == "" || attemptKey == "" {
			return
		}
		indexKey := group + "\x00" + attemptKey
		index, ok := indexes[indexKey]
		if !ok {
			index = attemptIndex{group: group, index: len(groups[group])}
			indexes[indexKey] = index
			groups[group] = append(groups[group], failedCheckAttempt{When: at})
		}
		attempt := groups[index.group][index.index]
		if at.After(attempt.When) || attempt.When.IsZero() {
			attempt.When = at
		}
		if failed {
			if failedKey != "" {
				if attempt.FailedKeys == nil {
					attempt.FailedKeys = map[string]bool{}
				}
				attempt.FailedKeys[failedKey] = true
			}
		}
		groups[index.group][index.index] = attempt
	}
	for _, item := range s.PassingChecks {
		check := FirstNonEmpty(item.Step.Name, item.Step.Type)
		group := FailedCheckAttemptGroupKey(item.Target, "", check)
		upsert(group, CheckAttemptKey(item.Agent, item.Round, item.When), item.When, false, "")
	}
	for _, item := range s.FailedChecks {
		group := FailedCheckAttemptGroupKey(item.Target, item.Finding.Target, item.Finding.Check)
		upsert(group, CheckAttemptKey(item.Agent, item.Round, item.When), item.When, true, FailedCheckSummaryKey(item.Target, item.Finding))
	}
	for group := range groups {
		sort.SliceStable(groups[group], func(i, j int) bool {
			return groups[group][i].When.After(groups[group][j].When)
		})
	}
	return groups
}

// FailedCheckAttemptGroupKey returns the grouping key used to compute failure
// percentages and streaks.
func FailedCheckAttemptGroupKey(target watch.TargetSnapshot, findingTarget string, check string) string {
	targetKey := FirstNonEmpty(CheckStatusTargetKey(target), findingTarget)
	check = FirstNonEmpty(check)
	if targetKey == "" || check == "" {
		return ""
	}
	return strings.Join([]string{targetKey, check}, "\x00")
}

// CheckAttemptKey returns a per-agent attempt key for one round or timestamp.
func CheckAttemptKey(agent watch.AgentSnapshot, round uint64, at time.Time) string {
	run := ""
	if round > 0 {
		run = fmt.Sprint(round)
	} else if !at.IsZero() {
		run = at.Format(time.RFC3339Nano)
	}
	if run == "" {
		return ""
	}
	return strings.Join([]string{RoundAgentKey(agent), run}, "\x00")
}

// FailedCheckStreak counts consecutive failed attempts for failedKey.
func FailedCheckStreak(attempts []failedCheckAttempt, failedKey string) int {
	streak := 0
	for _, attempt := range attempts {
		if attempt.FailedKeys[failedKey] {
			streak++
			continue
		}
		break
	}
	return streak
}

// FailureHotspots ranks targets by current failure streak, recent failure rate,
// total failures, and recency inside SummarySparklineWindow.
func (s State) FailureHotspots() []FailureHotspotSummary {
	now := s.CurrentTime()
	start := now.Add(-SummarySparklineWindow)
	type aggregate struct {
		summary FailureHotspotSummary
		runs    map[string]failureHotspotRun
	}
	aggregates := map[string]*aggregate{}
	ensure := func(agent watch.AgentSnapshot, target watch.TargetSnapshot, findingTargets ...string) *aggregate {
		if target.Name == "" {
			target.Name = FirstNonEmpty(target.SSID, target.BSSID, FirstNonEmpty(findingTargets...), "target")
		}
		key := FailureHotspotIdentity(agent, target, findingTargets...)
		item, ok := aggregates[key]
		if !ok {
			item = &aggregate{summary: FailureHotspotSummary{Agent: agent, Target: target}, runs: map[string]failureHotspotRun{}}
			aggregates[key] = item
		}
		if AgentKey(item.summary.Agent) == "" {
			item.summary.Agent = agent
		}
		item.summary.Target = MergeTargetSnapshot(item.summary.Target, target)
		return item
	}
	recordRun := func(item *aggregate, round uint64, at time.Time, failed bool) {
		if at.IsZero() {
			at = now
		}
		key := FailureHotspotRunKey(round, at)
		run := item.runs[key]
		if at.After(run.Last) || run.Last.IsZero() {
			run.Last = at
		}
		run.failed = run.failed || failed
		item.runs[key] = run
		if at.After(item.summary.Last) || item.summary.Last.IsZero() {
			item.summary.Last = at
		}
	}
	for _, passingCheck := range s.PassingChecks {
		if !WithinWindow(passingCheck.When, start) {
			continue
		}
		recordRun(ensure(passingCheck.Agent, passingCheck.Target), passingCheck.Round, passingCheck.When, false)
	}
	for _, failedCheck := range s.FailedChecks {
		if !WithinWindow(failedCheck.When, start) {
			continue
		}
		item := ensure(failedCheck.Agent, failedCheck.Target, failedCheck.Finding.Target)
		previousLast := item.summary.Last
		recordRun(item, failedCheck.Round, failedCheck.When, true)
		item.summary.FailCount++
		if failedCheck.When.After(previousLast) || item.summary.LatestCause == "" {
			item.summary.LatestCause = FailureHotspotCause(failedCheck.Finding)
			item.summary.LatestFinding = failedCheck.Finding
		}
	}
	rows := make([]FailureHotspotSummary, 0, len(aggregates))
	for _, item := range aggregates {
		if item.summary.FailCount == 0 {
			continue
		}
		item.summary.RunCount = len(item.runs)
		item.summary.FailRunCount = 0
		runs := make([]failureHotspotRun, 0, len(item.runs))
		for _, run := range item.runs {
			runs = append(runs, run)
			if run.failed {
				item.summary.FailRunCount++
			}
		}
		sort.SliceStable(runs, func(i, j int) bool {
			return runs[i].Last.After(runs[j].Last)
		})
		for _, run := range runs {
			if !run.failed {
				break
			}
			item.summary.FailStreak++
		}
		rows = append(rows, item.summary)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].FailStreak != rows[j].FailStreak {
			return rows[i].FailStreak > rows[j].FailStreak
		}
		leftRate := 0
		if rows[i].RunCount > 0 {
			leftRate = rows[i].FailRunCount * 1000 / rows[i].RunCount
		}
		rightRate := 0
		if rows[j].RunCount > 0 {
			rightRate = rows[j].FailRunCount * 1000 / rows[j].RunCount
		}
		if leftRate != rightRate {
			return leftRate > rightRate
		}
		if rows[i].FailCount != rows[j].FailCount {
			return rows[i].FailCount > rows[j].FailCount
		}
		return rows[i].Last.After(rows[j].Last)
	})
	return rows
}

// FilteredFailureHotspots returns hotspot summaries matching queryValue.
func (s State) FilteredFailureHotspots(queryValue string) []FailureHotspotSummary {
	query := NormalizedSearchQuery(queryValue)
	rows := s.FailureHotspots()
	if query == "" {
		return rows
	}
	filtered := rows[:0]
	for _, row := range rows {
		if FailureHotspotSummaryMatches(row, query) {
			filtered = append(filtered, row)
		}
	}
	return filtered
}

// WithinWindow reports whether at is non-zero and not before start.
func WithinWindow(at time.Time, start time.Time) bool {
	if at.IsZero() {
		return false
	}
	return !at.Before(start)
}

// FailureHotspotRunKey returns a stable run key, falling back to timestamp when
// older events do not carry a round number.
func FailureHotspotRunKey(round uint64, at time.Time) string {
	if round > 0 {
		return fmt.Sprint(round)
	}
	return at.Format(time.RFC3339Nano)
}

// FailureHotspotCause returns the operator-facing cause used for hotspot rows.
func FailureHotspotCause(finding watch.Finding) string {
	cause := FirstNonEmpty(
		finding.Message,
		strings.TrimSpace(FirstNonEmpty(finding.Check, "check")+" "+FirstNonEmpty(finding.Metric, "status")+"="+FirstNonEmpty(finding.Observed, "-")),
		finding.Check,
	)
	return SanitizeLogText(cause)
}

// FilteredFailedCheckSummaries returns failed summaries matching queryValue.
func (s State) FilteredFailedCheckSummaries(queryValue string) []FailedCheckSummary {
	rows := s.FailedCheckSummaries()
	query := NormalizedSearchQuery(queryValue)
	if query == "" {
		return rows
	}
	filtered := rows[:0]
	for _, row := range rows {
		if FailedCheckSummaryMatches(row, query) {
			filtered = append(filtered, row)
		}
	}
	return filtered
}

// FailedCheckSummaryIndex returns the current row index for a finding summary.
// Agent is accepted for call-site symmetry; summary identity is target/finding
// based so multi-agent failures aggregate into one row when appropriate.
func (s State) FailedCheckSummaryIndex(agent watch.AgentSnapshot, target watch.TargetSnapshot, finding watch.Finding) int {
	key := FailedCheckSummaryKey(target, finding)
	for i, item := range s.FailedCheckSummaries() {
		if FailedCheckSummaryKey(item.Target, item.Finding) == key {
			return i
		}
	}
	return Max(0, len(s.FailedCheckSummaries())-1)
}

// NormalizedSearchQuery trims and lowercases a table filter query.
func NormalizedSearchQuery(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

// PassingCheckSummaryMatches reports whether row contains query.
func PassingCheckSummaryMatches(row PassingCheckSummary, query string) bool {
	fields := []string{
		row.Agent.DisplayName(),
		row.Agent.DeviceModel,
		row.Agent.Name,
		row.Target.Name,
		row.Target.SSID,
		row.Target.BSSID,
		row.Target.Band,
		row.Step.Name,
		DisplayCheckName(row.Step.Name),
		row.Step.Type,
		DisplayCheckName(row.Step.Type),
		row.Step.Operation,
		row.Step.Status,
		DurationLabel(row.Duration),
		fmt.Sprint(row.Count),
	}
	return FieldsContainQuery(fields, query)
}

// FailedCheckSummaryMatches reports whether row contains query.
func FailedCheckSummaryMatches(row FailedCheckSummary, query string) bool {
	finding := row.Finding
	fields := []string{
		row.Target.Name,
		row.Target.SSID,
		row.Target.BSSID,
		row.Target.Band,
		finding.Target,
		finding.Check,
		DisplayCheckName(finding.Check),
		finding.Metric,
		finding.Observed,
		finding.Expected,
		finding.Message,
		fmt.Sprint(row.Count),
	}
	return FieldsContainQuery(fields, query)
}

// FailureHotspotSummaryMatches reports whether row contains query.
func FailureHotspotSummaryMatches(row FailureHotspotSummary, query string) bool {
	finding := row.LatestFinding
	fields := []string{
		AgentLabel(row.Agent),
		AgentKey(row.Agent),
		row.Target.Name,
		row.Target.SSID,
		row.Target.BSSID,
		row.Target.Band,
		row.LatestCause,
		finding.Target,
		finding.Check,
		DisplayCheckName(finding.Check),
		finding.Metric,
		finding.Observed,
		finding.Expected,
		finding.Message,
		fmt.Sprint(row.FailCount),
		fmt.Sprint(row.FailRunCount),
		fmt.Sprint(row.RunCount),
		fmt.Sprint(row.FailStreak),
	}
	return FieldsContainQuery(fields, query)
}

// FieldsContainQuery reports whether any field contains query.
func FieldsContainQuery(fields []string, query string) bool {
	if query == "" {
		return true
	}
	for _, field := range fields {
		if strings.Contains(strings.ToLower(field), query) {
			return true
		}
	}
	return false
}
