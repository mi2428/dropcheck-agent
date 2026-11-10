package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/mi2428/dropcheck-agent/controller/internal/safevalue"
	"github.com/mi2428/dropcheck-agent/controller/internal/tuirun"
)

type actionItem struct {
	title, description string
	scope              tuirun.Scope
	action             string
	disabled           bool
}

func (i actionItem) Title() string       { return i.title }
func (i actionItem) Description() string { return i.description }
func (i actionItem) FilterValue() string { return i.title + " " + i.description }

type actionMenu struct {
	list          list.Model
	scope         tuirun.Scope
	label, intent string
	confirmation  string
}

type controlResultMsg struct {
	scope  tuirun.Scope
	action string
	err    error
}

func (m model) scopeItems() []list.Item {
	if m.controls == nil || m.closed {
		return nil
	}
	var items []list.Item
	add := func(scope tuirun.Scope, title, description string) {
		if m.controls.HasScope(scope) && !m.controls.Canceled(scope) {
			items = append(items, actionItem{scope: scope, title: sanitizeLogText(title), description: sanitizeLogText(description)})
		}
	}
	add(tuirun.Scope{Kind: tuirun.ScopeRun}, "All agents", "Whole run; explicitly select this to control every agent")
	for _, agent := range m.Agents {
		if agent.ID != "" {
			add(tuirun.Scope{Kind: tuirun.ScopeAgent, AgentID: agent.ID}, "Agent: "+agentLabel(agent)+" ["+agent.ID+"]", agent.ID)
		}
	}
	for _, target := range m.Targets {
		if target.Agent.ID == "" || target.Target.ID == "" {
			continue
		}
		scope := tuirun.Scope{Kind: tuirun.ScopeTarget, AgentID: target.Agent.ID, TargetID: target.Target.ID}
		label := agentLabel(target.Agent) + " [" + target.Agent.ID + "] / " + firstNonEmpty(target.Target.Name, target.Target.SSID, target.Target.ID) + " [" + target.Target.ID + "]"
		add(scope, "Target: "+label, target.Target.ID)
		for _, step := range mergeRunQueueSteps(target.PlannedSteps, target.Steps) {
			if step.ID == "" {
				continue
			}
			scope.Kind, scope.CheckID = tuirun.ScopeCheck, step.ID
			add(scope, "Check: "+label+" / "+step.Name+" ["+step.ID+"]", target.Target.ID+" / "+step.ID)
		}
	}
	return items
}

func newActionList(items []list.Item, title string, width, height int) list.Model {
	delegate := list.NewDefaultDelegate()
	delegate.SetSpacing(0)
	picker := list.New(items, delegate, intMax(12, width), intMax(5, height))
	picker.Title = title
	picker.DisableQuitKeybindings()
	picker.SetShowHelp(false)
	picker.SetShowStatusBar(false)
	return picker
}

func (m *model) openScopeMenu(intent string) {
	if m.controlBusy {
		m.pushLog("A control request is pending; wait for its result")
		return
	}
	items := m.scopeItems()
	if len(items) == 0 {
		m.pushLog("No active control scope is available")
		return
	}
	if m.width < 40 || m.height < 14 {
		m.pushLog("Control dialogs need a terminal of at least 40x14")
		return
	}
	m.menu = &actionMenu{list: newActionList(items, "Select control scope", m.menuWidth()-4, m.menuHeight()-4), intent: intent}
	selected := m.selectionScope()
	for i, item := range items {
		if item.(actionItem).scope == selected {
			m.menu.list.Select(i)
			break
		}
	}
}

func (m model) selectionScope() tuirun.Scope {
	if m.detailOpen {
		return m.inspectorScope
	}
	scope := tuirun.Scope{Kind: tuirun.ScopeCheck}
	switch m.focus {
	case focusPassingChecks:
		rows := m.filteredPassingCheckSummaries()
		if len(rows) > 0 {
			item := rows[clamp(m.passingCheckCursor, 0, len(rows)-1)]
			scope.AgentID, scope.TargetID, scope.CheckID = item.Agent.ID, item.Target.ID, item.Step.ID
		}
	case focusFailedChecks:
		rows := m.filteredFailedCheckSummaries()
		if len(rows) > 0 {
			item := rows[clamp(m.failedCheckCursor, 0, len(rows)-1)]
			scope.AgentID, scope.TargetID, scope.CheckID = item.Agent.ID, item.Target.ID, item.Finding.CheckID
		}
	}
	if scope.AgentID == "" || scope.TargetID == "" {
		return tuirun.Scope{}
	}
	if scope.CheckID == "" {
		scope.Kind = tuirun.ScopeTarget
	}
	return scope
}

