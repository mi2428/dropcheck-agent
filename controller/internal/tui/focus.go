package tui

import (
	"strings"

	"dropcheck/controller/internal/watch"

	tea "charm.land/bubbletea/v2"
)

func (m *model) moveFailedCheckCursor(delta int) {
	rows := m.filteredFailedCheckSummaries()
	if len(rows) == 0 {
		m.failedCheckCursor = 0
		return
	}
	old := m.failedCheckCursor
	m.failedCheckCursor = clamp(m.failedCheckCursor+delta, 0, len(rows)-1)
	if m.failedCheckCursor != old {
		m.pinFailedCheckCursor()
	}
}

func (m *model) movePassingCheckCursor(delta int) {
	rows := m.filteredPassingCheckSummaries()
	if len(rows) == 0 {
		m.passingCheckCursor = 0
		return
	}
	old := m.passingCheckCursor
	m.passingCheckCursor = clamp(m.passingCheckCursor+delta, 0, len(rows)-1)
	if m.passingCheckCursor != old {
		m.pinPassingCheckCursor()
	}
}

func (m *model) moveFailureHotspotCursor(delta int) {
	rows := m.focusedFailureHotspotRows()
	if len(rows) == 0 {
		return
	}
	old := m.failureHotspotCursor
	position := -1
	for i, row := range rows {
		if row.Index == m.failureHotspotCursor {
			position = i
			break
		}
	}
	if position < 0 {
		position = 0
	} else {
		position = clamp(position+delta, 0, len(rows)-1)
	}
	m.failureHotspotCursor = rows[position].Index
	if m.failureHotspotCursor != old {
		m.pinFailureHotspotCursor()
	}
}

func (m *model) moveFocusedCursor(delta int) {
	switch m.focus {
	case focusPassingChecks:
		m.movePassingCheckCursor(delta)
	case focusFailureHotspots:
		m.moveFailureHotspotCursor(delta)
	case focusRunQueue:
		m.moveRunQueueCursor(delta)
	case focusCheckStatus:
		return
	default:
		m.moveFailedCheckCursor(delta)
	}
}

func (m *model) focusNextPanel() {
	m.movePanelFocus(1)
}

func (m *model) focusPreviousPanel() {
	m.movePanelFocus(-1)
}

func (m *model) movePanelFocus(delta int) {
	slots := m.focusSlots()
	if len(slots) == 0 || delta == 0 {
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
			if slot.Panel == m.focus {
				index = i
				break
			}
		}
	}
	if index < 0 {
		index = 0
	}
	next := (index + delta) % len(slots)
	if next < 0 {
		next += len(slots)
	}
	m.setFocusSlot(slots[next])
}

func (m model) focusSlots() []focusSlot {
	slots := []focusSlot{
		{Panel: focusFailedChecks},
		{Panel: focusPassingChecks},
		{Panel: focusCheckStatus},
	}
	if !m.runQueuePanelsSplit() {
		slots = append(slots, focusSlot{Panel: focusRunQueue})
	} else {
		agents := m.runQueueAgents()
		for i := len(agents) - 1; i >= 0; i-- {
			slots = append(slots, focusSlot{Panel: focusRunQueue, RunQueueAgentKey: roundAgentKey(agents[i])})
		}
	}
	if m.failureHotspotsVisible() {
		if !m.failureHotspotPanelsSplit() {
			slots = append(slots, focusSlot{Panel: focusFailureHotspots})
		} else {
			agents := m.failureHotspotAgents()
			for i := len(agents) - 1; i >= 0; i-- {
				slots = append(slots, focusSlot{Panel: focusFailureHotspots, HotspotAgentKey: roundAgentKey(agents[i])})
			}
		}
	}
	return slots
}

func (m model) currentFocusSlot() focusSlot {
	switch m.focus {
	case focusFailureHotspots:
		key := ""
		if m.failureHotspotPanelsSplit() {
			key = m.currentHotspotAgentKey()
		}
		return focusSlot{Panel: focusFailureHotspots, HotspotAgentKey: key}
	case focusRunQueue:
		key := ""
		if m.runQueuePanelsSplit() {
			key = m.currentRunQueueAgentKey()
		}
		return focusSlot{Panel: focusRunQueue, RunQueueAgentKey: key}
	default:
		return focusSlot{Panel: m.focus}
	}
}

