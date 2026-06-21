//go:build integration

package ingester_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"dropcheck/controller/internal/controlpb"
	core "dropcheck/controller/internal/ingester"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"google.golang.org/protobuf/proto"
)

const (
	minIOAccessKey = "dropcheck"
	minIOSecretKey = "dropcheck-secret"
)

var (
	minIOEndpoint string
	minIOSkip     string
	minIOCleanup  func()
)

func TestMain(m *testing.M) {
	if endpoint := strings.TrimSpace(os.Getenv("DROPCHECK_INGESTER_INTEGRATION_MINIO_ENDPOINT")); endpoint != "" {
		minIOEndpoint = endpoint
		os.Exit(m.Run())
	}

	service, err := startMinIOContainer()
	if err != nil {
		minIOSkip = err.Error()
		os.Exit(m.Run())
	}
	minIOEndpoint = service.endpoint
	minIOCleanup = service.close
	code := m.Run()
	minIOCleanup()
	os.Exit(code)
}

func TestBatchBackfillReadsMinIOAndPushesHarnessMetrics(t *testing.T) {
	env := newIntegrationEnv(t, "batch")
	archive := standaloneArchiveFixture("run-batch-1", "ok")
	env.putArchive(t, "incoming/device/run-batch-1.pb", archive)
	env.putObject(t, "incoming/device/ignored.txt", []byte("not a protobuf"))
	env.putArchive(t, "other/device/run-batch-2.pb", standaloneArchiveFixture("run-batch-2", "ok"))

	ing := env.newIngester(t, "incoming")
	if err := ing.ProcessBatch(context.Background()); err != nil {
		t.Fatalf("ProcessBatch: %v", err)
	}

	pushes := env.pushgateway.requests()
	if len(pushes) != 1 {
		t.Fatalf("push requests = %d, want 1", len(pushes))
	}
	assertPushPathGrouping(t, pushes[0].Path, map[string]string{
		"job":          "dropcheck_ingester_integration",
		"device_name":  "phone1",
		"device_model": "Phone",
		"festa":        "smoke",
		"wifi_group":   "lab",
		"wifi_essid":   "Lab",
		"wifi_bssid":   "any",
	})
	families := pushes[0].metricFamilies(t)
	assertGauge(t, families, core.MetricSuccess, nil, 1)
	assertGauge(t, families, core.MetricDuration, nil, 0.51)
	assertGauge(t, families, core.MetricPingSuccess, map[string]string{"target": "1.1.1.1"}, 1)
	assertGauge(t, families, core.MetricPingDuration, map[string]string{"target": "1.1.1.1"}, 0.12)
	assertGauge(t, families, core.MetricDNSSuccess, map[string]string{"target": "example.com"}, 1)
	assertGauge(t, families, core.MetricDNSDuration, map[string]string{"target": "example.com"}, 0.09)
	assertGauge(t, families, core.MetricHTTPSuccess, map[string]string{"target": "http://example.com/health"}, 1)
	assertGauge(t, families, core.MetricHTTPStatusCode, map[string]string{"target": "http://example.com/health"}, 204)
}

func TestNotificationPathFetchesMinIOObjectAndDeduplicatesSameObject(t *testing.T) {
	env := newIntegrationEnv(t, "notification")
	archive := standaloneArchiveFixture("run-event-1", "ok")
	key := "incoming/device/run-event-1.pb"
	info := env.putArchive(t, key, archive)

	ing := env.newIngester(t, "incoming")
	body := fmt.Sprintf(`{
		"Records": [{
			"eventName": "s3:ObjectCreated:Put",
			"s3": {
				"bucket": {"name": %q},
				"object": {"key": %q, "eTag": %q, "size": %d}
			}
		}]
	}`, env.bucket, key, info.ETag, info.Size)

	for attempt := 0; attempt < 2; attempt++ {
		req := httptest.NewRequest(http.MethodPost, "/minio/events", strings.NewReader(body))
		rec := httptest.NewRecorder()
		ing.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusAccepted {
			t.Fatalf("notification status = %d body=%s", rec.Code, rec.Body.String())
		}
	}

	pushes := env.pushgateway.requests()
	if len(pushes) != 1 {
		t.Fatalf("push requests = %d, want one push after duplicate notification", len(pushes))
	}
	families := pushes[0].metricFamilies(t)
	assertPushPathGrouping(t, pushes[0].Path, map[string]string{
		"festa":      "smoke",
		"wifi_group": "lab",
		"wifi_essid": "Lab",
		"wifi_bssid": "any",
	})
	assertGauge(t, families, core.MetricSuccess, nil, 1)
	assertGauge(t, families, core.MetricHTTPDuration, map[string]string{"target": "http://example.com/health"}, 0.11)
}

