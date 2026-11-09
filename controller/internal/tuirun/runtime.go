package tuirun

import (
	"context"
	"fmt"
	"time"

	"github.com/mi2428/dropcheck-agent/controller/internal/command"
	"github.com/mi2428/dropcheck-agent/controller/internal/control"
	"github.com/mi2428/dropcheck-agent/controller/internal/controlpb"
	"github.com/mi2428/dropcheck-agent/controller/internal/runner"
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

// Plan is private runtime input; Compile freezes operations and resolves secrets
// before Execute can perform any device operation.
type Plan struct {
	Name          string
	RoundInterval time.Duration
	Networks      []Network
	Checks        []Check
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
	ObserveFailureCause(context.Context, control.AgentInfo, FailureCauseContext, func(string)) func()
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

// OperationResult preserves compound acquisitions without retaining the dispatched
// command. Raw is the typed view used by expectation matchers.
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

type CheckInfo struct {
	ID, Name, Type, Operation string
	Required                  bool
	Policy                    Policy
	OperationTimeout          time.Duration
	Operations                []string // Actual constituent acquisitions per attempt.
	Destination, Traffic      string   // Per-attempt network destination/cost; policy bounds repetition.
	Expectations              []string
}

func (c CheckInfo) DisplayName() string { return c.Name }

// Target is a credential-free preview, also used by reducers and the TUI.
type Target struct {
	ID, Name, ShortName, Agent, SSID, BSSID, NetworkID, Band string
	DisconnectAfter, ForgetAfter                             *bool
	Checks                                                   []CheckInfo
	Connect, Wait                                            *CheckInfo
	BoundAgentIDs                                            []string
}

func (t Target) DisplayName() string { return t.Name }

type Preview struct {
	Name          string
	RoundInterval time.Duration
	Targets       []Target
	Checks        []CheckInfo
	Agents        []AgentSnapshot
}

type compiledStep struct {
	id               string
	typeName         string
	operationTimeout time.Duration
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
	name          string
	roundInterval time.Duration
	targets       []compiledTarget
	agents        []control.AgentInfo
	secrets       []string
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
