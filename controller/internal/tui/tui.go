package tui

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"dropcheck/controller/internal/watch"
	"github.com/charmbracelet/x/ansi"
)

type targetState struct {
	agent        watch.AgentSnapshot
	target       watch.TargetSnapshot
	status       string
	currentStep  string
	steps        []stepState
	plannedSteps []stepState
}

type stepState struct {
	name    string
	typ     string
	status  string
	message string
}

func plannedStepsForTarget(target watch.Target, checks []watch.Check) []stepState {
	if checks == nil {
		return nil
	}
	steps := plannedStepsForChecks(checks)
	if target.DisconnectAfter == nil || *target.DisconnectAfter {
		steps = append(steps, stepState{name: "disconnect", typ: "cleanup"})
	}
	if target.ForgetAfter != nil && *target.ForgetAfter {
		steps = append(steps, stepState{name: "forget", typ: "cleanup"})
	}
	return steps
}

func plannedStepsForChecks(checks []watch.Check) []stepState {
	if checks == nil {
		return nil
	}
	steps := []stepState{
		{name: "connect", typ: "connect"},
		{name: "wait_connected", typ: "wait_connected"},
	}
	for _, check := range checks {
		steps = append(steps, stepState{name: check.DisplayName(), typ: check.Type})
	}
	return steps
}

type failedCheckState struct {
	round   uint64
	when    time.Time
	agent   watch.AgentSnapshot
	target  watch.TargetSnapshot
	finding watch.Finding
}

type failedCheckSummary struct {
	last        time.Time
	count       int
	failPercent int
	failStreak  int
	agent       watch.AgentSnapshot
	target      watch.TargetSnapshot
	finding     watch.Finding
}

type passingCheckState struct {
	round    uint64
	when     time.Time
	agent    watch.AgentSnapshot
	target   watch.TargetSnapshot
	step     watch.StepSnapshot
	duration int64
}

type passingCheckSummary struct {
	last          time.Time
	count         int
	agent         watch.AgentSnapshot
	target        watch.TargetSnapshot
	step          watch.StepSnapshot
	duration      int64
	durationTotal int64
	durationCount int
	maxDuration   int64
}

type failureHotspotSummary struct {
	agent         watch.AgentSnapshot
	target        watch.TargetSnapshot
	last          time.Time
	failCount     int
	failRunCount  int
	runCount      int
	failStreak    int
	latestCause   string
	latestFinding watch.Finding
}

type eventLogEntry struct {
	when  time.Time
	event watch.Event
	line  string
}

type failureHotspotRun struct {
	last   time.Time
	failed bool
}

type occurrenceHistogram struct {
	first       time.Time
	last        time.Time
	bucketWidth time.Duration
	counts      []int
	max         int
	count       int
}

type outcomeEvent struct {
	when   time.Time
	agent  watch.AgentSnapshot
	target watch.TargetSnapshot
	status string
}

type failedCheckAttempt struct {
	when       time.Time
	failedKeys map[string]bool
}

type outcomeBucket struct {
	ok     int
	failed int
}

type targetRoundBucket struct {
	seen          bool
	failed        int
	connectFailed bool
}

type runQueueLine struct {
	text    string
	status  string
	current bool
}

type focusPanel int

const (
	focusPassingChecks focusPanel = iota
	focusFailedChecks
	focusFailureHotspots
)

type focusSlot struct {
	panel           focusPanel
	hotspotAgentKey string
}

const (
	roundTimelineMinVisibleRounds = 10
	roundTimelineTileGap          = 1
	summarySparklineRows          = 5
	summarySparklineWindow        = 30 * time.Minute
	checkHistoryRetentionWindow   = summarySparklineWindow
	eventLogRetentionWindow       = summarySparklineWindow
	maxPassingCheckHistory        = 20000
	maxFailedCheckHistory         = 10000
	maxEventLogHistory            = 10000
	visibleEventLogLimit          = 400
	detailModalHeightPercent      = 55
	detailModalLogLimit           = 120
	recencyFreshWindow            = 15 * time.Second
	recencyWarmWindow             = 90 * time.Second
)

type eventMsg watch.Event
type closedMsg struct{}
type tickMsg time.Time

type keyMap struct {
	quit key.Binding
}

type model struct {
	title                string
	events               <-chan watch.Event
	keys                 keyMap
	width                int
	height               int
	now                  time.Time
	round                uint64
	roundStatus          string
	phase                string
	targets              []targetState
	targetIndex          map[string]int
	agents               []watch.AgentSnapshot
	checks               []watch.Check
	multiAgent           bool
	failedChecks         []failedCheckState
	passingChecks        []passingCheckState
	logs                 []string
	eventLogEntries      []eventLogEntry
	closed               bool
	focus                focusPanel
	focusHotspotAgentKey string
	passingCheckCursor   int
	failedCheckCursor    int
	failureHotspotCursor int
	runQueueCursor       int
	runQueueOffset       int
	searchEditing        bool
	searchQuery          string
	detailOpen           bool
	detailPanel          focusPanel
	detailPassingKey     string
	detailFailedKey      string
	detailHotspotKey     string
	eventLogTarget       string
	eventLogStep         string
	eventLogLast         string
}

// Run renders watch events until the user quits or ctx is canceled.
func Run(ctx context.Context, title string, targets []watch.Target, checks []watch.Check, agents []watch.AgentSnapshot, events <-chan watch.Event) error {
	m := newModelWithChecks(title, targets, checks, events, agents)
	_, err := tea.NewProgram(m, tea.WithContext(ctx)).Run()
	if err != nil && ctx.Err() != nil {
		return nil
	}
	if errors.Is(err, tea.ErrProgramKilled) {
		return nil
	}
	return err
}

func newModel(title string, targets []watch.Target, events <-chan watch.Event, agentSets ...[]watch.AgentSnapshot) model {
	return newModelWithChecks(title, targets, nil, events, agentSets...)
}

