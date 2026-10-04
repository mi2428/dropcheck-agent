package harness

import (
	"context"
	"fmt"
	"time"

	"dropcheck/controller/internal/command"
	"dropcheck/controller/internal/control"
	"dropcheck/controller/internal/controlpb"
	"dropcheck/controller/internal/runner"
)

// Policy is shared by every probe and prerequisite. Attempts includes the first
// attempt; Repeat creates independent samples. Timeout bounds the entire check.
type Policy struct {
	Attempts, Repeat                                uint32
	Delay, Eventually, StableFor, Interval, Timeout time.Duration
}

const maxRetainedAttempts = uint64(200000)

func checkedProduct(values ...uint64) (uint64, error) {
	product := uint64(1)
	for _, value := range values {
		if value != 0 && product > ^uint64(0)/value {
			return 0, fmt.Errorf("count overflow")
		}
		product *= value
	}
	return product, nil
}

func checkedSum(a, b uint64) (uint64, error) {
	if a > ^uint64(0)-b {
		return 0, fmt.Errorf("count overflow")
	}
	return a + b, nil
}

type Outcome string

const (
	OutcomePass     Outcome = "pass"
	FailOutcome     Outcome = "fail"
	MissingOutcome  Outcome = "missing"
	SkipOutcome     Outcome = "skip"
	CanceledOutcome Outcome = "canceled"
)

type SkipReason string

const (
	SkipUnsupported  SkipReason = "unsupported"
	SkipPrerequisite SkipReason = "prerequisite"
	SkipOperator     SkipReason = "operator"
)

// Plan is an authoring value; Compile freezes operations and resolves secrets
// before Execute can perform any device operation.
type Plan struct {
	Name          string
	RoundInterval time.Duration
	Networks      []Network
	Checks        []Check
}

type OperationRunner interface {
	Run(context.Context, control.AgentInfo, command.Operation) (runner.Result, error)
}

type FailureCauseContext struct {
	Round     uint64
	Target    Target
	Step      StepSnapshot
	Operation command.Operation
	Execution runner.Result
	Err       error
}

type FailureCauseCollector interface {
	FailureCause(context.Context, control.AgentInfo, FailureCauseContext) string
}
type FailureCauseMonitor interface {
	WatchFailureCause(context.Context, control.AgentInfo, FailureCauseContext, func(string)) func()
}

type Scope struct {
	Kind     string `json:"kind"`
	AgentID  string `json:"agent_id,omitempty"`
	TargetID string `json:"target_id,omitempty"`
	CheckID  string `json:"check_id,omitempty"`
}

type OperationRecord struct {
	Scope          Scope  `json:"scope"`
	Round          uint64 `json:"round,omitempty"`
	Name           string `json:"name"`
	CommandID      string `json:"command_id,omitempty"`
	Started, Ended time.Time
	Raw            *controlpb.CommandResult `json:"result,omitempty"`
	Error          string                   `json:"error,omitempty"`
}

// OperationResult preserves compound acquisitions without retaining a command
// or its credentials. Raw is the typed view used by the existing matchers.
type OperationResult struct {
	Raw      *controlpb.CommandResult
	Parts    []OperationRecord
	Options  command.Options
	Findings []Finding
}

type Attempt struct {
	Scope                  Scope  `json:"scope"`
	Round                  uint64 `json:"round"`
	Repeat, Sample, Number uint32
	Started, Ended         time.Time
	Outcome                Outcome         `json:"outcome"`
	Result                 OperationResult `json:"result"`
	Findings               []Finding       `json:"findings,omitempty"`
	Reason                 string          `json:"reason,omitempty"`
}

type StepReport struct {
	Scope      Scope      `json:"scope"`
	Round      uint64     `json:"round"`
	Name       string     `json:"name"`
	Outcome    Outcome    `json:"outcome"`
	SkipReason SkipReason `json:"skip_reason,omitempty"`
	Attempts   []Attempt  `json:"attempts,omitempty"`
	Reason     string     `json:"reason,omitempty"`
}

type Problem struct {
	Kind    string `json:"kind"`
	Scope   Scope  `json:"scope"`
	Round   uint64 `json:"round"`
	Message string `json:"message"`
}

type Counts struct {
	Passed, Failed, Missing, Skipped, Canceled uint64
}

type AgentProgress struct {
	Agent        AgentSnapshot
	Round        uint64
	State, Phase string
	Outcome      Outcome
	Counts       Counts
}

