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
	"dropcheck/controller/internal/harness"
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

func runWatch(ctx context.Context, opts shellOptions, args []string) (retErr error) {
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
	compiled, err := harness.Compile(plan, agents)
	if err != nil {
		return err
	}
	preview := compiled.Preview()

	watchCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	controls := harness.NewControls()

	var sinks []harness.Sink
	if watchOpts.jsonlPath != "" {
		jsonlWriter, err := harness.OpenJSONLFile(watchOpts.jsonlPath)
		if err != nil {
			return fmt.Errorf("open jsonl output: %w", err)
		}
		sinks = append(sinks, jsonlWriter)
	}
	sinks = append(sinks, harness.ChannelSink{C: eventPipe.C})

	errCh := make(chan error, 1)
	go func() {
		opRunner := watchOperationRunner{operation: runner.New(controlSession.Server), adbPath: opts.ADBPath}
		_, runErr := harness.Execute(watchCtx, compiled, opRunner, harness.ExecuteOptions{Loop: true, RoundInterval: plan.RoundInterval, Controls: controls, Sinks: sinks})
		errCh <- runErr
		eventPipe.Close()
		close(errCh)
	}()

	if watchOpts.noTUI {
		for event := range eventPipe.C {
			printWatchNoTUIEvent(os.Stdout, event, len(agents) > 1)
		}
	} else if err := tui.RunWithControls(ctx, preview.Name, preview.Targets, preview.Checks, preview.Agents, eventPipe.C, controls); err != nil {
		cancel()
		return errors.Join(err, collectWatchErrors(errCh))
	}
	cancel()
	if err := collectWatchErrors(errCh); err != nil {
		return err
	}
	return nil
}

func printWatchNoTUIEvent(w io.Writer, event harness.Event, multiAgent bool) {
	finding, ok := watchNoTUIFinding(event)
	if !ok {
		return
	}
	agentLabel := ""
	if multiAgent {
		agentLabel = fmt.Sprintf(" agent=%s", event.Agent.DisplayName())
	}
	_, _ = fmt.Fprintf(w, "%s%s round=%d target=%s check=%s metric=%s observed=%s expected=%s\n",
		event.Time.Format("15:04:05"),
		agentLabel,
		event.Round,
		finding.Target,
		finding.Check,
		finding.Metric,
		finding.Observed,
		finding.Expected,
	)
}

func watchNoTUIFinding(event harness.Event) (harness.Finding, bool) {
	if event.Kind == harness.EventFinding && event.Finding != nil {
		return *event.Finding, true
	}
	return harness.Finding{}, false
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

func (r watchOperationRunner) FailureCause(ctx context.Context, agent control.AgentInfo, cause harness.FailureCauseContext) string {
	if !watchFailureCauseUsesWifiDiagnostics(cause) {
		return ""
	}
	serial := watchAgentADBSerial(agent)
	if serial == "" {
		return ""
	}
	return r.collectWifiFailureCause(ctx, serial, 2*time.Second)
}

func (r watchOperationRunner) WatchFailureCause(ctx context.Context, agent control.AgentInfo, cause harness.FailureCauseContext, emit func(string)) func() {
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

func watchFailureCauseUsesWifiDiagnostics(cause harness.FailureCauseContext) bool {
	switch cause.Operation.Name {
	case "wifi.connect", "wifi.wait":
		return true
	}
	return cause.Step.Type == "connect" || cause.Step.Type == "wait_connected"
}

type watchEventPipe struct {
	C      chan harness.Event
	mu     sync.Mutex
	closed bool
}

func newWatchEventPipe(size int) *watchEventPipe {
	return &watchEventPipe{C: make(chan harness.Event, size)}
}

func (p *watchEventPipe) Emit(event harness.Event) {
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

func watchSessionLogEvent(event control.LogEvent) (harness.Event, bool) {
	if event.Level == controlpb.CommandLog_LEVEL_DEBUG || event.Level == controlpb.CommandLog_LEVEL_INFO {
		return harness.Event{}, false
	}
	return harness.Event{
		Time: event.Time,
		Kind: harness.EventLog,
		Agent: harness.AgentSnapshot{
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
	return state.commandTargets()
}

func collectWatchErrors(errCh <-chan error) error {
	var failures []error
	for err := range errCh {
		failures = append(failures, err)
	}
	return errors.Join(failures...)
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
	_, _ = fmt.Fprintln(w, "Controller TUI options:")
	writeHelpRows(w, []helpRow{
		{"-c, --config PATH", "watch YAML plan for the Controller TUI"},
		{"--target TARGET", "agent ID, prefix, adb serial, model, display number, or all; default is all agents in the session"},
		{"--jsonl PATH", "append Controller TUI events to JSONL"},
		{"--no-tui", "print findings without starting the Controller TUI"},
	})
}
