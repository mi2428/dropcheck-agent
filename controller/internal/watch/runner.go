package watch

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"dropcheck/controller/internal/command"
	"dropcheck/controller/internal/control"
	"dropcheck/controller/internal/controlpb"
	"dropcheck/controller/internal/runner"
)

// OperationRunner executes one controller operation for one Android agent.
type OperationRunner interface {
	Run(context.Context, control.AgentInfo, command.Operation) (runner.Result, error)
}

// FailureCauseContext describes the failed or in-flight operation being diagnosed.
type FailureCauseContext struct {
	Round     uint64
	Target    Target
	Step      StepSnapshot
	Operation command.Operation
	Execution runner.Result
	Err       error
}

// FailureCauseCollector collects a best-effort failure cause after an operation attempt.
type FailureCauseCollector interface {
	FailureCause(context.Context, control.AgentInfo, FailureCauseContext) string
}

// FailureCauseMonitor streams best-effort failure causes while an operation is running.
type FailureCauseMonitor interface {
	WatchFailureCause(context.Context, control.AgentInfo, FailureCauseContext, func(string)) func()
}

// RunOptions configures one watch runner.
type RunOptions struct {
	Pause        *PauseController
	Skip         *SkipController
	RoundBarrier *RoundBarrier
}

type operationAttempt struct {
	exec    runner.Result
	runErr  error
	skipped bool
}

// Operation retries are intentionally fixed for watch runs. The watch command is
// used against shared Wi-Fi infrastructure, where one failed probe is often less
// useful than a bounded retry that records the flake without failing the target.
const operationRetryLimit = 3

var checkExpectationPollInterval = time.Second

// Run executes plan rounds until ctx is canceled or an unrecoverable runner or sink error occurs.
func Run(ctx context.Context, plan Plan, opRunner OperationRunner, agent control.AgentInfo, sink Sink) error {
	return RunWithOptions(ctx, plan, opRunner, agent, sink, RunOptions{})
}

// RunWithOptions executes plan rounds with optional runner controls.
func RunWithOptions(ctx context.Context, plan Plan, opRunner OperationRunner, agent control.AgentInfo, sink Sink, opts RunOptions) error {
	if opRunner == nil {
		return fmt.Errorf("watch runner is nil")
	}
	agentSnapshot := snapshotAgent(agent)
	emit := func(event Event) error {
		event.Time = time.Now()
		event.Plan = plan.Name
		event.Agent = agentSnapshot
		if sink == nil {
			return nil
		}
		return sink.Emit(ctx, event)
	}
	if err := emit(Event{Kind: EventWatchStarted, Message: "watch started"}); err != nil {
		return err
	}
	bands, err := detectBandSupport(ctx, opRunner, agent)
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		if err := emit(Event{Kind: EventLog, Status: "warn", Message: "wifi capabilities unavailable: " + err.Error()}); err != nil {
			return err
		}
	}
	for round := uint64(1); ; round++ {
		if err := ctx.Err(); err != nil {
			return nil
		}
		if err := opts.Pause.Wait(ctx); err != nil {
			return nil
		}
		failed, err := runRound(ctx, plan, opRunner, agent, round, bands, opts.Pause, opts.Skip, opts.RoundBarrier, emit)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		if plan.RoundInterval > 0 {
			status := "ok"
			if failed > 0 {
				status = "failed"
			}
			if err := emit(Event{Kind: EventLog, Round: round, Status: status, Message: fmt.Sprintf("sleeping %s", plan.RoundInterval)}); err != nil {
				return err
			}
			if err := sleepContext(ctx, plan.RoundInterval); err != nil {
				return nil
			}
		}
	}
}

func snapshotAgent(agent control.AgentInfo) AgentSnapshot {
	return AgentSnapshotFromInfo(agent)
}

func runRound(ctx context.Context, plan Plan, opRunner OperationRunner, agent control.AgentInfo, round uint64, bands bandSupport, pause *PauseController, skip *SkipController, barrier *RoundBarrier, emit func(Event) error) (int, error) {
	started := time.Now()
	if err := emit(Event{Kind: EventRoundStarted, Round: round, Status: "running"}); err != nil {
		return 0, err
	}
	if err := runRoundMacRotation(ctx, plan, opRunner, agent, round, "before_round", pause, skip, emit); err != nil && !errors.Is(err, ErrSkipRequested) {
		return 0, err
	}
	failedTargets := 0
	skippedTargets := 0
	for _, target := range plan.Targets {
		if err := pause.Wait(ctx); err != nil {
			return failedTargets, err
		}
		result, err := runTarget(ctx, plan, opRunner, agent, round, target, bands, pause, skip, emit)
		if err != nil {
			return failedTargets, err
		}
		switch result {
		case targetSkipped:
			skippedTargets++
		case targetFailed:
			failedTargets++
		}
	}
	if err := runRoundMacRotation(ctx, plan, opRunner, agent, round, "after_round", pause, skip, emit); err != nil && !errors.Is(err, ErrSkipRequested) {
		return failedTargets, err
	}
	status := "ok"
	if failedTargets > 0 {
		status = "failed"
	}
	if err := barrier.Wait(ctx); err != nil {
		return failedTargets, err
	}
	return failedTargets, emit(Event{
		Kind:     EventRoundFinished,
		Round:    round,
		Status:   status,
		Message:  fmt.Sprintf("targets=%d failed=%d skipped=%d", len(plan.Targets), failedTargets, skippedTargets),
		Duration: time.Since(started).Milliseconds(),
	})
}

