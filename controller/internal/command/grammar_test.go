package command

import (
	"slices"
	"strings"
	"testing"
)

func TestShellCanonicalOperationsAndDefaults(t *testing.T) {
	for _, tc := range []struct{ line, name string }{
		{"show wifi status", "wifi.status"}, {"show wifi status full", "wifi.diagnostics"},
		{"show wifi status capabilities", "wifi.capabilities"}, {"show wifi scan", "wifi.scan"},
		{"request wifi scan", "wifi.scan.fresh"},
		{"request wifi scan eht", "wifi.eht"}, {"show wifi status ip", "ip.status"},
		{"request wifi disconnect", "wifi.disconnect"}, {"request wifi forget ssid scan", "wifi.forget"},
		{"test wifi", "wifi.assert"}, {"monitor wifi", "wifi.monitor"},
		{"request wifi reconnect", "wifi.reconnect"}, {"request wifi cycle scan passphrase 00000000", "wifi.cycle"},
		{"request ping count", "ping"}, {"request traceroute count", "traceroute"},
		{"request pmtu count", "path-mtu"}, {"request ip external", "global-ip"},
		{"request dns resolve count", "dns"}, {"request http check example.test", "http"},
		{"request http download https://example.test", "download"},
	} {
		t.Run(tc.line, func(t *testing.T) {
			parsed, err := ParseShellTokens(strings.Fields(tc.line))
			if err != nil || parsed.Operation.Name != tc.name {
				t.Fatalf("name=%s want=%s err=%v", parsed.Operation.Name, tc.name, err)
			}
			if err := ValidateOperation(parsed.Operation); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestShellNetworkSelectorsAndLiteralArguments(t *testing.T) {
	parsed, err := ParseShellTokens([]string{"request", "traceroute", "count", "via", "192.0.2.1", "via", "192.0.2.2", "ssid", "Lab", "bssid", "aa:bb:cc:dd:ee:ff", "network-id", "949"})
	if err != nil || !slices.Equal(parsed.Operation.Options.TracerouteRequiredHops, []string{"192.0.2.1", "192.0.2.2"}) || parsed.Operation.Command.GetTraceroute().GetSelector().GetSsid() != "Lab" || parsed.Operation.Command.GetTraceroute().GetSelector().GetNetworkId() != "949" {
		t.Fatalf("typed hops/selector changed: %v", err)
	}
	for _, line := range []string{"request ping example.test bssid invalid", "request ping example.test bssid aabb.ccdd.eeff", "show wifi status ip network-id negative", "show wifi status ip network-id 0949", "request ping host count 0", "request ping host count 2147483648", "request ping host count -1", "request ping host count nope", "request pmtu host min-mtu 1500 max-mtu 1200", "request ping host family all", "request ip external family auto", "request traceroute host max-hops 256"} {
		if _, err := ParseShellTokens(strings.Fields(line)); err == nil {
			t.Errorf("accepted invalid %q", line)
		}
	}
	connect, err := ParseShellTokens([]string{"request", "wifi", "connect", "scan with space", "passphrase", "literal with \\ backslash"})
	if err != nil || connect.Connect == nil || connect.Connect.SSID != "scan with space" || connect.Connect.Passphrase != "literal with \\ backslash" {
		t.Fatalf("literal connection arguments changed: %v", err)
	}
}

func TestShellCredentialsResolveWithoutDisclosure(t *testing.T) {
	const secret = "synthetic-env-credential"
	t.Setenv("DROPCHECK_TEST_PSK", secret)
	parsed, err := ParseShellTokens([]string{"request", "wifi", "connect", "Lab", "passphrase-env", "DROPCHECK_TEST_PSK", "security", "wpa3"})
	if err != nil || parsed.Connect == nil || parsed.Connect.Passphrase != secret {
		t.Fatalf("env connection was not resolved safely: %v", err)
	}
	for _, args := range [][]string{
		{"request", "wifi", "connect", "Lab", "passphrase", "another-credential", "passphrase-env", "DROPCHECK_TEST_PSK"},
		{"request", "wifi", "connect", "Lab", "passphrase-env", "DROPCHECK_TEST_MISSING"},
		{"request", "wifi", "connect", "Lab", "passphrase-env", "BAD=NAME"},
		{"request", "wifi", "connect", "Lab", "passphrase", secret, "security", secret},
		{"request", "wifi", "cycle", "Lab", "passphrase", secret, "count", "0"},
	} {
		if _, err := ParseShellTokens(args); err == nil || strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "another-credential") {
			t.Fatalf("invalid credential input accepted or disclosed: %v", err)
		}
	}
	t.Setenv("DROPCHECK_TEST_PSK", "short")
	if _, err := ParseShellTokens([]string{"request", "wifi", "connect", "Lab", "passphrase-env", "DROPCHECK_TEST_PSK"}); err == nil || strings.Contains(err.Error(), "short") {
		t.Fatalf("invalid env value accepted or disclosed: %v", err)
	}
}
