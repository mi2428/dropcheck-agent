package festival_test

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"dropcheck/controller/internal/command"
	"dropcheck/controller/internal/control"
	"dropcheck/controller/internal/controlpb"
	"dropcheck/controller/internal/festival"
	"dropcheck/controller/internal/festival/dns"
	"dropcheck/controller/internal/festival/globalip"
	"dropcheck/controller/internal/festival/ping"
	"dropcheck/controller/internal/festival/pmtu"
	"dropcheck/controller/internal/festival/trace"
	"dropcheck/controller/internal/runner"
)

func TestRunExecutesNetworkAndChecksWithInjectedRunner(t *testing.T) {
	fake := &fakeRunner{}
	festival.Run(t, festival.Plan{
		Networks: []festival.Network{
			festival.WiFi("lab-5g").
				SSID("Lab").
				PSK("secret").
				BSSID("aa:bb:cc:dd:ee:ff"),
		},
		Checks: []festival.Check{
			festival.Ping("8.8.8.8").
				Count(5).
				Expect(
					ping.Received().Ge(5),
					ping.LossPercent().Eq(0),
					ping.AvgLatency().Gt(time.Millisecond).Le(50*time.Millisecond),
					ping.Assert("stable latency", func(r ping.Result) error {
						if r.MaxLatency > r.AvgLatency*3 {
							t.Fatalf("unexpected test fixture: max latency should be stable")
						}
						return nil
					}),
				),
			festival.DNS("example.com").
				A().
				Expect(dns.AnswerCount().Ge(1), dns.Elapsed().Le(time.Second)),
			festival.GlobalIP().
				IPv4().
				Expect(globalip.AddressCount().Ge(1)),
		},
	}, festival.WithRunner(fake, control.AgentInfo{ID: "agent-1"}))

	want := []string{"wifi.connect", "wifi.wait", "ping", "dns", "global-ip", "wifi.disconnect"}
	if !reflect.DeepEqual(fake.operations, want) {
		t.Fatalf("operations = %#v, want %#v", fake.operations, want)
	}
}

func TestRunBuildsSupportedCheckOperations(t *testing.T) {
	fake := &fakeRunner{}
	festival.Run(t, festival.Plan{
		Name: "supported-checks",
		Networks: []festival.Network{
			festival.WiFi("lab").
				SSID("Lab").
				PSK("secret").
				WaitConnected(false).
				DisconnectAfter(false),
		},
		Checks: []festival.Check{
			festival.PathMTU("8.8.8.8").
				Min(1200).
				Expect(pmtu.Discovered().IsTrue(), pmtu.PathMTU().Ge(1200)),
			festival.Traceroute("8.8.8.8").
				MaxHops(30).
				Expect(trace.OutputContains("8.8.8.8")),
			festival.HTTP("example.com").
				ExpectedStatus(200).
				Expect(festival.Assert("http matched", func(r festival.Result) error {
					if !r.Run.Raw.GetHttpCheck().GetMatched() {
						return fmt.Errorf("http check did not match")
					}
					return nil
				})),
			festival.Download("https://example.com").
				Expect(festival.Assert("downloaded bytes", func(r festival.Result) error {
					if r.Run.Raw.GetWget().GetBytesRead() == 0 {
						return fmt.Errorf("download read no bytes")
					}
					return nil
				})),
		},
	}, festival.WithRunner(fake, control.AgentInfo{ID: "agent-1"}))

	want := []string{"wifi.connect", "path-mtu", "traceroute", "http", "download"}
	if !reflect.DeepEqual(fake.operations, want) {
		t.Fatalf("operations = %#v, want %#v", fake.operations, want)
	}
}

func TestPingMatcherReportsFailedConstraint(t *testing.T) {
	expectation := ping.Received().Ge(5)
	findings := expectation.Evaluate(festival.Result{
		Check: "ping 8.8.8.8",
		Run: festival.RunResult{Raw: &controlpb.CommandResult{
			Status: controlpb.CommandResult_STATUS_OK,
			Payload: &controlpb.CommandResult_Ping{Ping: &controlpb.PingResult{
				Host:        "8.8.8.8",
				Count:       5,
				Transmitted: 5,
				Received:    4,
			}},
		}},
	})
	if len(findings) != 1 {
		t.Fatalf("findings len = %d, want 1", len(findings))
	}
	if findings[0].Passed {
		t.Fatalf("finding passed, want failed: %#v", findings[0])
	}
	if findings[0].Metric != "ping.received" || findings[0].Observed != "4" || findings[0].Expected != ">= 5" {
		t.Fatalf("finding = %#v", findings[0])
	}
}

