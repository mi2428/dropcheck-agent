package ingester

import (
	"math"
	"reflect"
	"strings"
	"testing"

	"dropcheck/controller/internal/controlpb"
)

func TestArchiveMetricBatchesBuildsStableWifiGroupMetrics(t *testing.T) {
	archive := metricArchiveFixture()
	batches := ArchiveMetricBatches(archive)

	if len(batches) != 1 {
		t.Fatalf("batch count = %d, want 1", len(batches))
	}
	batch := batches[0]
	assertGrouping(t, batch, map[string]string{
		"device_name":  "phone",
		"device_model": "Phone",
		"festa":        "smoke",
		"wifi_group":   "lab",
		"wifi_essid":   "Lab",
		"wifi_bssid":   "aa:bb:cc:dd:ee:ff",
	})

	assertSample(t, batch, MetricSuccess, nil, 1)
	assertSample(t, batch, MetricDuration, nil, 1.4)
	assertSample(t, batch, MetricConnectSuccess, nil, 1)
	assertSample(t, batch, MetricConnectDuration, nil, 0.22)
	assertSample(t, batch, MetricWaitConnectedSuccess, nil, 1)
	assertSample(t, batch, MetricWaitConnectedDuration, nil, 0.04)
	assertSample(t, batch, MetricDNSSuccess, map[string]string{"target": "example.com"}, 1)
	assertSample(t, batch, MetricDNSDuration, map[string]string{"target": "example.com"}, 0.055)
	assertSample(t, batch, MetricPingSuccess, map[string]string{"target": "1.1.1.1"}, 1)
	assertSample(t, batch, MetricPingDuration, map[string]string{"target": "1.1.1.1"}, 0.12)
	assertSample(t, batch, MetricHTTPSuccess, map[string]string{"target": "http://example.com/health"}, 1)
	assertSample(t, batch, MetricHTTPStatusCode, map[string]string{"target": "http://example.com/health"}, 204)
	assertSample(t, batch, MetricHTTPDuration, map[string]string{"target": "http://example.com/health"}, 0.075)
	assertMetricContract(t, batch)
}

func TestArchiveMetricBatchesSplitsWifiGroups(t *testing.T) {
	archive := metricArchiveFixture()
	archive.Festa.WifiGroups = append(archive.Festa.WifiGroups, &controlpb.StandaloneWifiGroup{
		Name:  "guest",
		Essid: "Guest",
	})
	archive.Steps = append(archive.Steps, &controlpb.StandaloneMeasurementStep{
		WifiGroupIndex: 2,
		WifiGroupName:  "guest",
		StepIndex:      6,
		StepName:       "ping",
		StartedUnixMs:  2600,
		FinishedUnixMs: 2700,
		Command: &controlpb.RunCommand{Command: &controlpb.RunCommand_Ping{Ping: &controlpb.Ping{
			Host: "8.8.8.8",
		}}},
		Result: &controlpb.CommandResult{
			Status: controlpb.CommandResult_STATUS_FAILED,
			Payload: &controlpb.CommandResult_Ping{Ping: &controlpb.PingResult{
				Host:      "8.8.8.8",
				ElapsedMs: 100,
			}},
		},
	})

	batches := ArchiveMetricBatches(archive)
	if len(batches) != 2 {
		t.Fatalf("batch count = %d, want 2", len(batches))
	}
	guest := findBatch(t, batches, "wifi_group", "guest")
	assertGrouping(t, guest, map[string]string{
		"wifi_group": "guest",
		"wifi_essid": "Guest",
		"wifi_bssid": "any",
	})
	assertSample(t, guest, MetricSuccess, nil, 0)
	assertSample(t, guest, MetricPingSuccess, map[string]string{"target": "8.8.8.8"}, 0)
	assertMetricContract(t, guest)
}

