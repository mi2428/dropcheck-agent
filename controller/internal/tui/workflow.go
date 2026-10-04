package tui

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"dropcheck/controller/internal/harness"

	tea "charm.land/bubbletea/v2"
)

// WorkflowOptions accepts either an already compiled Go Plan or a YAML loader.
// Load may establish a metadata session, but must not dispatch any operation.
// Returned errors and all compiled previews/reports must be credential-free.
type WorkflowOptions struct {
	Path     string
	Compiled *harness.CompiledPlan
	Runner   harness.OperationRunner
	Load     func(context.Context, string) (*harness.CompiledPlan, harness.OperationRunner, error)
}

// RunWorkflow uses the shared runtime; this UI never owns a measurement loop.
// The caller owns the metadata/transport session and closes it after return.
func RunWorkflow(ctx context.Context, options WorkflowOptions) error {
	feedCtx, stopFeed := context.WithCancel(ctx)
	lifetime := &workflowLifetime{}
	defer func() {
		stopFeed()
		lifetime.stop()
	}()
	m := newWorkflow(feedCtx, options, lifetime)
	_, err := tea.NewProgram(m, tea.WithContext(ctx)).Run()
	return err
}

type workflowPhase string

const (
	workflowLoad     workflowPhase = "load"
	workflowSelect   workflowPhase = "select"
	workflowPreview  workflowPhase = "preview"
	workflowRunning  workflowPhase = "running"
	workflowStopping workflowPhase = "stopping"
	workflowReview   workflowPhase = "review"
)

type workflowRun struct {
	events chan harness.Event
	result chan workflowFinished
	done   chan struct{}
	cancel context.CancelFunc
}

type workflowLifetime struct {
	mu  sync.Mutex
	run *workflowRun
}

func (l *workflowLifetime) set(run *workflowRun) {
	l.mu.Lock()
	l.run = run
	l.mu.Unlock()
}

func (l *workflowLifetime) stop() {
	l.mu.Lock()
	run := l.run
	l.mu.Unlock()
	if run != nil {
		run.cancel()
		<-run.done
	}
}

type workflowLoaded struct {
	compiled *harness.CompiledPlan
	runner   harness.OperationRunner
	err      error
}

type workflowEvent struct {
	run   *workflowRun
	event harness.Event
}

type workflowFinished struct {
	run    *workflowRun
	report harness.Report
	err    error
}

type retainedRun struct {
	report    harness.Report
	compiled  *harness.CompiledPlan
	dashboard model
}

type workflowModel struct {
	ctx           context.Context
	options       WorkflowOptions
	lifetime      *workflowLifetime
	phase         workflowPhase
	path          string
	loading       bool
	compiled      *harness.CompiledPlan
	source        *harness.CompiledPlan
	selected      *harness.CompiledPlan
	runner        harness.OperationRunner
	selection     harness.Selection
	group         int
	cursor        int
	count         string
	countEdit     bool
	loop          bool
	message       string
	width         int
	height        int
	active        *workflowRun
	controls      *harness.Controls
	dashboard     model
	liveReport    harness.Report
	liveEvicted   uint64
	runs          []retainedRun
	review        int
	inspect       bool
	stepCursor    int
	detail        bool
	detailLine    int
	scroll        int
	scope         harness.Scope
	scopes        []harness.Scope
	scopePick     bool
	scopeIndex    int
	globalControl string
	pauseAcks     map[string]bool
}

func newWorkflow(ctx context.Context, options WorkflowOptions, lifetime *workflowLifetime) workflowModel {
	m := workflowModel{ctx: ctx, options: options, lifetime: lifetime, phase: workflowLoad, path: options.Path, count: "1", width: 120, height: 32}
	if options.Compiled != nil {
		m.setCompiled(options.Compiled, options.Runner)
	} else if options.Path != "" && options.Load != nil {
		m.loading = true
	}
	return m
}

