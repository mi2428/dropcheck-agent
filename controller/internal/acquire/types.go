// Package acquire owns atomic/compound Android acquisitions and typed evidence
// validation. Callers supply policy, presentation, session lifetime and cleanup.
package acquire

import (
	"context"
	"time"

	"github.com/mi2428/dropcheck-agent/controller/internal/command"
	"github.com/mi2428/dropcheck-agent/controller/internal/control"
	"github.com/mi2428/dropcheck-agent/controller/internal/controlpb"
	"github.com/mi2428/dropcheck-agent/controller/internal/runner"
)

// Runner executes an operation on the supplied pinned connection. Implementations
// must honor ctx and must not silently select a replacement agent session.
type Runner interface {
	Run(context.Context, control.AgentInfo, command.Operation) (runner.Result, error)
}

// Result is owned acquisition data, without tui scope or retry/policy state.
type Result struct {
	// Raw is the final or compound payload; nil means no terminal result.
	Raw *controlpb.CommandResult
	// Parts preserves attempted acquisitions in order, with independent payloads.
	Parts []Part
	// Options is the owned local command context needed by Shell presentation.
	Options command.Options
	// Findings contains only payload/hop evidence checks, without policy ownership.
	Findings []Finding
}

// Part is one attempted acquisition. Identity is copied from the pinned input,
// never re-resolved from an agent ID after the operation completes.
type Part struct {
	// AgentID and SessionID identify the pinned connection; injected runners may
	// leave them empty. Connected is the controller's acceptance time for it.
	AgentID, SessionID string
	Connected          time.Time
	// Name and CommandID identify the operation and its transport request.
	Name, CommandID string
	// Started/Ended are host times, not the agent's Raw.ElapsedMs duration.
	Started, Ended time.Time
	// Raw is a clone of the acquired payload; nil means no terminal payload.
	Raw *controlpb.CommandResult
	// Error records transport error text. Execute also returns the original error.
	Error string
}

// Finding is a typed acquisition check, not a target/check policy verdict.
// Missing evidence is distinct from an observed zero, false or empty value.
type Finding struct {
	// Metric is the stable evidence/assertion name.
	Metric string
	// Observed/Expected are descriptions of the evidence and constraint.
	Observed, Expected string
	// Passed reports a match; Missing marks unavailable/inconsistent evidence.
	Passed, Missing bool
	// ObservedValue/ExpectedValue preserve typed evidence when supplied.
	ObservedValue, ExpectedValue any
	// Message gives additional acquisition context, not sanitized output.
	Message string
}
