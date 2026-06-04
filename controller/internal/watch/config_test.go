package watch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dropcheck/controller/internal/controlpb"
)

func boolPtr(value bool) *bool {
	return &value
}

func TestLoadFileAppliesDefaultsAndCompilesExpectations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "watch.yml")
	data := []byte(`version: 1
name: lab-watch
round_interval: 250ms
defaults:
  agent: 35251JEHN00258
  passphrase_env: DROPCHECK_WIFI_PSK
  security: wpa3
  require_ip: true
  require_validated: true
  disconnect_after: false
targets:
  - name: lab-6g
    short_name: L6
    ssid: Lab
    bssid: aa:bb:cc:dd:ee:ff
    band: 6ghz
checks:
  - name: ping gateway
    type: ping
    required: true
    host: 1.1.1.1
    count: 5
    expect:
      received: 5
      loss_percent: "<=0"
`)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	plan, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}
	if plan.Name != "lab-watch" || plan.RoundInterval.String() != "250ms" {
		t.Fatalf("plan metadata = %q %s", plan.Name, plan.RoundInterval)
	}
	target := plan.Targets[0]
	if target.PassphraseEnv != "DROPCHECK_WIFI_PSK" || target.Security != "wpa3" || !target.requireIP() || !target.requireValidated() || target.disconnectAfter() {
		t.Fatalf("target defaults not applied: %#v", target)
	}
	if target.Agent != "35251JEHN00258" {
		t.Fatalf("target agent = %q, want 35251JEHN00258", target.Agent)
	}
	if target.ShortName != "L6" {
		t.Fatalf("target short name = %q, want L6", target.ShortName)
	}
	if got := len(plan.Checks[0].compiledExpect); got != 2 {
		t.Fatalf("compiled expectations = %d, want 2", got)
	}
	if !plan.Checks[0].Required {
		t.Fatalf("required check flag was not loaded: %#v", plan.Checks[0])
	}
}

func TestLoadFileAllowsTargetVarsInChecks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "watch.yml")
	data := []byte(`version: 1
name: lab-watch
vars:
  dns_server_min: ">=1"
  legacy_gateway_ipv4: 10.20.128.1
  legacy_ipv4_cidr: 10.20.128.0/20
targets:
  - name: hp1-legacy
    ssid: hp1-legacy
    vars:
      gateway_ipv4: "${legacy_gateway_ipv4}"
      ipv4_cidr: "${legacy_ipv4_cidr}"
checks:
  - name: IP Provisioning
    type: ip_status
    expect:
      dns_server_count: "${dns_server_min}"
      ipv4_addresses:
        cidr: "${ipv4_cidr}"
  - name: Ping GW IPv4
    type: ping
    host: "${gateway_ipv4}"
`)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	plan, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}
	if got := plan.Targets[0].Vars["gateway_ipv4"]; got != "10.20.128.1" {
		t.Fatalf("target vars not resolved: gateway_ipv4=%q", got)
	}
	if got := plan.Targets[0].Vars["dns_server_min"]; got != ">=1" {
		t.Fatalf("top-level vars not merged: dns_server_min=%q", got)
	}
	if got := plan.Targets[0].Vars["ipv4_cidr"]; got != "10.20.128.0/20" {
		t.Fatalf("target alias missing: ipv4_cidr=%q", got)
	}
}

func TestLoadFileCompilesWifiFeatureExpectations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "watch.yml")
	data := []byte(`version: 1
targets:
  - name: cs1
    ssid: cs1
checks:
  - name: wifi feature probe
    type: scan_detail
    expect:
      mlo: true
      sae: true
      sae-public-key: dontcare
      transition: false
`)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	plan, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile() error = %v", err)
	}
	if len(plan.Checks) != 1 || len(plan.Checks[0].compiledExpect) != 4 {
		t.Fatalf("compiled expectations = %#v", plan.Checks)
	}
	wantOps := map[string]string{
		"mlo":            "==",
		"sae":            "==",
		"sae-public-key": "dontcare",
		"transition":     "==",
	}
	for _, matcher := range plan.Checks[0].compiledExpect {
		want, ok := wantOps[matcher.Metric]
		if !ok {
			t.Fatalf("unexpected matcher %#v", matcher)
		}
		if matcher.Op != want {
			t.Fatalf("matcher %q op = %q, want %q", matcher.Metric, matcher.Op, want)
		}
	}
}