func (m *workflowModel) setCompiled(compiled *harness.CompiledPlan, runner harness.OperationRunner) {
	m.compiled, m.source, m.runner, m.selected = compiled, compiled, runner, nil
	m.selection = harness.Selection{}
	view := compiled.Preview()
	for _, agent := range view.Agents {
		m.selection.AgentIDs = append(m.selection.AgentIDs, agent.ID)
	}
	for _, target := range view.Targets {
		m.selection.TargetIDs = append(m.selection.TargetIDs, target.ID)
	}
	for _, check := range view.Checks {
		m.selection.CheckIDs = append(m.selection.CheckIDs, check.ID)
	}
	m.phase, m.group, m.cursor, m.message = workflowSelect, 0, 0, ""
}

func (m workflowModel) Init() tea.Cmd {
	if m.compiled == nil && m.path != "" && m.options.Load != nil {
		return tea.Batch(m.load(), tickEverySecond())
	}
	return tickEverySecond()
}

func (m workflowModel) load() tea.Cmd {
	return func() tea.Msg {
		compiled, runner, err := m.options.Load(m.ctx, m.path)
		return workflowLoaded{compiled, runner, err}
	}
}

func (m *workflowModel) preflight() error {
	if m.compiled == nil || m.runner == nil {
		return fmt.Errorf("load a validated Plan and select a connected agent")
	}
	if len(m.compiled.Preview().Checks) > 0 && len(m.selection.CheckIDs) == 0 && len(m.selection.Scopes) == 0 {
		return fmt.Errorf("select at least one check")
	}
	selected, err := m.compiled.Select(m.selection)
	if err != nil {
		return err
	}
	if !m.loop {
		count, err := strconv.ParseUint(m.count, 10, 64)
		if err != nil || count == 0 {
			return fmt.Errorf("round count must be a positive integer")
		}
	}
	m.selected = selected
	return nil
}

func (m *workflowModel) start() tea.Cmd {
	if err := m.preflight(); err != nil {
		m.message = err.Error()
		return nil
	}
	// UI cancellation and operator run-stop have different lifetimes. Cleanup
	// remains the runtime's responsibility, including an unexpected UI exit.
	runCtx, cancel := context.WithCancel(context.WithoutCancel(m.ctx))
	run := &workflowRun{events: make(chan harness.Event, 128), result: make(chan workflowFinished, 1), done: make(chan struct{}), cancel: cancel}
	m.active = run
	m.controls = harness.NewControls()
	m.pauseAcks = map[string]bool{}
	m.scope, m.scopes, m.scopePick = harness.Scope{}, nil, false
	m.inspect, m.detail, m.message, m.globalControl = false, false, "", "running"
	m.phase = workflowRunning
	m.liveReport = harness.Report{State: "running"}
	m.liveEvicted = 0
	view := m.selected.Preview()
	m.dashboard = newModelWithChecks(view.Name, view.Targets, view.Checks, nil, view.Agents)
	m.dashboard.width, m.dashboard.height = m.width, max(1, m.height-3)
	compiled, runner, controls, feedCtx := m.selected, m.runner, m.controls, m.ctx
	options := harness.ExecuteOptions{Loop: m.loop, Controls: controls, Sinks: []harness.Sink{workflowSink{feedCtx, run.events}}}
	if !m.loop {
		options.Rounds, _ = strconv.ParseUint(m.count, 10, 64) // Checked by preflight.
	}
	m.lifetime.set(run)
	go func() {
		defer close(run.done)
		defer cancel()
		report, err := harness.Execute(runCtx, compiled, runner, options)
		run.result <- workflowFinished{run, report, err}
		close(run.events)
	}()
	return waitWorkflow(run)
}

type workflowSink struct {
	uiCtx  context.Context
	events chan<- harness.Event
}

func (sink workflowSink) Emit(ctx context.Context, event harness.Event) error {
	// Run cancellation must not drop its final results/cleanup events. An exited
	// UI cancels this separate delivery context so a producer cannot deadlock.
	select {
	case sink.events <- event:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-sink.uiCtx.Done():
		return sink.uiCtx.Err()
	}
}

func waitWorkflow(run *workflowRun) tea.Cmd {
	return func() tea.Msg {
		if event, ok := <-run.events; ok {
			return workflowEvent{run, event}
		}
		return <-run.result
	}
}