func newModelWithChecks(title string, targets []watch.Target, checks []watch.Check, events <-chan watch.Event, agentSets ...[]watch.AgentSnapshot) model {
	if strings.TrimSpace(title) == "" {
		title = "dropcheck watch"
	}
	agents := []watch.AgentSnapshot(nil)
	if len(agentSets) > 0 {
		agents = agentSets[0]
	}
	states := make([]targetState, 0, max(1, len(agents))*len(targets))
	index := make(map[string]int, max(1, len(agents))*len(targets))
	addTarget := func(agent watch.AgentSnapshot, target watch.Target) {
		snapshot := watch.TargetSnapshot{
			Name:  target.DisplayName(),
			SSID:  target.SSID,
			BSSID: target.BSSID,
			Band:  target.Band,
		}
		key := targetStateKey(agent, snapshot)
		index[key] = len(states)
		states = append(states, targetState{agent: agent, target: snapshot, status: "pending", plannedSteps: plannedStepsForTarget(target, checks)})
	}
	if len(agents) == 0 {
		for _, target := range targets {
			addTarget(watch.AgentSnapshot{}, target)
		}
	} else {
		for _, agent := range agents {
			for _, target := range targets {
				addTarget(agent, target)
			}
		}
	}
	return model{
		title:       title,
		events:      events,
		keys:        keyMap{quit: key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl-c", "quit"))},
		targets:     states,
		targetIndex: index,
		agents:      agents,
		checks:      checks,
		multiAgent:  len(agents) > 1,
		now:         time.Now(),
		roundStatus: "starting",
		phase:       "starting",
		focus:       focusFailedChecks,
		detailPanel: focusFailedChecks,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(waitEvent(m.events), tickEverySecond())
}

func waitEvent(events <-chan watch.Event) tea.Cmd {
	return func() tea.Msg {
		event, ok := <-events
		if !ok {
			return closedMsg{}
		}
		return eventMsg(event)
	}
}

func tickEverySecond() tea.Cmd {
	return tea.Every(time.Second, func(now time.Time) tea.Msg {
		return tickMsg(now)
	})
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if key.Matches(msg, m.keys.quit) {
			return m, tea.Quit
		}
		if m.searchEditing {
			m.handleSearchKey(msg)
			return m, nil
		}
		switch msg.String() {
		case "tab":
			m.focusNextPanel()
			if m.detailOpen {
				m.detailPanel = m.focus
				m.lockDetailToCursor(m.focus)
			}
		case "/":
			m.searchEditing = true
		case "j", "down":
			m.moveFocusedCursor(1)
			if m.detailOpen {
				m.detailPanel = m.focus
				m.lockDetailToCursor(m.focus)
			}
		case "k", "up":
			m.moveFocusedCursor(-1)
			if m.detailOpen {
				m.detailPanel = m.focus
				m.lockDetailToCursor(m.focus)
			}
		case "ctrl+d", "pgdown":
			m.moveFocusedCursor(10)
			if m.detailOpen {
				m.detailPanel = m.focus
				m.lockDetailToCursor(m.focus)
			}
		case "ctrl+u", "pgup":
			m.moveFocusedCursor(-10)
			if m.detailOpen {
				m.detailPanel = m.focus
				m.lockDetailToCursor(m.focus)
			}
		case "enter":
			switch {
			case m.focus == focusPassingChecks && len(m.filteredPassingCheckSummaries()) > 0:
				m.openDetailForPanel(focusPassingChecks)
			case m.focus == focusFailedChecks && len(m.filteredFailedCheckSummaries()) > 0:
				m.openDetailForPanel(focusFailedChecks)
			case m.focus == focusFailureHotspots && len(m.focusedFailureHotspotRows()) > 0:
				m.openDetailForPanel(focusFailureHotspots)
			}
		case "esc":
			if m.detailOpen {
				m.detailOpen = false
			} else if m.hasSearchFilter() {
				m.clearSearch()
			}
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.updateRunQueueCursor()
		m.normalizeCursors()
		return m, tea.ClearScreen
	case eventMsg:
		m.apply(watch.Event(msg))
		return m, waitEvent(m.events)
	case tickMsg:
		m.now = time.Time(msg)
		return m, tickEverySecond()
	case closedMsg:
		m.closed = true
		m.pushLog("watch event stream closed")
		return m, tea.Quit
	}
	return m, nil
}

func (m model) View() tea.View {
	view := tea.NewView(m.render())
	view.AltScreen = true
	return view
}

func (m *model) apply(event watch.Event) {
	m.pushEventLog(event)
	if event.Round > m.round {
		m.round = event.Round
	}
	switch event.Kind {
	case watch.EventWatchStarted:
	case watch.EventRoundStarted:
		m.round = event.Round
		m.roundStatus = "running"
		m.phase = fmt.Sprintf("round %d", event.Round)
		for i := range m.targets {
			if m.multiAgent && !sameAgent(m.targets[i].agent, event.Agent) {
				continue
			}
			m.targets[i].status = "pending"
			m.targets[i].currentStep = ""
			m.targets[i].steps = nil
		}
	case watch.EventRoundFinished:
		m.roundStatus = event.Status
		m.phase = "idle"
	case watch.EventTargetStarted:
		target := m.ensureTarget(event.Agent, event.Target)
		target.status = "running"
		m.phase = m.eventTargetLabel(event)
		m.eventLogTarget = m.eventTargetLabel(event)
		m.eventLogStep = ""
		m.eventLogLast = "target started"
	case watch.EventTargetFinished:
		target := m.ensureTarget(event.Agent, event.Target)
		target.status = event.Status
		target.currentStep = ""
		if firstNonEmpty(event.Status, target.status) == "ok" {
			m.recordPassingCheck(passingCheckState{
				round:  event.Round,
				when:   eventTime(event),
				agent:  event.Agent,
				target: event.Target,
				step: watch.StepSnapshot{
					Name:   "target",
					Type:   "target",
					Status: "ok",
				},
				duration: event.Duration,
			})
		}
		m.eventLogTarget = m.eventTargetLabel(event)
		m.eventLogStep = ""
		m.eventLogLast = "target " + firstNonEmpty(event.Status, "finished")
	case watch.EventStepStarted:
		target := m.ensureTarget(event.Agent, event.Target)
		target.currentStep = event.Step.Name
		m.phase = m.eventTargetLabel(event) + "/" + event.Step.Name
		upsertStep(target, event.Step)
		m.eventLogTarget = m.eventTargetLabel(event)
		m.eventLogStep = event.Step.Name
		m.eventLogLast = event.Step.Name + " running"
	case watch.EventStepFinished:
		target := m.ensureTarget(event.Agent, event.Target)
		if event.Step.Status != "running" && target.currentStep == event.Step.Name {
			target.currentStep = ""
		}
		upsertStep(target, event.Step)
		if passingCheckEvent(event) {
			m.recordPassingCheck(passingCheckState{
				round:    event.Round,
				when:     eventTime(event),
				agent:    event.Agent,
				target:   event.Target,
				step:     event.Step,
				duration: event.Duration,
			})
		}
		if finding, ok := requiredStepFailedCheck(event); ok {
			m.addFailedCheck(event.Agent, event.Target, event.Round, eventTime(event), finding)
		}
		m.eventLogTarget = m.eventTargetLabel(event)
		m.eventLogStep = event.Step.Name
		m.eventLogLast = event.Step.Name + " " + firstNonEmpty(event.Step.Message, event.Step.Error, event.Step.Status, event.Status)
	case watch.EventFinding:
		if event.Finding != nil {
			m.removePassingCheckForFailedCheck(event)
			m.addFailedCheck(event.Agent, event.Target, event.Round, event.Time, *event.Finding)
			target := m.ensureTarget(event.Agent, event.Target)
			target.status = "failed"
			event.Step.Status = "failed"
			upsertStep(target, event.Step)
			m.failedCheckCursor = m.failedCheckSummaryIndex(event.Agent, event.Target, *event.Finding)
			m.eventLogTarget = m.eventTargetLabel(event)
			m.eventLogStep = firstNonEmpty(event.Step.Name, event.Finding.Check)
			m.eventLogLast = firstNonEmpty(event.Finding.Check, event.Step.Name) + " " + firstNonEmpty(event.Finding.Message, event.Finding.Metric+"="+event.Finding.Observed)
		}
	case watch.EventLog:
	}
	m.updateRunQueueCursor()
}

func (m *model) ensureTarget(agent watch.AgentSnapshot, snapshot watch.TargetSnapshot) *targetState {
	name := snapshot.Name
	if name == "" {
		name = firstNonEmpty(snapshot.SSID, snapshot.BSSID, "target")
		snapshot.Name = name
	}
	key := targetStateKey(agent, snapshot)
	if index, ok := m.targetIndex[key]; ok {
		return &m.targets[index]
	}
	if fallback := checkStatusTargetKey(snapshot); fallback != "" {
		for i := range m.targets {
			if !sameAgent(m.targets[i].agent, agent) || checkStatusTargetKey(m.targets[i].target) != fallback {
				continue
			}
			m.targets[i].target = mergeTargetSnapshot(m.targets[i].target, snapshot)
			m.targetIndex[key] = i
			return &m.targets[i]
		}
	}
	m.targetIndex[key] = len(m.targets)
	m.targets = append(m.targets, targetState{agent: agent, target: snapshot, status: "pending", plannedSteps: plannedStepsForChecks(m.checks)})
	return &m.targets[len(m.targets)-1]
}

func mergeTargetSnapshot(base watch.TargetSnapshot, update watch.TargetSnapshot) watch.TargetSnapshot {
	if update.Name != "" {
		base.Name = update.Name
	}
	if update.SSID != "" {
		base.SSID = update.SSID
	}
	if update.BSSID != "" {
		base.BSSID = update.BSSID
	}
	if update.Band != "" {
		base.Band = update.Band
	}
	return base
}

func (m *model) addFailedCheck(agent watch.AgentSnapshot, target watch.TargetSnapshot, round uint64, when time.Time, finding watch.Finding) {
	if when.IsZero() {
		when = time.Now()
	}
	if target.Name == "" {
		target.Name = firstNonEmpty(target.SSID, target.BSSID, finding.Target, "target")
	}
	m.failedChecks = append(m.failedChecks, failedCheckState{round: round, when: when, agent: agent, target: target, finding: finding})
	m.trimFailedChecks(when)
	m.failedCheckCursor = m.failedCheckSummaryIndex(agent, target, finding)
	m.normalizeCursors()
}

func upsertStep(target *targetState, snapshot watch.StepSnapshot) {
	name := snapshot.Name
	if name == "" {
		name = snapshot.Type
	}
	for i := range target.steps {
		if target.steps[i].name == name {
			target.steps[i] = stepState{name: name, typ: snapshot.Type, status: snapshot.Status, message: firstNonEmpty(snapshot.Message, snapshot.Error)}
			return
		}
	}
	target.steps = append(target.steps, stepState{name: name, typ: snapshot.Type, status: snapshot.Status, message: firstNonEmpty(snapshot.Message, snapshot.Error)})
}

func (m *model) recordPassingCheck(passingCheck passingCheckState) {
	if passingCheck.when.IsZero() {
		passingCheck.when = time.Now()
	}
	if passingCheck.target.Name == "" {
		passingCheck.target.Name = firstNonEmpty(passingCheck.target.SSID, passingCheck.target.BSSID, "target")
	}
	if passingCheck.step.Name == "" {
		passingCheck.step.Name = firstNonEmpty(passingCheck.step.Type, "step")
	}
	m.passingChecks = append(m.passingChecks, passingCheck)
	m.trimPassingChecks(passingCheck.when)
	m.normalizeCursors()
}

func (m *model) trimPassingChecks(reference time.Time) {
	m.passingChecks = trimPassingCheckHistory(m.passingChecks, reference)
}

func (m *model) trimFailedChecks(reference time.Time) {
	m.failedChecks = trimFailedCheckHistory(m.failedChecks, reference)
}

func trimPassingCheckHistory(items []passingCheckState, reference time.Time) []passingCheckState {
	if len(items) == 0 {
		return items
	}
	if reference.IsZero() {
		reference = latestPassingCheckTime(items)
	}
	if !reference.IsZero() {
		cutoff := reference.Add(-checkHistoryRetentionWindow)
		items = filterPassingChecksSince(items, cutoff)
	}
	if len(items) > maxPassingCheckHistory {
		items = items[len(items)-maxPassingCheckHistory:]
	}
	return items
}

func trimFailedCheckHistory(items []failedCheckState, reference time.Time) []failedCheckState {
	if len(items) == 0 {
		return items
	}
	if reference.IsZero() {
		reference = latestFailedCheckTime(items)
	}
	if !reference.IsZero() {
		cutoff := reference.Add(-checkHistoryRetentionWindow)
		items = filterFailedChecksSince(items, cutoff)
	}
	if len(items) > maxFailedCheckHistory {
		items = items[len(items)-maxFailedCheckHistory:]
	}
	return items
}

func filterPassingChecksSince(items []passingCheckState, cutoff time.Time) []passingCheckState {
	filtered := items[:0]
	for _, item := range items {
		if item.when.IsZero() || item.when.Before(cutoff) {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

func filterFailedChecksSince(items []failedCheckState, cutoff time.Time) []failedCheckState {
	filtered := items[:0]
	for _, item := range items {
		if item.when.IsZero() || item.when.Before(cutoff) {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

func latestPassingCheckTime(items []passingCheckState) time.Time {
	var latest time.Time
	for _, item := range items {
		if item.when.After(latest) {
			latest = item.when
		}
	}
	return latest
}

func latestFailedCheckTime(items []failedCheckState) time.Time {
	var latest time.Time
	for _, item := range items {
		if item.when.After(latest) {
			latest = item.when
		}
	}
	return latest
}

func (m *model) removePassingCheckForFailedCheck(event watch.Event) {
	key := passingCheckKey(event.Agent, event.Target, event.Step)
	if key == "" {
		return
	}
	filtered := m.passingChecks[:0]
	for _, passingCheck := range m.passingChecks {
		if passingCheck.round == event.Round && passingCheckKey(passingCheck.agent, passingCheck.target, passingCheck.step) == key {
			continue
		}
		filtered = append(filtered, passingCheck)
	}
	m.passingChecks = filtered
	m.normalizeCursors()
}

func (m *model) pushLog(message string) {
	message = strings.TrimSpace(sanitizeLogText(message))
	if message == "" {
		return
	}
	when := time.Now()
	line := when.Format("15:04:05") + " " + message
	m.pushVisibleLog(line)
	m.eventLogEntries = append(m.eventLogEntries, eventLogEntry{when: when, line: line})
	m.trimEventLogEntries(when)
}

func (m *model) pushVisibleLog(line string) {
	m.logs = append(m.logs, line)
	if len(m.logs) > visibleEventLogLimit {
		m.logs = m.logs[len(m.logs)-visibleEventLogLimit:]
	}
}

func (m *model) moveFailedCheckCursor(delta int) {
	rows := m.filteredFailedCheckSummaries()
	if len(rows) == 0 {
		m.failedCheckCursor = 0
		return
	}
	m.failedCheckCursor = clamp(m.failedCheckCursor+delta, 0, len(rows)-1)
}

func (m *model) movePassingCheckCursor(delta int) {
	rows := m.filteredPassingCheckSummaries()
	if len(rows) == 0 {
		m.passingCheckCursor = 0
		return
	}
	m.passingCheckCursor = clamp(m.passingCheckCursor+delta, 0, len(rows)-1)
}

func (m *model) moveFailureHotspotCursor(delta int) {
	rows := m.focusedFailureHotspotRows()
	if len(rows) == 0 {
		return
	}
	position := -1
	for i, row := range rows {
		if row.index == m.failureHotspotCursor {
			position = i
			break
		}
	}
	if position < 0 {
		position = 0
	} else {
		position = clamp(position+delta, 0, len(rows)-1)
	}
	m.failureHotspotCursor = rows[position].index
}

func (m *model) moveFocusedCursor(delta int) {
	switch m.focus {
	case focusPassingChecks:
		m.movePassingCheckCursor(delta)
	case focusFailureHotspots:
		m.moveFailureHotspotCursor(delta)
	default:
		m.moveFailedCheckCursor(delta)
	}
}

func (m *model) focusNextPanel() {
	slots := m.focusSlots()
	if len(slots) == 0 {
		return
	}
	current := m.currentFocusSlot()
	index := -1
	for i, slot := range slots {
		if slot == current {
			index = i
			break
		}
	}
	if index < 0 {
		for i, slot := range slots {
			if slot.panel == m.focus {
				index = i
				break
			}
		}
	}
	if index < 0 {
		index = 0
	}
	m.setFocusSlot(slots[(index+1)%len(slots)])
}

func (m model) focusSlots() []focusSlot {
	slots := []focusSlot{
		{panel: focusPassingChecks},
		{panel: focusFailedChecks},
	}
	if !m.failureHotspotsVisible() {
		return slots
	}
	if !m.failureHotspotPanelsSplit() {
		return append(slots, focusSlot{panel: focusFailureHotspots})
	}
	for _, agent := range m.failureHotspotAgents() {
		slots = append(slots, focusSlot{panel: focusFailureHotspots, hotspotAgentKey: roundAgentKey(agent)})
	}
	return slots
}

func (m model) currentFocusSlot() focusSlot {
	if m.focus != focusFailureHotspots {
		return focusSlot{panel: m.focus}
	}
	key := ""
	if m.failureHotspotPanelsSplit() {
		key = m.currentHotspotAgentKey()
	}
	return focusSlot{panel: focusFailureHotspots, hotspotAgentKey: key}
}

func (m *model) setFocusSlot(slot focusSlot) {
	m.focus = slot.panel
	if slot.panel != focusFailureHotspots {
		return
	}
	m.focusHotspotAgentKey = slot.hotspotAgentKey
	m.ensureFailureHotspotCursorInFocus()
}

func (m model) currentHotspotAgentKey() string {
	if !m.failureHotspotPanelsSplit() {
		return ""
	}
	agents := m.failureHotspotAgents()
	if len(agents) == 0 {
		return ""
	}
	for _, agent := range agents {
		key := roundAgentKey(agent)
		if key == m.focusHotspotAgentKey {
			return key
		}
	}
	return roundAgentKey(agents[0])
}

func (m *model) normalizeFocusHotspotAgent() {
	if !m.failureHotspotPanelsSplit() {
		m.focusHotspotAgentKey = ""
		return
	}
	m.focusHotspotAgentKey = m.currentHotspotAgentKey()
}

func (m *model) ensureFailureHotspotCursorInFocus() {
	rows := m.focusedFailureHotspotRows()
	if len(rows) == 0 {
		return
	}
	for _, row := range rows {
		if row.index == m.failureHotspotCursor {
			return
		}
	}
	m.failureHotspotCursor = rows[0].index
}

func (m *model) handleSearchKey(msg tea.KeyPressMsg) {
	switch msg.String() {
	case "enter":
		m.searchEditing = false
	case "esc":
		m.searchEditing = false
		if m.searchQuery == "" {
			m.clearSearch()
		}
	case "backspace":
		if m.searchQuery != "" {
			runes := []rune(m.searchQuery)
			m.searchQuery = string(runes[:len(runes)-1])
			m.normalizeCursors()
		}
	case "ctrl+u":
		m.searchQuery = ""
		m.normalizeCursors()
	default:
		value := msg.String()
		if len([]rune(value)) == 1 {
			r := []rune(value)[0]
			if r >= 0x20 && r != 0x7f {
				m.searchQuery += string(r)
				m.normalizeCursors()
			}
		}
	}
}

func (m model) hasSearchFilter() bool {
	return strings.TrimSpace(m.searchQuery) != ""
}

func (m *model) clearSearch() {
	m.searchEditing = false
	m.searchQuery = ""
	m.normalizeCursors()
}

func (m *model) normalizeCursors() {
	m.passingCheckCursor = clamp(m.passingCheckCursor, 0, max(0, len(m.filteredPassingCheckSummaries())-1))
	m.failedCheckCursor = clamp(m.failedCheckCursor, 0, max(0, len(m.filteredFailedCheckSummaries())-1))
	m.failureHotspotCursor = clamp(m.failureHotspotCursor, 0, max(0, len(m.filteredFailureHotspots())-1))
	if m.focus == focusFailureHotspots && !m.failureHotspotsVisible() {
		m.focus = focusFailedChecks
	}
	if m.focus == focusFailureHotspots {
		m.normalizeFocusHotspotAgent()
		m.ensureFailureHotspotCursorInFocus()
	}
	if m.detailOpen {
		m.followLockedDetailItems()
	}
}

func (m *model) openDetailForPanel(panel focusPanel) {
	m.detailOpen = true
	m.detailPanel = panel
	m.lockDetailToCursor(panel)
}

func (m *model) lockDetailToCursor(panel focusPanel) {
	switch panel {
	case focusPassingChecks:
		rows := m.filteredPassingCheckSummaries()
		if len(rows) == 0 {
			m.detailPassingKey = ""
			return
		}
		index := clamp(m.passingCheckCursor, 0, len(rows)-1)
		m.passingCheckCursor = index
		m.detailPassingKey = passingCheckSummaryIdentity(rows[index])
	case focusFailedChecks:
		rows := m.filteredFailedCheckSummaries()
		if len(rows) == 0 {
			m.detailFailedKey = ""
			return
		}
		index := clamp(m.failedCheckCursor, 0, len(rows)-1)
		m.failedCheckCursor = index
		m.detailFailedKey = failedCheckSummaryIdentity(rows[index])
	case focusFailureHotspots:
		m.ensureFailureHotspotCursorInFocus()
		rows := m.focusedFailureHotspotRows()
		if len(rows) == 0 {
			m.detailHotspotKey = ""
			return
		}
		index := 0
		for i, row := range rows {
			if row.index == m.failureHotspotCursor {
				index = i
				break
			}
		}
		m.failureHotspotCursor = rows[index].index
		m.detailHotspotKey = failureHotspotSummaryIdentity(rows[index].item)
	}
}

func (m *model) followLockedDetailItems() {
	if m.detailPassingKey != "" {
		if index := passingCheckSummaryIndexByIdentity(m.filteredPassingCheckSummaries(), m.detailPassingKey); index >= 0 {
			m.passingCheckCursor = index
		}
	}
	if m.detailFailedKey != "" {
		if index := failedCheckSummaryIndexByIdentity(m.filteredFailedCheckSummaries(), m.detailFailedKey); index >= 0 {
			m.failedCheckCursor = index
		}
	}
	if m.detailHotspotKey != "" {
		rows := m.filteredFailureHotspots()
		if index := failureHotspotSummaryIndexByIdentity(rows, m.detailHotspotKey); index >= 0 {
			m.failureHotspotCursor = index
			if m.detailPanel == focusFailureHotspots {
				m.focusHotspotAgentKey = roundAgentKey(rows[index].agent)
			}
		}
	}
}

func (m *model) pushEventLog(event watch.Event) {
	line := strings.TrimSpace(sanitizeLogText(eventLogLine(event)))
	if line == "" {
		return
	}
	when := event.Time
	if when.IsZero() {
		when = time.Now()
	}
	formattedLine := when.Format("15:04:05") + " " + line
	m.pushVisibleLog(formattedLine)
	m.eventLogEntries = append(m.eventLogEntries, eventLogEntry{when: when, event: event, line: formattedLine})
	m.trimEventLogEntries(when)
}

func (m *model) trimEventLogEntries(reference time.Time) {
	if reference.IsZero() {
		return
	}
	cutoff := reference.Add(-eventLogRetentionWindow)
	filtered := m.eventLogEntries[:0]
	for _, entry := range m.eventLogEntries {
		if entry.when.IsZero() || !entry.when.Before(cutoff) {
			filtered = append(filtered, entry)
		}
	}
	m.eventLogEntries = filtered
	if len(m.eventLogEntries) > maxEventLogHistory {
		m.eventLogEntries = m.eventLogEntries[len(m.eventLogEntries)-maxEventLogHistory:]
	}
}

func eventLogLine(event watch.Event) string {
	if event.Kind == watch.EventLog && event.Message != "" {
		return sanitizeLogText(event.Message)
	}
	fields := []string{logField("kind", string(event.Kind))}
	add := func(key string, value string) {
		if field := logField(key, value); field != "" {
			fields = append(fields, field)
		}
	}
	if event.Round > 0 {
		fields = append(fields, fmt.Sprintf("round=%d", event.Round))
	}
	if event.Status != "" {
		add("status", event.Status)
	}
	if event.Agent.DisplayName() != "" {
		add("agent", event.Agent.DisplayName())
	}
	if event.Target.Name != "" {
		add("target", event.Target.Name)
	}
	if event.Target.SSID != "" {
		add("ssid", event.Target.SSID)
	}
	if event.Target.BSSID != "" {
		add("bssid", event.Target.BSSID)
	}
	if event.Target.Band != "" {
		add("band", event.Target.Band)
	}
	if event.Step.Name != "" {
		add("step", event.Step.Name)
	}
	if event.Step.Type != "" {
		add("type", event.Step.Type)
	}
	if event.Step.Operation != "" {
		add("op", event.Step.Operation)
	}
	if event.Step.Status != "" && event.Step.Status != event.Status {
		add("step_status", event.Step.Status)
	}
	if event.Step.Skipped {
		fields = append(fields, "skipped=true")
	}
	if event.Duration > 0 {
		fields = append(fields, fmt.Sprintf("duration_ms=%d", event.Duration))
	}
	if event.Finding != nil {
		add("check", event.Finding.Check)
		add("metric", event.Finding.Metric)
		add("observed", event.Finding.Observed)
		add("expected", event.Finding.Expected)
		if event.Finding.Message != "" {
			add("finding_msg", event.Finding.Message)
		}
	}
	message := firstNonEmpty(event.Message, event.Step.Message, event.Step.Error)
	if message != "" {
		add("msg", message)
	}
	return strings.Join(fields, " ")
}

func logField(key string, value string) string {
	value = strings.TrimSpace(sanitizeLogText(value))
	if value == "" {
		return ""
	}
	if strings.ContainsAny(value, " \t\n\r\"") {
		value = strconv.Quote(value)
	}
	return key + "=" + value
}

func detailValue(value string) string {
	value = strings.TrimSpace(sanitizeLogText(value))
	if strings.ContainsAny(value, " \t\n\"") || strings.HasPrefix(value, "=") {
		return strconv.Quote(value)
	}
	return value
}

func sanitizeLogText(value string) string {
	if value == "" {
		return ""
	}
	var b strings.Builder
	changed := false
	for _, r := range value {
		switch r {
		case '\n':
			b.WriteString(`\n`)
			changed = true
		case '\r':
			b.WriteString(`\r`)
			changed = true
		case '\t':
			b.WriteString(`\t`)
			changed = true
		case '\x1b':
			b.WriteString(`\x1b`)
			changed = true
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&b, "\\x%02x", r)
				changed = true
				continue
			}
			b.WriteRune(r)
		}
	}
	if !changed {
		return value
	}
	return b.String()
}

func (m model) render() string {
	width := m.width
	if width <= 0 {
		width = 120
	}
	height := m.height
	if height <= 0 {
		height = 32
	}
	help := m.helpBar(width)
	status := m.statusBar(width)
	bodyHeight := max(4, height-2)
	runQueueWidth := dashboardRunQueueWidth(width)
	leftWidth := dashboardMainWidth(width)
	roundTimelineHeight, checkStatusHeight, summaryHeight, eventLogHeight := m.dashboardPanelHeights(bodyHeight, panelContentWidth(width))
	lowerHeight := max(0, bodyHeight-checkStatusHeight-roundTimelineHeight)
	summaryGap := 1
	if leftWidth < 24 {
		summaryGap = 0
	}
	showHotspots := m.failureHotspotsVisible()
	passingChecksWidth := 0
	failedChecksWidth := 0
	hotspotsWidth := 0
	eventLogWidth := 0
	if showHotspots {
		summaryWidth := max(3, leftWidth-summaryGap*2)
		base := summaryWidth / 3
		remainder := summaryWidth % 3
		passingChecksWidth = base
		failedChecksWidth = base
		hotspotsWidth = base
		if remainder > 0 {
			passingChecksWidth++
		}
		if remainder > 1 {
			failedChecksWidth++
		}
		eventLogWidth = passingChecksWidth + summaryGap + failedChecksWidth
	} else {
		summaryWidth := max(2, leftWidth-summaryGap)
		passingChecksWidth = summaryWidth / 2
		failedChecksWidth = summaryWidth - passingChecksWidth
		eventLogWidth = passingChecksWidth + summaryGap + failedChecksWidth
	}

	checkStatus := ""
	if checkStatusHeight > 0 {
		checkStatus = renderPanel("Check Status", width, checkStatusHeight, m.checkStatusView(panelContentWidth(width), checkStatusHeight-2))
	}
	roundTimeline := ""
	if roundTimelineHeight > 0 {
		roundTimeline = renderPanel("Round Timeline", width, roundTimelineHeight, m.roundTimelineView(panelContentWidth(width), roundTimelineHeight-2))
	}
	passingChecks := renderPanelFocused("Passing Checks", passingChecksWidth, summaryHeight, m.passingChecksView(panelContentWidth(passingChecksWidth), summaryHeight-2), m.focus == focusPassingChecks)
	failedChecks := renderPanelFocused("Failed Checks", failedChecksWidth, summaryHeight, m.failedChecksView(panelContentWidth(failedChecksWidth), summaryHeight-2), m.focus == focusFailedChecks)
	summaries := lipgloss.JoinHorizontal(lipgloss.Top, passingChecks, horizontalSpacer(summaryGap, summaryHeight), failedChecks)
	eventLog := renderPanel("Event Log", eventLogWidth, eventLogHeight, m.eventLogView(panelContentWidth(eventLogWidth), eventLogHeight-2))
	left := lipgloss.JoinVertical(lipgloss.Left, summaries, eventLog)
	if showHotspots {
		hotspots := m.failureHotspotPanelsView(hotspotsWidth, lowerHeight)
		left = lipgloss.JoinHorizontal(lipgloss.Top, left, horizontalSpacer(summaryGap, lowerHeight), hotspots)
	}
	runQueue := m.runQueuePanelsView(runQueueWidth, lowerHeight)
	lower := lipgloss.JoinHorizontal(lipgloss.Top, left, verticalSpacer(lowerHeight), runQueue)
	body := lower
	if roundTimeline != "" {
		body = lipgloss.JoinVertical(lipgloss.Left, roundTimeline, body)
	}
	if checkStatus != "" {
		body = lipgloss.JoinVertical(lipgloss.Left, checkStatus, body)
	}
	content := lipgloss.JoinVertical(lipgloss.Left, help, body, status)
	frame := appStyle.Width(width).Height(height).Render(content)
	if m.detailOpen {
		frame = overlayModal(frame, width, height, m.detailModal(width, height))
	}
	return frame
}

func (m model) failureHotspotsVisible() bool {
	return dashboardMainWidth(m.width) >= 126
}

func dashboardRunQueueWidth(width int) int {
	if width <= 0 {
		width = 120
	}
	runQueueWidth := clamp(width*25/100, 32, 48)
	if width-runQueueWidth-1 < 50 {
		runQueueWidth = max(24, width-51)
	}
	if width < 80 {
		runQueueWidth = max(20, width/3)
	}
	return runQueueWidth
}

func dashboardMainWidth(width int) int {
	if width <= 0 {
		width = 120
	}
	return max(20, width-dashboardRunQueueWidth(width)-1)
}

func (m model) dashboardPanelHeights(bodyHeight int, roundTimelineWidths ...int) (roundTimelineHeight int, checkStatusHeight int, summaryHeight int, eventLogHeight int) {
	if bodyHeight <= 0 {
		return 0, 0, 0, 0
	}
	if bodyHeight < 14 {
		summaryHeight, eventLogHeight = summaryAndEventLogHeights(bodyHeight)
		return 0, 0, summaryHeight, eventLogHeight
	}
	roundTimelineWidth := 0
	if len(roundTimelineWidths) > 0 {
		roundTimelineWidth = roundTimelineWidths[0]
	} else {
		defaultWidth := m.width
		if defaultWidth <= 0 {
			defaultWidth = 120
		}
		roundTimelineWidth = panelContentWidth(max(20, defaultWidth))
	}
	eventLogHeight = clamp(bodyHeight/6, 4, 7)
	roundTimelineHeight = m.roundTimelinePanelHeight(roundTimelineWidth)
	checkStatusHeight = m.checkStatusPanelHeight()
	summaryHeight = bodyHeight - roundTimelineHeight - checkStatusHeight - eventLogHeight
	for summaryHeight < 8 && roundTimelineHeight > 4 {
		roundTimelineHeight--
		summaryHeight++
	}
	for summaryHeight < 8 && eventLogHeight > 3 {
		eventLogHeight--
		summaryHeight++
	}
	for summaryHeight < 4 && checkStatusHeight > 3 {
		checkStatusHeight--
		summaryHeight++
	}
	if summaryHeight < 4 {
		summaryHeight = max(0, bodyHeight-roundTimelineHeight-checkStatusHeight-eventLogHeight)
	}
	return roundTimelineHeight, checkStatusHeight, summaryHeight, eventLogHeight
}

func (m model) roundTimelinePanelHeight(width int) int {
	contentHeight := 1
	targets := m.checkStatusTargets()
	if len(targets) == 0 {
		contentHeight++ // empty-state line
	} else {
		columns := roundTimelineColumnCount(width, len(targets))
		contentHeight += (len(targets) + columns - 1) / columns
	}
	return clamp(contentHeight+2, 4, 12)
}

func (m model) checkStatusPanelHeight() int {
	contentHeight := 1
	if len(m.checkStatusTargets()) == 0 || len(m.checkStatusChecks()) == 0 {
		contentHeight = 1
	} else {
		contentHeight += len(m.checkStatusChecks())
	}
	return max(3, contentHeight+2)
}

func summaryAndEventLogHeights(bodyHeight int) (summaryHeight int, eventLogHeight int) {
	if bodyHeight <= 0 {
		return 0, 0
	}
	oldFailedChecksHeight := clamp(bodyHeight*63/100, 7, max(3, bodyHeight-6))
	oldEventLogHeight := bodyHeight - oldFailedChecksHeight
	if oldEventLogHeight < 4 && bodyHeight >= 8 {
		oldEventLogHeight = 4
	}
	eventLogHeight = max(2, (oldEventLogHeight*60+50)/100)
	if bodyHeight >= 12 {
		eventLogHeight = max(4, eventLogHeight)
	}
	if bodyHeight >= 8 {
		eventLogHeight = min(eventLogHeight, bodyHeight-4)
	} else {
		eventLogHeight = min(eventLogHeight, max(1, bodyHeight/3))
	}
	summaryHeight = max(0, bodyHeight-eventLogHeight)
	return summaryHeight, eventLogHeight
}

func (m model) helpBar(width int) string {
	now := m.currentTime().Format("15:04:05")
	label := " Keys:"
	parts := []string{"Tab=Panel", "j=Down", "k=Up"}
	if m.focus == focusPassingChecks && len(m.filteredPassingCheckSummaries()) > 0 ||
		m.focus == focusFailedChecks && len(m.filteredFailedCheckSummaries()) > 0 ||
		m.focus == focusFailureHotspots && len(m.focusedFailureHotspotRows()) > 0 {
		parts = append(parts, "Enter=Details")
	}
	parts = append(parts, "/=Filter")
	if m.hasSearchFilter() {
		parts = append(parts, "Esc=Clear")
	}
	parts = append(parts, "Ctrl-D=PageDown", "Ctrl-U=PageUp", "Ctrl-C=Quit")
	keys := " " + strings.Join(parts, " ")
	if m.searchEditing {
		keys = " filter: type text Enter=Apply Esc=Done Backspace=Delete Ctrl-U=Clear Ctrl-C=Quit"
	}
	right := "Now=" + now
	spaces := width - runeLen(label) - runeLen(keys) - runeLen(right) - 1
	if spaces < 1 {
		return valueStyle.Render(fit(label+keys+" "+right, width))
	}
	return keyStyle.Render(label) +
		valueStyle.Render(keys) +
		valueStyle.Render(strings.Repeat(" ", spaces)) +
		keyStyle.Render("Now=") +
		valueStyle.Render(now) +
		valueStyle.Render(" ")
}

func (m model) currentTime() time.Time {
	if !m.now.IsZero() {
		return m.now
	}
	return time.Now()
}

func (m model) statusBar(width int) string {
	current := m.currentTargetName()
	fields := [][2]string{
		{"status", firstNonEmpty(m.roundStatus, "starting")},
		{"plan", firstNonEmpty(m.title, "watch")},
		{"round", fmt.Sprint(m.round)},
		{"focus", m.focusName()},
		{"current", current},
		{"targets", fmt.Sprint(len(m.targets))},
		{"ok", fmt.Sprint(m.targetCount("ok"))},
		{"failed", fmt.Sprint(m.targetCount("failed"))},
		{"failed_checks", fmt.Sprint(len(m.failedChecks))},
	}
	if m.searchEditing || m.hasSearchFilter() {
		fields = append(fields, [2]string{"filter", "/" + m.searchQuery})
	}
	plain := statusPlain(fields)
	if runeLen(plain) > width {
		return valueStyle.Render(fit(plain, width))
	}
	var b strings.Builder
	b.WriteString(valueStyle.Render(" "))
	for i, field := range fields {
		if i > 0 {
			b.WriteString(valueStyle.Render(" "))
		}
		b.WriteString(keyStyle.Render(field[0] + "="))
		b.WriteString(statusBarValueStyle(field[0], field[1]).Render(field[1]))
	}
	b.WriteString(valueStyle.Render(strings.Repeat(" ", max(0, width-runeLen(plain)))))
	return b.String()
}

func statusBarValueStyle(key string, value string) lipgloss.Style {
	switch key {
	case "status":
		return checkStatusStyle(value)
	case "ok":
		return okStatusStyle
	case "failed", "failed_checks":
		return failedStatusStyle
	default:
		return valueStyle
	}
}

func statusPlain(fields [][2]string) string {
	var b strings.Builder
	b.WriteString(" ")
	for i, field := range fields {
		if i > 0 {
			b.WriteString(" ")
		}
		b.WriteString(field[0])
		b.WriteString("=")
		b.WriteString(field[1])
	}
	return b.String()
}

func (m model) focusName() string {
	switch m.focus {
	case focusPassingChecks:
		return "passing_checks"
	case focusFailureHotspots:
		if m.failureHotspotPanelsSplit() {
			key := m.currentHotspotAgentKey()
			for _, agent := range m.failureHotspotAgents() {
				if roundAgentKey(agent) == key {
					return "failure_hotspots:" + compactTargetLabel(agentLabel(agent), 24)
				}
			}
		}
		return "failure_hotspots"
	default:
		return "failed_checks"
	}
}

func (m model) eventLogView(width int, height int) string {
	var b strings.Builder
	used := 0
	targetName, stepName, last := m.eventLogSummary()
	if targetName != "" && height > 0 {
		b.WriteString(keyStyle.Render("target="))
		targetText := fitText(targetName, max(1, width/4))
		b.WriteString(valueStyle.Render(targetText))
		b.WriteString(keyStyle.Render("  step="))
		stepText := fitText(firstNonEmpty(stepName, "-"), max(1, width/4))
		b.WriteString(valueStyle.Render(stepText))
		if last != "" {
			b.WriteString(keyStyle.Render("  last="))
			usedWidth := lipgloss.Width("target=") + lipgloss.Width(targetText) + lipgloss.Width("  step=") + lipgloss.Width(stepText) + lipgloss.Width("  last=")
			b.WriteString(valueStyle.Render(fitText(last, max(1, width-usedWidth))))
		}
		b.WriteByte('\n')
		used++
	}
	visibleLogs := max(0, height-used)
	start := len(m.logs) - visibleLogs
	if start < 0 {
		start = 0
	}
	for _, line := range m.logs[start:] {
		b.WriteString(logStyle.Render(fitText(line, max(1, width))))
		b.WriteByte('\n')
	}
	return b.String()
}

func (m model) eventLogSummary() (string, string, string) {
	if m.eventLogTarget != "" || m.eventLogStep != "" || m.eventLogLast != "" {
		return m.eventLogTarget, m.eventLogStep, m.eventLogLast
	}
	if target, ok := m.currentTarget(); ok {
		last := ""
		if step, ok := currentStepState(target); ok {
			last = step.name + " " + firstNonEmpty(step.message, step.status)
		}
		return m.targetLabel(target), firstNonEmpty(target.currentStep, target.status), last
	}
	return "", "", ""
}

func (m model) roundTimelineView(width int, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	events := m.outcomeEvents()
	ok, failed := outcomeCounts(events)
	startRound, endRound := m.roundTimelineRoundSpan(width)
	lines := []string{m.roundTimelineHeaderView(width, startRound, endRound, ok, failed)}
	if height == 1 {
		return lines[0]
	}
	targets := m.checkStatusTargets()
	if len(targets) == 0 {
		lines = append(lines, dimStyle.Render("no targets"))
		return strings.Join(lines[:min(len(lines), height)], "\n")
	}
	columns := roundTimelineColumnCount(width, len(targets))
	for start := 0; start < len(targets) && len(lines) < height; start += columns {
		end := min(len(targets), start+columns)
		lines = append(lines, m.roundTimelineTargetRow(targets[start:end], width, columns))
	}
	return strings.Join(lines, "\n")
}

func (m model) roundTimelineHeaderView(width int, startRound uint64, endRound uint64, ok int, failed int) string {
	if width <= 0 {
		return ""
	}
	counts := m.roundProgressCounts()
	done := counts.ok + counts.failed + counts.skipped
	percent := 0
	if counts.total > 0 {
		percent = done * 100 / counts.total
	}
	span := "-"
	if endRound > 0 {
		span = fmt.Sprintf("%d..%d", startRound, endRound)
	}
	leftPlain := fmt.Sprintf("round=%d span=%s ok=%d fail=%d run=%d", m.round, span, ok, failed, counts.running)
	rightPlain := fmt.Sprintf("progress=%d%% %d/%d", percent, done, counts.total)
	if runeLen(leftPlain)+1+runeLen(rightPlain) > width {
		leftWidth := max(0, width-runeLen(rightPlain)-1)
		if leftWidth == 0 {
			return valueStyle.Render(fit(rightPlain, width))
		}
		return valueStyle.Render(fit(leftPlain, leftWidth)) +
			valueStyle.Render(strings.Repeat(" ", max(1, width-leftWidth-runeLen(rightPlain)))) +
			keyStyle.Render("progress=") +
			valueStyle.Render(fmt.Sprintf("%d%% %d/%d", percent, done, counts.total))
	}
	left := keyStyle.Render("round=") + valueStyle.Render(fmt.Sprint(m.round)) +
		keyStyle.Render(" span=") + valueStyle.Render(span) +
		keyStyle.Render(" ok=") + okStatusStyle.Render(fmt.Sprint(ok)) +
		keyStyle.Render(" fail=") + failedStatusStyle.Render(fmt.Sprint(failed)) +
		keyStyle.Render(" run=") + runningStatusStyle.Render(fmt.Sprint(counts.running))
	right := keyStyle.Render("progress=") + valueStyle.Render(fmt.Sprintf("%d%% %d/%d", percent, done, counts.total))
	spaces := max(1, width-runeLen(leftPlain)-runeLen(rightPlain))
	return left + valueStyle.Render(strings.Repeat(" ", spaces)) + right
}

func (m model) roundTimelineRoundSpan(width int) (uint64, uint64) {
	endRound := m.latestRound()
	if endRound == 0 {
		return 0, 0
	}
	visibleRounds := max(1, m.roundTimelineVisibleRounds(width))
	startRound := uint64(1)
	if endRound > uint64(visibleRounds) {
		startRound = endRound - uint64(visibleRounds) + 1
	}
	return startRound, endRound
}

func (m model) roundTimelineVisibleRounds(width int) int {
	columns := roundTimelineColumnCount(width, len(m.checkStatusTargets()))
	tileWidths := roundTimelineTileWidths(width, columns)
	if len(tileWidths) == 0 {
		return 1
	}
	visible := 1
	for _, tileWidth := range tileWidths {
		_, plotWidth := roundTimelineTileLayout(tileWidth)
		visible = max(visible, plotWidth)
	}
	return visible
}

func (m model) latestRound() uint64 {
	latest := m.round
	for _, passingCheck := range m.passingChecks {
		if passingCheck.round > latest {
			latest = passingCheck.round
		}
	}
	for _, failedCheck := range m.failedChecks {
		if failedCheck.round > latest {
			latest = failedCheck.round
		}
	}
	return latest
}

func (m model) roundTimelineTargetRow(targets []watch.TargetSnapshot, width int, columns int) string {
	if width <= 0 {
		return ""
	}
	columns = max(1, columns)
	tileWidths := roundTimelineTileWidths(width, columns)
	tiles := make([]string, 0, len(tileWidths))
	for column, tileWidth := range tileWidths {
		if column < len(targets) {
			tiles = append(tiles, m.roundTimelineTargetTile(targets[column], tileWidth))
		} else {
			tiles = append(tiles, valueStyle.Render(strings.Repeat(" ", tileWidth)))
		}
	}
	return strings.Join(tiles, valueStyle.Render(strings.Repeat(" ", roundTimelineTileGap)))
}

func roundTimelineColumnCount(width int, targetCount int) int {
	if width <= 0 || targetCount <= 0 {
		return 1
	}
	for columns := targetCount; columns >= 1; columns-- {
		widths := roundTimelineTileWidths(width, columns)
		if len(widths) == 0 {
			continue
		}
		fits := true
		for _, tileWidth := range widths {
			_, plotWidth := roundTimelineTileLayout(tileWidth)
			if plotWidth < roundTimelineMinVisibleRounds {
				fits = false
				break
			}
		}
		if fits {
			return columns
		}
	}
	return 1
}

func roundTimelineTileWidths(width int, columns int) []int {
	if width <= 0 || columns <= 0 {
		return nil
	}
	available := max(1, width-roundTimelineTileGap*(columns-1))
	baseWidth := available / columns
	remainder := available % columns
	widths := make([]int, columns)
	for column := 0; column < columns; column++ {
		widths[column] = baseWidth
		if column < remainder {
			widths[column]++
		}
	}
	return widths
}

func (m model) roundTimelineTargetTile(target watch.TargetSnapshot, width int) string {
	if width <= 0 {
		return ""
	}
	labelWidth, plotWidth := roundTimelineTileLayout(width)
	buckets, _, _ := m.targetRoundHistory(target, plotWidth)
	checkCount := max(1, len(m.checkStatusChecks()))
	label := groupStyle.Render(padVisible(compactTargetLabel(checkStatusTargetLabel(target), labelWidth), labelWidth))
	plot := renderTargetRoundHistory(buckets, checkCount, plotWidth)
	return fitANSI(label+valueStyle.Render(" ")+plot, width)
}

func roundTimelineTileLayout(width int) (labelWidth int, plotWidth int) {
	if width <= 0 {
		return 0, 0
	}
	labelWidth = clamp(width/4, 6, min(16, max(6, width-4)))
	plotWidth = max(1, width-labelWidth-1)
	return labelWidth, plotWidth
}

func (m model) targetRoundHistory(target watch.TargetSnapshot, width int) ([]targetRoundBucket, int, int) {
	width = max(1, width)
	buckets := make([]targetRoundBucket, width)
	connectFailedAgents := make([]map[string]bool, width)
	endRound := m.latestRound()
	if endRound == 0 {
		return buckets, 0, 0
	}
	startRound := uint64(1)
	if endRound > uint64(width) {
		startRound = endRound - uint64(width) + 1
	}
	indexForRound := func(round uint64) (int, bool) {
		if round == 0 || round < startRound || round > endRound {
			return 0, false
		}
		return int(round - startRound), true
	}
	total := 0
	peak := 0
	targetKey := checkStatusTargetKey(target)
	expectedAgents := m.expectedRoundAgentCount()
	markConnectFailed := func(index int, agent watch.AgentSnapshot) {
		if connectFailedAgents[index] == nil {
			connectFailedAgents[index] = map[string]bool{}
		}
		connectFailedAgents[index][roundAgentKey(agent)] = true
	}
	for _, passingCheck := range m.passingChecks {
		if checkStatusTargetKey(passingCheck.target) != targetKey {
			continue
		}
		index, ok := indexForRound(passingCheck.round)
		if !ok {
			continue
		}
		buckets[index].seen = true
	}
	for _, failedCheck := range m.failedChecks {
		if checkStatusTargetKey(failedCheck.target) != targetKey {
			continue
		}
		index, ok := indexForRound(failedCheck.round)
		if !ok {
			continue
		}
		buckets[index].seen = true
		buckets[index].failed++
		if connectionFailureCheck(failedCheck.finding.Check) {
			markConnectFailed(index, failedCheck.agent)
		}
		total++
		if buckets[index].failed > peak {
			peak = buckets[index].failed
		}
	}
	for i := range buckets {
		if len(connectFailedAgents[i]) == 0 {
			continue
		}
		if expectedAgents <= 1 || len(connectFailedAgents[i]) >= expectedAgents {
			buckets[i].connectFailed = true
		}
	}
	return buckets, total, peak
}

func (m model) expectedRoundAgentCount() int {
	if len(m.agents) > 0 {
		return len(m.agents)
	}
	seen := map[string]bool{}
	for _, target := range m.targets {
		seen[roundAgentKey(target.agent)] = true
	}
	for _, passingCheck := range m.passingChecks {
		seen[roundAgentKey(passingCheck.agent)] = true
	}
	for _, failedCheck := range m.failedChecks {
		seen[roundAgentKey(failedCheck.agent)] = true
	}
	return max(1, len(seen))
}

func roundAgentKey(agent watch.AgentSnapshot) string {
	if key := agentKey(agent); key != "" {
		return key
	}
	return "all"
}

func renderTargetRoundHistory(buckets []targetRoundBucket, checkCount int, width int) string {
	if width <= 0 {
		return ""
	}
	checkCount = max(1, checkCount)
	var b strings.Builder
	for i := 0; i < width; i++ {
		bucket := targetRoundBucket{}
		if i < len(buckets) {
			bucket = buckets[i]
		}
		if !bucket.seen {
			b.WriteString(dimStyle.Render(" "))
			continue
		}
		if bucket.failed == 0 {
			b.WriteString(timelineBaseStyle.Render("▁"))
			continue
		}
		if bucket.connectFailed {
			b.WriteString(connectFailureStyle.Render("X"))
			continue
		}
		b.WriteString(failGraphStyle.Render(failureCountBlock(bucket.failed, checkCount)))
	}
	return b.String()
}

func failureCountBlock(count int, checkCount int) string {
	blocks := []string{"▁", "▂", "▃", "▄", "▅", "▆", "▇", "█"}
	if count <= 0 {
		return blocks[0]
	}
	if checkCount <= 1 {
		return blocks[1]
	}
	index := 1 + (count*(len(blocks)-1)-1)/checkCount
	return blocks[clamp(index, 1, len(blocks)-1)]
}

func connectionFailureCheck(check string) bool {
	switch strings.ToLower(strings.TrimSpace(check)) {
	case "connect", "wait_connected":
		return true
	default:
		return false
	}
}

type roundProgressCounts struct {
	ok      int
	failed  int
	skipped int
	running int
	pending int
	total   int
}

func (m model) roundProgressView(width int) string {
	counts := m.roundProgressCounts()
	done := counts.ok + counts.failed + counts.skipped
	percent := 0
	if counts.total > 0 {
		percent = done * 100 / counts.total
	}
	prefixPlain := fmt.Sprintf("round=%d progress=", m.round)
	suffixPlain := fmt.Sprintf(" %3d%% %d/%d run=%d fail=%d", percent, done, counts.total, counts.running, counts.failed)
	lineWidth := width - runeLen(prefixPlain) - runeLen(suffixPlain)
	if lineWidth < 4 {
		line := prefixPlain + strings.TrimSpace(suffixPlain)
		return valueStyle.Render(fit(line, width))
	}
	lineWidth = clamp(lineWidth, 4, 48)
	line := keyStyle.Render("round=") +
		valueStyle.Render(fmt.Sprint(m.round)) +
		keyStyle.Render(" progress=") +
		renderRoundProgressLine(counts, lineWidth) +
		valueStyle.Render(suffixPlain)
	return fitANSI(line, width)
}

func (m model) roundProgressCounts() roundProgressCounts {
	counts := roundProgressCounts{total: len(m.targets)}
	for _, target := range m.targets {
		switch normalizeStatus(target.status) {
		case "ok":
			counts.ok++
		case "failed":
			counts.failed++
		case "skipped":
			counts.skipped++
		case "running":
			counts.running++
		default:
			counts.pending++
		}
	}
	return counts
}

func renderRoundProgressLine(counts roundProgressCounts, width int) string {
	if width <= 0 {
		return ""
	}
	if counts.total <= 0 {
		return dimStyle.Render(strings.Repeat("-", width))
	}
	widths := proportionalWidths([]int{counts.ok, counts.failed, counts.skipped, counts.running, counts.pending}, counts.total, width)
	var b strings.Builder
	b.WriteString(okGraphStyle.Render(strings.Repeat("-", widths[0])))
	b.WriteString(failGraphStyle.Render(strings.Repeat("-", widths[1])))
	b.WriteString(dimStyle.Render(strings.Repeat("-", widths[2])))
	b.WriteString(warnStyle.Render(strings.Repeat("-", widths[3])))
	b.WriteString(dimStyle.Render(strings.Repeat("-", widths[4])))
	return b.String()
}

func proportionalWidths(counts []int, total int, width int) []int {
	widths := make([]int, len(counts))
	if total <= 0 || width <= 0 {
		return widths
	}
	type remainder struct {
		index int
		value int
	}
	remainders := make([]remainder, 0, len(counts))
	used := 0
	for i, count := range counts {
		if count <= 0 {
			continue
		}
		scaled := count * width
		widths[i] = scaled / total
		used += widths[i]
		remainders = append(remainders, remainder{index: i, value: scaled % total})
	}
	sort.SliceStable(remainders, func(i, j int) bool {
		return remainders[i].value > remainders[j].value
	})
	for remaining, i := width-used, 0; remaining > 0 && len(remainders) > 0; remaining, i = remaining-1, i+1 {
		widths[remainders[i%len(remainders)].index]++
	}
	return widths
}

func (m model) checkStatusView(width int, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	targets := m.checkStatusTargets()
	if len(targets) == 0 {
		return dimStyle.Render("no targets")
	}
	checks := m.checkStatusChecks()
	if len(checks) == 0 {
		return dimStyle.Render("no checks")
	}
	agents := m.outcomeAgents(m.outcomeEvents())
	labelWidth := clamp(maxCheckStatusLabelWidth(checks), 5, min(18, max(5, width/4)))
	available := max(1, width-labelWidth-1)
	minCellWidth := 9
	visibleTargets := min(len(targets), max(1, (available+1)/(minCellWidth+1)))
	cellWidth := clamp((available-max(0, visibleTargets-1))/visibleTargets, minCellWidth, 12)
	targets = targets[:visibleTargets]
	var lines []string
	var header strings.Builder
	header.WriteString(tableHeaderStyle.Render(padVisible("Check", labelWidth)))
	header.WriteString(tableHeaderStyle.Render(" "))
	for i, target := range targets {
		if i > 0 {
			header.WriteString(tableHeaderStyle.Render(" "))
		}
		header.WriteString(tableHeaderStyle.Render(padVisible(compactTargetLabel(checkStatusTargetLabel(target), cellWidth), cellWidth)))
	}
	lines = append(lines, header.String())
	visibleRows := max(0, height-1)
	for _, check := range checks {
		if visibleRows <= 0 {
			break
		}
		var b strings.Builder
		b.WriteString(valueStyle.Render(padVisible(check, labelWidth)))
		b.WriteString(valueStyle.Render(" "))
		for i, target := range targets {
			if i > 0 {
				b.WriteString(valueStyle.Render(" "))
			}
			cell := m.checkStatusTargetCell(check, target, agents)
			b.WriteString(renderCheckStatusAggregateCell(cell, cellWidth, m.multiAgent))
		}
		lines = append(lines, b.String())
		visibleRows--
	}
	return strings.Join(lines, "\n")
}

func (m model) agentTargetCheckStatusView(width int, height int) string {
	return m.checkStatusView(width, height)
}

func (m model) outcomeEvents() []outcomeEvent {
	events := make([]outcomeEvent, 0, len(m.passingChecks)+len(m.failedChecks))
	for _, passingCheck := range m.passingChecks {
		events = append(events, outcomeEvent{when: passingCheck.when, agent: passingCheck.agent, target: passingCheck.target, status: "ok"})
	}
	for _, failedCheck := range m.failedChecks {
		events = append(events, outcomeEvent{when: failedCheck.when, agent: failedCheck.agent, target: failedCheck.target, status: "failed"})
	}
	sort.SliceStable(events, func(i, j int) bool {
		return events[i].when.Before(events[j].when)
	})
	return events
}

func (m model) outcomeAgents(events []outcomeEvent) []watch.AgentSnapshot {
	agents := make([]watch.AgentSnapshot, 0, max(1, len(m.agents)))
	seen := make(map[string]bool)
	add := func(agent watch.AgentSnapshot) {
		key := agentKey(agent)
		if key == "" && len(agents) > 0 {
			return
		}
		if seen[key] {
			return
		}
		seen[key] = true
		agents = append(agents, agent)
	}
	if len(m.agents) > 0 {
		for _, agent := range m.agents {
			add(agent)
		}
	}
	for _, target := range m.targets {
		add(target.agent)
	}
	for _, event := range events {
		add(event.agent)
	}
	if len(agents) == 0 {
		agents = append(agents, watch.AgentSnapshot{})
	}
	return agents
}

func (m model) checkStatusTargets() []watch.TargetSnapshot {
	targets := make([]watch.TargetSnapshot, 0, len(m.targets))
	seen := make(map[string]bool)
	add := func(target watch.TargetSnapshot) {
		key := checkStatusTargetKey(target)
		if key == "" || seen[key] {
			return
		}
		seen[key] = true
		targets = append(targets, target)
	}
	for _, target := range m.targets {
		add(target.target)
	}
	for _, passingCheck := range m.passingChecks {
		add(passingCheck.target)
	}
	for _, failedCheck := range m.failedChecks {
		add(failedCheck.target)
	}
	return targets
}

func (m model) checkStatusChecks() []string {
	checks := make([]string, 0, 8)
	seen := make(map[string]bool)
	add := func(check string) {
		check = strings.TrimSpace(check)
		if check == "" || check == "target" || seen[check] {
			return
		}
		seen[check] = true
		checks = append(checks, check)
	}
	for _, target := range m.targets {
		for _, step := range target.plannedSteps {
			add(firstNonEmpty(step.name, step.typ))
		}
		for _, step := range target.steps {
			add(firstNonEmpty(step.name, step.typ))
		}
	}
	for _, passingCheck := range m.passingChecks {
		add(firstNonEmpty(passingCheck.step.Name, passingCheck.step.Type))
	}
	for _, failedCheck := range m.failedChecks {
		add(failedCheck.finding.Check)
	}
	return checks
}

func maxCheckStatusLabelWidth(checks []string) int {
	width := 5
	for _, check := range checks {
		width = max(width, lipgloss.Width(check))
	}
	return width
}

func filterOutcomeEvents(events []outcomeEvent, agent watch.AgentSnapshot, target watch.TargetSnapshot) []outcomeEvent {
	filtered := make([]outcomeEvent, 0, len(events))
	targetKey := checkStatusTargetKey(target)
	for _, event := range events {
		if agentKey(agent) != "" && !sameAgent(event.agent, agent) {
			continue
		}
		if targetKey != "" && checkStatusTargetKey(event.target) != targetKey {
			continue
		}
		filtered = append(filtered, event)
	}
	return filtered
}

func outcomeCounts(events []outcomeEvent) (ok int, failed int) {
	for _, event := range events {
		if event.status == "failed" {
			failed++
		} else if event.status == "ok" {
			ok++
		}
	}
	return ok, failed
}

func outcomeRange(events []outcomeEvent) (time.Time, time.Time) {
	if len(events) == 0 {
		now := time.Now()
		return now, now
	}
	return events[0].when, events[len(events)-1].when
}

func renderOutcomeStrip(events []outcomeEvent, width int) string {
	if width <= 0 {
		return ""
	}
	if len(events) == 0 {
		return dimStyle.Render(strings.Repeat(" ", width))
	}
	buckets := outcomeBuckets(events, width)
	maxOK := 1
	maxFailed := 1
	for _, bucket := range buckets {
		if bucket.ok > maxOK {
			maxOK = bucket.ok
		}
		if bucket.failed > maxFailed {
			maxFailed = bucket.failed
		}
	}
	var b strings.Builder
	for _, bucket := range buckets {
		if bucket.ok+bucket.failed == 0 {
			b.WriteString(dimStyle.Render(" "))
			continue
		}
		if bucket.failed > 0 {
			block := intensityBlockWithFloor(bucket.failed, maxFailed, 4)
			b.WriteString(failGraphStyle.Render(block))
		} else {
			block := intensityBlockWithFloor(bucket.ok, maxOK, 2)
			b.WriteString(okGraphStyle.Render(block))
		}
	}
	return b.String()
}

func outcomeBuckets(events []outcomeEvent, width int) []outcomeBucket {
	width = max(1, width)
	buckets := make([]outcomeBucket, width)
	if len(events) == 0 {
		return buckets
	}
	first, last := outcomeRange(events)
	span := last.Sub(first)
	for _, event := range events {
		index := width - 1
		if span > 0 && width > 1 {
			index = int(float64(event.when.Sub(first)) / float64(span) * float64(width-1))
		} else if width > 1 {
			index = 0
		}
		index = clamp(index, 0, width-1)
		if event.status == "failed" {
			buckets[index].failed++
		} else if event.status == "ok" {
			buckets[index].ok++
		}
	}
	return buckets
}

func renderCheckStatusCell(status string, width int) string {
	if width <= 0 {
		return ""
	}
	status = normalizeStatus(status)
	token := checkStatusToken(status)
	return checkStatusStyle(status).Render(padVisible(token, width))
}

type checkStatusAggregate struct {
	status string
	count  int
	failed int
	total  int
	stale  bool
}

type checkStatusAgentResult struct {
	status string
	stale  bool
}

func renderCheckStatusAggregateCell(cell checkStatusAggregate, width int, multiAgent bool) string {
	if width <= 0 {
		return ""
	}
	token := checkStatusAggregateToken(cell, multiAgent)
	if cell.stale {
		return staleStatusStyle(cell.status).Render(padVisible(token, width))
	}
	return checkStatusStyle(cell.status).Render(padVisible(token, width))
}

func checkStatusAggregateToken(cell checkStatusAggregate, multiAgent bool) string {
	status := normalizeStatus(cell.status)
	count := cell.count
	if status == "failed" && count == 0 {
		count = cell.failed
	}
	if multiAgent && cell.total > 1 && count > 0 && count < cell.total {
		percent := 100 * count / cell.total
		return fmt.Sprintf("%s(%d%%)", checkStatusToken(status), percent)
	}
	return checkStatusToken(status)
}

func checkStatusToken(status string) string {
	switch normalizeStatus(status) {
	case "ok":
		return "PASS"
	case "failed":
		return "FAIL"
	case "running":
		return "RUN"
	case "skipped":
		return "SKIP"
	default:
		return "WAIT"
	}
}

func renderRecentOutcomeStrip(events []outcomeEvent, width int, fallbackStatus string) string {
	if width <= 0 {
		return ""
	}
	if len(events) == 0 {
		switch normalizeStatus(fallbackStatus) {
		case "running":
			return warnStyle.Render("▌" + strings.Repeat(" ", max(0, width-1)))
		case "ok":
			return okGraphStyle.Render(strings.Repeat("▁", width))
		case "failed":
			return failGraphStyle.Render(strings.Repeat("█", width))
		default:
			return dimStyle.Render(strings.Repeat("-", width))
		}
	}
	start := max(0, len(events)-width)
	events = events[start:]
	leftPad := width - len(events)
	var b strings.Builder
	if leftPad > 0 {
		b.WriteString(dimStyle.Render(strings.Repeat("-", leftPad)))
	}
	for _, event := range events {
		if event.status == "failed" {
			b.WriteString(failGraphStyle.Render("█"))
		} else {
			b.WriteString(okGraphStyle.Render("▁"))
		}
	}
	return b.String()
}

func intensityBlock(count int, maxCount int) string {
	return intensityBlockWithFloor(count, maxCount, 0)
}

func intensityBlockWithFloor(count int, maxCount int, floor int) string {
	if count <= 0 || maxCount <= 0 {
		return " "
	}
	blocks := []string{"▁", "▂", "▃", "▄", "▅", "▆", "▇", "█"}
	index := (count*len(blocks) - 1) / maxCount
	index = clamp(index, floor, len(blocks)-1)
	return blocks[index]
}

func maxOutcomeAgentLabelWidth(agents []watch.AgentSnapshot) int {
	width := 4
	for _, agent := range agents {
		if label := outcomeAgentLabel(agent, true); lipgloss.Width(label) > width {
			width = lipgloss.Width(label)
		}
	}
	return width
}

func outcomeAgentLabel(agent watch.AgentSnapshot, multiAgent bool) string {
	if !multiAgent && agentKey(agent) == "" {
		return "all"
	}
	return firstNonEmpty(agentLabel(agent), "all")
}

func checkStatusTargetKey(target watch.TargetSnapshot) string {
	return firstNonEmpty(target.Name, target.SSID, target.BSSID)
}

func checkStatusTargetLabel(target watch.TargetSnapshot) string {
	return firstNonEmpty(target.Name, target.SSID, target.BSSID, "-")
}

func (m model) checkStatusForTarget(agent watch.AgentSnapshot, target watch.TargetSnapshot, events []outcomeEvent) string {
	for _, state := range m.targets {
		if sameAgent(state.agent, agent) && checkStatusTargetKey(state.target) == checkStatusTargetKey(target) {
			status := normalizeStatus(state.status)
			if status == "pending" && len(events) > 0 {
				return normalizeStatus(events[len(events)-1].status)
			}
			return status
		}
	}
	if len(events) > 0 {
		return normalizeStatus(events[len(events)-1].status)
	}
	return "pending"
}

func (m model) checkStatusTargetCell(check string, target watch.TargetSnapshot, agents []watch.AgentSnapshot) checkStatusAggregate {
	if len(agents) == 0 {
		agents = []watch.AgentSnapshot{{}}
	}
	counts := map[string]int{}
	currentCounts := map[string]int{}
	for _, agent := range agents {
		result := m.checkStatusAgentResult(agent, target, check)
		status := normalizeStatus(result.status)
		counts[status]++
		if !result.stale {
			currentCounts[status]++
		}
	}
	total := len(agents)
	failed := counts["failed"]
	switch {
	case failed > 0:
		return checkStatusAggregate{status: "failed", count: failed, failed: failed, total: total, stale: currentCounts["failed"] == 0}
	case counts["running"] > 0:
		return checkStatusAggregate{status: "running", count: counts["running"], total: total}
	case counts["ok"] > 0:
		return checkStatusAggregate{status: "ok", count: counts["ok"], total: total, stale: currentCounts["ok"] == 0}
	case counts["skipped"] > 0:
		return checkStatusAggregate{status: "skipped", count: counts["skipped"], total: total, stale: currentCounts["skipped"] == 0}
	default:
		return checkStatusAggregate{status: "pending", total: total}
	}
}

func (m model) checkStatusAgentResult(agent watch.AgentSnapshot, target watch.TargetSnapshot, check string) checkStatusAgentResult {
	if status, ok := m.currentCheckStatus(agent, target, check); ok {
		if normalizeStatus(status) != "pending" {
			return checkStatusAgentResult{status: status}
		}
	}
	if status, ok := m.historicalCheckStatus(agent, target, check); ok {
		return checkStatusAgentResult{status: status, stale: true}
	}
	return checkStatusAgentResult{status: "pending"}
}

func (m model) historicalCheckStatus(agent watch.AgentSnapshot, target watch.TargetSnapshot, check string) (string, bool) {
	status := "pending"
	var seen time.Time
	for _, passingCheck := range m.passingChecks {
		if !sameAgent(passingCheck.agent, agent) || checkStatusTargetKey(passingCheck.target) != checkStatusTargetKey(target) {
			continue
		}
		if firstNonEmpty(passingCheck.step.Name, passingCheck.step.Type) != check {
			continue
		}
		if passingCheck.when.After(seen) || seen.IsZero() {
			seen = passingCheck.when
			status = "ok"
		}
	}
	for _, failedCheck := range m.failedChecks {
		if !sameAgent(failedCheck.agent, agent) || checkStatusTargetKey(failedCheck.target) != checkStatusTargetKey(target) {
			continue
		}
		if failedCheck.finding.Check != check {
			continue
		}
		if failedCheck.when.After(seen) || seen.IsZero() {
			seen = failedCheck.when
			status = "failed"
		}
	}
	if seen.IsZero() {
		return "", false
	}
	return status, true
}

func (m model) currentCheckStatus(agent watch.AgentSnapshot, target watch.TargetSnapshot, check string) (string, bool) {
	for _, state := range m.targets {
		if !sameAgent(state.agent, agent) || checkStatusTargetKey(state.target) != checkStatusTargetKey(target) {
			continue
		}
		for _, step := range state.steps {
			if firstNonEmpty(step.name, step.typ) != check {
				continue
			}
			status := normalizeStatus(step.status)
			if status == "pending" && state.currentStep == step.name {
				status = "running"
			}
			return status, true
		}
		return "pending", true
	}
	return "", false
}

func compactTargetLabel(label string, width int) string {
	label = strings.TrimSpace(label)
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(label) <= width {
		return label
	}
	parts := strings.Split(label, "-")
	if len(parts) > 1 {
		candidate := ""
		for i := len(parts) - 1; i >= 0; i-- {
			next := parts[i]
			if candidate != "" {
				next += "-" + candidate
			}
			if lipgloss.Width(next) > width {
				break
			}
			candidate = next
		}
		if candidate != "" {
			return candidate
		}
	}
	return fit(label, width)
}

func (m model) runQueueTreeView(width int, height int) string {
	lines := m.runQueueTreeLines(width)
	if len(lines) == 0 || height <= 0 {
		return ""
	}
	current, ok := m.currentRunQueueLineIndex()
	if !ok {
		current = m.runQueueCursor
	}
	current = clamp(current, 0, len(lines)-1)
	start := stableOffset(current, m.runQueueOffset, height, len(lines))
	end := min(len(lines), start+height)
	var b strings.Builder
	for _, line := range lines[start:end] {
		b.WriteString(renderRunQueueLine(line, width))
		b.WriteByte('\n')
	}
	return b.String()
}

func (m model) runQueuePanelsView(width int, height int) string {
	if !m.multiAgent || height < 6 {
		return renderPanel("Run Queue", width, height, m.runQueueTreeView(panelContentWidth(width), height-2))
	}
	agents := m.runQueueAgents()
	if len(agents) <= 1 || height < len(agents)*3 {
		return renderPanel("Run Queue", width, height, m.runQueueTreeView(panelContentWidth(width), height-2))
	}
	heights := splitHeights(height, len(agents))
	panels := make([]string, 0, len(agents))
	for i, agent := range agents {
		title := "Run Queue " + compactTargetLabel(agentLabel(agent), max(4, width-8))
		panelHeight := heights[i]
		panels = append(panels, renderPanel(title, width, panelHeight, m.runQueueTreeViewForAgent(agent, panelContentWidth(width), panelHeight-2)))
	}
	return lipgloss.JoinVertical(lipgloss.Left, panels...)
}

func (m model) runQueueAgents() []watch.AgentSnapshot {
	return m.outcomeAgents(m.outcomeEvents())
}

func splitHeights(total int, count int) []int {
	if count <= 0 {
		return nil
	}
	heights := make([]int, count)
	base := total / count
	remainder := total % count
	for i := range heights {
		heights[i] = base
		if i < remainder {
			heights[i]++
		}
	}
	return heights
}

func (m model) runQueueTreeViewForAgent(agent watch.AgentSnapshot, width int, height int) string {
	lines := m.runQueueTreeLinesForAgent(width, agent, false)
	if len(lines) == 0 || height <= 0 {
		return ""
	}
	current, ok := m.currentRunQueueLineIndexForAgent(agent)
	if !ok {
		current = 0
	}
	current = clamp(current, 0, len(lines)-1)
	start := stableOffset(current, 0, height, len(lines))
	end := min(len(lines), start+height)
	var b strings.Builder
	for _, line := range lines[start:end] {
		b.WriteString(renderRunQueueLine(line, width))
		b.WriteByte('\n')
	}
	return b.String()
}

func renderRunQueueLine(line runQueueLine, width int) string {
	style := runQueueRowStyle(line.status)
	if line.current {
		return selectedStyle.Render(padVisible(fitANSI(line.text, width), width))
	}
	return style.Render(fitANSI(line.text, width))
}

func (m model) runQueueTreeLines(width int) []runQueueLine {
	return m.runQueueTreeLinesForAgent(width, watch.AgentSnapshot{}, m.multiAgent)
}

func (m model) runQueueTreeLinesForAgent(width int, agent watch.AgentSnapshot, includeAgentLabel bool) []runQueueLine {
	lines := make([]runQueueLine, 0, len(m.targets)*2)
	for _, target := range m.targets {
		if agentKey(agent) != "" && !sameAgent(target.agent, agent) {
			continue
		}
		status := runQueueTargetStatus(target)
		active := normalizeStatus(status) == "running"
		failedChecks := m.targetFailedCheckCount(target.agent, target.target.Name)
		suffix := ""
		if failedChecks > 0 {
			suffix = fmt.Sprintf(" fail=%d", failedChecks)
		}
		currentStepName := runQueueCurrentStepName(target)
		line := fmt.Sprintf("%s %s%s", statusToken(status), m.runQueueTargetLabel(target, includeAgentLabel), suffix)
		lines = append(lines, runQueueLine{text: fitANSI(line, width), status: status, current: active && currentStepName == ""})
		steps := runQueueVisibleSteps(target)
		if len(steps) == 0 {
			continue
		}
		for i, step := range steps {
			branch := "├──"
			if i == len(steps)-1 {
				branch = "└──"
			}
			stepCurrent := currentStepName == step.name
			status := step.status
			if stepCurrent && status == "" {
				status = "running"
			} else if status == "" {
				status = "pending"
			}
			line := fmt.Sprintf("  %s %s %s", branch, statusToken(status), step.name)
			lines = append(lines, runQueueLine{text: fitANSI(line, width), status: status, current: stepCurrent})
		}
	}
	return lines
}

func runQueueVisibleSteps(target targetState) []stepState {
	if normalizeStatus(runQueueTargetStatus(target)) != "running" {
		return nil
	}
	steps := mergeRunQueueSteps(target.plannedSteps, target.steps)
	if len(steps) == 0 {
		return target.steps
	}
	return steps
}

func runQueueTargetStatus(target targetState) string {
	active := target.currentStep != "" || normalizeStatus(target.status) == "running"
	if active && normalizeStatus(target.status) != "failed" {
		return "running"
	}
	return target.status
}

func runQueueCurrentStepName(target targetState) string {
	if strings.TrimSpace(target.currentStep) != "" {
		return target.currentStep
	}
	if normalizeStatus(runQueueTargetStatus(target)) != "running" {
		return ""
	}
	steps := runQueueVisibleSteps(target)
	for i := len(steps) - 1; i >= 0; i-- {
		if normalizeStatus(steps[i].status) != "pending" {
			return steps[i].name
		}
	}
	return ""
}

func mergeRunQueueSteps(planned []stepState, actual []stepState) []stepState {
	steps := make([]stepState, 0, len(planned)+len(actual))
	index := make(map[string]int, len(planned)+len(actual))
	add := func(step stepState, replace bool) {
		name := strings.TrimSpace(step.name)
		if name == "" {
			name = strings.TrimSpace(step.typ)
			step.name = name
		}
		if name == "" {
			return
		}
		key := strings.ToLower(name)
		if pos, ok := index[key]; ok {
			if replace {
				steps[pos] = step
			}
			return
		}
		index[key] = len(steps)
		steps = append(steps, step)
	}
	for _, step := range planned {
		add(step, false)
	}
	for _, step := range actual {
		add(step, true)
	}
	return steps
}

func (m model) targetOutcomeStrip(agent watch.AgentSnapshot, target watch.TargetSnapshot, width int, fallbackStatus string) string {
	if width <= 0 {
		return ""
	}
	events := filterOutcomeEvents(m.outcomeEvents(), agent, target)
	if len(events) == 0 && normalizeStatus(fallbackStatus) == "pending" {
		return strings.Repeat("-", width)
	}
	return plainRecentOutcomeStrip(events, width, fallbackStatus)
}

func plainRecentOutcomeStrip(events []outcomeEvent, width int, fallbackStatus string) string {
	if width <= 0 {
		return ""
	}
	if len(events) == 0 {
		switch normalizeStatus(fallbackStatus) {
		case "running":
			return "▌" + strings.Repeat(" ", max(0, width-1))
		case "ok":
			return strings.Repeat("▁", width)
		case "failed":
			return strings.Repeat("█", width)
		default:
			return strings.Repeat("-", width)
		}
	}
	start := max(0, len(events)-width)
	events = events[start:]
	var b strings.Builder
	if leftPad := width - len(events); leftPad > 0 {
		b.WriteString(strings.Repeat("-", leftPad))
	}
	for _, event := range events {
		if event.status == "failed" {
			b.WriteString("█")
		} else {
			b.WriteString("▁")
		}
	}
	return b.String()
}

func runQueueStripWidth(width int) int {
	switch {
	case width >= 44:
		return 10
	case width >= 34:
		return 8
	case width >= 28:
		return 6
	default:
		return 0
	}
}

func lineWithRightSuffix(line string, suffix string, width int) string {
	if width <= 0 || suffix == "" {
		return line
	}
	suffixWidth := lipgloss.Width(suffix)
	if suffixWidth+2 >= width {
		return line
	}
	lineWidth := lipgloss.Width(line)
	available := width - suffixWidth - 1
	if lineWidth > available {
		line = fit(line, available)
		lineWidth = lipgloss.Width(line)
	}
	return line + strings.Repeat(" ", max(1, available-lineWidth+1)) + suffix
}

func (m *model) updateRunQueueCursor() {
	if index, ok := m.currentRunQueueLineIndex(); ok {
		m.runQueueCursor = index
		m.runQueueOffset = stableOffset(index, m.runQueueOffset, m.runQueueViewportHeight(), m.runQueueLineCount())
		return
	}
	m.runQueueCursor = clamp(m.runQueueCursor, 0, max(0, m.runQueueLineCount()-1))
	m.runQueueOffset = clamp(m.runQueueOffset, 0, max(0, m.runQueueLineCount()-m.runQueueViewportHeight()))
}

func (m model) runQueueViewportHeight() int {
	height := m.height
	if height <= 0 {
		height = 32
	}
	width := m.width
	if width <= 0 {
		width = 120
	}
	bodyHeight := max(4, height-2)
	roundTimelineHeight, checkStatusHeight, _, _ := m.dashboardPanelHeights(bodyHeight, panelContentWidth(width))
	lowerHeight := max(1, bodyHeight-checkStatusHeight-roundTimelineHeight)
	return max(1, lowerHeight-2)
}

func (m model) currentRunQueueLineIndex() (int, bool) {
	return m.currentRunQueueLineIndexForAgent(watch.AgentSnapshot{})
}

func (m model) currentRunQueueLineIndexForAgent(agent watch.AgentSnapshot) (int, bool) {
	index := 0
	for _, target := range m.targets {
		if agentKey(agent) != "" && !sameAgent(target.agent, agent) {
			continue
		}
		active := normalizeStatus(runQueueTargetStatus(target)) == "running"
		currentStepName := runQueueCurrentStepName(target)
		if active && currentStepName == "" {
			return index, true
		}
		index++
		for _, step := range runQueueVisibleSteps(target) {
			if currentStepName == step.name {
				return index, true
			}
			index++
		}
	}
	return 0, false
}

func (m model) runQueueLineCount() int {
	count := 0
	for _, target := range m.targets {
		count++
		count += len(runQueueVisibleSteps(target))
	}
	return count
}

func (m model) passingChecksView(width int, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	rows := m.filteredPassingCheckSummaries()
	maxCount := maxPassingCheckSummaryCount(rows)
	layout := passingCheckBarListLayout(rows, max(1, width-2), m.multiAgent)
	selected := clamp(m.passingCheckCursor, 0, max(0, len(rows)-1))
	now := m.currentTime()
	sparkHeight := summarySparklineHeight(height)
	tableHeight := max(0, height-sparkHeight)
	lines := make([]tableLine, 0, len(rows)*2+1)
	selectedLine := -1
	lines = append(lines, tableLine{text: "  " + barListHeader(passingCheckListHeaderColumns(m.multiAgent), passingCheckListRightHeaderColumns(), layout), style: summaryTableHeaderStyle, fill: true})
	for index, item := range rows {
		line := barListLine(passingCheckListColumns(item, m.multiAgent), passingCheckListRightColumns(item), item.count, maxCount, layout)
		style := summaryPanelRowStyle(item.last, now)
		fill := false
		if m.focus == focusPassingChecks && index == selected {
			style = selectedStyle
			fill = true
			selectedLine = len(lines)
		}
		lines = append(lines, tableLine{text: "  " + line, style: style, fill: fill})
	}
	if len(rows) == 0 {
		lines = append(lines, tableLine{text: "  " + m.emptyPanelText("passing checks"), style: summaryStaleRowStyle})
	}
	table := renderTableLines(lines, width, tableHeight, selectedLine)
	sparkline := m.summarySparklineView("passing checks", m.passingCheckEventTimes(), width, sparkHeight, summaryGraphStyle)
	return pinBottom(table, sparkline, width, height)
}

func (m model) failedChecksView(width int, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	rows := m.filteredFailedCheckSummaries()
	maxCount := maxFailedCheckSummaryCount(rows)
	layout := failedCheckBarListLayout(rows, max(1, width-2), m.multiAgent)
	selected := clamp(m.failedCheckCursor, 0, max(0, len(rows)-1))
	now := m.currentTime()
	sparkHeight := summarySparklineHeight(height)
	tableHeight := max(0, height-sparkHeight)
	lines := make([]tableLine, 0, len(rows)*2+1)
	selectedLine := -1
	lines = append(lines, tableLine{text: "  " + barListHeader(failedCheckListHeaderColumns(m.multiAgent), failedCheckListRightHeaderColumns(), layout), style: summaryTableHeaderStyle, fill: true})
	for index, item := range rows {
		line := barListLine(failedCheckListColumns(item, m.multiAgent), failedCheckListRightColumns(item), item.count, maxCount, layout)
		style := summaryPanelRowStyle(item.last, now)
		fill := false
		if m.focus == focusFailedChecks && index == selected {
			style = selectedStyle
			fill = true
			selectedLine = len(lines)
		}
		lines = append(lines, tableLine{text: "  " + line, style: style, fill: fill})
	}
	if len(rows) == 0 {
		lines = append(lines, tableLine{text: "  " + m.emptyPanelText("failed checks"), style: summaryStaleRowStyle})
	}
	table := renderTableLines(lines, width, tableHeight, selectedLine)
	sparkline := m.summarySparklineView("failed checks", m.failedCheckEventTimes(), width, sparkHeight, summaryGraphStyle)
	return pinBottom(table, sparkline, width, height)
}

func (m model) failureHotspotsView(width int, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	rows := m.filteredFailureHotspotRows()
	layout := failureHotspotListLayout(rows, max(1, width-2))
	selected := clamp(m.failureHotspotCursor, 0, max(0, len(rows)-1))
	lines := make([]tableLine, 0, len(rows)+1)
	selectedLine := -1
	lines = append(lines, tableLine{text: "  " + barListHeader(failureHotspotListHeaderColumns(), failureHotspotListRightHeaderColumns(), layout), style: summaryTableHeaderStyle, fill: true})
	for index, item := range rows {
		line := barListLine(failureHotspotListColumns(item), failureHotspotListRightColumns(item), item.failCount, 1, layout)
		style := summaryPanelRowStyle(item.last, m.currentTime())
		fill := false
		if m.focus == focusFailureHotspots && index == selected {
			style = selectedStyle
			fill = true
			selectedLine = len(lines)
		}
		lines = append(lines, tableLine{text: "  " + line, style: style, fill: fill})
	}
	if len(rows) == 0 {
		lines = append(lines, tableLine{text: "  " + m.emptyPanelText("failure hotspots"), style: summaryStaleRowStyle})
	}
	return renderTableLines(lines, width, height, selectedLine)
}

func (m model) failureHotspotPanelsView(width int, height int) string {
	if !m.failureHotspotPanelsSplit() {
		return renderPanelFocused("Failure Hotspots", width, height, m.failureHotspotsView(panelContentWidth(width), height-2), m.focus == focusFailureHotspots)
	}
	agents := m.failureHotspotAgents()
	heights := splitHeights(height, len(agents))
	panels := make([]string, 0, len(agents))
	for i, agent := range agents {
		title := "Failure Hotspots " + compactTargetLabel(agentLabel(agent), max(4, width-18))
		panelHeight := heights[i]
		focused := m.failureHotspotPanelHasFocus(agent)
		panels = append(panels, renderPanelFocused(title, width, panelHeight, m.failureHotspotsViewForAgent(agent, panelContentWidth(width), panelHeight-2), focused))
	}
	return lipgloss.JoinVertical(lipgloss.Left, panels...)
}

func (m model) failureHotspotPanelsSplit() bool {
	if !m.multiAgent || !m.failureHotspotsVisible() {
		return false
	}
	agents := m.failureHotspotAgents()
	if len(agents) <= 1 {
		return false
	}
	height := m.height
	if height <= 0 {
		height = 32
	}
	width := m.width
	if width <= 0 {
		width = 120
	}
	bodyHeight := max(4, height-2)
	roundTimelineHeight, checkStatusHeight, _, _ := m.dashboardPanelHeights(bodyHeight, panelContentWidth(width))
	lowerHeight := max(0, bodyHeight-checkStatusHeight-roundTimelineHeight)
	return lowerHeight >= 6 && lowerHeight >= len(agents)*3
}

func (m model) failureHotspotsViewForAgent(agent watch.AgentSnapshot, width int, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	rows := m.filteredFailureHotspotRowsForAgent(agent)
	items := make([]failureHotspotSummary, 0, len(rows))
	for _, row := range rows {
		items = append(items, row.item)
	}
	layout := failureHotspotListLayout(items, max(1, width-2))
	lines := make([]tableLine, 0, len(rows)+1)
	selectedLine := -1
	lines = append(lines, tableLine{text: "  " + barListHeader(failureHotspotListHeaderColumns(), failureHotspotListRightHeaderColumns(), layout), style: summaryTableHeaderStyle, fill: true})
	for _, row := range rows {
		item := row.item
		line := barListLine(failureHotspotListColumns(item), failureHotspotListRightColumns(item), item.failCount, 1, layout)
		style := summaryPanelRowStyle(item.last, m.currentTime())
		fill := false
		if m.failureHotspotPanelHasFocus(agent) && row.index == m.failureHotspotCursor {
			style = selectedStyle
			fill = true
			selectedLine = len(lines)
		}
		lines = append(lines, tableLine{text: "  " + line, style: style, fill: fill})
	}
	if len(rows) == 0 {
		lines = append(lines, tableLine{text: "  " + m.emptyPanelText("failure hotspots"), style: summaryStaleRowStyle})
	}
	return renderTableLines(lines, width, height, selectedLine)
}

func (m model) failureHotspotPanelHasFocus(agent watch.AgentSnapshot) bool {
	if m.focus != focusFailureHotspots || !m.failureHotspotPanelsSplit() {
		return false
	}
	return roundAgentKey(agent) == m.currentHotspotAgentKey()
}

func (m model) failureHotspotAgents() []watch.AgentSnapshot {
	return m.outcomeAgents(m.outcomeEvents())
}

func (m model) emptyPanelText(noun string) string {
	if m.hasSearchFilter() {
		return "no " + noun + " match"
	}
	return "no " + noun
}

func summarySparklineHeight(height int) int {
	if height >= 10 {
		return summarySparklineRows
	}
	if height >= 8 {
		return 4
	}
	if height >= 6 {
		return 3
	}
	return 0
}

func (m model) summarySparklineView(label string, times []time.Time, width int, height int, style lipgloss.Style) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	histogram := recentEventHistogram(times, width, summarySparklineWindow, m.currentTime())
	plotHeight := max(1, height-2)
	eventsPerRow := sparklineEventsPerRow(histogram.max, plotHeight)
	scaleText := ""
	if eventsPerRow > 1 {
		scaleText = fmt.Sprintf(" scale=%d/row", eventsPerRow)
	}
	headerPlain := fmt.Sprintf("%s events last=%s count=%d peak=%d%s", label, formatBucketDuration(summarySparklineWindow), histogram.count, histogram.max, scaleText)
	header := summaryKeyStyle.Render(label+" events ") +
		summaryValueStyle.Render("last="+formatBucketDuration(summarySparklineWindow)) +
		summaryKeyStyle.Render(" count=") +
		summaryValueStyle.Render(fmt.Sprint(histogram.count)) +
		summaryKeyStyle.Render(" peak=") +
		summaryValueStyle.Render(fmt.Sprint(histogram.max))
	if scaleText != "" {
		header += summaryKeyStyle.Render(" scale=") + summaryValueStyle.Render(fmt.Sprintf("%d/row", eventsPerRow))
	}
	if height == 1 {
		return fitANSI(header, width)
	}
	if lipgloss.Width(headerPlain) > width {
		header = summaryValueStyle.Render(fit(headerPlain, width))
	}
	lines := []string{fitANSI(header, width)}
	lines = append(lines, renderSparkline(histogram.counts, histogram.max, width, plotHeight, style)...)
	lines = append(lines, summarySparklineAxis(width, summarySparklineWindow))
	return strings.Join(lines[:min(len(lines), height)], "\n")
}

func (m model) passingCheckEventTimes() []time.Time {
	times := make([]time.Time, 0, len(m.passingChecks))
	for _, passingCheck := range m.passingChecks {
		times = append(times, passingCheck.when)
	}
	sort.SliceStable(times, func(i, j int) bool {
		return times[i].Before(times[j])
	})
	return times
}

func (m model) failedCheckEventTimes() []time.Time {
	times := make([]time.Time, 0, len(m.failedChecks))
	for _, failedCheck := range m.failedChecks {
		times = append(times, failedCheck.when)
	}
	sort.SliceStable(times, func(i, j int) bool {
		return times[i].Before(times[j])
	})
	return times
}

func pinBottom(top string, bottom string, width int, height int) string {
	if height <= 0 {
		return ""
	}
	topLines := panelBodyLines(top)
	bottomLines := panelBodyLines(bottom)
	if len(bottomLines) > height {
		bottomLines = bottomLines[len(bottomLines)-height:]
	}
	maxTop := max(0, height-len(bottomLines))
	if len(topLines) > maxTop {
		topLines = topLines[:maxTop]
	}
	lines := make([]string, 0, height)
	lines = append(lines, topLines...)
	for len(lines) < maxTop {
		lines = append(lines, "")
	}
	lines = append(lines, bottomLines...)
	for len(lines) < height {
		lines = append(lines, "")
	}
	for i := range lines {
		lines[i] = fitANSI(lines[i], width)
	}
	return strings.Join(lines, "\n")
}

type tableLine struct {
	text  string
	style lipgloss.Style
	fill  bool
}

type barListLayout struct {
	columnWidths []int
	barWidth     int
	rightWidths  []int
}

const listColumnGap = 2

func summaryPanelRowStyle(last time.Time, now time.Time) lipgloss.Style {
	if last.IsZero() {
		return summaryStaleRowStyle
	}
	if now.IsZero() {
		now = time.Now()
	}
	age := now.Sub(last)
	if age < 0 {
		age = 0
	}
	switch {
	case age <= recencyFreshWindow:
		return summaryFreshRowStyle
	case age <= recencyWarmWindow:
		return summaryValueStyle
	default:
		return summaryStaleRowStyle
	}
}

func passingCheckBarListLayout(rows []passingCheckSummary, width int, multiAgent bool) barListLayout {
	columns := [][]string{passingCheckListHeaderColumns(multiAgent)}
	rights := [][]string{passingCheckListRightHeaderColumns()}
	for _, row := range rows {
		columns = append(columns, passingCheckListColumns(row, multiAgent))
		rights = append(rights, passingCheckListRightColumns(row))
	}
	return newPlainListLayout(width, columns, rights)
}

func failedCheckBarListLayout(rows []failedCheckSummary, width int, multiAgent bool) barListLayout {
	columns := [][]string{failedCheckListHeaderColumns(multiAgent)}
	rights := [][]string{failedCheckListRightHeaderColumns()}
	for _, row := range rows {
		columns = append(columns, failedCheckListColumns(row, multiAgent))
		rights = append(rights, failedCheckListRightColumns(row))
	}
	return newPlainListLayout(width, columns, rights)
}

func failureHotspotListLayout(rows []failureHotspotSummary, width int) barListLayout {
	columns := [][]string{failureHotspotListHeaderColumns()}
	rights := [][]string{failureHotspotListRightHeaderColumns()}
	for _, row := range rows {
		columns = append(columns, failureHotspotListColumns(row))
		rights = append(rights, failureHotspotListRightColumns(row))
	}
	return newPlainListLayout(width, columns, rights)
}

func newPlainListLayout(width int, columns [][]string, rights [][]string) barListLayout {
	return newListLayout(width, columns, rights, false)
}

func newBarListLayout(width int, columns [][]string, rights [][]string) barListLayout {
	return newListLayout(width, columns, rights, true)
}

func newListLayout(width int, columns [][]string, rights [][]string, includeBar bool) barListLayout {
	if width <= 0 {
		return barListLayout{}
	}
	emptyRows := len(columns) <= 1
	columnWidths := maxColumnWidths(columns)
	rightWidths := maxColumnWidths(rights)
	rightWidth := joinedWidths(rightWidths)
	barWidth := 0
	if includeBar {
		barWidth = countBarWidth(width)
		barWidth = min(barWidth, max(4, width-joinedWidths(columnWidths)-rightWidth-listColumnGap*2))
	}
	gapWidth := listColumnGap
	if includeBar {
		gapWidth = listColumnGap*2 + barWidth
	}
	leftBudget := max(4, width-rightWidth-gapWidth)
	columnWidths = shrinkWidths(columnWidths, leftBudget, 4)
	if emptyRows {
		columnWidths = expandWidthsEvenly(columnWidths, leftBudget)
	}
	if includeBar {
		if joinedWidths(columnWidths)+barWidth+rightWidth+listColumnGap*2 > width {
			barWidth = max(4, width-joinedWidths(columnWidths)-rightWidth-listColumnGap*2)
		}
		if joinedWidths(columnWidths)+rightWidth+listColumnGap*2 < width {
			barWidth = max(4, width-joinedWidths(columnWidths)-rightWidth-listColumnGap*2)
		}
	} else {
		budget := max(1, width-joinedWidths(rightWidths)-listColumnGap)
		columnWidths = expandWidthsEvenly(columnWidths, budget)
	}
	return barListLayout{columnWidths: columnWidths, barWidth: barWidth, rightWidths: rightWidths}
}

func expandWidthsEvenly(widths []int, budget int) []int {
	out := append([]int(nil), widths...)
	if len(out) == 0 || budget <= 0 || joinedWidths(out) >= budget {
		return out
	}
	available := max(0, budget-listColumnGap*(len(out)-1))
	base := available / len(out)
	remainder := available % len(out)
	for i := range out {
		target := base
		if i < remainder {
			target++
		}
		if target > out[i] {
			out[i] = target
		}
	}
	for joinedWidths(out) < budget {
		for i := range out {
			if joinedWidths(out) >= budget {
				break
			}
			out[i]++
		}
	}
	if joinedWidths(out) > budget {
		out = shrinkWidths(out, budget, 1)
	}
	return out
}

func maxColumnWidths(rows [][]string) []int {
	count := 0
	for _, row := range rows {
		count = max(count, len(row))
	}
	widths := make([]int, count)
	for _, row := range rows {
		for i, value := range row {
			widths[i] = max(widths[i], lipgloss.Width(value))
		}
	}
	return widths
}

func shrinkWidths(widths []int, budget int, minWidth int) []int {
	out := append([]int(nil), widths...)
	for len(out) > 0 && joinedWidths(out) > budget {
		index := 0
		for i := range out {
			if out[i] > out[index] {
				index = i
			}
		}
		if out[index] <= minWidth {
			break
		}
		out[index]--
	}
	return out
}

func joinedWidths(widths []int) int {
	total := 0
	for _, width := range widths {
		total += width
	}
	if len(widths) > 1 {
		total += listColumnGap * (len(widths) - 1)
	}
	return total
}

func barListHeader(columns []string, rightColumns []string, layout barListLayout) string {
	if len(layout.columnWidths) == 0 {
		return ""
	}
	left := renderBarListColumns(columns, layout.columnWidths, false)
	right := renderBarListColumns(rightColumns, layout.rightWidths, true)
	if layout.barWidth <= 0 {
		return joinListColumns(left, right)
	}
	return left + strings.Repeat(" ", listColumnGap) + strings.Repeat(" ", layout.barWidth) + strings.Repeat(" ", listColumnGap) + right
}

func barListLine(columns []string, rightColumns []string, count int, maxCount int, layout barListLayout) string {
	if len(layout.columnWidths) == 0 {
		return ""
	}
	left := renderBarListColumns(columns, layout.columnWidths, false)
	right := renderBarListColumns(rightColumns, layout.rightWidths, true)
	if layout.barWidth <= 0 {
		return joinListColumns(left, right)
	}
	return left + strings.Repeat(" ", listColumnGap) + countBar(count, maxCount, layout.barWidth) + strings.Repeat(" ", listColumnGap) + right
}

func joinListColumns(left string, right string) string {
	if right == "" {
		return left
	}
	if left == "" {
		return right
	}
	return left + strings.Repeat(" ", listColumnGap) + right
}

func renderBarListColumns(values []string, widths []int, rightAlign bool) string {
	parts := make([]string, 0, len(widths))
	for i, width := range widths {
		value := ""
		if i < len(values) {
			value = values[i]
		}
		if rightAlign {
			parts = append(parts, padLeftVisible(value, width))
		} else {
			parts = append(parts, padVisible(value, width))
		}
	}
	return strings.Join(parts, strings.Repeat(" ", listColumnGap))
}

func countBarWidth(width int) int {
	return clamp(width/8, 4, 18)
}

func passingCheckListHeaderColumns(multiAgent bool) []string {
	if multiAgent {
		return []string{"Target", "Check"}
	}
	return []string{"Check"}
}

func passingCheckListColumns(item passingCheckSummary, multiAgent bool) []string {
	target := compactTargetLabel(firstNonEmpty(item.target.Name, item.target.SSID, item.target.BSSID, "-"), 18)
	step := firstNonEmpty(item.step.Name, item.step.Type, "step")
	if multiAgent {
		return []string{target, step}
	}
	return []string{step}
}

func passingCheckListRightHeaderColumns() []string {
	return []string{"Cnt", "Avg", "Max", "Last"}
}

func passingCheckListRightColumns(item passingCheckSummary) []string {
	return []string{fmt.Sprint(item.count), durationLabel(item.avgDuration()), durationLabel(item.maxDuration), item.last.Format("15:04:05")}
}

func failedCheckListHeaderColumns(multiAgent bool) []string {
	if multiAgent {
		return []string{"Target", "Check", "Metric"}
	}
	return []string{"Check", "Metric"}
}

func failedCheckListColumns(item failedCheckSummary, multiAgent bool) []string {
	target := compactTargetLabel(firstNonEmpty(item.finding.Target, item.target.Name, item.target.SSID, item.target.BSSID, "-"), 18)
	check := firstNonEmpty(item.finding.Check, "check")
	metric := firstNonEmpty(item.finding.Metric, "status")
	if multiAgent {
		return []string{target, check, metric}
	}
	return []string{check, metric}
}

func failedCheckListRightHeaderColumns() []string {
	return []string{"Cnt", "Fail%", "Strk", "Last"}
}

func failedCheckListRightColumns(item failedCheckSummary) []string {
	return []string{fmt.Sprint(item.count), fmt.Sprintf("%d%%", item.failPercent), fmt.Sprint(item.failStreak), item.last.Format("15:04:05")}
}

func failureHotspotListHeaderColumns() []string {
	return []string{"Target", "Cause"}
}

func failureHotspotListColumns(item failureHotspotSummary) []string {
	target := compactTargetLabel(firstNonEmpty(item.target.Name, item.target.SSID, item.target.BSSID, item.latestFinding.Target, "-"), 18)
	return []string{target, item.latestCause}
}

func failureHotspotListRightHeaderColumns() []string {
	return []string{"Fail%", "Fail/Run", "Last"}
}

func failureHotspotListRightColumns(item failureHotspotSummary) []string {
	rate := 0
	if item.runCount > 0 {
		rate = item.failRunCount * 100 / item.runCount
	}
	return []string{
		fmt.Sprintf("%d%%", rate),
		fmt.Sprintf("%d/%d", item.failCount, item.runCount),
		item.last.Format("15:04:05"),
	}
}

func countBar(count int, maxCount int, width int) string {
	if width <= 0 {
		return ""
	}
	if count <= 0 || maxCount <= 0 {
		return strings.Repeat(" ", width)
	}
	filled := max(1, (count*width+maxCount-1)/maxCount)
	filled = clamp(filled, 1, width)
	return strings.Repeat("█", filled) + strings.Repeat(" ", width-filled)
}

func maxPassingCheckSummaryCount(rows []passingCheckSummary) int {
	maxCount := 1
	for _, row := range rows {
		if row.count > maxCount {
			maxCount = row.count
		}
	}
	return maxCount
}

func maxFailedCheckSummaryCount(rows []failedCheckSummary) int {
	maxCount := 1
	for _, row := range rows {
		if row.count > maxCount {
			maxCount = row.count
		}
	}
	return maxCount
}

type passingCheckSummaryRow struct {
	index int
	item  passingCheckSummary
}

type passingCheckSummaryGroup struct {
	ssid string
	rows []passingCheckSummaryRow
}

type failedCheckSummaryRow struct {
	index int
	item  failedCheckSummary
}

type failedCheckSummaryGroup struct {
	ssid string
	rows []failedCheckSummaryRow
}

func renderTableLines(lines []tableLine, width int, visible int, selectedLine int) string {
	if visible <= 0 || len(lines) == 0 {
		return ""
	}
	start := 0
	if selectedLine >= 0 {
		start = correctedOffset(selectedLine, visible, len(lines))
	}
	end := min(len(lines), start+visible)
	var b strings.Builder
	for _, line := range lines[start:end] {
		text := fit(line.text, width)
		if line.fill {
			text = padToWidth(text, width)
		}
		b.WriteString(line.style.Render(text))
		b.WriteByte('\n')
	}
	return b.String()
}

func groupPassingCheckSummaries(rows []passingCheckSummary) []passingCheckSummaryGroup {
	groups := make([]passingCheckSummaryGroup, 0, len(rows))
	index := make(map[string]int, len(rows))
	for i, row := range rows {
		ssid := passingCheckSummarySSID(row)
		pos, ok := index[ssid]
		if !ok {
			pos = len(groups)
			index[ssid] = pos
			groups = append(groups, passingCheckSummaryGroup{ssid: ssid})
		}
		groups[pos].rows = append(groups[pos].rows, passingCheckSummaryRow{index: i, item: row})
	}
	return groups
}

func groupFailedCheckSummaries(rows []failedCheckSummary) []failedCheckSummaryGroup {
	groups := make([]failedCheckSummaryGroup, 0, len(rows))
	index := make(map[string]int, len(rows))
	for i, row := range rows {
		ssid := failedCheckSummarySSID(row)
		pos, ok := index[ssid]
		if !ok {
			pos = len(groups)
			index[ssid] = pos
			groups = append(groups, failedCheckSummaryGroup{ssid: ssid})
		}
		groups[pos].rows = append(groups[pos].rows, failedCheckSummaryRow{index: i, item: row})
	}
	return groups
}

func passingCheckSummarySSID(row passingCheckSummary) string {
	return firstNonEmpty(row.target.SSID, row.target.BSSID, row.target.Name, "-")
}

func failedCheckSummarySSID(row failedCheckSummary) string {
	return firstNonEmpty(row.target.SSID, row.target.BSSID, row.finding.Target, "-")
}

func (m model) passingCheckDetailView(width int, height int) string {
	rows := m.filteredPassingCheckSummaries()
	if len(rows) == 0 || height <= 0 {
		return dimStyle.Render("no passing check selected")
	}
	selected := clamp(m.passingCheckCursor, 0, len(rows)-1)
	item := rows[selected]
	histogram := recentEventHistogram(m.passingCheckOccurrences(item.agent, item.target, item.step), width, summarySparklineWindow, m.currentTime())
	targetLine := keyStyle.Render("target=") + valueStyle.Render(item.target.Name) +
		keyStyle.Render("  ssid=") + valueStyle.Render(firstNonEmpty(item.target.SSID, "-")) +
		keyStyle.Render("  step=") + valueStyle.Render(item.step.Name)
	if m.multiAgent && agentKey(item.agent) != "" {
		targetLine = keyStyle.Render("agent=") + valueStyle.Render(agentLabel(item.agent)) +
			keyStyle.Render("  ") + targetLine
	}
	lines := []string{
		targetLine,
		keyStyle.Render("status=") + valueStyle.Render(firstNonEmpty(item.step.Status, "ok")) +
			keyStyle.Render("  type=") + valueStyle.Render(firstNonEmpty(item.step.Type, "-")) +
			keyStyle.Render("  op=") + valueStyle.Render(firstNonEmpty(item.step.Operation, "-")) +
			keyStyle.Render("  duration=") + valueStyle.Render(durationLabel(item.duration)),
		keyStyle.Render("count=") + valueStyle.Render(fmt.Sprint(item.count)) +
			keyStyle.Render("  last=") + valueStyle.Render(item.last.Format("15:04:05")),
	}
	if histogram.count > 0 {
		lines[2] += detailHistogramSummary(histogram, detailCompactGraphHeight(height, len(lines), 2))
	}
	sections := []detailSection{
		{title: "recent passes", rows: m.passingCheckDetailRows(item)},
		{title: "logs", rows: m.passingCheckRelatedLogLines(item, detailModalLogLimit)},
	}
	return denseDetailView(lines, histogram, okGraphStyle, sections, width, height)
}

func (m model) failedCheckDetailView(width int, height int) string {
	rows := m.filteredFailedCheckSummaries()
	if len(rows) == 0 || height <= 0 {
		return dimStyle.Render("no failed check selected")
	}
	selected := clamp(m.failedCheckCursor, 0, len(rows)-1)
	item := rows[selected]
	finding := item.finding
	histogram := recentEventHistogram(m.failedCheckOccurrences(item.agent, item.target, finding), width, summarySparklineWindow, m.currentTime())
	targetLine := keyStyle.Render("target=") + valueStyle.Render(finding.Target) +
		keyStyle.Render("  check=") + valueStyle.Render(finding.Check) +
		keyStyle.Render("  metric=") + valueStyle.Render(finding.Metric)
	if m.multiAgent && agentKey(item.agent) != "" {
		targetLine = keyStyle.Render("agent=") + valueStyle.Render(agentLabel(item.agent)) +
			keyStyle.Render("  ") + targetLine
	}
	lines := []string{
		targetLine,
		keyStyle.Render("observed=") + valueStyle.Render(finding.Observed) +
			keyStyle.Render("  expected=") + valueStyle.Render(detailValue(finding.Expected)) +
			keyStyle.Render("  message=") + valueStyle.Render(finding.Message),
		keyStyle.Render("count=") + valueStyle.Render(fmt.Sprint(item.count)) +
			keyStyle.Render("  last=") + valueStyle.Render(item.last.Format("15:04:05")),
	}
	if histogram.count > 0 {
		lines[2] += detailHistogramSummary(histogram, detailCompactGraphHeight(height, len(lines), 2))
	}
	sections := []detailSection{
		{title: "recent failures", rows: m.failedCheckDetailRows(item)},
		{title: "logs", rows: m.failedCheckRelatedLogLines(item, detailModalLogLimit)},
	}
	return denseDetailView(lines, histogram, failGraphStyle, sections, width, height)
}

func (m model) failureHotspotDetailView(width int, height int) string {
	rows := m.focusedFailureHotspotRows()
	if len(rows) == 0 || height <= 0 {
		return dimStyle.Render("no failure hotspot selected")
	}
	selected := 0
	for i, row := range rows {
		if row.index == m.failureHotspotCursor {
			selected = i
			break
		}
	}
	item := rows[selected].item
	finding := item.latestFinding
	histogram := recentEventHistogram(m.failureHotspotOccurrences(item), width, summarySparklineWindow, m.currentTime())
	rate := 0
	if item.runCount > 0 {
		rate = item.failRunCount * 100 / item.runCount
	}
	targetLine := keyStyle.Render("target=") + valueStyle.Render(checkStatusTargetLabel(item.target)) +
		keyStyle.Render("  ssid=") + valueStyle.Render(firstNonEmpty(item.target.SSID, "-")) +
		keyStyle.Render("  band=") + valueStyle.Render(firstNonEmpty(item.target.Band, "-"))
	if m.multiAgent && agentKey(item.agent) != "" {
		targetLine = keyStyle.Render("agent=") + valueStyle.Render(agentLabel(item.agent)) +
			keyStyle.Render("  ") + targetLine
	}
	lines := []string{
		targetLine,
		keyStyle.Render("cause=") + valueStyle.Render(firstNonEmpty(item.latestCause, "-")),
		keyStyle.Render("fail_rate=") + valueStyle.Render(fmt.Sprintf("%d%%", rate)) +
			keyStyle.Render("  fail_runs=") + valueStyle.Render(fmt.Sprintf("%d/%d", item.failRunCount, item.runCount)) +
			keyStyle.Render("  failures=") + valueStyle.Render(fmt.Sprint(item.failCount)) +
			keyStyle.Render("  streak=") + valueStyle.Render(fmt.Sprint(item.failStreak)) +
			keyStyle.Render("  last=") + valueStyle.Render(item.last.Format("15:04:05")),
		keyStyle.Render("check=") + valueStyle.Render(firstNonEmpty(finding.Check, "-")) +
			keyStyle.Render("  metric=") + valueStyle.Render(firstNonEmpty(finding.Metric, "-")) +
			keyStyle.Render("  observed=") + valueStyle.Render(firstNonEmpty(finding.Observed, "-")) +
			keyStyle.Render("  expected=") + valueStyle.Render(detailValue(finding.Expected)),
	}
	if histogram.count > 0 {
		lines[2] += detailHistogramSummary(histogram, detailCompactGraphHeight(height, len(lines), 3))
	}
	sections := []detailSection{
		{title: "causes", rows: m.failureHotspotCauseRows(item)},
		{title: "recent failures", rows: m.failureHotspotDetailRows(item)},
		{title: "logs", rows: m.failureHotspotRelatedLogLines(item, detailModalLogLimit)},
	}
	return denseDetailView(lines, histogram, failGraphStyle, sections, width, height)
}

type detailSection struct {
	title string
	rows  []string
}

func denseDetailView(summary []string, histogram occurrenceHistogram, graphStyle lipgloss.Style, sections []detailSection, width int, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	lines := make([]string, 0, height)
	for _, line := range summary {
		if len(lines) >= height {
			return strings.Join(lines, "\n")
		}
		lines = append(lines, fitANSI(line, width))
	}
	graphHeight := detailCompactGraphHeight(height, len(lines), len(sections))
	if histogram.count > 0 && graphHeight > 0 && len(lines)+graphHeight <= height {
		lines = append(lines, renderDetailHistogram(histogram, width, graphHeight, graphStyle)...)
	}
	remaining := height - len(lines)
	if remaining <= 0 || len(sections) == 0 {
		return strings.Join(lines[:min(len(lines), height)], "\n")
	}
	allocations := detailSectionAllocations(sections, remaining)
	for i, section := range sections {
		if len(lines) >= height || allocations[i] <= 0 {
			continue
		}
		lines = append(lines, fitANSI(summaryKeyStyle.Render(section.title), width))
		rowLimit := allocations[i] - 1
		rows := section.rows
		if len(rows) == 0 {
			rows = []string{dimStyle.Render("  no matching entries")}
		}
		for _, row := range rows[:min(len(rows), rowLimit)] {
			if len(lines) >= height {
				break
			}
			lines = append(lines, fitANSI(row, width))
		}
	}
	for len(lines) < height {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

func detailCompactGraphHeight(height int, used int, sectionCount int) int {
	remaining := height - used
	if remaining < sectionCount*2+4 {
		return 0
	}
	if remaining >= 12 {
		return 4
	}
	return 3
}

func detailSectionAllocations(sections []detailSection, available int) []int {
	allocations := make([]int, len(sections))
	if len(sections) == 0 || available <= 0 {
		return allocations
	}
	weights := make([]int, len(sections))
	totalWeight := 0
	for i, section := range sections {
		weight := 1
		if section.title == "logs" {
			weight = 2
		}
		weights[i] = weight
		totalWeight += weight
	}
	remaining := available
	for i := range sections {
		value := max(1, available*weights[i]/totalWeight)
		allocations[i] = min(value, remaining)
		remaining -= allocations[i]
	}
	for remaining > 0 {
		changed := false
		for i := range allocations {
			if remaining <= 0 {
				break
			}
			allocations[i]++
			remaining--
			changed = true
		}
		if !changed {
			break
		}
	}
	return allocations
}

func (m model) passingCheckDetailRows(summary passingCheckSummary) []string {
	items := make([]passingCheckState, 0, summary.count)
	for _, item := range m.passingChecks {
		if passingCheckSummaryKey(item.target, item.step) == passingCheckSummaryKey(summary.target, summary.step) {
			items = append(items, item)
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].when.After(items[j].when)
	})
	rows := []string{summaryKeyStyle.Render("  time      round  status  dur     op/type")}
	for _, item := range items {
		op := firstNonEmpty(item.step.Operation, item.step.Type, "-")
		rows = append(rows, valueStyle.Render(fmt.Sprintf("  %s  %-5s  %-6s  %-6s  %s",
			item.when.Format("15:04:05"),
			roundLabel(item.round),
			firstNonEmpty(item.step.Status, "ok"),
			durationLabel(item.duration),
			op,
		)))
	}
	return rows
}

func (m model) failedCheckDetailRows(summary failedCheckSummary) []string {
	items := make([]failedCheckState, 0, summary.count)
	for _, item := range m.failedChecks {
		if failedCheckSummaryKey(item.target, item.finding) == failedCheckSummaryKey(summary.target, summary.finding) {
			items = append(items, item)
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].when.After(items[j].when)
	})
	rows := []string{summaryKeyStyle.Render("  time      round  check             metric      observed  expected  message")}
	for _, item := range items {
		finding := item.finding
		rows = append(rows, valueStyle.Render(fmt.Sprintf("  %s  %-5s  %-16s  %-10s  %-8s  %-8s  %s",
			item.when.Format("15:04:05"),
			roundLabel(item.round),
			firstNonEmpty(finding.Check, "-"),
			firstNonEmpty(finding.Metric, "-"),
			firstNonEmpty(finding.Observed, "-"),
			detailValue(finding.Expected),
			firstNonEmpty(finding.Message, "-"),
		)))
	}
	return rows
}

func (m model) failureHotspotDetailRows(summary failureHotspotSummary) []string {
	key := failureHotspotSummaryIdentity(summary)
	items := make([]failedCheckState, 0, summary.failCount)
	for _, item := range m.failedChecks {
		if failureHotspotIdentity(item.agent, item.target, item.finding.Target) == key {
			items = append(items, item)
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].when.After(items[j].when)
	})
	rows := []string{summaryKeyStyle.Render("  time      round  check             metric      observed  expected  cause")}
	for _, item := range items {
		finding := item.finding
		rows = append(rows, valueStyle.Render(fmt.Sprintf("  %s  %-5s  %-16s  %-10s  %-8s  %-8s  %s",
			item.when.Format("15:04:05"),
			roundLabel(item.round),
			firstNonEmpty(finding.Check, "-"),
			firstNonEmpty(finding.Metric, "-"),
			firstNonEmpty(finding.Observed, "-"),
			detailValue(finding.Expected),
			failureHotspotCause(finding),
		)))
	}
	return rows
}

type failureHotspotCauseSummary struct {
	cause string
	count int
	last  time.Time
}

func (m model) failureHotspotCauseRows(summary failureHotspotSummary) []string {
	key := failureHotspotSummaryIdentity(summary)
	index := map[string]int{}
	rows := make([]failureHotspotCauseSummary, 0)
	for _, item := range m.failedChecks {
		if failureHotspotIdentity(item.agent, item.target, item.finding.Target) != key {
			continue
		}
		cause := failureHotspotCause(item.finding)
		pos, ok := index[cause]
		if !ok {
			pos = len(rows)
			index[cause] = pos
			rows = append(rows, failureHotspotCauseSummary{cause: cause})
		}
		rows[pos].count++
		if item.when.After(rows[pos].last) || rows[pos].last.IsZero() {
			rows[pos].last = item.when
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].count != rows[j].count {
			return rows[i].count > rows[j].count
		}
		return rows[i].last.After(rows[j].last)
	})
	out := []string{summaryKeyStyle.Render("  count  last      cause")}
	for _, row := range rows {
		out = append(out, valueStyle.Render(fmt.Sprintf("  %-5d  %-8s  %s", row.count, row.last.Format("15:04:05"), row.cause)))
	}
	return out
}

func (m model) passingCheckRelatedLogLines(item passingCheckSummary, limit int) []string {
	return m.scopedLogLines(limit, func(entry eventLogEntry) bool {
		event := entry.event
		if event.Kind == "" || !scopedEventAgentMatches(event.Agent, item.agent) || !scopedEventTargetMatches(event, item.target) {
			return false
		}
		if targetSummaryStep(item.step) {
			return event.Kind == watch.EventTargetStarted || event.Kind == watch.EventTargetFinished
		}
		if !eventMatchesStep(event, item.step) {
			return false
		}
		return event.Kind == watch.EventStepStarted || event.Kind == watch.EventStepFinished || event.Kind == watch.EventLog
	})
}

func (m model) failedCheckRelatedLogLines(item failedCheckSummary, limit int) []string {
	return m.scopedLogLines(limit, func(entry eventLogEntry) bool {
		event := entry.event
		if event.Kind == "" || !scopedEventAgentMatches(event.Agent, item.agent) || !scopedEventTargetMatches(event, item.target, item.finding.Target) {
			return false
		}
		if event.Finding != nil && failedCheckKey(*event.Finding) == failedCheckKey(item.finding) {
			return true
		}
		if !eventMatchesFinding(event, item.finding) {
			return false
		}
		return event.Kind == watch.EventStepStarted || event.Kind == watch.EventStepFinished || event.Kind == watch.EventLog
	})
}

func (m model) failureHotspotRelatedLogLines(item failureHotspotSummary, limit int) []string {
	return m.scopedLogLines(limit, func(entry eventLogEntry) bool {
		event := entry.event
		if event.Kind == "" || !scopedEventAgentMatches(event.Agent, item.agent) || !scopedEventTargetMatches(event, item.target, item.latestFinding.Target) {
			return false
		}
		switch event.Kind {
		case watch.EventFinding:
			return true
		case watch.EventStepFinished:
			return eventFailureLike(event)
		case watch.EventStepStarted:
			return eventMatchesFinding(event, item.latestFinding)
		case watch.EventLog:
			return eventFailureLike(event)
		default:
			return false
		}
	})
}

func (m model) scopedLogLines(limit int, match func(eventLogEntry) bool) []string {
	if limit <= 0 {
		return nil
	}
	rows := make([]string, 0, limit)
	for i := len(m.eventLogEntries) - 1; i >= 0 && len(rows) < limit; i-- {
		entry := m.eventLogEntries[i]
		if match(entry) {
			rows = append(rows, valueStyle.Render("  "+entry.line))
		}
	}
	return rows
}

func scopedEventAgentMatches(eventAgent watch.AgentSnapshot, selected watch.AgentSnapshot) bool {
	if agentKey(selected) == "" || agentKey(eventAgent) == "" {
		return true
	}
	return sameAgent(eventAgent, selected)
}

func scopedEventTargetMatches(event watch.Event, target watch.TargetSnapshot, aliases ...string) bool {
	expected := scopedTargetKey(target, aliases...)
	actual := scopedEventTargetKey(event)
	return expected != "" && actual != "" && expected == actual
}

func scopedEventTargetKey(event watch.Event) string {
	findingTarget := ""
	if event.Finding != nil {
		findingTarget = event.Finding.Target
	}
	return scopedTargetKey(event.Target, findingTarget)
}

func scopedTargetKey(target watch.TargetSnapshot, aliases ...string) string {
	values := []string{target.Name}
	values = append(values, aliases...)
	values = append(values, target.SSID, target.BSSID)
	return normalizedScopeToken(firstNonEmpty(values...))
}

func eventMatchesFinding(event watch.Event, finding watch.Finding) bool {
	if event.Finding != nil && failedCheckKey(*event.Finding) == failedCheckKey(finding) {
		return true
	}
	return eventMatchesStep(event, watch.StepSnapshot{Name: finding.Check, Type: finding.Check})
}

func eventMatchesStep(event watch.Event, step watch.StepSnapshot) bool {
	expected := scopeTokenSet(step.Name, step.Type, step.Operation)
	if len(expected) == 0 {
		return false
	}
	actual := scopeTokenList(event.Step.Name, event.Step.Type, event.Step.Operation)
	if event.Finding != nil {
		actual = append(actual, scopeTokenList(event.Finding.Check)...)
	}
	for _, token := range actual {
		if _, ok := expected[token]; ok {
			return true
		}
	}
	return false
}

func scopeTokenSet(values ...string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, token := range scopeTokenList(values...) {
		out[token] = struct{}{}
	}
	return out
}

func scopeTokenList(values ...string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		token := normalizedScopeToken(value)
		if token == "" || token == "-" {
			continue
		}
		if _, ok := seen[token]; ok {
			continue
		}
		seen[token] = struct{}{}
		out = append(out, token)
	}
	return out
}

func normalizedScopeToken(value string) string {
	return strings.ToLower(strings.TrimSpace(sanitizeLogText(value)))
}

func targetSummaryStep(step watch.StepSnapshot) bool {
	return normalizedScopeToken(firstNonEmpty(step.Name, step.Type)) == "target"
}

func eventFailureLike(event watch.Event) bool {
	status := normalizedScopeToken(firstNonEmpty(event.Status, event.Step.Status))
	if status == "failed" || status == "fail" || status == "warn" || status == "warning" || status == "error" {
		return true
	}
	message := normalizedScopeToken(firstNonEmpty(event.Message, event.Step.Message, event.Step.Error))
	return strings.Contains(message, "fail") ||
		strings.Contains(message, "reject") ||
		strings.Contains(message, "declined") ||
		strings.Contains(message, "timeout") ||
		strings.Contains(message, "not available") ||
		strings.Contains(message, "constraint")
}

func roundLabel(round uint64) string {
	if round == 0 {
		return "-"
	}
	return fmt.Sprint(round)
}

func detailHistogramSummary(histogram occurrenceHistogram, graphHeight int) string {
	plotHeight := max(1, graphHeight-1)
	eventsPerRow := sparklineEventsPerRow(histogram.max, plotHeight)
	out := keyStyle.Render("  events=") +
		valueStyle.Render("last="+formatBucketDuration(summarySparklineWindow)) +
		keyStyle.Render(" count=") +
		valueStyle.Render(fmt.Sprint(histogram.count)) +
		keyStyle.Render(" peak=") +
		valueStyle.Render(fmt.Sprint(histogram.max))
	if eventsPerRow > 1 {
		out += keyStyle.Render(" scale=") + valueStyle.Render(fmt.Sprintf("%d/row", eventsPerRow))
	}
	return out
}

func renderDetailHistogram(histogram occurrenceHistogram, width int, height int, style lipgloss.Style) []string {
	if width <= 0 || height <= 0 || len(histogram.counts) == 0 || histogram.max <= 0 {
		return nil
	}
	if height == 1 {
		return renderSparkline(histogram.counts, histogram.max, width, 1, style)
	}
	lines := renderSparkline(histogram.counts, histogram.max, width, max(1, height-1), style)
	lines = append(lines, summarySparklineAxis(width, summarySparklineWindow))
	return lines[:min(len(lines), height)]
}

func (m model) detailModal(width int, height int) string {
	switch m.detailPanel {
	case focusPassingChecks:
		return m.passingCheckDetailModal(width, height)
	case focusFailureHotspots:
		return m.failureHotspotDetailModal(width, height)
	default:
		return m.failedCheckDetailModal(width, height)
	}
}

func (m model) passingCheckDetailModal(width int, height int) string {
	modalWidth := max(2, width*90/100)
	modalHeight := detailModalHeight(height)
	return renderPanel("Passing Check Detail", modalWidth, modalHeight, m.passingCheckDetailView(panelContentWidth(modalWidth), modalHeight-2))
}

func (m model) failedCheckDetailModal(width int, height int) string {
	modalWidth := max(2, width*90/100)
	modalHeight := detailModalHeight(height)
	return renderPanel("Failed Check Detail", modalWidth, modalHeight, m.failedCheckDetailView(panelContentWidth(modalWidth), modalHeight-2))
}

func (m model) failureHotspotDetailModal(width int, height int) string {
	modalWidth := max(2, width*90/100)
	modalHeight := detailModalHeight(height)
	return renderPanel("Failure Hotspot Detail", modalWidth, modalHeight, m.failureHotspotDetailView(panelContentWidth(modalWidth), modalHeight-2))
}

func detailModalHeight(height int) int {
	return max(2, height*detailModalHeightPercent/100)
}

func overlayModal(frame string, width int, height int, modal string) string {
	modalWidth := lipgloss.Width(modal)
	modalHeight := lipgloss.Height(modal)
	if modalWidth <= 0 || modalHeight <= 0 {
		return frame
	}
	x := max(0, (width-modalWidth)/2)
	y := max(0, (height-modalHeight)/2)
	frameLines := strings.Split(frame, "\n")
	for len(frameLines) < height {
		frameLines = append(frameLines, valueStyle.Render(strings.Repeat(" ", width)))
	}
	modalLines := strings.Split(modal, "\n")
	for i, modalLine := range modalLines {
		row := y + i
		if row < 0 || row >= len(frameLines) {
			continue
		}
		line := frameLines[row]
		left := ansi.Cut(line, 0, x)
		right := ansi.Cut(line, x+modalWidth, width)
		frameLines[row] = left + modalLine + right
	}
	return strings.Join(frameLines, "\n")
}

func renderPanel(title string, width int, height int, body string) string {
	return renderPanelFocused(title, width, height, body, false)
}

func renderPanelFocused(title string, width int, height int, body string, focused bool) string {
	width = max(2, width)
	height = max(2, height)
	contentWidth := panelContentWidth(width)
	contentHeight := max(0, height-2)
	lines := panelBodyLines(body)
	border := borderStyle
	titleSty := titleStyle
	if focused {
		border = focusBorderStyle
		titleSty = focusTitleStyle
	}

	var b strings.Builder
	b.WriteString(panelTop(title, width, border, titleSty))
	for i := 0; i < contentHeight; i++ {
		b.WriteByte('\n')
		line := ""
		if i < len(lines) {
			line = lines[i]
		}
		line = fitANSI(line, contentWidth)
		anchor := panelRowAnchorStyle(i)
		b.WriteString(border.Render("│"))
		b.WriteString(anchor.Render(" "))
		b.WriteString(line)
		b.WriteString(anchor.Render(strings.Repeat(" ", max(0, contentWidth-lipgloss.Width(line)))))
		b.WriteString(anchor.Render(" "))
		b.WriteString(border.Render("│"))
	}
	b.WriteByte('\n')
	b.WriteString(border.Render("└" + strings.Repeat("─", max(0, width-2)) + "┘"))
	return b.String()
}

func panelRowAnchorStyle(row int) lipgloss.Style {
	return panelRowAnchorStyles[row%len(panelRowAnchorStyles)]
}

func panelTop(title string, width int, border lipgloss.Style, titleStyle lipgloss.Style) string {
	innerWidth := max(0, width-2)
	title = fit(title, innerWidth)
	if title == "" {
		return border.Render("┌" + strings.Repeat("─", innerWidth) + "┐")
	}
	fill := max(0, innerWidth-runeLen(title))
	return border.Render("┌") + titleStyle.Render(title) + border.Render(strings.Repeat("─", fill)+"┐")
}

func panelBodyLines(body string) []string {
	body = strings.TrimRight(body, "\n")
	if body == "" {
		return nil
	}
	return strings.Split(body, "\n")
}

func panelContentWidth(width int) int {
	return max(0, width-4)
}

func verticalSpacer(height int) string {
	return horizontalSpacer(1, height)
}

func horizontalSpacer(width int, height int) string {
	if height <= 0 {
		return ""
	}
	width = max(0, width)
	lines := make([]string, height)
	for i := range lines {
		lines[i] = valueStyle.Render(strings.Repeat(" ", width))
	}
	return strings.Join(lines, "\n")
}

func statusToken(status string) string {
	switch normalizeStatus(status) {
	case "ok":
		return "OK  "
	case "running":
		return "RUN "
	case "failed":
		return "FAIL"
	case "skipped":
		return "SKIP"
	default:
		return "WAIT"
	}
}

func normalizeStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "ok", "pass", "passed":
		return "ok"
	case "running", "run":
		return "running"
	case "failed", "fail", "failure":
		return "failed"
	case "skipped", "skip":
		return "skipped"
	default:
		return "pending"
	}
}

