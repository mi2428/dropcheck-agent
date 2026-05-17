package watchstate

import (
	"reflect"
	"testing"
	"time"
)

func TestRecentEventHistogramScrollsWithoutReshaping(t *testing.T) {
	base := time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC)
	times := []time.Time{
		base,
		base.Add(3 * time.Second),
		base.Add(6 * time.Second),
	}
	before := RecentEventHistogram(times, 10, 10*time.Second, base.Add(8*time.Second))
	after := RecentEventHistogram(times, 10, 10*time.Second, base.Add(9*time.Second))
	if before.Max != after.Max || before.Count != after.Count {
		t.Fatalf("histogram stats changed while no event entered or expired: before=%#v after=%#v", before, after)
	}
	if got, want := after.Counts[:9], before.Counts[1:]; !reflect.DeepEqual(got, want) {
		t.Fatalf("histogram should scroll left one bucket without reshaping:\nbefore=%v\nafter =%v", before.Counts, after.Counts)
	}
	if after.Counts[9] != 0 {
		t.Fatalf("new rightmost bucket should be empty without new events: %v", after.Counts)
	}
}

func TestRecentEventHistogramDoesNotRebucketWithinBucketWidth(t *testing.T) {
	base := time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC)
	times := []time.Time{
		base.Add(1 * time.Second),
		base.Add(3 * time.Second),
		base.Add(8 * time.Second),
	}
	before := RecentEventHistogram(times, 5, 10*time.Second, base.Add(8*time.Second+500*time.Millisecond))
	after := RecentEventHistogram(times, 5, 10*time.Second, base.Add(9*time.Second+500*time.Millisecond))
	if !reflect.DeepEqual(after.Counts, before.Counts) {
		t.Fatalf("histogram should keep stable bucket boundaries until the next bucket:\nbefore=%v\nafter =%v", before.Counts, after.Counts)
	}
}

func TestRecentEventHistogramStacksOnlyCurrentBucketForNewEvents(t *testing.T) {
	base := time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC)
	before := RecentEventHistogram([]time.Time{
		base.Add(1 * time.Second),
		base.Add(3 * time.Second),
	}, 5, 10*time.Second, base.Add(8*time.Second+500*time.Millisecond))
	after := RecentEventHistogram([]time.Time{
		base.Add(1 * time.Second),
		base.Add(3 * time.Second),
		base.Add(9 * time.Second),
	}, 5, 10*time.Second, base.Add(9*time.Second+500*time.Millisecond))
	if !reflect.DeepEqual(after.Counts[:4], before.Counts[:4]) {
		t.Fatalf("new live event should not stack into past buckets:\nbefore=%v\nafter =%v", before.Counts, after.Counts)
	}
	if got, want := after.Counts[4], before.Counts[4]+1; got != want {
		t.Fatalf("new live event should stack only in the current bucket: got %d want %d counts=%v", got, want, after.Counts)
	}
}
