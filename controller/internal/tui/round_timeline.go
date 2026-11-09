package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/mi2428/dropcheck-agent/controller/internal/tuirun"
)

func (m model) roundTimelineView(width int, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	events := m.outcomeEvents()
	startRound, endRound := m.roundTimelineRoundSpan(width)
	ok, failed := roundTimelineOutcomeCounts(events, startRound, endRound)
	lines := []string{m.roundTimelineHeaderView(width, startRound, endRound, ok, failed)}
	if height == 1 {
		return lines[0]
	}
	targets := m.checkStatusTargets()
	if len(targets) == 0 {
		lines = append(lines, dimStyle.Render("no targets"))
		return strings.Join(lines[:intMin(len(lines), height)], "\n")
	}
	grid := roundTimelineGrid(width, len(targets))
	for row := 0; row < grid.Rows && len(lines) < height; row++ {
		lines = append(lines, m.roundTimelineTargetRow(targets, width, grid, row))
	}
	return strings.Join(lines, "\n")
}

func (m model) roundTimelineHeaderView(width int, startRound uint64, endRound uint64, ok int, failed int) string {
	if width <= 0 {
		return ""
	}
	counts := m.roundProgressCounts()
	done := counts.OK + counts.Failed + counts.Skipped
	percent := 0
	if counts.Total > 0 {
		percent = done * 100 / counts.Total
	}
	span := "-"
	if endRound > 0 {
		span = fmt.Sprintf("%d..%d", startRound, endRound)
	}
	progressText := fmt.Sprintf("%d%% (%d/%d)", percent, done, counts.Total)
	leftPlain := fmt.Sprintf("round=%d span=%s ok=%d fail=%d run=%d", m.Round, span, ok, failed, counts.Running)
	rightPlain := "progress=" + progressText
	if runeLen(leftPlain)+1+runeLen(rightPlain) > width {
		leftWidth := intMax(0, width-runeLen(rightPlain)-1)
		if leftWidth == 0 {
			return valueStyle.Render(fit(rightPlain, width))
		}
		return valueStyle.Render(fit(leftPlain, leftWidth)) +
			valueStyle.Render(strings.Repeat(" ", intMax(1, width-leftWidth-runeLen(rightPlain)))) +
			keyStyle.Render("progress=") +
			valueStyle.Render(progressText)
	}
	left := keyStyle.Render("round=") + valueStyle.Render(fmt.Sprint(m.Round)) +
		keyStyle.Render(" span=") + valueStyle.Render(span) +
		keyStyle.Render(" ok=") + valueStyle.Render(fmt.Sprint(ok)) +
		keyStyle.Render(" fail=") + valueStyle.Render(fmt.Sprint(failed)) +
		keyStyle.Render(" run=") + valueStyle.Render(fmt.Sprint(counts.Running))
	right := keyStyle.Render("progress=") + valueStyle.Render(progressText)
	spaces := intMax(1, width-runeLen(leftPlain)-runeLen(rightPlain))
	return left + valueStyle.Render(strings.Repeat(" ", spaces)) + right
}

func (m model) roundTimelineRoundSpan(width int) (uint64, uint64) {
	visibleRounds := intMax(1, m.roundTimelineVisibleRounds(width))
	return m.roundTimelineRoundRange(visibleRounds)
}

func (m model) roundTimelineRoundRange(visibleRounds int) (uint64, uint64) {
	endRound := m.latestRound()
	if endRound == 0 {
		return 0, 0
	}
	visibleRounds = intMax(1, visibleRounds)
	startRound := uint64(1)
	if endRound > uint64(visibleRounds) {
		startRound = endRound - uint64(visibleRounds) + 1
	}
	return startRound, endRound
}

func (m model) roundTimelineVisibleRounds(width int) int {
	targets := m.checkStatusTargets()
	grid := roundTimelineGrid(width, len(targets))
	columns := grid.Columns
	tileWidths := roundTimelineTileWidths(width, columns)
	if len(tileWidths) == 0 {
		return 1
	}
	labelWidths := roundTimelineColumnLabelWidths(targets, tileWidths, grid.Rows)
	visible := 1
	for i, tileWidth := range tileWidths {
		_, plotWidth := roundTimelineTileLayoutForLabel(tileWidth, labelWidths[i])
		visible = intMax(visible, plotWidth)
	}
	return visible
}

func (m model) latestRound() uint64 {
	latest := m.Round
	for _, passingCheck := range m.PassingChecks {
		if passingCheck.Round > latest {
			latest = passingCheck.Round
		}
	}
	for _, failedCheck := range m.FailedChecks {
		if failedCheck.Round > latest {
			latest = failedCheck.Round
		}
	}
	return latest
}