func TestMatcherReportsMissingPayload(t *testing.T) {
	findings := dns.AnswerCount().Ge(1).Evaluate(festival.Result{
		Check: "dns example.com",
		Run: festival.RunResult{Raw: &controlpb.CommandResult{
			Status:  controlpb.CommandResult_STATUS_OK,
			Payload: &controlpb.CommandResult_Ping{Ping: &controlpb.PingResult{}},
		}},
	})
	if len(findings) != 1 {
		t.Fatalf("findings len = %d, want 1", len(findings))
	}
	if findings[0].Passed || findings[0].Observed != "<missing>" {
		t.Fatalf("finding = %#v, want missing failure", findings[0])
	}
}

type fakeRunner struct {
	operations []string
}

func (r *fakeRunner) Run(_ context.Context, _ control.AgentInfo, op command.Operation) (runner.Result, error) {
	r.operations = append(r.operations, op.Name)
	return runner.Result{Operation: op, Result: fakeResult(op.Name)}, nil
}

func fakeResult(name string) *controlpb.CommandResult {
	result := &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_OK}
	switch name {
	case "ping":
		result.Payload = &controlpb.CommandResult_Ping{Ping: &controlpb.PingResult{
			Host:              "8.8.8.8",
			Count:             5,
			Transmitted:       5,
			Received:          5,
			PacketLossPercent: 0,
			MinMs:             10,
			AvgMs:             20,
			MaxMs:             40,
			ElapsedMs:         120,
		}}
	case "dns":
		result.Payload = &controlpb.CommandResult_ResolveDns{ResolveDns: &controlpb.ResolveDnsResult{
			Name:      "example.com",
			ElapsedMs: 80,
			Answers: []*controlpb.DnsAnswer{{
				Type:    controlpb.DnsRecordType_DNS_RECORD_TYPE_A,
				Address: "93.184.216.34",
			}},
		}}
	case "global-ip":
		result.Payload = &controlpb.CommandResult_GlobalIp{GlobalIp: &controlpb.GlobalIpResult{
			RequestedFamily: controlpb.IpFamily_IP_FAMILY_IPV4,
			ElapsedMs:       100,
			Addresses: []*controlpb.GlobalIpAddress{{
				Family: controlpb.IpFamily_IP_FAMILY_IPV4,
				Ip:     "203.0.113.10",
				Global: true,
				Status: 200,
			}},
		}}
	case "path-mtu":
		result.Payload = &controlpb.CommandResult_PathMtu{PathMtu: &controlpb.PathMtuResult{
			Host:         "8.8.8.8",
			Discovered:   true,
			PathMtuBytes: 1400,
			Probes: []*controlpb.PathMtuProbe{{
				MtuBytes: 1400,
				Passed:   true,
			}},
		}}
	case "traceroute":
		result.Payload = &controlpb.CommandResult_Traceroute{Traceroute: &controlpb.TracerouteResult{
			Host:      "8.8.8.8",
			MaxHops:   30,
			Output:    "1 192.0.2.1 1.0 ms\n2 8.8.8.8 5.0 ms\n",
			ElapsedMs: 500,
		}}
	case "http":
		result.Payload = &controlpb.CommandResult_HttpCheck{HttpCheck: &controlpb.HttpCheckResult{
			Url:            "https://example.com",
			Status:         200,
			ExpectedStatus: 200,
			Matched:        true,
			ElapsedMs:      100,
		}}
	case "download":
		result.Payload = &controlpb.CommandResult_Wget{Wget: &controlpb.WgetResult{
			Url:       "https://example.com",
			Status:    200,
			BytesRead: 1024,
			ElapsedMs: 100,
		}}
	default:
		result.Message = name
	}
	return result
}
