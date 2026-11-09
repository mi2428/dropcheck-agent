package command

import (
	"os"
	"strings"
	"testing"

	"github.com/mi2428/dropcheck-agent/controller/internal/controlpb"
	"google.golang.org/protobuf/proto"
)

func TestPortableCommandContract(t *testing.T) {
	data, err := os.ReadFile("../../../testdata/command_cases.tsv")
	if err != nil {
		t.Fatal(err)
	}
	covered := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")[1:] {
		cols := strings.Split(line, "\t")
		args, err := SplitArgs(cols[1])
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := ParseShellTokens(args)
		if err != nil || parsed.Path != cols[0] {
			t.Errorf("%s: %+v %v", cols[0], parsed, err)
		}
		covered[cols[0]] = true
	}
	for _, spec := range portableGrammar {
		if !covered[spec.Path] {
			t.Errorf("missing contract case: %s", spec.Path)
		}
	}
	invalid, err := os.ReadFile("../../../testdata/invalid_cases.tsv")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSuffix(string(invalid), "\n"), "\n")[1:] {
		args, err := SplitArgs(line)
		var parsed Parsed
		if err == nil {
			parsed, err = ParseShellTokens(args)
		}
		if err == nil && parsed.Rejection == "" {
			t.Errorf("accepted invalid common input: %s", line)
		}
	}
}

func TestWifiStatusUsesLightweightAcquisition(t *testing.T) {
	status, err := ParseShellTokens(strings.Fields("show wifi status"))
	if err != nil {
		t.Fatal(err)
	}
	connection, err := ParseShellTokens(strings.Fields("show wifi status connection"))
	if err != nil || !proto.Equal(status.Operation.Command, connection.Operation.Command) {
		t.Fatal("summary must reuse the existing Wi-Fi status RPC", err)
	}
	for _, input := range []string{"show wifi status full", "sh wi st f"} {
		full, err := ParseShellTokens(strings.Fields(input))
		if err != nil || full.Path != "show wifi status full" || full.Operation.Options.WifiRenderMode != WifiRenderModeFull {
			t.Fatalf("%s: %+v %v", input, full, err)
		}
		if status.Operation.Options.WifiRenderMode != WifiRenderModeStatus || status.Operation.Options.Detail || !full.Operation.Options.Detail ||
			status.Operation.Command.GetGetWifiStatus() == nil || full.Operation.Command.GetGetWifiDiagnostics() == nil {
			t.Fatal("summary/full acquisition scopes were not separated")
		}
	}
	for _, input := range []string{"show wifi status full full", "show wifi status full ssid Lab", "show wifi status full connection"} {
		if _, err := ParseShellTokens(strings.Fields(input)); err == nil {
			t.Fatalf("accepted unsupported full arguments: %s", input)
		}
	}
}

func TestNetworkIdentityBoundary(t *testing.T) {
	for _, target := range []string{"default", "42"} {
		op, err := NetworkOperation(target)
		if err != nil || ValidateOperation(op) != nil {
			t.Fatal(target, err)
		}
	}
	for _, target := range []string{"0", "01", "-1", "stale", "2147483648"} {
		if _, err := NetworkOperation(target); err == nil {
			t.Errorf("accepted %s", target)
		}
	}
	for _, query := range []*controlpb.GetNetwork{{}, {DefaultNetwork: true, NetworkId: "42"}, {NetworkId: "01"}} {
		op := NewOperation("network.show", &controlpb.RunCommand{Command: &controlpb.RunCommand_GetNetwork{GetNetwork: query}}, Options{})
		if ValidateOperation(op) == nil {
			t.Fatal("accepted inconsistent selector")
		}
	}
}

