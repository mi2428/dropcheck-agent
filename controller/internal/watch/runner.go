package watch

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"

	"dropcheck/controller/internal/command"
	"dropcheck/controller/internal/control"
	"dropcheck/controller/internal/controlpb"
	"dropcheck/controller/internal/runner"
)

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

// Run executes plan forever until ctx is canceled.
func Run(ctx context.Context, plan Plan, opRunner OperationRunner, agent control.AgentInfo, sink Sink) error {
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
	for round := uint64(1); ; round++ {
		if err := ctx.Err(); err != nil {
			return nil
		}
		failed, err := runRound(ctx, plan, opRunner, agent, round, emit)
		if err != nil {
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
	serial := ""
	if agent.Hello != nil {
		serial = agent.Hello.GetAdbSerial()
	}
	name := firstNonEmpty(serial, agent.ID, agent.SessionID)
	if serial == "" && len(name) > 12 {
		name = name[:12]
	}
	return AgentSnapshot{
		ID:        agent.ID,
		SessionID: agent.SessionID,
		Name:      name,
		ADBSerial: serial,
	}
}

func runRound(ctx context.Context, plan Plan, opRunner OperationRunner, agent control.AgentInfo, round uint64, emit func(Event) error) (int, error) {
	started := time.Now()
	if err := emit(Event{Kind: EventRoundStarted, Round: round, Status: "running"}); err != nil {
		return 0, err
	}
	failedTargets := 0
	for _, target := range plan.Targets {
		ok, err := runTarget(ctx, plan, opRunner, agent, round, target, emit)
		if err != nil {
			return failedTargets, err
		}
		if !ok {
			failedTargets++
		}
	}
	status := "ok"
	if failedTargets > 0 {
		status = "failed"
	}
	return failedTargets, emit(Event{
		Kind:     EventRoundFinished,
		Round:    round,
		Status:   status,
		Message:  fmt.Sprintf("targets=%d failed=%d", len(plan.Targets), failedTargets),
		Duration: time.Since(started).Milliseconds(),
	})
}

func runTarget(ctx context.Context, plan Plan, opRunner OperationRunner, agent control.AgentInfo, round uint64, target Target, emit func(Event) error) (bool, error) {
	targetStart := time.Now()
	targetSnapshot := snapshotTarget(target)
	if err := emit(Event{Kind: EventTargetStarted, Round: round, Target: targetSnapshot, Status: "running"}); err != nil {
		return false, err
	}
	targetOK := true
	ready := true
	connect, err := connectOperation(target)
	if err != nil {
		return false, err
	}
	ok, err := runRequiredStep(ctx, opRunner, agent, round, target, StepSnapshot{Name: "connect", Type: "connect", Operation: connect.Name}, connect, emit)
	if err != nil {
		return false, err
	}
	if !ok {
		ready = false
		targetOK = false
	}
	if ready {
		wait, err := waitOperation(target)
		if err != nil {
			return false, err
		}
		ok, err = runRequiredStep(ctx, opRunner, agent, round, target, StepSnapshot{Name: "wait_connected", Type: "wait_connected", Operation: wait.Name}, wait, emit)
		if err != nil {
			return false, err
		}
		if !ok {
			ready = false
			targetOK = false
		}
	}
	if ready {
		for _, check := range plan.Checks {
			ok, err := runCheck(ctx, opRunner, agent, round, target, check, emit)
			if err != nil {
				return false, err
			}
			if !ok {
				targetOK = false
			}
		}
	} else {
		for _, check := range plan.Checks {
			if err := emit(Event{
				Kind:   EventStepFinished,
				Round:  round,
				Target: targetSnapshot,
				Step: StepSnapshot{
					Name:    check.DisplayName(),
					Type:    check.Type,
					Status:  "skipped",
					Skipped: true,
					Message: "connect or wait_connected failed",
				},
				Status: "skipped",
			}); err != nil {
				return false, err
			}
		}
	}
	if err := runCleanup(ctx, opRunner, agent, round, target, emit); err != nil {
		return false, err
	}
	status := "ok"
	if !targetOK {
		status = "failed"
	}
	return targetOK, emit(Event{
		Kind:     EventTargetFinished,
		Round:    round,
		Target:   targetSnapshot,
		Status:   status,
		Duration: time.Since(targetStart).Milliseconds(),
	})
}

func runRequiredStep(ctx context.Context, opRunner OperationRunner, agent control.AgentInfo, round uint64, target Target, step StepSnapshot, op command.Operation, emit func(Event) error) (bool, error) {
	exec, err := runOperationStep(ctx, opRunner, agent, round, target, step, op, emit)
	if err != nil || exec.Result == nil {
		return false, err
	}
	return exec.Result.GetStatus() == controlpb.CommandResult_STATUS_OK, nil
}

func runCheck(ctx context.Context, opRunner OperationRunner, agent control.AgentInfo, round uint64, target Target, check Check, emit func(Event) error) (bool, error) {
	op, err := checkOperation(check, target)
	if err != nil {
		return false, err
	}
	step := StepSnapshot{Name: check.DisplayName(), Type: check.Type, Operation: op.Name}
	exec, err := runOperationStep(ctx, opRunner, agent, round, target, step, op, emit)
	if err != nil || exec.Result == nil {
		return false, err
	}
	ok := exec.Result.GetStatus() == controlpb.CommandResult_STATUS_OK
	var findings []Finding
	if !ok {
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
	for _, item := range findings {
		if err := emit(Event{
			Kind:    EventFinding,
			Round:   round,
			Target:  snapshotTarget(target),
			Step:    step,
			Finding: &item,
			Status:  "failed",
			Message: item.Message,
		}); err != nil {
			return false, err
		}
	}
	return ok && len(findings) == 0, nil
}

func runOperationStep(ctx context.Context, opRunner OperationRunner, agent control.AgentInfo, round uint64, target Target, step StepSnapshot, op command.Operation, emit func(Event) error) (runner.Result, error) {
	targetSnapshot := snapshotTarget(target)
	step.Status = "running"
	if err := emit(Event{Kind: EventStepStarted, Round: round, Target: targetSnapshot, Step: step, Status: "running"}); err != nil {
		return runner.Result{}, err
	}
	started := time.Now()
	emitCause := failureCauseEmitter(round, target, step, emit)
	stopCauseMonitor := startFailureCauseMonitor(ctx, opRunner, agent, round, target, step, op, emitCause)
	exec, err := opRunner.Run(ctx, agent, op)
	if stopCauseMonitor != nil {
		stopCauseMonitor()
	}
	step.Status = "ok"
	if err != nil {
		if ctx.Err() != nil {
			return exec, ctx.Err()
		}
		step.Status = "failed"
		step.Error = err.Error()
		if emitErr := emit(Event{Kind: EventStepFinished, Round: round, Target: targetSnapshot, Step: step, Status: "failed", Message: err.Error(), Duration: time.Since(started).Milliseconds()}); emitErr != nil {
			return exec, emitErr
		}
		if emitErr := emitFailureCause(ctx, opRunner, agent, round, target, step, op, exec, err, emitCause); emitErr != nil {
			return exec, emitErr
		}
		return exec, nil
	}
	if exec.Result != nil && exec.Result.GetStatus() != controlpb.CommandResult_STATUS_OK {
		step.Status = "failed"
		step.Message = exec.Result.GetMessage()
	}
	status := step.Status
	if err := emit(Event{
		Kind:     EventStepFinished,
		Round:    round,
		Target:   targetSnapshot,
		Step:     step,
		Status:   status,
		Message:  step.Message,
		Duration: time.Since(started).Milliseconds(),
	}); err != nil {
		return exec, err
	}
	if step.Status == "failed" {
		if err := emitFailureCause(ctx, opRunner, agent, round, target, step, op, exec, nil, emitCause); err != nil {
			return exec, err
		}
	}
	return exec, nil
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
		return emit(Event{
			Kind:    EventLog,
			Round:   round,
			Target:  snapshotTarget(target),
			Step:    step,
			Status:  "warn",
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

func runCleanup(ctx context.Context, opRunner OperationRunner, agent control.AgentInfo, round uint64, target Target, emit func(Event) error) error {
	if target.disconnectAfter() {
		op := command.WifiDisconnectOperation()
		_, err := runOperationStep(ctx, opRunner, agent, round, target, StepSnapshot{Name: "disconnect", Type: "cleanup", Operation: op.Name}, op, emit)
		if err != nil {
			return err
		}
	}
	if target.forgetAfter() {
		op := command.WifiForgetOperation(target.SSID)
		_, err := runOperationStep(ctx, opRunner, agent, round, target, StepSnapshot{Name: "forget", Type: "cleanup", Operation: op.Name}, op, emit)
		if err != nil {
			return err
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
		return command.PingOperation(command.PingOptions{Host: host, Count: number(check.Count), Size: number(check.SizeBytes), Timeout: durationMillis(check.Timeout)})
	case "traceroute":
		host := firstNonEmpty(check.Host, "1.1.1.1")
		return command.TracerouteOperation(command.TracerouteOptions{Host: host, MaxHops: number(check.MaxHops), Size: number(check.SizeBytes), Timeout: durationMillis(check.Timeout)})
	case "path_mtu":
		host := firstNonEmpty(check.Host, "1.1.1.1")
		return command.PathMTUOperation(command.PathMTUOptions{Host: host, MinMTU: number(check.MinMTU), MaxMTU: number(check.MaxMTU), Timeout: durationMillis(check.Timeout)})
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
