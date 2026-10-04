package command

import (
	"slices"
	"strings"
	"testing"

	"dropcheck/controller/internal/controlpb"
)

// Canonical commands cover the 22 live executor entries plus the EHT composite.
func TestCanonicalInventoryAndDefaults(t *testing.T) {
	for _, tc := range []struct{ line, name string }{
		{"show wifi status", "wifi.status"}, {"show wifi diagnostics", "wifi.diagnostics"},
		{"show wifi capabilities", "wifi.capabilities"}, {"show wifi scan", "wifi.scan"},
		{"show wifi scan fresh", "wifi.scan.fresh"}, {"show wifi scan detail scan", "wifi.scan.detail"},
		{"show wifi eht fresh", "wifi.eht"}, {"show ip status", "ip.status"},
		{"wifi connect scan passphrase 00000000", "wifi.connect"}, {"wifi disconnect", "wifi.disconnect"},
		{"wifi forget scan", "wifi.forget"}, {"wifi wait connected", "wifi.wait"},
		{"wifi assert", "wifi.assert"}, {"wifi monitor", "wifi.monitor"},
		{"wifi reconnect", "wifi.reconnect"}, {"wifi cycle scan passphrase 00000000", "wifi.cycle"},
		{"ping count", "ping"}, {"traceroute count", "traceroute"},
		{"path-mtu count", "path-mtu"}, {"global-ip", "global-ip"},
		{"dns count", "dns"}, {"http example.test", "http"},
		{"download https://example.test", "download"},
	} {
		t.Run(tc.line, func(t *testing.T) {
			parsed, err := ParseTokens(strings.Fields(tc.line))
			if err != nil {
				t.Fatal(err)
			}
			if parsed.Operation.Name != tc.name {
				t.Fatalf("%s != %s", parsed.Operation.Name, tc.name)
			}
			if err := ValidateOperation(parsed.Operation); err != nil {
				t.Fatal(err)
			}
		})
	}
	for line, check := range map[string]func(*controlpb.RunCommand) bool{
		"wifi connect Lab passphrase 00000000": func(c *controlpb.RunCommand) bool { return c.GetConnectWifi().GetTimeoutMs() == 45000 },
		"wifi wait connected":                  func(c *controlpb.RunCommand) bool { return c.GetWaitWifiConnected().GetTimeoutMs() == 30000 },
		"wifi assert":                          func(c *controlpb.RunCommand) bool { return c.GetAssertWifi().GetTimeoutMs() == 0 },
		"wifi cycle Lab passphrase 00000000": func(c *controlpb.RunCommand) bool {
			return c.GetCycleWifi().GetCount() == 3 && c.GetCycleWifi().GetPauseMs() == 1000
		},
		"ping Lab": func(c *controlpb.RunCommand) bool {
			return c.GetPing().GetCount() == 3 && c.GetPing().GetTimeoutMs() == 9000 && c.GetPing().GetFamily() == controlpb.IpFamily_IP_FAMILY_UNSPECIFIED
		},
		"traceroute Lab": func(c *controlpb.RunCommand) bool {
			return c.GetTraceroute().GetMaxHops() == 30 && c.GetTraceroute().GetTimeoutMs() == 60000
		},
		"path-mtu Lab": func(c *controlpb.RunCommand) bool { return c.GetPathMtu().GetMaxMtuBytes() == 0 },
		"global-ip": func(c *controlpb.RunCommand) bool {
			return c.GetGlobalIp().GetFamily() == controlpb.IpFamily_IP_FAMILY_ALL
		},
		"dns Lab": func(c *controlpb.RunCommand) bool { return len(c.GetResolveDns().GetQtypes()) == 2 },
		"http example.test": func(c *controlpb.RunCommand) bool {
			return c.GetHttpCheck().GetExpectedStatus() == 200 && c.GetHttpCheck().GetTimeoutMs() == 5000
		},
		"download https://example.test": func(c *controlpb.RunCommand) bool { return c.GetWget().GetTimeoutMs() == 60000 },
	} {
		parsed, err := ParseTokens(strings.Fields(line))
		if err != nil {
			t.Fatal(err)
		}
		if !check(parsed.Operation.Command) {
			t.Fatalf("default mismatch: %s", line)
		}
	}
}

