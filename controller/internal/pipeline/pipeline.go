package pipeline

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"dropcheck/controller/internal/command"
)

// Format selects a renderer output format.
type Format string

const (
	// FormatText renders human-readable tabular or paragraph output.
	FormatText Format = "text"
	// FormatJSON renders machine-readable JSON output.
	FormatJSON Format = "json"
)

// Pipeline is a compiled list of output transformations.
//
// A zero-value Pipeline leaves output unchanged and uses the caller's default
// output format.
type Pipeline struct {
	displayJSON bool
	stages      []stage
}

type stage struct {
	op      string
	pattern string
	re      *regexp.Regexp
}

// Split separates a shell line into command and pipeline stage strings.
//
// Pipe characters inside single or double quotes are preserved. Returned parts
// are trimmed, and empty stages are rejected.
func Split(line string) ([]string, error) {
	var parts []string
	var b strings.Builder
	var quote rune
	escaped := false

	flush := func() {
		parts = append(parts, strings.TrimSpace(b.String()))
		b.Reset()
	}

	for _, r := range line {
		if escaped {
			b.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' {
			// Keep the backslash in the segment. command.SplitArgs will consume
			// it later, but Split still needs to know that the next rune cannot
			// terminate a quoted or piped segment.
			b.WriteRune(r)
			escaped = true
			continue
		}
		if quote != 0 {
			b.WriteRune(r)
			if r == quote {
				quote = 0
			}
			continue
		}
		switch r {
		case '\'', '"':
			quote = r
			b.WriteRune(r)
		case '|':
			// A pipe is only structural outside quotes. Quoted pipes remain part
			// of the command text passed to command.SplitArgs.
			flush()
		default:
			b.WriteRune(r)
		}
	}
	if escaped {
		return nil, fmt.Errorf("trailing escape")
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated quote")
	}
	flush()
	if slices.Contains(parts, "") {
		return nil, fmt.Errorf("empty pipeline stage")
	}
	return parts, nil
}

// Parse compiles pipeline stage strings returned by Split.
//
// Supported stages are "display json", "match <regex>", "except <regex>",
// "count", and "no-more". "display json" must be placed before "count".
func Parse(parts []string) (Pipeline, error) {
	var pipeline Pipeline
	countSeen := false
	for _, part := range parts {
		args, err := command.SplitArgs(part)
		if err != nil {
			return Pipeline{}, err
		}
		if len(args) == 0 {
			return Pipeline{}, fmt.Errorf("empty pipeline stage")
		}
		switch args[0] {
		case "display":
			if len(args) != 2 || args[1] != "json" {
				return Pipeline{}, fmt.Errorf("usage: | display json")
			}
			if countSeen {
				return Pipeline{}, fmt.Errorf("display json must appear before count")
			}
			pipeline.displayJSON = true
		case "match", "except":
			if len(args) < 2 {
				return Pipeline{}, fmt.Errorf("usage: | %s <regex>", args[0])
			}
			pattern := strings.Join(args[1:], " ")
			re, err := regexp.Compile(pattern)
			if err != nil {
				return Pipeline{}, fmt.Errorf("%s regex: %w", args[0], err)
			}
			pipeline.stages = append(pipeline.stages, stage{op: args[0], pattern: pattern, re: re})
		case "count":
			if len(args) != 1 {
				return Pipeline{}, fmt.Errorf("usage: | count")
			}
			pipeline.stages = append(pipeline.stages, stage{op: "count"})
			countSeen = true
		case "no-more":
			if len(args) != 1 {
				return Pipeline{}, fmt.Errorf("usage: | no-more")
			}
		default:
			return Pipeline{}, fmt.Errorf("unknown pipe %q", args[0])
		}
	}
	return pipeline, nil
}

// Format returns the effective output format after applying the pipeline.
func (p Pipeline) Format(defaultFormat Format) Format {
	if p.displayJSON {
		return FormatJSON
	}
	return defaultFormat
}

// Apply applies text-filtering pipeline stages to rendered output.
//
// The "display json" and "no-more" stages do not change the text here; they are
// consumed by callers when selecting format or pager behavior.
func (p Pipeline) Apply(text string) (string, error) {
	out := text
	for _, stage := range p.stages {
		switch stage.op {
		case "match":
			out = filterLines(out, func(line string) bool { return stage.re.MatchString(line) })
		case "except":
			out = filterLines(out, func(line string) bool { return !stage.re.MatchString(line) })
		case "count":
			out = fmt.Sprintf("Count: %d lines\n", countNonEmptyLines(out))
		default:
			return "", fmt.Errorf("unsupported pipe %q", stage.op)
		}
	}
	return out, nil
}

// DisplayJSON reports whether the pipeline requested JSON output.
func (p Pipeline) DisplayJSON() bool {
	return p.displayJSON
}

// StageCount returns the number of text-transforming stages in the pipeline.
func (p Pipeline) StageCount() int {
	return len(p.stages)
}

func filterLines(text string, keep func(string) bool) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	var out []string
	for _, line := range lines {
		if keep(line) {
			out = append(out, line)
		}
	}
	if len(out) == 0 {
		return ""
	}
	return strings.Join(out, "\n") + "\n"
}

func countNonEmptyLines(text string) int {
	trimmed := strings.TrimRight(text, "\n")
	if trimmed == "" {
		return 0
	}
	return len(strings.Split(trimmed, "\n"))
}
