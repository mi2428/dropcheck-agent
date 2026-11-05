package adbdiag

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/mi2428/dropcheck-agent/controller/internal/pipeline"
	"github.com/mi2428/dropcheck-agent/controller/internal/render"
)

func styleADBField(t *testing.T, text, label, value string, indent int) int {
	t.Helper()
	prefix := strings.Repeat(" ", indent) + label + ":"
	for line := range strings.SplitSeq(text, "\n") {
		if after, ok := strings.CutPrefix(line, prefix); ok {
			tail := after
			if !strings.HasPrefix(tail, " ") || strings.TrimSpace(tail) != value {
				t.Errorf("invalid %s field: %q", label, line)
			}
			return ansi.StringWidth(prefix) + len(tail) - len(strings.TrimLeft(tail, " "))
		}
	}
	t.Errorf("missing %s: field at indent %d", label, indent)
	return -1
}

func TestStyleConsistencyADBMLORecords(t *testing.T) {
	text := RenderMLOSummary(ParseMLOSummary("mlo_mode=0 wifi_link_count=1"))
	if !strings.HasPrefix(text, "ADB MLO Snapshot\n") {
		t.Fatal("heading changed")
	}
	if styleADBField(t, text, "mlo_mode", "0", 2) != styleADBField(t, text, "wifi_link_count", "1", 2) {
		t.Error("ADB MLO values must align")
	}
}

func TestStyleConsistencyADBSharedRecords(t *testing.T) {
	var b strings.Builder
	writeKVSection(&b, "Generated metadata", kv("A", 0), kv("長い項目", false))
	text := render.LayoutText(b.String(), render.Presentation{})
	if styleADBField(t, text, "A", "0", 2) != styleADBField(t, text, "長い項目", "false", 2) {
		t.Error("shared ADB helper must align by display width")
	}
}

func TestStyleConsistencyADBIPv6Records(t *testing.T) {
	text := RenderIPv6RASummary(IPv6RASummary{Interface: "wlan0", AcceptRA: "0", Advertisements: []IPv6RAAdvertisement{{Source: "fe80::1", Destination: "ff02::1", RouterLifetime: "0"}}})
	styleADBField(t, text, "interface", "wlan0", 2)
	styleADBField(t, text, "accept_ra", "0", 2)
	a := styleADBField(t, text, "src", "fe80::1", 6)
	b := styleADBField(t, text, "router_lifetime", "0", 6)
	if a != b {
		t.Error("RA advertisement values must align")
	}
	if !strings.Contains(text, "  advertisements\n    ra_1\n") {
		t.Error("RA headings must retain the two-space hierarchy")
	}
}

func TestStyleConsistencyADBBundleMetadata(t *testing.T) {
	text, err := Render(Bundle{Serial: "fixture", Kind: KindFull, Commands: []CommandResult{{Name: "capture", Command: "cmd wifi status", ExitCode: 0, NotCollected: true}}})
	if err != nil {
		t.Fatal(err)
	}
	styleADBField(t, text, "serial", "fixture", 2)
	styleADBField(t, text, "exit", "0", 2)
	styleADBField(t, text, "not_collected", "true", 2)
}

func TestStyleConsistencyADBRawAndTableRoles(t *testing.T) {
	raw := "native foo=0: 2001:db8::1 password=qa-synthetic-secret"
	bundle := Bundle{Commands: []CommandResult{{Stdout: raw}}}
	text, err := Render(bundle)
	if err != nil || strings.Contains(text, "qa-synthetic-secret") || !strings.Contains(text, "foo=0: 2001:db8::1") {
		t.Fatal("safe text changed native syntax or exposed a credential")
	}
	encoded, err := pipeline.JSON(text, bundle.OK())
	var decoded struct {
		OK    bool     `json:"ok"`
		Lines []string `json:"lines"`
	}
	if err != nil || json.Unmarshal([]byte(encoded), &decoded) != nil || !decoded.OK || !strings.Contains(strings.Join(decoded.Lines, "\n"), "foo=0: 2001:db8::1") || strings.Contains(encoded, "qa-synthetic-secret") || bundle.Commands[0].Stdout != raw {
		t.Fatal("safe Shell JSON changed native values, exposed a credential or modified acquisition data")
	}
	table := RenderMLOSummary(MLOSummary{LinkStats: []MLOLinkStat{{LinkID: "0", RadioID: "1", RSSIMgmt: "-40", BeaconRx: "0"}}})
	if !strings.Contains(table, "\n\nlink_id") || !strings.HasSuffix(table, "\n\n") || strings.Contains(table, "link_id:") {
		t.Fatal("table headers are not scalar fields")
	}
	if render.TextBlock("https://example.test/a:b?q=x=y\n", render.Presentation{}) != "https://example.test/a:b?q=x=y\n" {
		t.Fatal("raw punctuation was parsed")
	}
}
