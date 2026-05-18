package watchstate

import (
	"fmt"
	"sort"
	"time"

	"dropcheck/controller/internal/watch"
)

// RecentEventHistogram builds a fixed-width histogram over the recent time
// window ending at a stable bucket boundary.
func RecentEventHistogram(times []time.Time, width int, window time.Duration, now time.Time) OccurrenceHistogram {
	if width <= 0 || window <= 0 {
		return OccurrenceHistogram{}
	}
	if now.IsZero() {
		now = time.Now()
	}
	bucketCount := RecentEventBucketCount(window, width)
	bucketWidth := RecentEventBucketWidth(window, bucketCount)
	end := RecentHistogramEnd(times, bucketWidth, now)
	start := end.Add(-bucketWidth * time.Duration(bucketCount))
	counts := make([]int, bucketCount)
	count := 0
	maxCount := 0
	for _, at := range times {
		if at.Before(start) || !at.Before(end) {
			continue
		}
		index := int(at.Sub(start) / bucketWidth)
		index = Clamp(index, 0, bucketCount-1)
		counts[index]++
		count++
		if counts[index] > maxCount {
			maxCount = counts[index]
		}
	}
	return OccurrenceHistogram{
		First:       start,
		Last:        end,
		BucketWidth: bucketWidth,
		Counts:      counts,
		Max:         maxCount,
		Count:       count,
	}
}

// RecentEventBucketCount caps bucket count to the requested graph width and
// avoids sub-second buckets for watch timelines.
func RecentEventBucketCount(window time.Duration, width int) int {
	if window <= 0 || width <= 0 {
		return 0
	}
	oneSecondBuckets := max(int((window+time.Second-1)/time.Second), 1)
	return Clamp(width, 1, oneSecondBuckets)
}

// RecentEventBucketWidth returns the rounded-up duration represented by each
// histogram bucket.
func RecentEventBucketWidth(window time.Duration, bucketCount int) time.Duration {
	if window <= 0 || bucketCount <= 0 {
		return time.Second
	}
	return (window + time.Duration(bucketCount) - 1) / time.Duration(bucketCount)
}

// RecentHistogramEnd returns the next bucket boundary after now or any future
// event timestamp.
func RecentHistogramEnd(times []time.Time, bucketWidth time.Duration, now time.Time) time.Time {
	end := now
	if end.IsZero() {
		end = time.Now()
	}
	for _, at := range times {
		if at.After(end) {
			end = at
		}
	}
	return NextHistogramBucketBoundary(end, bucketWidth)
}

// NextHistogramBucketBoundary rounds at up to the next bucket boundary.
func NextHistogramBucketBoundary(at time.Time, bucketWidth time.Duration) time.Time {
	if bucketWidth <= 0 {
		return at
	}
	width := int64(bucketWidth)
	unix := at.UnixNano()
	next := ((unix / width) + 1) * width
	return time.Unix(0, next).In(at.Location())
}

// SparklineEventsPerRow chooses a readable absolute scale for a graph height.
func SparklineEventsPerRow(maxCount int, height int) int {
	if maxCount <= 0 || height <= 0 {
		return 1
	}
	needed := (maxCount + height - 1) / height
	return NiceSparklineUnit(needed)
}

// NiceSparklineUnit rounds needed up to a 1, 2, 3, or 5 times power-of-ten
// scale.
func NiceSparklineUnit(needed int) int {
	if needed <= 1 {
		return 1
	}
	magnitude := 1
	steps := []int{1, 2, 3, 5}
	for {
		for _, step := range steps {
			candidate := step * magnitude
			if candidate >= needed {
				return candidate
			}
		}
		magnitude *= 10
	}
}

