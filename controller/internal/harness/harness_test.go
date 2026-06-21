package harness_test

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"dropcheck/controller/internal/command"
	"dropcheck/controller/internal/control"
	"dropcheck/controller/internal/controlpb"
	"dropcheck/controller/internal/harness"
	"dropcheck/controller/internal/harness/capabilities"
	"dropcheck/controller/internal/harness/dns"
	"dropcheck/controller/internal/harness/globalip"
	"dropcheck/controller/internal/harness/ip"
	"dropcheck/controller/internal/harness/mlo"
	"dropcheck/controller/internal/harness/ping"
	"dropcheck/controller/internal/harness/pmtu"
	"dropcheck/controller/internal/harness/scan"
	"dropcheck/controller/internal/harness/trace"
	"dropcheck/controller/internal/harness/wifi"
	"dropcheck/controller/internal/runner"
	"google.golang.org/protobuf/proto"
)

func TestRunExecutesNetworkAndChecksWithInjectedRunner(t *testing.T) {
	fake := &fakeRunner{}
	harness.Run(t, harness.Plan{
		Networks: []harness.Network{
			harness.WiFi("lab-5g").
				SSID("Lab").
				PSK("secret").
				BSSID("aa:bb:cc:dd:ee:ff"),
		},
		Checks: []harness.Check{
			harness.Ping("8.8.8.8").
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
			harness.DNS("example.com").
				A().
				Expect(dns.AnswerCount().Ge(1), dns.Elapsed().Le(time.Second)),
			harness.GlobalIP().
				IPv4().
				Expect(globalip.AddressCount().Ge(1)),
		},
	}, harness.WithRunner(fake, control.AgentInfo{ID: "agent-1"}))

	want := []string{"wifi.connect", "wifi.wait", "ping", "dns", "global-ip", "wifi.disconnect"}
	if !reflect.DeepEqual(fake.operations, want) {
		t.Fatalf("operations = %#v, want %#v", fake.operations, want)
	}
}

func TestRunBuildsSupportedCheckOperations(t *testing.T) {
	fake := &fakeRunner{}
	harness.Run(t, harness.Plan{
		Name: "supported-checks",
		Networks: []harness.Network{
			harness.WiFi("lab").
				SSID("Lab").
				PSK("secret").
				WaitConnected(false).
				DisconnectAfter(false),
		},
		Checks: []harness.Check{
			harness.IPStatus().
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
			harness.WiFiStatus().
				Expect(
					wifi.Enabled().IsTrue(),
					wifi.SSID().Eq("Lab"),
					wifi.BSSID().Eq("aa:bb:cc:dd:ee:ff"),
					wifi.Standard().Eq("be"),
					wifi.Channel().Eq(37),
					wifi.Band().Eq("6ghz"),
					wifi.TxLinkSpeedMbps().Ge(1000),
					wifi.MLOPresent().IsTrue(),
					wifi.APMLDMAC().Eq("02:00:00:00:00:01"),
					wifi.APMLOLinkID().Eq(1),
					wifi.AssociatedMLOLinkCount().Ge(1),
					wifi.AffiliatedMLOLinkCount().Ge(1),
					wifi.AssociatedMLOLink().ID(1).Band("6ghz").State("active").Exists(),
				),
			harness.WiFiScan().
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
						MLOCapable().
						APMLDMAC("02:00:00:00:00:01").
						MLOLinkID(1).
						AffiliatedMLOLinkID(2).
						Exists(),
				),
			harness.WiFiEHT().
				Expect(
					mlo.Connected().Present(),
					mlo.Connected().APMLD().Known(),
					mlo.Connected().APMLOLinkID().Eq(1),
					mlo.Connected().AssociatedLinkCount().Ge(1),
					mlo.Scan().CandidateCount().Ge(1),
					mlo.Scan().Group().
						SSID("Lab").
						APMLD("02:00:00:00:00:01").
						LinkCount().Ge(2),
					mlo.CurrentRelation().ConnectedMLDSeenInScan().IsTrue(),
					mlo.CurrentRelation().AssociatedLinksCoveredByScan().IsTrue(),
					mlo.Metadata().Complete().IsTrue(),
				),
			harness.WiFiCapabilities().
				Expect(
					capabilities.Band("6ghz").Supported(),
					capabilities.Standard("be").Supported(),
					capabilities.Security("wpa3_sae").Supported(),
					capabilities.ErrorCount().Eq(0),
				),
			harness.PathMTU("8.8.8.8").
				Min(1200).
				Expect(pmtu.Discovered().IsTrue(), pmtu.PathMTU().Ge(1200)),
			harness.Traceroute("8.8.8.8").
				MaxHops(30).
				Expect(trace.OutputContains("8.8.8.8")),
			harness.HTTP("example.com").
				ExpectedStatus(200).
				Expect(harness.Assert("http matched", func(r harness.Result) error {
					if !r.Run.Raw.GetHttpCheck().GetMatched() {
						return fmt.Errorf("http check did not match")
					}
					return nil
				})),
			harness.Download("https://example.com").
				Expect(harness.Assert("downloaded bytes", func(r harness.Result) error {
					if r.Run.Raw.GetWget().GetBytesRead() == 0 {
						return fmt.Errorf("download read no bytes")
					}
					return nil
				})),
		},
	}, harness.WithRunner(fake, control.AgentInfo{ID: "agent-1"}))

	want := []string{"wifi.connect", "ip.status", "wifi.status", "wifi.scan.fresh", "wifi.eht", "wifi.capabilities", "path-mtu", "traceroute", "http", "download"}
	if !reflect.DeepEqual(fake.operations, want) {
		t.Fatalf("operations = %#v, want %#v", fake.operations, want)
	}
}

