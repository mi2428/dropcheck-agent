package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestTopLevelHelpOnlyAdvertisesShellAndWatch(t *testing.T) {
	help := renderTopLevelHelp()
	for _, want := range []string{"Usage:", "Global flags:", "--adb PATH", "--listen ADDR", "dropcheck [flags] shell", "dropcheck [flags] watch", "dropcheck --version"} {
		if !strings.Contains(help, want) {
			t.Fatalf("missing %q: %s", want, help)
		}
	}
	for _, removed := range []string{"dropcheck [flags] tui", "one-shot", "--format", "Common commands:", "show devices", "ping <host>", "\t"} {
		if strings.Contains(help, removed) {
			t.Fatalf("help advertises retired surface %q", removed)
		}
	}
}

func TestRemovedEntrypointsNeverStartADB(t *testing.T) {
	path := filepath.Join(t.TempDir(), "adb")
	marker := filepath.Join(t.TempDir(), "called")
	if err := os.WriteFile(path, []byte("#!/bin/sh\ntouch '"+marker+"'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"tui"}, {"tui", "missing.yml"}, {"show", "devices"}, {"ping", "example.test"}, {"--format", "json", "show", "wifi", "status"}, {"wifi", "connect", "Lab", "passphrase", "synthetic-private"}} {
		full := append([]string{"--adb", path}, args...)
		if err := runWithContext(context.Background(), full); err == nil || strings.Contains(err.Error(), "synthetic-private") {
			t.Fatalf("retired entrypoint accepted or disclosed a credential: %v", err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := runWithContext(ctx, []string{"--adb", path, "shell"}); err == nil {
		t.Fatal("canceled Shell entry started")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("invalid entry ran adb: %v", err)
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
	global, rest, err := parseTopLevelArgs([]string{"-listen", "127.0.0.1:37588", "shell"})
	if err != nil || global.ListenAddr != "127.0.0.1:37588" || !slices.Equal(rest, []string{"shell"}) {
		t.Fatalf("global=%+v rest=%v err=%v", global, rest, err)
	}
}
