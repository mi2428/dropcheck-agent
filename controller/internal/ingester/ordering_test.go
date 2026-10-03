package ingester

import (
	"context"
	"errors"
	"io"
	"log"
	"strings"
	"sync"
	"testing"

	"dropcheck/controller/internal/controlpb"
	"google.golang.org/protobuf/proto"
)

func TestArchiveOrderingPolicy(t *testing.T) {
	for _, tc := range []struct {
		name   string
		keys   []string
		times  []int64
		starts []int64
		pushes int
	}{
		{"undated_before_known", []string{"legacy.pb", "known.pb"}, []int64{0, 2000}, nil, 2},
		{"undated_after_known", []string{"known.pb", "legacy.pb"}, []int64{2000, 0}, nil, 1},
		{"legacy_updates", []string{"first.pb", "second.pb"}, []int64{0, 0}, nil, 2},
		{"started_fallback", []string{"new.pb", "old.pb"}, []int64{0, 0}, []int64{2000, 1000}, 1},
		{"negative_is_undated", []string{"known.pb", "legacy.pb"}, []int64{2000, -1}, []int64{0, -1}, 1},
		{"tie_ascending_key", []string{"a.pb", "z.pb"}, []int64{2000, 2000}, nil, 2},
		{"tie_descending_key", []string{"z.pb", "a.pb"}, []int64{2000, 2000}, nil, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeStore{objects: make(map[string][]byte)}
			for index, key := range tc.keys {
				archive := timestampedArchive(tc.times[index], key != "a.pb" && key != "legacy.pb" && key != "old.pb")
				archive.Summary.StartedUnixMs = 0
				if tc.starts != nil {
					archive.Summary.StartedUnixMs = tc.starts[index]
				}
				store.objects[key] = marshalOrderingArchive(t, archive)
			}
			pusher := &fakePusher{}
			ing := New(testConfig(), store, pusher, log.New(io.Discard, "", 0))
			for _, key := range tc.keys {
				if err := ing.ProcessObject(context.Background(), ObjectRef{Key: key, ETag: key}); err != nil {
					t.Fatal(err)
				}
			}
			if len(pusher.pushes) != tc.pushes || lastSuccessValue(pusher) != 1 {
				t.Fatalf("pushes=%d last_success=%v, want pushes=%d last_success=1", len(pusher.pushes), lastSuccessValue(pusher), tc.pushes)
			}
		})
	}
}

func TestSameKeyChangedSignatureAndTimestampTieCanUpdate(t *testing.T) {
	store := &fakeStore{objects: map[string][]byte{"same.pb": marshalOrderingArchive(t, timestampedArchive(2000, false))}}
	pusher := &fakePusher{}
	ing := New(testConfig(), store, pusher, log.New(io.Discard, "", 0))
	if err := ing.ProcessObject(context.Background(), ObjectRef{Key: "same.pb", ETag: "first"}); err != nil {
		t.Fatal(err)
	}
	store.objects["same.pb"] = marshalOrderingArchive(t, timestampedArchive(2000, true))
	if err := ing.ProcessObject(context.Background(), ObjectRef{Key: "same.pb", ETag: "changed"}); err != nil {
		t.Fatal(err)
	}
	if len(pusher.pushes) != 2 || lastSuccessValue(pusher) != 1 {
		t.Fatal("same-key correction with equal timestamp was not pushed")
	}
}

func TestConcurrentNotificationAndBackfillKeepLatest(t *testing.T) {
	store := &fakeStore{objects: map[string][]byte{
		"old.pb": marshalOrderingArchive(t, timestampedArchive(1000, false)),
		"new.pb": marshalOrderingArchive(t, timestampedArchive(2000, true)),
	}}
	pusher := &fakePusher{}
	ing := New(testConfig(), store, pusher, log.New(io.Discard, "", 0))
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		if err := ing.ProcessBatch(context.Background()); err != nil {
			t.Error(err)
		}
	}()
	go func() {
		defer wg.Done()
		if err := ing.ProcessObject(context.Background(), ObjectRef{Key: "new.pb", ETag: "new"}); err != nil {
			t.Error(err)
		}
	}()
	wg.Wait()
	if lastSuccessValue(pusher) != 1 {
		t.Fatal("concurrent backfill overwrote newer notification")
	}
}