type targetResult int

const (
	targetPassed targetResult = iota
	targetFailed
	targetSkipped
)

func runTarget(ctx context.Context, plan Plan, opRunner OperationRunner, agent control.AgentInfo, round uint64, target Target, bands bandSupport, pause *PauseController, skip *SkipController, emit func(Event) error) (targetResult, error) {
	targetStart := time.Now()
	targetSnapshot := snapshotTarget(target)
	if message, ok := bands.skipReason(target.Band); ok {
		return targetSkipped, skipTarget(round, target, plan.Checks, targetStart, message, emit)
	}
	if err := emit(Event{Kind: EventTargetStarted, Round: round, Target: targetSnapshot, Status: "running"}); err != nil {
		return targetFailed, err
	}
	targetOK := true
	ready := true
	if target.macRotation() == macRotationPerTarget {
		ok, skipped, err := runMacRotationForget(ctx, opRunner, agent, round, target, "before_target", skip, emit)
		if err != nil {
			return targetFailed, err
		}
		if skipped {
			return targetSkipped, finishOperatorSkippedTarget(round, target, targetStart, emit)
		}
		if !ok {
			message := "mac rotation forget failed"
			if err := skipTargetPlanSteps(round, targetSnapshot, plan.Checks, message, emit); err != nil {
				return targetFailed, err
			}
			return targetFailed, emit(Event{
				Kind:     EventTargetFinished,
				Round:    round,
				Target:   targetSnapshot,
				Status:   "failed",
				Message:  message,
				Duration: time.Since(targetStart).Milliseconds(),
			})
		}
	}
	connect, err := connectOperation(target)
	if err != nil {
		return targetFailed, err
	}
	if err := pause.Wait(ctx); err != nil {
		return targetFailed, err
	}
	ok, skipped, err := runRequiredStepWithSkip(ctx, opRunner, agent, round, target, StepSnapshot{Name: "connect", Type: "connect", Operation: connect.Name}, connect, skip, emit)
	if err != nil {
		return targetFailed, err
	}
	if skipped {
		return targetSkipped, finishOperatorSkippedTarget(round, target, targetStart, emit)
	}
	if !ok {
		ready = false
		targetOK = false
	}
	if ready {
		wait, err := waitOperation(target)
		if err != nil {
			return targetFailed, err
		}
		if err := pause.Wait(ctx); err != nil {
			return targetFailed, err
		}
		ok, skipped, err = runRequiredStepWithSkip(ctx, opRunner, agent, round, target, StepSnapshot{Name: "wait_connected", Type: "wait_connected", Operation: wait.Name}, wait, skip, emit)
		if err != nil {
			return targetFailed, err
		}
		if skipped {
			return targetSkipped, finishOperatorSkippedTarget(round, target, targetStart, emit)
		}
		if !ok {
			ready = false
			targetOK = false
		}
	}
	if ready {
		for i, check := range plan.Checks {
			if err := pause.Wait(ctx); err != nil {
				return targetFailed, err
			}
			ok, skipped, err := runCheckWithSkip(ctx, opRunner, agent, round, target, check, skip, emit)
			if err != nil {
				return targetFailed, err
			}
			if skipped {
				return targetSkipped, finishOperatorSkippedTarget(round, target, targetStart, emit)
			}
			if !ok {
				targetOK = false
				if check.Required {
					message := fmt.Sprintf("required check failed: %s", check.DisplayName())
					if err := skipChecks(round, targetSnapshot, plan.Checks[i+1:], message, emit); err != nil {
						return targetFailed, err
					}
					break
				}
			}
		}
	} else {
		if err := skipChecks(round, targetSnapshot, plan.Checks, "connect or wait_connected failed", emit); err != nil {
			return targetFailed, err
		}
	}
	if err := pause.Wait(ctx); err != nil {
		return targetFailed, err
	}
	if err := runCleanup(ctx, opRunner, agent, round, target, skip, emit); err != nil && !errors.Is(err, ErrSkipRequested) {
		return targetFailed, err
	}
	status := "ok"
	result := targetPassed
	if !targetOK {
		status = "failed"
		result = targetFailed
	}
	return result, emit(Event{
		Kind:     EventTargetFinished,
		Round:    round,
		Target:   targetSnapshot,
		Status:   status,
		Duration: time.Since(targetStart).Milliseconds(),
	})
}

