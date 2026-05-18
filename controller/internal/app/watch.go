package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"dropcheck/controller/internal/adb"
	"dropcheck/controller/internal/adbdiag"
	"dropcheck/controller/internal/command"
	"dropcheck/controller/internal/control"
	"dropcheck/controller/internal/controlpb"
	"dropcheck/controller/internal/runner"
	"dropcheck/controller/internal/tui"
	"dropcheck/controller/internal/watch"
)

type watchOptions struct {
	configPath string
	target     string
	jsonlPath  string
	noTUI      bool
}

func runWatch(ctx context.Context, opts shellOptions, args []string) error {
	watchOpts, err := parseWatchOptions(args)
	if err != nil {
		if errors.Is(err, errWatchHelpRequested) {
			writeWatchHelp(os.Stdout)
			return nil
		}
		return err
	}
	plan, err := watch.LoadFile(watchOpts.configPath)
	if err != nil {
		return fmt.Errorf("load watch config: %w", err)
	}
	eventPipe := newWatchEventPipe(512)
	if !watchOpts.noTUI {
		opts.OnLog = watchSessionLogHandler(eventPipe)
	}
	controlSession, err := startControlSession(ctx, opts)
	if err != nil {
		return err
	}
	defer controlSession.Close()

	state := &shellState{server: controlSession.Server, adbPath: opts.ADBPath}
	if len(controlSession.Agents) == 1 {
		state.setSelectedAgent(controlSession.Agents[0])
	}
	if watchOpts.target != "" {
		if watchOpts.target == "all" {
			state.targetAll = true
		} else {
			info, err := resolveShellAgent(state, watchOpts.target)
			if err != nil {
				return err
			}
			state.setSelectedAgent(info)
		}
	}
	agents, err := watchTargetAgents(state)
	if err != nil {
		return err
	}
	agentPlans, uiPlan, err := watchAgentPlans(plan, agents)
	if err != nil {
		return err
	}
	agents = watchAgentsFromPlans(agentPlans)
	agentSnapshots := watchAgentSnapshots(agents)

	watchCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	pauseControl := watch.NewPauseController()
	roundBarrier := watch.NewRoundBarrier(len(agentPlans))

	var sinks watch.MultiSink
	var jsonlFile *os.File
	if watchOpts.jsonlPath != "" {
		jsonlFile, err = os.OpenFile(watchOpts.jsonlPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return fmt.Errorf("open jsonl output: %w", err)
		}
		defer jsonlFile.Close()
		sinks = append(sinks, watch.NewJSONLWriter(jsonlFile))
	}
	sinks = append(sinks, watch.ChannelSink{C: eventPipe.C})

	errCh := make(chan error, len(agentPlans))
	var wg sync.WaitGroup
	for _, agentPlan := range agentPlans {
		wg.Go(func() {
			opRunner := watchOperationRunner{operation: runner.New(controlSession.Server), adbPath: opts.ADBPath}
			if err := watch.RunWithOptions(watchCtx, agentPlan.Plan, opRunner, agentPlan.Agent, sinks, watch.RunOptions{Pause: pauseControl, RoundBarrier: roundBarrier}); err != nil {
				errCh <- fmt.Errorf("%s: %w", agentDisplayName(agentPlan.Agent), err)
				cancel()
			}
		})
	}
	go func() {
		wg.Wait()
		eventPipe.Close()
		close(errCh)
	}()

	if watchOpts.noTUI {
		for event := range eventPipe.C {
			if event.Kind == watch.EventFinding && event.Finding != nil {
				agentLabel := ""
				if len(agents) > 1 {
					agentLabel = fmt.Sprintf(" agent=%s", event.Agent.DisplayName())
				}
				fmt.Printf("%s%s round=%d target=%s check=%s metric=%s observed=%s expected=%s\n",
					event.Time.Format("15:04:05"),
					agentLabel,
					event.Round,
					event.Finding.Target,
					event.Finding.Check,
					event.Finding.Metric,
					event.Finding.Observed,
					event.Finding.Expected,
				)
			}
		}
	} else if err := tui.RunWithPauseControl(watchCtx, uiPlan.Name, uiPlan.Targets, uiPlan.Checks, agentSnapshots, eventPipe.C, pauseControl); err != nil {
		cancel()
		_ = collectWatchErrors(errCh)
		return err
	}
	cancel()
	if err := collectWatchErrors(errCh); err != nil {
		return err
	}
	return nil
}

