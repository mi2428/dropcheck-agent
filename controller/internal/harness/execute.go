package harness

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"dropcheck/controller/internal/command"
	"dropcheck/controller/internal/control"
	"dropcheck/controller/internal/controlpb"
)

const cleanupTimeout = 5 * time.Second
const deliveryTimeout = time.Second

type execution struct {
	plan        *CompiledPlan
	runner      OperationRunner
	opts        ExecuteOptions
	controls    *Controls
	ctx         context.Context
	stop        context.CancelCauseFunc
	mu, eventMu sync.Mutex
	report      Report
	errors      []executionError
	seq         uint64
	now         func() time.Time
	sleep       func(context.Context, time.Duration) error
}

type executionError struct {
	round uint64
	err   error
}

type redactedError struct {
	cause error
	text  string
}

func (e redactedError) Error() string { return e.text }
func (e redactedError) Unwrap() error { return e.cause }

// Execute is the only scenario loop, shared by Go tests, YAML, TUI and profiles.
// It always returns the report, including when transport, cleanup or sinks fail.
func Execute(ctx context.Context, plan *CompiledPlan, r OperationRunner, opts ExecuteOptions) (Report, error) {
	return execute(ctx, plan, r, opts, time.Now, sleepContext)
}

func execute(ctx context.Context, plan *CompiledPlan, r OperationRunner, opts ExecuteOptions, now func() time.Time, sleep func(context.Context, time.Duration) error) (Report, error) {
	if plan == nil || r == nil || opts.Loop && opts.Rounds != 0 || !opts.Loop && opts.Rounds == 0 || opts.RoundInterval < 0 {
		return Report{}, fmt.Errorf("invalid execution options")
	}
	if len(plan.targets) == 0 || len(plan.agents) == 0 {
		return Report{}, fmt.Errorf("compiled plan has no selected targets or agents")
	}
	var attempts uint64
	for _, target := range plan.targets {
		for _, step := range append([]compiledStep{target.connect, target.wait}, target.checks...) {
			if step.id == "" {
				continue
			}
			samples := uint64(1)
			if window := step.policy.Eventually + step.policy.StableFor; window > 0 {
				samples += uint64(window/step.policy.Interval) + 1
			}
			count, countErr := checkedProduct(uint64(step.policy.Attempts), uint64(step.policy.Repeat), samples, uint64(len(target.agents)))
			if countErr != nil {
				return Report{}, countErr
			}
			total, sumErr := checkedSum(attempts, count)
			if sumErr != nil || total > maxRetainedAttempts {
				return Report{}, fmt.Errorf("plan exceeds retained attempt budget")
			}
			attempts = total
		}
	}
	if !opts.Loop && opts.Rounds > maxRetainedAttempts/max(uint64(1), attempts) {
		return Report{}, fmt.Errorf("finite rounds exceed retained report budget")
	}
	for i, agent := range plan.agents {
		for _, other := range plan.agents[:i] {
			if agentKey(agent) == agentKey(other) {
				return Report{}, fmt.Errorf("duplicate agent identity")
			}
		}
	}
	runID, err := control.RandomHex(8)
	if err != nil {
		return Report{}, err
	}
	runCtx, stop := context.WithCancelCause(ctx)
	defer stop(nil)
	c := opts.Controls
	if c == nil {
		c = NewControls()
	}
	e := &execution{plan: plan, runner: r, opts: opts, controls: c, ctx: runCtx, stop: stop, now: now, sleep: sleep, report: Report{RunID: runID, Started: now(), State: "running", Outcome: OutcomePass}}
	for _, agent := range plan.agents {
		e.report.Agents = append(e.report.Agents, AgentProgress{Agent: AgentSnapshotFromInfo(agent), State: "pending", Outcome: OutcomePass})
	}
	if err := c.bind(plan, stop, e.emit); err != nil {
		return Report{}, err
	}
	defer c.finish()
	_ = e.emit(Event{Kind: EventWatchStarted, Status: "running"})
	for round := uint64(1); runCtx.Err() == nil && (opts.Loop || round <= opts.Rounds); round++ {
		if round > 1 {
			c.nextRound()
		}
		if opts.Loop && round > 2 {
			e.evictBefore(round - 1)
		}
		_ = e.emit(Event{Kind: EventRoundStarted, Round: round, Status: "running"})
		var workers sync.WaitGroup
		for _, agent := range plan.agents {
			workers.Add(1)
			go func() { defer workers.Done(); e.runAgentRound(agent, round) }()
		}
		// One common round barrier: all selected agent scopes, including cleanup,
		// terminate before any agent may begin the next round.
		workers.Wait()
		e.mu.Lock()
		progress := append([]AgentProgress(nil), e.report.Agents...)
		outcome := OutcomePass
		for _, agent := range progress {
			outcome = combineOutcome(outcome, agent.Outcome)
		}
		e.mu.Unlock()
		_ = e.emit(Event{Kind: EventRoundFinished, Round: round, Status: outcomeStatus(outcome), Agents: progress})
		if runCtx.Err() != nil || !opts.Loop && round == opts.Rounds {
			break
		}
		if err := sleep(runCtx, opts.RoundInterval); err != nil {
			break
		}
	}
	e.mu.Lock()
	e.report.Ended = now()
	e.report.State = "completed"
	if runCtx.Err() != nil {
		e.report.State = "stopped"
		e.report.Outcome = combineOutcome(e.report.Outcome, CanceledOutcome)
	}
	if len(e.report.Problems) > 0 {
		e.report.State = "failed"
		e.report.Outcome = FailOutcome
	}
	e.mu.Unlock()
	_ = e.emit(Event{Kind: EventRunEnding, Status: "finishing", Message: "delivery finalization pending"})
	var remaining []Sink
	for _, sink := range opts.Sinks {
		if closer, ok := sink.(interface{ Close(context.Context) error }); ok {
			closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
			closeErr := closer.Close(closeCtx)
			cancel()
			if closeErr != nil {
				e.problem("delivery", Scope{Kind: ScopeRun}, 0, closeErr)
			}
		} else {
			remaining = append(remaining, sink)
		}
	}
	e.opts.Sinks = remaining
	e.mu.Lock()
	if len(e.report.Problems) > 0 {
		e.report.State = "failed"
		e.report.Outcome = FailOutcome
	}
	state := e.report.State
	e.mu.Unlock()
	_ = e.emit(Event{Kind: EventRunFinished, Status: state})
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.report.Problems) > 0 {
		e.report.State, e.report.Outcome = "failed", FailOutcome
	}
	var failures []error
	for _, failure := range e.errors {
		failures = append(failures, failure.err)
	}
	return snapshotReport(e.report), errors.Join(failures...)
}