func skipChecks(round uint64, target TargetSnapshot, checks []Check, message string, emit func(Event) error) error {
	for _, check := range checks {
		if err := emit(Event{
			Kind:   EventStepFinished,
			Round:  round,
			Target: target,
			Step: StepSnapshot{
				Name:    check.DisplayName(),
				Type:    check.Type,
				Status:  "skipped",
				Skipped: true,
				Message: message,
			},
			Status:  "skipped",
			Message: message,
		}); err != nil {
			return err
		}
	}
	return nil
}

func skipTargetPlanSteps(round uint64, target TargetSnapshot, checks []Check, message string, emit func(Event) error) error {
	steps := []StepSnapshot{
		{Name: "connect", Type: "connect", Operation: "wifi.connect", Status: "skipped", Skipped: true, Message: message},
		{Name: "wait_connected", Type: "wait_connected", Operation: "wifi.wait", Status: "skipped", Skipped: true, Message: message},
	}
	for _, check := range checks {
		steps = append(steps, StepSnapshot{Name: check.DisplayName(), Type: check.Type, Status: "skipped", Skipped: true, Message: message})
	}
	for _, step := range steps {
		if err := emit(Event{Kind: EventStepFinished, Round: round, Target: target, Step: step, Status: "skipped", Message: message}); err != nil {
			return err
		}
	}
	return nil
}

func skipTarget(round uint64, target Target, checks []Check, started time.Time, message string, emit func(Event) error) error {
	targetSnapshot := snapshotTarget(target)
	if err := emit(Event{Kind: EventTargetStarted, Round: round, Target: targetSnapshot, Status: "skipped", Message: message}); err != nil {
		return err
	}
	for _, step := range skippedTargetSteps(checks, message) {
		if err := emit(Event{Kind: EventStepFinished, Round: round, Target: targetSnapshot, Step: step, Status: "skipped", Message: message}); err != nil {
			return err
		}
	}
	return emit(Event{
		Kind:     EventTargetFinished,
		Round:    round,
		Target:   targetSnapshot,
		Status:   "skipped",
		Message:  message,
		Duration: time.Since(started).Milliseconds(),
	})
}

func finishOperatorSkippedTarget(round uint64, target Target, started time.Time, emit func(Event) error) error {
	message := "skipped by operator"
	return emit(Event{
		Kind:     EventTargetFinished,
		Round:    round,
		Target:   snapshotTarget(target),
		Status:   "skipped",
		Message:  message,
		Duration: time.Since(started).Milliseconds(),
	})
}

func skippedTargetSteps(checks []Check, message string) []StepSnapshot {
	steps := []StepSnapshot{
		{Name: "connect", Type: "connect", Operation: "wifi.connect", Status: "skipped", Skipped: true, Message: message},
		{Name: "wait_connected", Type: "wait_connected", Operation: "wifi.wait", Status: "skipped", Skipped: true, Message: message},
	}
	for _, check := range checks {
		steps = append(steps, StepSnapshot{Name: check.DisplayName(), Type: check.Type, Status: "skipped", Skipped: true, Message: message})
	}
	return steps
}

func runRequiredStep(ctx context.Context, opRunner OperationRunner, agent control.AgentInfo, round uint64, target Target, step StepSnapshot, op command.Operation, emit func(Event) error) (bool, error) {
	ok, _, err := runRequiredStepWithSkip(ctx, opRunner, agent, round, target, step, op, nil, emit)
	return ok, err
}

func runRequiredStepWithSkip(ctx context.Context, opRunner OperationRunner, agent control.AgentInfo, round uint64, target Target, step StepSnapshot, op command.Operation, skip *SkipController, emit func(Event) error) (bool, bool, error) {
	exec, skipped, err := runOperationStepWithSkip(ctx, opRunner, agent, round, target, step, op, skip, emit)
	if err != nil || exec.Result == nil {
		return false, skipped, err
	}
	return exec.Result.GetStatus() == controlpb.CommandResult_STATUS_OK, skipped, nil
}

func runCheck(ctx context.Context, opRunner OperationRunner, agent control.AgentInfo, round uint64, target Target, check Check, emit func(Event) error) (bool, error) {
	ok, _, err := runCheckWithSkip(ctx, opRunner, agent, round, target, check, nil, emit)
	return ok, err
}

