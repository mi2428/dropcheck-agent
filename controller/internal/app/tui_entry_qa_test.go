package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTUIEntryHelpAndInvalidInputNeverStartADB(t *testing.T) {
	dir := t.TempDir()
	marker, adb := filepath.Join(dir, "called"), filepath.Join(dir, "adb")
	if err := os.WriteFile(adb, []byte("#!/bin/sh\ntouch '"+marker+"'\nexit 97\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"tui", "--help"}, {"tui", "-h"}, {"tui", "help"},
		{"watch", "--help"}, {"watch", "-c", "missing.yml"},
		{"tui"}, {"tui", "-c", "missing.yml"},
		{"tui", "--target="}, {"tui", "--jsonl="},
		{"tui", "--no-tui=false"}, {"tui", "--help=false"},
		{"tui", "--unknown"}, {"tui", "--target"}, {"tui", "--jsonl"},
	} {
		err := runWithContext(context.Background(), append([]string{"--adb", adb}, args...))
		wantHelp := len(args) == 2 && args[0] == "tui" && (args[1] == "--help" || args[1] == "-h" || args[1] == "help")
		if (err == nil) != wantHelp {
			t.Fatalf("args=%v err=%v", args, err)
		}
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("help or rejected input invoked ADB: %v", err)
	}
	for _, help := range []func(*bytes.Buffer){func(b *bytes.Buffer) { writeTopLevelHelp(b) }, func(b *bytes.Buffer) { writeTUIHelp(b) }} {
		var b bytes.Buffer
		help(&b)
		if strings.Contains(b.String(), "watch") || !strings.Contains(b.String(), "tui -c CONFIG.yml") {
			t.Fatalf("stale help: %s", b.String())
		}
	}
}

func TestTUIExplicitEmptyOptionsDoNotFallBackToDefaults(t *testing.T) {
	for _, option := range []string{"--config=", "--target=", "--jsonl="} {
		if _, err := parseTUIOptions([]string{"-c", "unused.yml", option}); err == nil {
			t.Fatalf("explicit empty option accepted: %s", option)
		}
	}
}
