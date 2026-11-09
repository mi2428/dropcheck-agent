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

	"github.com/mi2428/dropcheck-agent/controller/internal/adb"
	"github.com/mi2428/dropcheck-agent/controller/internal/adbdiag"
	"github.com/mi2428/dropcheck-agent/controller/internal/command"
	"github.com/mi2428/dropcheck-agent/controller/internal/control"
	"github.com/mi2428/dropcheck-agent/controller/internal/controlpb"
	"github.com/mi2428/dropcheck-agent/controller/internal/runner"
	"github.com/mi2428/dropcheck-agent/controller/internal/safevalue"
	"github.com/mi2428/dropcheck-agent/controller/internal/session"
	"github.com/mi2428/dropcheck-agent/controller/internal/tui"
	"github.com/mi2428/dropcheck-agent/controller/internal/tuiconfig"
	"github.com/mi2428/dropcheck-agent/controller/internal/tuirun"
)

type tuiOptions struct {
	configPath string
	target     string
	jsonlPath  string
	noTUI      bool
}

func runTUI(ctx context.Context, opts session.Options, args []string) (retErr error) {
	tuiOpts, err := parseTUIOptions(args)
	if err != nil {
		if errors.Is(err, errTUIHelpRequested) {
			writeTUIHelp(os.Stdout)
			return nil
		}
		return err
	}
	plan, err := tuiconfig.LoadFile(tuiOpts.configPath)
	if err != nil {
		return fmt.Errorf("load tui config: %w", err)
	}
	if err := tuirun.Validate(plan); err != nil {
		return fmt.Errorf("validate tui config: %w", err)
	}
	eventPipe := newTUIEventPipe(512)
	defer eventPipe.StopConsumer()
	if !tuiOpts.noTUI {
		opts.OnLog = tuiSessionLogHandler(eventPipe)
	}
	controlSession, err := session.Open(ctx, opts)
	if err != nil {
		return err
	}
	defer func() { retErr = errors.Join(retErr, controlSession.Close()) }()

	agents, err := tuiTargetAgents(controlSession.Server, tuiOpts.target)
	if err != nil {
		return err
	}
	compiled, err := tuirun.Compile(plan, agents)
	if err != nil {
		return err
	}
	preview := compiled.Preview()

	tuiCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	controls := tuirun.NewControls()

	var sinks []tuirun.Sink
	if tuiOpts.jsonlPath != "" {
		jsonlWriter, err := tuirun.OpenJSONLFile(tuiOpts.jsonlPath)
		if err != nil {
			return fmt.Errorf("open jsonl output: %w", err)
		}
		sinks = append(sinks, jsonlWriter)
	}
	sinks = append(sinks, tuirun.ChannelSink{C: eventPipe.C, Done: eventPipe.done})

	errCh := make(chan error, 1)
	go func() {
		opRunner := tuiOperationRunner{operation: runner.New(controlSession.Server), adbPath: opts.ADBPath}
		_, runErr := tuirun.Execute(tuiCtx, compiled, opRunner, tuirun.ExecuteOptions{Loop: true, RoundInterval: plan.RoundInterval, Controls: controls, Sinks: sinks})
		errCh <- runErr
		eventPipe.Close()
		close(errCh)
	}()

	if tuiOpts.noTUI {
		for event := range eventPipe.C {
			printTUINoTUIEvent(os.Stdout, event, len(agents) > 1)
		}
	} else {
		uiErr := tui.RunWithControls(ctx, preview.Name, preview.Targets, preview.Checks, preview.Agents, eventPipe.C, controls)
		eventPipe.StopConsumer()
		cancel()
		return errors.Join(uiErr, collectTUIErrors(errCh))
	}
	cancel()
	if err := collectTUIErrors(errCh); err != nil {
		return err
	}
	return nil
}

