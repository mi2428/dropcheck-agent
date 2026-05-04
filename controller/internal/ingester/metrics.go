package ingester

import (
	"context"
	"fmt"
	"strings"
	"time"

	"dropcheck/controller/internal/controlpb"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/push"
)

const (
	MetricIngestSuccess     = "dropcheck_festival_ingest_success"
	MetricIngestDuration    = "dropcheck_festival_ingest_duration_seconds"
	MetricResultSuccess     = "dropcheck_festival_result_success"
	MetricResultDuration    = "dropcheck_festival_result_duration_seconds"
	MetricResultWifiGroups  = "dropcheck_festival_result_wifi_groups"
	MetricResultSteps       = "dropcheck_festival_result_steps"
	MetricResultFailedSteps = "dropcheck_festival_result_failed_steps"
	MetricStepSuccess       = "dropcheck_festival_step_success"
	MetricStepDuration      = "dropcheck_festival_step_duration_seconds"
	MetricPingTransmitted   = "dropcheck_festival_ping_transmitted_packets"
	MetricPingReceived      = "dropcheck_festival_ping_received_packets"
	MetricPingPacketLoss    = "dropcheck_festival_ping_packet_loss_ratio"
	MetricPingLatency       = "dropcheck_festival_ping_latency_seconds"
	MetricDNSAnswers        = "dropcheck_festival_dns_answers"
	MetricDNSDuration       = "dropcheck_festival_dns_duration_seconds"
	MetricHTTPSuccess       = "dropcheck_festival_http_success"
	MetricHTTPStatusCode    = "dropcheck_festival_http_status_code"
	MetricHTTPDuration      = "dropcheck_festival_http_duration_seconds"
	MetricWgetBytes         = "dropcheck_festival_wget_bytes"
	MetricWgetThroughput    = "dropcheck_festival_wget_throughput_bytes_per_second"
	MetricPathMTUSuccess    = "dropcheck_festival_path_mtu_success"
	MetricPathMTUBytes      = "dropcheck_festival_path_mtu_bytes"
	MetricGlobalIPAddresses = "dropcheck_festival_global_ip_addresses"
)

type metricDef struct {
	Help   string
	Labels []string
}

var metricDefs = map[string]metricDef{
	MetricIngestSuccess:     {"Whether the ingester parsed and pushed a Festival result object.", []string{"run_id", "festa", "status", "error"}},
	MetricIngestDuration:    {"Seconds spent ingesting a Festival result object.", []string{"run_id", "festa", "status", "error"}},
	MetricResultSuccess:     {"Whether a Festival result archive completed without failed steps.", archiveLabels()},
	MetricResultDuration:    {"Festival result archive duration in seconds.", archiveLabels()},
	MetricResultWifiGroups:  {"Number of Wi-Fi groups in the Festival result archive.", archiveLabels()},
	MetricResultSteps:       {"Number of measurement steps in the Festival result archive.", archiveLabels()},
	MetricResultFailedSteps: {"Number of failed measurement steps in the Festival result archive.", archiveLabels()},
	MetricStepSuccess:       {"Whether a Festival measurement step succeeded.", stepLabels()},
	MetricStepDuration:      {"Festival measurement step duration in seconds.", stepLabels()},
	MetricPingTransmitted:   {"Ping packets transmitted by a Festival measurement step.", append(stepLabels(), "host")},
	MetricPingReceived:      {"Ping packets received by a Festival measurement step.", append(stepLabels(), "host")},
	MetricPingPacketLoss:    {"Ping packet loss ratio for a Festival measurement step.", append(stepLabels(), "host")},
	MetricPingLatency:       {"Ping latency in seconds for a Festival measurement step.", append(stepLabels(), "host", "stat")},
	MetricDNSAnswers:        {"DNS answer count for a Festival measurement step.", append(stepLabels(), "name")},
	MetricDNSDuration:       {"DNS resolution duration in seconds for a Festival measurement step.", append(stepLabels(), "name")},
	MetricHTTPSuccess:       {"Whether an HTTP check matched its expected status.", append(stepLabels(), "url")},
	MetricHTTPStatusCode:    {"HTTP status code observed by a Festival measurement step.", append(stepLabels(), "url")},
	MetricHTTPDuration:      {"HTTP check duration in seconds for a Festival measurement step.", append(stepLabels(), "url")},
	MetricWgetBytes:         {"Bytes read by a Festival download measurement step.", append(stepLabels(), "url")},
	MetricWgetThroughput:    {"Download throughput in bytes per second for a Festival measurement step.", append(stepLabels(), "url")},
	MetricPathMTUSuccess:    {"Whether path MTU discovery converged.", append(stepLabels(), "host")},
	MetricPathMTUBytes:      {"Discovered path MTU in bytes.", append(stepLabels(), "host")},
	MetricGlobalIPAddresses: {"Number of global IP addresses observed.", append(stepLabels(), "service", "family")},
}

