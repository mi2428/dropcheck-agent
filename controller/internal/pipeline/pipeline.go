package pipeline

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/mi2428/dropcheck-agent/controller/internal/command"
)

type Pipeline struct {
	displayJSON bool
	stages      []stage
}

type stage struct {
	op string
	re *regexp.Regexp
	n  int
}

func Split(line string) ([]string, error) {
	lexed, err := command.Lex(line, false)
	if err != nil {
		return nil, err
	}
	var parts []string
	start := 0
	for _, token := range lexed.Tokens {
		if token.Pipe {
			parts = append(parts, strings.TrimSpace(line[start:token.Start]))
			start = token.End
		}
	}
	parts = append(parts, strings.TrimSpace(line[start:]))
	if len(parts) == 1 && parts[0] == "" {
		return parts, nil
	}
	if slices.Contains(parts, "") {
		return nil, fmt.Errorf("empty pipeline stage")
	}
	return parts, nil
}

// PortablePattern deliberately excludes engine-specific flags, Unicode classes,
// backreferences and zero-width extensions. Both engines execute in linear time.
func PortablePattern(pattern string) bool {
	extensions := pattern
	for _, flag := range []string{"(?i)", "(?m)", "(?s)", "(?-i)", "(?-m)", "(?-s)"} {
		extensions = strings.ReplaceAll(extensions, flag, "")
	}
	if len(pattern) == 0 || len(pattern) > 512 || strings.Contains(extensions, "(?") {
		return false
	}
	escaped := false
	for _, r := range pattern {
		if escaped {
			if !strings.ContainsRune(`\\.^$[]()|*+?{}-dDsSwWnrtf`, r) {
				return false
			}
			escaped = false
		} else if r == '\\' {
			escaped = true
		}
	}
	return !escaped
}

func parseFilterCount(op string, value string) (int, error) {
	n, err := strconv.ParseUint(value, 10, 32)
	if err != nil || n == 0 || n > 2147483647 {
		return 0, fmt.Errorf("%s requires a positive integer in 1..2147483647", op)
	}
	return int(n), nil
}

func Parse(parts []string) (Pipeline, error) {
	var p Pipeline
	if len(parts) > 16 {
		return p, fmt.Errorf("pipeline exceeds 16 stages")
	}
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
			if p.displayJSON {
				return Pipeline{}, fmt.Errorf("display format specified twice")
			}
			p.displayJSON = true
		case "find", "match", "except":
			if len(args) != 2 {
				return Pipeline{}, fmt.Errorf("usage: | %s <quoted-pattern>", args[0])
			}
			if !PortablePattern(args[1]) {
				return Pipeline{}, fmt.Errorf("%s regex: unsupported portable pattern (1..512 UTF-8 bytes)", args[0])
			}
			re, err := regexp.Compile(args[1])
			if err != nil {
				return Pipeline{}, fmt.Errorf("%s regex: invalid pattern", args[0])
			}
			p.stages = append(p.stages, stage{op: args[0], re: re})
		case "last", "trim":
			if len(args) != 2 {
				return Pipeline{}, fmt.Errorf("usage: | %s <positive-integer>", args[0])
			}
			n, err := parseFilterCount(args[0], args[1])
			if err != nil {
				return Pipeline{}, err
			}
			p.stages = append(p.stages, stage{op: args[0], n: n})
		case "count":
			if len(args) != 1 {
				return Pipeline{}, fmt.Errorf("usage: | count")
			}
			p.stages = append(p.stages, stage{op: "count"})
		default:
			return Pipeline{}, fmt.Errorf("unsupported pipe")
		}
	}
	if p.DisplayJSON() && len(p.stages) > 0 {
		return Pipeline{}, fmt.Errorf("display json cannot be combined with line filters")
	}
	return p, nil
}

func (p Pipeline) DisplayJSON() bool { return p.displayJSON }
func (p Pipeline) StageCount() int   { return len(p.stages) }

func Lines(text string) []string {
	if text == "" {
		return []string{}
	}
	return strings.Split(strings.TrimSuffix(text, "\n"), "\n")
}

// JSON encodes already-sanitized presentation text in the portable result envelope.
func JSON(text string, ok bool) (string, error) {
	data, err := json.Marshal(struct {
		OK    bool     `json:"ok"`
		Lines []string `json:"lines"`
	}{ok, Lines(text)})
	return string(data) + "\n", err
}

func (p Pipeline) Apply(text string) (string, error) {
	lines := Lines(text)
	for _, s := range p.stages {
		switch s.op {
		case "count":
			lines = []string{fmt.Sprintf("Count: %d lines", len(lines))}
		case "find":
			if first := slices.IndexFunc(lines, s.re.MatchString); first >= 0 {
				lines = lines[first:]
			} else {
				lines = nil
			}
		case "match", "except":
			kept := make([]string, 0, len(lines))
			for _, line := range lines {
				if s.re.MatchString(line) == (s.op == "match") {
					kept = append(kept, line)
				}
			}
			lines = kept
		case "last":
			if len(lines) > s.n {
				lines = lines[len(lines)-s.n:]
			}
		case "trim":
			for i, line := range lines {
				runes := []rune(line)
				lines[i] = string(runes[min(s.n-1, len(runes)):])
			}
		}
	}
	if len(lines) == 0 {
		return "", nil
	}
	return strings.Join(lines, "\n") + "\n", nil
}