func TestRestartBackfillOrderAndFetchCount(t *testing.T) {
	for _, keys := range [][]string{
		{"legacy.pb", "a.pb", "z.pb", "old.pb"},
		{"z.pb", "old.pb", "a.pb", "legacy.pb"},
	} {
		store := &orderedCountingStore{fakeStore: fakeStore{objects: make(map[string][]byte)}}
		for key, archive := range map[string]*controlpb.StandaloneRunArchive{
			"legacy.pb": timestampedArchive(0, false),
			"old.pb":    timestampedArchive(1000, false),
			"a.pb":      timestampedArchive(2000, false),
			"z.pb":      timestampedArchive(2000, true),
		} {
			archive.Summary.StartedUnixMs = 0
			store.objects[key] = marshalOrderingArchive(t, archive)
		}
		for _, key := range keys {
			store.refs = append(store.refs, ObjectRef{Key: key, ETag: key})
		}
		pusher := &fakePusher{}
		for restart := 0; restart < 2; restart++ {
			ing := New(testConfig(), store, pusher, log.New(io.Discard, "", 0))
			before := store.gets
			if err := ing.ProcessBatch(context.Background()); err != nil {
				t.Fatal(err)
			}
			if store.gets-before != len(keys) || lastSuccessValue(pusher) != 1 {
				t.Fatalf("restart=%d gets=%d success=%v, want gets=%d success=1", restart, store.gets-before, lastSuccessValue(pusher), len(keys))
			}
			if err := ing.ProcessBatch(context.Background()); err != nil {
				t.Fatal(err)
			}
			if store.gets-before != len(keys) {
				t.Fatal("unchanged batch performed additional GETs")
			}
		}
	}
}

func TestPartialPushRetryPreservesSuccessfulGroupFence(t *testing.T) {
	archive := timestampedArchive(2000, true)
	second := proto.Clone(archive.Steps[0]).(*controlpb.StandaloneMeasurementStep)
	second.WifiGroupName = "second"
	archive.Steps = append(archive.Steps, second)
	store := &fakeStore{objects: map[string][]byte{
		"new.pb": marshalOrderingArchive(t, archive),
		"old.pb": marshalOrderingArchive(t, timestampedArchive(1000, false)),
	}}
	pusher := &partialFailurePusher{fail: true, calls: make(map[string]int)}
	ing := New(testConfig(), store, pusher, log.New(io.Discard, "", 0))
	object := ObjectRef{Key: "new.pb", ETag: "new"}
	if err := ing.ProcessObject(context.Background(), object); err == nil {
		t.Fatal("partial push failure was not returned")
	}
	if ing.alreadyProcessed(object) {
		t.Fatal("partially pushed archive was marked processed")
	}
	if err := ing.ProcessObject(context.Background(), ObjectRef{Key: "old.pb", ETag: "old"}); err != nil {
		t.Fatal(err)
	}
	if pusher.calls["lab"] != 1 {
		t.Fatal("older archive overwrote the successfully pushed group")
	}
	pusher.fail = false
	if err := ing.ProcessObject(context.Background(), object); err != nil {
		t.Fatal(err)
	}
	if !ing.alreadyProcessed(object) || pusher.calls["second"] != 2 {
		t.Fatalf("retry did not complete failed group: calls=%v", pusher.calls)
	}
	before := len(pusher.pushes)
	if err := ing.ProcessObject(context.Background(), object); err != nil || len(pusher.pushes) != before {
		t.Fatal("successfully retried archive was not deduplicated")
	}
}

func TestBatchReturnsFetchAndDecodeErrorsWithoutPrefetch(t *testing.T) {
	store := &orderedCountingStore{
		fakeStore: fakeStore{objects: map[string][]byte{"bad.pb": []byte("bad protobuf")}},
		refs:      []ObjectRef{{Key: "missing.pb", ETag: "missing"}, {Key: "bad.pb", ETag: "bad"}},
	}
	ing := New(testConfig(), store, &fakePusher{}, log.New(io.Discard, "", 0))
	err := ing.ProcessBatch(context.Background())
	if err == nil || !strings.Contains(err.Error(), "fetch object") || !strings.Contains(err.Error(), "decode standalone archive") || store.gets != 2 {
		t.Fatalf("error=%v gets=%d, want both errors and exactly two GETs", err, store.gets)
	}
}

func marshalOrderingArchive(t *testing.T, archive *controlpb.StandaloneRunArchive) []byte {
	t.Helper()
	data, err := proto.Marshal(archive)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

type orderedCountingStore struct {
	fakeStore
	refs []ObjectRef
	gets int
}

func (s *orderedCountingStore) ListObjects(context.Context) ([]ObjectRef, error) {
	return s.refs, nil
}

func (s *orderedCountingStore) GetObject(ctx context.Context, key string) ([]byte, error) {
	s.gets++
	return s.fakeStore.GetObject(ctx, key)
}

type partialFailurePusher struct {
	fakePusher
	fail  bool
	calls map[string]int
}

func (p *partialFailurePusher) Push(ctx context.Context, batch MetricBatch) error {
	group := batch.Grouping["wifi_group"]
	p.calls[group]++
	if p.fail && group == "second" {
		return errors.New("synthetic push failure")
	}
	return p.fakePusher.Push(ctx, batch)
}
