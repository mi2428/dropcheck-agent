package watchrun

import (
	"context"

	"github.com/mi2428/dropcheck-agent/controller/internal/acquire"
	"github.com/mi2428/dropcheck-agent/controller/internal/command"
	"github.com/mi2428/dropcheck-agent/controller/internal/control"
)

// executeOperation is the sole adapter from neutral acquisition to watch data.
// Target/check/round attribution is applied by the owning policy attempt/cleanup.
func executeOperation(ctx context.Context, r acquire.Runner, agent control.AgentInfo, op command.Operation) (OperationResult, error) {
	value, err := acquire.Execute(ctx, r, agent, op)
	result := OperationResult{Raw: value.Raw, Options: value.Options}
	for _, part := range value.Parts {
		id := part.AgentID
		if id == "" {
			id = agentKey(agent)
		}
		result.Parts = append(result.Parts, OperationRecord{Scope: Scope{Kind: ScopeAgent, AgentID: id}, Name: part.Name, CommandID: part.CommandID, Started: part.Started, Ended: part.Ended, Raw: part.Raw, Error: part.Error})
	}
	for _, f := range value.Findings {
		result.Findings = append(result.Findings, Finding{Metric: f.Metric, Observed: f.Observed, Expected: f.Expected, Passed: f.Passed, Missing: f.Missing, ObservedValue: f.ObservedValue, ExpectedValue: f.ExpectedValue, Message: f.Message})
	}
	return result, err
}