func runCheckWithSkip(ctx context.Context, opRunner OperationRunner, agent control.AgentInfo, round uint64, target Target, check Check, skip *SkipController, emit func(Event) error) (bool, bool, error) {
	resolvedCheck, err := resolveCheckForTarget(check, target)
	if err != nil {
		return false, false, err
	}
	check = resolvedCheck
	if isGatewayPingCheck(check) {
		return runGatewayPingCheckWithSkip(ctx, opRunner, agent, round, target, check, skip, emit)
	}
	op, err := checkOperation(check, target)
	if err != nil {
		return false, false, err
	}
	step := StepSnapshot{Name: check.DisplayName(), Type: check.Type, Operation: op.Name}
	targetSnapshot := snapshotTarget(target)
	started := time.Now()
	emitCause := failureCauseEmitter(round, target, step, emit)
	deadline, poll := checkExpectationDeadline(check, started)
	maxAttempts := checkMaxAttempts(step, check, poll)
	for attempt := 1; ; attempt++ {
		attemptResult, err := runOperationAttempt(ctx, opRunner, agent, round, target, step, op, attempt, maxAttempts, skip, emit, emitCause)
		if err != nil {
			return false, false, err
		}
		if attemptResult.skipped {
			return false, true, emitOperatorSkippedStep(round, targetSnapshot, step, started, emit)
		}
		failedStep, operationFailed := operationFailureStep(step, attemptResult.exec, attemptResult.runErr)
		findings := checkFindings(target, check, attemptResult.exec)
		if !operationFailed && len(findings) == 0 {
			if err := emitRetrySucceeded(round, target, step, attempt, maxAttempts, emit); err != nil {
				return false, false, err
			}
			step.Status = "ok"
			if attempt > 1 {
				step.Message = retrySucceededMessage(step, attempt, maxAttempts)
			}
			return true, false, emit(Event{
				Kind:     EventStepFinished,
				Round:    round,
				Target:   targetSnapshot,
				Step:     step,
				Status:   "ok",
				Message:  step.Message,
				Duration: time.Since(started).Milliseconds(),
			})
		}
		reason := checkFailureMessage(failedStep, findings)
		if checkRetryAvailable(attempt, maxAttempts, deadline) {
			if err := emitRetrying(round, target, step, attempt, maxAttempts, reason, emit); err != nil {
				return false, false, err
			}
			if operationFailed {
				if err := emitFailureCause(ctx, opRunner, agent, round, target, failedStep, op, attemptResult.exec, attemptResult.runErr, emitCause); err != nil {
					return false, false, err
				}
			}
			if err := sleepCheckRetry(ctx, deadline, poll); err != nil {
				return false, false, err
			}
			continue
		}
		failedStep.Status = "failed"
		failedStep.Message = firstNonEmpty(failedStep.Message, failedStep.Error, reason)
		if err := emit(Event{
			Kind:     EventStepFinished,
			Round:    round,
			Target:   targetSnapshot,
			Step:     failedStep,
			Status:   "failed",
			Message:  failedStep.Message,
			Duration: time.Since(started).Milliseconds(),
		}); err != nil {
			return false, false, err
		}
		if operationFailed {
			if err := emitFailureCause(ctx, opRunner, agent, round, target, failedStep, op, attemptResult.exec, attemptResult.runErr, emitCause); err != nil {
				return false, false, err
			}
		}
		for _, item := range findings {
			if err := emit(Event{
				Kind:    EventFinding,
				Round:   round,
				Target:  targetSnapshot,
				Step:    failedStep,
				Finding: &item,
				Status:  "failed",
				Message: item.Message,
			}); err != nil {
				return false, false, err
			}
		}
		return false, false, nil
	}
}

func checkExpectationDeadline(check Check, started time.Time) (time.Time, time.Duration) {
	if !pollsExpectationUntilTimeout(check) || check.Timeout.Duration <= 0 {
		return time.Time{}, 0
	}
	return started.Add(check.Timeout.Duration), checkExpectationPollInterval
}

func pollsExpectationUntilTimeout(check Check) bool {
	switch check.Type {
	case "ip_status", "wifi_status":
		return true
	default:
		return false
	}
}

func checkMaxAttempts(step StepSnapshot, check Check, poll time.Duration) int {
	maxAttempts := operationMaxAttempts(step)
	if poll <= 0 || check.Timeout.Duration <= 0 {
		return maxAttempts
	}
	attempts := 1 + int(check.Timeout.Duration/poll)
	if check.Timeout.Duration%poll != 0 {
		attempts++
	}
	if attempts < maxAttempts {
		return maxAttempts
	}
	return attempts
}

func checkRetryAvailable(attempt int, maxAttempts int, deadline time.Time) bool {
	if attempt >= maxAttempts {
		return false
	}
	if deadline.IsZero() {
		return true
	}
	return time.Now().Before(deadline)
}

func sleepCheckRetry(ctx context.Context, deadline time.Time, poll time.Duration) error {
	if deadline.IsZero() || poll <= 0 {
		return nil
	}
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return nil
	}
	if poll > remaining {
		poll = remaining
	}
	return sleepContext(ctx, poll)
}

