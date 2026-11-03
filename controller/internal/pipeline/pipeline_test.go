package pipeline

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestPortablePipelineContract(t *testing.T) {
	data, err := os.ReadFile("../../../shell/pipeline_cases.tsv")
	if err != nil {
		t.Fatal(err)
	}
	decode := func(s string) string { return strings.ReplaceAll(s, `\n`, "\n") }
	for _, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")[1:] {
		cols := strings.Split(line, "\t")
		t.Run(cols[0], func(t *testing.T) {
			parts, err := Split("show version | " + cols[0])
			var p Pipeline
			if err == nil {
				p, err = Parse(parts[1:])
			}
			if cols[3] == "false" {
				if err == nil {
					t.Fatal("accepted invalid pipeline")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			out, err := p.Apply(decode(cols[1]))
			if err != nil || out != decode(cols[2]) {
				t.Fatalf("got %q, %v; want %q", out, err, decode(cols[2]))
			}
		})
	}
}

func TestPatternBoundsAndJSON(t *testing.T) {
	if _, err := Parse([]string{"match " + strings.Repeat("x", 513)}); err == nil {
		t.Fatal("unbounded pattern")
	}
	if _, err := Parse(strings.Fields(strings.Repeat("count ", 17))); err == nil {
		t.Fatal("unbounded pipeline")
	}
	encoded, err := JSON("a\n\nquoted: \"\\\n", false)
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		OK    bool
		Lines []string
	}
	if err := json.Unmarshal([]byte(encoded), &body); err != nil || body.OK || len(body.Lines) != 3 {
		t.Fatalf("invalid envelope: %s %v", encoded, err)
	}
}

func TestSplitPreservesQuotedAndEscapedPipes(t *testing.T) {
	parts, err := Split(`show wifi scan detail "Lab|Guest" | match "RSSI|BSSID" | count`)
	if err != nil || len(parts) != 3 {
		t.Fatal(parts, err)
	}
	parts, err = Split(`show wifi scan detail Lab\|Guest | count`)
	if err != nil || len(parts) != 2 {
		t.Fatal(parts, err)
	}
}
