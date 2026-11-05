package adbdiag

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mi2428/dropcheck-agent/controller/internal/adb"
)

func TestInteractiveCollectionPlansAndCanceledSources(t *testing.T) {
	for kind, want := range map[string]int{"shell-full": 10, "shell-wifi": 5, "shell-connectivity": 5, KindDumpsysConnectivityNetworks: 1} {
		specs, err := InteractiveSpecs(kind)
		if err != nil || len(specs) != want {
			t.Fatal(kind, len(specs), err)
		}
		seen := map[string]bool{}
		for _, spec := range specs {
			key := strings.Join(spec.Args, "\x00")
			if seen[key] {
				t.Fatal("duplicate source", kind, spec.Name)
			}
			seen[key] = true
		}
	}
	legacy, _ := Specs(KindFull)
	if len(legacy) != 8 {
		t.Fatal("one-shot collection changed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	bundle, err := CollectInteractive(ctx, adb.Client{Path: "/must-not-execute", Serial: "fake"}, "test", "shell-full", time.Second, nil)
	if err != nil || bundle.OK() || len(bundle.Commands) != 10 {
		t.Fatal(bundle, err)
	}
	for _, result := range bundle.Commands {
		if !result.NotCollected || result.Error == "" {
			t.Fatal("cancellation omitted source status", result)
		}
	}
	// Local false is a stand-in executable, never an ADB/device operation.
	bundle, err = CollectInteractive(context.Background(), adb.Client{Path: "/usr/bin/false", Serial: "fake"}, "test", "shell-full", 10*time.Second, nil)
	if err != nil || bundle.OK() || len(bundle.Commands) != 10 {
		t.Fatal(bundle, err)
	}
	for _, result := range bundle.Commands {
		if result.NotCollected || result.ExitCode == 0 || result.Error == "" || result.StartedAt == "" {
			t.Fatal("independent failure suppressed", result)
		}
	}
}

func TestJunosQACollectionDeadlineRetainsAttemptAndMissingSources(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fake-adb")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf 'safe-reference\\n'\nexec sleep 30\n"), 0700); err != nil {
		t.Fatal(err)
	}
	bundle, err := CollectInteractive(t.Context(), adb.Client{Path: path, Serial: "fake"}, "fake", "shell-full", 500*time.Millisecond, nil)
	if err != nil || bundle.OK() || len(bundle.Commands) != 10 {
		t.Fatal(bundle, err)
	}
	first := bundle.Commands[0]
	if first.NotCollected || !first.TimedOut || first.Error == "" || first.Stdout != "safe-reference\n" {
		t.Fatal("partial attempt lost", first)
	}
	for _, missing := range bundle.Commands[1:] {
		if !missing.NotCollected || missing.StartedAt != "" || missing.Error == "" {
			t.Fatal("post-deadline acquisition", missing)
		}
	}
}