func checkStatusStyle(status string) lipgloss.Style {
	switch normalizeStatus(status) {
	case "failed":
		return failedStatusStyle
	case "running":
		return runningStatusStyle
	case "ok":
		return okStatusStyle
	case "skipped":
		return skippedStatusStyle
	default:
		return waitStatusStyle
	}
}

func staleStatusStyle(status string) lipgloss.Style {
	switch normalizeStatus(status) {
	case "failed":
		return staleFailedStatusStyle
	case "ok":
		return staleOKStatusStyle
	case "skipped":
		return staleSkippedStatusStyle
	default:
		return dimStyle
	}
}

func (m model) currentTargetName() string {
	if target, ok := m.currentTarget(); ok {
		return m.targetLabel(target)
	}
	return "-"
}

func (m model) currentTarget() (targetState, bool) {
	for _, target := range m.targets {
		if target.currentStep != "" {
			return target, true
		}
	}
	for _, target := range m.targets {
		if target.status == "running" {
			return target, true
		}
	}
	return targetState{}, false
}

func currentStepState(target targetState) (stepState, bool) {
	for _, step := range target.steps {
		if step.name == target.currentStep {
			return step, true
		}
	}
	return stepState{}, false
}

func (m model) targetFailedCheckCount(agent watch.AgentSnapshot, target string) int {
	count := 0
	for _, item := range m.failedChecks {
		if sameAgent(item.agent, agent) && item.finding.Target == target {
			count++
		}
	}
	return count
}