func (e *execution) emit(event Event) error {
	e.eventMu.Lock()
	defer e.eventMu.Unlock()
	e.seq++
	event.Seq, event.RunID, event.Plan = e.seq, e.report.RunID, e.plan.redact(e.plan.name)
	if event.Time.IsZero() {
		event.Time = e.now()
	}
	if event.Scope.AgentID != "" && event.Agent.ID == "" {
		for _, agent := range e.plan.agents {
			if agentKey(agent) == event.Scope.AgentID {
				event.Agent = AgentSnapshotFromInfo(agent)
				break
			}
		}
	}
	event.Message = e.plan.redact(event.Message)
	event.Target.Name, event.Target.ShortName, event.Target.SSID, event.Target.BSSID = e.plan.redact(event.Target.Name), e.plan.redact(event.Target.ShortName), e.plan.redact(event.Target.SSID), e.plan.redact(event.Target.BSSID)
	event.Step.Name, event.Step.Message, event.Step.Error = e.plan.redact(event.Step.Name), e.plan.redact(event.Step.Message), e.plan.redact(event.Step.Error)
	var failures []error
	for _, sink := range e.opts.Sinks {
		if sink == nil {
			continue
		}
		deliveryCtx, cancel := context.WithTimeout(context.WithoutCancel(e.ctx), deliveryTimeout)
		failures = append(failures, sink.Emit(deliveryCtx, snapshotEvent(event)))
		cancel()
	}
	err := errors.Join(failures...)
	if err != nil {
		e.problem("delivery", event.Scope, event.Round, err)
		e.stop(err)
	}
	return err
}