func runOperationStepWithSkip(ctx context.Context, opRunner OperationRunner, agent control.AgentInfo, round uint64, target Target, step StepSnapshot, op command.Operation, skip *SkipController, emit func(Event) error) (runner.Result, bool, error) {
	targetSnapshot := snapshotTarget(target)
	started := time.Now()
	emitCause := failureCauseEmitter(round, target, step, emit)
	maxAttempts := operationMaxAttempts(step)
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		attemptResult, err := runOperationAttempt(ctx, opRunner, agent, round, target, step, op, attempt, maxAttempts, skip, emit, emitCause)
		if err != nil {
			return attemptResult.exec, false, err
		}
		if attemptResult.skipped {
			return attemptResult.exec, true, emitOperatorSkippedStep(round, targetSnapshot, step, started, emit)
		}
		finishedStep, failed := operationFailureStep(step, attemptResult.exec, attemptResult.runErr)
		if !failed {
			if err := emitRetrySucceeded(round, target, step, attempt, maxAttempts, emit); err != nil {
				return attemptResult.exec, false, err
			}
			finishedStep.Status = "ok"
			if attempt > 1 {
				finishedStep.Message = retrySucceededMessage(step, attempt, maxAttempts)
			}
			return attemptResult.exec, false, emit(Event{
				Kind:     EventStepFinished,
				Round:    round,
				Target:   targetSnapshot,
				Step:     finishedStep,
				Status:   "ok",
				Message:  finishedStep.Message,
				Duration: time.Since(started).Milliseconds(),
			})
		}
		reason := operationFailureReason(finishedStep)
		if attempt < maxAttempts {
			if err := emitRetrying(round, target, step, attempt, maxAttempts, reason, emit); err != nil {
				return attemptResult.exec, false, err
			}
			if err := emitFailureCause(ctx, opRunner, agent, round, target, finishedStep, op, attemptResult.exec, attemptResult.runErr, emitCause); err != nil {
				return attemptResult.exec, false, err
			}
			continue
		}
		if err := emit(Event{
			Kind:     EventStepFinished,
			Round:    round,
			Target:   targetSnapshot,
			Step:     finishedStep,
			Status:   "failed",
			Message:  firstNonEmpty(operationFailureReason(finishedStep), "operation failed"),
			Duration: time.Since(started).Milliseconds(),
		}); err != nil {
			return attemptResult.exec, false, err
		}
		if err := emitFailureCause(ctx, opRunner, agent, round, target, finishedStep, op, attemptResult.exec, attemptResult.runErr, emitCause); err != nil {
			return attemptResult.exec, false, err
		}
		return attemptResult.exec, false, nil
	}
	return runner.Result{}, false, nil
}

func emitOperatorSkippedStep(round uint64, target TargetSnapshot, step StepSnapshot, started time.Time, emit func(Event) error) error {
	step.Status = "pending"
	step.Skipped = true
	step.Message = "skipped by operator"
	return emit(Event{
		Kind:     EventStepFinished,
		Round:    round,
		Target:   target,
		Step:     step,
		Status:   "skipped",
		Message:  step.Message,
		Duration: time.Since(started).Milliseconds(),
	})
}

func runOperationAttempt(ctx context.Context, opRunner OperationRunner, agent control.AgentInfo, round uint64, target Target, step StepSnapshot, op command.Operation, attempt int, maxAttempts int, skip *SkipController, emit func(Event) error, emitCause func(string) error) (operationAttempt, error) {
	targetSnapshot := snapshotTarget(target)
	attemptStep := step
	attemptStep.Status = "running"
	if attempt > 1 {
		attemptStep.Message = retryAttemptMessage(step, attempt, maxAttempts)
	}
	if err := emit(Event{Kind: EventStepStarted, Round: round, Target: targetSnapshot, Step: attemptStep, Status: "running", Message: attemptStep.Message}); err != nil {
		return operationAttempt{}, err
	}
	opCtx, finish := skip.operationContext(ctx)
	stopCauseMonitor := startFailureCauseMonitor(opCtx, opRunner, agent, round, target, attemptStep, op, emitCause)
	exec, runErr := opRunner.Run(opCtx, agent, op)
	finish()
	if stopCauseMonitor != nil {
		stopCauseMonitor()
	}
	if operationSkipped(opCtx) {
		return operationAttempt{exec: exec, runErr: runErr, skipped: true}, nil
	}
	if runErr != nil && ctx.Err() != nil {
		return operationAttempt{exec: exec, runErr: runErr}, ctx.Err()
	}
	return operationAttempt{exec: exec, runErr: runErr}, nil
}

func operationMaxAttempts(step StepSnapshot) int {
	if !retryableStep(step) {
		return 1
	}
	return 1 + operationRetryLimit
}

func retryableStep(step StepSnapshot) bool {
	stepType := strings.ToLower(firstNonEmpty(step.Type, step.Name))
	return stepType != "cleanup"
}