func printTUINoTUIEvent(w io.Writer, event tuirun.Event, multiAgent bool) {
	finding, ok := tuiNoTUIFinding(event)
	if !ok {
		return
	}
	agentLabel := ""
	if multiAgent {
		agentLabel = fmt.Sprintf(" agent=%s", safevalue.Normalize(safevalue.Text(event.Agent.DisplayName())))
	}
	_, _ = fmt.Fprintf(w, "%s%s round=%d target=%s check=%s metric=%s observed=%s expected=%s\n",
		event.Time.Format("15:04:05"),
		agentLabel,
		event.Round,
		safevalue.Normalize(safevalue.Text(finding.Target)),
		safevalue.Normalize(safevalue.Text(finding.Check)),
		safevalue.Normalize(safevalue.Text(finding.Metric)),
		safevalue.Normalize(safevalue.Field(finding.Metric, finding.Observed)),
		safevalue.Normalize(safevalue.Field(finding.Metric, finding.Expected)),
	)
}

func tuiNoTUIFinding(event tuirun.Event) (tuirun.Finding, bool) {
	if event.Kind == tuirun.EventFinding && event.Finding != nil {
		return *event.Finding, true
	}
	return tuirun.Finding{}, false
}

type tuiOperationRunner struct {
	operation runner.Runner
	adbPath   string
}

const (
	tuiWifiConnectEventTimeout  = 2 * time.Second
	tuiWifiConnectStatusTimeout = 300 * time.Millisecond
)

func (r tuiOperationRunner) Run(ctx context.Context, agent control.AgentInfo, op command.Operation) (runner.Result, error) {
	return r.operation.Run(ctx, agent, op)
}

func (r tuiOperationRunner) FailureCause(ctx context.Context, agent control.AgentInfo, cause tuirun.FailureCauseContext) string {
	if ctx.Err() != nil || !tuiFailureCauseUsesWifiDiagnostics(cause) {
		return ""
	}
	serial := tuiAgentADBSerial(agent)
	if serial == "" {
		return ""
	}
	return r.collectWifiFailureCause(ctx, serial, 2*time.Second)
}