func (e *execution) problem(kind string, scope Scope, round uint64, err error) {
	if err == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.report.Problems = append(e.report.Problems, Problem{Kind: kind, Scope: scope, Round: round, Message: e.plan.redact(err.Error())})
	e.errors = append(e.errors, executionError{round: round, err: redactedError{cause: err, text: e.plan.redact(err.Error())}})
	e.report.Outcome = FailOutcome
}

func (e *execution) progress(agent control.AgentInfo, round uint64, state, phase string, outcome Outcome) {
	phase = e.plan.redact(phase)
	e.mu.Lock()
	defer e.mu.Unlock()
	for i := range e.report.Agents {
		if agentKey(agent) == firstNonEmpty(e.report.Agents[i].Agent.ID, "agent/0") {
			p := &e.report.Agents[i]
			if p.Round != round {
				p.Outcome = OutcomePass
				p.Counts = Counts{}
			}
			p.Round, p.State, p.Phase = round, state, phase
			p.Outcome = combineOutcome(p.Outcome, outcome)
			return
		}
	}
}

func (e *execution) runAgentRound(agent control.AgentInfo, round uint64) {
	e.progress(agent, round, "running", "preflight", OutcomePass)
	var targets []compiledTarget
	for _, target := range e.plan.targets {
		for _, bound := range target.agents {
			if agentKey(bound) == agentKey(agent) {
				targets = append(targets, target)
				break
			}
		}
	}
	unsupported := map[string]bool{}
	for _, target := range targets {
		if target.network.band == "" || target.network.band == "all" {
			continue
		}
		s, _ := compileStep(step{name: "device capabilities", operation: command.WifiCapabilitiesOperation()}, "capabilities", "capabilities")
		report := e.check(agent, compiledTarget{network: target.network, preview: Target{ID: "preflight", Name: "preflight"}}, s, round)
		if report.Outcome == OutcomePass && len(report.Attempts) > 0 {
			caps := report.Attempts[len(report.Attempts)-1].Result.Raw.GetWifiCapabilities()
			for _, band := range caps.GetUnsupportedBands() {
				unsupported[normalizeBand(band)] = true
			}
		}
		break
	}
	rotationOK := map[string]bool{}
	for _, target := range targets {
		if target.network.rotation != "per_round" {
			continue
		}
		key := target.network.ssid
		if _, done := rotationOK[key]; done {
			continue
		}
		rotationOK[key] = e.rotate(agent, target, round, "before_round")
	}
	for _, target := range targets {
		e.progress(agent, round, "running", target.preview.Name, OutcomePass)
		if unsupported[normalizeBand(target.network.band)] {
			e.skipTarget(agent, target, round, SkipUnsupported, "band is explicitly unsupported")
			continue
		}
		if target.network.rotation == "per_round" && !rotationOK[target.network.ssid] {
			e.skipTarget(agent, target, round, SkipPrerequisite, "MAC rotation failed")
			continue
		}
		e.runTarget(agent, target, round)
	}
	seen := map[string]bool{}
	for _, target := range targets {
		if target.network.rotation == "per_round" && !seen[target.network.ssid] {
			seen[target.network.ssid] = true
			e.rotate(agent, target, round, "after_round")
		}
	}
	e.progress(agent, round, "completed", "idle", OutcomePass)
}

