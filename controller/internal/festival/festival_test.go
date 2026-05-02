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
	"dropcheck/controller/internal/festival/capabilities"
	"dropcheck/controller/internal/festival/dns"
	"dropcheck/controller/internal/festival/globalip"
	"dropcheck/controller/internal/festival/ip"
	"dropcheck/controller/internal/festival/ping"
	"dropcheck/controller/internal/festival/pmtu"
	"dropcheck/controller/internal/festival/scan"
	"dropcheck/controller/internal/festival/trace"
	"dropcheck/controller/internal/festival/wifi"
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
			festival.IPStatus().
				Expect(
					ip.Validated().IsTrue(),
					ip.Internet().IsTrue(),
					ip.IPv4Address().InCIDR("192.168.10.0/24"),
					ip.IPv6Address().Scope("link-local"),
					ip.IPv6Address().Scope("global"),
					ip.IPv6Prefix().Within("2001:db8::/32"),
					ip.DHCPServer().Eq("192.168.10.1"),
					ip.DNSServer().Contains("192.168.10.1"),
					ip.IPv4DNSServerCount().Eq(1),
					ip.IPv6DNSServerCount().Eq(1),
					ip.IPv6DNSServerAddress().InCIDR("2001:4860:4860::/48"),
					ip.Transport().Contains("wifi"),
					ip.IPv6DefaultRoute().IsTrue(),
					ip.DefaultParsedRoute().Gateway("192.168.10.1").Interface("wlan0").Exists(),
					ip.IPv6ParsedRoute().Gateway("fe80::1").Interface("wlan0").Exists(),
					ip.MTU().Ge(1280),
					ip.PrivateDNSActive().IsTrue(),
					ip.PrivateDNSServerName().Eq("dns.example"),
				),
			festival.WiFiStatus().
				Expect(
					wifi.Enabled().IsTrue(),
					wifi.SSID().Eq("Lab"),
					wifi.BSSID().Eq("aa:bb:cc:dd:ee:ff"),
					wifi.Standard().Eq("be"),
					wifi.Channel().Eq(37),
					wifi.Band().Eq("6ghz"),
					wifi.ChannelWidth().Eq("160mhz"),
					wifi.TxLinkSpeedMbps().Ge(1000),
				),
			festival.WiFiScan().
				Fresh().
				Band("6ghz").
				Timeout(5*time.Second).
				Expect(
					scan.ResultCount().Ge(1),
					scan.APs().
						SSID("Lab").
						BSSID("aa:bb:cc:dd:ee:ff").
						Standard("be").
						ChannelWidth("320mhz").
						Channel(37).
						Security("wpa3_sae").
						Exists(),
				),
			festival.WiFiCapabilities().
				Expect(
					capabilities.Band("6ghz").Supported(),
					capabilities.Standard("be").Supported(),
					capabilities.Security("wpa3_sae").Supported(),
					capabilities.ErrorCount().Eq(0),
				),
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

	want := []string{"wifi.connect", "ip.status", "wifi.status", "wifi.scan.fresh", "wifi.capabilities", "path-mtu", "traceroute", "http", "download"}
	if !reflect.DeepEqual(fake.operations, want) {
		t.Fatalf("operations = %#v, want %#v", fake.operations, want)
	}
}

func TestRunRetriesAndRepeatsChecks(t *testing.T) {
	fake := &retryRunner{}
	festival.Run(t, festival.Plan{
		Networks: []festival.Network{
			festival.WiFi("lab").
				SSID("Lab").
				PSK("secret").
				WaitConnected(false).
				DisconnectAfter(false),
		},
		Checks: []festival.Check{
			festival.Ping("8.8.8.8").
				Count(5).
				Retry(2, 0).
				Repeat(2).
				Expect(ping.Received().Eq(5)),
		},
	}, festival.WithRunner(fake, control.AgentInfo{ID: "agent-1"}))

	want := []string{"wifi.connect", "ping", "ping", "ping"}
	if !reflect.DeepEqual(fake.operations, want) {
		t.Fatalf("operations = %#v, want %#v", fake.operations, want)
	}
}

