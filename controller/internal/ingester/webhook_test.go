package ingester

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const validNotification = `{"Records":[{"eventName":"s3:ObjectCreated:Put","s3":{"bucket":{"name":"dropcheck"},"object":{"key":"incoming%2Frun.pb","eTag":"synthetic","size":12}}}]}`

func TestWebhookRejectsBeforeFetchingOrPushing(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		headers    []string
		status     int
	}{
		{"missing_auth", validNotification, nil, 401},
		{"auth_before_json", `not JSON`, nil, 401},
		{"wrong_auth", validNotification, []string{"Bearer wrong-token"}, 401},
		{"raw_token", validNotification, []string{"synthetic-webhook-token"}, 401},
		{"wrong_scheme", validNotification, []string{"Basic synthetic-webhook-token"}, 401},
		{"extra_space", validNotification, []string{"Bearer  synthetic-webhook-token"}, 401},
		{"duplicate_auth", validNotification, []string{"Bearer synthetic-webhook-token", "Bearer synthetic-webhook-token"}, 401},
		{"comma_auth", validNotification, []string{"Bearer synthetic-webhook-token, Bearer wrong-token"}, 401},
		{"wrong_bucket", strings.ReplaceAll(validNotification, `"dropcheck"`, `"other"`), nil, 400},
		{"empty_bucket", strings.ReplaceAll(validNotification, `"dropcheck"`, `""`), nil, 400},
		{"wrong_bucket_ignored_suffix", strings.ReplaceAll(strings.ReplaceAll(validNotification, `"dropcheck"`, `"other"`), "run.pb", "run.txt"), nil, 400},
		{"wrong_bucket_removed", strings.ReplaceAll(strings.ReplaceAll(validNotification, `"dropcheck"`, `"other"`), "s3:ObjectCreated:Put", "s3:ObjectRemoved:Delete"), nil, 400},
		{"missing_key", strings.ReplaceAll(validNotification, "incoming%2Frun.pb", ""), nil, 400},
		{"key_too_long", strings.ReplaceAll(validNotification, "incoming%2Frun.pb", strings.Repeat("x", 1024)+".pb"), nil, 400},
		{"bad_key_escape", strings.ReplaceAll(validNotification, "incoming%2Frun.pb", "bad%XX.pb"), nil, 400},
		{"control_key", strings.ReplaceAll(validNotification, "incoming%2Frun.pb", "bad%00.pb"), nil, 400},
		{"invalid_utf8_key", strings.ReplaceAll(validNotification, "incoming%2Frun.pb", "%FF.pb"), nil, 400},
		{"negative_size", strings.ReplaceAll(validNotification, `"size":12`, `"size":-1`), nil, 400},
		{"missing_size", strings.ReplaceAll(validNotification, `,"size":12`, ""), nil, 400},
		{"null_size", strings.ReplaceAll(validNotification, `"size":12`, `"size":null`), nil, 400},
		{"object_over_limit", strings.ReplaceAll(validNotification, `"size":12`, `"size":67108865`), nil, 400},
		{"missing_event", strings.ReplaceAll(validNotification, "s3:ObjectCreated:Put", ""), nil, 400},
		{"unknown_created_event", strings.ReplaceAll(validNotification, "s3:ObjectCreated:Put", "s3:ObjectCreated:Unexpected"), nil, 400},
		{"malformed", `{"Records":[`, nil, 400},
		{"empty_records", `{"Records":[]}`, nil, 400},
		{"null", `null`, nil, 400},
		{"trailing_json", validNotification + `{}`, nil, 400},
		{"oversized", validNotification + strings.Repeat(" ", notificationBodyLimit), nil, 413},
		{"valid_then_invalid", strings.TrimSuffix(validNotification, "]}") + `,{"eventName":"s3:ObjectCreated:Put"}]}`, nil, 400},
		{"valid_then_wrong_bucket", strings.TrimSuffix(validNotification, "]}") + "," + strings.TrimPrefix(strings.ReplaceAll(validNotification, `"dropcheck"`, `"other"`), `{"Records":[`), nil, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &orderedCountingStore{}
			pusher := &fakePusher{}
			var logs bytes.Buffer
			ing := New(testConfig(), store, pusher, log.New(&logs, "", 0))
			req := httptest.NewRequest(http.MethodPost, "/minio/events", strings.NewReader(tc.body))
			headers := tc.headers
			if tc.status != 401 {
				headers = []string{"Bearer synthetic-webhook-token"}
			}
			if tc.name == "oversized" {
				req.ContentLength = -1 // Exercise the streaming limit, not the length precheck.
			}
			for _, header := range headers {
				req.Header.Add("Authorization", header)
			}
			rec := httptest.NewRecorder()
			ing.Handler().ServeHTTP(rec, req)
			if rec.Code != tc.status || store.gets != 0 || len(pusher.pushes) != 0 {
				t.Fatalf("status=%d gets=%d pushes=%d, want status=%d with no I/O: %s", rec.Code, store.gets, len(pusher.pushes), tc.status, rec.Body.String())
			}
			if strings.Contains(logs.String()+rec.Body.String(), "synthetic-webhook-token") {
				t.Fatal("credential leaked into diagnostics")
			}
		})
	}
}

