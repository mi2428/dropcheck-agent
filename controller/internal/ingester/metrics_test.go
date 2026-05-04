package ingester

import (
	"testing"
	"time"

	"dropcheck/controller/internal/controlpb"
)

func TestArchiveMetricsBuildsBlackboxStyleSeries(t *testing.T) {
	archive := metricArchiveFixture()
	samples := ArchiveMetrics(archive, 250*time.Millisecond)

	assertSample(t, samples, MetricIngestSuccess, map[string]string{"run_id": "run-1", "status": "ok"}, 1)
	assertSample(t, samples, MetricResultSuccess, map[string]string{"run_id": "run-1", "festa": "smoke"}, 1)
	assertSample(t, samples, MetricResultDuration, map[string]string{"run_id": "run-1"}, 2)
	assertSample(t, samples, MetricStepSuccess, map[string]string{"step": "ping", "command": "ping"}, 1)
	assertSample(t, samples, MetricStepDuration, map[string]string{"step": "ping"}, 0.12)
	assertSample(t, samples, MetricPingReceived, map[string]string{"host": "1.1.1.1"}, 3)
	assertSample(t, samples, MetricPingPacketLoss, map[string]string{"host": "1.1.1.1"}, 0)
	assertSample(t, samples, MetricDNSAnswers, map[string]string{"name": "example.com"}, 1)
	assertSample(t, samples, MetricHTTPSuccess, map[string]string{"url": "http://example.com/health"}, 1)
}

func TestIngestFailureMetrics(t *testing.T) {
	samples := IngestFailureMetrics(100*time.Millisecond, "decode_failed", errFixture("bad protobuf"))
	assertSample(t, samples, MetricIngestSuccess, map[string]string{"status": "decode_failed", "error": "bad_protobuf"}, 0)
	assertSample(t, samples, MetricIngestDuration, map[string]string{"status": "decode_failed"}, 0.1)
}

func assertSample(t *testing.T, samples []MetricSample, name string, labels map[string]string, value float64) {
	t.Helper()
	for _, sample := range samples {
		if sample.Name != name {
			continue
		}
		matched := true
		for key, want := range labels {
			if sample.Labels[key] != want {
				matched = false
				break
			}
		}
		if matched {
			if sample.Value != value {
				t.Fatalf("%s labels=%v value=%v, want %v", name, labels, sample.Value, value)
			}
			return
		}
	}
	t.Fatalf("missing sample %s labels=%v in %#v", name, labels, samples)
}

type errFixture string

func (e errFixture) Error() string { return string(e) }

func metricArchiveFixture() *controlpb.StandaloneRunArchive {
	return &controlpb.StandaloneRunArchive{
		Summary: &controlpb.StandaloneRunSummary{
			RunId:           "run-1",
			FestaName:       "smoke",
			StartedUnixMs:   1000,
			FinishedUnixMs:  3000,
			Status:          "ok",
			WifiGroupCount:  1,
			StepCount:       3,
			FailedStepCount: 0,
		},
		Device: &controlpb.DeviceInfo{
			Manufacturer: "Acme",
			Model:        "Phone",
			Device:       "phone",
		},
		Steps: []*controlpb.StandaloneMeasurementStep{
			{
				WifiGroupIndex: 1,
				WifiGroupName:  "lab",
				StepIndex:      1,
				StepName:       "ping",
				StartedUnixMs:  1000,
				FinishedUnixMs: 1200,
				Command:        &controlpb.RunCommand{Command: &controlpb.RunCommand_Ping{Ping: &controlpb.Ping{Host: "1.1.1.1"}}},
				Result: &controlpb.CommandResult{
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
					}},
				},
			},
			{
				WifiGroupIndex: 1,
				WifiGroupName:  "lab",
				StepIndex:      2,
				StepName:       "dns",
				Command:        &controlpb.RunCommand{Command: &controlpb.RunCommand_ResolveDns{ResolveDns: &controlpb.ResolveDns{Name: "example.com"}}},
				Result: &controlpb.CommandResult{
					Status: controlpb.CommandResult_STATUS_OK,
					Payload: &controlpb.CommandResult_ResolveDns{ResolveDns: &controlpb.ResolveDnsResult{
						Name:      "example.com",
						ElapsedMs: 55,
						Answers:   []*controlpb.DnsAnswer{{Type: controlpb.DnsRecordType_DNS_RECORD_TYPE_A, Address: "93.184.216.34"}},
					}},
				},
			},
			{
				WifiGroupIndex: 1,
				WifiGroupName:  "lab",
				StepIndex:      3,
				StepName:       "http",
				Command:        &controlpb.RunCommand{Command: &controlpb.RunCommand_HttpCheck{HttpCheck: &controlpb.HttpCheck{Url: "http://example.com/health"}}},
				Result: &controlpb.CommandResult{
					Status: controlpb.CommandResult_STATUS_OK,
					Payload: &controlpb.CommandResult_HttpCheck{HttpCheck: &controlpb.HttpCheckResult{
						Url:       "http://example.com/health",
						Status:    204,
						Matched:   true,
						ElapsedMs: 75,
					}},
				},
			},
		},
	}
}