func operationFailureStep(step StepSnapshot, exec runner.Result, runErr error) (StepSnapshot, bool) {
	step.Status = "ok"
	if runErr != nil {
		step.Status = "failed"
		step.Error = runErr.Error()
		return step, true
	}
	if exec.Result == nil {
		step.Status = "failed"
		step.Message = "missing command result"
		return step, true
	}
	if exec.Result.GetStatus() != controlpb.CommandResult_STATUS_OK {
		step.Status = "failed"
		step.Message = operationFailureMessage(exec.Result)
		return step, true
	}
	return step, false
}

func checkFindings(target Target, check Check, exec runner.Result) []Finding {
	if exec.Result == nil {
		return nil
	}
	var findings []Finding
	if exec.Result.GetStatus() != controlpb.CommandResult_STATUS_OK {
		findings = append(findings, Finding{
			Target:   target.DisplayName(),
			Check:    check.DisplayName(),
			Metric:   "status",
			Observed: statusName(exec.Result.GetStatus()),
			Expected: "== ok",
			Message:  exec.Result.GetMessage(),
		})
	}
	findings = append(findings, evaluateMatchers(target, check, metricsForResult(exec.Result))...)
	return findings
}

func checkFailureMessage(step StepSnapshot, findings []Finding) string {
	if message := operationFailureReason(step); message != "" {
		return message
	}
	for _, finding := range findings {
		if message := firstNonEmpty(finding.Message, finding.Metric+"="+finding.Observed); message != "" {
			return message
		}
	}
	return "check failed"
}

func operationFailureReason(step StepSnapshot) string {
	return firstNonEmpty(step.Message, step.Error)
}

func emitRetrying(round uint64, target Target, step StepSnapshot, attempt int, maxAttempts int, reason string, emit func(Event) error) error {
	if attempt >= maxAttempts {
		return nil
	}
	return emit(Event{
		Kind:    EventLog,
		Round:   round,
		Target:  snapshotTarget(target),
		Step:    step,
		Status:  "warn",
		Message: fmt.Sprintf("retrying %s attempt=%d/%d retry=%d/%d reason=%s", retryStepLabel(step), attempt, maxAttempts, attempt, maxAttempts-1, firstNonEmpty(reason, "failed")),
	})
}

func emitRetrySucceeded(round uint64, target Target, step StepSnapshot, attempt int, maxAttempts int, emit func(Event) error) error {
	if attempt <= 1 {
		return nil
	}
	return emit(Event{
		Kind:    EventLog,
		Round:   round,
		Target:  snapshotTarget(target),
		Step:    step,
		Status:  "info",
		Message: retrySucceededMessage(step, attempt, maxAttempts),
	})
}

func retryAttemptMessage(step StepSnapshot, attempt int, maxAttempts int) string {
	return fmt.Sprintf("retry attempt %d/%d for %s", attempt, maxAttempts, retryStepLabel(step))
}

func retrySucceededMessage(step StepSnapshot, attempt int, maxAttempts int) string {
	return fmt.Sprintf("retry succeeded %s attempt=%d/%d retries=%d", retryStepLabel(step), attempt, maxAttempts, attempt-1)
}

func retryStepLabel(step StepSnapshot) string {
	return firstNonEmpty(step.Name, step.Type, step.Operation, "operation")
}

func operationFailureMessage(result *controlpb.CommandResult) string {
	if result == nil {
		return ""
	}
	message := result.GetMessage()
	if detail := wifiAssertFailureMessage(result.GetWifiAssert()); detail != "" {
		if message == "" {
			return detail
		}
		if strings.Contains(message, detail) {
			return message
		}
		return message + ": " + detail
	}
	return message
}

func wifiAssertFailureMessage(result *controlpb.WifiAssertResult) string {
	if result == nil || result.GetPassed() {
		return ""
	}
	failed := make([]string, 0)
	lastPass := ""
	foundFailure := false
	for _, check := range result.GetChecks() {
		key := firstNonEmpty(check.GetKey(), "check")
		if check.GetPassed() {
			if !foundFailure {
				lastPass = key
			}
			continue
		}
		foundFailure = true
		failed = append(failed, fmt.Sprintf("%s(actual=%s expected=%s)",
			key,
			firstNonEmpty(check.GetActual(), "-"),
			firstNonEmpty(check.GetExpected(), "-"),
		))
	}
	parts := make([]string, 0, 3)
	if lastPass != "" && len(failed) > 0 {
		parts = append(parts, "last_pass="+lastPass)
	}
	if len(failed) > 0 {
		parts = append(parts, "failed="+strings.Join(failed, ","))
	}
	if result.GetElapsedMs() > 0 {
		parts = append(parts, fmt.Sprintf("assert_elapsed_ms=%d", result.GetElapsedMs()))
	}
	if len(result.GetErrors()) > 0 {
		parts = append(parts, "errors="+strings.Join(result.GetErrors(), ";"))
	}
	return strings.Join(parts, " ")
}

