//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"dropcheck/controller/internal/controlpb"
	f "dropcheck/controller/internal/harness"
	ingestermetrics "dropcheck/controller/internal/ingester"
	"google.golang.org/protobuf/proto"
)

const (
	standaloneObservabilityFesta = "observability-e2e"
	defaultIngesterHTTPPort      = "8082"
	defaultPrometheusHTTPPort    = "9090"
	defaultPushgatewayHTTPPort   = "9091"
)

func TestStandaloneUploadIngesterPrometheusLive(t *testing.T) {
	cfg := loadConfig(t)
	requireLiveStandalone(t, cfg, "standalone MinIO ingester Prometheus E2E")
	prefix := fmt.Sprintf("%s/observability-%d", standaloneUploadPrefix, time.Now().UnixNano())
	stack := cfg.ensureStandaloneObservabilityStack(t, prefix)
	cfg.prepareStandaloneLive(t, "standalone-ingester-prometheus", "Standalone MinIO Ingester Prometheus")
	cfg.clearStandaloneUploadObjectsAtPrefix(t, prefix)
	t.Cleanup(func() {
		cfg.runShellCleanup("config> set standalone disabled")
		cfg.runShellCleanup("clear standalone runs all")
		cfg.runShellCleanup("config> delete standalone upload")
		cfg.runShellCleanup("config> delete standalone festa " + standaloneObservabilityFesta)
	})

	t.Logf("standalone observability live target=%s minio_prefix=%q prometheus=%s", stack.uploadURL, prefix, stack.prometheusURL)
	cfg.resetStandaloneFesta(t, standaloneObservabilityFesta)
	cfg.configureStandaloneUploadFesta(t, standaloneObservabilityFesta, stack.uploadURL)

	status := cfg.waitStandaloneUploadSuccess(t, 2*time.Minute)
	t.Logf("standalone upload reached MinIO: %s", oneLine(redact(status, cfg.psk)))

	objectKey, archiveBytes := cfg.fetchStandaloneUploadArchiveFromMinIOPrefix(t, prefix)
	archive := decodeStandaloneArchive(t, archiveBytes)
	runID := archive.GetSummary().GetRunId()
	if runID == "" {
		t.Fatalf("uploaded archive summary has empty run_id")
	}
	if got := archive.GetSummary().GetFestaName(); got != standaloneObservabilityFesta {
		t.Fatalf("uploaded archive festa=%q, want %q", got, standaloneObservabilityFesta)
	}
	t.Logf("standalone archive uploaded key=%s run_id=%s", objectKey, runID)

	f.Run(t, f.Plan{
		Name: "standalone-observability-minio-eval",
		Results: []f.ResultSource{
			f.StandaloneArchiveBytes("observability-minio-upload", archiveBytes),
		},
		Checks: standaloneHarnessChecks(),
	})

	cfg.waitHarnessMetricsInPrometheus(t, stack.prometheusURL, archive, 2*time.Minute)
}

type standaloneObservabilityStack struct {
	uploadURL     string
	prometheusURL string
}

func (cfg *e2eConfig) ensureStandaloneObservabilityStack(t *testing.T, prefix string) standaloneObservabilityStack {
	t.Helper()
	minioPort := envOr("MINIO_API_PORT", defaultMinIOAPIPort)
	ingesterPort := envOr("INGESTER_HTTP_PORT", defaultIngesterHTTPPort)
	prometheusPort := envOr("PROMETHEUS_HTTP_PORT", defaultPrometheusHTTPPort)
	pushgatewayPort := envOr("PUSHGATEWAY_HTTP_PORT", defaultPushgatewayHTTPPort)

	cfg.startObservabilityStack(t, prefix)
	waitHTTPReady(t, fmt.Sprintf("http://127.0.0.1:%s/minio/health/ready", minioPort), 90*time.Second)
	waitHTTPReady(t, fmt.Sprintf("http://127.0.0.1:%s/healthz", ingesterPort), 90*time.Second)
	waitHTTPReady(t, fmt.Sprintf("http://127.0.0.1:%s/-/ready", pushgatewayPort), 90*time.Second)
	waitHTTPReady(t, fmt.Sprintf("http://127.0.0.1:%s/-/ready", prometheusPort), 90*time.Second)
	cfg.ensureMinIOWebhookForIngester(t)
	cfg.managedMinIO = true

	if cfg.setupADBReverse(t, minioPort) {
		cfg.uploadURL = fmt.Sprintf("http://127.0.0.1:%s/%s/%s", minioPort, standaloneUploadBucket, prefix)
	} else {
		host := hostIPv4Address()
		if host == "" {
			t.Fatalf("adb reverse failed and no non-loopback IPv4 address was found; set %s explicitly", envUploadURL)
		}
		cfg.uploadURL = fmt.Sprintf("http://%s:%s/%s/%s", host, minioPort, standaloneUploadBucket, prefix)
	}
	return standaloneObservabilityStack{
		uploadURL:     cfg.uploadURL,
		prometheusURL: fmt.Sprintf("http://127.0.0.1:%s", prometheusPort),
	}
}

