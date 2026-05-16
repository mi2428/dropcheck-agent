package ingester

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"

	"dropcheck/controller/internal/controlpb"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/expfmt"
)

const (
	// MetricSuccess is 1 when every step in a Wi-Fi target succeeded.
	MetricSuccess = "dropcheck_success"
	// MetricDuration is the Wi-Fi target run duration in seconds.
	MetricDuration = "dropcheck_duration_seconds"
	// MetricConnectSuccess is 1 when the Wi-Fi connect step succeeded.
	MetricConnectSuccess = "dropcheck_connect_success"
	// MetricConnectDuration is the Wi-Fi connect step duration in seconds.
	MetricConnectDuration = "dropcheck_connect_duration_seconds"
	// MetricWaitConnectedSuccess is 1 when the Wi-Fi connected assertion succeeded.
	MetricWaitConnectedSuccess = "dropcheck_wait_connected_success"
	// MetricWaitConnectedDuration is the Wi-Fi connected assertion duration in seconds.
	MetricWaitConnectedDuration = "dropcheck_wait_connected_duration_seconds"
	// MetricDNSSuccess is 1 when a DNS probe succeeded.
	MetricDNSSuccess = "dropcheck_dns_success"
	// MetricDNSDuration is the DNS probe duration in seconds.
	MetricDNSDuration = "dropcheck_dns_duration_seconds"
	// MetricPingSuccess is 1 when a ping probe succeeded.
	MetricPingSuccess = "dropcheck_ping_success"
	// MetricPingDuration is the ping probe duration in seconds.
	MetricPingDuration = "dropcheck_ping_duration_seconds"
	// MetricHTTPSuccess is 1 when an HTTP probe matched its expected status.
	MetricHTTPSuccess = "dropcheck_http_success"
	// MetricHTTPStatusCode is the HTTP status code observed by an HTTP probe.
	MetricHTTPStatusCode = "dropcheck_http_status_code"
	// MetricHTTPDuration is the HTTP probe duration in seconds.
	MetricHTTPDuration = "dropcheck_http_duration_seconds"
)

const (
	groupingDeviceName  = "device_name"
	groupingDeviceModel = "device_model"
	groupingFesta       = "festa"
	groupingWifiGroup   = "wifi_group"
	groupingWifiESSID   = "wifi_essid"
	groupingWifiBSSID   = "wifi_bssid"
	labelTarget         = "target"
)

var groupingLabelNames = []string{
	groupingDeviceName,
	groupingDeviceModel,
	groupingFesta,
	groupingWifiGroup,
	groupingWifiESSID,
	groupingWifiBSSID,
}

type metricDef struct {
	Help   string
	Labels []string
}

var metricDefs = map[string]metricDef{
	MetricSuccess:               {"Whether the latest Dropcheck run for this Wi-Fi target succeeded.", nil},
	MetricDuration:              {"Seconds spent by the latest Dropcheck run for this Wi-Fi target.", nil},
	MetricConnectSuccess:        {"Whether the latest Wi-Fi connect step succeeded.", nil},
	MetricConnectDuration:       {"Seconds spent by the latest Wi-Fi connect step.", nil},
	MetricWaitConnectedSuccess:  {"Whether the latest Wi-Fi connected assertion succeeded.", nil},
	MetricWaitConnectedDuration: {"Seconds spent by the latest Wi-Fi connected assertion.", nil},
	MetricDNSSuccess:            {"Whether the latest DNS probe succeeded.", []string{labelTarget}},
	MetricDNSDuration:           {"Seconds spent by the latest DNS probe.", []string{labelTarget}},
	MetricPingSuccess:           {"Whether the latest ping probe succeeded.", []string{labelTarget}},
	MetricPingDuration:          {"Seconds spent by the latest ping probe.", []string{labelTarget}},
	MetricHTTPSuccess:           {"Whether the latest HTTP probe matched its expected status.", []string{labelTarget}},
	MetricHTTPStatusCode:        {"HTTP status code observed by the latest HTTP probe.", []string{labelTarget}},
	MetricHTTPDuration:          {"Seconds spent by the latest HTTP probe.", []string{labelTarget}},
}

