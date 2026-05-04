package ingester

import (
	"fmt"
	"strings"
	"time"

	"dropcheck/controller/internal/controlpb"
	"github.com/prometheus/client_golang/prometheus"
)

// PushInput is the complete metric payload for one ingested object.
type PushInput struct {
	Archive        *controlpb.StandaloneRunArchive
	Meta           ObjectMeta
	Trigger        string
	IngestSuccess  bool
	IngestReason   string
	IngestDuration time.Duration
}

// BuildRegistry converts one ingested object into a fresh Prometheus registry.
func BuildRegistry(input PushInput) (*prometheus.Registry, error) {
	registry := prometheus.NewRegistry()
	labels := commonLabels(input)
	if err := addGauge(registry, "dropcheck_festival_ingest_success", "Whether the Festival result object was ingested successfully.", labels, boolFloat(input.IngestSuccess)); err != nil {
		return nil, err
	}
	if err := addGauge(registry, "dropcheck_festival_ingest_duration_seconds", "Duration of the Festival result ingestion attempt.", labels, input.IngestDuration.Seconds()); err != nil {
		return nil, err
	}
	if err := addGauge(registry, "dropcheck_festival_ingest_object_size_bytes", "Size of the Festival result object read from MinIO.", labels, float64(input.Meta.Size)); err != nil {
		return nil, err
	}
	if err := addGauge(registry, "dropcheck_festival_ingest_last_time_seconds", "Unix time of the latest Festival result ingestion attempt.", labels, float64(time.Now().Unix())); err != nil {
		return nil, err
	}
	archive := input.Archive
	if archive == nil {
		return registry, nil
	}
	summary := archive.GetSummary()
	runLabels := cloneLabels(labels)
	runLabels["run_status"] = summary.GetStatus()
	runSuccess := strings.EqualFold(summary.GetStatus(), "ok") && summary.GetFailedStepCount() == 0
	if err := addGauge(registry, "dropcheck_festival_run_success", "Whether the Festival run completed without failed steps.", runLabels, boolFloat(runSuccess)); err != nil {
		return nil, err
	}
	if err := addGauge(registry, "dropcheck_festival_run_duration_seconds", "Duration of the Festival run.", runLabels, millisRangeSeconds(summary.GetStartedUnixMs(), summary.GetFinishedUnixMs())); err != nil {
		return nil, err
	}
	if err := addGauge(registry, "dropcheck_festival_run_steps_total", "Number of archived Festival measurement steps.", runLabels, float64(summary.GetStepCount())); err != nil {
		return nil, err
	}
	if err := addGauge(registry, "dropcheck_festival_run_failed_steps_total", "Number of failed Festival measurement steps.", runLabels, float64(summary.GetFailedStepCount())); err != nil {
		return nil, err
	}
	if summary.GetStartedUnixMs() > 0 {
		if err := addGauge(registry, "dropcheck_festival_run_start_time_seconds", "Unix start time of the Festival run.", runLabels, millisUnixSeconds(summary.GetStartedUnixMs())); err != nil {
			return nil, err
		}
	}
	if summary.GetFinishedUnixMs() > 0 {
		if err := addGauge(registry, "dropcheck_festival_run_finish_time_seconds", "Unix finish time of the Festival run.", runLabels, millisUnixSeconds(summary.GetFinishedUnixMs())); err != nil {
			return nil, err
		}
	}

	stepSuccess := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name:        "dropcheck_festival_step_success",
		Help:        "Whether an individual Festival measurement step succeeded.",
		ConstLabels: labels,
	}, []string{"wifi_group", "step", "command", "status"})
	stepDuration := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name:        "dropcheck_festival_step_duration_seconds",
		Help:        "Duration of an individual Festival measurement step.",
		ConstLabels: labels,
	}, []string{"wifi_group", "step", "command", "status"})
	pingSuccess := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name:        "dropcheck_festival_ping_success",
		Help:        "Whether a Festival ping probe succeeded.",
		ConstLabels: labels,
	}, []string{"wifi_group", "step", "target"})
	pingDuration := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name:        "dropcheck_festival_ping_duration_seconds",
		Help:        "Duration of a Festival ping probe.",
		ConstLabels: labels,
	}, []string{"wifi_group", "step", "target"})
	pingLoss := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name:        "dropcheck_festival_ping_loss_percent",
		Help:        "Packet loss percentage reported by a Festival ping probe.",
		ConstLabels: labels,
	}, []string{"wifi_group", "step", "target"})
	pingPackets := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name:        "dropcheck_festival_ping_packets",
		Help:        "Packet counters reported by a Festival ping probe.",
		ConstLabels: labels,
	}, []string{"wifi_group", "step", "target", "direction"})
	pingRTT := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name:        "dropcheck_festival_ping_rtt_seconds",
		Help:        "Round-trip time summary reported by a Festival ping probe.",
		ConstLabels: labels,
	}, []string{"wifi_group", "step", "target", "stat"})
	dnsSuccess := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name:        "dropcheck_festival_dns_success",
		Help:        "Whether a Festival DNS probe succeeded.",
		ConstLabels: labels,
	}, []string{"wifi_group", "step", "target"})
	dnsDuration := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name:        "dropcheck_festival_dns_duration_seconds",
		Help:        "Duration of a Festival DNS probe.",
		ConstLabels: labels,
	}, []string{"wifi_group", "step", "target"})
	dnsAnswers := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name:        "dropcheck_festival_dns_answers",
		Help:        "Number of DNS answers returned by a Festival DNS probe.",
		ConstLabels: labels,
	}, []string{"wifi_group", "step", "target"})
	httpSuccess := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name:        "dropcheck_festival_http_success",
		Help:        "Whether a Festival HTTP probe matched the expected status.",
		ConstLabels: labels,
	}, []string{"wifi_group", "step", "target"})
	httpDuration := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name:        "dropcheck_festival_http_duration_seconds",
		Help:        "Duration of a Festival HTTP probe.",
		ConstLabels: labels,
	}, []string{"wifi_group", "step", "target"})
	httpStatus := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name:        "dropcheck_festival_http_status_code",
		Help:        "HTTP status code returned by a Festival HTTP probe.",
		ConstLabels: labels,
	}, []string{"wifi_group", "step", "target"})
	httpExpectedStatus := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name:        "dropcheck_festival_http_expected_status_code",
		Help:        "Expected HTTP status code configured for a Festival HTTP probe.",
		ConstLabels: labels,
	}, []string{"wifi_group", "step", "target"})
	if err := registerAll(registry, stepSuccess, stepDuration, pingSuccess, pingDuration, pingLoss, pingPackets, pingRTT, dnsSuccess, dnsDuration, dnsAnswers, httpSuccess, httpDuration, httpStatus, httpExpectedStatus); err != nil {
		return nil, err
	}

	for _, step := range archive.GetSteps() {
		group := step.GetWifiGroupName()
		if group == "" && step.GetWifiGroupIndex() > 0 {
			group = fmt.Sprintf("wifi_group_%d", step.GetWifiGroupIndex())
		}
		command := commandName(step.GetCommand())
		status := resultStatusName(step.GetResult())
		success := stepSucceeded(step)
		stepSuccess.WithLabelValues(group, step.GetStepName(), command, status).Set(boolFloat(success))
		stepDuration.WithLabelValues(group, step.GetStepName(), command, status).Set(stepDurationSeconds(step))
		if ping := step.GetResult().GetPing(); ping != nil {
			target := firstNonBlank(ping.GetHost(), step.GetCommand().GetPing().GetHost())
			pingSuccess.WithLabelValues(group, step.GetStepName(), target).Set(boolFloat(success))
			pingDuration.WithLabelValues(group, step.GetStepName(), target).Set(millisSeconds(ping.GetElapsedMs(), stepDurationSeconds(step)))
			pingLoss.WithLabelValues(group, step.GetStepName(), target).Set(ping.GetPacketLossPercent())
			pingPackets.WithLabelValues(group, step.GetStepName(), target, "transmitted").Set(float64(ping.GetTransmitted()))
			pingPackets.WithLabelValues(group, step.GetStepName(), target, "received").Set(float64(ping.GetReceived()))
			pingRTT.WithLabelValues(group, step.GetStepName(), target, "min").Set(ping.GetMinMs() / 1000)
			pingRTT.WithLabelValues(group, step.GetStepName(), target, "avg").Set(ping.GetAvgMs() / 1000)
			pingRTT.WithLabelValues(group, step.GetStepName(), target, "max").Set(ping.GetMaxMs() / 1000)
		}
		if dns := step.GetResult().GetResolveDns(); dns != nil {
			target := firstNonBlank(dns.GetName(), step.GetCommand().GetResolveDns().GetName())
			dnsSuccess.WithLabelValues(group, step.GetStepName(), target).Set(boolFloat(success && dns.GetError() == "" && len(dns.GetAnswers()) > 0))
			dnsDuration.WithLabelValues(group, step.GetStepName(), target).Set(millisSeconds(dns.GetElapsedMs(), stepDurationSeconds(step)))
			dnsAnswers.WithLabelValues(group, step.GetStepName(), target).Set(float64(len(dns.GetAnswers())))
		}
		if http := step.GetResult().GetHttpCheck(); http != nil {
			target := firstNonBlank(http.GetUrl(), step.GetCommand().GetHttpCheck().GetUrl())
			httpSuccess.WithLabelValues(group, step.GetStepName(), target).Set(boolFloat(success && http.GetMatched()))
			httpDuration.WithLabelValues(group, step.GetStepName(), target).Set(millisSeconds(http.GetElapsedMs(), stepDurationSeconds(step)))
			httpStatus.WithLabelValues(group, step.GetStepName(), target).Set(float64(http.GetStatus()))
			httpExpectedStatus.WithLabelValues(group, step.GetStepName(), target).Set(float64(http.GetExpectedStatus()))
		}
	}
	return registry, nil
}