func TestArchiveMetricBatchesFallsBackToCommandWifiSelector(t *testing.T) {
	archive := &controlpb.StandaloneRunArchive{
		Summary: &controlpb.StandaloneRunSummary{
			FestaName: "selector",
			Status:    "ok",
		},
		Device: &controlpb.DeviceInfo{
			Model:  "Tablet",
			Device: "tablet",
		},
		Steps: []*controlpb.StandaloneMeasurementStep{{
			WifiGroupIndex: 3,
			StepIndex:      1,
			StepName:       "dns",
			Command: &controlpb.RunCommand{Command: &controlpb.RunCommand_ResolveDns{ResolveDns: &controlpb.ResolveDns{
				Name:     "example.net",
				Selector: &controlpb.NetworkSelector{Ssid: "Cafe"},
			}}},
			Result: &controlpb.CommandResult{
				Status: controlpb.CommandResult_STATUS_OK,
				Payload: &controlpb.CommandResult_ResolveDns{ResolveDns: &controlpb.ResolveDnsResult{
					Name:      "example.net",
					ElapsedMs: 33,
				}},
			},
		}},
	}

	batches := ArchiveMetricBatches(archive)
	if len(batches) != 1 {
		t.Fatalf("batch count = %d, want 1", len(batches))
	}
	assertGrouping(t, batches[0], map[string]string{
		"device_name":  "tablet",
		"device_model": "Tablet",
		"festa":        "selector",
		"wifi_group":   "wifi_group_3",
		"wifi_essid":   "Cafe",
		"wifi_bssid":   "any",
	})
	assertSample(t, batches[0], MetricDNSSuccess, map[string]string{"target": "example.net"}, 1)
	assertMetricContract(t, batches[0])
}

func assertGrouping(t *testing.T, batch MetricBatch, labels map[string]string) {
	t.Helper()
	for key, want := range labels {
		if got := batch.Grouping[key]; got != want {
			t.Fatalf("grouping[%s] = %q, want %q; grouping=%v", key, got, want, batch.Grouping)
		}
	}
	for _, forbidden := range []string{"run_id", "object_key", "step", "command", "result_status", "wifi_band", "wifi_security"} {
		if _, ok := batch.Grouping[forbidden]; ok {
			t.Fatalf("forbidden grouping label %q present in %v", forbidden, batch.Grouping)
		}
	}
}

func assertSample(t *testing.T, batch MetricBatch, name string, labels map[string]string, value float64) {
	t.Helper()
	if labels == nil {
		labels = map[string]string{}
	}
	for _, sample := range batch.Samples {
		if sample.Name != name || !sameLabels(sample.Labels, labels) {
			continue
		}
		if math.Abs(sample.Value-value) > 1e-9 {
			t.Fatalf("%s labels=%v value=%v, want %v", name, labels, sample.Value, value)
		}
		return
	}
	t.Fatalf("missing sample %s labels=%v in %#v", name, labels, batch.Samples)
}

func findBatch(t *testing.T, batches []MetricBatch, label string, value string) MetricBatch {
	t.Helper()
	for _, batch := range batches {
		if batch.Grouping[label] == value {
			return batch
		}
	}
	t.Fatalf("missing batch with %s=%q in %#v", label, value, batches)
	return MetricBatch{}
}

func sameLabels(got map[string]string, want map[string]string) bool {
	if got == nil {
		got = map[string]string{}
	}
	return reflect.DeepEqual(got, want)
}

func assertMetricContract(t *testing.T, batch MetricBatch) {
	t.Helper()
	for _, sample := range batch.Samples {
		if !strings.HasPrefix(sample.Name, "dropcheck_") {
			t.Fatalf("metric %q does not use dropcheck_ prefix", sample.Name)
		}
		if strings.Contains(sample.Name, "_festival_") {
			t.Fatalf("metric %q still contains _festival_", sample.Name)
		}
		if !strings.HasSuffix(sample.Name, "_success") &&
			!strings.HasSuffix(sample.Name, "_duration_seconds") &&
			!strings.HasSuffix(sample.Name, "_status_code") {
			t.Fatalf("metric %q has unsupported suffix", sample.Name)
		}
		for _, forbidden := range []string{"run_id", "object_key", "step", "command", "result_status", "wifi_band", "wifi_security"} {
			if _, ok := sample.Labels[forbidden]; ok {
				t.Fatalf("metric %q has forbidden label %q in %v", sample.Name, forbidden, sample.Labels)
			}
		}
		for label := range sample.Labels {
			if label != "target" {
				t.Fatalf("metric %q has unexpected label %q in %v", sample.Name, label, sample.Labels)
			}
		}
	}
}