func TestLoadFileRejectsTargetVarCycles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "watch.yml")
	data := []byte(`version: 1
targets:
  - name: hp1
    ssid: hp1
    vars:
      gateway_ipv4: "${legacy_gateway_ipv4}"
      legacy_gateway_ipv4: "${gateway_ipv4}"
checks:
  - name: Ping GW IPv4
    type: ping
    host: "${gateway_ipv4}"
`)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadFile(path)
	if err == nil || !strings.Contains(err.Error(), `target variable cycle at "`) {
		t.Fatalf("LoadFile() error = %v, want target variable cycle", err)
	}
}

func TestLoadFileRejectsUnknownTargetVarsInChecks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "watch.yml")
	data := []byte(`version: 1
targets:
  - name: hp1
    ssid: hp1
checks:
  - name: Ping GW IPv4
    type: ping
    host: "${gateway_ipv4}"
`)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadFile(path)
	if err == nil || !strings.Contains(err.Error(), `unknown target variable "gateway_ipv4"`) {
		t.Fatalf("LoadFile() error = %v, want unknown target variable", err)
	}
}

func TestApplyConfigVarsMergesTopLevelAndTargetOverrides(t *testing.T) {
	target := applyConfigVars(Target{
		Vars: map[string]string{
			"gateway_ipv4": "10.20.0.1",
			"ipv4_cidr":    "10.20.0.0/20",
		},
	}, map[string]string{
		"gateway_ipv4":   "10.20.0.254",
		"dns_server_min": ">=1",
	})
	if got := target.Vars["gateway_ipv4"]; got != "10.20.0.1" {
		t.Fatalf("gateway_ipv4 = %q, want target override", got)
	}
	if got := target.Vars["dns_server_min"]; got != ">=1" {
		t.Fatalf("dns_server_min = %q, want merged top-level var", got)
	}
	if got := target.Vars["ipv4_cidr"]; got != "10.20.0.0/20" {
		t.Fatalf("ipv4_cidr = %q, want target value", got)
	}
}

func TestExampleWatchConfigLoads(t *testing.T) {
	plan, err := LoadFile(filepath.Join("..", "..", "..", "examples", "watch.yml"))
	if err != nil {
		t.Fatalf("LoadFile(examples/watch.yml) error = %v", err)
	}
	if len(plan.Targets) == 0 || len(plan.Checks) == 0 {
		t.Fatalf("example config produced empty plan: targets=%d checks=%d", len(plan.Targets), len(plan.Checks))
	}
}

func TestMacRotationDefaultsToNonPersistentAndForcesCleanup(t *testing.T) {
	plan, err := Config{
		Defaults: TargetDefaults{
			MacRotation:     "per_target",
			DisconnectAfter: boolPtr(false),
			ForgetAfter:     boolPtr(false),
		},
		Targets: []Target{{SSID: "Lab"}},
		Checks:  []Check{{Type: "ip_status"}},
	}.Plan()
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	target := plan.Targets[0]
	if target.MacRotation != macRotationPerTarget {
		t.Fatalf("mac rotation = %q, want %q", target.MacRotation, macRotationPerTarget)
	}
	if target.MacRandomization != "non-persistent" {
		t.Fatalf("mac randomization = %q, want non-persistent", target.MacRandomization)
	}
	if !target.disconnectAfter() || !target.forgetAfter() {
		t.Fatalf("per-target rotation should force disconnect and forget cleanup: %#v", target)
	}
}

func TestMacRotationRejectsNonRotatingRandomizationModes(t *testing.T) {
	_, err := Config{
		Targets: []Target{{SSID: "Lab", MacRotation: "per_round", MacRandomization: "none"}},
		Checks:  []Check{{Type: "ip_status"}},
	}.Plan()
	if err == nil || !strings.Contains(err.Error(), "requires mac_randomization: non-persistent") {
		t.Fatalf("Plan() error = %v, want mac randomization validation", err)
	}
}