func TestRunRetriesAndRepeatsChecks(t *testing.T) {
	fake := &retryRunner{}
	harness.Run(t, harness.Plan{
		Networks: []harness.Network{
			harness.WiFi("lab").
				SSID("Lab").
				PSK("secret").
				WaitConnected(false).
				DisconnectAfter(false),
		},
		Checks: []harness.Check{
			harness.Ping("8.8.8.8").
				Count(5).
				Retry(2, 0).
				Repeat(2).
				Expect(ping.Received().Eq(5)),
		},
	}, harness.WithRunner(fake, control.AgentInfo{ID: "agent-1"}))

	want := []string{"wifi.connect", "ping", "ping", "ping"}
	if !reflect.DeepEqual(fake.operations, want) {
		t.Fatalf("operations = %#v, want %#v", fake.operations, want)
	}
}

func TestRunSamplesStableChecks(t *testing.T) {
	fake := &fakeRunner{}
	harness.Run(t, harness.Plan{
		Networks: []harness.Network{
			harness.WiFi("lab").
				SSID("Lab").
				PSK("secret").
				WaitConnected(false).
				DisconnectAfter(false),
		},
		Checks: []harness.Check{
			harness.Ping("8.8.8.8").
				Count(5).
				StableFor(3 * time.Millisecond).
				StableInterval(time.Millisecond).
				Expect(ping.Received().Eq(5)),
		},
	}, harness.WithRunner(fake, control.AgentInfo{ID: "agent-1"}))

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

func TestRunEvaluatesStandaloneArchiveResults(t *testing.T) {
	archive := standaloneArchiveFixture()
	harness.Run(t, harness.Plan{
		Results: []harness.ResultSource{
			harness.StandaloneArchive("standalone-smoke", archive),
		},
		Checks: []harness.Check{
			harness.Ping("8.8.8.8").
				Count(3).
				Expect(
					ping.Received().Ge(3),
					ping.LossPercent().Eq(0),
					ping.AvgLatency().Le(50*time.Millisecond),
				),
			harness.DNS("example.com").
				A().
				Expect(dns.AnswerCount().Ge(1), dns.Elapsed().Le(time.Second)),
			harness.HTTP("http://example.com/health").
				ExpectedStatus(204).
				Expect(harness.Assert("http matched", func(r harness.Result) error {
					if !r.Run.Raw.GetHttpCheck().GetMatched() {
						return fmt.Errorf("http check did not match")
					}
					return nil
				})),
		},
	})
}

func TestStandaloneArchiveResultSourcesLoadProtobufBinary(t *testing.T) {
	archive := standaloneArchiveFixture()
	data, err := proto.Marshal(archive)
	if err != nil {
		t.Fatalf("marshal archive: %v", err)
	}

	targets, err := harness.StandaloneArchiveBytes("bytes", data).Targets()
	if err != nil {
		t.Fatalf("StandaloneArchiveBytes targets: %v", err)
	}
	if len(targets) != 1 || targets[0].Name != "lab" || len(targets[0].Steps) != 5 {
		t.Fatalf("byte targets = %#v", targets)
	}

	path := t.TempDir() + "/standalone.pb"
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write archive: %v", err)
	}
	targets, err = harness.StandaloneArchiveFile(path).Targets()
	if err != nil {
		t.Fatalf("StandaloneArchiveFile targets: %v", err)
	}
	if len(targets) != 1 || targets[0].SourceName != "standalone.pb" {
		t.Fatalf("file targets = %#v", targets)
	}
}

