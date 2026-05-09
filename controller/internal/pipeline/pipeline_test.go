package pipeline

import (
	"strings"
	"testing"
)

func TestSplitPreservesQuotedPipes(t *testing.T) {
	parts, err := Split(`show wifi scan detail "Lab|Guest" | match "RSSI|BSSID" | count`)
	if err != nil {
		t.Fatalf("Split() error = %v", err)
	}
	want := []string{`show wifi scan detail "Lab|Guest"`, `match "RSSI|BSSID"`, "count"}
	if len(parts) != len(want) {
		t.Fatalf("Split() = %#v, want %#v", parts, want)
	}
	for i := range want {
		if parts[i] != want[i] {
			t.Fatalf("Split()[%d] = %q, want %q", i, parts[i], want[i])
		}
	}
}

func TestSplitRejectsMalformedPipelines(t *testing.T) {
	for _, line := range []string{
		`show devices |`,
		`show devices || count`,
		`show "unterminated`,
		`show devices \`,
	} {
		t.Run(line, func(t *testing.T) {
			if _, err := Split(line); err == nil {
				t.Fatalf("Split(%q) error = nil", line)
			}
		})
	}
}

func TestParseAndApplyTextStages(t *testing.T) {
	p, err := Parse([]string{`display set`, `match "^set"`, `except secret`, `count`})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got := p.Format(FormatText); got != FormatSet {
		t.Fatalf("Format() = %q, want %q", got, FormatSet)
	}
	if !p.DisplaySet() || p.DisplayJSON() || p.StageCount() != 3 {
		t.Fatalf("pipeline flags: display_set=%t display_json=%t stages=%d", p.DisplaySet(), p.DisplayJSON(), p.StageCount())
	}

	out, err := p.Apply("set standalone enabled\nset standalone upload passphrase secret\nshow config\n")
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if out != "Count: 1 lines\n" {
		t.Fatalf("Apply() = %q", out)
	}
}

func TestParseRejectsInvalidStages(t *testing.T) {
	tests := []struct {
		name  string
		parts []string
		want  string
	}{
		{
			name:  "display after count",
			parts: []string{"count", "display json"},
			want:  "must appear before count",
		},
		{
			name:  "duplicate display",
			parts: []string{"display json", "display set"},
			want:  "display format specified twice",
		},
		{
			name:  "bad regex",
			parts: []string{`match "["`},
			want:  "match regex",
		},
		{
			name:  "unknown pipe",
			parts: []string{"sort"},
			want:  `unknown pipe "sort"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(tt.parts)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Parse() error = %v, want substring %q", err, tt.want)
			}
		})
	}
}