func (m *model) setFocusSlot(slot focusSlot) {
	previous := m.focus
	previousRunQueueKey := m.focusRunQueueAgentKey
	m.focus = slot.Panel
	if previous == focusCheckStatus && slot.Panel != focusCheckStatus {
		m.checkStatusPinned = false
		m.normalizeCheckStatusOffset()
	}
	if slot.Panel == focusRunQueue {
		m.focusRunQueueAgentKey = slot.RunQueueAgentKey
		m.normalizeFocusRunQueueAgent()
		if previous != focusRunQueue || previousRunQueueKey != m.focusRunQueueAgentKey {
			m.runQueueCursor = 0
			m.runQueueOffset = 0
		}
		m.runQueuePinned = false
		m.updateRunQueueCursor()
	}
	if slot.Panel == focusFailureHotspots {
		m.focusHotspotAgentKey = slot.HotspotAgentKey
		m.ensureFailureHotspotCursorInFocus()
	}
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

func (m model) currentRunQueueAgentKey() string {
	if !m.runQueuePanelsSplit() {
		return ""
	}
	agents := m.runQueueAgents()
	if len(agents) == 0 {
		return ""
	}
	for _, agent := range agents {
		key := roundAgentKey(agent)
		if key == m.focusRunQueueAgentKey {
			return key
		}
	}
	return roundAgentKey(agents[0])
}

func (m model) currentRunQueueAgent() watch.AgentSnapshot {
	if !m.runQueuePanelsSplit() {
		return watch.AgentSnapshot{}
	}
	key := m.currentRunQueueAgentKey()
	for _, agent := range m.runQueueAgents() {
		if roundAgentKey(agent) == key {
			return agent
		}
	}
	return watch.AgentSnapshot{}
}

func (m *model) normalizeFocusRunQueueAgent() {
	if !m.runQueuePanelsSplit() {
		m.focusRunQueueAgentKey = ""
		return
	}
	m.focusRunQueueAgentKey = m.currentRunQueueAgentKey()
}

func (m *model) ensureFailureHotspotCursorInFocus() {
	rows := m.focusedFailureHotspotRows()
	if len(rows) == 0 {
		return
	}
	for _, row := range rows {
		if row.Index == m.failureHotspotCursor {
			return
		}
	}
	m.failureHotspotCursor = rows[0].Index
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
	m.followPinnedSummaryCursors()
	m.passingCheckCursor = clamp(m.passingCheckCursor, 0, max(0, len(m.filteredPassingCheckSummaries())-1))
	m.failedCheckCursor = clamp(m.failedCheckCursor, 0, max(0, len(m.filteredFailedCheckSummaries())-1))
	m.failureHotspotCursor = clamp(m.failureHotspotCursor, 0, max(0, len(m.filteredFailureHotspots())-1))
	m.normalizeCheckStatusOffset()
	if m.focus == focusFailureHotspots && !m.failureHotspotsVisible() {
		m.focus = focusFailedChecks
	}
	if m.focus == focusFailureHotspots {
		m.normalizeFocusHotspotAgent()
		m.ensureFailureHotspotCursorInFocus()
	}
	if m.focus == focusRunQueue {
		m.normalizeFocusRunQueueAgent()
		m.updateRunQueueCursor()
	}
	if m.detailOpen {
		m.followLockedDetailItems()
	}
}

func (m *model) pinPassingCheckCursor() {
	rows := m.filteredPassingCheckSummaries()
	if len(rows) == 0 {
		m.passingCheckPinned = false
		m.passingCheckPinnedKey = ""
		return
	}
	index := clamp(m.passingCheckCursor, 0, len(rows)-1)
	m.passingCheckCursor = index
	m.passingCheckPinned = true
	m.passingCheckPinnedKey = passingCheckSummaryIdentity(rows[index])
}

func (m *model) pinFailedCheckCursor() {
	rows := m.filteredFailedCheckSummaries()
	if len(rows) == 0 {
		m.failedCheckPinned = false
		m.failedCheckPinnedKey = ""
		return
	}
	index := clamp(m.failedCheckCursor, 0, len(rows)-1)
	m.failedCheckCursor = index
	m.failedCheckPinned = true
	m.failedCheckPinnedKey = failedCheckSummaryIdentity(rows[index])
}

func (m *model) pinFailureHotspotCursor() {
	rows := m.filteredFailureHotspots()
	if len(rows) == 0 {
		m.failureHotspotPinned = false
		m.failureHotspotKey = ""
		return
	}
	index := clamp(m.failureHotspotCursor, 0, len(rows)-1)
	m.failureHotspotCursor = index
	m.failureHotspotPinned = true
	m.failureHotspotKey = failureHotspotSummaryIdentity(rows[index])
}

func (m *model) followPinnedSummaryCursors() {
	if m.passingCheckPinned && m.passingCheckPinnedKey != "" {
		if index := passingCheckSummaryIndexByIdentity(m.filteredPassingCheckSummaries(), m.passingCheckPinnedKey); index >= 0 {
			m.passingCheckCursor = index
		}
	}
	if m.failedCheckPinned && m.failedCheckPinnedKey != "" {
		if index := failedCheckSummaryIndexByIdentity(m.filteredFailedCheckSummaries(), m.failedCheckPinnedKey); index >= 0 {
			m.failedCheckCursor = index
		}
	}
	if m.failureHotspotPinned && m.failureHotspotKey != "" {
		rows := m.filteredFailureHotspots()
		if index := failureHotspotSummaryIndexByIdentity(rows, m.failureHotspotKey); index >= 0 {
			m.failureHotspotCursor = index
			if m.focus == focusFailureHotspots {
				m.focusHotspotAgentKey = roundAgentKey(rows[index].Agent)
			}
		}
	}
}

func (m model) focusedScrollPinned() bool {
	switch m.focus {
	case focusPassingChecks:
		return m.passingCheckPinned
	case focusFailedChecks:
		return m.failedCheckPinned
	case focusFailureHotspots:
		return m.failureHotspotPinned
	case focusRunQueue:
		return m.runQueuePinned
	case focusCheckStatus:
		return m.checkStatusPinned
	default:
		return false
	}
}

func (m *model) clearFocusedScrollPin() bool {
	switch m.focus {
	case focusPassingChecks:
		if !m.passingCheckPinned {
			return false
		}
		m.passingCheckPinned = false
		m.passingCheckPinnedKey = ""
		m.passingCheckCursor = 0
		m.normalizeCursors()
		return true
	case focusFailedChecks:
		if !m.failedCheckPinned {
			return false
		}
		m.failedCheckPinned = false
		m.failedCheckPinnedKey = ""
		m.failedCheckCursor = 0
		m.normalizeCursors()
		return true
	case focusFailureHotspots:
		if !m.failureHotspotPinned {
			return false
		}
		m.failureHotspotPinned = false
		m.failureHotspotKey = ""
		rows := m.focusedFailureHotspotRows()
		if len(rows) == 0 {
			m.failureHotspotCursor = 0
		} else {
			m.failureHotspotCursor = rows[0].Index
		}
		m.normalizeCursors()
		return true
	case focusRunQueue:
		if !m.runQueuePinned {
			return false
		}
		m.runQueuePinned = false
		m.updateRunQueueCursor()
		return true
	case focusCheckStatus:
		if !m.checkStatusPinned {
			return false
		}
		m.checkStatusPinned = false
		m.normalizeCheckStatusOffset()
		return true
	default:
		return false
	}
}

func (m *model) openDetailForPanel(panel focusPanel) {
	m.detailOpen = true
	m.detailPanel = panel
	m.lockDetailToCursor(panel)
}

// lockDetailToCursor stores the selected summary identity instead of only the
// row index. New events can reorder summary tables, so the modal follows the
// same logical item rather than whichever row happens to reuse the old index.
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
			if row.Index == m.failureHotspotCursor {
				index = i
				break
			}
		}
		m.failureHotspotCursor = rows[index].Index
		m.detailHotspotKey = failureHotspotSummaryIdentity(rows[index].Item)
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
				m.focusHotspotAgentKey = roundAgentKey(rows[index].Agent)
			}
		}
	}
}
