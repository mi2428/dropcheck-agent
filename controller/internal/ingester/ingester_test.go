package ingester

import (
	"context"
	"fmt"
	"iter"
	"log"
	"sync"
	"testing"

	"dropcheck/controller/internal/controlpb"
	"google.golang.org/protobuf/proto"
)

func TestProcessObjectParsesArchiveAndPushesMetrics(t *testing.T) {
	archive := metricArchiveFixture()
	data, err := proto.Marshal(archive)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	pusher := &fakePusher{}
	ing := New(testConfig(), &fakeStore{objects: map[string][]byte{"device/run-1.pb": data}}, pusher, log.New(testWriter{t}, "", 0))

	if err := ing.ProcessObject(context.Background(), ObjectRef{Bucket: "dropcheck", Key: "device/run-1.pb", ETag: "etag", Size: int64(len(data))}); err != nil {
		t.Fatalf("ProcessObject: %v", err)
	}
	if len(pusher.pushes) != 1 {
		t.Fatalf("push count = %d, want 1", len(pusher.pushes))
	}
	assertGrouping(t, pusher.pushes[0].batch, map[string]string{
		"festa":      "smoke",
		"wifi_group": "lab",
	})
	assertSample(t, pusher.pushes[0].batch, MetricSuccess, nil, 1)

	if err := ing.ProcessObject(context.Background(), ObjectRef{Bucket: "dropcheck", Key: "device/run-1.pb", ETag: "etag", Size: int64(len(data))}); err != nil {
		t.Fatalf("dedup ProcessObject: %v", err)
	}
	if len(pusher.pushes) != 1 {
		t.Fatalf("push count after duplicate = %d, want 1", len(pusher.pushes))
	}
}

func TestProcessObjectReturnsDecodeFailureWithoutPushingMetrics(t *testing.T) {
	pusher := &fakePusher{}
	ing := New(testConfig(), &fakeStore{objects: map[string][]byte{"bad.pb": []byte("not protobuf")}}, pusher, log.New(testWriter{t}, "", 0))

	err := ing.ProcessObject(context.Background(), ObjectRef{Bucket: "dropcheck", Key: "bad.pb", ETag: "bad", Size: 12})
	if err == nil {
		t.Fatal("ProcessObject err = nil, want decode error")
	}
	if len(pusher.pushes) != 0 {
		t.Fatalf("push count = %d, want 0", len(pusher.pushes))
	}
}

func TestProcessObjectKeepsNewestArchiveAcrossArrivalAndBackfillOrder(t *testing.T) {
	newer, older := timestampedArchive(2000, true), timestampedArchive(1000, false)
	objects := make(map[string][]byte)
	for key, archive := range map[string]*controlpb.StandaloneRunArchive{"new.pb": newer, "old.pb": older} {
		data, err := proto.Marshal(archive)
		if err != nil {
			t.Fatal(err)
		}
		objects[key] = data
	}
	for _, reverse := range []bool{false, true} {
		pusher := &fakePusher{}
		ing := New(testConfig(), &fakeStore{objects: objects}, pusher, log.New(testWriter{t}, "", 0))
		keys := []string{"old.pb", "new.pb"}
		if reverse {
			keys = []string{"new.pb", "old.pb"}
		}
		for _, key := range keys {
			if err := ing.ProcessObject(context.Background(), ObjectRef{Bucket: "dropcheck", Key: key, ETag: key, Size: int64(len(objects[key]))}); err != nil {
				t.Fatal(err)
			}
		}
		if got := lastSuccessValue(pusher); got != 1 {
			t.Fatalf("reverse=%v last success = %v, want newer value 1", reverse, got)
		}
	}
	pusher := &fakePusher{}
	ing := New(testConfig(), &fakeStore{objects: objects}, pusher, log.New(testWriter{t}, "", 0))
	if err := ing.ProcessBatch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := lastSuccessValue(pusher); got != 1 {
		t.Fatalf("backfill last success = %v, want newer value 1", got)
	}
}

func TestProcessObjectConcurrentNewerAndOlder(t *testing.T) {
	newer, older := timestampedArchive(2000, true), timestampedArchive(1000, false)
	objects := make(map[string][]byte)
	for key, archive := range map[string]*controlpb.StandaloneRunArchive{"new.pb": newer, "old.pb": older} {
		data, err := proto.Marshal(archive)
		if err != nil {
			t.Fatal(err)
		}
		objects[key] = data
	}
	pusher := &fakePusher{}
	ing := New(testConfig(), &fakeStore{objects: objects}, pusher, log.New(testWriter{t}, "", 0))
	var wg sync.WaitGroup
	for _, key := range []string{"new.pb", "old.pb"} {
		wg.Add(1)
		go func(key string) {
			defer wg.Done()
			if err := ing.ProcessObject(context.Background(), ObjectRef{Bucket: "dropcheck", Key: key, ETag: key, Size: int64(len(objects[key]))}); err != nil {
				t.Errorf("ProcessObject(%s): %v", key, err)
			}
		}(key)
	}
	wg.Wait()
	if got := lastSuccessValue(pusher); got != 1 {
		t.Fatalf("last success = %v, want newer value 1", got)
	}
}

func timestampedArchive(finished int64, succeeded bool) *controlpb.StandaloneRunArchive {
	archive := metricArchiveFixture()
	archive.Summary.FinishedUnixMs = finished
	status := controlpb.CommandResult_STATUS_FAILED
	if succeeded {
		status = controlpb.CommandResult_STATUS_OK
	}
	for _, step := range archive.Steps {
		step.Result.Status = status
	}
	return archive
}

func lastSuccessValue(pusher *fakePusher) float64 {
	for n := len(pusher.pushes) - 1; n >= 0; n-- {
		for _, sample := range pusher.pushes[n].batch.Samples {
			if sample.Name == MetricSuccess {
				return sample.Value
			}
		}
	}
	return -1
}

func testConfig() Config {
	return Config{
		ListenAddr:     ":0",
		WebhookToken:   "synthetic-webhook-token",
		MinIOBucket:    "dropcheck",
		ObjectSuffix:   ".pb",
		PushgatewayURL: "http://pushgateway:9091",
		PushJob:        "dropcheck_results",
	}
}

type fakeStore struct {
	objects map[string][]byte
}

func (s *fakeStore) GetObject(_ context.Context, key string) ([]byte, error) {
	data, ok := s.objects[key]
	if !ok {
		return nil, fmt.Errorf("missing object %s", key)
	}
	return data, nil
}

func (s *fakeStore) ListObjects(context.Context) iter.Seq2[ObjectRef, error] {
	return func(yield func(ObjectRef, error) bool) {
		for key, data := range s.objects {
			if !yield(ObjectRef{Bucket: "dropcheck", Key: key, Size: int64(len(data))}, nil) {
				return
			}
		}
	}
}

type fakePusher struct {
	pushes []struct {
		batch MetricBatch
	}
}

func (p *fakePusher) Push(_ context.Context, batch MetricBatch) error {
	p.pushes = append(p.pushes, struct {
		batch MetricBatch
	}{batch: batch})
	return nil
}

type testWriter struct {
	t *testing.T
}

func (w testWriter) Write(p []byte) (int, error) {
	w.t.Log(string(p))
	return len(p), nil
}