func TestGrammarBoundariesAndLiterals(t *testing.T) {
	if err := validateAliases(GrammarTable, map[string]string{"show": "ping"}); err == nil {
		t.Fatal("canonical collision accepted")
	}
	if err := validateAliases(GrammarTable, map[string]string{"new": "absent"}); err == nil {
		t.Fatal("unknown alias target accepted")
	}
	for _, tc := range []struct {
		input string
		want  string
	}{
		{"show wifi s", "status, scan"}, {"s", "show, set"},
		{"show \"\"", "empty keyword"}, {"request ping Lab", "unknown keyword"},
		{"show wifi eht brief", "unknown keyword"}, {"show wifi scan mlo", "mlo requires brief"},
		{"wifi connect Lab passphrase 00000000 passphrase 00000000", "specified twice"},
		{"wifi connect Lab passphrase 0000000", "8..63"},
		{"wifi connect Lab passphrase " + strings.Repeat("x", 64), "8..63"},
		{"ping host count 0", "1..2147483647"}, {"ping host count 2147483648", "1..2147483647"},
		{"ping host count 4294967295", "1..2147483647"}, {"ping host count -1", "1..2147483647"},
		{"ping host count nope", "1..2147483647"}, {"ping host count 2147483647", "count exceeds operation limit"},
		{"path-mtu host min-mtu 1500 max-mtu 1200", "invalid path-mtu arguments"},
		{"ping host family all", "invalid family"}, {"ping --format", "invalid probe host"}, {"wifi assert require-ip yes", "true or false"},
		{"global-ip family auto", "invalid family"}, {"wifi assert require-i true require-ip false", "specified twice"},
		{"wifi cycle Lab passphrase 00000000 http fixture.invalid", "invalid HTTP endpoint"},
		{"traceroute host max-hops 256", "exceeds operation limit"},
		{"check", "unsupported"}, {"use Lab", "unsupported"},
	} {
		args, err := SplitArgs(tc.input)
		if err != nil {
			t.Fatal(err)
		}
		_, err = ParseTokens(args)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: %v; want %s", tc.input, err, tc.want)
		}
	}
	for _, tc := range []struct{ input, name string }{
		{"p count count 3", "ping"}, {"tr count via 192.0.2.1 via 192.0.2.2", "traceroute"},
		{"show wifi scan detail all", "wifi.scan.detail"}, {"ping count", "ping"},
		{"wifi connect scan passphrase 00000000", "wifi.connect"},
		{"show wifi eht detail fresh", "wifi.eht"}, {"wifi wait connected require-ip false", "wifi.wait"},
		{"wifi reconnect timeout 2147483647", "wifi.reconnect"},
		{"wifi connect Lab passphrase " + strings.Repeat("a", 64), "wifi.connect"},
	} {
		p, err := ParseTokens(strings.Fields(tc.input))
		if err != nil {
			t.Errorf("%s: %v", tc.input, err)
			continue
		}
		if p.Operation.Name != tc.name {
			t.Errorf("%s: %s", tc.input, p.Operation.Name)
		}
	}
	p, err := ParseTokens([]string{"wifi", "connect", "scan with space", "passphrase", "literal with \\ backslash"})
	if err != nil || p.Operation.Command.GetConnectWifi().GetSsid() != "scan with space" {
		t.Fatal(err)
	}
	if strings.Contains(p.Operation.Command.Label, "literal with") {
		t.Fatal("credential leaked into label")
	}
	p, err = ParseTokens([]string{"traceroute", "count", "via", "192.0.2.1", "via", "192.0.2.2", "ssid", "Lab"})
	if err != nil || !slices.Equal(p.Operation.Options.TracerouteRequiredHops, []string{"192.0.2.1", "192.0.2.2"}) || p.Operation.Command.GetTraceroute().GetSelector().GetSsid() != "Lab" {
		t.Fatal(err)
	}
}

func TestCredentialErrorsNeverEchoOriginalToken(t *testing.T) {
	for _, args := range [][]string{
		{"wifi", "connect", "Lab", "passphrase", "example-secret", "security", "example-secret"},
		{"wifi", "cycle", "Lab", "passphrase", "example-secret", "count", "0"},
		{"wifi", "connect", "Lab", "passphrase", "example-secret", "unknown", "value"},
	} {
		_, err := ParseTokens(args)
		if err == nil || strings.Contains(err.Error(), "example-secret") {
			t.Fatalf("unsafe error: %v", err)
		}
	}
}

