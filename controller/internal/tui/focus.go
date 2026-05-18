package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

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
			if slot.Panel == m.focus {
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
		{Panel: focusPassingChecks},
		{Panel: focusCheckStatus},
		{Panel: focusRunQueue},
		{Panel: focusFailedChecks},
	}
	if !m.failureHotspotsVisible() {
		return slots
	}
	if !m.failureHotspotPanelsSplit() {
		return append(slots, focusSlot{Panel: focusFailureHotspots})
	}
	for _, agent := range m.failureHotspotAgents() {
		slots = append(slots, focusSlot{Panel: focusFailureHotspots, HotspotAgentKey: roundAgentKey(agent)})
	}
	return slots
}

func (m model) currentFocusSlot() focusSlot {
	if m.focus != focusFailureHotspots {
		return focusSlot{Panel: m.focus}
	}
	key := ""
	if m.failureHotspotPanelsSplit() {
		key = m.currentHotspotAgentKey()
	}
	return focusSlot{Panel: focusFailureHotspots, HotspotAgentKey: key}
}

func (m *model) setFocusSlot(slot focusSlot) {
	m.focus = slot.Panel
	if slot.Panel == focusRunQueue {
		m.runQueuePinned = false
		m.updateRunQueueCursor()
	}
	if slot.Panel != focusFailureHotspots {
		return
	}
	m.focusHotspotAgentKey = slot.HotspotAgentKey
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
	if m.detailOpen {
		m.followLockedDetailItems()
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