func (cfg *e2eConfig) startObservabilityStack(t *testing.T, prefix string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "compose", "-f", "docker-compose.test.yml", "up", "-d", "--build", "--force-recreate", "minio", "minio-init", "pushgateway", "prometheus", "ingester")
	cmd.Dir = cfg.repoRoot
	cmd.Env = environWith(map[string]string{
		"DROPCHECK_INGESTER_MINIO_PREFIX":  prefix,
		"DROPCHECK_INGESTER_POLL_INTERVAL": "5s",
	})
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	if err != nil {
		t.Fatalf("start observability compose stack: %v\n%s", err, out)
	}
	t.Logf("observability compose stack started with ingester prefix %q", prefix)
}

func (cfg *e2eConfig) ensureMinIOWebhookForIngester(t *testing.T) {
	t.Helper()
	script := minIOAliasScript() + fmt.Sprintf(`
mc mb --ignore-existing "local/${MINIO_BUCKET:-%s}" >/dev/null
mc anonymous set upload "local/${MINIO_BUCKET:-%s}" >/dev/null
mc anonymous get "local/${MINIO_BUCKET:-%s}" >/dev/null
mc event add "local/${MINIO_BUCKET:-%s}" arn:minio:sqs::INGESTER:webhook --event put --suffix .pb --ignore-existing >/dev/null
mc event ls "local/${MINIO_BUCKET:-%s}"
`, standaloneUploadBucket, standaloneUploadBucket, standaloneUploadBucket, standaloneUploadBucket, standaloneUploadBucket)
	out := cfg.runObservabilityMinIOClient(t, "", script)
	if !strings.Contains(out, "arn:minio:sqs::INGESTER:webhook") {
		t.Fatalf("MinIO webhook for ingester was not configured; mc output=%s", oneLine(out))
	}
}

func (cfg *e2eConfig) runObservabilityMinIOClient(t *testing.T, outputDir string, script string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	args := []string{"compose", "-f", "docker-compose.test.yml", "run", "--rm", "-T", "--no-deps", "--entrypoint", "/bin/sh"}
	if outputDir != "" {
		args = append(args, "-v", outputDir+":/out")
	}
	args = append(args, "minio-init", "-c", script)
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Dir = cfg.repoRoot
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	if err != nil {
		t.Fatalf("run observability MinIO client command: %v\n%s", err, out)
	}
	return string(out)
}

func (cfg *e2eConfig) clearStandaloneUploadObjectsAtPrefix(t *testing.T, prefix string) {
	t.Helper()
	script := minIOAliasScript() + fmt.Sprintf(`
mc rm --recursive --force "local/${MINIO_BUCKET:-%s}/%s" >/dev/null 2>&1 || true
`, standaloneUploadBucket, prefix)
	cfg.runObservabilityMinIOClient(t, "", script)
}

func (cfg *e2eConfig) configureStandaloneUploadFesta(t *testing.T, festa string, uploadURL string) {
	t.Helper()
	ssid, psk := cfg.ssid, cfg.psk
	commands := []string{
		"config> set standalone upload to " + quoteToken(uploadURL),
		fmt.Sprintf("config> set standalone upload via wifi essid %s passphrase %s security auto band all mac-randomization auto timeout 25000", quoteToken(ssid), quoteToken(psk)),
		fmt.Sprintf("config> set standalone festa %s interval 2s", festa),
		fmt.Sprintf("config> set standalone festa %s wifi mgmt match essid %s", festa, quoteToken(ssid)),
		fmt.Sprintf("config> set standalone festa %s wifi mgmt passphrase %s security auto", festa, quoteToken(psk)),
		fmt.Sprintf("config> set standalone festa %s wifi mgmt band all", festa),
		fmt.Sprintf("config> set standalone festa %s wifi mgmt wait ip", festa),
		fmt.Sprintf("config> set standalone festa %s wifi mgmt wait validated", festa),
		fmt.Sprintf("config> set standalone festa %s wifi mgmt timeout 25000", festa),
		fmt.Sprintf("config> set standalone festa %s check dns-main test dns name %s type A timeout 8000", festa, standaloneDNSName),
		fmt.Sprintf("config> set standalone festa %s check cloudflare test ping host %s count 1 timeout 8000", festa, standalonePingHost),
		fmt.Sprintf("config> set standalone festa %s check healthz test http url %s expected-status 204 timeout 10000", festa, standaloneHTTPURL),
		fmt.Sprintf("config> set standalone festa %s enabled", festa),
		"config> set standalone enabled",
	}
	for _, line := range commands {
		cfg.runShellLiveCommand(t, line, 90*time.Second)
	}
}