func TestDecodeFailureReturnsErrorWithoutPushingMetrics(t *testing.T) {
	env := newIntegrationEnv(t, "decode-failure")
	env.putObject(t, "incoming/device/bad.pb", []byte("not a standalone archive"))

	ing := env.newIngester(t, "incoming")
	err := ing.ProcessBatch(context.Background())
	if err == nil {
		t.Fatal("ProcessBatch err = nil, want decode error")
	}
	if !strings.Contains(err.Error(), "decode standalone archive") {
		t.Fatalf("ProcessBatch err = %v, want decode context", err)
	}

	pushes := env.pushgateway.requests()
	if len(pushes) != 0 {
		t.Fatalf("push requests = %d, want 0", len(pushes))
	}
}

type integrationEnv struct {
	bucket      string
	client      *minio.Client
	pushgateway *pushgatewayCapture
}

func newIntegrationEnv(t *testing.T, name string) *integrationEnv {
	t.Helper()
	if minIOSkip != "" {
		t.Skipf("MinIO integration skipped: %s", minIOSkip)
	}
	client := newMinIOClient(t)
	bucket := fmt.Sprintf("dropcheck-it-%s-%d", strings.ReplaceAll(name, "_", "-"), time.Now().UnixNano())
	if err := client.MakeBucket(context.Background(), bucket, minio.MakeBucketOptions{}); err != nil {
		t.Fatalf("create bucket %s: %v", bucket, err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		for object := range client.ListObjects(ctx, bucket, minio.ListObjectsOptions{Recursive: true}) {
			if object.Err == nil {
				_ = client.RemoveObject(ctx, bucket, object.Key, minio.RemoveObjectOptions{})
			}
		}
		_ = client.RemoveBucket(ctx, bucket)
	})
	return &integrationEnv{
		bucket:      bucket,
		client:      client,
		pushgateway: newPushgatewayCapture(t),
	}
}

func (e *integrationEnv) newIngester(t *testing.T, prefix string) *core.Ingester {
	t.Helper()
	cfg := core.Config{
		ListenAddr:     ":0",
		MinIOEndpoint:  minIOEndpoint,
		MinIOAccessKey: minIOAccessKey,
		MinIOSecretKey: minIOSecretKey,
		MinIOBucket:    e.bucket,
		MinIOPrefix:    prefix,
		ObjectSuffix:   ".pb",
		PushgatewayURL: e.pushgateway.URL(),
		PushJob:        "dropcheck_ingester_integration",
		BatchInterval:  time.Hour,
		MaxObjectBytes: 1 << 20,
	}
	store, err := core.NewMinIOStore(cfg)
	if err != nil {
		t.Fatalf("create MinIO store: %v", err)
	}
	return core.New(cfg, store, core.NewPushgatewayPusher(cfg.PushgatewayURL, cfg.PushJob), log.New(io.Discard, "", 0))
}

func (e *integrationEnv) putArchive(t *testing.T, key string, archive *controlpb.StandaloneRunArchive) minio.UploadInfo {
	t.Helper()
	data, err := proto.Marshal(archive)
	if err != nil {
		t.Fatalf("marshal archive: %v", err)
	}
	return e.putObject(t, key, data)
}