func startFailureCauseMonitor(ctx context.Context, opRunner OperationRunner, agent control.AgentInfo, round uint64, target Target, step StepSnapshot, op command.Operation, emitCause func(string) error) func() {
	monitor, ok := opRunner.(FailureCauseMonitor)
	if !ok {
		return nil
	}
	return monitor.WatchFailureCause(ctx, agent, FailureCauseContext{
		Round:     round,
		Target:    target,
		Step:      step,
		Operation: op,
	}, func(message string) {
		_ = emitCause(message)
	})
}

func failureCauseEmitter(round uint64, target Target, step StepSnapshot, emit func(Event) error) func(string) error {
	var mu sync.Mutex
	seen := make(map[string]struct{})
	return func(message string) error {
		message = firstNonEmpty(message)
		if message == "" {
			return nil
		}
		mu.Lock()
		if _, ok := seen[message]; ok {
			mu.Unlock()
			return nil
		}
		seen[message] = struct{}{}
		mu.Unlock()
		status := "warn"
		if strings.HasPrefix(message, "wifi connect state:") {
			status = "info"
		}
		return emit(Event{
			Kind:    EventLog,
			Round:   round,
			Target:  snapshotTarget(target),
			Step:    step,
			Status:  status,
			Message: message,
		})
	}
}

func emitFailureCause(ctx context.Context, opRunner OperationRunner, agent control.AgentInfo, round uint64, target Target, step StepSnapshot, op command.Operation, exec runner.Result, runErr error, emitCause func(string) error) error {
	collector, ok := opRunner.(FailureCauseCollector)
	if !ok {
		return nil
	}
	message := firstNonEmpty(collector.FailureCause(ctx, agent, FailureCauseContext{
		Round:     round,
		Target:    target,
		Step:      step,
		Operation: op,
		Execution: exec,
		Err:       runErr,
	}))
	if message == "" {
		return nil
	}
	return emitCause(message)
}

func runRoundMacRotation(ctx context.Context, plan Plan, opRunner OperationRunner, agent control.AgentInfo, round uint64, phase string, pause *PauseController, skip *SkipController, emit func(Event) error) error {
	seen := map[string]struct{}{}
	for _, target := range plan.Targets {
		if target.macRotation() != macRotationPerRound {
			continue
		}
		key := macRotationForgetKey(target)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		if err := pause.Wait(ctx); err != nil {
			return err
		}
		_, skipped, err := runMacRotationForget(ctx, opRunner, agent, round, target, phase, skip, emit)
		if err != nil {
			return err
		}
		if skipped {
			return ErrSkipRequested
		}
	}
	return nil
}

func macRotationForgetKey(target Target) string {
	return strings.ToLower(strings.TrimSpace(target.SSID))
}

func runMacRotationForget(ctx context.Context, opRunner OperationRunner, agent control.AgentInfo, round uint64, target Target, phase string, skip *SkipController, emit func(Event) error) (bool, bool, error) {
	op := command.WifiForgetOperation(target.SSID)
	opCtx, finish := skip.operationContext(ctx)
	exec, runErr := opRunner.Run(opCtx, agent, op)
	finish()
	if operationSkipped(opCtx) {
		return false, true, nil
	}
	if runErr != nil && ctx.Err() != nil {
		return false, false, ctx.Err()
	}
	ok, message := macRotationForgetResult(exec, runErr)
	status := "info"
	stepStatus := "ok"
	if !ok {
		status = "warn"
		stepStatus = "failed"
	}
	if err := emit(Event{
		Kind:   EventLog,
		Round:  round,
		Target: snapshotTarget(target),
		Step: StepSnapshot{
			Name:      "mac_rotation_" + phase,
			Type:      "cleanup",
			Operation: op.Name,
			Status:    stepStatus,
		},
		Status:  status,
		Message: fmt.Sprintf("mac_rotation=%s phase=%s ssid=%q %s", target.macRotation(), phase, target.SSID, message),
	}); err != nil {
		return false, false, err
	}
	return ok, false, nil
}

func macRotationForgetResult(exec runner.Result, runErr error) (bool, string) {
	if runErr != nil {
		return false, "error=" + runErr.Error()
	}
	if exec.Result == nil {
		return false, "missing command result"
	}
	message := firstNonEmpty(exec.Result.GetMessage(), statusName(exec.Result.GetStatus()))
	if exec.Result.GetStatus() == controlpb.CommandResult_STATUS_OK {
		return true, "result=" + message
	}
	if macRotationForgetNotFound(exec.Result) {
		return true, "result=not_found"
	}
	return false, "result=" + message
}