func (m workflowModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case workflowLoaded:
		m.loading = false
		if msg.err != nil {
			m.message = msg.err.Error()
			return m, nil
		}
		if msg.compiled == nil || msg.runner == nil {
			m.message = "Plan loader returned no compiled Plan or runner"
			return m, nil
		}
		m.setCompiled(msg.compiled, msg.runner)
	case workflowEvent:
		if msg.run != m.active {
			return m, nil
		}
		event := msg.event
		m.dashboard.apply(event)
		m.liveReport.RunID = event.RunID
		if len(event.Agents) > 0 {
			m.liveReport.Agents = slices.Clone(event.Agents)
		}
		if event.Kind == harness.EventStepFinished && event.Report != nil {
			m.liveReport.Steps = append(m.liveReport.Steps, *event.Report)
			// ponytail: live inspector keeps 512 completed steps; canonical final
			// Report/core reducer own long retention, not another UI evaluator.
			if len(m.liveReport.Steps) > 512 {
				m.liveReport.Steps = slices.Clone(m.liveReport.Steps[len(m.liveReport.Steps)-512:])
				m.liveEvicted++
			}
		}
		m.rememberScope(event.Scope)
		if event.Scope.Kind == harness.ScopeRun {
			switch event.Kind {
			case harness.EventControlRequested:
				m.globalControl = event.Status + " requested"
				if event.Status == "pause" {
					clear(m.pauseAcks)
					m.globalControl = "pausing"
				}
			case harness.EventControlApplied:
				m.globalControl = event.Status
			}
		}
		if event.Kind == harness.EventControlApplied && event.Scope.AgentID != "" {
			if event.Status == "paused" {
				m.pauseAcks[event.Scope.AgentID] = true
			} else if event.Status == "running" {
				delete(m.pauseAcks, event.Scope.AgentID)
			}
			if m.controls.Paused(harness.Scope{Kind: harness.ScopeRun}) && len(m.pauseAcks) == len(m.selected.Preview().Agents) {
				m.globalControl = "paused"
			}
			if !m.controls.Paused(harness.Scope{Kind: harness.ScopeRun}) && len(m.pauseAcks) == 0 {
				m.globalControl = "running"
			}
		}
		return m, waitWorkflow(m.active)
	case workflowFinished:
		if msg.run != m.active {
			return m, nil
		}
		m.runs = append(m.runs, retainedRun{msg.report, m.selected, m.dashboard})
		m.review = len(m.runs) - 1
		m.phase, m.active, m.scopePick = workflowReview, nil, false
		m.stepCursor = min(m.stepCursor, max(0, len(msg.report.Steps)-1))
		if msg.err != nil {
			m.message = msg.err.Error()
		}
		return m, nil // Completion retains review; never tea.Quit.
	case tea.WindowSizeMsg:
		m.width, m.height = max(1, msg.Width), max(1, msg.Height)
		m.dashboard.width, m.dashboard.height = m.width, max(1, m.height-3)
	case tickMsg:
		if m.phase == workflowRunning || m.phase == workflowStopping {
			updated, cmd := m.dashboard.Update(msg)
			m.dashboard = updated.(model)
			return m, cmd
		}
		return m, tickEverySecond()
	case tea.KeyPressMsg:
		return m.updateKey(msg)
	}
	return m, nil
}

