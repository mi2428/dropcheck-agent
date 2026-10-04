package harness

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReviewLiveAdapterStaticValidationDoesNotBindFakeAgent(t *testing.T) {
	if marker := os.Getenv("DROPCHECK_REVIEW_STATIC_MARKER"); marker != "" {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		Run(t, Plan{Networks: []Network{WiFi("lab").SSID("Lab").Agent("synthetic-a")}}, WithContext(ctx), WithSerial("synthetic-a"), WithADBPath(os.Getenv("DROPCHECK_REVIEW_STATIC_ADB")))
		return
	}
	dir := t.TempDir()
	marker, adb := filepath.Join(dir, "reached"), filepath.Join(dir, "fake-adb")
	if err := os.WriteFile(adb, []byte("#!/bin/sh\n: > \"$DROPCHECK_REVIEW_STATIC_MARKER\"\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestReviewLiveAdapterStaticValidationDoesNotBindFakeAgent$", "-test.v")
	cmd.Env = append(os.Environ(), "DROPCHECK_REVIEW_STATIC_MARKER="+marker, "DROPCHECK_REVIEW_STATIC_ADB="+adb)
	out, _ := cmd.CombinedOutput()
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("static validation rejected real agent selector before fake session: %s", out)
	}
	if strings.Contains(string(out), "selected agent is not connected") {
		t.Fatalf("fabricated preflight binding rejected selector: %s", out)
	}
}