func TestEvaluateMatchersReportsFailures(t *testing.T) {
	target := Target{Name: "lab", SSID: "Lab"}
	check := Check{
		Name: "ping",
		Type: "ping",
		compiledExpect: []Matcher{
			{Metric: "received", Op: "==", Want: "5"},
			{Metric: "loss_percent", Op: "<=", Want: "0"},
		},
	}
	result := &controlpb.CommandResult{
		Status: controlpb.CommandResult_STATUS_OK,
		Payload: &controlpb.CommandResult_Ping{Ping: &controlpb.PingResult{
			Received:          4,
			PacketLossPercent: 20,
		}},
	}
	findings := evaluateMatchers(target, check, metricsForResult(result))
	if len(findings) != 2 {
		t.Fatalf("findings = %d, want 2: %#v", len(findings), findings)
	}
	if findings[0].Target != "lab" || findings[0].Check != "ping" {
		t.Fatalf("finding context not set: %#v", findings[0])
	}
}

func TestDontCareExpectationIgnoresMissingMetric(t *testing.T) {
	target := Target{Name: "lab", SSID: "Lab"}
	check := Check{
		Name: "wifi",
		Type: "wifi_status",
		compiledExpect: []Matcher{
			{Metric: "sae-public-key", Op: "dontcare"},
			{Metric: "mlo", Op: "==", Want: "true"},
		},
	}
	metrics := map[string]Value{"mlo": boolValue(true)}
	if findings := evaluateMatchers(target, check, metrics); len(findings) != 0 {
		t.Fatalf("findings = %#v, want none", findings)
	}
}

func TestPingMetricsParseAgentOutput(t *testing.T) {
	target := Target{Name: "lab", SSID: "Lab"}
	check := Check{
		Name: "ping",
		Type: "ping",
		compiledExpect: []Matcher{
			{Metric: "received", Op: "==", Want: "5"},
			{Metric: "loss_percent", Op: "==", Want: "0"},
			{Metric: "avg_latency_ms", Op: "<=", Want: "50"},
		},
	}
	result := &controlpb.CommandResult{
		Status: controlpb.CommandResult_STATUS_OK,
		Payload: &controlpb.CommandResult_Ping{Ping: &controlpb.PingResult{
			Host:  "1.1.1.1",
			Count: 5,
			Output: "PING 1.1.1.1 (1.1.1.1): 56 data bytes\n" +
				"64 bytes from 1.1.1.1: icmp_seq=1 ttl=58 time=12.3 ms\n" +
				"\n--- 1.1.1.1 ping statistics ---\n" +
				"5 packets transmitted, 5 received, 0% packet loss, time 4007ms\n" +
				"rtt min/avg/max/mdev = 10.000/12.400/16.300/1.900 ms\n",
		}},
	}

	if findings := evaluateMatchers(target, check, metricsForResult(result)); len(findings) != 0 {
		t.Fatalf("findings = %#v, want none", findings)
	}
}