// MetricSample is one gauge value to push for a Festival result object.
type MetricSample struct {
	Name   string
	Labels map[string]string
	Value  float64
}

// MetricPusher writes metric samples for one object grouping key.
type MetricPusher interface {
	Push(ctx context.Context, objectKey string, samples []MetricSample) error
}

// PushgatewayPusher pushes Festival result metrics to Prometheus Pushgateway.
type PushgatewayPusher struct {
	url string
	job string
}

func NewPushgatewayPusher(url, job string) *PushgatewayPusher {
	return &PushgatewayPusher{url: strings.TrimRight(url, "/"), job: job}
}

func (p *PushgatewayPusher) Push(ctx context.Context, objectKey string, samples []MetricSample) error {
	registry := prometheus.NewRegistry()
	gauges := map[string]*prometheus.GaugeVec{}
	for _, sample := range samples {
		def, ok := metricDefs[sample.Name]
		if !ok {
			return fmt.Errorf("unknown metric %q", sample.Name)
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
	return push.New(p.url, p.job).
		Grouping("object_key", objectKey).
		Gatherer(registry).
		PushContext(ctx)
}

func ArchiveMetrics(archive *controlpb.StandaloneRunArchive, ingestDuration time.Duration) []MetricSample {
	summary := archive.GetSummary()
	labels := labelsForArchive(archive)
	ingestLabels := map[string]string{
		"run_id": labels["run_id"],
		"festa":  labels["festa"],
		"status": "ok",
		"error":  "",
	}
	samples := []MetricSample{
		{Name: MetricIngestSuccess, Labels: ingestLabels, Value: 1},
		{Name: MetricIngestDuration, Labels: ingestLabels, Value: ingestDuration.Seconds()},
		{Name: MetricResultSuccess, Labels: labels, Value: boolFloat(resultSucceeded(summary))},
		{Name: MetricResultDuration, Labels: labels, Value: unixMillisDuration(summary.GetStartedUnixMs(), summary.GetFinishedUnixMs())},
		{Name: MetricResultWifiGroups, Labels: labels, Value: float64(summary.GetWifiGroupCount())},
		{Name: MetricResultSteps, Labels: labels, Value: float64(summary.GetStepCount())},
		{Name: MetricResultFailedSteps, Labels: labels, Value: float64(summary.GetFailedStepCount())},
	}
	for _, step := range archive.GetSteps() {
		samples = append(samples, stepMetrics(labels, step)...)
	}
	return samples
}

func IngestFailureMetrics(duration time.Duration, status string, err error) []MetricSample {
	labels := map[string]string{
		"run_id": "",
		"festa":  "",
		"status": status,
		"error":  errorLabel(err),
	}
	return []MetricSample{
		{Name: MetricIngestSuccess, Labels: labels, Value: 0},
		{Name: MetricIngestDuration, Labels: labels, Value: duration.Seconds()},
	}
}

func stepMetrics(archiveLabels map[string]string, step *controlpb.StandaloneMeasurementStep) []MetricSample {
	if step == nil {
		return nil
	}
	labels := labelsForStep(archiveLabels, step)
	duration := stepDurationSeconds(step)
	samples := []MetricSample{
		{Name: MetricStepSuccess, Labels: labels, Value: boolFloat(stepSucceeded(step))},
		{Name: MetricStepDuration, Labels: labels, Value: duration},
	}
	result := step.GetResult()
	if ping := result.GetPing(); ping != nil {
		pingLabels := cloneLabels(labels)
		pingLabels["host"] = ping.GetHost()
		samples = append(samples,
			MetricSample{Name: MetricPingTransmitted, Labels: pingLabels, Value: float64(ping.GetTransmitted())},
			MetricSample{Name: MetricPingReceived, Labels: pingLabels, Value: float64(ping.GetReceived())},
			MetricSample{Name: MetricPingPacketLoss, Labels: pingLabels, Value: ping.GetPacketLossPercent() / 100},
		)
		for _, stat := range []struct {
			name  string
			value float64
		}{
			{"min", ping.GetMinMs()},
			{"avg", ping.GetAvgMs()},
			{"max", ping.GetMaxMs()},
		} {
			statLabels := cloneLabels(pingLabels)
			statLabels["stat"] = stat.name
			samples = append(samples, MetricSample{Name: MetricPingLatency, Labels: statLabels, Value: stat.value / 1000})
		}
	}
	if dns := result.GetResolveDns(); dns != nil {
		dnsLabels := cloneLabels(labels)
		dnsLabels["name"] = dns.GetName()
		samples = append(samples,
			MetricSample{Name: MetricDNSAnswers, Labels: dnsLabels, Value: float64(len(dns.GetAnswers()))},
			MetricSample{Name: MetricDNSDuration, Labels: dnsLabels, Value: millisSeconds(dns.GetElapsedMs())},
		)
	}
	if httpCheck := result.GetHttpCheck(); httpCheck != nil {
		httpLabels := cloneLabels(labels)
		httpLabels["url"] = httpCheck.GetUrl()
		samples = append(samples,
			MetricSample{Name: MetricHTTPSuccess, Labels: httpLabels, Value: boolFloat(httpCheck.GetMatched())},
			MetricSample{Name: MetricHTTPStatusCode, Labels: httpLabels, Value: float64(httpCheck.GetStatus())},
			MetricSample{Name: MetricHTTPDuration, Labels: httpLabels, Value: millisSeconds(httpCheck.GetElapsedMs())},
		)
	}
	if wget := result.GetWget(); wget != nil {
		wgetLabels := cloneLabels(labels)
		wgetLabels["url"] = wget.GetUrl()
		samples = append(samples,
			MetricSample{Name: MetricWgetBytes, Labels: wgetLabels, Value: float64(wget.GetBytesRead())},
			MetricSample{Name: MetricWgetThroughput, Labels: wgetLabels, Value: wget.GetThroughputBps() / 8},
		)
	}
	if pmtu := result.GetPathMtu(); pmtu != nil {
		pmtuLabels := cloneLabels(labels)
		pmtuLabels["host"] = pmtu.GetHost()
		samples = append(samples,
			MetricSample{Name: MetricPathMTUSuccess, Labels: pmtuLabels, Value: boolFloat(pmtu.GetDiscovered())},
			MetricSample{Name: MetricPathMTUBytes, Labels: pmtuLabels, Value: float64(pmtu.GetPathMtuBytes())},
		)
	}
	if globalIP := result.GetGlobalIp(); globalIP != nil {
		globalLabels := cloneLabels(labels)
		globalLabels["service"] = globalIP.GetService()
		globalLabels["family"] = strings.ToLower(strings.TrimPrefix(globalIP.GetRequestedFamily().String(), "IP_FAMILY_"))
		count := 0
		for _, address := range globalIP.GetAddresses() {
			if address.GetGlobal() {
				count++
			}
		}
		samples = append(samples, MetricSample{Name: MetricGlobalIPAddresses, Labels: globalLabels, Value: float64(count)})
	}
	return samples
}

func labelsForArchive(archive *controlpb.StandaloneRunArchive) map[string]string {
	summary := archive.GetSummary()
	device := archive.GetDevice()
	festa := firstNonEmpty(summary.GetFestaName(), archive.GetFesta().GetName(), "unknown")
	return map[string]string{
		"run_id":              firstNonEmpty(summary.GetRunId(), "unknown"),
		"festa":               festa,
		"result_status":       firstNonEmpty(summary.GetStatus(), "unknown"),
		"device_manufacturer": device.GetManufacturer(),
		"device_model":        device.GetModel(),
		"device_name":         device.GetDevice(),
	}
}

func labelsForStep(archiveLabels map[string]string, step *controlpb.StandaloneMeasurementStep) map[string]string {
	labels := map[string]string{
		"run_id":        archiveLabels["run_id"],
		"festa":         archiveLabels["festa"],
		"wifi_group":    firstNonEmpty(step.GetWifiGroupName(), fmt.Sprintf("wifi_group_%d", step.GetWifiGroupIndex())),
		"step":          firstNonEmpty(step.GetStepName(), fmt.Sprintf("step_%d", step.GetStepIndex())),
		"command":       commandKind(step.GetCommand()),
		"result_status": resultStatus(step),
	}
	return labels
}

func archiveLabels() []string {
	return []string{"run_id", "festa", "result_status", "device_manufacturer", "device_model", "device_name"}
}

func stepLabels() []string {
	return []string{"run_id", "festa", "wifi_group", "step", "command", "result_status"}
}

func resultSucceeded(summary *controlpb.StandaloneRunSummary) bool {
	return strings.EqualFold(summary.GetStatus(), "ok") && summary.GetFailedStepCount() == 0
}

func stepSucceeded(step *controlpb.StandaloneMeasurementStep) bool {
	return step.GetError() == "" && step.GetResult().GetStatus() == controlpb.CommandResult_STATUS_OK
}

func resultStatus(step *controlpb.StandaloneMeasurementStep) string {
	if step.GetError() != "" {
		return "error"
	}
	result := step.GetResult()
	if result == nil {
		return "missing"
	}
	status := strings.TrimPrefix(result.GetStatus().String(), "STATUS_")
	status = strings.ToLower(status)
	if status == "unspecified" {
		return "unknown"
	}
	return status
}

func commandKind(command *controlpb.RunCommand) string {
	switch {
	case command.GetConnectWifi() != nil:
		return "connect_wifi"
	case command.GetWaitWifiConnected() != nil:
		return "wait_wifi_connected"
	case command.GetPing() != nil:
		return "ping"
	case command.GetResolveDns() != nil:
		return "dns"
	case command.GetHttpCheck() != nil:
		return "http"
	case command.GetWget() != nil:
		return "wget"
	case command.GetPathMtu() != nil:
		return "path_mtu"
	case command.GetGlobalIp() != nil:
		return "global_ip"
	case command.GetGetIpStatus() != nil:
		return "ip_status"
	case command.GetGetWifiStatus() != nil:
		return "wifi_status"
	case command.GetGetWifiScan() != nil || command.GetGetFreshWifiScan() != nil:
		return "wifi_scan"
	case command.GetGetWifiCapabilities() != nil:
		return "wifi_capabilities"
	case command.GetTraceroute() != nil:
		return "traceroute"
	default:
		return "unknown"
	}
}

func stepDurationSeconds(step *controlpb.StandaloneMeasurementStep) float64 {
	if elapsed := step.GetResult().GetElapsedMs(); elapsed > 0 {
		return millisSeconds(elapsed)
	}
	return unixMillisDuration(step.GetStartedUnixMs(), step.GetFinishedUnixMs())
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

func cloneLabels(labels map[string]string) map[string]string {
	out := make(map[string]string, len(labels)+2)
	for key, value := range labels {
		out[key] = value
	}
	return out
}

func errorLabel(err error) string {
	if err == nil {
		return ""
	}
	name := err.Error()
	if idx := strings.Index(name, ":"); idx >= 0 {
		name = name[:idx]
	}
	name = strings.ToLower(name)
	name = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			return r
		}
		return '_'
	}, name)
	return strings.Trim(name, "_")
}