func metricArchiveFixture() *controlpb.StandaloneRunArchive {
	const (
		started = int64(1000)
		group   = "lab"
	)
	return &controlpb.StandaloneRunArchive{
		Summary: &controlpb.StandaloneRunSummary{
			RunId:           "run-1",
			FestaName:       "smoke",
			StartedUnixMs:   started,
			FinishedUnixMs:  started + 2000,
			Status:          "ok",
			WifiGroupCount:  1,
			StepCount:       5,
			FailedStepCount: 0,
		},
		Device: &controlpb.DeviceInfo{
			Manufacturer: "Acme",
			Model:        "Phone",
			Device:       "phone",
		},
		Festa: &controlpb.StandaloneFesta{
			Name: "smoke",
			WifiGroups: []*controlpb.StandaloneWifiGroup{{
				Name:  group,
				Essid: "Lab",
				Bssid: "aa:bb:cc:dd:ee:ff",
			}},
		},
		Steps: []*controlpb.StandaloneMeasurementStep{
			{
				WifiGroupIndex: 1,
				WifiGroupName:  group,
				StepIndex:      1,
				StepName:       "connect",
				StartedUnixMs:  started,
				FinishedUnixMs: started + 220,
				Command: &controlpb.RunCommand{Command: &controlpb.RunCommand_ConnectWifi{ConnectWifi: &controlpb.ConnectWifi{
					Ssid:  "Lab",
					Bssid: "aa:bb:cc:dd:ee:ff",
				}}},
				Result: &controlpb.CommandResult{
					Status:    controlpb.CommandResult_STATUS_OK,
					ElapsedMs: 220,
					Payload: &controlpb.CommandResult_ConnectWifi{ConnectWifi: &controlpb.ConnectWifiResult{
						Ssid:      "Lab",
						Connected: true,
					}},
				},
			},
			{
				WifiGroupIndex: 1,
				WifiGroupName:  group,
				StepIndex:      2,
				StepName:       "wait_connected",
				StartedUnixMs:  started + 300,
				FinishedUnixMs: started + 360,
				Command: &controlpb.RunCommand{Command: &controlpb.RunCommand_WaitWifiConnected{WaitWifiConnected: &controlpb.WaitWifiConnected{
					Ssid:  "Lab",
					Bssid: "aa:bb:cc:dd:ee:ff",
				}}},
				Result: &controlpb.CommandResult{
					Status:    controlpb.CommandResult_STATUS_OK,
					ElapsedMs: 60,
					Payload: &controlpb.CommandResult_WifiAssert{WifiAssert: &controlpb.WifiAssertResult{
						Passed:    true,
						ElapsedMs: 40,
					}},
				},
			},
			{
				WifiGroupIndex: 1,
				WifiGroupName:  group,
				StepIndex:      3,
				StepName:       "dns",
				StartedUnixMs:  started + 500,
				FinishedUnixMs: started + 555,
				Command: &controlpb.RunCommand{Command: &controlpb.RunCommand_ResolveDns{ResolveDns: &controlpb.ResolveDns{
					Name:     "example.com",
					Selector: &controlpb.NetworkSelector{Ssid: "Lab"},
				}}},
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
				WifiGroupName:  group,
				StepIndex:      4,
				StepName:       "ping",
				StartedUnixMs:  started + 700,
				FinishedUnixMs: started + 820,
				Command: &controlpb.RunCommand{Command: &controlpb.RunCommand_Ping{Ping: &controlpb.Ping{
					Host:     "1.1.1.1",
					Selector: &controlpb.NetworkSelector{Ssid: "Lab"},
				}}},
				Result: &controlpb.CommandResult{
					Status: controlpb.CommandResult_STATUS_OK,
					Payload: &controlpb.CommandResult_Ping{Ping: &controlpb.PingResult{
						Host:              "1.1.1.1",
						Transmitted:       3,
						Received:          3,
						PacketLossPercent: 0,
						ElapsedMs:         120,
					}},
				},
			},
			{
				WifiGroupIndex: 1,
				WifiGroupName:  group,
				StepIndex:      5,
				StepName:       "http",
				StartedUnixMs:  started + 1300,
				FinishedUnixMs: started + 1400,
				Command: &controlpb.RunCommand{Command: &controlpb.RunCommand_HttpCheck{HttpCheck: &controlpb.HttpCheck{
					Url:      "http://example.com/health",
					Selector: &controlpb.NetworkSelector{Ssid: "Lab"},
				}}},
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