func (cfg *e2eConfig) fetchStandaloneUploadArchiveFromMinIOPrefix(t *testing.T, prefix string) (string, []byte) {
	t.Helper()
	tmpRoot := filepath.Join(cfg.repoRoot, ".tmp")
	if err := os.MkdirAll(tmpRoot, 0o755); err != nil {
		t.Fatalf("create MinIO fetch temp root: %v", err)
	}
	outDir, err := os.MkdirTemp(tmpRoot, "e2e-observability-minio-")
	if err != nil {
		t.Fatalf("create MinIO fetch temp dir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.RemoveAll(outDir)
	})
	script := minIOAliasScript() + fmt.Sprintf(`
object="$(mc find "local/${MINIO_BUCKET:-%s}/%s" --name "*.pb" 2>/dev/null | sort | tail -n 1 || true)"
if [ -z "$object" ]; then
  echo "no standalone protobuf objects found under %s" >&2
  exit 1
fi
mc cp "$object" /out/standalone.pb >/dev/null
printf 'object=%%s\n' "$object"
`, standaloneUploadBucket, prefix, prefix)
	out := cfg.runObservabilityMinIOClient(t, outDir, script)
	path := filepath.Join(outDir, "standalone.pb")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fetched standalone archive %s: %v; mc output=%s", path, err, oneLine(out))
	}
	if len(data) == 0 {
		t.Fatalf("fetched standalone archive is empty; mc output=%s", oneLine(out))
	}
	object := parseMinIOObjectLine(out)
	key := strings.TrimPrefix(object, fmt.Sprintf("local/%s/", standaloneUploadBucket))
	return key, data
}

func decodeStandaloneArchive(t *testing.T, data []byte) *controlpb.StandaloneRunArchive {
	t.Helper()
	archive := &controlpb.StandaloneRunArchive{}
	if err := proto.Unmarshal(data, archive); err != nil {
		t.Fatalf("decode standalone archive from MinIO: %v", err)
	}
	return archive
}

func parseMinIOObjectLine(output string) string {
	for _, line := range strings.Split(output, "\n") {
		if object, ok := strings.CutPrefix(strings.TrimSpace(line), "object="); ok {
			return object
		}
	}
	return ""
}

func (cfg *e2eConfig) waitHarnessMetricsInPrometheus(t *testing.T, prometheusURL string, archive *controlpb.StandaloneRunArchive, timeout time.Duration) {
	t.Helper()
	baseLabels := firstArchiveMetricGrouping(t, archive)
	expectations := []struct {
		name   string
		labels map[string]string
		min    float64
	}{
		{
			name:   ingestermetrics.MetricSuccess,
			labels: baseLabels,
			min:    1,
		},
		{
			name:   ingestermetrics.MetricDuration,
			labels: baseLabels,
			min:    0,
		},
		{
			name:   ingestermetrics.MetricConnectSuccess,
			labels: baseLabels,
			min:    1,
		},
		{
			name:   ingestermetrics.MetricConnectDuration,
			labels: baseLabels,
			min:    0,
		},
		{
			name:   ingestermetrics.MetricWaitConnectedSuccess,
			labels: baseLabels,
			min:    1,
		},
		{
			name:   ingestermetrics.MetricWaitConnectedDuration,
			labels: baseLabels,
			min:    0,
		},
		{
			name:   ingestermetrics.MetricDNSSuccess,
			labels: mergeLabels(baseLabels, map[string]string{"target": standaloneDNSName}),
			min:    1,
		},
		{
			name:   ingestermetrics.MetricDNSDuration,
			labels: mergeLabels(baseLabels, map[string]string{"target": standaloneDNSName}),
			min:    0,
		},
		{
			name:   ingestermetrics.MetricPingSuccess,
			labels: mergeLabels(baseLabels, map[string]string{"target": standalonePingHost}),
			min:    1,
		},
		{
			name:   ingestermetrics.MetricPingDuration,
			labels: mergeLabels(baseLabels, map[string]string{"target": standalonePingHost}),
			min:    0,
		},
		{
			name:   ingestermetrics.MetricHTTPSuccess,
			labels: mergeLabels(baseLabels, map[string]string{"target": standaloneHTTPURL}),
			min:    1,
		},
		{
			name:   ingestermetrics.MetricHTTPStatusCode,
			labels: mergeLabels(baseLabels, map[string]string{"target": standaloneHTTPURL}),
			min:    204,
		},
		{
			name:   ingestermetrics.MetricHTTPDuration,
			labels: mergeLabels(baseLabels, map[string]string{"target": standaloneHTTPURL}),
			min:    0,
		},
	}
	for _, expectation := range expectations {
		cfg.waitPrometheusGaugeAtLeast(t, prometheusURL, expectation.name, expectation.labels, expectation.min, timeout)
	}
}