func TestWebhookAcceptsAuthenticatedNotificationAndPreservesFilters(t *testing.T) {
	for _, key := range []string{"incoming%2Frun.pb", "other%2Frun.pb", "incoming%2Frun.txt"} {
		t.Run(key, func(t *testing.T) {
			cfg := testConfig()
			cfg.MinIOPrefix = "incoming"
			store := &orderedCountingStore{fakeStore: fakeStore{objects: map[string][]byte{
				"incoming/run.pb": marshalOrderingArchive(t, metricArchiveFixture()),
			}}}
			pusher := &fakePusher{}
			ing := New(cfg, store, pusher, log.New(io.Discard, "", 0))
			req := httptest.NewRequest(http.MethodPost, "/minio/events", strings.NewReader(strings.ReplaceAll(validNotification, "incoming%2Frun.pb", key)))
			req.Header.Set("Authorization", "Bearer "+cfg.WebhookToken)
			rec := httptest.NewRecorder()
			ing.Handler().ServeHTTP(rec, req)
			want := 0
			if key == "incoming%2Frun.pb" {
				want = 1
			}
			if rec.Code != 202 || store.gets != want || len(pusher.pushes) != want {
				t.Fatalf("status=%d gets=%d pushes=%d, want 202 and %d I/O calls", rec.Code, store.gets, len(pusher.pushes), want)
			}
		})
	}
}

func TestWebhookMixedRecordsPreserveLargeNonArchiveFilters(t *testing.T) {
	for _, tc := range []struct {
		name, bucket, key, event string
		size                     int64
		status, calls            int
	}{
		{"large_other_prefix", "dropcheck", "other/large.pb", "s3:ObjectCreated:Put", 80 << 20, 202, 1},
		{"large_other_suffix", "dropcheck", "incoming/ignored.bin", "s3:ObjectCreated:Put", 80 << 20, 202, 1},
		{"large_removed", "dropcheck", "incoming/removed.pb", "s3:ObjectRemoved:Delete", 80 << 20, 202, 1},
		{"wrong_bucket_other_prefix", "other", "other/large.pb", "s3:ObjectCreated:Put", 80 << 20, 400, 0},
		{"wrong_bucket_other_suffix", "other", "incoming/ignored.bin", "s3:ObjectCreated:Put", 80 << 20, 400, 0},
		{"negative_other_prefix", "dropcheck", "other/large.pb", "s3:ObjectCreated:Put", -1, 400, 0},
		{"negative_other_suffix", "dropcheck", "incoming/ignored.bin", "s3:ObjectCreated:Put", -1, 400, 0},
		{"oversized_archive", "dropcheck", "incoming/large.pb", "s3:ObjectCreated:Put", 80 << 20, 400, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := testConfig()
			cfg.MinIOPrefix = "incoming"
			store := &orderedCountingStore{fakeStore: fakeStore{objects: map[string][]byte{
				"incoming/run.pb": marshalOrderingArchive(t, metricArchiveFixture()),
			}}}
			pusher := &fakePusher{}
			ing := New(cfg, store, pusher, log.New(io.Discard, "", 0))
			record := fmt.Sprintf(`{"eventName":%q,"s3":{"bucket":{"name":%q},"object":{"key":%q,"size":%d}}}`, tc.event, tc.bucket, tc.key, tc.size)
			body := strings.TrimSuffix(validNotification, "]}") + "," + record + "]}"
			req := httptest.NewRequest(http.MethodPost, "/minio/events", strings.NewReader(body))
			req.Header.Set("Authorization", "Bearer "+cfg.WebhookToken)
			rec := httptest.NewRecorder()
			ing.Handler().ServeHTTP(rec, req)
			if rec.Code != tc.status || store.gets != tc.calls || len(pusher.pushes) != tc.calls {
				t.Fatalf("status=%d gets=%d pushes=%d, want status=%d calls=%d", rec.Code, store.gets, len(pusher.pushes), tc.status, tc.calls)
			}
		})
	}
}