func (e *execution) runTarget(agent control.AgentInfo, target compiledTarget, round uint64) {
	scope := Scope{Kind: ScopeTarget, AgentID: agentKey(agent), TargetID: target.preview.ID}
	_ = e.emit(Event{Kind: EventTargetStarted, Scope: scope, Round: round, Target: snapshotTarget(target.preview), Status: "running"})
	if target.network.rotation == "per_target" && !e.rotate(agent, target, round, "before_target") {
		e.skipTarget(agent, target, round, SkipPrerequisite, "MAC rotation failed")
		return
	}
	attempted := false
	outcome := OutcomePass
	defer func() {
		if attempted {
			outcome = combineOutcome(outcome, e.cleanup(agent, target, round))
		}
		e.progress(agent, round, "running", target.preview.Name, outcome)
		_ = e.emit(Event{Kind: EventTargetFinished, Scope: scope, Round: round, Target: snapshotTarget(target.preview), Status: outcomeStatus(outcome)})
	}()
	ready := true
	if target.connect.id != "" {
		connect := e.checkAttempted(agent, target, target.connect, round, &attempted)
		outcome = combineOutcome(outcome, connect.Outcome)
		ready = connect.Outcome == OutcomePass
	}
	if ready && target.wait.id != "" {
		wait := e.check(agent, target, target.wait, round)
		outcome = combineOutcome(outcome, wait.Outcome)
		ready = wait.Outcome == OutcomePass
	}
	for _, check := range target.checks {
		if !ready {
			e.skippedStep(agent, target, check, round, SkipPrerequisite, "required prerequisite did not pass")
			continue
		}
		report := e.check(agent, target, check, round)
		outcome = combineOutcome(outcome, report.Outcome)
		if report.Outcome != OutcomePass && check.required {
			ready = false
		}
		if report.Outcome == CanceledOutcome || report.SkipReason == SkipOperator {
			ready = false
		}
	}
}

func (e *execution) check(agent control.AgentInfo, target compiledTarget, s compiledStep, round uint64) StepReport {
	return e.checkAttempted(agent, target, s, round, nil)
}

