package harness

import "google.golang.org/protobuf/proto"

func snapshotValue(value any) any {
	switch v := value.(type) {
	case []string:
		return append([]string(nil), v...)
	case []any:
		copy := make([]any, len(v))
		for i, item := range v {
			copy[i] = snapshotValue(item)
		}
		return copy
	case map[string]any:
		copy := make(map[string]any, len(v))
		for key, item := range v {
			copy[key] = snapshotValue(item)
		}
		return copy
	case proto.Message:
		return proto.Clone(v)
	}
	return value
}

func snapshotFindings(findings []Finding) []Finding {
	copy := append([]Finding(nil), findings...)
	for i := range copy {
		copy[i].ObservedValue, copy[i].ExpectedValue = snapshotValue(copy[i].ObservedValue), snapshotValue(copy[i].ExpectedValue)
	}
	return copy
}

func snapshotResult(result OperationResult) OperationResult {
	result.Raw = cloneRaw(result.Raw)
	result.Parts = snapshotParts(result.Parts)
	result.Options.TracerouteRequiredHops = append([]string(nil), result.Options.TracerouteRequiredHops...)
	result.Findings = snapshotFindings(result.Findings)
	return result
}

func snapshotParts(parts []OperationRecord) []OperationRecord {
	copy := append([]OperationRecord(nil), parts...)
	for i := range copy {
		copy[i].Raw = cloneRaw(copy[i].Raw)
	}
	return copy
}

func snapshotAttempt(attempt Attempt) Attempt {
	attempt.Result = snapshotResult(attempt.Result)
	attempt.Findings = snapshotFindings(attempt.Findings)
	return attempt
}

func snapshotStep(step StepReport) StepReport {
	step.Attempts = append([]Attempt(nil), step.Attempts...)
	for i := range step.Attempts {
		step.Attempts[i] = snapshotAttempt(step.Attempts[i])
	}
	return step
}

func snapshotEvent(event Event) Event {
	if event.Attempt != nil {
		attempt := snapshotAttempt(*event.Attempt)
		event.Attempt = &attempt
	}
	if event.Report != nil {
		report := snapshotStep(*event.Report)
		event.Report = &report
	}
	if event.Finding != nil {
		finding := snapshotFindings([]Finding{*event.Finding})[0]
		event.Finding = &finding
	}
	event.Agents = append([]AgentProgress(nil), event.Agents...)
	return event
}

func snapshotReport(report Report) Report {
	report.Steps = append([]StepReport(nil), report.Steps...)
	for i := range report.Steps {
		report.Steps[i] = snapshotStep(report.Steps[i])
	}
	report.Cleanup = snapshotParts(report.Cleanup)
	report.Problems = append([]Problem(nil), report.Problems...)
	report.Agents = append([]AgentProgress(nil), report.Agents...)
	return report
}