func TestProcessObjectAndBatchIgnoreLargeNonArchives(t *testing.T) {
	for _, key := range []string{"other/large.pb", "incoming/ignored.bin"} {
		cfg := testConfig()
		cfg.MinIOPrefix = "incoming"
		object := ObjectRef{Bucket: cfg.MinIOBucket, Key: key, Size: 80 << 20}
		store := &orderedCountingStore{refs: []ObjectRef{object}}
		pusher := &fakePusher{}
		ing := New(cfg, store, pusher, log.New(io.Discard, "", 0))
		if err := ing.ProcessObject(context.Background(), object); err != nil {
			t.Fatalf("filtered ProcessObject: %v", err)
		}
		if err := ing.ProcessBatch(context.Background()); err != nil {
			t.Fatalf("filtered ProcessBatch: %v", err)
		}
		if store.gets != 0 || len(pusher.pushes) != 0 {
			t.Fatal("large non-archive caused I/O")
		}
	}
}

func TestWebhookConfigurationFailsClosedWithoutDisclosingToken(t *testing.T) {
	for _, token := range []string{"", " ", " padded ", "Bearer two-parts", "line\nbreak", "invalid:token", "=", "ab=c"} {
		t.Run(fmt.Sprintf("length_%d", len(token)), func(t *testing.T) {
			t.Setenv("DROPCHECK_INGESTER_WEBHOOK_TOKEN", token)
			if _, err := ConfigFromEnv(); err == nil || (strings.TrimSpace(token) != "" && strings.Contains(err.Error(), token)) {
				t.Fatalf("invalid token was accepted or disclosed")
			}
			cfg := testConfig()
			cfg.WebhookToken = token
			store := &orderedCountingStore{}
			ing := New(cfg, store, &fakePusher{}, log.New(io.Discard, "", 0))
			if err := ing.Run(context.Background()); err == nil {
				t.Fatal("Run accepted invalid configuration")
			}
			rec := httptest.NewRecorder()
			ing.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/minio/events", strings.NewReader(validNotification)))
			if rec.Code != 503 || store.gets != 0 {
				t.Fatalf("status=%d gets=%d, want 503 and 0", rec.Code, store.gets)
			}
		})
	}
	t.Setenv("DROPCHECK_INGESTER_WEBHOOK_TOKEN", "single-token_+/~.==")
	if _, err := ConfigFromEnv(); err != nil {
		t.Fatal(err)
	}
}

func TestProcessObjectAndBatchRejectInvalidReferences(t *testing.T) {
	for _, object := range []ObjectRef{
		{Key: "incoming/run.pb"},
		{Bucket: "other", Key: "incoming/run.pb"},
		{Bucket: "other", Key: "ignored.txt"},
		{Bucket: "dropcheck"},
		{Bucket: "dropcheck", Key: "bad\x00.pb"},
		{Bucket: "dropcheck", Key: "incoming/run.pb", Size: -1},
		{Bucket: "dropcheck", Key: "incoming/run.pb", Size: defaultMaxObjectBytes + 1},
	} {
		store := &orderedCountingStore{refs: []ObjectRef{object}}
		pusher := &fakePusher{}
		ing := New(testConfig(), store, pusher, log.New(io.Discard, "", 0))
		if err := ing.ProcessObject(context.Background(), object); err == nil {
			t.Fatalf("ProcessObject accepted invalid reference: %+v", object)
		}
		if err := ing.ProcessBatch(context.Background()); err == nil {
			t.Fatalf("ProcessBatch accepted invalid reference: %+v", object)
		}
		if store.gets != 0 || len(pusher.pushes) != 0 {
			t.Fatal("invalid reference caused I/O")
		}
	}
	cfg := testConfig()
	cfg.MinIOBucket = ""
	ing := New(cfg, &orderedCountingStore{}, &fakePusher{}, log.New(io.Discard, "", 0))
	if err := ing.ProcessObject(context.Background(), ObjectRef{Key: "run.pb"}); err == nil {
		t.Fatal("empty configured bucket was accepted")
	}
}

func TestWebhookProcessingHonorsDeadlineBehindActiveBackfillPush(t *testing.T) {
	data := marshalOrderingArchive(t, metricArchiveFixture())
	store := &atomicCountingStore{fakeStore: fakeStore{objects: map[string][]byte{
		"backfill.pb":     data,
		"incoming/run.pb": marshalOrderingArchive(t, metricArchiveFixture()),
	}}}
	pusher := &blockingPusher{started: make(chan struct{})}
	batchStore := &listingStore{ObjectStore: store, list: func(yield func(ObjectRef, error) bool) {
		yield(ObjectRef{Bucket: "dropcheck", Key: "backfill.pb", ETag: "backfill"}, nil)
	}}
	ing := New(testConfig(), batchStore, pusher, log.New(io.Discard, "", 0))
	batchCtx, stopBatch := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- ing.ProcessBatch(batchCtx) }()
	t.Cleanup(func() {
		stopBatch()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("backfill did not stop")
		}
	})
	select {
	case <-pusher.started:
	case <-time.After(time.Second):
		t.Fatal("backfill did not reach Push")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	req := httptest.NewRequest(http.MethodPost, "/minio/events", strings.NewReader(validNotification)).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer synthetic-webhook-token")
	rec := httptest.NewRecorder()
	start := time.Now()
	ing.Handler().ServeHTTP(rec, req)
	if rec.Code != 504 || pusher.calls.Load() != 1 || time.Since(start) > time.Second {
		t.Fatalf("status=%d pushes=%d elapsed=%s, want bounded 504 with only the active backfill push", rec.Code, pusher.calls.Load(), time.Since(start))
	}
}