type roundTimelineGridLayout struct {
	Columns int
	Rows    int
}

func roundTimelineGrid(width int, targetCount int) roundTimelineGridLayout {
	if targetCount <= 0 {
		return roundTimelineGridLayout{Columns: 1}
	}
	maxColumns := roundTimelineColumnCount(width, targetCount)
	rows := (targetCount + maxColumns - 1) / maxColumns
	rows = intMax(1, rows)
	columns := (targetCount + rows - 1) / rows
	return roundTimelineGridLayout{Columns: intMax(1, columns), Rows: rows}
}

func (m model) roundTimelineTargetRow(targets []tuirun.TargetSnapshot, width int, grid roundTimelineGridLayout, row int) string {
	if width <= 0 {
		return ""
	}
	columns := intMax(1, grid.Columns)
	rows := intMax(1, grid.Rows)
	tileWidths := roundTimelineTileWidths(width, columns)
	labelWidths := roundTimelineColumnLabelWidths(targets, tileWidths, rows)
	tiles := make([]string, 0, len(tileWidths))
	for column, tileWidth := range tileWidths {
		index := column*rows + row
		if index < len(targets) {
			tiles = append(tiles, m.roundTimelineTargetTile(targets[index], tileWidth, labelWidths[column]))
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
			if tileWidth < roundTimelineMinTileWidth() {
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
	available := intMax(1, width-roundTimelineTileGap*(columns-1))
	baseWidth := available / columns
	remainder := available % columns
	widths := make([]int, columns)
	for column := range columns {
		widths[column] = baseWidth
		if column < remainder {
			widths[column]++
		}
	}
	return widths
}

func roundTimelineColumnLabelWidths(targets []tuirun.TargetSnapshot, tileWidths []int, rows int) []int {
	widths := make([]int, len(tileWidths))
	rows = intMax(1, rows)
	for column, tileWidth := range tileWidths {
		maxLabelWidth, _ := roundTimelineTileLayout(tileWidth)
		labelWidth := 1
		for row := 0; row < rows; row++ {
			index := column*rows + row
			if index >= len(targets) {
				continue
			}
			labelWidth = intMax(labelWidth, intMin(maxLabelWidth, lipgloss.Width(checkStatusTargetLabel(targets[index]))))
		}
		widths[column] = clamp(labelWidth, 1, maxLabelWidth)
	}
	return widths
}

func (m model) roundTimelineTargetTile(target tuirun.TargetSnapshot, width int, labelWidth int) string {
	if width <= 0 {
		return ""
	}
	labelWidth, plotWidth := roundTimelineTileLayoutForLabel(width, labelWidth)
	plotWidth = m.roundTimelinePlotWidth(plotWidth)
	buckets, _, _ := m.targetRoundHistory(target, plotWidth)
	checkCount := intMax(1, len(m.checkStatusChecks()))
	label := timelineKeyStyle.Render(padVisible(roundTimelineTargetLabel(target, labelWidth), labelWidth))
	plot := renderTargetRoundHistory(buckets, checkCount, plotWidth)
	content := fitANSI(label+timelineValueStyle.Render(" ")+plot, width)
	return content + timelineValueStyle.Render(strings.Repeat(" ", intMax(0, width-lipgloss.Width(content))))
}

func (m model) roundTimelinePlotWidth(maxWidth int) int {
	if maxWidth <= 0 {
		return 0
	}
	latest := int(m.latestRound())
	if latest <= 0 {
		return 1
	}
	return clamp(latest, 1, maxWidth)
}

func roundTimelineTileLayout(width int) (labelWidth int, plotWidth int) {
	if width <= 0 {
		return 0, 0
	}
	labelWidth = intMin(roundTimelineTargetLabelRunes, width)
	if width > roundTimelineMinVisibleRounds+1 && width < roundTimelineMinTileWidth() {
		labelWidth = intMax(1, width-roundTimelineMinVisibleRounds-1)
	}
	plotWidth = intMax(1, width-labelWidth-1)
	return labelWidth, plotWidth
}

func roundTimelineTileLayoutForLabel(width int, labelWidth int) (int, int) {
	maxLabelWidth, _ := roundTimelineTileLayout(width)
	if maxLabelWidth <= 0 {
		return 0, 0
	}
	labelWidth = clamp(labelWidth, 1, maxLabelWidth)
	return labelWidth, intMax(1, width-labelWidth-1)
}

func roundTimelineMinTileWidth() int {
	return roundTimelineTargetLabelRunes + 1 + roundTimelineMinVisibleRounds
}

func roundTimelineTargetLabel(target tuirun.TargetSnapshot, width int) string {
	return fitANSI(checkStatusTargetLabel(target), width)
}

func (m model) targetRoundHistory(target tuirun.TargetSnapshot, width int) ([]targetRoundBucket, int, int) {
	width = intMax(1, width)
	buckets := make([]targetRoundBucket, width)
	connectFailedAgents := make([]map[string]bool, width)
	startRound, endRound := m.roundTimelineRoundRange(width)
	if startRound == 0 || endRound == 0 {
		return buckets, 0, 0
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
	expectedAgents := m.expectedRoundAgentCount(target)
	markConnectFailed := func(index int, agent tuirun.AgentSnapshot) {
		if connectFailedAgents[index] == nil {
			connectFailedAgents[index] = map[string]bool{}
		}
		connectFailedAgents[index][roundAgentKey(agent)] = true
	}
	for _, passingCheck := range m.PassingChecks {
		if checkStatusTargetKey(passingCheck.Target) != targetKey {
			continue
		}
		index, ok := indexForRound(passingCheck.Round)
		if !ok {
			continue
		}
		buckets[index].Seen = true
	}
	for _, failedCheck := range m.FailedChecks {
		if checkStatusTargetKey(failedCheck.Target) != targetKey {
			continue
		}
		index, ok := indexForRound(failedCheck.Round)
		if !ok {
			continue
		}
		buckets[index].Seen = true
		buckets[index].Failed++
		if connectionFailureCheck(failedCheck.Finding.Check) {
			markConnectFailed(index, failedCheck.Agent)
		}
		total++
		if buckets[index].Failed > peak {
			peak = buckets[index].Failed
		}
	}
	for i := range buckets {
		if len(connectFailedAgents[i]) == 0 {
			continue
		}
		if expectedAgents <= 1 || len(connectFailedAgents[i]) >= expectedAgents {
			buckets[i].ConnectFailed = true
		}
	}
	return buckets, total, peak
}

func roundTimelineOutcomeCounts(events []outcomeEvent, startRound uint64, endRound uint64) (ok, failed int) {
	if startRound == 0 || endRound == 0 {
		return 0, 0
	}
	for _, event := range events {
		if event.Round < startRound || event.Round > endRound {
			continue
		}
		switch event.Status {
		case "ok":
			ok++
		case "failed":
			failed++
		}
	}
	return ok, failed
}

func (m model) expectedRoundAgentCount(target tuirun.TargetSnapshot) int {
	targetKey := checkStatusTargetKey(target)
	seenForTarget := map[string]bool{}
	for _, state := range m.Targets {
		if checkStatusTargetKey(state.Target) == targetKey {
			seenForTarget[roundAgentKey(state.Agent)] = true
		}
	}
	if len(seenForTarget) > 0 {
		return len(seenForTarget)
	}
	if len(m.Agents) > 0 {
		return len(m.Agents)
	}
	seen := map[string]bool{}
	for _, target := range m.Targets {
		seen[roundAgentKey(target.Agent)] = true
	}
	for _, passingCheck := range m.PassingChecks {
		seen[roundAgentKey(passingCheck.Agent)] = true
	}
	for _, failedCheck := range m.FailedChecks {
		seen[roundAgentKey(failedCheck.Agent)] = true
	}
	return intMax(1, len(seen))
}

func roundAgentKey(agent tuirun.AgentSnapshot) string {
	if key := agentKey(agent); key != "" {
		return key
	}
	return "all"
}

func renderTargetRoundHistory(buckets []targetRoundBucket, checkCount int, width int) string {
	if width <= 0 {
		return ""
	}
	checkCount = intMax(1, checkCount)
	var b strings.Builder
	for i := range width {
		bucket := targetRoundBucket{}
		if i < len(buckets) {
			bucket = buckets[i]
		}
		if !bucket.Seen {
			b.WriteString(dimStyle.Render(" "))
			continue
		}
		if bucket.Failed == 0 {
			b.WriteString(timelineBaseStyle.Render("▁"))
			continue
		}
		if bucket.ConnectFailed {
			b.WriteString(connectFailureStyle.Render("X"))
			continue
		}
		b.WriteString(failGraphStyle.Render(failureCountBlock(bucket.Failed, checkCount)))
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
	OK      int
	Failed  int
	Skipped int
	Running int
	Pending int
	Total   int
}

func (m model) roundProgressCounts() roundProgressCounts {
	counts := roundProgressCounts{Total: len(m.Targets)}
	for _, target := range m.Targets {
		switch normalizeStatus(target.Status) {
		case "ok":
			counts.OK++
		case "failed":
			counts.Failed++
		case "skipped":
			counts.Skipped++
		case "running":
			counts.Running++
		default:
			counts.Pending++
		}
	}
	return counts
}
