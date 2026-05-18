package tui

import (
	"context"
	"errors"
	"strings"
	"time"

	"dropcheck/controller/internal/watch"
	"dropcheck/controller/internal/watchstate"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

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
	return model{
		Title:       title,
		events:      events,
		keys:        keyMap{quit: key.NewBinding(key.WithKeys("ctrl+c"), key.WithHelp("ctrl-c", "quit"))},
		State:       watchstate.New(targets, checks, agents, time.Now()),
		focus:       focusFailedChecks,
		detailPanel: focusFailedChecks,
	}
}

// Init starts the event reader and the wall-clock tick command required by the
// Bubble Tea program.
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

// Update applies keyboard input, terminal resize messages, watch events, and
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
		case "h", "left":
			m.moveCheckStatusHorizontal(-1)
		case "l", "right":
			m.moveCheckStatusHorizontal(1)
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
		m.Now = time.Time(msg)
		return m, tickEverySecond()
	case closedMsg:
		m.closed = true
		m.pushLog("watch event stream closed")
		return m, tea.Quit
	}
	return m, nil
}

// View renders the current frame in the alternate screen buffer.
func (m model) View() tea.View {
	view := tea.NewView(m.render())
	view.AltScreen = true
	return view
}