func (e *integrationEnv) putObject(t *testing.T, key string, data []byte) minio.UploadInfo {
	t.Helper()
	info, err := e.client.PutObject(context.Background(), e.bucket, key, bytes.NewReader(data), int64(len(data)), minio.PutObjectOptions{
		ContentType: "application/x-protobuf",
	})
	if err != nil {
		t.Fatalf("put object %s: %v", key, err)
	}
	return info
}

type minIOService struct {
	endpoint  string
	name      string
	closeOnce sync.Once
}

func startMinIOContainer() (*minIOService, error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return nil, fmt.Errorf("docker command not found: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if out, err := exec.CommandContext(ctx, "docker", "version", "--format", "{{.Server.Version}}").CombinedOutput(); err != nil {
		return nil, fmt.Errorf("docker daemon unavailable: %v: %s", err, strings.TrimSpace(string(out)))
	}
	port, err := freePort()
	if err != nil {
		return nil, err
	}
	name := fmt.Sprintf("dropcheck-ingester-it-%d", time.Now().UnixNano())
	args := []string{
		"run", "--rm", "-d",
		"--name", name,
		"-p", fmt.Sprintf("127.0.0.1:%d:9000", port),
		"-e", "MINIO_ROOT_USER=" + minIOAccessKey,
		"-e", "MINIO_ROOT_PASSWORD=" + minIOSecretKey,
		"minio/minio:latest",
		"server", "/data",
	}
	if out, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput(); err != nil {
		return nil, fmt.Errorf("start MinIO container: %v: %s", err, strings.TrimSpace(string(out)))
	}
	service := &minIOService{
		endpoint: fmt.Sprintf("127.0.0.1:%d", port),
		name:     name,
	}
	if err := waitMinIOReady(ctx, service.endpoint); err != nil {
		service.close()
		return nil, err
	}
	return service, nil
}

func (s *minIOService) close() {
	s.closeOnce.Do(func() {
		_ = exec.Command("docker", "rm", "-f", s.name).Run()
	})
}

func freePort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

func waitMinIOReady(ctx context.Context, endpoint string) error {
	client := &http.Client{Timeout: time.Second}
	url := "http://" + endpoint + "/minio/health/ready"
	deadline := time.Now().Add(90 * time.Second)
	var last string
	for time.Now().Before(deadline) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
			last = resp.Status
		} else {
			last = err.Error()
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("MinIO health endpoint %s not ready: %s", url, last)
}

func newMinIOClient(t *testing.T) *minio.Client {
	t.Helper()
	client, err := minio.New(minIOEndpoint, &minio.Options{
		Creds: credentials.NewStaticV4(minIOAccessKey, minIOSecretKey, ""),
	})
	if err != nil {
		t.Fatalf("create MinIO client: %v", err)
	}
	return client
}

type pushgatewayCapture struct {
	server *httptest.Server
	mu     sync.Mutex
	reqs   []pushRequest
}

type pushRequest struct {
	Method      string
	Path        string
	ContentType string
	Body        []byte
}

func newPushgatewayCapture(t *testing.T) *pushgatewayCapture {
	t.Helper()
	capture := &pushgatewayCapture{}
	capture.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read body", http.StatusBadRequest)
			return
		}
		capture.mu.Lock()
		capture.reqs = append(capture.reqs, pushRequest{
			Method:      r.Method,
			Path:        r.URL.EscapedPath(),
			ContentType: r.Header.Get("Content-Type"),
			Body:        append([]byte(nil), body...),
		})
		capture.mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(capture.server.Close)
	return capture
}

func (c *pushgatewayCapture) URL() string {
	return c.server.URL
}

func (c *pushgatewayCapture) requests() []pushRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]pushRequest(nil), c.reqs...)
}

