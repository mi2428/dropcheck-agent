package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExplicitTUINonTTYFailsBeforePlanOrADB(t *testing.T) {
	input, output, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	defer output.Close()
	marker := filepath.Join(t.TempDir(), "adb-called")
	adb := filepath.Join(t.TempDir(), "fake-adb")
	t.Setenv("DROPCHECK_TEST_TUI_MARKER", marker)
	if err := os.WriteFile(adb, []byte("#!/bin/sh\n: > \"$DROPCHECK_TEST_TUI_MARKER\"\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	err = runTUIFiles(context.Background(), shellOptions{ADBPath: adb}, []string{"/nonexistent/synthetic-plan.yml"}, input, output)
	if err == nil || !strings.Contains(err.Error(), "requires terminal input and output") {
		t.Fatalf("non-TTY error = %v", err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("ADB was invoked: %v", err)
	}
}

func TestTUIHelpWorksWithoutTTYOrSession(t *testing.T) {
	input, output, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	defer output.Close()
	if err := runTUIFiles(context.Background(), shellOptions{ADBPath: "/nonexistent/synthetic-adb"}, []string{"--help"}, input, output); err != nil {
		t.Fatal(err)
	}
}
