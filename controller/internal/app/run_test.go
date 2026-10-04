package app

import (
	"bytes"
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestTopLevelHelpIncludesFlagsCommandsAndExamples(t *testing.T) {
	help := renderTopLevelHelp()
	for _, want := range []string{
		"Usage:",
		"Global flags:",
		"--adb PATH",
		"--listen ADDR",
		"CLI output and target flags:",
		"--format text|json",
		"Common commands:",
		"show devices",
		"show wifi scan [fresh] [brief] [mlo]",
		"ping 1.1.1.1 count 5",
	} {
		if !strings.Contains(help, want) {
			t.Fatalf("topLevelHelp() missing %q:\n%s", want, help)
		}
	}
	if strings.Contains(help, "request wifi scan") {
		t.Fatalf("topLevelHelp() contains stale request wifi scan help:\n%s", help)
	}
	for _, removed := range []string{"standalone", "show config", "configure <set|delete>"} {
		if strings.Contains(help, removed) {
			t.Fatalf("topLevelHelp() advertises removed control %q:\n%s", removed, help)
		}
	}
	if strings.Contains(help, "\t") {
		t.Fatalf("topLevelHelp() contains tab indentation:\n%s", help)
	}
	for _, want := range []string{
		"  shell                                 start the Controller Shell",
		"  --format text|json                    output format for one-shot commands",
		"  ping <host> [count <value>]",
	} {
		if !strings.Contains(help, want) {
			t.Fatalf("topLevelHelp() missing aligned row %q:\n%s", want, help)
		}
	}
}

func TestHelpTopicUsesLiveGrammarTable(t *testing.T) {
	var out bytes.Buffer
	writeCommandHelp(&out, "wifi")
	if !strings.Contains(out.String(), "wifi cycle <ssid> passphrase <value>") || !strings.Contains(out.String(), "wifi monitor") || strings.Contains(out.String(), "request") {
		t.Fatalf("wifi topic drift: %s", out.String())
	}
	out.Reset()
	writeCommandHelp(&out, "check")
	if !strings.Contains(out.String(), "unsupported") {
		t.Fatalf("unavailable profile advertised: %s", out.String())
	}
}

func renderTopLevelHelp() string {
	var b bytes.Buffer
	writeTopLevelHelp(&b)
	return b.String()
}

func TestParseTopLevelArgsRecognizesFlagPackageHelpForms(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"-help"}, {"-h"}} {
		_, _, err := parseTopLevelArgs(args)
		if !errors.Is(err, errHelpRequested) {
			t.Fatalf("parseTopLevelArgs(%#v) error = %v, want help", args, err)
		}
	}
}

func TestParseTopLevelArgsAcceptsSingleDashListen(t *testing.T) {
	global, rest, err := parseTopLevelArgs([]string{"-listen", "127.0.0.1:37588", "show", "devices"})
	if err != nil {
		t.Fatalf("parseTopLevelArgs() error = %v", err)
	}
	if global.ListenAddr != "127.0.0.1:37588" {
		t.Fatalf("listen = %q", global.ListenAddr)
	}
	if !slices.Equal(rest, []string{"show", "devices"}) {
		t.Fatalf("rest = %#v", rest)
	}
}