// ResampleSparklineCounts compresses or expands counts to the rendered width.
func ResampleSparklineCounts(counts []int, width int) []int {
	if width <= 0 {
		return nil
	}
	out := make([]int, width)
	if len(counts) == 0 {
		return out
	}
	if len(counts) == width {
		copy(out, counts)
		return out
	}
	for column := range width {
		start := column * len(counts) / width
		end := (column + 1) * len(counts) / width
		if end <= start {
			end = start + 1
		}
		start = Clamp(start, 0, len(counts)-1)
		end = Clamp(end, start+1, len(counts))
		for _, count := range counts[start:end] {
			if count > out[column] {
				out[column] = count
			}
		}
	}
	return out
}

// MaxInt returns the largest integer in values.
func MaxInt(values []int) int {
	maxValue := 0
	for _, value := range values {
		if value > maxValue {
			maxValue = value
		}
	}
	return maxValue
}

// FormatBucketDuration returns the compact duration label used in graph axes.
func FormatBucketDuration(duration time.Duration) string {
	if duration <= 0 {
		return "instant"
	}
	if duration < time.Second {
		return duration.String()
	}
	if duration < time.Minute {
		return fmt.Sprintf("%ds", int(duration.Round(time.Second)/time.Second))
	}
	if duration < time.Hour {
		return fmt.Sprintf("%dm", int(duration.Round(time.Minute)/time.Minute))
	}
	return fmt.Sprintf("%dh", int(duration.Round(time.Hour)/time.Hour))
}

// OccurrenceGraphHeight reserves roughly half of detail content for a graph.
func OccurrenceGraphHeight(contentHeight int) int {
	if contentHeight <= 0 {
		return 0
	}
	height := Max(4, contentHeight*50/100)
	return Min(height, Max(1, contentHeight-3))
}

// PassingCheckOccurrences returns the event times that belong to one passing
// summary key.
func (s State) PassingCheckOccurrences(agent watch.AgentSnapshot, target watch.TargetSnapshot, step watch.StepSnapshot) []time.Time {
	key := PassingCheckKey(agent, target, step)
	if AgentKey(agent) == "" {
		key = PassingCheckSummaryKey(watch.AgentSnapshot{}, target, step)
	}
	if key == "" {
		return nil
	}
	times := make([]time.Time, 0, len(s.PassingChecks))
	for _, item := range s.PassingChecks {
		itemKey := PassingCheckKey(item.Agent, item.Target, item.Step)
		if AgentKey(agent) == "" {
			itemKey = PassingCheckSummaryKey(watch.AgentSnapshot{}, item.Target, item.Step)
		}
		if itemKey == key {
			times = append(times, item.When)
		}
	}
	return times
}

// FailedCheckOccurrences returns the event times that belong to one failed
// summary key.
func (s State) FailedCheckOccurrences(agent watch.AgentSnapshot, target watch.TargetSnapshot, finding watch.Finding) []time.Time {
	key := FailedCheckStateKey(agent, target, finding)
	if AgentKey(agent) == "" {
		key = FailedCheckSummaryKey(watch.AgentSnapshot{}, target, finding)
	}
	times := make([]time.Time, 0, len(s.FailedChecks))
	for _, item := range s.FailedChecks {
		itemKey := FailedCheckStateKey(item.Agent, item.Target, item.Finding)
		if AgentKey(agent) == "" {
			itemKey = FailedCheckSummaryKey(watch.AgentSnapshot{}, item.Target, item.Finding)
		}
		if itemKey == key {
			times = append(times, item.When)
		}
	}
	return times
}

// FailureHotspotOccurrences returns failure times for the selected hotspot.
func (s State) FailureHotspotOccurrences(item FailureHotspotSummary) []time.Time {
	key := FailureHotspotSummaryIdentity(item)
	if key == "" {
		return nil
	}
	times := make([]time.Time, 0, len(s.FailedChecks))
	for _, failedCheck := range s.FailedChecks {
		targetKey := FailureHotspotIdentity(failedCheck.Agent, failedCheck.Target, failedCheck.Finding.Target)
		if targetKey == key {
			times = append(times, failedCheck.When)
		}
	}
	sort.SliceStable(times, func(i, j int) bool {
		return times[i].Before(times[j])
	})
	return times
}