func (e *execution) checkAttempted(agent control.AgentInfo, target compiledTarget, s compiledStep, round uint64, attempted *bool) StepReport {
	scope := Scope{Kind: ScopeCheck, AgentID: agentKey(agent), TargetID: target.preview.ID, CheckID: s.id}
	report := StepReport{Scope: scope, Round: round, Name: s.name, Outcome: OutcomePass}
	stepCtx, finish := e.controls.context(e.ctx, scope)
	defer finish()
	policyCtx, cancel := context.WithTimeout(stepCtx, s.policy.Timeout)
	defer cancel()
	_ = e.emit(Event{Kind: EventStepStarted, Scope: scope, Round: round, Target: snapshotTarget(target.preview), Step: stepSnapshot(s, "running"), Status: "running"})
	var terminalErrors []error
	for repeat := uint32(1); repeat <= s.policy.Repeat; repeat++ {
		var lastError error
		eventualDeadline := e.now().Add(s.policy.Eventually)
		var stableDeadline time.Time
		for sample := uint32(1); ; sample++ {
			last := MissingOutcome
			for number := uint32(1); number <= s.policy.Attempts; number++ {
				if err := e.controls.wait(policyCtx, scope); err != nil {
					last, report.Reason = contextOutcome(err), err.Error()
					break
				}
				started := e.now()
				attemptCtx := policyCtx
				stop := func() {}
				if stableDeadline.IsZero() && s.policy.Eventually > 0 {
					remaining := eventualDeadline.Sub(e.now())
					if remaining <= 0 {
						last, report.Reason = FailOutcome, "eventual deadline exceeded"
						break
					}
					attemptCtx, stop = context.WithTimeout(policyCtx, remaining)
				}
				if attempted != nil {
					*attempted = true
				}
				var result OperationResult
				var err error
				if s.gateway != nil {
					result, err = executeGateway(attemptCtx, e.runner, agent, target.network, *s.gateway)
				} else {
					result, err = ExecuteOperation(attemptCtx, e.runner, agent, s.operation)
				}
				if err == nil && attemptCtx.Err() != nil {
					err = attemptCtx.Err()
				}
				lastError = err
				stop()
				findings, outcome, reason := evaluateAttempt(target.network, s, result, err, context.Cause(stepCtx))
				for i := range result.Parts {
					result.Parts[i].Scope, result.Parts[i].Round = scope, round
				}
				last = outcome
				for i := range findings {
					findings[i].Target, findings[i].Check = e.plan.redact(target.preview.Name), e.plan.redact(s.name)
					findings[i].Metric = e.plan.redact(findings[i].Metric)
					findings[i].TargetID, findings[i].CheckID = target.preview.ID, s.id
					findings[i].Message = e.plan.redact(findings[i].Message)
					findings[i].Observed = e.plan.redact(findings[i].Observed)
					findings[i].Expected = e.plan.redact(findings[i].Expected)
					findings[i].ObservedValue, findings[i].ExpectedValue = e.plan.safeValue(findings[i].ObservedValue), e.plan.safeValue(findings[i].ExpectedValue)
				}
				result = e.plan.sanitizeResult(result)
				attempt := Attempt{Scope: scope, Round: round, Repeat: repeat, Sample: sample, Number: number, Started: started, Ended: e.now(), Outcome: outcome, Result: result, Findings: findings, Reason: e.plan.redact(reason)}
				report.Attempts = append(report.Attempts, attempt)
				_ = e.emit(Event{Kind: EventAttemptFinished, Scope: scope, Round: round, Target: snapshotTarget(target.preview), Attempt: &attempt, Status: outcomeStatus(outcome)})
				if last == OutcomePass || last == CanceledOutcome || last == SkipOutcome {
					break
				}
				if number < s.policy.Attempts {
					if err := e.sleep(policyCtx, s.policy.Delay); err != nil {
						last, report.Reason = contextOutcome(context.Cause(stepCtx)), err.Error()
						break
					}
				}
			}
			if last == CanceledOutcome || last == SkipOutcome || policyCtx.Err() != nil {
				report.Outcome = last
				if policyCtx.Err() != nil && last == OutcomePass {
					report.Outcome = FailOutcome
				}
				break
			}
			if stableDeadline.IsZero() {
				if last != OutcomePass {
					if s.policy.Eventually > 0 && e.now().Before(eventualDeadline) {
						if err := e.sleep(policyCtx, min(s.policy.Interval, eventualDeadline.Sub(e.now()))); err == nil {
							continue
						} else {
							report.Outcome, report.Reason = contextOutcome(context.Cause(stepCtx)), err.Error()
							break
						}
					}
					report.Outcome = last
					break
				}
				if s.policy.StableFor == 0 {
					break
				}
				stableDeadline = e.now().Add(s.policy.StableFor)
			} else if last != OutcomePass {
				report.Outcome = last
				break
			}
			if !e.now().Before(stableDeadline) {
				break
			}
			if err := e.sleep(policyCtx, min(s.policy.Interval, stableDeadline.Sub(e.now()))); err != nil {
				report.Outcome, report.Reason = contextOutcome(context.Cause(stepCtx)), err.Error()
				break
			}
		}
		if report.Outcome != OutcomePass && lastError != nil && !onlyCancellation(lastError) {
			terminalErrors = append(terminalErrors, lastError)
		}
		if report.Outcome == CanceledOutcome || report.Outcome == SkipOutcome {
			break
		}
	}
	if err := errors.Join(terminalErrors...); err != nil {
		e.problem("primary", scope, round, err)
	}
	if report.Outcome == SkipOutcome {
		report.SkipReason = SkipOperator
	}
	if report.Reason == "" && len(report.Attempts) > 0 {
		report.Reason = report.Attempts[len(report.Attempts)-1].Reason
	}
	e.record(agent, target, s, report)
	return report
}