func (m model) passingCheckSummaries() []passingCheckSummary {
	rows := make([]passingCheckSummary, 0, len(m.passingChecks))
	index := make(map[string]int, len(m.passingChecks))
	for _, item := range m.passingChecks {
		key := passingCheckSummaryKey(item.target, item.step)
		if pos, ok := index[key]; ok {
			rows[pos].last = item.when
			rows[pos].count++
			rows[pos].target = item.target
			rows[pos].step = item.step
			rows[pos].duration = item.duration
			if item.duration > 0 {
				rows[pos].durationTotal += item.duration
				rows[pos].durationCount++
				rows[pos].maxDuration = maxInt64(rows[pos].maxDuration, item.duration)
			}
			continue
		}
		index[key] = len(rows)
		row := passingCheckSummary{
			last:     item.when,
			count:    1,
			target:   item.target,
			step:     item.step,
			duration: item.duration,
		}
		if item.duration > 0 {
			row.durationTotal = item.duration
			row.durationCount = 1
			row.maxDuration = item.duration
		}
		rows = append(rows, row)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		return rows[i].last.After(rows[j].last)
	})
	return rows
}

func (m model) filteredPassingCheckSummaries() []passingCheckSummary {
	rows := m.passingCheckSummaries()
	query := normalizedSearchQuery(m.searchQuery)
	if query == "" {
		return rows
	}
	filtered := rows[:0]
	for _, row := range rows {
		if passingCheckSummaryMatches(row, query) {
			filtered = append(filtered, row)
		}
	}
	return filtered
}