type watchOperationRunner struct {
	operation runner.Runner
	adbPath   string
}

const (
	watchWifiConnectEventTimeout  = 2 * time.Second
	watchWifiConnectStatusTimeout = 300 * time.Millisecond
)

func (r watchOperationRunner) Run(ctx context.Context, agent control.AgentInfo, op command.Operation) (runner.Result, error) {
	return r.operation.Run(ctx, agent, op)
}

func (r watchOperationRunner) FailureCause(ctx context.Context, agent control.AgentInfo, cause watch.FailureCauseContext) string {
	if !watchFailureCauseUsesWifiDiagnostics(cause) {
		return ""
	}
	serial := watchAgentADBSerial(agent)
	if serial == "" {
		return ""
	}
	return r.collectWifiFailureCause(ctx, serial, 2*time.Second)
}

func (r watchOperationRunner) WatchFailureCause(ctx context.Context, agent control.AgentInfo, cause watch.FailureCauseContext, emit func(string)) func() {
	if !watchFailureCauseUsesWifiDiagnostics(cause) {
		return nil
	}
	serial := watchAgentADBSerial(agent)
	if serial == "" {
		return nil
	}
	monitorCtx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Go(func() {
		lastConnectState := r.collectWifiConnectStatus(monitorCtx, serial, watchWifiConnectStatusTimeout)
		if lastConnectState != "" {
			emit(lastConnectState)
		}
		connectTicker := time.NewTicker(time.Second)
		defer connectTicker.Stop()
		for {
			select {
			case <-monitorCtx.Done():
				return
			case <-connectTicker.C:
				message := r.collectWifiConnectState(monitorCtx, serial)
				if message != "" && message != lastConnectState {
					lastConnectState = message
					emit(message)
				}
			}
		}
	})
	wg.Go(func() {
		baseline := r.collectWifiFailureCause(monitorCtx, serial, 1500*time.Millisecond)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-monitorCtx.Done():
				return
			case <-ticker.C:
				message := r.collectWifiFailureCause(monitorCtx, serial, 1500*time.Millisecond)
				if message != "" && message != baseline {
					baseline = message
					emit(message)
				}
			}
		}
	})
	return func() {
		cancel()
		wg.Wait()
	}
}

func (r watchOperationRunner) collectWifiFailureCause(ctx context.Context, serial string, timeout time.Duration) string {
	diagCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	message := adbdiag.CollectWifiFailureCause(diagCtx, adb.Client{
		Path:    r.adbPath,
		Serial:  serial,
		Timeout: timeout,
	})
	if message == "" {
		return ""
	}
	return "wifi failure cause: " + message
}

func (r watchOperationRunner) collectWifiConnectState(ctx context.Context, serial string) string {
	if message := r.collectWifiConnectEvents(ctx, serial, watchWifiConnectEventTimeout); message != "" {
		return message
	}
	return r.collectWifiConnectStatus(ctx, serial, watchWifiConnectStatusTimeout)
}

func (r watchOperationRunner) collectWifiConnectEvents(ctx context.Context, serial string, timeout time.Duration) string {
	diagCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	message := adbdiag.CollectWifiConnectEventState(diagCtx, adb.Client{
		Path:    r.adbPath,
		Serial:  serial,
		Timeout: timeout,
	}).LogMessage()
	if message == "" {
		return ""
	}
	return message
}

func (r watchOperationRunner) collectWifiConnectStatus(ctx context.Context, serial string, timeout time.Duration) string {
	diagCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	message := adbdiag.CollectWifiConnectStatusState(diagCtx, adb.Client{
		Path:    r.adbPath,
		Serial:  serial,
		Timeout: timeout,
	}).LogMessage()
	if message == "" {
		return ""
	}
	return message
}

func watchAgentADBSerial(agent control.AgentInfo) string {
	if agent.Hello == nil {
		return ""
	}
	return agent.Hello.GetAdbSerial()
}