func evaluateAttempt(network Network, s compiledStep, result OperationResult, err error, cause error) ([]Finding, Outcome, string) {
	if errors.Is(cause, errOperatorSkip) && (err == nil || onlyCancellation(err)) {
		return nil, SkipOutcome, cause.Error()
	}
	if (errors.Is(cause, errOperatorCancel) || errors.Is(cause, context.Canceled)) && (err == nil || onlyCancellation(err)) {
		return nil, CanceledOutcome, cause.Error()
	}
	findings := append([]Finding(nil), result.Findings...)
	if err != nil {
		findings = append(findings, Fail("transport", err.Error(), "operation result", err.Error()))
		return findings, FailOutcome, err.Error()
	}
	if result.Raw == nil {
		findings = append(findings, MissingFinding("payload", "<missing>", "typed result", "command result is missing"))
		return findings, MissingOutcome, "command result is missing"
	}
	outcome := OutcomePass
	if result.Raw.Status == controlpb.CommandResult_STATUS_CANCELED {
		return findings, CanceledOutcome, result.Raw.Message
	}
	if result.Raw.Status != controlpb.CommandResult_STATUS_OK {
		outcome = FailOutcome
		findings = append(findings, Fail("status", result.Raw.Status.String(), "STATUS_OK", result.Raw.Message))
	}
	valid := s.gateway != nil && result.Raw.GetPing() != nil || s.gateway == nil && payloadMatches(s.operation, result.Raw)
	if !valid {
		findings = append(findings, MissingFinding("payload", "<missing>", "expected typed payload", "wrong or missing payload"))
		return findings, combineOutcome(outcome, MissingOutcome), "wrong or missing payload"
	}
	for _, expectation := range s.expectations {
		// Assertions may inspect or mutate their private raw copy; report data is
		// never borrowed by user code.
		input := Result{Network: network, Check: s.name, Run: RunResult{Raw: cloneRaw(result.Raw)}}
		if len(result.Parts) > 0 {
			input.Run.CommandID = result.Parts[len(result.Parts)-1].CommandID
		}
		findings = append(findings, expectation.Evaluate(input)...)
	}
	for _, finding := range findings {
		if finding.Missing {
			outcome = combineOutcome(outcome, MissingOutcome)
		} else if !finding.Passed {
			outcome = FailOutcome
		}
	}
	return findings, outcome, result.Raw.Message
}

func (e *execution) record(agent control.AgentInfo, target compiledTarget, s compiledStep, report StepReport) {
	report.Name, report.Reason = e.plan.redact(report.Name), e.plan.redact(report.Reason)
	report = snapshotStep(report)
	e.mu.Lock()
	e.report.Steps = append(e.report.Steps, report)
	e.report.Outcome = combineOutcome(e.report.Outcome, report.Outcome)
	countOutcome(&e.report.Counts, report.Outcome)
	for i := range e.report.Agents {
		if agentKey(agent) == firstNonEmpty(e.report.Agents[i].Agent.ID, "agent/0") {
			countOutcome(&e.report.Agents[i].Counts, report.Outcome)
			e.report.Agents[i].Outcome = combineOutcome(e.report.Agents[i].Outcome, report.Outcome)
		}
	}
	e.mu.Unlock()
	step := stepSnapshot(s, outcomeStatus(report.Outcome))
	step.Outcome, step.SkipReason = report.Outcome, report.SkipReason
	step.Skipped = report.Outcome == SkipOutcome
	step.Message = report.Reason
	_ = e.emit(Event{Kind: EventStepFinished, Scope: report.Scope, Round: report.Round, Target: snapshotTarget(target.preview), Step: step, Report: &report, Status: outcomeStatus(report.Outcome), Message: report.Reason})
	if report.Outcome == FailOutcome || report.Outcome == MissingOutcome {
		for _, finding := range terminalFindings(report) {
			finding.Target, finding.Check = e.plan.redact(target.preview.Name), e.plan.redact(s.name)
			finding.TargetID, finding.CheckID = target.preview.ID, s.id
			_ = e.emit(Event{Kind: EventFinding, Scope: report.Scope, Round: report.Round, Target: snapshotTarget(target.preview), Step: step, Finding: &finding, Status: outcomeStatus(report.Outcome)})
		}
	}
}