func commonLabels(input PushInput) prometheus.Labels {
	archive := input.Archive
	device := archive.GetDevice()
	return prometheus.Labels{
		"source_bucket":       input.Meta.Bucket,
		"source_object":       input.Meta.Key,
		"trigger":             firstNonBlank(input.Trigger, "unknown"),
		"ingest_reason":       input.IngestReason,
		"festa":               firstNonBlank(archive.GetSummary().GetFestaName(), archive.GetFesta().GetName()),
		"device_manufacturer": device.GetManufacturer(),
		"device_model":        device.GetModel(),
		"device_name":         device.GetDevice(),
	}
}

func addGauge(registry *prometheus.Registry, name, help string, labels prometheus.Labels, value float64) error {
	gauge := prometheus.NewGauge(prometheus.GaugeOpts{Name: name, Help: help, ConstLabels: labels})
	if err := registry.Register(gauge); err != nil {
		return err
	}
	gauge.Set(value)
	return nil
}

func registerAll(registry *prometheus.Registry, collectors ...prometheus.Collector) error {
	for _, collector := range collectors {
		if err := registry.Register(collector); err != nil {
			return err
		}
	}
	return nil
}

func cloneLabels(labels prometheus.Labels) prometheus.Labels {
	cloned := make(prometheus.Labels, len(labels))
	for key, value := range labels {
		cloned[key] = value
	}
	return cloned
}

