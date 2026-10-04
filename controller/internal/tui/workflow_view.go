package tui

import (
	"fmt"
	"slices"
	"strings"

	"dropcheck/controller/internal/harness"
	"dropcheck/controller/internal/pipeline"
	"dropcheck/controller/internal/render"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

type workflowSelectionRow struct { id, label string; selected bool }

func (m workflowModel) selectionRows() []workflowSelectionRow {
	if m.compiled == nil { return nil }
	view := m.compiled.Preview()
	var rows []workflowSelectionRow
	switch m.group {
	case 0:
		for _, a := range view.Agents { rows = append(rows, workflowSelectionRow{a.ID, a.DisplayName()+" ["+a.ID+"]", slices.Contains(m.selection.AgentIDs,a.ID)}) }
	case 1:
		for _, t := range view.Targets { rows = append(rows, workflowSelectionRow{t.ID, t.Name+" ssid="+t.SSID+" band="+t.Band+" ["+t.ID+"]", slices.Contains(m.selection.TargetIDs,t.ID)}) }
	case 2:
		for _, c := range view.Checks { rows = append(rows, workflowSelectionRow{c.ID, c.Name+" ["+c.ID+"]", slices.Contains(m.selection.CheckIDs,c.ID)}) }
	}
	return rows
}

func (m *workflowModel) toggleSelection() {
	rows := m.selectionRows()
	if len(rows) == 0 { return }
	id := rows[m.cursor].id
	selected := &m.selection.AgentIDs
	if m.group == 1 { selected = &m.selection.TargetIDs } else if m.group == 2 { selected = &m.selection.CheckIDs }
	if index := slices.Index(*selected,id); index >= 0 { *selected = slices.Delete(*selected,index,index+1) } else { *selected = append(*selected,id) }
	m.selected, m.message = nil, ""
}

func (m workflowModel) currentReport() harness.Report {
	if m.phase == workflowReview && len(m.runs) > 0 { return m.runs[m.review].report }
	return m.liveReport
}

func workflowScopeLabel(scope harness.Scope) string {
	if scope.Kind == "" { return "none (v=choose; focus is not scope)" }
	if scope.Kind == harness.ScopeRun { return "GLOBAL run" }
	return strings.Join([]string{scope.Kind, "agent="+scope.AgentID, "target="+scope.TargetID, "check="+scope.CheckID}, " ")
}

func (m workflowModel) modeLabel() string {
	if m.loop { return "Loop (explicit stop required)" }
	if m.count == "1" { return "Once" }
	return "N rounds="+m.count
}

func (m workflowModel) View() tea.View {
	view := tea.NewView(m.renderWorkflow())
	view.AltScreen = true
	return view
}

func (m workflowModel) renderWorkflow() string {
	width, height := max(1,m.width), max(1,m.height)
	help := "Ctrl-C=Exit"
	header := "Dropcheck TUI / "+string(m.phase)
	var body []string
	scroll := 0
	dashboardFrame := false
	switch m.phase {
	case workflowLoad:
		help = "Enter=Load existing YAML  Esc=Retained review  Ctrl-C=Exit"
		body = []string{"Load Plan / YAML", "path> "+m.path, "No Wi-Fi mutation or probe before explicit Start."}
		if m.loading { body = append(body,"Loading metadata / validating Plan...") }
	case workflowSelect:
		help = "Tab=Agents/Targets/Checks Space=Toggle 1=Once n=N l=Loop Enter=Preview q=Exit"
		groups := []string{"Agents","Targets","Checks"}
		body = append(body, "Select "+groups[m.group]+" / "+m.compiled.Preview().Name,
			fmt.Sprintf("selected agents=%d targets=%d checks=%d",len(m.selection.AgentIDs),len(m.selection.TargetIDs),len(m.selection.CheckIDs)))
		for i,row := range m.selectionRows() {
			marker := "[ ]"
			if row.selected { marker = "[x]" }
			prefix := "  "
			if i == m.cursor { prefix = "> " }
			body = append(body,prefix+marker+" "+row.label)
		}
		body = append(body,"Run mode: "+m.modeLabel())
		if m.countEdit { body = append(body,"N> "+m.count+" (Enter=apply)") }
		scroll = max(0,m.cursor-(height-6))
	case workflowPreview:
		help = "Enter=START explicitly  j/k=Scroll  Esc=Selection  q=Exit"
		header += " / Preview / "+m.modeLabel()
		body = workflowPreviewLines(m.selected.Preview())
		scroll = m.scroll
	case workflowRunning, workflowStopping, workflowReview:
		report := m.currentReport()
		if m.phase == workflowReview {
			header = fmt.Sprintf("Review %d/%d run=%s state=%s outcome=%s",m.review+1,len(m.runs),report.RunID,report.State,report.Outcome)
			help = "i=Inspect r=Selected rerun f=Failed rerun e=Selection o=Reload [ / ]=Reports q=Exit"
		} else {
			dashboardFrame = true
			header += " / run="+report.RunID+" / "+m.globalControl
			help = "GLOBAL Ctrl-Z=Pause/Resume Ctrl-N=Skip q/Ctrl-C=Stop+Cleanup / v=Scope p/s/x=Scoped Pause/Skip/Cancel i=Inspect"
		}
		if m.scopePick {
			body = []string{"Confirm explicit execution scope (Enter); Esc=back", "Dashboard focus never changes execution scope."}
			for i,scope := range m.scopes {
				prefix := "  "
				if i == m.scopeIndex { prefix = "> " }
				body = append(body,prefix+workflowScopeLabel(scope))
			}
			scroll = max(0,m.scopeIndex-(height-5))
		} else if m.inspect {
			body = workflowReportLines(report,m.stepCursor,m.detail)
			if m.active!=nil&&m.liveEvicted>0 {body=append(body,fmt.Sprintf("live inspector: %d prior steps evicted; canonical final Report remains core-owned",m.liveEvicted))}
			if m.detail { scroll = m.detailLine } else { scroll = max(0,m.stepCursor-(height-8)) }
			help = "j/k=Result or detail scroll Enter=Attempts+Measurements Esc=Back r=Selected rerun i=Dashboard q=Safe stop/exit"
		} else {
			dashboard := m.dashboard
			if m.phase == workflowReview { dashboard = m.runs[m.review].dashboard }
			dashboard.width, dashboard.height = width,max(1,height-3)
			lines := strings.Split(dashboard.render(),"\n")
			// The wrapper owns execution controls; don't repeat the watch-only
			// help line that describes Ctrl-C as immediate Quit.
			if len(lines) > 0 { lines = lines[1:] }
			body = lines
		}
		if m.active != nil { header += " / selectedScope="+workflowScopeLabel(m.scope) }
	}
	if !dashboardFrame {
		var wrapped []string
		for _,line := range body {
			plain := sanitizeLogText(ansi.Strip(line))
			if plain == "" { wrapped = append(wrapped,"") } else { wrapped = append(wrapped,detailWrapLogBody(plain,width)...) }
		}
		body = wrapped
	}
	lines := []string{fitText(header,width),fitText(help,width)}
	if m.message != "" { lines = append(lines,fitText(m.message,width)) }
	available := max(0,height-len(lines))
	start := min(max(0,scroll),max(0,len(body)-available))
	for _,line := range body[start:min(len(body),start+available)] {
		if dashboardFrame { lines = append(lines,fitANSI(line,width)) } else { lines = append(lines,fitText(line,width)) }
	}
	return strings.Join(lines[:min(height,len(lines))],"\n")
}

func workflowPreviewLines(view harness.Preview) []string {
	lines := []string{"Validated Plan: "+view.Name,"Preview only; operations=0 until Start."}
	for _,a := range view.Agents { lines = append(lines,"agent "+a.ID+" / "+a.DisplayName()) }
	for _,t := range view.Targets {
		lines = append(lines,fmt.Sprintf("target %s / %s ssid=%s bssid=%s band=%s agents=%s secret_present=%t cleanup: disconnect=%t forget=%t",t.ID,t.Name,t.SSID,t.BSSID,t.Band,strings.Join(t.BoundAgentIDs,","),t.SecretPresent,t.DisconnectAfter!=nil&&*t.DisconnectAfter,t.ForgetAfter!=nil&&*t.ForgetAfter))
		if t.Connect != nil { lines = append(lines,workflowCheckPreview(*t.Connect)) }
		if t.Wait != nil { lines = append(lines,workflowCheckPreview(*t.Wait)) } else { lines = append(lines,"  wait: explicitly disabled") }
		for _,c := range t.Checks { lines = append(lines,workflowCheckPreview(c)) }
		for _,cap := range t.CapabilityPreflight { lines = append(lines,"  capability "+cap.Operation+" band="+cap.Band+" state="+cap.State+" reason="+cap.Reason) }
	}
	return lines
}

func workflowCheckPreview(check harness.CheckInfo) string {
	p := check.Policy
	return fmt.Sprintf("  %s %s op=%s required=%t operation_timeout=%s policy: attempts=%d repeat=%d delay=%s eventual=%s stable=%s interval=%s timeout=%s expected=[%s]",check.ID,check.Name,check.Operation,check.Required,check.OperationTimeout,p.Attempts,p.Repeat,p.Delay,p.Eventually,p.StableFor,p.Interval,p.Timeout,strings.Join(check.Expectations,"; "))
}

func workflowReportLines(report harness.Report, cursor int, detail bool) []string {
	lines := []string{fmt.Sprintf("run=%s state=%s outcome=%s pass=%d fail=%d missing=%d skip=%d canceled=%d",report.RunID,report.State,report.Outcome,report.Counts.Passed,report.Counts.Failed,report.Counts.Missing,report.Counts.Skipped,report.Counts.Canceled)}
	for _,agent := range report.Agents { lines = append(lines,fmt.Sprintf("agent=%s round=%d state=%s phase=%s outcome=%s pass=%d fail=%d missing=%d skipped=%d canceled=%d",agent.Agent.ID,agent.Round,agent.State,agent.Phase,agent.Outcome,agent.Counts.Passed,agent.Counts.Failed,agent.Counts.Missing,agent.Counts.Skipped,agent.Counts.Canceled)) }
	if !detail {
		for i,step := range report.Steps {
			prefix := "  "
			if i == cursor { prefix = "> " }
			lines = append(lines,fmt.Sprintf("%sround=%d %s %s outcome=%s skip=%s attempts=%d reason=%s",prefix,step.Round,workflowScopeLabel(step.Scope),step.Name,step.Outcome,step.SkipReason,len(step.Attempts),step.Reason))
		}
	} else if len(report.Steps) > 0 {
		step := report.Steps[min(cursor,len(report.Steps)-1)]
		lines = append(lines,fmt.Sprintf("%s %s outcome=%s skip=%s reason=%s",workflowScopeLabel(step.Scope),step.Name,step.Outcome,step.SkipReason,step.Reason))
		for _,attempt := range step.Attempts {
			lines = append(lines,fmt.Sprintf("attempt=%d repeat=%d sample=%d outcome=%s reason=%s",attempt.Number,attempt.Repeat,attempt.Sample,attempt.Outcome,attempt.Reason))
			for _,finding := range attempt.Findings { lines = append(lines,fmt.Sprintf("  %s observed=%s expected=%s %s",finding.Metric,finding.Observed,finding.Expected,finding.Message)) }
			if attempt.Result.Raw != nil {
				text,err := render.CommandResult("",attempt.Result.Raw,attempt.Result.Options,pipeline.FormatText)
				if err != nil { lines = append(lines,"measurement rendering error: "+err.Error()) } else { lines = append(lines,strings.Split(text,"\n")...) }
			}
			for _,part := range attempt.Result.Parts {
				lines = append(lines,"acquisition "+part.Name+" error="+part.Error)
				if part.Raw != nil {
					text,err := render.CommandResult("",part.Raw,attempt.Result.Options,pipeline.FormatText)
					if err != nil { lines = append(lines,"measurement rendering error: "+err.Error()) } else { lines = append(lines,strings.Split(text,"\n")...) }
				}
			}
		}
		if step.Outcome == harness.OutcomePass {
			for _,attempt := range step.Attempts { if attempt.Outcome != harness.OutcomePass { lines = append(lines,"Recovered: earlier failed/missing attempt retained."); break } }
		}
	}
	lines = append(lines,"Cleanup (independent from check outcome):")
	for _,cleanup := range report.Cleanup {
		status := "missing result"
		if cleanup.Raw != nil { status = cleanup.Raw.GetStatus().String() }
		lines = append(lines,fmt.Sprintf("  %s status=%s error=%s",cleanup.Name,status,cleanup.Error))
	}
	for _,problem := range report.Problems { lines = append(lines,"problem "+problem.Kind+" "+workflowScopeLabel(problem.Scope)+" "+problem.Message) }
	if report.EvictedRounds > 0 || report.EvictedProblems > 0 { lines = append(lines,fmt.Sprintf("core retention: evicted rounds=%d problems=%d",report.EvictedRounds,report.EvictedProblems)) }
	return lines
}
