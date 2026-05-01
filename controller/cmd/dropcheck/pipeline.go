package main

import (
	"fmt"
	"regexp"
	"strings"
)

type outputFormat string

const (
	outputText outputFormat = "text"
	outputJSON outputFormat = "json"
)

type pipePipeline struct {
	displayJSON bool
	stages      []pipeStage
}

type pipeStage struct {
	op      string
	pattern string
	re      *regexp.Regexp
}

func splitPipeline(line string) ([]string, error) {
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
	for _, part := range parts {
		if part == "" {
			return nil, fmt.Errorf("empty pipeline stage")
		}
	}
	return parts, nil
}

func parsePipePipeline(parts []string) (pipePipeline, error) {
	var pipeline pipePipeline
	for _, part := range parts {
		args, err := splitArgs(part)
		if err != nil {
			return pipePipeline{}, err
		}
		if len(args) == 0 {
			return pipePipeline{}, fmt.Errorf("empty pipeline stage")
		}
		switch args[0] {
		case "display":
			if len(args) != 2 || args[1] != "json" {
				return pipePipeline{}, fmt.Errorf("usage: | display json")
			}
			pipeline.displayJSON = true
		case "match", "except":
			if len(args) < 2 {
				return pipePipeline{}, fmt.Errorf("usage: | %s <regex>", args[0])
			}
			pattern := strings.Join(args[1:], " ")
			re, err := regexp.Compile(pattern)
			if err != nil {
				return pipePipeline{}, fmt.Errorf("%s regex: %w", args[0], err)
			}
			pipeline.stages = append(pipeline.stages, pipeStage{op: args[0], pattern: pattern, re: re})
		case "count":
			if len(args) != 1 {
				return pipePipeline{}, fmt.Errorf("usage: | count")
			}
			pipeline.stages = append(pipeline.stages, pipeStage{op: "count"})
		case "no-more":
			if len(args) != 1 {
				return pipePipeline{}, fmt.Errorf("usage: | no-more")
			}
		default:
			return pipePipeline{}, fmt.Errorf("unknown pipe %q", args[0])
		}
	}
	return pipeline, nil
}

func (p pipePipeline) format(defaultFormat outputFormat) outputFormat {
	if p.displayJSON {
		return outputJSON
	}
	return defaultFormat
}

func (p pipePipeline) apply(text string) (string, error) {
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