// Each independent repeat/sample has its own retry terminal. Earlier failed
// attempts recovered within that sample are history, not permanent findings.
func terminalFindings(report StepReport) []Finding {
	last := map[[2]uint32]int{}
	for i, attempt := range report.Attempts {
		last[[2]uint32{attempt.Repeat, attempt.Sample}] = i
	}
	var findings []Finding
	for i, attempt := range report.Attempts {
		if last[[2]uint32{attempt.Repeat, attempt.Sample}] != i || attempt.Outcome != FailOutcome && attempt.Outcome != MissingOutcome {
			continue
		}
		for _, finding := range attempt.Findings {
			if !finding.Passed {
				findings = append(findings, finding)
			}
		}
	}
	if len(findings) == 0 && (report.Outcome == FailOutcome || report.Outcome == MissingOutcome) {
		finding := Fail("status", string(report.Outcome), "pass", report.Reason)
		finding.Missing = report.Outcome == MissingOutcome
		findings = append(findings, finding)
	}
	return findings
}

func (e *execution) skippedStep(agent control.AgentInfo, target compiledTarget, s compiledStep, round uint64, reason SkipReason, message string) {
	e.record(agent, target, s, StepReport{Scope: Scope{Kind: ScopeCheck, AgentID: agentKey(agent), TargetID: target.preview.ID, CheckID: s.id}, Round: round, Name: s.name, Outcome: SkipOutcome, SkipReason: reason, Reason: message})
}

func (e *execution) skipTarget(agent control.AgentInfo, target compiledTarget, round uint64, reason SkipReason, message string) {
	for _, s := range append([]compiledStep{target.connect, target.wait}, target.checks...) {
		if s.id != "" {
			e.skippedStep(agent, target, s, round, reason, message)
		}
	}
	_ = e.emit(Event{Kind: EventTargetFinished, Round: round, Agent: AgentSnapshotFromInfo(agent), Target: snapshotTarget(target.preview), Status: "skipped", Message: message})
}

func (e *execution) cleanup(agent control.AgentInfo, target compiledTarget, round uint64) Outcome {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(e.ctx), cleanupTimeout)
	defer cancel()
	var ops []command.Operation
	if target.network.disconnectAfter {
		ops = append(ops, command.WifiDisconnectOperation())
	}
	if target.network.forgetAfter {
		ops = append(ops, command.WifiForgetOperation(firstNonEmpty(target.network.ssid, target.network.bssid)))
	}
	outcome := OutcomePass
	var events []Event
	for _, op := range ops {
		scope := Scope{Kind: ScopeCheck, AgentID: agentKey(agent), TargetID: target.preview.ID, CheckID: "cleanup/" + op.Name}
		started := e.now()
		result, err := ExecuteOperation(ctx, e.runner, agent, op)
		s := compiledStep{id: scope.CheckID, typeName: "cleanup", step: step{name: strings.TrimPrefix(op.Name, "wifi."), operation: op}}
		_, state, message := evaluateAttempt(target.network, s, result, err, nil)
		result = e.plan.sanitizeResult(result)
		outcome = combineOutcome(outcome, state)
		if state != OutcomePass {
			if err == nil {
				err = fmt.Errorf("%s cleanup %s: %s", op.Name, state, message)
			}
			e.problem("cleanup", scope, round, err)
		}
		for _, part := range result.Parts {
			part.Round = round
			part.Scope = scope
			e.mu.Lock()
			e.report.Cleanup = append(e.report.Cleanup, part)
			e.mu.Unlock()
		}
		events = append(events, Event{Kind: EventStepStarted, Time: started, Scope: scope, Round: round, Target: snapshotTarget(target.preview), Step: stepSnapshot(s, "running"), Status: "running"}, Event{Kind: EventStepFinished, Time: e.now(), Scope: scope, Round: round, Target: snapshotTarget(target.preview), Step: stepSnapshot(s, outcomeStatus(state)), Status: outcomeStatus(state), Message: message})
	}
	// Result delivery never prevents disconnect/forget, including at shutdown.
	for _, event := range events {
		_ = e.emit(event)
	}
	return outcome
}