type Report struct {
	RunID           string `json:"run_id"`
	Started, Ended  time.Time
	State           string            `json:"state"`
	Outcome         Outcome           `json:"outcome"`
	Steps           []StepReport      `json:"steps"`
	Cleanup         []OperationRecord `json:"cleanup,omitempty"`
	Problems        []Problem         `json:"problems,omitempty"`
	Agents          []AgentProgress   `json:"agents"`
	Counts          Counts            `json:"counts"`
	EvictedRounds   uint64            `json:"evicted_rounds,omitempty"`
	EvictedProblems uint64            `json:"evicted_problems,omitempty"`
}

func (r Report) Passed() bool { return r.Outcome == OutcomePass && len(r.Problems) == 0 }

// Selection uses compiler-assigned ordinal IDs, never display names.
type Selection struct {
	AgentIDs, TargetIDs, CheckIDs []string
	// Scopes is exact agent-target-check selection; it cannot be mixed with
	// flat sets, whose explicit meaning is a Cartesian selection.
	Scopes []Scope
}

type CheckInfo struct {
	ID, Name, Type, Operation string
	Required                  bool
	Policy                    Policy
	OperationTimeout          time.Duration
	Destination, Traffic      string // Per-attempt network destination/cost; policy bounds repetition.
	Expectations              []string
}

func (c CheckInfo) DisplayName() string { return c.Name }

// Target is a credential-free preview, also used by reducers and the TUI.
type Target struct {
	ID, Name, ShortName, Agent, SSID, BSSID, Band string
	DisconnectAfter, ForgetAfter                  *bool
	Checks                                        []CheckInfo
	SecretPresent                                 bool
	Connect, Wait                                 *CheckInfo
	BoundAgentIDs                                 []string
	CapabilityPreflight                           []CapabilityPreflight
}

type CapabilityPreflight struct{ Operation, Band, State, Reason string }

func (t Target) DisplayName() string { return t.Name }

type Preview struct {
	Name    string
	Targets []Target
	Checks  []CheckInfo
	Agents  []AgentSnapshot
}

type compiledStep struct {
	id       string
	typeName string
	step
}

type compiledTarget struct {
	preview       Target
	network       Network
	connect, wait compiledStep
	checks        []compiledStep
	agents        []control.AgentInfo
}

type CompiledPlan struct {
	name    string
	targets []compiledTarget
	agents  []control.AgentInfo
	secrets []string
}

type ExecuteOptions struct {
	Rounds        uint64
	Loop          bool
	RoundInterval time.Duration
	Controls      *Controls
	Sinks         []Sink
}

func normalizePolicy(p Policy, operationTimeout time.Duration) (Policy, error) {
	if p.Attempts == 0 {
		p.Attempts = 1
	}
	if p.Repeat == 0 {
		p.Repeat = 1
	}
	for _, d := range []time.Duration{p.Delay, p.Eventually, p.StableFor, p.Interval, p.Timeout} {
		if d < 0 {
			return Policy{}, fmt.Errorf("policy durations must not be negative")
		}
	}
	if p.Interval == 0 {
		p.Interval = time.Second
	}
	window := p.Eventually + p.StableFor
	if window < p.Eventually {
		return Policy{}, fmt.Errorf("policy duration overflow")
	}
	samples := uint64(1)
	if window > 0 {
		samples += uint64(window/p.Interval) + 1
	}
	if uint64(p.Attempts) > maxRetainedAttempts/uint64(p.Repeat) || uint64(p.Attempts)*uint64(p.Repeat) > maxRetainedAttempts/samples {
		return Policy{}, fmt.Errorf("policy exceeds retained attempt budget")
	}
	if p.Timeout == 0 {
		// All policy loops have a finite, checked budget, even without a parent
		// deadline. One final sample may finish after the stability window.
		perAttempt := operationTimeout + p.Delay
		if perAttempt <= 0 || perAttempt < operationTimeout {
			return Policy{}, fmt.Errorf("invalid operation timeout")
		}
		max := uint64((1 << 63) - 1)
		if uint64(window) > max/uint64(p.Repeat) {
			return Policy{}, fmt.Errorf("policy duration overflow")
		}
		windows := uint64(window) * uint64(p.Repeat)
		n := uint64(p.Attempts)
		if n > max/uint64(p.Repeat) {
			return Policy{}, fmt.Errorf("policy count overflow")
		}
		n *= uint64(p.Repeat)
		if n > max/samples {
			return Policy{}, fmt.Errorf("policy count overflow")
		}
		n *= samples
		if n > (max-windows)/uint64(perAttempt) {
			return Policy{}, fmt.Errorf("policy duration overflow")
		}
		p.Timeout = time.Duration(n*uint64(perAttempt) + windows)
	}
	return p, nil
}