func (m workflowModel) updateKey(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	stroke := key.Keystroke()
	dashboard := m.visibleDashboard()
	if stroke == "ctrl+c" || stroke == "q" && m.phase != workflowLoad && !m.countEdit && !dashboard.searchEditing {
		if m.active != nil {
			m.active.cancel()
			m.phase, m.message = workflowStopping, "GLOBAL stop requested; waiting for cleanup and review"
			return m, nil
		}
		return m, tea.Quit
	}
	if m.phase == workflowLoad {
		if m.loading {
			return m, nil
		}
		switch stroke {
		case "enter":
			if m.options.Load == nil || strings.TrimSpace(m.path) == "" || m.loading {
				m.message = "enter an existing YAML Plan path"
				return m, nil
			}
			m.loading, m.message = true, "loading metadata and validating; no operations"
			return m, m.load()
		case "esc":
			if len(m.runs) > 0 {
				m.phase = workflowReview
			}
		default:
			m.path = workflowEditInput(m.path, key)
		}
		return m, nil
	}
	if m.countEdit {
		switch stroke {
		case "enter", "esc":
			m.countEdit = false
		default:
			m.count = workflowEditInput(m.count, key)
		}
		return m, nil
	}
	if m.phase == workflowSelect {
		switch stroke {
		case "tab":
			m.group, m.cursor = (m.group+1)%3, 0
		case "shift+tab":
			m.group, m.cursor = (m.group+2)%3, 0
		case "up", "k":
			m.cursor = max(0, m.cursor-1)
		case "down", "j":
			m.cursor = min(max(0, len(m.selectionRows())-1), m.cursor+1)
		case "space":
			m.toggleSelection()
		case "1":
			m.count, m.loop = "1", false
		case "n":
			m.countEdit, m.loop = true, false
		case "l":
			m.loop = true
		case "enter":
			if err := m.preflight(); err != nil {
				m.message = err.Error()
			} else {
				m.phase, m.message = workflowPreview, ""
			}
		}
		return m, nil
	}
	if m.phase == workflowPreview {
		switch stroke {
		case "enter":
			cmd := m.start()
			return m, cmd
		case "esc":
			m.phase = workflowSelect
		case "up", "k":
			m.scroll = max(0, m.scroll-1)
		case "down", "j":
			m.scroll++
		}
		return m, nil
	}
	if dashboard.searchEditing && !m.inspect && !m.scopePick {
		return m.dashboardKey(key)
	}
	if m.active != nil {
		if stroke == "ctrl+z" {
			m.control("pause", harness.Scope{Kind: harness.ScopeRun})
			return m, nil
		}
		if stroke == "ctrl+n" {
			m.control("skip", harness.Scope{Kind: harness.ScopeRun})
			return m, nil
		}
		if m.scopePick {
			switch stroke {
			case "up", "k":
				m.scopeIndex = max(0, m.scopeIndex-1)
			case "down", "j":
				m.scopeIndex = min(max(0, len(m.scopes)-1), m.scopeIndex+1)
			case "enter":
				if len(m.scopes) > 0 {
					m.scope = m.scopes[m.scopeIndex]
					m.scopePick = false
				}
			case "esc":
				m.scopePick = false
			}
			return m, nil
		}
		switch stroke {
		case "v":
			m.scopePick, m.scopeIndex = true, 0
		case "p", "s", "x":
			action := map[string]string{"p": "pause", "s": "skip", "x": "cancel"}[stroke]
			m.control(action, m.scope)
			return m, nil
		}
	}
	if m.phase == workflowReview {
		switch stroke {
		case "ctrl+z", "ctrl+n":
			m.message = "execution controls are disabled in retained review"
			return m, nil
		case "[", "]":
			if stroke == "[" {
				m.review = max(0, m.review-1)
			} else {
				m.review = min(len(m.runs)-1, m.review+1)
			}
			m.stepCursor, m.detail, m.detailLine = 0, false, 0
			return m, nil
		case "e":
			m.setCompiled(m.source, m.runner)
			return m, nil
		case "o":
			m.phase, m.loading = workflowLoad, false
			return m, nil
		case "r", "f":
			m.prepareRerun(stroke == "f")
			return m, nil
		}
	}
	if stroke == "i" {
		m.inspect, m.detail, m.detailLine = !m.inspect, false, 0
		return m, nil
	}
	if m.inspect {
		switch stroke {
		case "up", "k":
			if m.detail {
				m.detailLine = max(0, m.detailLine-1)
			} else {
				m.stepCursor = max(0, m.stepCursor-1)
			}
		case "down", "j":
			if m.detail {
				m.detailLine++
			} else {
				m.stepCursor = min(max(0, len(m.currentReport().Steps)-1), m.stepCursor+1)
			}
		case "enter":
			m.detail, m.detailLine = true, 0
		case "esc":
			if m.detail {
				m.detail = false
			} else {
				m.inspect = false
			}
		}
		return m, nil
	}
	return m.dashboardKey(key)
}