func firstArchiveMetricGrouping(t *testing.T, archive *controlpb.StandaloneRunArchive) map[string]string {
	t.Helper()
	batches := ingestermetrics.ArchiveMetricBatches(archive)
	if len(batches) == 0 {
		t.Fatalf("archive produced no metric batches")
	}
	grouping := make(map[string]string, len(batches[0].Grouping))
	for key, value := range batches[0].Grouping {
		grouping[key] = value
	}
	return grouping
}

func (cfg *e2eConfig) waitPrometheusGaugeAtLeast(t *testing.T, prometheusURL string, metric string, labels map[string]string, min float64, timeout time.Duration) {
	t.Helper()
	query := promVectorSelector(metric, labels)
	deadline := time.Now().Add(timeout)
	var last string
	for time.Now().Before(deadline) {
		samples, err := queryPrometheus(t, prometheusURL, query)
		if err != nil {
			last = err.Error()
		} else {
			last = fmt.Sprintf("samples=%v", samples)
			for _, sample := range samples {
				if sample.Value >= min || math.Abs(sample.Value-min) < 1e-9 {
					t.Logf("prometheus metric ready: %s value=%v labels=%v", metric, sample.Value, sample.Metric)
					return
				}
			}
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("Prometheus metric %s did not reach >= %v within %s; query=%s last=%s", metric, min, timeout, query, last)
}

type prometheusSample struct {
	Metric map[string]string
	Value  float64
}

func queryPrometheus(t *testing.T, prometheusURL string, query string) ([]prometheusSample, error) {
	t.Helper()
	endpoint, err := url.Parse(strings.TrimRight(prometheusURL, "/") + "/api/v1/query")
	if err != nil {
		return nil, err
	}
	values := endpoint.Query()
	values.Set("query", query)
	endpoint.RawQuery = values.Encode()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("Prometheus query status=%s body=%s", resp.Status, oneLine(string(body)))
	}

	var parsed struct {
		Status string `json:"status"`
		Error  string `json:"error"`
		Data   struct {
			Result []struct {
				Metric map[string]string `json:"metric"`
				Value  []json.RawMessage `json:"value"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("decode Prometheus response: %w body=%s", err, oneLine(string(body)))
	}
	if parsed.Status != "success" {
		return nil, fmt.Errorf("Prometheus query failed status=%q error=%q", parsed.Status, parsed.Error)
	}
	samples := make([]prometheusSample, 0, len(parsed.Data.Result))
	for _, result := range parsed.Data.Result {
		if len(result.Value) != 2 {
			return nil, fmt.Errorf("Prometheus sample value has %d parts", len(result.Value))
		}
		var rawValue string
		if err := json.Unmarshal(result.Value[1], &rawValue); err != nil {
			return nil, fmt.Errorf("decode Prometheus sample value: %w", err)
		}
		value, err := strconv.ParseFloat(rawValue, 64)
		if err != nil {
			return nil, fmt.Errorf("parse Prometheus sample value %q: %w", rawValue, err)
		}
		samples = append(samples, prometheusSample{Metric: result.Metric, Value: value})
	}
	if len(samples) == 0 {
		return nil, fmt.Errorf("no samples")
	}
	return samples, nil
}

func promVectorSelector(metric string, labels map[string]string) string {
	if len(labels) == 0 {
		return metric
	}
	parts := make([]string, 0, len(labels))
	for key, value := range labels {
		parts = append(parts, key+"="+strconv.Quote(value))
	}
	return metric + "{" + strings.Join(parts, ",") + "}"
}

func mergeLabels(base map[string]string, extra map[string]string) map[string]string {
	merged := make(map[string]string, len(base)+len(extra))
	for key, value := range base {
		merged[key] = value
	}
	for key, value := range extra {
		merged[key] = value
	}
	return merged
}

func environWith(overrides map[string]string) []string {
	env := os.Environ()
	for key, value := range overrides {
		entry := key + "=" + value
		replaced := false
		for i, current := range env {
			if strings.HasPrefix(current, key+"=") {
				env[i] = entry
				replaced = true
				break
			}
		}
		if !replaced {
			env = append(env, entry)
		}
	}
	return env
}