func TestRunSamplesStableChecks(t *testing.T) {
	fake := &fakeRunner{}
	festival.Run(t, festival.Plan{
		Networks: []festival.Network{
			festival.WiFi("lab").
				SSID("Lab").
				PSK("secret").
				WaitConnected(false).
				DisconnectAfter(false),
		},
		Checks: []festival.Check{
			festival.Ping("8.8.8.8").
				Count(5).
				StableFor(3 * time.Millisecond).
				StableInterval(time.Millisecond).
				Expect(ping.Received().Eq(5)),
		},
	}, festival.WithRunner(fake, control.AgentInfo{ID: "agent-1"}))

	pingRuns := 0
	for _, operation := range fake.operations {
		if operation == "ping" {
			pingRuns++
		}
	}
	if pingRuns < 2 {
		t.Fatalf("ping runs = %d, want at least 2; operations = %#v", pingRuns, fake.operations)
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

func TestIPMatcherReportsCIDRMismatch(t *testing.T) {
	findings := ip.IPv4Prefix().Within("10.0.0.0/8").Evaluate(festival.Result{
		Check: "ip status",
		Run:   festival.RunResult{Raw: fakeResult("ip.status")},
	})
	if len(findings) != 1 {
		t.Fatalf("findings len = %d, want 1", len(findings))
	}
	if findings[0].Passed || findings[0].Metric != "ip.ipv4_prefix" {
		t.Fatalf("finding = %#v, want failed ipv4 prefix finding", findings[0])
	}
}

func TestWiFiMatcherRequiresWiFiStatusPayload(t *testing.T) {
	findings := wifi.Standard().Eq("be").Evaluate(festival.Result{
		Check: "ip status",
		Run:   festival.RunResult{Raw: fakeResult("ip.status")},
	})
	if len(findings) != 1 {
		t.Fatalf("findings len = %d, want 1", len(findings))
	}
	if findings[0].Passed || findings[0].Observed != "<missing>" {
		t.Fatalf("finding = %#v, want missing wifi status failure", findings[0])
	}
}

func TestScanMatcherReportsMissingAP(t *testing.T) {
	findings := scan.APs().SSID("missing").Exists().Evaluate(festival.Result{
		Check: "wifi scan fresh",
		Run:   festival.RunResult{Raw: fakeResult("wifi.scan.fresh")},
	})
	if len(findings) != 1 {
		t.Fatalf("findings len = %d, want 1", len(findings))
	}
	if findings[0].Passed || findings[0].Metric != "scan.ap" {
		t.Fatalf("finding = %#v, want failed scan AP finding", findings[0])
	}
}

type fakeRunner struct {
	operations []string
}

func (r *fakeRunner) Run(_ context.Context, _ control.AgentInfo, op command.Operation) (runner.Result, error) {
	r.operations = append(r.operations, op.Name)
	return runner.Result{Operation: op, Result: fakeResult(op.Name)}, nil
}

type retryRunner struct {
	operations []string
	pingCalls  int
}

func (r *retryRunner) Run(_ context.Context, _ control.AgentInfo, op command.Operation) (runner.Result, error) {
	r.operations = append(r.operations, op.Name)
	if op.Name == "ping" {
		r.pingCalls++
		if r.pingCalls == 1 {
			return runner.Result{Operation: op, Result: &controlpb.CommandResult{
				Status:  controlpb.CommandResult_STATUS_FAILED,
				Message: "temporary ping failure",
			}}, nil
		}
	}
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
	case "ip.status":
		result.Payload = &controlpb.CommandResult_IpStatus{IpStatus: &controlpb.IpStatus{
			NetworkId:            "100",
			Transports:           []string{"wifi"},
			Validated:            true,
			Internet:             true,
			InterfaceName:        "wlan0",
			Mtu:                  1500,
			Addresses:            []string{"fe80::123/64", "192.168.10.23/24", "2001:db8:1::123/64"},
			DnsServers:           []string{"192.168.10.1", "2001:4860:4860::8888"},
			DhcpServer:           "192.168.10.1",
			Routes:               []string{"0.0.0.0/0 -> 192.168.10.1 wlan0", "::/0 -> fe80::1 wlan0"},
			Capabilities:         []string{"internet", "validated"},
			Nat64Prefix:          "64:ff9b::/96",
			PrivateDnsActive:     true,
			PrivateDnsServerName: "dns.example",
			RawLinkProperties:    "LinkProperties{LinkAddresses: [192.168.10.23/24,2001:db8:1::123/64] Routes: [::/0]}",
		}}
	case "wifi.status":
		result.Payload = &controlpb.CommandResult_WifiStatus{WifiStatus: &controlpb.WifiStatus{
			Enabled: true,
			State:   "enabled",
			Connection: &controlpb.WifiConnection{
				Ssid:               "Lab",
				Bssid:              "aa:bb:cc:dd:ee:ff",
				RssiDbm:            -45,
				FrequencyMhz:       6135,
				LinkSpeedMbps:      2401,
				TxLinkSpeedMbps:    2401,
				RxLinkSpeedMbps:    2401,
				WifiStandard:       "802.11be",
				ChannelWidth:       "160MHz",
				SecurityType:       "wpa3_sae",
				AssociatedMloLinks: []*controlpb.MloLinkInfo{{LinkId: 1, Band: "6ghz", Channel: 37}},
				Raw:                "ssid=Lab channelWidth=160MHz",
			},
		}}
	case "wifi.scan.fresh", "wifi.scan":
		result.Payload = &controlpb.CommandResult_WifiScan{WifiScan: &controlpb.WifiScan{
			Results: []*controlpb.WifiScanResult{{
				Ssid:          "Lab",
				Bssid:         "aa:bb:cc:dd:ee:ff",
				Capabilities:  "[RSN-SAE-CCMP][EHT][ESS]",
				RssiDbm:       -41,
				FrequencyMhz:  6135,
				Band:          "6GHz",
				ChannelWidth:  "320MHz",
				WifiStandard:  "802.11be",
				SecurityTypes: []string{"wpa3_sae"},
			}},
		}}
	case "wifi.capabilities":
		result.Payload = &controlpb.CommandResult_WifiCapabilities{WifiCapabilities: &controlpb.WifiCapabilities{
			SupportedBands:         []string{"2.4GHz", "5GHz", "6GHz"},
			SupportedStandards:     []string{"802.11ax", "802.11be"},
			SupportedSecurityModes: []string{"wpa3_sae"},
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