func TestNetworkProbeMetrics(t *testing.T) {
	cases := []struct {
		name   string
		result *controlpb.CommandResult
		want   map[string]string
	}{
		{
			name: "wifi status security features",
			result: &controlpb.CommandResult{Payload: &controlpb.CommandResult_WifiStatus{WifiStatus: &controlpb.WifiStatus{
				Enabled: true,
				Connection: &controlpb.WifiConnection{
					Ssid:            "Lab",
					SecurityType:    "sae",
					WifiStandard:    "802.11be",
					ApMldMacAddress: "02:00:00:00:00:01",
					ApMloLinkId:     1,
					AssociatedMloLinks: []*controlpb.MloLinkInfo{{
						LinkId: 1,
						State:  "active",
						Band:   "6ghz",
					}},
					SecurityDetails: &controlpb.WifiSecurityDetails{
						RsnPresent:           true,
						RsnxePresent:         true,
						PmfCapable:           true,
						PmfRequired:          true,
						Gcmp_256:             true,
						SaeGdh:               true,
						FtSaeGdh:             true,
						Wifi7PersonalReady:   true,
						BeaconProtection:     true,
						AkmSuites:            []string{"psk", "sae_gdh", "ft_sae_gdh"},
						PairwiseCiphers:      []string{"gcmp_256"},
						RsnxeCapabilities:    []string{"sae_h2e", "sae_pk", "ssid_protection"},
						ExtendedCapabilities: []string{"bss_transition"},
					},
				},
			}}},
			want: map[string]string{
				"mlo":                      "true",
				"sae":                      "true",
				"transition":               "true",
				"sae-public-key":           "true",
				"h2e":                      "true",
				"ssid-protection":          "true",
				"pmf-required":             "true",
				"security_akm_suites":      "psk,sae_gdh,ft_sae_gdh",
				"security_details_present": "true",
			},
		},
		{
			name: "scan detail security features",
			result: &controlpb.CommandResult{Payload: &controlpb.CommandResult_WifiScanDetail{WifiScanDetail: &controlpb.WifiScanDetail{
				Results: []*controlpb.WifiScanResult{{
					Ssid:         "Lab",
					Bssid:        "aa:bb:cc:dd:ee:ff",
					RssiDbm:      -54,
					Band:         "6ghz",
					WifiStandard: "802.11be",
					SecurityTypes: []string{
						"sae",
					},
					ApMloLinkId: -1,
					InformationElements: []*controlpb.WifiInformationElement{{
						Id:    255,
						IdExt: 107,
					}},
					SecurityDetails: &controlpb.WifiSecurityDetails{
						RsnPresent:           true,
						RsnxePresent:         true,
						PmfCapable:           true,
						PmfRequired:          true,
						AkmSuites:            []string{"sae"},
						RsnxeCapabilities:    []string{"sae_h2e"},
						ExtendedCapabilities: []string{"bss_transition"},
					},
				}},
			}}},
			want: map[string]string{
				"mlo":            "true",
				"sae":            "true",
				"transition":     "false",
				"sae-public-key": "false",
				"sae-h2e":        "true",
				"bss-transition": "true",
				"security_types": "sae",
				"rsnxe-present":  "true",
			},
		},
		{
			name: "traceroute",
			result: &controlpb.CommandResult{Payload: &controlpb.CommandResult_Traceroute{Traceroute: &controlpb.TracerouteResult{
				Host:      "1.1.1.1",
				MaxHops:   8,
				ExitCode:  0,
				ElapsedMs: 123,
				Output:    "traceroute to 1.1.1.1\n 1  192.168.1.1  1.0 ms\n 2  1.1.1.1  10.0 ms\n",
			}}},
			want: map[string]string{"hop_count": "2", "reached": "true", "max_hops": "8"},
		},
		{
			name: "path mtu",
			result: &controlpb.CommandResult{Payload: &controlpb.CommandResult_PathMtu{PathMtu: &controlpb.PathMtuResult{
				Host:         "1.1.1.1",
				Discovered:   true,
				PathMtuBytes: 1500,
				Probes: []*controlpb.PathMtuProbe{
					{MtuBytes: 1280, Passed: true},
					{MtuBytes: 1500, Passed: true},
				},
			}}},
			want: map[string]string{"discovered": "true", "path_mtu_bytes": "1500", "probe_count": "2", "passed_probe_count": "2"},
		},
		{
			name: "download",
			result: &controlpb.CommandResult{Payload: &controlpb.CommandResult_Wget{Wget: &controlpb.WgetResult{
				Url:           "http://1.1.1.1/cdn-cgi/trace",
				Status:        301,
				BytesRead:     167,
				ContentLength: 167,
				ThroughputBps: 1000,
			}}},
			want: map[string]string{"status_code": "301", "bytes_read": "167", "content_length": "167"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			metrics := metricsForResult(tc.result)
			for key, want := range tc.want {
				got, ok := metrics[key]
				if !ok {
					t.Fatalf("metric %q missing from %#v", key, metrics)
				}
				if got.String() != want {
					t.Fatalf("metric %q = %q, want %q", key, got.String(), want)
				}
			}
		})
	}
}

