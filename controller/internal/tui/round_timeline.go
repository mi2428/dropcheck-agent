package tui

import (
	"fmt"
	"sort"
	"strings"

	"dropcheck/controller/internal/watch"
)

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
	done := counts.OK + counts.Failed + counts.Skipped
	percent := 0
	if counts.Total > 0 {
		percent = done * 100 / counts.Total
	}
	span := "-"
	if endRound > 0 {
		span = fmt.Sprintf("%d..%d", startRound, endRound)
	}
	leftPlain := fmt.Sprintf("round=%d span=%s ok=%d fail=%d run=%d", m.Round, span, ok, failed, counts.Running)
	rightPlain := fmt.Sprintf("progress=%d%% %d/%d", percent, done, counts.Total)
	if runeLen(leftPlain)+1+runeLen(rightPlain) > width {
		leftWidth := max(0, width-runeLen(rightPlain)-1)
		if leftWidth == 0 {
			return valueStyle.Render(fit(rightPlain, width))
		}
		return valueStyle.Render(fit(leftPlain, leftWidth)) +
			valueStyle.Render(strings.Repeat(" ", max(1, width-leftWidth-runeLen(rightPlain)))) +
			keyStyle.Render("progress=") +
			valueStyle.Render(fmt.Sprintf("%d%% %d/%d", percent, done, counts.Total))
	}
	left := keyStyle.Render("round=") + valueStyle.Render(fmt.Sprint(m.Round)) +
		keyStyle.Render(" span=") + valueStyle.Render(span) +
		keyStyle.Render(" ok=") + okStatusStyle.Render(fmt.Sprint(ok)) +
		keyStyle.Render(" fail=") + failedStatusStyle.Render(fmt.Sprint(failed)) +
		keyStyle.Render(" run=") + runningStatusStyle.Render(fmt.Sprint(counts.Running))
	right := keyStyle.Render("progress=") + valueStyle.Render(fmt.Sprintf("%d%% %d/%d", percent, done, counts.Total))
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
	for column := range columns {
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

func (m model) expectedRoundAgentCount() int {
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

func (m model) roundProgressView(width int) string {
	counts := m.roundProgressCounts()
	done := counts.OK + counts.Failed + counts.Skipped
	percent := 0
	if counts.Total > 0 {
		percent = done * 100 / counts.Total
	}
	prefixPlain := fmt.Sprintf("round=%d progress=", m.Round)
	suffixPlain := fmt.Sprintf(" %3d%% %d/%d run=%d fail=%d", percent, done, counts.Total, counts.Running, counts.Failed)
	lineWidth := width - runeLen(prefixPlain) - runeLen(suffixPlain)
	if lineWidth < 4 {
		line := prefixPlain + strings.TrimSpace(suffixPlain)
		return valueStyle.Render(fit(line, width))
	}
	lineWidth = clamp(lineWidth, 4, 48)
	line := keyStyle.Render("round=") +
		valueStyle.Render(fmt.Sprint(m.Round)) +
		keyStyle.Render(" progress=") +
		renderRoundProgressLine(counts, lineWidth) +
		valueStyle.Render(suffixPlain)
	return fitANSI(line, width)
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

func renderRoundProgressLine(counts roundProgressCounts, width int) string {
	if width <= 0 {
		return ""
	}
	if counts.Total <= 0 {
		return dimStyle.Render(strings.Repeat("-", width))
	}
	widths := proportionalWidths([]int{counts.OK, counts.Failed, counts.Skipped, counts.Running, counts.Pending}, counts.Total, width)
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
		Index int
		Value int
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
		remainders = append(remainders, remainder{Index: i, Value: scaled % total})
	}
	sort.SliceStable(remainders, func(i, j int) bool {
		return remainders[i].Value > remainders[j].Value
	})
	for remaining, i := width-used, 0; remaining > 0 && len(remainders) > 0; remaining, i = remaining-1, i+1 {
		widths[remainders[i%len(remainders)].Index]++
	}
	return widths
}
