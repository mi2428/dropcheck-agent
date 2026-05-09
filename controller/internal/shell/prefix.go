package shell

import (
	"fmt"
	"strings"

	"dropcheck/controller/internal/command"
	"dropcheck/controller/internal/pipeline"
)

func resolveUniquePrefix(kind string, value string, candidates []string) (string, error) {
	if value == "" {
		return "", fmt.Errorf("empty %s", kind)
	}
	for _, candidate := range candidates {
		if value == candidate {
			return candidate, nil
		}
	}
	var matches []string
	for _, candidate := range candidates {
		if strings.HasPrefix(candidate, value) {
			matches = append(matches, candidate)
		}
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("unknown %s %q", kind, value)
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("ambiguous %s %q (matches %s)", kind, value, strings.Join(matches, ", "))
	}
}

func splitArgs(line string) ([]string, error) {
	return command.SplitArgs(line)
}

func splitPipeline(line string) ([]string, error) {
	return pipeline.Split(line)
}

func normalizeIPFamily(value string) (string, error) {
	return command.NormalizeIPFamily(value)
}

func normalizeDNSQType(value string) (string, error) {
	return command.NormalizeDNSQType(value)
}