func (m model) failedCheckSummaries() []failedCheckSummary {
	rows := make([]failedCheckSummary, 0, len(m.failedChecks))
	index := make(map[string]int, len(m.failedChecks))
	for _, item := range m.failedChecks {
		finding := item.finding
		key := failedCheckSummaryKey(item.target, finding)
		if pos, ok := index[key]; ok {
			rows[pos].last = item.when
			rows[pos].count++
			rows[pos].target = item.target
			rows[pos].finding = finding
			continue
		}
		index[key] = len(rows)
		rows = append(rows, failedCheckSummary{last: item.when, count: 1, target: item.target, finding: finding})
	}
	m.decorateFailedCheckSummaries(rows)
	sort.SliceStable(rows, func(i, j int) bool {
		return rows[i].last.After(rows[j].last)
	})
	return rows
}

func (m model) decorateFailedCheckSummaries(rows []failedCheckSummary) {
	if len(rows) == 0 {
		return
	}
	attemptsByGroup := m.failedCheckAttemptsByGroup()
	for i := range rows {
		key := failedCheckSummaryIdentity(rows[i])
		attempts := attemptsByGroup[failedCheckAttemptGroupKey(rows[i].target, rows[i].finding.Target, rows[i].finding.Check)]
		failedAttempts := 0
		for _, attempt := range attempts {
			if attempt.failedKeys[key] {
				failedAttempts++
			}
		}
		if len(attempts) > 0 {
			rows[i].failPercent = failedAttempts * 100 / len(attempts)
		}
		rows[i].failStreak = failedCheckStreak(attempts, key)
	}
}

