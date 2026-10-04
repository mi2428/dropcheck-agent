package shell

import (
	"fmt"
	"io"
	"os"
	"strings"

	"dropcheck/controller/internal/command"
	"dropcheck/controller/internal/pipeline"
)

func PrintHelp() { writeHelp(os.Stdout) }

func writeHelp(w io.Writer) {
	fmt.Fprintln(w, "Controller Shell commands (key value; switches fresh/brief/mlo/detail):")
	for _, entry := range command.GrammarTable {
		if entry.Unsupported {
			continue
		}
		fmt.Fprint(w, "  ", entry.Path)
		for _, pos := range entry.Positionals {
			fmt.Fprint(w, " <", pos, ">")
		}
		for _, key := range entry.Required {
			fmt.Fprint(w, " ", key, " <value>")
		}
		for _, key := range entry.Switches {
			fmt.Fprint(w, " [", key, "]")
		}
		for _, key := range entry.Values {
			if !contains(entry.Required, key) {
				fmt.Fprint(w, " [", key, " <value>]")
			}
		}
		fmt.Fprintln(w)
	}
	fmt.Fprintln(w, "  exit | quit\n  | display json|set | match <regex> | except <regex> | count | no-more")
}

func contains(list []string, word string) bool {
	for _, item := range list {
		if item == word {
			return true
		}
	}
	return false
}

func IsHelpRune(value rune) bool { return value == '?' || value == '？' }

func IsHelpLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "?" || trimmed == "？" {
		return true
	}
	parts, err := pipeline.Split(trimmed)
	if err != nil {
		return false
	}
	args, err := command.SplitArgs(parts[len(parts)-1])
	return err == nil && len(args) > 0 && (args[len(args)-1] == "?" || args[len(args)-1] == "？")
}

func PrintContextHelp(line string) { WriteContextHelp(os.Stdout, line) }

func WriteContextHelp(w io.Writer, line string) {
	if strings.TrimSpace(line) == "?" || strings.TrimSpace(line) == "？" {
		writeHelp(w)
		return
	}
	line = strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(strings.TrimSpace(line), "?"), "？"))
	if parts, err := pipeline.Split(line + "x"); err == nil && len(parts) > 1 {
		for _, candidate := range []string{"display json", "display set", "match <regex>", "except <regex>", "count", "no-more"} {
			fmt.Fprintln(w, "  ", candidate)
		}
		return
	}
	args, err := command.SplitArgs(line)
	if err != nil {
		writeHelp(w)
		return
	}
	if len(args) == 0 {
		writeHelp(w)
		return
	}
	for _, candidate := range command.Suggestions(args) {
		fmt.Fprintln(w, "  ", candidate)
	}
	if len(command.Suggestions(args)) == 0 {
		writeHelp(w)
	}
}

func CompleteLine(line string) []string {
	if parts, err := pipeline.Split(line + "x"); err != nil {
		return nil
	} else if len(parts) > 1 {
		segment := strings.TrimSuffix(parts[len(parts)-1], "x")
		args, err := command.SplitArgs(segment)
		if err != nil {
			return nil
		}
		prefix := ""
		base := args
		if !strings.HasSuffix(line, " ") && len(args) > 0 {
			prefix = args[len(args)-1]
			base = args[:len(args)-1]
		}
		var choices []string
		switch {
		case len(base) == 0:
			choices = []string{"display", "match", "except", "count", "no-more"}
		case len(base) == 1 && base[0] == "display":
			choices = []string{"json", "set"}
		}
		var out []string
		for _, choice := range choices {
			if strings.HasPrefix(choice, prefix) {
				out = append(out, line[:len(line)-len(prefix)]+choice)
			}
		}
		return out
	}
	args, err := command.SplitArgs(line)
	if err != nil {
		return nil
	}
	base := args
	prefix := ""
	if len(line) > 0 && !strings.HasSuffix(line, " ") && len(args) > 0 {
		prefix = args[len(args)-1]
		base = args[:len(args)-1]
	}
	var candidates []string
	if len(base) == 0 {
		candidates = command.Suggestions(nil)
	} else {
		candidates = command.Suggestions(base)
	}
	var out []string
	for _, candidate := range candidates {
		if strings.HasPrefix(candidate, prefix) {
			completion := line[:len(line)-len(prefix)] + candidate
			if prefix == candidate && !IsPlaceholderCandidate(candidate) && len(command.Suggestions(append(append([]string(nil), base...), candidate))) > 0 {
				completion += " "
			}
			out = append(out, completion)
		}
	}
	return out
}

func CompletionHintLine(line string) string {
	var hints []string
	for _, candidate := range CompleteLine(line) {
		if strings.Contains(candidate, "<") {
			hints = append(hints, strings.TrimPrefix(candidate, line))
		}
	}
	return strings.Join(hints, "  ")
}

func IsPlaceholderCandidate(value string) bool {
	return strings.HasPrefix(value, "<") && strings.HasSuffix(value, ">")
}
