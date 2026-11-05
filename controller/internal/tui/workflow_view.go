package tui

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"dropcheck/controller/internal/control"
	"dropcheck/controller/internal/harness"
	"dropcheck/controller/internal/render"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

type workflowSelectionRow struct {
	id, label string
	selected  bool
}

func (m workflowModel) selectionRows() []workflowSelectionRow {
	if m.compiled == nil {
		return nil
	}
	view := m.compiled.Preview()
	var rows []workflowSelectionRow
	switch m.group {
	case 0:
		for _, a := range view.Agents {
			rows = append(rows, workflowSelectionRow{a.ID, a.DisplayName() + " [" + a.ID + "]", slices.Contains(m.selection.AgentIDs, a.ID)})
		}
	case 1:
		for _, t := range view.Targets {
			rows = append(rows, workflowSelectionRow{t.ID, t.Name + " ssid=" + t.SSID + " band=" + t.Band + " [" + t.ID + "]", slices.Contains(m.selection.TargetIDs, t.ID)})
		}
	case 2:
		for _, c := range view.Checks {
			rows = append(rows, workflowSelectionRow{c.ID, c.Name + " [" + c.ID + "]", slices.Contains(m.selection.CheckIDs, c.ID)})
		}
	}
	return rows
}

func (m *workflowModel) toggleSelection() {
	rows := m.selectionRows()
	if len(rows) == 0 {
		return
	}
	id := rows[m.cursor].id
	selected := &m.selection.AgentIDs
	if m.group == 1 {
		selected = &m.selection.TargetIDs
	} else if m.group == 2 {
		selected = &m.selection.CheckIDs
	}
	if index := slices.Index(*selected, id); index >= 0 {
		*selected = slices.Delete(*selected, index, index+1)
	} else {
		*selected = append(*selected, id)
	}
	m.selected, m.message = nil, ""
}

func (m workflowModel) currentReport() harness.Report {
	if m.phase == workflowReview && len(m.runs) > 0 {
		return m.runs[m.review].report
	}
	return m.liveReport
}

func workflowScopeLabel(scope harness.Scope) string {
	if scope.Kind == "" {
		return "none (v=choose; focus is not scope)"
	}
	if scope.Kind == harness.ScopeRun {
		return "GLOBAL run"
	}
	return strings.Join([]string{scope.Kind, "agent=" + scope.AgentID, "target=" + scope.TargetID, "check=" + scope.CheckID}, " ")
}

func (m workflowModel) modeLabel() string {
	if m.loop {
		return "Loop (explicit stop required)"
	}
	if m.count == "1" {
		return "Once"
	}
	return "N rounds=" + m.count
}

func (m workflowModel) View() tea.View {
	view := tea.NewView(m.renderWorkflow())
	view.AltScreen = true
	return view
}

