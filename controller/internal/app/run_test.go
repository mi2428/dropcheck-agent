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
		"show wifi scan fresh [options]",
		"request ping 1.1.1.1 --count 5",
	} {
		if !strings.Contains(help, want) {
			t.Fatalf("topLevelHelp() missing %q:\n%s", want, help)
		}
	}
	if strings.Contains(help, "request wifi scan") {
		t.Fatalf("topLevelHelp() contains stale request wifi scan help:\n%s", help)
	}
	if strings.Contains(help, "\t") {
		t.Fatalf("topLevelHelp() contains tab indentation:\n%s", help)
	}
	for _, want := range []string{
		"  shell                                 start the Controller Shell",
		"  --format text|json                    output format for one-shot commands",
		"  request ping <host> [options]         run ICMP ping",
	} {
		if !strings.Contains(help, want) {
			t.Fatalf("topLevelHelp() missing aligned row %q:\n%s", want, help)
		}
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