func (r pushRequest) metricFamilies(t *testing.T) map[string]*dto.MetricFamily {
	t.Helper()
	format := expfmt.ResponseFormat(http.Header{"Content-Type": []string{r.ContentType}})
	decoder := expfmt.NewDecoder(bytes.NewReader(r.Body), format)
	families := map[string]*dto.MetricFamily{}
	for {
		family := &dto.MetricFamily{}
		err := decoder.Decode(family)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("parse pushed metrics content_type=%q: %v", r.ContentType, err)
		}
		families[family.GetName()] = family
	}
	return families
}

func assertGauge(t *testing.T, families map[string]*dto.MetricFamily, name string, labels map[string]string, want float64) {
	t.Helper()
	family := families[name]
	if family == nil {
		t.Fatalf("metric %s not found", name)
	}
	for _, metric := range family.GetMetric() {
		if !metricHasLabels(metric, labels) {
			continue
		}
		if metric.GetGauge() == nil {
			t.Fatalf("metric %s is not a gauge", name)
		}
		got := metric.GetGauge().GetValue()
		if math.Abs(got-want) > 1e-9 {
			t.Fatalf("metric %s value = %v, want %v labels=%v", name, got, want, labels)
		}
		return
	}
	t.Fatalf("metric %s with labels %v not found", name, labels)
}

func assertPushPathGrouping(t *testing.T, path string, labels map[string]string) {
	t.Helper()
	segments := decodedPushPathSegments(t, path)
	for key, value := range labels {
		if !containsAdjacentPushPathSegments(segments, key, value) {
			t.Fatalf("push path %q missing grouping %s=%q", path, key, value)
		}
	}
	for _, forbidden := range []string{"run_id", "object_key", "step", "command", "result_status", "wifi_band", "wifi_security"} {
		if containsPushPathSegment(segments, forbidden) {
			t.Fatalf("push path %q contains forbidden grouping label %q", path, forbidden)
		}
	}
}

func decodedPushPathSegments(t *testing.T, path string) []string {
	t.Helper()
	raw := strings.Split(strings.Trim(path, "/"), "/")
	segments := make([]string, 0, len(raw))
	for i := 0; i < len(raw); i++ {
		segment := raw[i]
		decoded, err := url.PathUnescape(segment)
		if err != nil {
			t.Fatalf("decode push path segment %q: %v", segment, err)
		}
		if label, ok := strings.CutSuffix(decoded, "@base64"); ok {
			segments = append(segments, label)
			i++
			if i >= len(raw) {
				t.Fatalf("base64 push path label %q missing value in path %q", decoded, path)
			}
			value, err := base64.RawURLEncoding.DecodeString(raw[i])
			if err != nil {
				t.Fatalf("decode base64 push path segment %q: %v", raw[i], err)
			}
			segments = append(segments, string(value))
			continue
		}
		segments = append(segments, decoded)
	}
	return segments
}

func containsAdjacentPushPathSegments(segments []string, key string, value string) bool {
	for i := 0; i+1 < len(segments); i++ {
		if segments[i] == key && segments[i+1] == value {
			return true
		}
	}
	return false
}

func containsPushPathSegment(segments []string, value string) bool {
	for _, segment := range segments {
		if segment == value {
			return true
		}
	}
	return false
}

