package command

import (
	"fmt"
	"strings"
	"testing"
)

func TestRound2CredentialByteAndControlBoundaries(t *testing.T) {
	for _, tc := range []struct {
		psk   string
		valid bool
	}{
		{"", false}, {"1234567", false}, {"12345678", true},
		{strings.Repeat("x", 63), true}, {strings.Repeat("x", 64), false}, {strings.Repeat("A", 64), true},
		{strings.Repeat("é", 4), true}, {strings.Repeat("é", 32), false},
		{"abc\x00defgh", false}, {"abc\tdefgh", false}, {"abc\ndefgh", false},
	} {
		for _, path := range []string{"wifi connect Lab passphrase", "wifi cycle Lab passphrase", "use Lab passphrase", "set default passphrase"} {
			input := path + " '" + tc.psk + "'"
			want := tc.valid || path == "set default passphrase" && tc.psk == ""
			tokens, lexErr := SplitArgs(input)
			var err error
			var parsed Parsed
			if lexErr == nil {
				parsed, err = ParseShellTokens(tokens)
			} else {
				err = lexErr
			}
			if got := err == nil && parsed.Rejection == ""; got != want {
				t.Errorf("%s, bytes=%d: accepted=%v, want=%v", path, len(tc.psk), got, want)
			}
		}
	}
}

func TestRound2NumericAndOptionOrderBoundaries(t *testing.T) {
	for _, tc := range []struct {
		raw   string
		valid bool
	}{{"0", false}, {"1", true}, {"2147483647", true}, {"2147483648", false}, {"+1", false}, {"-1", false}, {"1e3", false}, {"１", false}} {
		for _, order := range []string{"ping example.test timeout %s count 1", "ping example.test count 1 timeout %s"} {
			tokens, _ := SplitArgs(fmt.Sprintf(order, tc.raw))
			_, err := ParseShellTokens(tokens)
			if (err == nil) != tc.valid {
				t.Errorf("%s: valid=%v, error=%v", fmt.Sprintf(order, tc.raw), tc.valid, err)
			}
		}
	}
}

func TestRound2CanonicalBSSIDForms(t *testing.T) {
	for _, tc := range []struct {
		value string
		valid bool
	}{
		{"02:00:00:11:22:33", true}, {"AA:BB:CC:DD:EE:FF", true},
		{"02-00-00-11-22-33", false}, {"0200.0011.2233", false}, {"02:00:00:11:22", false},
	} {
		tokens, _ := SplitArgs("wifi assert bssid " + tc.value)
		_, err := ParseShellTokens(tokens)
		if (err == nil) != tc.valid {
			t.Errorf("BSSID %q accepted=%v", tc.value, err == nil)
		}
	}
}

func TestRound2ProbeEndpointTrustBoundaries(t *testing.T) {
	for _, tc := range []struct {
		host  string
		valid bool
	}{
		{"192.0.2.1", true}, {"2001:db8::1", true}, {"example.test", true},
		{"999.0.0.1", false}, {"256.1.1.1", false}, {"2001:db8:::1", false},
	} {
		for _, path := range []string{"ping", "traceroute", "path-mtu"} {
			tokens, _ := SplitArgs(path + " " + tc.host)
			_, err := ParseShellTokens(tokens)
			if (err == nil) != tc.valid {
				t.Errorf("%s %q accepted=%v, want=%v", path, tc.host, err == nil, tc.valid)
			}
		}
	}
	for _, port := range []int{443, 65535, 65536, 2147483648} {
		for _, path := range []string{"http", "download"} {
			input := fmt.Sprintf("%s https://example.test:%d/", path, port)
			tokens, _ := SplitArgs(input)
			_, err := ParseShellTokens(tokens)
			if (err == nil) != (port <= 65535) {
				t.Errorf("%s accepted=%v", input, err == nil)
			}
		}
	}
}
