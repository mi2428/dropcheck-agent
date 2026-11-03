package shell

import (
	"strings"
	"testing"
)

func TestJunosQARejectedAbbreviatedTestsKeepTheirIdentity(t *testing.T) {
	for _, input := range []string{"te wi require-ip BAD", "te li ssid Lab family BAD", "te la ssid Lab", "te wi | trim 0", "te wi ssid 'unfinished", "test internet"} {
		parsed, err := ParseLine(input)
		if err != nil || parsed.Rejection == "" || parsed.Kind != WifiTest && parsed.Kind != Profile {
			t.Errorf("rejected attempt cannot replace stale PASS: %q %+v %v", input, parsed, err)
		}
		if HistorySafe(input) {
			t.Errorf("rejected input retained: %q", input)
		}
	}
	for _, input := range []string{"request", "test", "monitor", "set cli", "help request wifi"} {
		parsed, err := ParseLine(input)
		if err != nil || parsed.Kind != Help || parsed.Operation.Command != nil {
			t.Fatal(input, parsed, err)
		}
	}
}

func TestJunosQAHelpPrefixAndIdentityNamespaces(t *testing.T) {
	if got := strings.Join(CompleteLine("h req wi "), "\n"); !strings.Contains(got, "connect") {
		t.Error("deep help prefix completion:", got)
	}
	for _, ssid := range []string{"123", "0", "02:00:00:00:00:01", " ", "eht"} {
		parsed, err := ParseLine("request wifi forget ssid '" + ssid + "'")
		if err != nil || parsed.Operation.Command.GetForgetWifi().GetSsid() != ssid || parsed.Operation.Command.GetForgetWifi().Target != "" {
			t.Fatal(ssid, parsed, err)
		}
	}
	for _, input := range []string{"show wifi status", "show wifi status eht", "show wifi scan", "show network default", "show network id 12", "show adb", "request wifi scan", "test wifi"} {
		if HistorySafe(input) {
			t.Error("broadened history allowlist:", input)
		}
	}
}
