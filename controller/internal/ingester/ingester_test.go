package ingester

import (
	"context"
	"fmt"
	"log"
	"testing"

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

	if err := ing.ProcessObject(context.Background(), ObjectRef{Key: "device/run-1.pb", ETag: "etag", Size: int64(len(data))}); err != nil {
		t.Fatalf("ProcessObject: %v", err)
	}
	if len(pusher.pushes) != 1 {
		t.Fatalf("push count = %d, want 1", len(pusher.pushes))
	}
	assertSample(t, pusher.pushes[0].samples, MetricResultSuccess, map[string]string{"run_id": "run-1"}, 1)

	if err := ing.ProcessObject(context.Background(), ObjectRef{Key: "device/run-1.pb", ETag: "etag", Size: int64(len(data))}); err != nil {
		t.Fatalf("dedup ProcessObject: %v", err)
	}
	if len(pusher.pushes) != 1 {
		t.Fatalf("push count after duplicate = %d, want 1", len(pusher.pushes))
	}
}

func TestProcessObjectPushesDecodeFailureMetric(t *testing.T) {
	pusher := &fakePusher{}
	ing := New(testConfig(), &fakeStore{objects: map[string][]byte{"bad.pb": []byte("not protobuf")}}, pusher, log.New(testWriter{t}, "", 0))

	err := ing.ProcessObject(context.Background(), ObjectRef{Key: "bad.pb", ETag: "bad", Size: 12})
	if err == nil {
		t.Fatal("ProcessObject err = nil, want decode error")
	}
	if len(pusher.pushes) != 1 {
		t.Fatalf("push count = %d, want 1", len(pusher.pushes))
	}
	assertSample(t, pusher.pushes[0].samples, MetricIngestSuccess, map[string]string{"status": "decode_failed"}, 0)
}

func testConfig() Config {
	return Config{
		ListenAddr:     ":0",
		MinIOBucket:    "dropcheck",
		ObjectSuffix:   ".pb",
		PushgatewayURL: "http://pushgateway:9091",
		PushJob:        "dropcheck_festival_results",
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

func (s *fakeStore) ListObjects(context.Context) ([]ObjectRef, error) {
	var refs []ObjectRef
	for key, data := range s.objects {
		refs = append(refs, ObjectRef{Key: key, Size: int64(len(data))})
	}
	return refs, nil
}

type fakePusher struct {
	pushes []struct {
		key     string
		samples []MetricSample
	}
}

func (p *fakePusher) Push(_ context.Context, objectKey string, samples []MetricSample) error {
	p.pushes = append(p.pushes, struct {
		key     string
		samples []MetricSample
	}{key: objectKey, samples: samples})
	return nil
}

type testWriter struct {
	t *testing.T
}

func (w testWriter) Write(p []byte) (int, error) {
	w.t.Log(string(p))
	return len(p), nil
}