func TestIPDefaultRouteMetricsByFamily(t *testing.T) {
	metrics := metricsForResult(&controlpb.CommandResult{Payload: &controlpb.CommandResult_IpStatus{IpStatus: &controlpb.IpStatus{
		Routes: []string{
			"0.0.0.0/0 -> 192.168.23.254 wlan0",
			"2405:6581:3e00:a600::/64 -> :: wlan0",
			"2405:6581:3e00:a604::/64 -> fe80::88c:2e0c:8c82:2c02 wlan0",
		},
	}}})

	for key, want := range map[string]bool{
		"default_route":      true,
		"ipv4_default_route": true,
		"ipv6_default_route": false,
	} {
		got, ok := metrics[key]
		if !ok {
			t.Fatalf("metric %q missing from %#v", key, metrics)
		}
		gotBool, ok := got.Bool()
		if !ok || gotBool != want {
			t.Fatalf("metric %q = %q, want %t", key, got.String(), want)
		}
	}
}

func TestCIDRAndValueExpectations(t *testing.T) {
	cases := []struct {
		name   string
		check  Check
		result *controlpb.CommandResult
	}{
		{
			name: "assigned addresses and dns resolvers",
			check: Check{
				Name: "ip",
				Type: "ip_status",
				compiledExpect: []Matcher{
					{Metric: "ipv4_addresses", Op: "cidr", Values: []string{"192.168.20.0/22"}, Mode: "at_least"},
					{Metric: "ipv6_addresses", Op: "cidr", Values: []string{"2405:6581:3e00:a600::/64"}, Mode: "at_least"},
					{Metric: "ipv4_dns_servers", Op: "cidr", Values: []string{"1.1.1.0/24", "8.8.8.0/24"}, Mode: "exact"},
				},
			},
			result: &controlpb.CommandResult{Payload: &controlpb.CommandResult_IpStatus{IpStatus: &controlpb.IpStatus{
				Addresses:  []string{"192.168.22.90/22", "2405:6581:3e00:a600::123/64"},
				DnsServers: []string{"1.1.1.1", "8.8.8.8"},
			}}},
		},
		{
			name: "traceroute hop cidr",
			check: Check{
				Name:           "trace",
				Type:           "traceroute",
				compiledExpect: []Matcher{{Metric: "hop_ips", Op: "cidr", Values: []string{"192.168.20.0/22"}, Mode: "at_least"}},
			},
			result: &controlpb.CommandResult{Payload: &controlpb.CommandResult_Traceroute{Traceroute: &controlpb.TracerouteResult{
				Host:   "1.1.1.1",
				Output: "traceroute to 1.1.1.1\n 1  gateway (192.168.23.254)  1.0 ms\n 2  1.1.1.1  10.0 ms\n",
			}}},
		},
		{
			name: "global ip cidr",
			check: Check{
				Name:           "global",
				Type:           "global_ip",
				compiledExpect: []Matcher{{Metric: "global_ips", Op: "cidr", Values: []string{"203.0.113.0/24"}, Mode: "at_least"}},
			},
			result: &controlpb.CommandResult{Payload: &controlpb.CommandResult_GlobalIp{GlobalIp: &controlpb.GlobalIpResult{
				Addresses: []*controlpb.GlobalIpAddress{{Family: controlpb.IpFamily_IP_FAMILY_IPV4, Ip: "203.0.113.10", Global: true}},
			}}},
		},
		{
			name: "dns answer values",
			check: Check{
				Name: "dns",
				Type: "dns",
				compiledExpect: []Matcher{
					{Metric: "a_answers", Op: "contains_values", Values: []string{"1.1.1.1"}, Mode: "at_least"},
					{Metric: "a_answers", Op: "exact_values", Values: []string{"1.1.1.1", "1.0.0.1"}},
				},
			},
			result: &controlpb.CommandResult{Payload: &controlpb.CommandResult_ResolveDns{ResolveDns: &controlpb.ResolveDnsResult{
				Answers: []*controlpb.DnsAnswer{
					{Type: controlpb.DnsRecordType_DNS_RECORD_TYPE_A, Address: "1.0.0.1"},
					{Type: controlpb.DnsRecordType_DNS_RECORD_TYPE_A, Address: "1.1.1.1"},
				},
			}}},
		},
	}
	target := Target{Name: "lab", SSID: "Lab"}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if findings := evaluateMatchers(target, tc.check, metricsForResult(tc.result)); len(findings) != 0 {
				t.Fatalf("findings = %#v, want none", findings)
			}
		})
	}
}