func (m model) failedCheckAttemptsByGroup() map[string][]failedCheckAttempt {
	type attemptIndex struct {
		group string
		index int
	}
	groups := map[string][]failedCheckAttempt{}
	indexes := map[string]attemptIndex{}
	upsert := func(group string, attemptKey string, at time.Time, failed bool, failedKey string) {
		if group == "" || attemptKey == "" {
			return
		}
		indexKey := group + "\x00" + attemptKey
		index, ok := indexes[indexKey]
		if !ok {
			index = attemptIndex{group: group, index: len(groups[group])}
			indexes[indexKey] = index
			groups[group] = append(groups[group], failedCheckAttempt{when: at})
		}
		attempt := groups[index.group][index.index]
		if at.After(attempt.when) || attempt.when.IsZero() {
			attempt.when = at
		}
		if failed {
			if failedKey != "" {
				if attempt.failedKeys == nil {
					attempt.failedKeys = map[string]bool{}
				}
				attempt.failedKeys[failedKey] = true
			}
		}
		groups[index.group][index.index] = attempt
	}
	for _, item := range m.passingChecks {
		check := firstNonEmpty(item.step.Name, item.step.Type)
		group := failedCheckAttemptGroupKey(item.target, "", check)
		upsert(group, checkAttemptKey(item.agent, item.round, item.when), item.when, false, "")
	}
	for _, item := range m.failedChecks {
		group := failedCheckAttemptGroupKey(item.target, item.finding.Target, item.finding.Check)
		upsert(group, checkAttemptKey(item.agent, item.round, item.when), item.when, true, failedCheckSummaryKey(item.target, item.finding))
	}
	for group := range groups {
		sort.SliceStable(groups[group], func(i, j int) bool {
			return groups[group][i].when.After(groups[group][j].when)
		})
	}
	return groups
}