func (r tuiOperationRunner) ObserveFailureCause(ctx context.Context, agent control.AgentInfo, cause tuirun.FailureCauseContext, emit func(string)) func() {
	if ctx.Err() != nil || !tuiFailureCauseUsesWifiDiagnostics(cause) {
		return nil
	}
	serial := tuiAgentADBSerial(agent)
	if serial == "" {
		return nil
	}
	monitorCtx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Go(func() {
		if monitorCtx.Err() != nil {
			return
		}
		lastConnectState := r.collectWifiConnectStatus(monitorCtx, serial, tuiWifiConnectStatusTimeout)
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
				if monitorCtx.Err() != nil {
					return
				}
				message := r.collectWifiConnectState(monitorCtx, serial)
				if message != "" && message != lastConnectState {
					lastConnectState = message
					emit(message)
				}
			}
		}
	})
	wg.Go(func() {
		if monitorCtx.Err() != nil {
			return
		}
		baseline := r.collectWifiFailureCause(monitorCtx, serial, 1500*time.Millisecond)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-monitorCtx.Done():
				return
			case <-ticker.C:
				if monitorCtx.Err() != nil {
					return
				}
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

func (r tuiOperationRunner) collectWifiFailureCause(ctx context.Context, serial string, timeout time.Duration) string {
	if ctx.Err() != nil {
		return ""
	}
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

func (r tuiOperationRunner) collectWifiConnectState(ctx context.Context, serial string) string {
	if message := r.collectWifiConnectEvents(ctx, serial, tuiWifiConnectEventTimeout); message != "" {
		return message
	}
	return r.collectWifiConnectStatus(ctx, serial, tuiWifiConnectStatusTimeout)
}

func (r tuiOperationRunner) collectWifiConnectEvents(ctx context.Context, serial string, timeout time.Duration) string {
	if ctx.Err() != nil {
		return ""
	}
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

func (r tuiOperationRunner) collectWifiConnectStatus(ctx context.Context, serial string, timeout time.Duration) string {
	if ctx.Err() != nil {
		return ""
	}
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

func tuiAgentADBSerial(agent control.AgentInfo) string {
	if agent.Hello == nil {
		return ""
	}
	return agent.Hello.GetAdbSerial()
}

func tuiFailureCauseUsesWifiDiagnostics(cause tuirun.FailureCauseContext) bool {
	switch cause.Operation.Name {
	case "wifi.connect", "wifi.wait":
		return true
	}
	return cause.Step.Type == "connect" || cause.Step.Type == "wait_connected"
}

type tuiEventPipe struct {
	C       chan tuirun.Event
	done    chan struct{}
	mu      sync.Mutex
	closed  bool
	stopped bool
}

func newTUIEventPipe(size int) *tuiEventPipe {
	return &tuiEventPipe{C: make(chan tuirun.Event, size), done: make(chan struct{})}
}

// StopConsumer detaches the UI without interrupting durable sinks or cleanup.
func (p *tuiEventPipe) StopConsumer() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.stopped {
		p.stopped = true
		close(p.done)
	}
}

func (p *tuiEventPipe) Emit(event tuirun.Event) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || p.stopped {
		return
	}
	select {
	case p.C <- event:
	default:
	}
}

func (p *tuiEventPipe) Close() {
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

func tuiSessionLogHandler(pipe *tuiEventPipe) func(control.LogEvent) {
	return func(event control.LogEvent) {
		tuiEvent, ok := tuiSessionLogEvent(event)
		if !ok {
			return
		}
		pipe.Emit(tuiEvent)
	}
}

func tuiSessionLogEvent(event control.LogEvent) (tuirun.Event, bool) {
	if event.Level == controlpb.CommandLog_LEVEL_DEBUG || event.Level == controlpb.CommandLog_LEVEL_INFO {
		return tuirun.Event{}, false
	}
	return tuirun.Event{
		Time: event.Time,
		Kind: tuirun.EventLog,
		Agent: tuirun.AgentSnapshot{
			ID:        event.AgentID,
			SessionID: event.SessionID,
		},
		Message: tuiSessionLogMessage(event),
	}, true
}

func tuiSessionLogMessage(event control.LogEvent) string {
	level := tuiSessionLogLevelName(event.Level)
	agent := event.AgentID
	if agent == "" {
		agent = "unknown"
	}
	if event.CommandID != "" {
		return fmt.Sprintf("[%s agent=%s command=%s] %s", level, agent, event.CommandID, event.Message)
	}
	return fmt.Sprintf("[%s agent=%s] %s", level, agent, event.Message)
}

func tuiSessionLogLevelName(level controlpb.CommandLog_Level) string {
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

func tuiTargetAgents(server *control.Server, target string) ([]control.AgentInfo, error) {
	if target != "" && target != "all" {
		agent, err := resolveAgent(server, target)
		if err != nil {
			return nil, err
		}
		return []control.AgentInfo{agent}, nil
	}
	agents := server.Agents()
	if len(agents) == 0 {
		return nil, errors.New("no Android agents connected")
	}
	return agents, nil
}

func collectTUIErrors(errCh <-chan error) error {
	var failures []error
	for err := range errCh {
		failures = append(failures, err)
	}
	return errors.Join(failures...)
}

func parseTUIOptions(args []string) (tuiOptions, error) {
	var opts tuiOptions
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
			return opts, errTUIHelpRequested
		default:
			return opts, fmt.Errorf("unknown tui option %q", args[i])
		}
	}
	if opts.configPath == "" {
		return opts, fmt.Errorf("tui requires -c CONFIG.yml")
	}
	return opts, nil
}

var errTUIHelpRequested = errors.New("tui help requested")

func writeTUIHelp(w io.Writer) {
	_, _ = fmt.Fprintln(w, "Usage:")
	_, _ = fmt.Fprintln(w, "  dropcheck [flags] tui -c CONFIG.yml [--target TARGET|all] [--jsonl PATH] [--no-tui]")
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintln(w, "TUI options:")
	writeHelpRows(w, []helpRow{
		{"-c, --config PATH", "YAML plan for continuous monitoring"},
		{"--target TARGET", "agent ID, prefix, adb serial, model, display number, or all; default is all agents in the session"},
		{"--jsonl PATH", "append tui events to JSONL"},
		{"--no-tui", "print findings without starting the tui dashboard"},
	})
}