func (e *execution) rotate(agent control.AgentInfo, target compiledTarget, round uint64, phase string) bool {
	op := command.WifiForgetOperation(target.network.ssid)
	s, err := compileStep(step{name: "MAC rotation " + phase, operation: op, required: true}, "rotation/"+phase, "mac_rotation")
	if err != nil {
		e.problem("primary", Scope{Kind: ScopeTarget, AgentID: agentKey(agent), TargetID: target.preview.ID}, round, err)
		return false
	}
	report := e.check(agent, target, s, round)
	return report.Outcome == OutcomePass
}

func (e *execution) evictBefore(round uint64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.report.Steps = slices.DeleteFunc(e.report.Steps, func(step StepReport) bool { return step.Round < round })
	e.report.Cleanup = slices.DeleteFunc(e.report.Cleanup, func(part OperationRecord) bool { return part.Round < round })
	before := len(e.report.Problems)
	e.report.Problems = slices.DeleteFunc(e.report.Problems, func(problem Problem) bool { return problem.Round != 0 && problem.Round < round })
	e.report.EvictedProblems += uint64(before - len(e.report.Problems))
	e.errors = slices.DeleteFunc(e.errors, func(err executionError) bool { return err.round != 0 && err.round < round })
	e.report.EvictedRounds = round - 1
}

func stepSnapshot(s compiledStep, status string) StepSnapshot {
	return StepSnapshot{ID: s.id, Name: s.name, Type: s.typeName, Operation: s.operation.Name, Status: status}
}
func outcomeStatus(o Outcome) string {
	if o == OutcomePass {
		return "ok"
	}
	if o == FailOutcome {
		return "failed"
	}
	if o == SkipOutcome {
		return "skipped"
	}
	return string(o)
}
func contextOutcome(err error) Outcome {
	if errors.Is(err, errOperatorSkip) {
		return SkipOutcome
	}
	if errors.Is(err, errOperatorCancel) || errors.Is(err, context.Canceled) {
		return CanceledOutcome
	}
	return FailOutcome
}
func combineOutcome(a, b Outcome) Outcome {
	rank := map[Outcome]int{OutcomePass: 0, SkipOutcome: 1, CanceledOutcome: 2, MissingOutcome: 3, FailOutcome: 4}
	if rank[b] > rank[a] {
		return b
	}
	return a
}
func countOutcome(c *Counts, outcome Outcome) {
	switch outcome {
	case OutcomePass:
		c.Passed++
	case FailOutcome:
		c.Failed++
	case MissingOutcome:
		c.Missing++
	case SkipOutcome:
		c.Skipped++
	case CanceledOutcome:
		c.Canceled++
	}
}
func normalizeBand(band string) string { return strings.ReplaceAll(strings.ToLower(band), " ", "") }
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
func sleepContext(ctx context.Context, duration time.Duration) error {
	if duration <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func onlyCancellation(err error) bool {
	switch value := err.(type) {
	case interface{ Unwrap() []error }:
		if len(value.Unwrap()) == 0 {
			return false
		}
		for _, child := range value.Unwrap() {
			if !onlyCancellation(child) {
				return false
			}
		}
		return true
	case interface{ Unwrap() error }:
		return onlyCancellation(value.Unwrap())
	default:
		return err == context.Canceled || err == errOperatorCancel || err == errOperatorSkip
	}
}
