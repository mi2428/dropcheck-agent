package ingester

import (
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"log"
	"runtime"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"
)

// BenchmarkBackfillScale uses synthetic keys and one representative five-step
// archive. Run with -benchtime=1x -benchmem; sampled_peak_B is not a hard RSS bound.
func BenchmarkBackfillScale(b *testing.B) {
	for _, count := range []int{1000, 10000, 100000} {
		for _, mode := range []string{"initial", "repeat", "retention", "restart", "failure"} {
			b.Run(fmt.Sprintf("%s/%d", mode, count), func(b *testing.B) {
				data, err := proto.Marshal(metricArchiveFixture())
				if err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				var cached, peak, gets, errorBytes int
				for n := 0; n < b.N; n++ {
					store := &generatedStore{count: count, data: data, fail: mode == "failure", sampleHeap: true}
					ing := New(testConfig(), store, discardPusher{}, log.New(io.Discard, "", 0))
					runtime.GC()
					var before runtime.MemStats
					runtime.ReadMemStats(&before)
					store.baseHeap = before.HeapAlloc
					batchErr := ing.ProcessBatch(context.Background())
					if mode == "failure" {
						if batchErr == nil {
							b.Fatal("missing synthetic failures")
						}
						errorBytes = len(batchErr.Error())
					} else if batchErr != nil {
						b.Fatal(batchErr)
					}
					if mode == "repeat" || mode == "retention" || mode == "restart" {
						if mode == "retention" {
							store.offset = count
						}
						if mode == "restart" {
							ing = New(testConfig(), store, discardPusher{}, log.New(io.Discard, "", 0))
						}
						if err := ing.ProcessBatch(context.Background()); err != nil {
							b.Fatal(err)
						}
					}
					cached = processedObjectCount(ing)
					store.sample()
					peak, gets = int(store.peakHeap), store.gets
					wantCached, wantGets := count, count
					if mode == "failure" {
						wantCached = 0
					}
					if mode == "retention" || mode == "restart" {
						wantGets *= 2
					}
					if cached != wantCached || gets != wantGets {
						b.Fatalf("cached=%d gets=%d, want cached=%d gets=%d", cached, gets, wantCached, wantGets)
					}
				}
				b.ReportMetric(float64(cached), "cached_objects")
				b.ReportMetric(float64(peak), "sampled_peak_B")
				b.ReportMetric(float64(gets), "get_calls")
				b.ReportMetric(float64(errorBytes), "error_bytes")
			})
		}
	}
}

func TestBackfillRetentionRemovesDeletedObjectSignatures(t *testing.T) {
	data, err := proto.Marshal(metricArchiveFixture())
	if err != nil {
		t.Fatal(err)
	}
	store := &generatedStore{count: 10, data: data}
	ing := New(testConfig(), store, discardPusher{}, log.New(io.Discard, "", 0))
	for cycle := 0; cycle < 3; cycle++ {
		store.offset = cycle * store.count
		if err := ing.ProcessBatch(context.Background()); err != nil {
			t.Fatal(err)
		}
		if got := processedObjectCount(ing); got != store.count {
			t.Fatalf("cycle=%d cached=%d, want retained=%d", cycle, got, store.count)
		}
		before := store.gets
		if err := ing.ProcessBatch(context.Background()); err != nil || store.gets != before {
			t.Fatalf("repeat fetched retained objects: gets=%d before=%d err=%v", store.gets, before, err)
		}
	}
}

func TestBackfillFailureSummaryIsBoundedAndFailuresRetry(t *testing.T) {
	store := &generatedStore{count: 1000, fail: true}
	ing := New(testConfig(), store, discardPusher{}, log.New(io.Discard, "", 0))
	for cycle := 1; cycle <= 2; cycle++ {
		err := ing.ProcessBatch(context.Background())
		if err == nil || !strings.Contains(err.Error(), "failed=1000 total=1000 (showing first 10 errors)") || len(err.Error()) > 1024 {
			t.Fatalf("unbounded or incomplete summary: %v", err)
		}
		if store.gets != cycle*store.count || processedObjectCount(ing) != 0 {
			t.Fatal("failed objects were cached or not retried")
		}
	}
}

