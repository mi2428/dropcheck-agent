package tui

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/mi2428/dropcheck-agent/controller/internal/tuirun"
	"github.com/mi2428/dropcheck-agent/controller/internal/tuistate"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// RunWithControls renders tui events and lets keyboard input control the
// tui runner through pause and skip controllers.
func RunWithControls(ctx context.Context, title string, targets []tuirun.Target, checks []tuirun.CheckInfo, agents []tuirun.AgentSnapshot, events <-chan tuirun.Event, controls *tuirun.Controls) error {
	m := newModelWithChecks(title, targets, checks, events, agents)
	m.controls = controls
	if controls != nil {
		m.paused = controls.Paused(tuirun.Scope{Kind: tuirun.ScopeRun})
	}
	return runModel(ctx, m)
}

func runModel(ctx context.Context, m model) error {
	_, err := tea.NewProgram(m, tea.WithContext(ctx)).Run()
	if err != nil && ctx.Err() != nil {
		return nil
	}
	if errors.Is(err, tea.ErrProgramKilled) {
		return nil
	}
	return err
}

func newModelWithChecks(title string, targets []tuirun.Target, checks []tuirun.CheckInfo, events <-chan tuirun.Event, agentSets ...[]tuirun.AgentSnapshot) model {
	if strings.TrimSpace(title) == "" {
		title = "dropcheck tui"
	}
	agents := []tuirun.AgentSnapshot(nil)
	if len(agentSets) > 0 {
		agents = agentSets[0]
	}
	return model{
		Title:       title,
		events:      events,
		keys:        keyMap{quit: key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl-c", "quit"))},
		State:       tuistate.New(targets, checks, agents, time.Now()),
		focus:       focusCheckStatus,
		detailPanel: focusFailedChecks,
	}
}

// Init starts the event reader and the wall-clock tick command required by the
// Bubble Tea program.
func (m model) Init() tea.Cmd {
	return tea.Batch(waitEvent(m.events), tickEverySecond())
}

func waitEvent(events <-chan tuirun.Event) tea.Cmd {
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

// Update applies keyboard input, terminal resize messages, tui events, and
// clock ticks to the TUI model.
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
		switch msg.Keystroke() {
		case "tab":
			m.focusPreviousPanel()
			if m.detailOpen {
				if panelSupportsDetail(m.focus) {
					m.detailPanel = m.focus
					m.lockDetailToCursor(m.focus)
				} else {
					m.detailOpen = false
				}
			}
		case "shift+tab":
			m.focusNextPanel()
			if m.detailOpen {
				if panelSupportsDetail(m.focus) {
					m.detailPanel = m.focus
					m.lockDetailToCursor(m.focus)
				} else {
					m.detailOpen = false
				}
			}
		case "ctrl+z":
			m.pause()
		case "ctrl+n":
			m.skipCurrent()
		case "/":
			if panelSupportsFilter(m.focus) {
				m.searchPanel = m.focus
				m.searchEditing = true
			}
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
		case "h", "left":
			if m.focus == focusCheckStatus {
				m.moveCheckStatusHorizontal(-1)
			}
		case "l", "right":
			if m.focus == focusCheckStatus {
				m.moveCheckStatusHorizontal(1)
			}
		case "m":
			if m.focus == focusFailureHotspots {
				m.toggleFailureHotspotMode()
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
			switch {
			case m.paused:
				m.resume()
			case m.detailOpen:
				m.detailOpen = false
			case !m.clearFocusedScrollPin() && m.hasSearchFilter():
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
		m.apply(tuirun.Event(msg))
		return m, waitEvent(m.events)
	case tickMsg:
		m.Now = time.Time(msg)
		return m, tickEverySecond()
	case closedMsg:
		m.closed = true
		m.pushLog("tui event stream closed")
		return m, tea.Quit
	}
	return m, nil
}

func panelSupportsDetail(panel focusPanel) bool {
	return panel == focusPassingChecks || panel == focusFailedChecks || panel == focusFailureHotspots
}

// View renders the current frame in the alternate screen buffer.
func (m model) View() tea.View {
	view := tea.NewView(m.render())
	view.AltScreen = true
	return view
}
