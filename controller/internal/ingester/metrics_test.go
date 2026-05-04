package ingester

import (
	"testing"
	"time"

	"dropcheck/controller/internal/controlpb"
	dto "github.com/prometheus/client_model/go"
)

func TestBuildRegistryEmitsRunAndProbeMetrics(t *testing.T) {
	started := int64(1_700_000_000_000)
	archive := &controlpb.StandaloneRunArchive{
		Summary: &controlpb.StandaloneRunSummary{
			RunId:           "run-1",
			FestaName:       "smoke",
			StartedUnixMs:   started,
			FinishedUnixMs:  started + 2500,
			Status:          "ok",
			WifiGroupCount:  1,
			StepCount:       3,
			FailedStepCount: 0,
		},
		Device: &controlpb.DeviceInfo{
			Manufacturer: "Acme",
			Model:        "Phone",
			Device:       "phone1",
		},
		Steps: []*controlpb.StandaloneMeasurementStep{
			step(1, "lab", 1, "ping", started, started+120, &controlpb.RunCommand{
				Command: &controlpb.RunCommand_Ping{Ping: &controlpb.Ping{Host: "1.1.1.1"}},
			}, &controlpb.CommandResult{
				Status:    controlpb.CommandResult_STATUS_OK,
				ElapsedMs: 120,
				Payload: &controlpb.CommandResult_Ping{Ping: &controlpb.PingResult{
					Host:              "1.1.1.1",
					Transmitted:       3,
					Received:          3,
					PacketLossPercent: 0,
					MinMs:             10,
					AvgMs:             20,
					MaxMs:             30,
					ElapsedMs:         120,
				}},
			}),
			step(1, "lab", 2, "dns", started+200, started+290, &controlpb.RunCommand{
				Command: &controlpb.RunCommand_ResolveDns{ResolveDns: &controlpb.ResolveDns{Name: "example.com"}},
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
			step(1, "lab", 3, "http", started+400, started+510, &controlpb.RunCommand{
				Command: &controlpb.RunCommand_HttpCheck{HttpCheck: &controlpb.HttpCheck{Url: "http://example.com/health"}},
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
	registry, err := BuildRegistry(PushInput{
		Archive:        archive,
		Meta:           ObjectMeta{Bucket: "dropcheck", Key: "incoming/device/run.pb", Size: 1234},
		Trigger:        triggerNotification,
		IngestSuccess:  true,
		IngestDuration: 150 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("BuildRegistry: %v", err)
	}
	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	assertMetric(t, families, "dropcheck_festival_ingest_success", nil, 1)
	assertMetric(t, families, "dropcheck_festival_run_success", map[string]string{"run_status": "ok", "festa": "smoke"}, 1)
	assertMetric(t, families, "dropcheck_festival_run_duration_seconds", nil, 2.5)
	assertMetric(t, families, "dropcheck_festival_step_success", map[string]string{"wifi_group": "lab", "step": "ping", "command": "ping", "status": "ok"}, 1)
	assertMetric(t, families, "dropcheck_festival_ping_duration_seconds", map[string]string{"target": "1.1.1.1"}, 0.12)
	assertMetric(t, families, "dropcheck_festival_dns_answers", map[string]string{"target": "example.com"}, 1)
	assertMetric(t, families, "dropcheck_festival_http_success", map[string]string{"target": "http://example.com/health"}, 1)
	assertMetric(t, families, "dropcheck_festival_http_status_code", map[string]string{"target": "http://example.com/health"}, 204)
}

func TestBuildRegistryEmitsDecodeFailureMetric(t *testing.T) {
	registry, err := BuildRegistry(PushInput{
		Meta:           ObjectMeta{Bucket: "dropcheck", Key: "bad.pb", Size: 12},
		Trigger:        triggerBatch,
		IngestSuccess:  false,
		IngestReason:   reasonDecode,
		IngestDuration: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("BuildRegistry: %v", err)
	}
	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	assertMetric(t, families, "dropcheck_festival_ingest_success", map[string]string{"ingest_reason": "decode", "source_object": "bad.pb"}, 0)
	if metricFamily(families, "dropcheck_festival_run_success") != nil {
		t.Fatalf("run metrics should not be emitted for a decode failure")
	}
}

func step(groupIndex uint32, group string, stepIndex uint32, name string, started, finished int64, command *controlpb.RunCommand, result *controlpb.CommandResult) *controlpb.StandaloneMeasurementStep {
	return &controlpb.StandaloneMeasurementStep{
		WifiGroupIndex: groupIndex,
		WifiGroupName:  group,
		StepIndex:      stepIndex,
		StepName:       name,
		StartedUnixMs:  started,
		FinishedUnixMs: finished,
		Command:        command,
		Result:         result,
	}
}

func assertMetric(t *testing.T, families []*dto.MetricFamily, name string, labels map[string]string, want float64) {
	t.Helper()
	family := metricFamily(families, name)
	if family == nil {
		t.Fatalf("metric %s not found", name)
	}
	for _, metric := range family.GetMetric() {
		if metricHasLabels(metric, labels) {
			if got := metric.GetGauge().GetValue(); got != want {
				t.Fatalf("metric %s value = %v, want %v labels=%v", name, got, want, labels)
			}
			return
		}
	}
	t.Fatalf("metric %s with labels %v not found", name, labels)
}

func metricFamily(families []*dto.MetricFamily, name string) *dto.MetricFamily {
	for _, family := range families {
		if family.GetName() == name {
			return family
		}
	}
	return nil
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