func TestIncompleteOrCanceledListingDoesNotPrune(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		t.Run(fmt.Sprintf("canceled=%v", canceled), func(t *testing.T) {
			store := &orderedCountingStore{fakeStore: fakeStore{objects: map[string][]byte{
				"retained.pb": marshalOrderingArchive(t, timestampedArchive(2000, true)),
				"unseen.pb":   marshalOrderingArchive(t, timestampedArchive(1000, false)),
			}}, refs: []ObjectRef{{Bucket: "dropcheck", Key: "retained.pb", ETag: "retained"}, {Bucket: "dropcheck", Key: "unseen.pb", ETag: "unseen"}}}
			ing := New(testConfig(), store, discardPusher{}, log.New(io.Discard, "", 0))
			if err := ing.ProcessBatch(context.Background()); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			ing.store = &listingStore{ObjectStore: store, list: func(yield func(ObjectRef, error) bool) {
				if !yield(store.refs[0], nil) {
					return
				}
				if canceled {
					cancel()
					return
				}
				yield(ObjectRef{}, errors.New("synthetic listing failure"))
			}}
			if err := ing.ProcessBatch(ctx); err == nil || processedObjectCount(ing) != 2 {
				t.Fatalf("incomplete listing pruned cache: err=%v cached=%d", err, processedObjectCount(ing))
			}
		})
	}
}

func TestRetentionPruningPreservesConcurrentNotificationsAndLatestFence(t *testing.T) {
	store := &orderedCountingStore{fakeStore: fakeStore{objects: map[string][]byte{
		"new.pb":    marshalOrderingArchive(t, timestampedArchive(2000, true)),
		"old.pb":    marshalOrderingArchive(t, timestampedArchive(1000, false)),
		"notify.pb": marshalOrderingArchive(t, timestampedArchive(3000, true)),
	}}, refs: []ObjectRef{{Bucket: "dropcheck", Key: "new.pb", ETag: "new"}}}
	pusher := &fakePusher{}
	ing := New(testConfig(), store, pusher, log.New(io.Discard, "", 0))
	if err := ing.ProcessBatch(context.Background()); err != nil {
		t.Fatal(err)
	}
	notification := ObjectRef{Bucket: "dropcheck", Key: "notify.pb", ETag: "notify"}
	ing.store = &listingStore{ObjectStore: store, list: func(func(ObjectRef, error) bool) {
		done := make(chan struct{})
		go func() {
			defer close(done)
			if err := ing.ProcessObject(context.Background(), notification); err != nil {
				t.Error(err)
			}
		}()
		<-done
	}}
	if err := ing.ProcessBatch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if processedObjectCount(ing) != 1 || !ing.alreadyProcessed(notification) || ing.alreadyProcessed(store.refs[0]) {
		t.Fatal("pruning lost concurrent notification or retained deleted signature")
	}
	before := len(pusher.pushes)
	if err := ing.ProcessObject(context.Background(), ObjectRef{Bucket: "dropcheck", Key: "old.pb", ETag: "old"}); err != nil || len(pusher.pushes) != before {
		t.Fatalf("pruning lost latest metric fence: %v", err)
	}
}

type listingStore struct {
	ObjectStore
	list iter.Seq2[ObjectRef, error]
}

func (s *listingStore) ListObjects(context.Context) iter.Seq2[ObjectRef, error] { return s.list }

func processedObjectCount(ing *Ingester) int {
	count := 0
	ing.processed.Range(func(_, _ any) bool { count++; return true })
	return count
}

type generatedStore struct {
	count, offset, gets int
	data                []byte
	fail                bool
	sampleHeap          bool
	baseHeap, peakHeap  uint64
}

func (s *generatedStore) ListObjects(context.Context) iter.Seq2[ObjectRef, error] {
	return func(yield func(ObjectRef, error) bool) {
		for n := 0; n < s.count; n++ {
			object := ObjectRef{Bucket: "dropcheck", Key: fmt.Sprintf("synthetic/run-%09d.pb", s.offset+n), ETag: "synthetic", Size: int64(len(s.data))}
			if !yield(object, nil) {
				return
			}
		}
		s.sample()
	}
}

func (s *generatedStore) GetObject(context.Context, string) ([]byte, error) {
	s.gets++
	if s.gets%1024 == 0 {
		s.sample()
	}
	if s.fail {
		return nil, errors.New("synthetic fetch failure")
	}
	return s.data, nil
}

func (s *generatedStore) sample() {
	if !s.sampleHeap {
		return
	}
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	if stats.HeapAlloc > s.baseHeap && stats.HeapAlloc-s.baseHeap > s.peakHeap {
		s.peakHeap = stats.HeapAlloc - s.baseHeap
	}
}

type discardPusher struct{}

func (discardPusher) Push(context.Context, MetricBatch) error { return nil }