func TestRawDebugReusesPassiveRPCsAndExplicitNetworkIdentity(t *testing.T) {
	for _, tc := range []struct{ line, view string }{
		{"debug android wifi info", "wifi info"},
		{"debug android wifi scan ssid 'password=Guest' band 6ghz", "wifi scan"},
		{"debug android network capabilities network-id 42", "network capabilities"},
		{"debug android network wifi-info network-id 42", "network wifi-info"},
		{"debug android network link-properties network-id 42", "network link-properties"},
	} {
		args, _ := SplitArgs(tc.line)
		parsed, err := ParseShellTokens(args)
		if err != nil || parsed.Operation.Options.RawView != tc.view || ValidateOperation(parsed.Operation) != nil {
			t.Fatalf("%s: %+v %v", tc.line, parsed, err)
		}
		wire := parsed.Operation.Command
		switch tc.view {
		case "wifi info":
			if wire.GetGetWifiStatus() == nil {
				t.Fatal("debug WifiInfo must reuse passive Wi-Fi status")
			}
		case "wifi scan":
			if wire.GetGetWifiScan() == nil || wire.GetGetFreshWifiScan() != nil || parsed.Operation.Options.WifiScanSSID != "password=Guest" {
				t.Fatal("debug scan changed acquisition or literal filtering")
			}
		default:
			if wire.GetGetNetwork().GetNetworkId() != "42" || parsed.Operation.Options.RawNetworkID != "42" {
				t.Fatal("debug Network lost its explicit identity")
			}
		}
	}
	if _, err := ParseShellTokens(strings.Fields("show adb")); err == nil {
		t.Fatal("interactive raw ADB acquisition must move to debug")
	}
	if parsed, err := ParseShellTokens(strings.Fields("debug adb wifi")); err != nil || parsed.ADBKind == "" {
		t.Fatal("Shell ADB debug acquisition changed", err)
	}
}

func TestRequestNamesPreserveTypedOperations(t *testing.T) {
	for _, tc := range []struct{ shell, name, args string }{
		{"request pmtu", "path-mtu", "example.test min-mtu 1200 max-mtu 1500 family ipv6 timeout 1234"},
		{"request ip external", "global-ip", "family all timeout 1234"},
		{"request dns resolve", "dns", "resolve record AAAA timeout 1234"},
		{"request http check", "http", "download expected-status 204 timeout 1234"},
		{"request http download", "download", "https://example.test/check timeout 1234"},
	} {
		t.Run(tc.shell, func(t *testing.T) {
			args := tc.args + " ssid 'Case Sensitive' bssid aa:bb:cc:dd:ee:ff network-id 42"
			shellTokens, err := SplitArgs(tc.shell + " " + args)
			if err != nil {
				t.Fatal(err)
			}
			interactive, err := ParseShellTokens(shellTokens)
			if err != nil {
				t.Fatal(err)
			}
			var expected Operation
			switch tc.name {
			case "path-mtu":
				expected, err = PathMTUOperation(PathMTUOptions{Host: "example.test", MinMTU: "1200", MaxMTU: "1500", Family: "ipv6", Timeout: "1234"})
			case "global-ip":
				expected, err = GlobalIPOperation("all", "1234")
			case "dns":
				expected, err = DNSOperation("resolve", "AAAA", "1234")
			case "http":
				expected, err = HTTPOperation("download", "204", "1234")
			case "download":
				expected, err = DownloadOperation("https://example.test/check", "1234")
			}
			if err != nil {
				t.Fatal(err)
			}
			selector := &controlpb.NetworkSelector{Ssid: "Case Sensitive", Bssid: "aa:bb:cc:dd:ee:ff", NetworkId: "42"}
			switch c := expected.Command.Command.(type) {
			case *controlpb.RunCommand_PathMtu:
				c.PathMtu.Selector = selector
			case *controlpb.RunCommand_GlobalIp:
				c.GlobalIp.Selector = selector
			case *controlpb.RunCommand_ResolveDns:
				c.ResolveDns.Selector = selector
			case *controlpb.RunCommand_HttpCheck:
				c.HttpCheck.Selector = selector
			case *controlpb.RunCommand_Wget:
				c.Wget.Selector = selector
			}
			if interactive.Path != tc.shell || interactive.Effect != "probe" || !interactive.Operation.Options.Detail || !proto.Equal(interactive.Operation.Command, expected.Command) {
				t.Fatalf("Shell changed the typed operation or selectors: %+v / %+v", interactive, expected)
			}
		})
	}
	for _, old := range []string{"request path-mtu example.test", "request global-ip", "request dns example.test", "request http https://example.test", "request download https://example.test"} {
		if _, err := ParseShellTokens(strings.Fields(old)); err == nil {
			t.Errorf("obsolete interactive spelling accepted: %s", old)
		}
	}
}
