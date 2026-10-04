package render

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"dropcheck/controller/internal/command"
	"dropcheck/controller/internal/controlpb"
	"dropcheck/controller/internal/harness"
	"dropcheck/controller/internal/pipeline"
)

// OperationResult consumes the shared acquisition contract; Parts never contain commands.
// Required acquisition failures are supplied by core, not reconstructed by the UI.
func OperationResult(agent string, result harness.OperationResult, format pipeline.Format, view Presentation) (string, error) {
	out := "Acquisition: ? (aggregate typed payload unavailable)\n"
	if result.Raw != nil {
		var err error
		out, err = CommandResult(agent, result.Raw, result.Options, format, view)
		if err != nil {
			return "", err
		}
	}
	if format == pipeline.FormatJSON {
		parts := make([]map[string]any, 0, len(result.Parts))
		for _, part := range result.Parts {
			value := map[string]any{"name": cleanDisplayCell(part.Name), "started": part.Started, "ended": part.Ended}
			if part.Error != "" {
				value["error"] = cleanDisplayCell(safePresentationText(part.Error))
			}
			if part.Raw != nil {
				raw, renderErr := CommandResult(agent, part.Raw, result.Options, format, view)
				if renderErr != nil {
					return "", renderErr
				}
				value["result"] = json.RawMessage(raw)
			}
			parts = append(parts, value)
		}
		value := map[string]any{"parts": parts, "findings": result.Findings}
		if result.Raw != nil {
			value["result"] = json.RawMessage(out)
		}
		data, marshalErr := json.MarshalIndent(value, "", "  ")
		if marshalErr != nil {
			return "", marshalErr
		}
		return string(data) + "\n", nil
	}
	var b strings.Builder
	b.WriteString(out)
	for _, part := range result.Parts {
		writeSection(&b, part.Name)
		if part.Raw != nil {
			writeRecord(&b, "Result", strings.ToUpper(resultStatus(part.Raw.GetStatus())))
		}
		if part.Raw != nil && part.Raw.GetStatus() == controlpb.CommandResult_STATUS_CANCELED {
			writeRecord(&b, "Reason", part.Raw.GetMessage())
		}
		writeRecord(&b, "Acquisition elapsed", acquisitionElapsed(part.Started, part.Ended))
		if part.Error != "" {
			writeRecord(&b, "Error", safePresentationText(part.Error))
		}
		if part.Raw != nil {
			text, renderErr := CommandResult(agent, part.Raw, result.Options, format, view)
			if renderErr != nil {
				return "", renderErr
			}
			b.WriteString(text)
		}
	}
	writeFindings(&b, result.Findings, true)
	return wrapPresentation(b.String(), view.Width), nil
}

// Report consumes the complete safe core report without execution or re-evaluation.
// Run/check/command identifiers appear only in detail; target ordinals identify scope.
func Report(report harness.Report, view Presentation) (string, error) {
	var b strings.Builder
	labels := reportLabels(report)
	fmt.Fprintf(&b, "Check  %s  %s\n", strings.ToUpper(string(report.Outcome)), acquisitionElapsed(report.Started, report.Ended))
	writeRecord(&b, "Overall outcome (core)", strings.ToUpper(string(report.Outcome)))
	writeRecord(&b, "Core passed", report.Passed())
	writeRecord(&b, "State", report.State)
	writeRecord(&b, "Counts", reportCounts(report.Counts))
	if view.Detail {
		writeRecord(&b, "Run", report.RunID)
	}
	// Problems and cleanup stay visible even when ordinary checks passed.
	for _, problem := range report.Problems {
		writeRecord(&b, "Problem", fmt.Sprintf("%s round=%d %s", problem.Kind, problem.Round, reportScope(problem.Scope, labels, view.Detail)))
		writeRecord(&b, "Reason", problem.Message)
	}
	writeSection(&b, "Cleanup")
	if len(report.Cleanup) == 0 {
		writeRecord(&b, "Cleanup", "none (no cleanup operation recorded)")
	}
	for _, cleanup := range report.Cleanup {
		writeRecord(&b, "Cleanup", fmt.Sprintf("%s round=%d %s", cleanup.Name, cleanup.Round, acquisitionRecordStatus(cleanup)))
		writeRecord(&b, "Acquisition elapsed", acquisitionElapsed(cleanup.Started, cleanup.Ended))
		if cleanup.Error != "" {
			writeRecord(&b, "Error", cleanup.Error)
		}
		if cleanup.Raw != nil && cleanup.Raw.GetMessage() != "" {
			writeRecord(&b, "Reason", cleanup.Raw.GetMessage())
		}
		if view.Detail && cleanup.Raw != nil {
			text, err := CommandResult("cleanup", cleanup.Raw, command.Options{}, pipeline.FormatText, view)
			if err != nil {
				return "", err
			}
			b.WriteString(text)
		}
	}
	for _, agent := range report.Agents {
		writeRecord(&b, "Agent", empty(agent.Agent.DisplayName(), agent.Agent.ID))
		writeRecord(&b, "Agent progress", fmt.Sprintf("round=%d state=%s phase=%s outcome=%s %s", agent.Round, agent.State, agent.Phase, strings.ToUpper(string(agent.Outcome)), reportCounts(agent.Counts)))
	}
	writeSection(&b, "Steps")
	for _, step := range report.Steps {
		writeRecord(&b, "Scope", fmt.Sprintf("%s round=%d", reportScope(step.Scope, labels, view.Detail), step.Round))
		writeRecord(&b, "Step", fmt.Sprintf("%s  %s", step.Name, strings.ToUpper(string(step.Outcome))))
		if step.SkipReason != "" {
			writeRecord(&b, "Skip reason", step.SkipReason)
		}
		if step.Reason != "" {
			writeRecord(&b, "Reason", step.Reason)
		}
		if len(step.Attempts) == 0 {
			if step.Outcome == harness.SkipOutcome {
				writeRecord(&b, "Time", "n/a (skipped)")
			} else {
				writeRecord(&b, "Time", "? (no attempt timeline recorded)")
			}
		}
		for _, attempt := range step.Attempts {
			writeRecord(&b, "Attempt", fmt.Sprintf("repeat=%d sample=%d attempt=%d %s %s", attempt.Repeat, attempt.Sample, attempt.Number, strings.ToUpper(string(attempt.Outcome)), acquisitionElapsed(attempt.Started, attempt.Ended)))
			if attempt.Reason != "" {
				writeRecord(&b, "Reason", attempt.Reason)
			}
			writeFindings(&b, attempt.Findings, view.Detail)
			for _, part := range attempt.Result.Parts {
				writeRecord(&b, "Acquisition", fmt.Sprintf("%s %s %s", part.Name, acquisitionRecordStatus(part), acquisitionElapsed(part.Started, part.Ended)))
				if part.Error != "" {
					writeRecord(&b, "Error", part.Error)
				}
				if part.Raw != nil && part.Raw.GetStatus() != controlpb.CommandResult_STATUS_OK {
					writeRecord(&b, "Reason", part.Raw.GetMessage())
				}
			}
			if view.Detail {
				text, err := OperationResult(reportScope(attempt.Scope, labels, true), attempt.Result, pipeline.FormatText, view)
				if err != nil {
					return "", err
				}
				b.WriteString(text)
			}
		}
	}
	if report.EvictedRounds > 0 {
		writeRecord(&b, "Omission", fmt.Sprintf("%d older rounds evicted by core retention", report.EvictedRounds))
	}
	if report.EvictedProblems > 0 {
		writeRecord(&b, "Omission", fmt.Sprintf("%d older problems evicted by core retention", report.EvictedProblems))
	}
	return wrapPresentation(b.String(), view.Width), nil
}