type blockingPusher struct {
	started chan struct{}
	calls   atomic.Int32
}

func TestWebhookDeadlineCancelsFetchAndPush(t *testing.T) {
	for _, blockFetch := range []bool{true, false} {
		t.Run(fmt.Sprintf("block_fetch=%v", blockFetch), func(t *testing.T) {
			store := &cancelingStore{fakeStore: fakeStore{objects: map[string][]byte{
				"incoming/run.pb": marshalOrderingArchive(t, metricArchiveFixture()),
			}}, block: blockFetch}
			pusher := &blockingPusher{started: make(chan struct{})}
			ing := New(testConfig(), store, pusher, log.New(io.Discard, "", 0))
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()
			req := httptest.NewRequest(http.MethodPost, "/minio/events", strings.NewReader(validNotification)).WithContext(ctx)
			req.Header.Set("Authorization", "Bearer synthetic-webhook-token")
			rec := httptest.NewRecorder()
			start := time.Now()
			ing.Handler().ServeHTTP(rec, req)
			wantPushes := int32(1)
			if blockFetch {
				wantPushes = 0
			}
			if rec.Code != 504 || pusher.calls.Load() != wantPushes || time.Since(start) > time.Second {
				t.Fatalf("status=%d pushes=%d elapsed=%s", rec.Code, pusher.calls.Load(), time.Since(start))
			}
		})
	}
}

type cancelingStore struct {
	fakeStore
	block bool
}

func (s *cancelingStore) GetObject(ctx context.Context, key string) ([]byte, error) {
	if s.block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return s.fakeStore.GetObject(ctx, key)
}

func (p *blockingPusher) Push(ctx context.Context, _ MetricBatch) error {
	if p.calls.Add(1) == 1 {
		close(p.started)
	}
	<-ctx.Done()
	return ctx.Err()
}

func TestWebhookRunBoundsSlowBodyAndKeepsReadiness(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	_ = listener.Close()
	cfg := testConfig()
	cfg.ListenAddr, cfg.BatchInterval = addr, time.Hour
	store := &atomicCountingStore{}
	ing := New(cfg, store, discardPusher{}, log.New(io.Discard, "", 0))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- ing.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil && !errors.Is(err, context.Canceled) {
				t.Error(err)
			}
		case <-time.After(12 * time.Second):
			t.Error("server did not stop")
		}
	})
	client := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(3 * time.Second)
	for {
		resp, err := client.Get("http://" + addr + "/healthz")
		if err == nil {
			_ = resp.Body.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("server did not become ready")
		}
		time.Sleep(10 * time.Millisecond)
	}
	conn, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(notificationReadTimeout + 2*time.Second))
	start := time.Now()
	_, err = fmt.Fprintf(conn, "POST /minio/events HTTP/1.1\r\nHost: synthetic\r\nAuthorization: Bearer %s\r\nContent-Length: 100\r\nConnection: close\r\n\r\n{", cfg.WebhookToken)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/healthz", "/minio/events"} {
		resp, err := client.Get("http://" + addr + path)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("readiness status=%d", resp.StatusCode)
		}
	}
	for _, tc := range []struct {
		body   string
		status int
	}{{`{"Records":[`, 400}, {validNotification + strings.Repeat(" ", notificationBodyLimit), 413}} {
		req, err := http.NewRequest(http.MethodPost, "http://"+addr+"/minio/events", strings.NewReader(tc.body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+cfg.WebhookToken)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != tc.status {
			t.Fatalf("body status=%d, want %d", resp.StatusCode, tc.status)
		}
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 408 || time.Since(start) > notificationReadTimeout+time.Second || store.gets.Load() != 0 {
		t.Fatalf("status=%d elapsed=%s gets=%d, want bounded 408 and no fetch", resp.StatusCode, time.Since(start), store.gets.Load())
	}
}

type atomicCountingStore struct {
	fakeStore
	gets atomic.Int32
}

func (s *atomicCountingStore) GetObject(ctx context.Context, key string) ([]byte, error) {
	s.gets.Add(1)
	return s.fakeStore.GetObject(ctx, key)
}