func failedCheckAttemptGroupKey(target watch.TargetSnapshot, findingTarget string, check string) string {
	targetKey := firstNonEmpty(checkStatusTargetKey(target), findingTarget)
	check = firstNonEmpty(check)
	if targetKey == "" || check == "" {
		return ""
	}
	return strings.Join([]string{targetKey, check}, "\x00")
}

func checkAttemptKey(agent watch.AgentSnapshot, round uint64, at time.Time) string {
	run := ""
	if round > 0 {
		run = fmt.Sprint(round)
	} else if !at.IsZero() {
		run = at.Format(time.RFC3339Nano)
	}
	if run == "" {
		return ""
	}
	return strings.Join([]string{roundAgentKey(agent), run}, "\x00")
}

func failedCheckStreak(attempts []failedCheckAttempt, failedKey string) int {
	streak := 0
	for _, attempt := range attempts {
		if attempt.failedKeys[failedKey] {
			streak++
			continue
		}
		break
	}
	return streak
}

func (m model) failureHotspots() []failureHotspotSummary {
	now := m.currentTime()
	start := now.Add(-summarySparklineWindow)
	type aggregate struct {
		summary failureHotspotSummary
		runs    map[string]failureHotspotRun
	}
	aggregates := map[string]*aggregate{}
	ensure := func(agent watch.AgentSnapshot, target watch.TargetSnapshot, findingTargets ...string) *aggregate {
		if target.Name == "" {
			target.Name = firstNonEmpty(target.SSID, target.BSSID, firstNonEmpty(findingTargets...), "target")
		}
		key := failureHotspotIdentity(agent, target, findingTargets...)
		item, ok := aggregates[key]
		if !ok {
			item = &aggregate{summary: failureHotspotSummary{agent: agent, target: target}, runs: map[string]failureHotspotRun{}}
			aggregates[key] = item
		}
		if agentKey(item.summary.agent) == "" {
			item.summary.agent = agent
		}
		item.summary.target = mergeTargetSnapshot(item.summary.target, target)
		return item
	}
	recordRun := func(item *aggregate, round uint64, at time.Time, failed bool) {
		if at.IsZero() {
			at = now
		}
		key := failureHotspotRunKey(round, at)
		run := item.runs[key]
		if at.After(run.last) || run.last.IsZero() {
			run.last = at
		}
		run.failed = run.failed || failed
		item.runs[key] = run
		if at.After(item.summary.last) || item.summary.last.IsZero() {
			item.summary.last = at
		}
	}
	for _, passingCheck := range m.passingChecks {
		if !withinWindow(passingCheck.when, start) {
			continue
		}
		recordRun(ensure(passingCheck.agent, passingCheck.target), passingCheck.round, passingCheck.when, false)
	}
	for _, failedCheck := range m.failedChecks {
		if !withinWindow(failedCheck.when, start) {
			continue
		}
		item := ensure(failedCheck.agent, failedCheck.target, failedCheck.finding.Target)
		previousLast := item.summary.last
		recordRun(item, failedCheck.round, failedCheck.when, true)
		item.summary.failCount++
		if failedCheck.when.After(previousLast) || item.summary.latestCause == "" {
			item.summary.latestCause = failureHotspotCause(failedCheck.finding)
			item.summary.latestFinding = failedCheck.finding
		}
	}
	rows := make([]failureHotspotSummary, 0, len(aggregates))
	for _, item := range aggregates {
		if item.summary.failCount == 0 {
			continue
		}
		item.summary.runCount = len(item.runs)
		item.summary.failRunCount = 0
		runs := make([]failureHotspotRun, 0, len(item.runs))
		for _, run := range item.runs {
			runs = append(runs, run)
			if run.failed {
				item.summary.failRunCount++
			}
		}
		sort.SliceStable(runs, func(i, j int) bool {
			return runs[i].last.After(runs[j].last)
		})
		for _, run := range runs {
			if !run.failed {
				break
			}
			item.summary.failStreak++
		}
		rows = append(rows, item.summary)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].failStreak != rows[j].failStreak {
			return rows[i].failStreak > rows[j].failStreak
		}
		leftRate := 0
		if rows[i].runCount > 0 {
			leftRate = rows[i].failRunCount * 1000 / rows[i].runCount
		}
		rightRate := 0
		if rows[j].runCount > 0 {
			rightRate = rows[j].failRunCount * 1000 / rows[j].runCount
		}
		if leftRate != rightRate {
			return leftRate > rightRate
		}
		if rows[i].failCount != rows[j].failCount {
			return rows[i].failCount > rows[j].failCount
		}
		return rows[i].last.After(rows[j].last)
	})
	return rows
}

func (m model) filteredFailureHotspots() []failureHotspotSummary {
	return m.filteredFailureHotspotRows()
}

type failureHotspotSummaryRow struct {
	index int
	item  failureHotspotSummary
}

func (m model) filteredFailureHotspotRows() []failureHotspotSummary {
	query := normalizedSearchQuery(m.searchQuery)
	rows := m.failureHotspots()
	if query == "" {
		return rows
	}
	filtered := rows[:0]
	for _, row := range rows {
		if failureHotspotSummaryMatches(row, query) {
			filtered = append(filtered, row)
		}
	}
	return filtered
}

func (m model) filteredFailureHotspotRowsForAgent(agent watch.AgentSnapshot) []failureHotspotSummaryRow {
	rows := m.filteredFailureHotspotRows()
	filtered := make([]failureHotspotSummaryRow, 0, len(rows))
	for index, row := range rows {
		if sameAgent(row.agent, agent) {
			filtered = append(filtered, failureHotspotSummaryRow{index: index, item: row})
		}
	}
	return filtered
}

func (m model) focusedFailureHotspotRows() []failureHotspotSummaryRow {
	if m.failureHotspotPanelsSplit() {
		key := m.currentHotspotAgentKey()
		for _, agent := range m.failureHotspotAgents() {
			if roundAgentKey(agent) == key {
				return m.filteredFailureHotspotRowsForAgent(agent)
			}
		}
		return nil
	}
	return indexedFailureHotspotRows(m.filteredFailureHotspotRows())
}

func indexedFailureHotspotRows(rows []failureHotspotSummary) []failureHotspotSummaryRow {
	indexed := make([]failureHotspotSummaryRow, 0, len(rows))
	for index, row := range rows {
		indexed = append(indexed, failureHotspotSummaryRow{index: index, item: row})
	}
	return indexed
}

func withinWindow(at time.Time, start time.Time) bool {
	if at.IsZero() {
		return false
	}
	return !at.Before(start)
}

func failureHotspotRunKey(round uint64, at time.Time) string {
	if round > 0 {
		return fmt.Sprint(round)
	}
	return at.Format(time.RFC3339Nano)
}

func failureHotspotCause(finding watch.Finding) string {
	cause := firstNonEmpty(
		finding.Message,
		strings.TrimSpace(firstNonEmpty(finding.Check, "check")+" "+firstNonEmpty(finding.Metric, "status")+"="+firstNonEmpty(finding.Observed, "-")),
		finding.Check,
	)
	return sanitizeLogText(cause)
}

func (m model) filteredFailedCheckSummaries() []failedCheckSummary {
	rows := m.failedCheckSummaries()
	query := normalizedSearchQuery(m.searchQuery)
	if query == "" {
		return rows
	}
	filtered := rows[:0]
	for _, row := range rows {
		if failedCheckSummaryMatches(row, query) {
			filtered = append(filtered, row)
		}
	}
	return filtered
}

func (m model) failedCheckSummaryIndex(agent watch.AgentSnapshot, target watch.TargetSnapshot, finding watch.Finding) int {
	key := failedCheckSummaryKey(target, finding)
	for i, item := range m.failedCheckSummaries() {
		if failedCheckSummaryKey(item.target, item.finding) == key {
			return i
		}
	}
	return max(0, len(m.failedCheckSummaries())-1)
}