func (m *model) updateActionMenu(msg tea.Msg) tea.Cmd {
	if key, ok := msg.(tea.KeyPressMsg); ok {
		switch key.Keystroke() {
		case "esc":
			m.menu = nil
			return nil
		case "enter":
			if m.menu.confirmation != "" || m.menu.list.SettingFilter() || key.IsRepeat {
				break
			}
			item, ok := m.menu.list.SelectedItem().(actionItem)
			if !ok {
				return nil
			}
			if item.disabled {
				m.pushLog(item.description)
				return nil
			}
			if item.action == "" {
				m.menu.scope, m.menu.label = item.scope, item.title
				if m.menu.intent == "skip" {
					m.menu.confirmation = "skip"
					return nil
				}
				action := "pause"
				if m.controls.Paused(item.scope) {
					action = "resume"
				}
				items := []list.Item{
					actionItem{title: strings.ToUpper(action[:1]) + action[1:], action: action, description: "At attempt boundaries; existing policy deadlines continue"},
					actionItem{title: "Skip this round", action: "skip", description: "Remaining work in this scope; later dependent checks can also be skipped"},
					actionItem{title: "Cancel for this run", action: "cancel", description: "Remaining work in this scope; cannot be undone in this run"},
				}
				if parent, paused := pausedAncestor(m.controls, item.scope); paused {
					items[0] = actionItem{title: "Resume unavailable in this scope", disabled: true,
						description: "Paused by " + parent.Kind + "; select that parent scope to resume"}
				}
				m.menu.list = newActionList(items, m.menu.label, m.menuWidth()-4, m.menuHeight()-4)
				return nil
			}
			if item.action == "skip" || item.action == "cancel" {
				m.menu.confirmation = item.action
				return nil
			}
			scope, label := m.menu.scope, m.menu.label
			m.menu = nil
			return m.requestControl(item.action, scope, label)
		case "y":
			if m.menu.confirmation != "" && !key.IsRepeat {
				action, scope, label := m.menu.confirmation, m.menu.scope, m.menu.label
				m.menu = nil
				return m.requestControl(action, scope, label)
			}
		case "n":
			if m.menu.confirmation != "" {
				m.menu = nil
				return nil
			}
		}
	}
	if m.menu.confirmation != "" {
		return nil
	}
	var cmd tea.Cmd
	m.menu.list, cmd = m.menu.list.Update(msg)
	return cmd
}

func (m *model) requestControl(action string, scope tuirun.Scope, label string) tea.Cmd {
	if m.controlBusy {
		return nil
	}
	if m.controls == nil || m.closed || !m.controls.HasScope(scope) || m.controls.Canceled(scope) {
		m.pushLog("Control rejected: no active matching scope")
		return nil
	}
	if _, paused := pausedAncestor(m.controls, scope); action == "resume" && paused {
		m.pushLog("Resume rejected: select the paused parent scope")
		return nil
	}
	m.controlBusy, m.controlApplied = true, false
	m.controlAction, m.controlScope = action, scope
	m.controlLabel = label
	m.controlStatus = label + ": " + action + " requested"
	controls := m.controls
	return func() tea.Msg {
		var err error
		if !controls.HasScope(scope) || controls.Canceled(scope) {
			return controlResultMsg{scope: scope, action: action, err: fmt.Errorf("scope is no longer actionable")}
		}
		switch action {
		case "pause":
			err = controls.Pause(scope)
		case "resume":
			err = controls.Resume(scope)
		case "skip":
			err = controls.Skip(scope)
		case "cancel":
			err = controls.Cancel(scope)
		default:
			err = fmt.Errorf("unknown control action")
		}
		return controlResultMsg{scope: scope, action: action, err: err}
	}
}