func (m workflowModel) renderWorkflow() string {
	width, height := max(1, m.width), max(1, m.height)
	help := "Ctrl-C=Exit"
	header := "Dropcheck TUI / " + string(m.phase)
	var body []string
	scroll := 0
	selectedRow := -1
	rowStart, rowEnd := 0, 0
	dashboardFrame := false
	switch m.phase {
	case workflowLoad:
		help = "Enter=Load existing YAML  Esc=Retained review  Ctrl-C=Exit"
		body = []string{"Load Plan / YAML", "path> " + m.path, "No Wi-Fi mutation or probe before explicit Start."}
		if m.loading {
			body = append(body, "Loading metadata / validating Plan...")
		}
	case workflowSelect:
		help = "Tab=Agents/Targets/Checks Space=Toggle 1=Once n=N l=Loop Enter=Preview q=Exit"
		groups := []string{"Agents", "Targets", "Checks"}
		body = append(body, "Select "+groups[m.group]+" / "+m.compiled.Preview().Name,
			fmt.Sprintf("selected agents=%d targets=%d checks=%d", len(m.selection.AgentIDs), len(m.selection.TargetIDs), len(m.selection.CheckIDs)))
		for i, row := range m.selectionRows() {
			marker := "[ ]"
			if row.selected {
				marker = "[x]"
			}
			prefix := "  "
			if i == m.cursor {
				prefix = "> "
			}
			body = append(body, prefix+marker+" "+row.label)
		}
		body = append(body, "Run mode: "+m.modeLabel())
		if m.countEdit {
			body = append(body, "N> "+m.count+" (Enter=apply)")
		}
		if len(m.selectionRows()) > 0 {
			selectedRow = 2 + m.cursor
		}
	case workflowPreview:
		help = "Enter=START explicitly  j/k=Scroll  Esc=Selection  q=Exit"
		if m.rerunReview {
			help = "Enter=START explicitly  j/k=Scroll  Esc=Review  q=Exit"
		}
		header += " / Preview / " + m.modeLabel()
		body = workflowPreviewLines(m.selected.Preview())
		rounds := uint64(0)
		if !m.loop {
			rounds, _ = strconv.ParseUint(m.count, 10, 64) // preflight validated this.
		}
		if cost, err := m.selected.Cost(rounds, m.loop); err == nil {
			label := fmt.Sprintf("Cost upper bound: %d retained attempts and %d controller dispatches per round", cost.AttemptsPerRound, cost.OperationsPerRound)
			if m.loop {
				label += "; Loop has no finite total"
			} else {
				label += fmt.Sprintf("; %d controller dispatches across %d rounds", cost.OperationsTotal, rounds)
			}
			if cost.UnboundedDownload {
				label += "; download response bytes unbounded"
			}
			body = slices.Insert(body, 3, label)
		}
		scroll = m.scroll
	case workflowRunning, workflowStopping, workflowReview:
		report := m.currentReport()
		if m.phase == workflowReview {
			header = fmt.Sprintf("Review %d/%d run=%s state=%s outcome=%s", m.review+1, len(m.runs), report.RunID, report.State, report.Outcome)
			help = "i=Inspect r=Selected rerun f=Failed rerun e=Selection o=Reload w=Export W=Overwrite [ / ]=Reports q=Exit"
		} else {
			dashboardFrame = true
			header += " / run=" + report.RunID + " / " + m.globalControl
			help = "GLOBAL Ctrl-Z=Pause/Resume Ctrl-N=Skip q/Ctrl-C=Stop+Cleanup / v=Scope p/s/x=Scoped Pause/Skip/Cancel i=Inspect"
		}
		if m.exportInput {
			help = "Report JSON destination: Enter=Save Esc=Cancel; " + map[bool]string{false: "existing file rejected", true: "replace existing regular file"}[m.exportOverwrite]
			body = []string{"Export retained run " + report.RunID, "path> " + m.exportPath}
		} else if m.scopePick {
			body = []string{"Confirm explicit execution scope (Enter); Esc=back", "Dashboard focus never changes execution scope."}
			for i, scope := range m.scopes {
				prefix := "  "
				if i == m.scopeIndex {
					prefix = "> "
				}
				body = append(body, prefix+workflowScopeLabel(scope))
			}
			if len(m.scopes) > 0 {
				selectedRow = 2 + m.scopeIndex
			}
		} else if m.inspect {
			body = workflowReportLines(report, m.stepCursor, m.detail, width)
			if m.active != nil && m.liveEvicted > 0 {
				body = append(body, fmt.Sprintf("live inspector: %d prior steps evicted; canonical final Report remains core-owned", m.liveEvicted))
			}
			if m.detail {
				scroll = m.detailLine
			} else {
				if len(report.Steps) > 0 {
					selectedRow = 1 + len(report.Agents) + m.stepCursor
				}
			}
			help = "j/k=Result or detail scroll Enter=Attempts+Measurements Esc=Back r=Selected rerun i=Dashboard q=Safe stop/exit"
		} else {
			dashboard := m.dashboard
			if m.phase == workflowReview {
				dashboard = m.runs[m.review].dashboard
			}
			dashboard.width, dashboard.height = width, max(1, height-3)
			lines := strings.Split(dashboard.render(), "\n")
			// The wrapper owns execution controls; don't repeat the watch-only
			// help line that describes Ctrl-C as immediate Quit.
			if len(lines) > 0 {
				lines = lines[1:]
			}
			body = lines
		}
		if m.active != nil {
			header += " / selectedScope=" + workflowScopeLabel(m.scope)
		}
	}
	if !dashboardFrame {
		var wrapped []string
		for i, line := range body {
			if i == selectedRow {
				rowStart = len(wrapped)
			}
			plain := sanitizeLogText(ansi.Strip(line))
			if plain == "" {
				wrapped = append(wrapped, "")
			} else {
				wrapped = append(wrapped, detailWrapLogBody(plain, width)...)
			}
			if i == selectedRow {
				rowEnd = len(wrapped)
			}
		}
		body = wrapped
	}
	lines := []string{fitText(header, width), fitText(help, width)}
	if m.options.Transport != nil && (m.phase == workflowRunning || m.phase == workflowStopping || m.phase == workflowReview) {
		statusByAgent := map[string]control.TransportStatus{}
		for _, status := range m.options.Transport() {
			statusByAgent[status.AgentID] = status
		}
		agents := reportAgentsForTransport(m)
		if len(agents) > max(1, height-4) {
			lines = append(lines, fitText(fmt.Sprintf("Transport now: %d agents; detail requires taller viewport", len(agents)), width))
			agents = nil
		}
		for _, agent := range agents {
			status, ok := statusByAgent[agent]
			if !ok {
				lines = append(lines, fitText("Transport now agent="+agent+" stream=disconnected", width))
				continue
			}
			state := "idle"
			if status.Pending > 0 {
				state = "busy"
			}
			age := func(value time.Time) string {
				if value.IsZero() {
					return "never"
				}
				return time.Since(value).Truncate(time.Second).String()
			}
			lines = append(lines, fitText(fmt.Sprintf("Transport now agent=%s stream=connected %s last_frame=%s last_accepted=%s pending=%d", agent, state, age(status.LastSeen), age(status.LastAccepted), status.Pending), width))
		}
		failures := 0
		for _, problem := range m.currentReport().Problems {
			if problem.Kind == "cleanup" {
				failures++
			}
		}
		if failures > 0 && len(lines) < height-2 {
			lines = append(lines, fitText(fmt.Sprintf("Cleanup (separate from Wi-Fi verdict): %d errors; inspect Report for details", failures), width))
		}
	}
	if m.phase == workflowReview && m.runEvictions > 0 {
		lines = append(lines, fitText(fmt.Sprintf("Whole-run retention: last %d reports; %d oldest evicted", maxRetainedRuns, m.runEvictions), width))
	}
	if m.message != "" {
		lines = append(lines, fitText(m.message, width))
	}
	available := max(0, height-len(lines))
	if selectedRow >= 0 {
		// Anchor the selected row in physical lines, after header and messages are known.
		scroll = rowStart
		if rowEnd-rowStart <= available {
			scroll = max(0, rowEnd-available)
		}
	}
	start := min(max(0, scroll), max(0, len(body)-available))
	for _, line := range body[start:min(len(body), start+available)] {
		if dashboardFrame {
			lines = append(lines, fitANSI(line, width))
		} else {
			lines = append(lines, fitText(line, width))
		}
	}
	return strings.Join(lines[:min(height, len(lines))], "\n")
}