func watchFailureCauseUsesWifiDiagnostics(cause watch.FailureCauseContext) bool {
	switch cause.Operation.Name {
	case "wifi.connect", "wifi.wait":
		return true
	}
	return cause.Step.Type == "connect" || cause.Step.Type == "wait_connected"
}

type watchEventPipe struct {
	C      chan watch.Event
	mu     sync.Mutex
	closed bool
}

func newWatchEventPipe(size int) *watchEventPipe {
	return &watchEventPipe{C: make(chan watch.Event, size)}
}

func (p *watchEventPipe) Emit(event watch.Event) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	select {
	case p.C <- event:
	default:
	}
}

func (p *watchEventPipe) Close() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	close(p.C)
	p.closed = true
}

func watchSessionLogHandler(pipe *watchEventPipe) func(control.LogEvent) {
	return func(event control.LogEvent) {
		watchEvent, ok := watchSessionLogEvent(event)
		if !ok {
			return
		}
		pipe.Emit(watchEvent)
	}
}

func watchSessionLogEvent(event control.LogEvent) (watch.Event, bool) {
	if event.Level == controlpb.CommandLog_LEVEL_DEBUG || event.Level == controlpb.CommandLog_LEVEL_INFO {
		return watch.Event{}, false
	}
	return watch.Event{
		Time: event.Time,
		Kind: watch.EventLog,
		Agent: watch.AgentSnapshot{
			ID:        event.AgentID,
			SessionID: event.SessionID,
		},
		Message: watchSessionLogMessage(event),
	}, true
}

func watchSessionLogMessage(event control.LogEvent) string {
	level := watchSessionLogLevelName(event.Level)
	agent := event.AgentID
	if agent == "" {
		agent = "unknown"
	}
	if event.CommandID != "" {
		return fmt.Sprintf("[%s agent=%s command=%s] %s", level, agent, event.CommandID, event.Message)
	}
	return fmt.Sprintf("[%s agent=%s] %s", level, agent, event.Message)
}

func watchSessionLogLevelName(level controlpb.CommandLog_Level) string {
	switch level {
	case controlpb.CommandLog_LEVEL_DEBUG:
		return "debug"
	case controlpb.CommandLog_LEVEL_WARN:
		return "warn"
	case controlpb.CommandLog_LEVEL_ERROR:
		return "error"
	default:
		return "info"
	}
}

func watchTargetAgents(state *shellState) ([]control.AgentInfo, error) {
	if state.targetAll || state.selected == "" {
		agents := state.server.Agents()
		if len(agents) == 0 {
			return nil, fmt.Errorf("no Android agents connected")
		}
		if state.selected == "" && len(agents) == 1 {
			state.setSelectedAgent(agents[0])
		}
		return agents, nil
	}
	info, err := selectedAgent(state)
	if err != nil {
		return nil, err
	}
	return []control.AgentInfo{info}, nil
}

type watchAgentPlan struct {
	Agent control.AgentInfo
	Plan  watch.Plan
}

func watchAgentPlans(plan watch.Plan, agents []control.AgentInfo) ([]watchAgentPlan, watch.Plan, error) {
	if len(agents) == 0 {
		return nil, plan, fmt.Errorf("no Android agents connected")
	}
	snapshots := watchAgentSnapshots(agents)
	plans := make([]watchAgentPlan, len(agents))
	active := make([]bool, len(agents))
	for i, agent := range agents {
		agentPlan := plan
		agentPlan.Targets = nil
		plans[i] = watchAgentPlan{Agent: agent, Plan: agentPlan}
	}
	uiPlan := plan
	uiPlan.Targets = append([]watch.Target(nil), plan.Targets...)
	for targetIndex, target := range plan.Targets {
		selector := strings.TrimSpace(target.Agent)
		if selector == "" {
			for i := range plans {
				plans[i].Plan.Targets = append(plans[i].Plan.Targets, target)
				active[i] = true
			}
			continue
		}
		agentIndex, err := resolveWatchPlanAgent(selector, snapshots)
		if err != nil {
			return nil, plan, fmt.Errorf("targets[%d] %s agent %q: %w", targetIndex, target.DisplayName(), selector, err)
		}
		resolved := watchAgentStableSelector(snapshots[agentIndex])
		target.Agent = resolved
		uiPlan.Targets[targetIndex].Agent = resolved
		plans[agentIndex].Plan.Targets = append(plans[agentIndex].Plan.Targets, target)
		active[agentIndex] = true
	}
	result := make([]watchAgentPlan, 0, len(plans))
	for i, agentPlan := range plans {
		if active[i] {
			result = append(result, agentPlan)
		}
	}
	if len(result) == 0 {
		return nil, plan, fmt.Errorf("watch plan has no targets for the selected agents")
	}
	return result, uiPlan, nil
}

