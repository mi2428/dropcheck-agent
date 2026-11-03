package pipeline

import (
	"fmt"
	"strings"
	"testing"
)

func TestRound2PipelineBoundsAndAlgebra(t *testing.T) {
	for _, count := range []int{16, 17} {
		_, err := Parse(strings.Split(strings.TrimSuffix(strings.Repeat("count|", count), "|"), "|"))
		if (err == nil) != (count == 16) {
			t.Errorf("stage boundary %d: %v", count, err)
		}
	}
	for _, pattern := range []string{strings.Repeat("é", 256), strings.Repeat("é", 257)} {
		_, err := Parse([]string{"match '" + pattern + "'"})
		if (err == nil) != (len(pattern) <= 512) {
			t.Errorf("UTF-8 pattern boundary %d: %v", len(pattern), err)
		}
	}
	for sample := range 257 {
		var source strings.Builder
		for line := range sample {
			if line%3 == 0 {
				source.WriteString(fmt.Sprintf("hit-%d", line))
			}
			if line%3 == 1 {
				source.WriteString("日本語")
			}
			source.WriteByte('\n')
		}
		for _, stage := range []string{"match '^hit'", "except '^hit'", "begin '^hit'", "match '^$'"} {
			p, err := Parse([]string{stage})
			if err != nil {
				t.Fatal(err)
			}
			once, _ := p.Apply(source.String())
			twice, _ := p.Apply(once)
			if once != twice {
				t.Errorf("stage not idempotent: %s sample %d", stage, sample)
			}
		}
		keep, _ := Parse([]string{"match '^hit'"})
		drop, _ := Parse([]string{"except '^hit'"})
		a, _ := keep.Apply(source.String())
		b, _ := drop.Apply(source.String())
		if len(Lines(a))+len(Lines(b)) != sample {
			t.Errorf("line conservation failed for sample %d", sample)
		}
	}
}