func reportAgentsForTransport(m workflowModel) []string {
	if m.phase == workflowReview {
		var agents []string
		for _, agent := range m.runs[m.review].report.Agents {
			agents = append(agents, agent.Agent.ID)
		}
		return agents
	}
	var agents []string
	for _, agent := range m.selected.Preview().Agents {
		agents = append(agents, agent.ID)
	}
	return agents
}

func workflowPreviewLines(view harness.Preview) []string {
	lines := []string{"Validated Plan: " + view.Name, "Preview only; operations=0 until Start.", fmt.Sprintf("Round interval: %s", view.RoundInterval)}
	for _, a := range view.Agents {
		lines = append(lines, "agent "+a.ID+" / "+a.DisplayName())
	}
	for _, t := range view.Targets {
		lines = append(lines, fmt.Sprintf("target %s / %s ssid=%s bssid=%s network_id=%s band=%s agents=%s secret_present=%t cleanup: disconnect=%t forget=%t", t.ID, t.Name, t.SSID, t.BSSID, t.NetworkID, t.Band, strings.Join(t.BoundAgentIDs, ","), t.SecretPresent, t.DisconnectAfter != nil && *t.DisconnectAfter, t.ForgetAfter != nil && *t.ForgetAfter))
		if t.Connect != nil {
			lines = append(lines, workflowCheckPreview(*t.Connect))
		}
		if t.Wait != nil {
			lines = append(lines, workflowCheckPreview(*t.Wait))
		} else {
			lines = append(lines, "  wait: explicitly disabled")
		}
		for _, c := range t.Checks {
			lines = append(lines, workflowCheckPreview(c))
		}
		for _, cap := range t.CapabilityPreflight {
			lines = append(lines, "  capability "+cap.Operation+" band="+cap.Band+" state="+cap.State+" reason="+cap.Reason)
		}
	}
	return lines
}