func resolveWatchPlanAgent(selector string, agents []watch.AgentSnapshot) (int, error) {
	resolved, err := watch.ResolveAgentSnapshot(selector, agents)
	if err != nil {
		return 0, err
	}
	for i, agent := range agents {
		if sameWatchAgentSnapshot(agent, resolved) {
			return i, nil
		}
	}
	return 0, fmt.Errorf("agent serial %q is not selected", selector)
}

func sameWatchAgentSnapshot(a watch.AgentSnapshot, b watch.AgentSnapshot) bool {
	return a.ID == b.ID &&
		a.SessionID == b.SessionID &&
		a.Name == b.Name &&
		a.ADBSerial == b.ADBSerial &&
		a.DeviceModel == b.DeviceModel
}

func watchAgentStableSelector(agent watch.AgentSnapshot) string {
	for _, value := range []string{agent.ADBSerial, agent.ID, agent.Name, agent.SessionID} {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return agent.DisplayName()
}

func watchAgentsFromPlans(plans []watchAgentPlan) []control.AgentInfo {
	agents := make([]control.AgentInfo, 0, len(plans))
	for _, plan := range plans {
		agents = append(agents, plan.Agent)
	}
	return agents
}

func watchAgentSnapshots(agents []control.AgentInfo) []watch.AgentSnapshot {
	snapshots := make([]watch.AgentSnapshot, 0, len(agents))
	for _, agent := range agents {
		snapshots = append(snapshots, watch.AgentSnapshotFromInfo(agent))
	}
	return snapshots
}

func collectWatchErrors(errCh <-chan error) error {
	var first error
	for err := range errCh {
		if err != nil && first == nil {
			first = err
		}
	}
	return first
}

func parseWatchOptions(args []string) (watchOptions, error) {
	var opts watchOptions
	for i := 0; i < len(args); i++ {
		name, value, hasValue := strings.Cut(args[i], "=")
		switch name {
		case "-c", "--config":
			if !hasValue {
				if i+1 >= len(args) {
					return opts, fmt.Errorf("%s requires a value", name)
				}
				i++
				value = args[i]
			}
			opts.configPath = value
		case "--target":
			if !hasValue {
				if i+1 >= len(args) {
					return opts, fmt.Errorf("--target requires a value")
				}
				i++
				value = args[i]
			}
			opts.target = value
		case "--jsonl":
			if !hasValue {
				if i+1 >= len(args) {
					return opts, fmt.Errorf("--jsonl requires a value")
				}
				i++
				value = args[i]
			}
			opts.jsonlPath = value
		case "--no-tui":
			opts.noTUI = true
		case "-h", "--help", "help":
			return opts, errWatchHelpRequested
		default:
			return opts, fmt.Errorf("unknown watch option %q", args[i])
		}
	}
	if opts.configPath == "" {
		return opts, fmt.Errorf("watch requires -c CONFIG.yml")
	}
	return opts, nil
}

var errWatchHelpRequested = errors.New("watch help requested")

func writeWatchHelp(w io.Writer) {
	_, _ = fmt.Fprintln(w, "Usage:")
	_, _ = fmt.Fprintln(w, "  dropcheck [flags] watch -c CONFIG.yml [--target TARGET|all] [--jsonl PATH] [--no-tui]")
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "Options:")
	writeHelpRows(w, []helpRow{
		{"-c, --config PATH", "watch YAML configuration"},
		{"--target TARGET", "agent ID, prefix, adb serial, model, display number, or all; default is all agents in the session"},
		{"--jsonl PATH", "append watch events to JSONL"},
		{"--no-tui", "print findings without starting the live TUI"},
	})
}