func metricHasLabels(metric *dto.Metric, labels map[string]string) bool {
	for key, want := range labels {
		found := false
		for _, label := range metric.GetLabel() {
			if label.GetName() == key {
				found = label.GetValue() == want
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func standaloneArchiveFixture(runID, status string) *controlpb.StandaloneRunArchive {
	const (
		started = int64(1_700_000_000_000)
		group   = "lab"
	)
	failed := uint32(0)
	if status != "ok" {
		failed = 1
	}
	return &controlpb.StandaloneRunArchive{
		Summary: &controlpb.StandaloneRunSummary{
			RunId:           runID,
			FestaName:       "smoke",
			StartedUnixMs:   started,
			FinishedUnixMs:  started + 5000,
			Status:          status,
			WifiGroupCount:  1,
			StepCount:       3,
			FailedStepCount: failed,
			Message:         "connectivity completed",
		},
		Device: &controlpb.DeviceInfo{
			Manufacturer: "Acme",
			Model:        "Phone",
			Device:       "phone1",
			Sdk:          36,
			Release:      "16",
		},
		Festa: &controlpb.StandaloneFesta{
			Name: "smoke",
			WifiGroups: []*controlpb.StandaloneWifiGroup{{
				Name:      group,
				Essid:     "Lab",
				Security:  controlpb.ConnectWifi_SECURITY_WPA2_PSK,
				Band:      controlpb.WifiBand_WIFI_BAND_5_GHZ,
				RequireIp: true,
			}},
		},
		Steps: []*controlpb.StandaloneMeasurementStep{
			standaloneStep(1, group, 1, "ping", started, started+120, &controlpb.RunCommand{
				Label: "standalone ping 1.1.1.1",
				Command: &controlpb.RunCommand_Ping{Ping: &controlpb.Ping{
					Host:      "1.1.1.1",
					Count:     3,
					TimeoutMs: 10000,
				}},
			}, &controlpb.CommandResult{
				Status:    controlpb.CommandResult_STATUS_OK,
				ElapsedMs: 120,
				Payload: &controlpb.CommandResult_Ping{Ping: &controlpb.PingResult{
					Host:              "1.1.1.1",
					Count:             3,
					Transmitted:       3,
					Received:          3,
					PacketLossPercent: 0,
					MinMs:             10,
					AvgMs:             20,
					MaxMs:             30,
					ElapsedMs:         120,
				}},
			}),
			standaloneStep(1, group, 2, "dns", started+200, started+290, &controlpb.RunCommand{
				Label: "standalone dns example.com",
				Command: &controlpb.RunCommand_ResolveDns{ResolveDns: &controlpb.ResolveDns{
					Name:      "example.com",
					Qtypes:    []controlpb.DnsRecordType{controlpb.DnsRecordType_DNS_RECORD_TYPE_A},
					TimeoutMs: 10000,
				}},
			}, &controlpb.CommandResult{
				Status:    controlpb.CommandResult_STATUS_OK,
				ElapsedMs: 90,
				Payload: &controlpb.CommandResult_ResolveDns{ResolveDns: &controlpb.ResolveDnsResult{
					Name:      "example.com",
					ElapsedMs: 90,
					Answers: []*controlpb.DnsAnswer{{
						Type:    controlpb.DnsRecordType_DNS_RECORD_TYPE_A,
						Address: "93.184.216.34",
					}},
				}},
			}),
			standaloneStep(1, group, 3, "http", started+400, started+510, &controlpb.RunCommand{
				Label: "standalone http http://example.com/health",
				Command: &controlpb.RunCommand_HttpCheck{HttpCheck: &controlpb.HttpCheck{
					Url:            "http://example.com/health",
					ExpectedStatus: 204,
					TimeoutMs:      10000,
				}},
			}, &controlpb.CommandResult{
				Status:    controlpb.CommandResult_STATUS_OK,
				ElapsedMs: 110,
				Payload: &controlpb.CommandResult_HttpCheck{HttpCheck: &controlpb.HttpCheckResult{
					Url:            "http://example.com/health",
					Status:         204,
					ExpectedStatus: 204,
					Matched:        true,
					ElapsedMs:      110,
				}},
			}),
		},
	}
}

func standaloneStep(groupIndex uint32, group string, stepIndex uint32, name string, started, finished int64, command *controlpb.RunCommand, result *controlpb.CommandResult) *controlpb.StandaloneMeasurementStep {
	return &controlpb.StandaloneMeasurementStep{
		WifiGroupIndex: groupIndex,
		WifiGroupName:  group,
		StepIndex:      stepIndex,
		StepName:       name,
		Attempt:        1,
		StartedUnixMs:  started,
		FinishedUnixMs: finished,
		Command:        command,
		Result:         result,
	}
}
