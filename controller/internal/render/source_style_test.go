package render

import (
	"strings"
	"testing"
)

func TestSourceRecordsKeepBoundariesPrivacyAndNativeText(t *testing.T) {
	rows := [][2]string{{"Source", "adb"}, {"長い項目", "first\nsecond"}, {"        深い項目", "2001:db8::1"}, {"SSID", "password=Guest"}, {"Password", "source-style-secret"}, {"URL", "https://user:source-style-secret@example.test/a:b?token=source-style-secret&ordinary=x=y"}}
	logical := SourceFields(rows)
	if !strings.Contains(logical, "Source\tadb") || strings.Contains(logical, "source-style-secret") {
		t.Fatal("source fields lost ownership or privacy")
	}
	for _, width := range []int{0, 8, 24, 40, 80, 120} {
		text := LayoutText(logical, Presentation{Width: width})
		for _, want := range []string{"Source:adb", "Password:<redacted>", "SSID:password=Guest", "firstsecond", "ordinary=x=y"} {
			if !strings.Contains(alignmentCompact(text), want) {
				t.Fatalf("width %d lost %q", width, want)
			}
		}
		for line := range strings.SplitSeq(text, "\n") {
			if width > 0 && displayWidth(line) > width {
				t.Fatalf("width %d overflow: %q", width, line)
			}
		}
	}
}

func TestSourcedTableSharesRecordFallbackWithoutParsingValues(t *testing.T) {
	for _, width := range []int{0, 8, 24, 80} {
		text := Table([]string{"SSID", "Password", "VALUE"}, [][]string{{"password=Guest", "source-style-secret", "2001:db8::1"}}, Presentation{Width: width})
		if strings.Contains(text, "source-style-secret") || !strings.Contains(alignmentCompact(text), "password=Guest") || !strings.Contains(alignmentCompact(text), "2001:db8::1") {
			t.Fatal("table changed identities/values or leaked a secret")
		}
		if width == 8 && !strings.Contains(alignmentCompact(text), "SSID:password=Guest") {
			t.Fatal("narrow table did not select owned records")
		}
	}
}