func TestCIDRExpectationModes(t *testing.T) {
	observed := stringListValue([]string{"192.168.20.10", "10.0.0.10"})
	if !((Matcher{Op: "cidr", Values: []string{"192.168.20.0/24"}, Mode: "at_least"}).matches(observed)) {
		t.Fatal("at_least CIDR matcher should pass when one observed IP is in range")
	}
	if (Matcher{Op: "cidr", Values: []string{"192.168.20.0/24"}, Mode: "exact"}).matches(observed) {
		t.Fatal("exact CIDR matcher should fail when any observed IP is out of range")
	}
	if !((Matcher{Op: "cidr", Values: []string{"192.168.20.0/24", "10.0.0.0/8"}, Mode: "exact"}).matches(observed)) {
		t.Fatal("exact CIDR matcher should pass when all observed IPs are in configured ranges")
	}
}

func TestCompileStructuredExpectations(t *testing.T) {
	matchers, err := compileMatchers(map[string]any{
		"ipv4_addresses": map[string]any{"cidr": "192.168.20.0/22"},
		"dns_servers":    map[string]any{"cidrs": []any{"1.1.1.0/24", "8.8.8.0/24"}, "mode": "exact"},
		"a_answers":      map[string]any{"exact": []any{"1.0.0.1", "1.1.1.1"}},
	})
	if err != nil {
		t.Fatalf("compileMatchers() error = %v", err)
	}
	if len(matchers) != 3 {
		t.Fatalf("matchers = %d, want 3", len(matchers))
	}
}

func TestCompileStructuredExpectationsRejectsUnknownMode(t *testing.T) {
	_, err := compileMatchers(map[string]any{
		"dns_servers": map[string]any{"cidr": "1.1.1.0/24", "mode": "mostly"},
	})
	if err == nil {
		t.Fatal("compileMatchers() error = nil, want unsupported mode error")
	}
}

func TestCheckOperationSupportsExtendedProbeTypes(t *testing.T) {
	target := Target{SSID: "Lab"}
	cases := []struct {
		check      Check
		wantFamily controlpb.IpFamily
	}{
		{check: Check{Type: "ping", Host: "one.one.one.one", Family: "ipv6"}, wantFamily: controlpb.IpFamily_IP_FAMILY_IPV6},
		{check: Check{Type: "traceroute", Host: "one.one.one.one", MaxHops: 8, Family: "ipv4"}, wantFamily: controlpb.IpFamily_IP_FAMILY_IPV4},
		{check: Check{Type: "path_mtu", Host: "one.one.one.one", MinMTU: 1280, MaxMTU: 1500, Family: "ipv6"}, wantFamily: controlpb.IpFamily_IP_FAMILY_IPV6},
		{check: Check{Type: "download", URL: "http://1.1.1.1/cdn-cgi/trace"}},
	}
	for _, tc := range cases {
		op, err := checkOperation(tc.check, target)
		if err != nil {
			t.Fatalf("checkOperation(%s) error = %v", tc.check.Type, err)
		}
		switch tc.check.Type {
		case "ping":
			if got := op.Command.GetPing().GetFamily(); got != tc.wantFamily {
				t.Fatalf("ping family = %s, want %s", got, tc.wantFamily)
			}
		case "traceroute":
			if got := op.Command.GetTraceroute().GetFamily(); got != tc.wantFamily {
				t.Fatalf("traceroute family = %s, want %s", got, tc.wantFamily)
			}
		case "path_mtu":
			if got := op.Command.GetPathMtu().GetFamily(); got != tc.wantFamily {
				t.Fatalf("path-mtu family = %s, want %s", got, tc.wantFamily)
			}
		}
	}
}