func workflowCheckPreview(check harness.CheckInfo) string {
	p := check.Policy
	destination := check.Destination
	if strings.Contains(destination, "https://") || strings.Contains(destination, "http://") {
		destination = render.TextBlock(destination, render.Presentation{})
	}
	return fmt.Sprintf("  %s %s ops=[%s] required=%t destination=%s traffic=%s operation_timeout=%s policy: attempts=%d repeat=%d delay=%s eventual=%s stable=%s interval=%s timeout=%s expected=[%s]", check.ID, check.Name, strings.Join(check.Operations, ","), check.Required, destination, check.Traffic, check.OperationTimeout, p.Attempts, p.Repeat, p.Delay, p.Eventually, p.StableFor, p.Interval, p.Timeout, strings.Join(check.Expectations, "; "))
}

func workflowReportLines(report harness.Report, cursor int, detail bool, widths ...int) []string {
	if detail {
		view := render.Presentation{Detail: true}
		if len(widths) > 0 {
			view.Width = widths[0]
		}
		if len(report.Steps) > 0 {
			report.Steps = report.Steps[min(max(cursor, 0), len(report.Steps)-1):][:1]
		}
		text, err := render.Report(report, view)
		if err != nil {
			return strings.Split(render.Fields([][2]string{{"Error", "measurement rendering: " + err.Error()}}, view), "\n")
		}
		text = render.Fields([][2]string{{"View", "selected step; core outcome/counts cover the whole retained run"}}, view) + "\n" + text
		for _, step := range report.Steps {
			if step.Outcome == harness.OutcomePass {
				for _, attempt := range step.Attempts {
					if attempt.Outcome != harness.OutcomePass {
						text += render.Fields([][2]string{{"Recovered", "earlier failed/missing attempt retained."}}, view)
						break
					}
				}
			}
		}
		return strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	}
	rows := [][2]string{{"Run", fmt.Sprintf("run=%s state=%s outcome=%s pass=%d fail=%d missing=%d skip=%d canceled=%d", report.RunID, report.State, report.Outcome, report.Counts.Passed, report.Counts.Failed, report.Counts.Missing, report.Counts.Skipped, report.Counts.Canceled)}}
	for _, agent := range report.Agents {
		rows = append(rows, [2]string{"Agent", fmt.Sprintf("agent=%s round=%d state=%s phase=%s outcome=%s pass=%d fail=%d missing=%d skipped=%d canceled=%d", agent.Agent.ID, agent.Round, agent.State, agent.Phase, agent.Outcome, agent.Counts.Passed, agent.Counts.Failed, agent.Counts.Missing, agent.Counts.Skipped, agent.Counts.Canceled)})
	}
	lines := strings.Split(strings.TrimSuffix(render.Fields(rows, render.Presentation{}), "\n"), "\n")
	for i, step := range report.Steps {
		prefix := "  "
		if i == cursor {
			prefix = "> "
		}
		lines = append(lines, fmt.Sprintf("%sround=%d %s %s outcome=%s skip=%s attempts=%d reason=%s", prefix, step.Round, workflowScopeLabel(step.Scope), step.Name, step.Outcome, step.SkipReason, len(step.Attempts), step.Reason))
	}
	lines = append(lines, "Cleanup (independent from check outcome)")
	for _, cleanup := range report.Cleanup {
		status := "missing result"
		if cleanup.Raw != nil {
			status = cleanup.Raw.GetStatus().String()
		}
		lines = append(lines, fmt.Sprintf("  %s status=%s error=%s", cleanup.Name, status, cleanup.Error))
	}
	for _, problem := range report.Problems {
		lines = append(lines, "problem "+problem.Kind+" "+workflowScopeLabel(problem.Scope)+" "+problem.Message)
	}
	if report.EvictedRounds > 0 || report.EvictedProblems > 0 {
		lines = append(lines, fmt.Sprintf("core retention: evicted rounds=%d problems=%d", report.EvictedRounds, report.EvictedProblems))
	}
	return lines
}