// MetricSample is one gauge value inside a MetricBatch.
//
// Labels only contains labels that vary within a Pushgateway group. Stable labels
// such as festa, device, and Wi-Fi identifiers live on MetricBatch.Grouping so a
// new object upload overwrites the previous values for the same tested unit.
type MetricSample struct {
	// Name is one of the Metric* constants.
	Name string
	// Labels contains per-metric labels, currently only target on probe metrics.
	Labels map[string]string
	// Value is the gauge value to push.
	Value float64
}

// MetricBatch is the complete Pushgateway payload for one tested Wi-Fi unit.
//
// Pushgateway grouping labels become Prometheus labels after scrape, but they
// must not also appear in MetricSample.Labels. This keeps run_id and object_key
// out of the metric identity while preserving labels needed to find a failing
// Wi-Fi test.
type MetricBatch struct {
	// Grouping is the stable Pushgateway grouping for one tested Wi-Fi unit.
	Grouping map[string]string
	// Samples is the complete set of gauges that should replace the group.
	Samples []MetricSample
}

// MetricPusher writes one tested Wi-Fi unit to a metrics sink.
type MetricPusher interface {
	Push(ctx context.Context, batch MetricBatch) error
}

// PushgatewayPusher pushes Dropcheck result metrics to Prometheus Pushgateway.
type PushgatewayPusher struct {
	url string
	job string
}

// NewPushgatewayPusher creates a pusher for the configured Pushgateway endpoint
// and job name.
func NewPushgatewayPusher(url, job string) *PushgatewayPusher {
	return &PushgatewayPusher{url: strings.TrimRight(url, "/"), job: job}
}

// Push replaces the Pushgateway group for one tested Wi-Fi unit.
func (p *PushgatewayPusher) Push(ctx context.Context, batch MetricBatch) error {
	if len(batch.Samples) == 0 {
		return nil
	}
	registry := prometheus.NewRegistry()
	gauges := map[string]*prometheus.GaugeVec{}
	for _, sample := range batch.Samples {
		def, ok := metricDefs[sample.Name]
		if !ok {
			return fmt.Errorf("unknown metric %q", sample.Name)
		}
		if err := validateSampleLabels(sample, def.Labels); err != nil {
			return err
		}
		gauge, ok := gauges[sample.Name]
		if !ok {
			gauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
				Name: sample.Name,
				Help: def.Help,
			}, def.Labels)
			registry.MustRegister(gauge)
			gauges[sample.Name] = gauge
		}
		labels := prometheus.Labels{}
		for _, label := range def.Labels {
			labels[label] = sample.Labels[label]
		}
		gauge.With(labels).Set(sample.Value)
	}

	return p.pushGatherer(ctx, registry, batch.Grouping)
}

