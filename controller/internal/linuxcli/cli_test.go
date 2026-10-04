package linuxcli

import (
	"slices"
	"strings"
	"testing"
)

func TestArgvBoundaryAndFlatSurface(t *testing.T) {
	opts, rest, err := ExtractOptions([]string{"--format", "json", "--target", "self", "wifi", "connect", "--format", "passphrase", "space separated secret"})
	if err != nil || opts.Target != "self" || opts.Format != "json" || !slices.Equal(rest, []string{"wifi", "connect", "--format", "passphrase", "space separated secret"}) {
		t.Fatal(opts, rest, err)
	}
	parsed, err := Parse(rest)
	if err != nil || parsed.Operation.Command.GetConnectWifi().GetSsid() != "--format" || parsed.Operation.Command.GetConnectWifi().GetPassphrase() != "space separated secret" {
		t.Fatal(err)
	}
	if strings.Contains(parsed.Operation.Command.GetLabel(), "space separated") {
		t.Fatal("secret leaked")
	}
	if _, err := Parse([]string{"ping", "--format"}); err == nil {
		t.Fatal("argv literal must be validated as a host")
	}
	parsed, err = Parse([]string{"ping", "count", "count", "3"})
	if err != nil || parsed.Operation.Command.GetPing().GetHost() != "count" {
		t.Fatal(err)
	}
	parsed, err = Parse([]string{"show", "version"})
	if err != nil || parsed.Kind != Version {
		t.Fatal(err)
	}
	parsed, err = Parse([]string{"show", "devices"})
	if err != nil || parsed.Kind != Devices {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"request", "ping", "host"}, {"ping", "host", "--count", "2"}, {"show", "wifi", "s"}, {"s"}, {"show", "wifi", "scan", "fresh", "timeout", "0"}} {
		if _, err := Parse(args); err == nil {
			t.Errorf("accepted %q", args)
		}
	}
	for _, args := range [][]string{{"--all", "--target", "self", "ping", "host"}, {"--format", "yaml", "ping", "host"}} {
		if _, _, err := ExtractOptions(args); err == nil {
			t.Errorf("accepted flags %q", args)
		}
	}
}