func acquisitionRecordStatus(record harness.OperationRecord) string {
	if record.Raw != nil && record.Raw.GetStatus() != controlpb.CommandResult_STATUS_OK {
		return strings.ToUpper(resultStatus(record.Raw.GetStatus()))
	}
	if record.Error != "" {
		return "ERROR"
	}
	if record.Raw == nil {
		return "MISSING (typed acquisition unavailable)"
	}
	return strings.ToUpper(resultStatus(record.Raw.GetStatus()))
}

func reportCounts(counts harness.Counts) string {
	return fmt.Sprintf("pass=%d fail=%d missing=%d skip=%d canceled=%d", counts.Passed, counts.Failed, counts.Missing, counts.Skipped, counts.Canceled)
}

func reportLabels(report harness.Report) map[string]string {
	labels := make(map[string]string)
	for _, progress := range report.Agents {
		labels["agent:"+progress.Agent.ID] = empty(progress.Agent.DisplayName(), progress.Agent.ID)
	}
	for _, step := range report.Steps {
		for _, attempt := range step.Attempts {
			for _, finding := range attempt.Findings {
				if finding.TargetID != "" && finding.Target != "" {
					labels["target:"+finding.TargetID] = finding.Target
				}
			}
		}
	}
	return labels
}

func reportScope(scope harness.Scope, labels map[string]string, detail bool) string {
	agentName := empty(labels["agent:"+scope.AgentID], scope.AgentID)
	target := empty(labels["target:"+scope.TargetID], scope.TargetID)
	parts := []string{scope.Kind}
	if agentName != "" {
		parts = append(parts, "agent="+agentName)
	}
	if target != "" {
		parts = append(parts, "target="+target)
	}
	if detail && scope.CheckID != "" {
		parts = append(parts, "check="+scope.CheckID)
	}
	return strings.Join(parts, " ")
}

func writeFindings(b *strings.Builder, findings []harness.Finding, includePassed bool) {
	for _, finding := range findings {
		if finding.Passed && !includePassed {
			continue
		}
		writeRecord(b, "Metric", finding.Metric)
		state := "FAIL"
		if finding.Passed {
			state = "PASS"
		} else if finding.Missing {
			state = "MISSING"
		}
		writeRecord(b, "Finding", state)
		writeRecord(b, "Observed", finding.Observed)
		if finding.ObservedValue != nil {
			writeRecord(b, "Typed observed value", fmt.Sprint(finding.ObservedValue))
		}
		writeRecord(b, "Expected", finding.Expected)
		if finding.ExpectedValue != nil {
			writeRecord(b, "Typed expected value", fmt.Sprint(finding.ExpectedValue))
		}
		if finding.Message != "" {
			writeRecord(b, "Reason", safePresentationText(finding.Message))
		}
	}
}

func acquisitionElapsed(started, ended time.Time) string {
	if started.IsZero() || ended.IsZero() || ended.Before(started) {
		return "?"
	}
	return ended.Sub(started).String()
}