func (p *PushgatewayPusher) pushGatherer(ctx context.Context, gatherer prometheus.Gatherer, grouping map[string]string) error {
	families, err := gatherer.Gather()
	if err != nil {
		return err
	}
	var body bytes.Buffer
	format := expfmt.NewFormat(expfmt.TypeTextPlain)
	encoder := expfmt.NewEncoder(&body, format)
	for _, family := range families {
		if err := encoder.Encode(family); err != nil {
			return err
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, p.fullURL(grouping), &body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", string(format))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusAccepted {
		return nil
	}
	responseBody, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("unexpected status code %d while pushing to %s: %s", resp.StatusCode, p.fullURL(grouping), responseBody)
}

func (p *PushgatewayPusher) fullURL(grouping map[string]string) string {
	components := []string{"job@base64", pushgatewayPathValue(p.job)}
	for _, label := range groupingLabelNames {
		components = append(components, label+"@base64", pushgatewayPathValue(nonEmptyLabel(grouping[label], "unknown")))
	}
	return p.url + "/metrics/" + strings.Join(components, "/")
}

func pushgatewayPathValue(value string) string {
	if value == "" {
		return "="
	}
	return base64.RawURLEncoding.EncodeToString([]byte(value))
}

// ArchiveMetricBatches converts a standalone result archive into one metrics
// batch per Wi-Fi target. Each batch is intentionally keyed by stable test-unit
// labels instead of run_id or object_key, so Pushgateway replaces old values
// rather than accumulating one series per upload.
func ArchiveMetricBatches(archive *controlpb.StandaloneRunArchive) []MetricBatch {
	if archive == nil {
		return nil
	}
	groups := map[string]*wifiMetricGroup{}
	order := []string{}
	for _, step := range archive.GetSteps() {
		if step == nil {
			continue
		}
		grouping := groupingForStep(archive, step)
		key := groupingKey(grouping)
		group := groups[key]
		if group == nil {
			group = &wifiMetricGroup{grouping: grouping}
			groups[key] = group
			order = append(order, key)
		}
		group.record(step)
		group.samples = append(group.samples, operationMetrics(step)...)
	}

	batches := make([]MetricBatch, 0, len(order))
	for _, key := range order {
		group := groups[key]
		samples := []MetricSample{
			{Name: MetricSuccess, Value: boolFloat(group.succeeded())},
			{Name: MetricDuration, Value: group.durationSeconds()},
		}
		samples = append(samples, group.samples...)
		batches = append(batches, MetricBatch{
			Grouping: group.grouping,
			Samples:  samples,
		})
	}
	return batches
}

type wifiMetricGroup struct {
	grouping       map[string]string
	samples        []MetricSample
	seenStep       bool
	failed         bool
	startedUnixMs  int64
	finishedUnixMs int64
	durationTotal  float64
}

func (g *wifiMetricGroup) record(step *controlpb.StandaloneMeasurementStep) {
	g.seenStep = true
	if !stepSucceeded(step) {
		g.failed = true
	}
	g.durationTotal += stepDurationSeconds(step)
	started := step.GetStartedUnixMs()
	finished := step.GetFinishedUnixMs()
	if started > 0 && (g.startedUnixMs == 0 || started < g.startedUnixMs) {
		g.startedUnixMs = started
	}
	if finished > g.finishedUnixMs {
		g.finishedUnixMs = finished
	}
}

func (g *wifiMetricGroup) succeeded() bool {
	return g.seenStep && !g.failed
}

func (g *wifiMetricGroup) durationSeconds() float64 {
	if duration := unixMillisDuration(g.startedUnixMs, g.finishedUnixMs); duration > 0 {
		return duration
	}
	return g.durationTotal
}

func operationMetrics(step *controlpb.StandaloneMeasurementStep) []MetricSample {
	result := step.GetResult()
	command := step.GetCommand()
	switch {
	case command.GetConnectWifi() != nil || result.GetConnectWifi() != nil:
		return []MetricSample{
			{Name: MetricConnectSuccess, Value: boolFloat(connectSucceeded(step))},
			{Name: MetricConnectDuration, Value: stepDurationSeconds(step)},
		}
	case command.GetWaitWifiConnected() != nil || result.GetWifiAssert() != nil:
		return []MetricSample{
			{Name: MetricWaitConnectedSuccess, Value: boolFloat(waitConnectedSucceeded(step))},
			{Name: MetricWaitConnectedDuration, Value: waitConnectedDurationSeconds(step)},
		}
	case command.GetResolveDns() != nil || result.GetResolveDns() != nil:
		dns := result.GetResolveDns()
		target := nonEmptyLabel(firstNonEmpty(dns.GetName(), command.GetResolveDns().GetName()), "unknown")
		labels := map[string]string{labelTarget: target}
		return []MetricSample{
			{Name: MetricDNSSuccess, Labels: labels, Value: boolFloat(dnsSucceeded(step))},
			{Name: MetricDNSDuration, Labels: labels, Value: dnsDurationSeconds(step)},
		}
	case command.GetPing() != nil || result.GetPing() != nil:
		ping := result.GetPing()
		target := nonEmptyLabel(firstNonEmpty(ping.GetHost(), command.GetPing().GetHost()), "unknown")
		labels := map[string]string{labelTarget: target}
		return []MetricSample{
			{Name: MetricPingSuccess, Labels: labels, Value: boolFloat(stepSucceeded(step))},
			{Name: MetricPingDuration, Labels: labels, Value: pingDurationSeconds(step)},
		}
	case command.GetHttpCheck() != nil || result.GetHttpCheck() != nil:
		httpCheck := result.GetHttpCheck()
		target := nonEmptyLabel(firstNonEmpty(httpCheck.GetUrl(), command.GetHttpCheck().GetUrl()), "unknown")
		labels := map[string]string{labelTarget: target}
		return []MetricSample{
			{Name: MetricHTTPSuccess, Labels: labels, Value: boolFloat(httpSucceeded(step))},
			{Name: MetricHTTPStatusCode, Labels: labels, Value: float64(httpCheck.GetStatus())},
			{Name: MetricHTTPDuration, Labels: labels, Value: httpDurationSeconds(step)},
		}
	default:
		return nil
	}
}

func groupingForStep(archive *controlpb.StandaloneRunArchive, step *controlpb.StandaloneMeasurementStep) map[string]string {
	device := archive.GetDevice()
	wifi := wifiGroupForStep(archive, step)
	festaName := firstNonEmpty(archive.GetSummary().GetFestaName(), archive.GetFesta().GetName(), "unknown")
	wifiGroupName := firstNonEmpty(step.GetWifiGroupName(), wifi.GetName(), wifiGroupFallback(step.GetWifiGroupIndex()))
	wifiESSID := firstNonEmpty(wifi.GetEssid(), commandSSID(step.GetCommand()), resultSSID(step.GetResult()), "unknown")
	wifiBSSID := firstNonEmpty(wifi.GetBssid(), commandBSSID(step.GetCommand()), "any")
	return map[string]string{
		groupingDeviceName:  nonEmptyLabel(device.GetDevice(), "unknown"),
		groupingDeviceModel: nonEmptyLabel(device.GetModel(), "unknown"),
		groupingFesta:       nonEmptyLabel(festaName, "unknown"),
		groupingWifiGroup:   nonEmptyLabel(wifiGroupName, "default"),
		groupingWifiESSID:   nonEmptyLabel(wifiESSID, "unknown"),
		groupingWifiBSSID:   nonEmptyLabel(wifiBSSID, "any"),
	}
}

func wifiGroupForStep(archive *controlpb.StandaloneRunArchive, step *controlpb.StandaloneMeasurementStep) *controlpb.StandaloneWifiGroup {
	groups := archive.GetFesta().GetWifiGroups()
	if name := strings.TrimSpace(step.GetWifiGroupName()); name != "" {
		for _, group := range groups {
			if group.GetName() == name {
				return group
			}
		}
	}
	index := step.GetWifiGroupIndex()
	if index > 0 && int(index) <= len(groups) {
		return groups[index-1]
	}
	return nil
}

func groupingKey(grouping map[string]string) string {
	parts := make([]string, 0, len(groupingLabelNames))
	for _, label := range groupingLabelNames {
		parts = append(parts, grouping[label])
	}
	return strings.Join(parts, "\xff")
}

func connectSucceeded(step *controlpb.StandaloneMeasurementStep) bool {
	if !stepSucceeded(step) {
		return false
	}
	connect := step.GetResult().GetConnectWifi()
	return connect == nil || connect.GetConnected()
}

func waitConnectedSucceeded(step *controlpb.StandaloneMeasurementStep) bool {
	if !stepSucceeded(step) {
		return false
	}
	assert := step.GetResult().GetWifiAssert()
	return assert == nil || assert.GetPassed()
}

func dnsSucceeded(step *controlpb.StandaloneMeasurementStep) bool {
	if !stepSucceeded(step) {
		return false
	}
	dns := step.GetResult().GetResolveDns()
	return dns == nil || dns.GetError() == ""
}

func httpSucceeded(step *controlpb.StandaloneMeasurementStep) bool {
	if !stepSucceeded(step) {
		return false
	}
	httpCheck := step.GetResult().GetHttpCheck()
	return httpCheck == nil || (httpCheck.GetMatched() && httpCheck.GetError() == "")
}

func waitConnectedDurationSeconds(step *controlpb.StandaloneMeasurementStep) float64 {
	if elapsed := step.GetResult().GetWifiAssert().GetElapsedMs(); elapsed > 0 {
		return millisSeconds(elapsed)
	}
	return stepDurationSeconds(step)
}

func dnsDurationSeconds(step *controlpb.StandaloneMeasurementStep) float64 {
	if elapsed := step.GetResult().GetResolveDns().GetElapsedMs(); elapsed > 0 {
		return millisSeconds(elapsed)
	}
	return stepDurationSeconds(step)
}

func pingDurationSeconds(step *controlpb.StandaloneMeasurementStep) float64 {
	if elapsed := step.GetResult().GetPing().GetElapsedMs(); elapsed > 0 {
		return millisSeconds(elapsed)
	}
	return stepDurationSeconds(step)
}

func httpDurationSeconds(step *controlpb.StandaloneMeasurementStep) float64 {
	if elapsed := step.GetResult().GetHttpCheck().GetElapsedMs(); elapsed > 0 {
		return millisSeconds(elapsed)
	}
	return stepDurationSeconds(step)
}

func stepSucceeded(step *controlpb.StandaloneMeasurementStep) bool {
	return step.GetError() == "" && step.GetResult().GetStatus() == controlpb.CommandResult_STATUS_OK
}

func stepDurationSeconds(step *controlpb.StandaloneMeasurementStep) float64 {
	if elapsed := step.GetResult().GetElapsedMs(); elapsed > 0 {
		return millisSeconds(elapsed)
	}
	return unixMillisDuration(step.GetStartedUnixMs(), step.GetFinishedUnixMs())
}

func commandSSID(command *controlpb.RunCommand) string {
	return firstNonEmpty(
		command.GetConnectWifi().GetSsid(),
		command.GetWaitWifiConnected().GetSsid(),
		command.GetResolveDns().GetSelector().GetSsid(),
		command.GetPing().GetSelector().GetSsid(),
		command.GetHttpCheck().GetSelector().GetSsid(),
	)
}

func commandBSSID(command *controlpb.RunCommand) string {
	return firstNonEmpty(
		command.GetConnectWifi().GetBssid(),
		command.GetWaitWifiConnected().GetBssid(),
	)
}

func resultSSID(result *controlpb.CommandResult) string {
	return firstNonEmpty(
		result.GetConnectWifi().GetSsid(),
		result.GetWifiAssert().GetStatus().GetConnection().GetSsid(),
	)
}

func wifiGroupFallback(index uint32) string {
	if index == 0 {
		return "default"
	}
	return fmt.Sprintf("wifi_group_%d", index)
}

func unixMillisDuration(start, finish int64) float64 {
	if start <= 0 || finish <= start {
		return 0
	}
	return float64(finish-start) / 1000
}

func millisSeconds(ms int64) float64 {
	if ms <= 0 {
		return 0
	}
	return float64(ms) / 1000
}

func boolFloat(value bool) float64 {
	if value {
		return 1
	}
	return 0
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func nonEmptyLabel(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value != "" {
		return value
	}
	return fallback
}

func validateSampleLabels(sample MetricSample, labels []string) error {
	for _, label := range labels {
		if strings.TrimSpace(sample.Labels[label]) == "" {
			return fmt.Errorf("%s missing label %q", sample.Name, label)
		}
	}
	for label := range sample.Labels {
		if !slices.Contains(labels, label) {
			return fmt.Errorf("%s has unexpected label %q", sample.Name, label)
		}
	}
	return nil
}
