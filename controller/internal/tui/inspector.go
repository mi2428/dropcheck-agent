package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/mi2428/dropcheck-agent/controller/internal/safevalue"
	"github.com/mi2428/dropcheck-agent/controller/internal/tuirun"
)

func (content detailContent) inspectorText(width int) string {
	var lines []string
	for _, field := range content.Fields {
		lines = append(lines, detailFieldHeader(field.Key)+": "+safevalue.Normalize(safevalue.Field(field.Key, field.Value)))
	}
	if content.Histogram.Count > 0 {
		lines = append(lines, "", detailTimelineLabel(content.Histogram, 5))
		lines = append(lines, renderDetailHistogram(content.Histogram, width, 5, content.GraphStyle)...)
	}
	for _, section := range content.Sections {
		lines = append(lines, "", detailSectionTitle(section))
		for _, row := range detailSectionNaturalRows(section) {
			lines = append(lines, detailSectionRowLines(section, row, width)...)
		}
	}
	return strings.Join(lines, "\n")
}

func (m model) inspectorDimensions(width, height int) (int, int) {
	if m.inspectorFull || width < 120 {
		return intMax(2, width), intMax(4, height)
	}
	return intMax(48, width*52/100), intMax(4, height)
}

func (m *model) refreshInspector() {
	m.followLockedDetailItems()
	w, h := m.inspectorDimensions(m.width, m.height)
	m.inspector = viewport.New(viewport.WithWidth(intMax(1, w-4)), viewport.WithHeight(intMax(1, h-5)))
	m.inspector.SoftWrap, m.inspector.FillHeight = true, true
	m.inspectorAt = m.currentTime()
	var content detailContent
	var ok bool
	switch m.detailPanel {
	case focusPassingChecks:
		m.inspectorTitle = "Passing Check Detail"
		if passingCheckSummaryIndexByIdentity(m.filteredPassingCheckSummaries(), m.detailPassingKey) >= 0 {
			content, ok = m.passingCheckDetailContent(intMax(1, w-4))
		}
	case focusFailedChecks:
		m.inspectorTitle = "Failed Check Detail"
		if failedCheckSummaryIndexByIdentity(m.filteredFailedCheckSummaries(), m.detailFailedKey) >= 0 {
			content, ok = m.failedCheckDetailContent(intMax(1, w-4))
		}
	case focusFailureHotspots:
		m.inspectorTitle = "Failure Hotspot Detail"
		if m.detailHotspotMode == failureHotspotModeCauses {
			m.inspectorTitle = "Failure Cause Detail"
		}
		if _, found := m.failureAnalysisRowByIdentity(m.detailHotspotMode, m.detailHotspotKey); found {
			content, ok = m.failureHotspotDetailContent(intMax(1, w-4))
		}
	}
	if !ok {
		m.inspectorScope = tuirun.Scope{}
		m.inspector.SetContent("The selected result is no longer retained. Close this pane and select another result.")
		return
	}
	selection := *m
	selection.detailOpen = false
	selection.focus = m.detailPanel
	m.inspectorScope = selection.selectionScope()
	m.inspector.SetContent(content.inspectorText(intMax(1, w-4)))
}

func (m *model) updateInspector(msg tea.Msg) tea.Cmd {
	if key, ok := msg.(tea.KeyPressMsg); ok {
		switch key.Keystroke() {
		case "esc":
			m.detailOpen = false
			return nil
		case "r":
			m.refreshInspector()
			return nil
		case "f":
			m.inspectorFull = !m.inspectorFull
			w, h := m.inspectorDimensions(m.width, m.height)
			m.inspector.SetWidth(intMax(1, w-4))
			m.inspector.SetHeight(intMax(1, h-5))
			return nil
		}
	}
	var cmd tea.Cmd
	m.inspector, cmd = m.inspector.Update(msg)
	return cmd
}

func (m model) inspectorView(frame string, width, height int) string {
	w, h := m.inspectorDimensions(width, height)
	view := m.inspector
	view.SetWidth(intMax(1, w-4))
	view.SetHeight(intMax(1, h-5))
	header := fmt.Sprintf("Snapshot %s  %.0f%%", m.inspectorAt.Format("15:04:05"), 100*view.ScrollPercent())
	body := header + "\n" + view.View() + "\nr=Refresh  f=Full screen  a=Actions  Esc=Close"
	pane := renderPanel(m.inspectorTitle, w, h, body)
	return lipgloss.NewCompositor(lipgloss.NewLayer(frame), lipgloss.NewLayer(pane).X(intMax(0, width-w)).Z(1)).Render()
}