func macRotationForgetNotFound(result *controlpb.CommandResult) bool {
	if result == nil {
		return false
	}
	return strings.Contains(strings.ToLower(result.GetMessage()), "wifi network not found")
}

func runCleanup(ctx context.Context, opRunner OperationRunner, agent control.AgentInfo, round uint64, target Target, skip *SkipController, emit func(Event) error) error {
	if target.disconnectAfter() {
		op := command.WifiDisconnectOperation()
		_, skipped, err := runOperationStepWithSkip(ctx, opRunner, agent, round, target, StepSnapshot{Name: "disconnect", Type: "cleanup", Operation: op.Name}, op, skip, emit)
		if err != nil {
			return err
		}
		if skipped {
			return ErrSkipRequested
		}
	}
	if target.forgetAfter() {
		op := command.WifiForgetOperation(target.SSID)
		_, skipped, err := runOperationStepWithSkip(ctx, opRunner, agent, round, target, StepSnapshot{Name: "forget", Type: "cleanup", Operation: op.Name}, op, skip, emit)
		if err != nil {
			return err
		}
		if skipped {
			return ErrSkipRequested
		}
	}
	return nil
}

func connectOperation(target Target) (command.Operation, error) {
	passphrase, err := targetPassphrase(target)
	if err != nil {
		return command.Operation{}, err
	}
	return command.WifiConnectOperation(command.WifiConnectOptions{
		SSID:             target.SSID,
		Passphrase:       passphrase,
		Security:         target.Security,
		BSSID:            target.BSSID,
		Band:             target.Band,
		MacRandomization: target.MacRandomization,
		Timeout:          durationMillis(target.ConnectTimeout),
	})
}

func waitOperation(target Target) (command.Operation, error) {
	return command.WifiWaitConnectedOperation(target.SSID, command.WifiExpectationOptions{
		BSSID:            target.BSSID,
		Security:         target.Security,
		Band:             target.Band,
		RequireIP:        target.requireIP(),
		RequireValidated: target.requireValidated(),
		Timeout:          durationMillis(target.WaitTimeout),
	})
}

func checkOperation(check Check, target Target) (command.Operation, error) {
	switch check.Type {
	case "wifi_status":
		return command.WifiStatusOperation(), nil
	case "ip_status":
		return command.IPStatusOperation(), nil
	case "ping":
		host := firstNonEmpty(check.Host, "1.1.1.1")
		return command.PingOperation(command.PingOptions{Host: host, Count: number(check.Count), Size: number(check.SizeBytes), Family: check.Family, Timeout: durationMillis(check.Timeout)})
	case "traceroute":
		host := firstNonEmpty(check.Host, "1.1.1.1")
		return command.TracerouteOperation(command.TracerouteOptions{Host: host, MaxHops: number(check.MaxHops), Size: number(check.SizeBytes), Family: check.Family, Timeout: durationMillis(check.Timeout)})
	case "path_mtu":
		host := firstNonEmpty(check.Host, "1.1.1.1")
		return command.PathMTUOperation(command.PathMTUOptions{Host: host, MinMTU: number(check.MinMTU), MaxMTU: number(check.MaxMTU), Family: check.Family, Timeout: durationMillis(check.Timeout)})
	case "dns":
		query := firstNonEmpty(check.Query, check.Host, "example.com")
		return command.DNSOperation(query, check.Record, durationMillis(check.Timeout))
	case "http":
		url := firstNonEmpty(check.URL, "http://connectivitycheck.gstatic.com/generate_204")
		status := number(check.Status)
		if check.URL == "" && status == "" {
			status = "204"
		}
		return command.HTTPOperation(url, status, durationMillis(check.Timeout))
	case "global_ip":
		family := firstNonEmpty(check.Family, "ipv4")
		return command.GlobalIPOperation(family, durationMillis(check.Timeout))
	case "download":
		url := firstNonEmpty(check.URL, "http://1.1.1.1/cdn-cgi/trace")
		return command.DownloadOperation(url, durationMillis(check.Timeout))
	case "scan_detail":
		targetValue := firstNonEmpty(check.ScanTarget, target.BSSID, target.SSID)
		band := firstNonEmpty(check.Band, target.Band)
		return command.WifiScanDetailOperation(targetValue, band)
	default:
		return command.Operation{}, fmt.Errorf("unsupported watch check type %q", check.Type)
	}
}

func targetPassphrase(target Target) (string, error) {
	if target.Passphrase != "" {
		return target.Passphrase, nil
	}
	if target.PassphraseEnv == "" {
		return "", nil
	}
	value, ok := os.LookupEnv(target.PassphraseEnv)
	if !ok || value == "" {
		return "", fmt.Errorf("environment variable %s is not set or empty", target.PassphraseEnv)
	}
	return value, nil
}

func number(value uint32) string {
	if value == 0 {
		return ""
	}
	return strconv.FormatUint(uint64(value), 10)
}

func sleepContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