// Retain the old adapter's capability assertions, but exercise only canonical
// tokens and the actual wire command plus local options.
func TestCanonicalWireArgumentsAndPolicyDefaults(t *testing.T) {
	for _, tc := range []struct {
		line string
		ok   func(*controlpb.RunCommand, Options) bool
	}{
		{"wifi wait connected ssid Lab security transition band 5ghz require-ip true require-validated true timeout 9000", func(c *controlpb.RunCommand, _ Options) bool {
			w := c.GetWaitWifiConnected()
			return w != nil && w.GetSsid() == "Lab" && w.GetSecurity() == controlpb.ConnectWifi_SECURITY_WPA2_WPA3_TRANSITION && w.GetBand() == controlpb.WifiBand_WIFI_BAND_5_GHZ && w.GetRequireIp() && w.GetRequireValidated() && w.GetTimeoutMs() == 9000
		}},
		{"wifi assert ssid Lab bssid aa:bb:cc:dd:ee:ff security wpa3 band 6ghz require-ip true require-validated true timeout 8000", func(c *controlpb.RunCommand, _ Options) bool {
			w := c.GetAssertWifi()
			return w != nil && w.GetSsid() == "Lab" && w.GetBssid() == "aa:bb:cc:dd:ee:ff" && w.GetSecurity() == controlpb.ConnectWifi_SECURITY_WPA3_SAE && w.GetBand() == controlpb.WifiBand_WIFI_BAND_6_GHZ && w.GetRequireIp() && w.GetRequireValidated() && w.GetTimeoutMs() == 8000
		}},
		{"wifi monitor interval 250", func(c *controlpb.RunCommand, _ Options) bool {
			w := c.GetMonitorWifi()
			return w != nil && w.GetDurationMs() == 10000 && w.GetIntervalMs() == 250
		}},
		{"wifi monitor duration 5000 interval 250", func(c *controlpb.RunCommand, _ Options) bool {
			w := c.GetMonitorWifi()
			return w != nil && w.GetDurationMs() == 5000 && w.GetIntervalMs() == 250
		}},
		{"wifi reconnect timeout 9000", func(c *controlpb.RunCommand, _ Options) bool {
			w := c.GetReconnectWifi()
			return w != nil && w.GetTimeoutMs() == 9000
		}},
		{"download https://example.test/file.bin", func(c *controlpb.RunCommand, _ Options) bool {
			w := c.GetWget()
			return w != nil && w.GetUrl() == "https://example.test/file.bin" && w.GetTimeoutMs() == 60000 && w.GetSelector() != nil
		}},
		{"http example.test/health", func(c *controlpb.RunCommand, _ Options) bool {
			w := c.GetHttpCheck()
			return w != nil && w.GetUrl() == "https://example.test/health" && w.GetExpectedStatus() == 200 && w.GetTimeoutMs() == 5000 && w.GetSelector() != nil
		}},
		{"dns example.test", func(c *controlpb.RunCommand, _ Options) bool {
			w := c.GetResolveDns()
			return w != nil && w.GetName() == "example.test" && slices.Equal(w.GetQtypes(), []controlpb.DnsRecordType{controlpb.DnsRecordType_DNS_RECORD_TYPE_A, controlpb.DnsRecordType_DNS_RECORD_TYPE_AAAA}) && w.GetTimeoutMs() == 5000 && w.GetSelector() != nil
		}},
		{"show wifi scan fresh brief mlo band 6ghz timeout 9000", func(c *controlpb.RunCommand, o Options) bool {
			w := c.GetGetFreshWifiScan()
			return w != nil && w.GetTimeoutMs() == 9000 && w.GetBand() == controlpb.WifiBand_WIFI_BAND_6_GHZ && o.WifiScanBrief && o.WifiScanMLO
		}},
		{"show wifi eht detail fresh ssid Lab", func(c *controlpb.RunCommand, o Options) bool {
			return c.GetGetWifiDiagnostics() != nil && o.Detail && o.WifiEHTFreshScan && o.WifiEHTSSID == "Lab" && o.WifiEHTFreshScanTimeoutMs == 10000
		}},
		{"ping example.test family auto ssid Lab", func(c *controlpb.RunCommand, _ Options) bool {
			w := c.GetPing()
			return w != nil && w.GetFamily() == controlpb.IpFamily_IP_FAMILY_UNSPECIFIED && w.GetSelector().GetSsid() == "Lab"
		}},
		{"traceroute example.test family ipv6 via 192.0.2.1 via 192.0.2.2", func(c *controlpb.RunCommand, o Options) bool {
			w := c.GetTraceroute()
			return w != nil && w.GetFamily() == controlpb.IpFamily_IP_FAMILY_IPV6 && slices.Equal(o.TracerouteRequiredHops, []string{"192.0.2.1", "192.0.2.2"})
		}},
		{"global-ip family all", func(c *controlpb.RunCommand, _ Options) bool {
			w := c.GetGlobalIp()
			return w != nil && w.GetFamily() == controlpb.IpFamily_IP_FAMILY_ALL
		}},
	} {
		parsed, err := ParseTokens(strings.Fields(tc.line))
		if err != nil || !tc.ok(parsed.Operation.Command, parsed.Operation.Options) {
			t.Errorf("%q wire/default/options mismatch: %v", tc.line, err)
		}
	}
}