func (m workflowModel) dashboardKey(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.phase == workflowReview {
		entry := &m.runs[m.review]
		entry.dashboard.width, entry.dashboard.height = m.width, max(1, m.height-3)
		updated, cmd := entry.dashboard.Update(key)
		entry.dashboard = updated.(model)
		return m, cmd
	}
	updated, cmd := m.dashboard.Update(key)
	m.dashboard = updated.(model)
	return m, cmd
}

func (m *workflowModel) visibleDashboard() *model {
	if m.phase == workflowReview && len(m.runs) > 0 {
		return &m.runs[m.review].dashboard
	}
	return &m.dashboard
}

func (m *workflowModel) control(action string, scope harness.Scope) {
	if scope.Kind == "" {
		m.message = "select and confirm an execution scope with v; dashboard focus is not a control scope"
		return
	}
	var err error
	switch action {
	case "pause":
		if scope.Kind != harness.ScopeRun && m.controls.Paused(harness.Scope{Kind: harness.ScopeRun}) {
			m.message = "GLOBAL pause is active; Ctrl-Z resumes GLOBAL, not the selected scope"
			return
		}
		if m.controls.Paused(scope) {
			err = m.controls.Resume(scope)
		} else {
			err = m.controls.Pause(scope)
		}
	case "skip":
		err = m.controls.Skip(scope)
	case "cancel":
		err = m.controls.Cancel(scope)
	}
	if err != nil {
		m.message = err.Error()
	} else {
		m.message = action + " requested for " + workflowScopeLabel(scope)
	}
}

func (m *workflowModel) rememberScope(scope harness.Scope) {
	if scope.AgentID == "" || scope.Kind == harness.ScopeRun || scope.Kind == "" {
		return
	}
	for _, candidate := range []harness.Scope{{Kind: harness.ScopeAgent, AgentID: scope.AgentID}, {Kind: harness.ScopeTarget, AgentID: scope.AgentID, TargetID: scope.TargetID}, scope} {
		if candidate.Kind == harness.ScopeTarget && candidate.TargetID == "" {
			continue
		}
		if !slices.Contains(m.scopes, candidate) {
			m.scopes = append(m.scopes, candidate)
		}
	}
}

func (m *workflowModel) prepareRerun(failed bool) {
	entry := m.runs[m.review]
	selection := harness.Selection{}
	steps := entry.report.Steps
	if !failed {
		if !m.inspect || len(steps) == 0 {
			m.message = "i=inspect results; choose a target/check row, then r=preview selected rerun"
			return
		}
		index := min(max(0, m.stepCursor), len(steps)-1)
		steps = steps[index : index+1]
	}
	for _, step := range steps {
		if failed && step.Outcome != harness.FailOutcome && step.Outcome != harness.MissingOutcome {
			continue
		}
		scope := step.Scope
		if scope.AgentID == "" || scope.TargetID == "" {
			continue
		}
		if scope.CheckID == "connect" || scope.CheckID == "wait_connected" || scope.Kind == harness.ScopeTarget {
			scope.Kind, scope.CheckID = harness.ScopeTarget, ""
		} else {
			scope.Kind = harness.ScopeCheck
		}
		if !slices.Contains(selection.Scopes, scope) {
			selection.Scopes = append(selection.Scopes, scope)
		}
	}
	if len(selection.Scopes) == 0 {
		m.message = "no failed/missing work selected"
		return
	}
	selected, err := entry.compiled.Select(selection)
	if err != nil {
		m.message = err.Error()
		return
	}
	m.compiled, m.selected, m.selection = entry.compiled, selected, selection
	m.phase, m.count, m.loop, m.message = workflowPreview, "1", false, "new run; prerequisites restored by core; agent binding retained"
}

func workflowEditInput(value string, key tea.KeyPressMsg) string {
	if key.Keystroke() == "backspace" {
		_, size := utf8.DecodeLastRuneInString(value)
		if size > 0 {
			return value[:len(value)-size]
		}
	}
	if key.Text != "" && utf8.RuneCountInString(value)+utf8.RuneCountInString(key.Text) <= 4096 {
		return value + sanitizeLogText(key.Text)
	}
	return value
}