func normalizedSearchQuery(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func passingCheckSummaryMatches(row passingCheckSummary, query string) bool {
	fields := []string{
		row.target.Name,
		row.target.SSID,
		row.target.BSSID,
		row.target.Band,
		row.step.Name,
		row.step.Type,
		row.step.Operation,
		row.step.Status,
		durationLabel(row.duration),
		fmt.Sprint(row.count),
	}
	return fieldsContainQuery(fields, query)
}

func failedCheckSummaryMatches(row failedCheckSummary, query string) bool {
	finding := row.finding
	fields := []string{
		row.target.Name,
		row.target.SSID,
		row.target.BSSID,
		row.target.Band,
		finding.Target,
		finding.Check,
		finding.Metric,
		finding.Observed,
		finding.Expected,
		finding.Message,
		fmt.Sprint(row.count),
	}
	return fieldsContainQuery(fields, query)
}

func failureHotspotSummaryMatches(row failureHotspotSummary, query string) bool {
	finding := row.latestFinding
	fields := []string{
		agentLabel(row.agent),
		agentKey(row.agent),
		row.target.Name,
		row.target.SSID,
		row.target.BSSID,
		row.target.Band,
		row.latestCause,
		finding.Target,
		finding.Check,
		finding.Metric,
		finding.Observed,
		finding.Expected,
		finding.Message,
		fmt.Sprint(row.failCount),
		fmt.Sprint(row.failRunCount),
		fmt.Sprint(row.runCount),
		fmt.Sprint(row.failStreak),
	}
	return fieldsContainQuery(fields, query)
}

func fieldsContainQuery(fields []string, query string) bool {
	if query == "" {
		return true
	}
	for _, field := range fields {
		if strings.Contains(strings.ToLower(field), query) {
			return true
		}
	}
	return false
}

func recentEventHistogram(times []time.Time, width int, window time.Duration, now time.Time) occurrenceHistogram {
	if width <= 0 || window <= 0 {
		return occurrenceHistogram{}
	}
	if now.IsZero() {
		now = time.Now()
	}
	bucketCount := recentEventBucketCount(window, width)
	bucketWidth := recentEventBucketWidth(window, bucketCount)
	end := recentHistogramEnd(times, bucketWidth, now)
	start := end.Add(-bucketWidth * time.Duration(bucketCount))
	counts := make([]int, bucketCount)
	count := 0
	maxCount := 0
	for _, at := range times {
		if at.Before(start) || !at.Before(end) {
			continue
		}
		index := int(at.Sub(start) / bucketWidth)
		index = clamp(index, 0, bucketCount-1)
		counts[index]++
		count++
		if counts[index] > maxCount {
			maxCount = counts[index]
		}
	}
	return occurrenceHistogram{
		first:       start,
		last:        end,
		bucketWidth: bucketWidth,
		counts:      counts,
		max:         maxCount,
		count:       count,
	}
}

func recentEventBucketCount(window time.Duration, width int) int {
	if window <= 0 || width <= 0 {
		return 0
	}
	oneSecondBuckets := int((window + time.Second - 1) / time.Second)
	if oneSecondBuckets < 1 {
		oneSecondBuckets = 1
	}
	return clamp(width, 1, oneSecondBuckets)
}

func recentEventBucketWidth(window time.Duration, bucketCount int) time.Duration {
	if window <= 0 || bucketCount <= 0 {
		return time.Second
	}
	return (window + time.Duration(bucketCount) - 1) / time.Duration(bucketCount)
}

func recentHistogramEnd(times []time.Time, bucketWidth time.Duration, now time.Time) time.Time {
	end := now
	if end.IsZero() {
		end = time.Now()
	}
	for _, at := range times {
		if at.After(end) {
			end = at
		}
	}
	return nextHistogramBucketBoundary(end, bucketWidth)
}

func nextHistogramBucketBoundary(at time.Time, bucketWidth time.Duration) time.Time {
	if bucketWidth <= 0 {
		return at
	}
	width := int64(bucketWidth)
	unix := at.UnixNano()
	next := ((unix / width) + 1) * width
	return time.Unix(0, next).In(at.Location())
}

func renderSparkline(counts []int, maxCount int, width int, height int, style lipgloss.Style) []string {
	if width <= 0 || height <= 0 {
		return nil
	}
	counts = resampleSparklineCounts(counts, width)
	maxCount = max(maxCount, maxInt(counts))
	eventsPerRow := sparklineEventsPerRow(maxCount, height)
	if len(counts) == 0 || maxCount <= 0 {
		lines := make([]string, height)
		for i := range lines {
			lines[i] = dimStyle.Render(strings.Repeat(" ", width))
		}
		return lines
	}
	columnHeights := make([]int, width)
	for i := 0; i < width; i++ {
		count := 0
		if i < len(counts) {
			count = counts[i]
		}
		if count > 0 {
			columnHeights[i] = clamp((count+eventsPerRow-1)/eventsPerRow, 1, height)
		}
	}
	lines := make([]string, 0, height)
	for row := height; row >= 1; row-- {
		var b strings.Builder
		for _, columnHeight := range columnHeights {
			if columnHeight >= row {
				b.WriteString(style.Render("█"))
			} else {
				b.WriteString(dimStyle.Render(" "))
			}
		}
		lines = append(lines, b.String())
	}
	return lines
}

func sparklineEventsPerRow(maxCount int, height int) int {
	if maxCount <= 0 || height <= 0 {
		return 1
	}
	needed := (maxCount + height - 1) / height
	return niceSparklineUnit(needed)
}

func niceSparklineUnit(needed int) int {
	if needed <= 1 {
		return 1
	}
	magnitude := 1
	steps := []int{1, 2, 3, 5}
	for {
		for _, step := range steps {
			candidate := step * magnitude
			if candidate >= needed {
				return candidate
			}
		}
		magnitude *= 10
	}
}

func resampleSparklineCounts(counts []int, width int) []int {
	if width <= 0 {
		return nil
	}
	out := make([]int, width)
	if len(counts) == 0 {
		return out
	}
	if len(counts) == width {
		copy(out, counts)
		return out
	}
	for column := 0; column < width; column++ {
		start := column * len(counts) / width
		end := (column + 1) * len(counts) / width
		if end <= start {
			end = start + 1
		}
		start = clamp(start, 0, len(counts)-1)
		end = clamp(end, start+1, len(counts))
		for _, count := range counts[start:end] {
			if count > out[column] {
				out[column] = count
			}
		}
	}
	return out
}

func maxInt(values []int) int {
	maxValue := 0
	for _, value := range values {
		if value > maxValue {
			maxValue = value
		}
	}
	return maxValue
}

func summarySparklineAxis(width int, window time.Duration) string {
	if width <= 0 {
		return ""
	}
	left := formatBucketDuration(window) + " ago"
	right := "now"
	if width < len(left)+len(right)+1 {
		return dimStyle.Render(strings.Repeat("-", width))
	}
	mid := strings.Repeat("-", max(1, width-len(left)-len(right)))
	return dimStyle.Render(left + mid + right)
}

func (m model) passingCheckOccurrences(agent watch.AgentSnapshot, target watch.TargetSnapshot, step watch.StepSnapshot) []time.Time {
	key := passingCheckKey(agent, target, step)
	if agentKey(agent) == "" {
		key = passingCheckSummaryKey(target, step)
	}
	if key == "" {
		return nil
	}
	times := make([]time.Time, 0, len(m.passingChecks))
	for _, item := range m.passingChecks {
		itemKey := passingCheckKey(item.agent, item.target, item.step)
		if agentKey(agent) == "" {
			itemKey = passingCheckSummaryKey(item.target, item.step)
		}
		if itemKey == key {
			times = append(times, item.when)
		}
	}
	return times
}

func (m model) failedCheckOccurrences(agent watch.AgentSnapshot, target watch.TargetSnapshot, finding watch.Finding) []time.Time {
	key := failedCheckStateKey(agent, target, finding)
	if agentKey(agent) == "" {
		key = failedCheckSummaryKey(target, finding)
	}
	times := make([]time.Time, 0, len(m.failedChecks))
	for _, item := range m.failedChecks {
		itemKey := failedCheckStateKey(item.agent, item.target, item.finding)
		if agentKey(agent) == "" {
			itemKey = failedCheckSummaryKey(item.target, item.finding)
		}
		if itemKey == key {
			times = append(times, item.when)
		}
	}
	return times
}

func (m model) failureHotspotOccurrences(item failureHotspotSummary) []time.Time {
	key := failureHotspotSummaryIdentity(item)
	if key == "" {
		return nil
	}
	times := make([]time.Time, 0, len(m.failedChecks))
	for _, failedCheck := range m.failedChecks {
		targetKey := failureHotspotIdentity(failedCheck.agent, failedCheck.target, failedCheck.finding.Target)
		if targetKey == key {
			times = append(times, failedCheck.when)
		}
	}
	sort.SliceStable(times, func(i, j int) bool {
		return times[i].Before(times[j])
	})
	return times
}

func formatBucketDuration(duration time.Duration) string {
	if duration <= 0 {
		return "instant"
	}
	if duration < time.Second {
		return duration.String()
	}
	if duration < time.Minute {
		return fmt.Sprintf("%ds", int(duration.Round(time.Second)/time.Second))
	}
	if duration < time.Hour {
		return fmt.Sprintf("%dm", int(duration.Round(time.Minute)/time.Minute))
	}
	return fmt.Sprintf("%dh", int(duration.Round(time.Hour)/time.Hour))
}

func occurrenceGraphHeight(contentHeight int) int {
	if contentHeight <= 0 {
		return 0
	}
	height := max(4, contentHeight*50/100)
	return min(height, max(1, contentHeight-3))
}

func padVisible(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(value) > width {
		value = ansi.Truncate(value, width, "~")
	}
	return value + strings.Repeat(" ", max(0, width-lipgloss.Width(value)))
}

func padLeftVisible(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(value) > width {
		value = ansi.Truncate(value, width, "~")
	}
	return strings.Repeat(" ", max(0, width-lipgloss.Width(value))) + value
}

func failedCheckKey(finding watch.Finding) string {
	return strings.Join([]string{finding.Target, finding.Check, finding.Metric, finding.Expected, finding.Message}, "\x00")
}

func failedCheckSummaryKey(target watch.TargetSnapshot, finding watch.Finding) string {
	return failedCheckStateKey(watch.AgentSnapshot{}, target, finding)
}

func failedCheckSummaryIdentity(item failedCheckSummary) string {
	return failedCheckSummaryKey(item.target, item.finding)
}

func failedCheckSummaryIndexByIdentity(rows []failedCheckSummary, key string) int {
	for i, row := range rows {
		if failedCheckSummaryIdentity(row) == key {
			return i
		}
	}
	return -1
}

func failedCheckStateKey(agent watch.AgentSnapshot, target watch.TargetSnapshot, finding watch.Finding) string {
	targetName := firstNonEmpty(target.Name, target.SSID, target.BSSID, finding.Target)
	return strings.Join([]string{agentKey(agent), targetName, target.SSID, target.BSSID, failedCheckKey(finding)}, "\x00")
}

func failureHotspotSummaryIdentity(item failureHotspotSummary) string {
	return failureHotspotIdentity(item.agent, item.target, item.latestFinding.Target)
}

func failureHotspotSummaryIndexByIdentity(rows []failureHotspotSummary, key string) int {
	for i, row := range rows {
		if failureHotspotSummaryIdentity(row) == key {
			return i
		}
	}
	return -1
}

func failureHotspotIdentity(agent watch.AgentSnapshot, target watch.TargetSnapshot, findingTargets ...string) string {
	targetKey := firstNonEmpty(checkStatusTargetKey(target), firstNonEmpty(findingTargets...), "target")
	return strings.Join([]string{roundAgentKey(agent), targetKey}, "\x00")
}

func targetStateKey(agent watch.AgentSnapshot, target watch.TargetSnapshot) string {
	targetName := firstNonEmpty(target.Name, target.SSID, target.BSSID)
	return strings.Join([]string{agentKey(agent), targetName, target.SSID, target.BSSID, target.Band}, "\x00")
}

func agentKey(agent watch.AgentSnapshot) string {
	return firstNonEmpty(agent.ID, agent.ADBSerial, agent.SessionID, agent.Name)
}

func agentLabel(agent watch.AgentSnapshot) string {
	return firstNonEmpty(agent.DisplayName(), "-")
}

func sameAgent(a watch.AgentSnapshot, b watch.AgentSnapshot) bool {
	return agentKey(a) == agentKey(b)
}

func (m model) targetLabel(target targetState) string {
	return m.runQueueTargetLabel(target, m.multiAgent)
}

func (m model) runQueueTargetLabel(target targetState, includeAgentLabel bool) string {
	label := target.target.Name
	if !includeAgentLabel {
		return label
	}
	return agentLabel(target.agent) + " " + label
}

func (m model) eventTargetLabel(event watch.Event) string {
	label := event.Target.Name
	if !m.multiAgent {
		return label
	}
	return agentLabel(event.Agent) + " " + label
}

func passingCheckEvent(event watch.Event) bool {
	status := firstNonEmpty(event.Step.Status, event.Status)
	if status != "ok" || event.Step.Skipped {
		return false
	}
	if event.Step.Type == "cleanup" {
		return false
	}
	return firstNonEmpty(event.Step.Name, event.Step.Type) != ""
}

func requiredStepFailedCheck(event watch.Event) (watch.Finding, bool) {
	status := firstNonEmpty(event.Step.Status, event.Status)
	if status != "failed" {
		return watch.Finding{}, false
	}
	stepType := firstNonEmpty(event.Step.Type, event.Step.Name)
	if stepType != "connect" && stepType != "wait_connected" {
		return watch.Finding{}, false
	}
	return watch.Finding{
		Target:   firstNonEmpty(event.Target.Name, event.Target.SSID, event.Target.BSSID),
		Check:    firstNonEmpty(event.Step.Name, stepType),
		Metric:   "status",
		Observed: status,
		Expected: "== ok",
		Message:  firstNonEmpty(event.Step.Message, event.Step.Error, event.Message, "step failed"),
	}, true
}

func passingCheckKey(agent watch.AgentSnapshot, target watch.TargetSnapshot, step watch.StepSnapshot) string {
	targetName := firstNonEmpty(target.Name, target.SSID, target.BSSID)
	stepName := firstNonEmpty(step.Name, step.Type)
	if targetName == "" || stepName == "" {
		return ""
	}
	return strings.Join([]string{agentKey(agent), targetName, target.SSID, target.BSSID, stepName, step.Type}, "\x00")
}

func passingCheckSummaryKey(target watch.TargetSnapshot, step watch.StepSnapshot) string {
	return passingCheckKey(watch.AgentSnapshot{}, target, step)
}

func passingCheckSummaryIdentity(item passingCheckSummary) string {
	return passingCheckSummaryKey(item.target, item.step)
}

func passingCheckSummaryIndexByIdentity(rows []passingCheckSummary, key string) int {
	for i, row := range rows {
		if passingCheckSummaryIdentity(row) == key {
			return i
		}
	}
	return -1
}

func eventTime(event watch.Event) time.Time {
	if !event.Time.IsZero() {
		return event.Time
	}
	return time.Now()
}

func durationLabel(duration int64) string {
	if duration <= 0 {
		return "-"
	}
	if duration < 1000 {
		return fmt.Sprintf("%dms", duration)
	}
	return fmt.Sprintf("%.1fs", float64(duration)/1000)
}

func (item passingCheckSummary) avgDuration() int64 {
	if item.durationCount <= 0 {
		return 0
	}
	return (item.durationTotal + int64(item.durationCount/2)) / int64(item.durationCount)
}

func fit(value string, width int) string {
	runes := []rune(value)
	if width <= 0 || len(runes) <= width {
		return value
	}
	if width == 1 {
		return "~"
	}
	return string(runes[:width-1]) + "~"
}

func fitText(value string, width int) string {
	return fitANSI(sanitizeLogText(value), width)
}

func fitANSI(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(value) <= width {
		return value
	}
	return ansi.Truncate(value, width, "~")
}

func padToWidth(value string, width int) string {
	if width <= 0 {
		return ""
	}
	value = fit(value, width)
	return value + strings.Repeat(" ", max(0, width-runeLen(value)))
}

func runeLen(value string) int {
	return len([]rune(value))
}

func (m model) targetCount(status string) int {
	count := 0
	for _, target := range m.targets {
		if target.status == status {
			count++
		}
	}
	return count
}

func runQueueRowStyle(status string) lipgloss.Style {
	switch normalizeStatus(status) {
	case "failed":
		return failedStatusStyle
	case "running":
		return runningStatusStyle
	case "ok":
		return okStatusStyle
	case "skipped":
		return skippedStatusStyle
	default:
		return waitStatusStyle
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func clamp(value, low, high int) int {
	if high < low {
		return low
	}
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

func correctedOffset(selected int, visibleRows int, totalRows int) int {
	if totalRows <= 0 {
		return 0
	}
	visibleRows = max(1, visibleRows)
	selected = clamp(selected, 0, totalRows-1)
	if totalRows <= visibleRows {
		return 0
	}
	start := selected - visibleRows/2
	return clamp(start, 0, totalRows-visibleRows)
}

func stableOffset(selected int, currentOffset int, visibleRows int, totalRows int) int {
	if totalRows <= 0 {
		return 0
	}
	visibleRows = max(1, visibleRows)
	selected = clamp(selected, 0, totalRows-1)
	currentOffset = clamp(currentOffset, 0, max(0, totalRows-visibleRows))
	if totalRows <= visibleRows {
		return 0
	}
	if selected < currentOffset {
		return selected
	}
	if selected >= currentOffset+visibleRows {
		return selected - visibleRows + 1
	}
	return currentOffset
}

var (
	appStyle                = lipgloss.NewStyle().Foreground(lipgloss.Color("#C87528")).Background(lipgloss.Color("#1A0A02"))
	borderStyle             = lipgloss.NewStyle().Foreground(lipgloss.Color("#A95A18")).Background(lipgloss.Color("#1A0A02"))
	focusBorderStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("#F0903A")).Background(lipgloss.Color("#1A0A02"))
	titleStyle              = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#C87528")).Background(lipgloss.Color("#1A0A02"))
	focusTitleStyle         = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFAA44")).Background(lipgloss.Color("#1A0A02"))
	keyStyle                = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#58F2A5")).Background(lipgloss.Color("#1A0A02"))
	valueStyle              = lipgloss.NewStyle().Foreground(lipgloss.Color("#EE8822")).Background(lipgloss.Color("#1A0A02"))
	logStyle                = lipgloss.NewStyle().Foreground(lipgloss.Color("#B46522")).Background(lipgloss.Color("#1A0A02"))
	groupStyle              = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#58F2A5")).Background(lipgloss.Color("#1A0A02"))
	tableHeaderStyle        = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#58F2A5")).Background(lipgloss.Color("#3A1203"))
	summaryKeyStyle         = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFAA44")).Background(lipgloss.Color("#1A0A02"))
	summaryValueStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("#EE8822")).Background(lipgloss.Color("#1A0A02"))
	summaryFreshRowStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFAA44")).Background(lipgloss.Color("#1A0A02"))
	summaryStaleRowStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#B46522")).Background(lipgloss.Color("#1A0A02"))
	summaryTableHeaderStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFAA44")).Background(lipgloss.Color("#3A1203"))
	summaryGraphStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF9F1C")).Background(lipgloss.Color("#1A0A02"))
	selectedStyle           = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFAA44")).Background(lipgloss.Color("#5C1802"))
	okStatusStyle           = lipgloss.NewStyle().Foreground(lipgloss.Color("#86C779")).Background(lipgloss.Color("#1A0A02"))
	failedStatusStyle       = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FF6B4A")).Background(lipgloss.Color("#1A0A02"))
	runningStatusStyle      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFD166")).Background(lipgloss.Color("#1A0A02"))
	waitStatusStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("#9B6A43")).Background(lipgloss.Color("#1A0A02"))
	skippedStatusStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("#D28A3C")).Background(lipgloss.Color("#1A0A02"))
	staleOKStatusStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("#3C9D62")).Background(lipgloss.Color("#1A0A02"))
	staleFailedStatusStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#B45A38")).Background(lipgloss.Color("#1A0A02"))
	staleSkippedStatusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#9C6436")).Background(lipgloss.Color("#1A0A02"))
	warnStyle               = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFD166")).Background(lipgloss.Color("#1A0A02"))
	okGraphStyle            = lipgloss.NewStyle().Foreground(lipgloss.Color("#B7D866")).Background(lipgloss.Color("#1A0A02"))
	timelineBaseStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("#4F9F67")).Background(lipgloss.Color("#1A0A02"))
	failGraphStyle          = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FF6B4A")).Background(lipgloss.Color("#1A0A02"))
	connectFailureStyle     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FF2D55")).Background(lipgloss.Color("#1A0A02"))
	dimStyle                = lipgloss.NewStyle().Foreground(lipgloss.Color("#6B4A37")).Background(lipgloss.Color("#1A0A02"))
	panelRowAnchorStyles    = []lipgloss.Style{
		lipgloss.NewStyle().Foreground(lipgloss.Color("#1A0A02")).Background(lipgloss.Color("#1A0A02")),
		lipgloss.NewStyle().Foreground(lipgloss.Color("#1B0A02")).Background(lipgloss.Color("#1A0A02")),
		lipgloss.NewStyle().Foreground(lipgloss.Color("#1C0A02")).Background(lipgloss.Color("#1A0A02")),
		lipgloss.NewStyle().Foreground(lipgloss.Color("#1D0A02")).Background(lipgloss.Color("#1A0A02")),
		lipgloss.NewStyle().Foreground(lipgloss.Color("#1E0A02")).Background(lipgloss.Color("#1A0A02")),
		lipgloss.NewStyle().Foreground(lipgloss.Color("#1F0A02")).Background(lipgloss.Color("#1A0A02")),
		lipgloss.NewStyle().Foreground(lipgloss.Color("#200A02")).Background(lipgloss.Color("#1A0A02")),
		lipgloss.NewStyle().Foreground(lipgloss.Color("#210A02")).Background(lipgloss.Color("#1A0A02")),
	}
)