func pausedAncestor(controls *tuirun.Controls, scope tuirun.Scope) (tuirun.Scope, bool) {
	parents := []tuirun.Scope{{Kind: tuirun.ScopeRun}}
	if scope.Kind == tuirun.ScopeTarget || scope.Kind == tuirun.ScopeCheck {
		parents = append(parents, tuirun.Scope{Kind: tuirun.ScopeAgent, AgentID: scope.AgentID})
	}
	if scope.Kind == tuirun.ScopeCheck {
		parents = append(parents, tuirun.Scope{Kind: tuirun.ScopeTarget, AgentID: scope.AgentID, TargetID: scope.TargetID})
	}
	for _, parent := range parents {
		if parent != scope && controls.Paused(parent) {
			return parent, true
		}
	}
	return tuirun.Scope{}, false
}

func (m *model) finishControl(result controlResultMsg) {
	m.controlBusy = false
	if result.err != nil {
		m.controlStatus = "Control failed: " + safevalue.Text(result.err.Error())
		m.pushLog(m.controlStatus)
	} else if !m.controlApplied {
		m.controlStatus = m.controlLabel + ": " + result.action + " accepted; waiting for execution feedback"
	}
}

func (m *model) controlFeedback(event tuirun.Event) {
	if !scopeContains(m.controlScope, event.Scope) {
		return
	}
	applied := event.Kind == tuirun.EventControlApplied &&
		(m.controlAction == "pause" && event.Status == "paused" || m.controlAction == "resume" && event.Status == "running")
	if applied || event.Kind == tuirun.EventStepFinished &&
		(m.controlAction == "skip" && event.Step.SkipReason == tuirun.SkipOperator || m.controlAction == "cancel" && event.Status == "canceled") {
		m.controlApplied = true
		m.controlStatus = fmt.Sprintf("%s: %s applied at %s/%s/%s", m.controlLabel, m.controlAction, event.Scope.AgentID, event.Scope.TargetID, event.Scope.CheckID)
	}
}

func scopeContains(parent, child tuirun.Scope) bool {
	switch parent.Kind {
	case tuirun.ScopeRun:
		return child.Kind != ""
	case tuirun.ScopeAgent:
		return parent.AgentID == child.AgentID
	case tuirun.ScopeTarget:
		return parent.AgentID == child.AgentID && parent.TargetID == child.TargetID
	case tuirun.ScopeCheck:
		return parent == child
	}
	return false
}

func (m model) menuWidth() int  { return intMax(16, intMin(88, m.width-4)) }
func (m model) menuHeight() int { return intMax(8, intMin(26, m.height-4)) }

func (m model) actionMenuView(frame string, width, height int) string {
	w, h := m.menuWidth(), m.menuHeight()
	body := m.menu.list.View() + "\nEnter=Select  /=Filter  Esc=Close"
	if m.menu.confirmation != "" {
		label := lipgloss.NewStyle().Width(intMax(1, w-4)).MaxHeight(2).Render(m.menu.label)
		body = label + "\n"
		if m.menu.scope.AgentID != "" {
			body += "Agent ID: " + m.menu.scope.AgentID + "\n"
		}
		if m.menu.scope.TargetID != "" {
			body += "Target ID: " + m.menu.scope.TargetID + "\n"
		}
		if m.menu.scope.CheckID != "" {
			body += "Check ID: " + m.menu.scope.CheckID + "\n"
		}
		if m.menu.confirmation == "cancel" {
			body += "Cancel this scope for this run. Cannot undo."
		} else {
			body += "Skip this scope for this round, including later dependent checks."
		}
		body += "\nY=Confirm  N/Esc=Keep running"
	}
	modal := renderPanel("TUI controls", w, h, lipgloss.NewStyle().Width(intMax(1, w-4)).Render(body))
	return lipgloss.NewCompositor(
		lipgloss.NewLayer(frame),
		lipgloss.NewLayer(modal).X(intMax(0, (width-w)/2)).Y(intMax(0, (height-h)/2)).Z(1),
	).Render()
}
