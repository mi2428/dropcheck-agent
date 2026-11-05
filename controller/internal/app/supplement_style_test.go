package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mi2428/dropcheck-agent/controller/internal/control"
	"github.com/mi2428/dropcheck-agent/controller/internal/controlpb"
	"github.com/mi2428/dropcheck-agent/controller/internal/render"
)

func TestSupplementDefersWidthNotReceiptOrAcquisition(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fake-adb")
	counter := filepath.Join(t.TempDir(), "calls")
	t.Setenv("DROPCHECK_STYLE_CALLS", counter)
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf x >> \"$DROPCHECK_STYLE_CALLS\"\nprintf 'mlo_mode=0 wifi_link_count=1\\n'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	block := collectADBMLOSupplement(t.Context(), &shellState{adbPath: path}, control.AgentInfo{Hello: &controlpb.AgentHello{AdbSerial: "synthetic"}})
	if block == nil {
		t.Fatal("synthetic supplement was not captured")
	}
	var supplements commandResultSupplements
	supplements.addText(block)
	receipt := ""
	for _, width := range []int{0, 120, 24, 0} {
		text := supplements.appendToText("Android result\n", render.Presentation{Width: width})
		compact := strings.Join(strings.Fields(text), "")
		for _, fact := range []string{"Source:adb(hostsupplement)", "mlo_mode:0", "wifi_link_count:1"} {
			if !strings.Contains(compact, fact) {
				t.Fatalf("width %d lost %q", width, fact)
			}
		}
		if width == 0 || width == 120 {
			for line := range strings.SplitSeq(text, "\n") {
				if after, ok := strings.CutPrefix(line, "  Received on controller:"); ok {
					value := strings.TrimSpace(after)
					if receipt != "" && receipt != value {
						t.Fatal("formatting changed the recorded acquisition receipt")
					}
					receipt = value
				}
			}
		}
	}
	if receipt == "" {
		t.Fatal("missing source receipt")
	}
	if calls, err := os.ReadFile(counter); err != nil || string(calls) != "xx" {
		t.Fatal("formatting repeated acquisition", err)
	}
}