func TestPingMatcherReportsFailedConstraint(t *testing.T) {
	expectation := ping.Received().Ge(5)
	findings := expectation.Evaluate(harness.Result{
		Check: "ping 8.8.8.8",
		Run: harness.RunResult{Raw: &controlpb.CommandResult{
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
	findings := dns.AnswerCount().Ge(1).Evaluate(harness.Result{
		Check: "dns example.com",
		Run: harness.RunResult{Raw: &controlpb.CommandResult{
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
	findings := ip.IPv4Prefix().Within("10.0.0.0/8").Evaluate(harness.Result{
		Check: "ip status",
		Run:   harness.RunResult{Raw: fakeResult("ip.status")},
	})
	if len(findings) != 1 {
		t.Fatalf("findings len = %d, want 1", len(findings))
	}
	if findings[0].Passed || findings[0].Metric != "ip.ipv4_prefix" {
		t.Fatalf("finding = %#v, want failed ipv4 prefix finding", findings[0])
	}
}

func TestIPRouteMatcherRejectsInvalidSelectorValues(t *testing.T) {
	for name, expectation := range map[string]harness.Expectation{
		"destination": ip.ParsedRoute().Destination("not-a-cidr").Exists(),
		"gateway":     ip.ParsedRoute().Gateway("not-an-ip").Exists(),
	} {
		t.Run(name, func(t *testing.T) {
			findings := expectation.Evaluate(harness.Result{
				Check: "ip status",
				Run:   harness.RunResult{Raw: fakeResult("ip.status")},
			})
			if len(findings) != 1 {
				t.Fatalf("findings len = %d, want 1", len(findings))
			}
			if findings[0].Passed || findings[0].Observed != "<invalid selector>" {
				t.Fatalf("finding = %#v, want invalid selector failure", findings[0])
			}
		})
	}
}

func TestWiFiMatcherRequiresWiFiStatusPayload(t *testing.T) {
	findings := wifi.Standard().Eq("be").Evaluate(harness.Result{
		Check: "ip status",
		Run:   harness.RunResult{Raw: fakeResult("ip.status")},
	})
	if len(findings) != 1 {
		t.Fatalf("findings len = %d, want 1", len(findings))
	}
	if findings[0].Passed || findings[0].Observed != "<missing>" {
		t.Fatalf("finding = %#v, want missing wifi status failure", findings[0])
	}
}

func TestScanMatcherReportsMissingAP(t *testing.T) {
	findings := scan.APs().SSID("missing").Exists().Evaluate(harness.Result{
		Check: "wifi scan fresh",
		Run:   harness.RunResult{Raw: fakeResult("wifi.scan.fresh")},
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

func standaloneArchiveFixture() *controlpb.StandaloneRunArchive {
	const group = "lab"
	return &controlpb.StandaloneRunArchive{
		Summary: &controlpb.StandaloneRunSummary{
			RunId:           "run-1",
			FestaName:       "smoke",
			Status:          "ok",
			WifiGroupCount:  1,
			StepCount:       5,
			FailedStepCount: 0,
		},
		Festa: &controlpb.StandaloneFesta{
			Name: "smoke",
			WifiGroups: []*controlpb.StandaloneWifiGroup{{
				Name:       group,
				Essid:      "Lab",
				Passphrase: "secret",
				Security:   controlpb.ConnectWifi_SECURITY_WPA2_PSK,
				Band:       controlpb.WifiBand_WIFI_BAND_5_GHZ,
				RequireIp:  true,
			}},
		},
		Steps: []*controlpb.StandaloneMeasurementStep{
			standaloneStep(1, "connect", &controlpb.RunCommand{
				Label: "standalone connect lab",
				Command: &controlpb.RunCommand_ConnectWifi{ConnectWifi: &controlpb.ConnectWifi{
					Ssid:       "Lab",
					Passphrase: "secret",
					Security:   controlpb.ConnectWifi_SECURITY_WPA2_PSK,
					Band:       controlpb.WifiBand_WIFI_BAND_5_GHZ,
					TimeoutMs:  35000,
				}},
			}, &controlpb.CommandResult{
				Status:  controlpb.CommandResult_STATUS_OK,
				Message: "connected",
				Payload: &controlpb.CommandResult_ConnectWifi{ConnectWifi: &controlpb.ConnectWifiResult{
					Ssid:      "Lab",
					Connected: true,
				}},
			}),
			standaloneStep(2, "wait_connected", &controlpb.RunCommand{
				Label: "standalone wait lab",
				Command: &controlpb.RunCommand_WaitWifiConnected{WaitWifiConnected: &controlpb.WaitWifiConnected{
					Ssid:      "Lab",
					Security:  controlpb.ConnectWifi_SECURITY_WPA2_PSK,
					Band:      controlpb.WifiBand_WIFI_BAND_5_GHZ,
					RequireIp: true,
					TimeoutMs: 35000,
				}},
			}, &controlpb.CommandResult{
				Status:  controlpb.CommandResult_STATUS_OK,
				Message: "connected",
				Payload: &controlpb.CommandResult_WifiAssert{WifiAssert: &controlpb.WifiAssertResult{
					Passed: true,
				}},
			}),
			standaloneStep(3, "dns", &controlpb.RunCommand{
				Label: "standalone dns example.com",
				Command: &controlpb.RunCommand_ResolveDns{ResolveDns: &controlpb.ResolveDns{
					Name:      "example.com",
					Qtypes:    []controlpb.DnsRecordType{controlpb.DnsRecordType_DNS_RECORD_TYPE_A},
					TimeoutMs: 10000,
					Selector:  &controlpb.NetworkSelector{Ssid: "Lab"},
				}},
			}, &controlpb.CommandResult{
				Status: controlpb.CommandResult_STATUS_OK,
				Payload: &controlpb.CommandResult_ResolveDns{ResolveDns: &controlpb.ResolveDnsResult{
					Name:      "example.com",
					ElapsedMs: 80,
					Answers: []*controlpb.DnsAnswer{{
						Type:    controlpb.DnsRecordType_DNS_RECORD_TYPE_A,
						Address: "93.184.216.34",
					}},
				}},
			}),
			standaloneStep(4, "ping", &controlpb.RunCommand{
				Label: "standalone ping 8.8.8.8",
				Command: &controlpb.RunCommand_Ping{Ping: &controlpb.Ping{
					Host:      "8.8.8.8",
					Count:     3,
					TimeoutMs: 10000,
					Selector:  &controlpb.NetworkSelector{Ssid: "Lab"},
				}},
			}, &controlpb.CommandResult{
				Status: controlpb.CommandResult_STATUS_OK,
				Payload: &controlpb.CommandResult_Ping{Ping: &controlpb.PingResult{
					Host:              "8.8.8.8",
					Count:             3,
					Transmitted:       3,
					Received:          3,
					PacketLossPercent: 0,
					MinMs:             10,
					AvgMs:             25,
					MaxMs:             40,
					ElapsedMs:         120,
				}},
			}),
			standaloneStep(5, "http", &controlpb.RunCommand{
				Label: "standalone http http://example.com/health",
				Command: &controlpb.RunCommand_HttpCheck{HttpCheck: &controlpb.HttpCheck{
					Url:            "http://example.com/health",
					ExpectedStatus: 204,
					TimeoutMs:      10000,
					Selector:       &controlpb.NetworkSelector{Ssid: "Lab"},
				}},
			}, &controlpb.CommandResult{
				Status: controlpb.CommandResult_STATUS_OK,
				Payload: &controlpb.CommandResult_HttpCheck{HttpCheck: &controlpb.HttpCheckResult{
					Url:            "http://example.com/health",
					Status:         204,
					ExpectedStatus: 204,
					Matched:        true,
					ElapsedMs:      100,
				}},
			}),
		},
	}
}

func standaloneStep(stepIndex uint32, name string, command *controlpb.RunCommand, result *controlpb.CommandResult) *controlpb.StandaloneMeasurementStep {
	return &controlpb.StandaloneMeasurementStep{
		WifiGroupIndex: 1,
		WifiGroupName:  "lab",
		StepIndex:      stepIndex,
		StepName:       name,
		Attempt:        1,
		Command:        command,
		Result:         result,
	}
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
				ApMldMacAddress:    "02:00:00:00:00:01",
				ApMloLinkId:        1,
				AssociatedMloLinks: []*controlpb.MloLinkInfo{{LinkId: 1, State: "active", Band: "6ghz", Channel: 37}},
				AffiliatedMloLinks: []*controlpb.MloLinkInfo{{LinkId: 2, State: "idle", Band: "5ghz", Channel: 149}},
				Raw:                "ssid=Lab channelWidth=160MHz",
			},
		}}
	case "wifi.scan.fresh", "wifi.scan":
		result.Payload = &controlpb.CommandResult_WifiScan{WifiScan: &controlpb.WifiScan{
			Results: []*controlpb.WifiScanResult{{
				Ssid:            "Lab",
				Bssid:           "aa:bb:cc:dd:ee:ff",
				Capabilities:    "[RSN-SAE-CCMP][EHT][ESS]",
				RssiDbm:         -41,
				FrequencyMhz:    6135,
				Band:            "6GHz",
				ChannelWidth:    "320MHz",
				WifiStandard:    "802.11be",
				SecurityTypes:   []string{"wpa3_sae"},
				ApMldMacAddress: "02:00:00:00:00:01",
				ApMloLinkId:     1,
				AffiliatedMloLinks: []*controlpb.MloLinkInfo{{
					LinkId:  2,
					Band:    "5ghz",
					Channel: 149,
				}},
			}},
		}}
	case "wifi.eht":
		result.Payload = &controlpb.CommandResult_WifiDiagnostics{WifiDiagnostics: &controlpb.WifiDiagnostics{
			Status:       fakeResult("wifi.status").GetWifiStatus(),
			Scan:         fakeResult("wifi.scan").GetWifiScan(),
			Capabilities: fakeResult("wifi.capabilities").GetWifiCapabilities(),
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