func stepSucceeded(step *controlpb.StandaloneMeasurementStep) bool {
	return step.GetError() == "" && step.GetResult().GetStatus() == controlpb.CommandResult_STATUS_OK
}

func stepDurationSeconds(step *controlpb.StandaloneMeasurementStep) float64 {
	if result := step.GetResult(); result.GetElapsedMs() > 0 {
		return float64(result.GetElapsedMs()) / 1000
	}
	return millisRangeSeconds(step.GetStartedUnixMs(), step.GetFinishedUnixMs())
}

func commandName(command *controlpb.RunCommand) string {
	switch command.GetCommand().(type) {
	case *controlpb.RunCommand_GetWifiStatus:
		return "get_wifi_status"
	case *controlpb.RunCommand_ConnectWifi:
		return "connect_wifi"
	case *controlpb.RunCommand_GetIpStatus:
		return "get_ip_status"
	case *controlpb.RunCommand_Ping:
		return "ping"
	case *controlpb.RunCommand_ResolveDns:
		return "resolve_dns"
	case *controlpb.RunCommand_HttpCheck:
		return "http_check"
	case *controlpb.RunCommand_GetWifiDiagnostics:
		return "get_wifi_diagnostics"
	case *controlpb.RunCommand_GetWifiScan:
		return "get_wifi_scan"
	case *controlpb.RunCommand_GetWifiCapabilities:
		return "get_wifi_capabilities"
	case *controlpb.RunCommand_GetFreshWifiScan:
		return "get_fresh_wifi_scan"
	case *controlpb.RunCommand_DisconnectWifi:
		return "disconnect_wifi"
	case *controlpb.RunCommand_ForgetWifi:
		return "forget_wifi"
	case *controlpb.RunCommand_WaitWifiConnected:
		return "wait_wifi_connected"
	case *controlpb.RunCommand_AssertWifi:
		return "assert_wifi"
	case *controlpb.RunCommand_MonitorWifi:
		return "monitor_wifi"
	case *controlpb.RunCommand_GetWifiScanDetail:
		return "get_wifi_scan_detail"
	case *controlpb.RunCommand_ReconnectWifi:
		return "reconnect_wifi"
	case *controlpb.RunCommand_CycleWifi:
		return "cycle_wifi"
	case *controlpb.RunCommand_Traceroute:
		return "traceroute"
	case *controlpb.RunCommand_Wget:
		return "wget"
	case *controlpb.RunCommand_PathMtu:
		return "path_mtu"
	case *controlpb.RunCommand_GlobalIp:
		return "global_ip"
	case *controlpb.RunCommand_EditStandaloneConfig:
		return "edit_standalone_config"
	case *controlpb.RunCommand_GetStandaloneConfig:
		return "get_standalone_config"
	case *controlpb.RunCommand_GetStandaloneStatus:
		return "get_standalone_status"
	case *controlpb.RunCommand_ListStandaloneRuns:
		return "list_standalone_runs"
	case *controlpb.RunCommand_GetStandaloneRun:
		return "get_standalone_run"
	case *controlpb.RunCommand_ClearStandaloneRuns:
		return "clear_standalone_runs"
	case *controlpb.RunCommand_RunStandaloneOnce:
		return "run_standalone_once"
	default:
		return "unknown"
	}
}

func resultStatusName(result *controlpb.CommandResult) string {
	switch result.GetStatus() {
	case controlpb.CommandResult_STATUS_OK:
		return "ok"
	case controlpb.CommandResult_STATUS_FAILED:
		return "failed"
	case controlpb.CommandResult_STATUS_CANCELED:
		return "canceled"
	default:
		if result == nil {
			return "missing"
		}
		return "unspecified"
	}
}

func boolFloat(value bool) float64 {
	if value {
		return 1
	}
	return 0
}

func millisRangeSeconds(start, finish int64) float64 {
	if start <= 0 || finish <= 0 || finish < start {
		return 0
	}
	return float64(finish-start) / 1000
}

func millisUnixSeconds(value int64) float64 {
	return float64(value) / 1000
}

func millisSeconds(value int64, fallback float64) float64 {
	if value <= 0 {
		return fallback
	}
	return float64(value) / 1000
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
