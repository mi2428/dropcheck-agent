package shell

import (
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/mi2428/dropcheck-agent/controller/internal/command"
)

func writeHelp(w io.Writer) {
	WriteHelpTopic(w, "")
}

func WriteHelpTopic(w io.Writer, topic string) {
	fmt.Fprintln(w, "Controller Shell (pinned Android target; PC-only ADB/device commands are labelled)")
	fmt.Fprintln(w)
	for _, entry := range command.ShellGrammarTable {
		if topic != "" && entry.Path != topic && !strings.HasPrefix(entry.Path, topic+" ") {
			continue
		}
		fmt.Fprint(w, "  ", entry.Path)
		for _, pos := range entry.Positionals {
			fmt.Fprint(w, " <", pos, ">")
		}
		for _, key := range entry.Switches {
			fmt.Fprint(w, " [", key, "]")
		}
		for _, key := range entry.Values {
			if slices.Contains(entry.Required, key) {
				fmt.Fprint(w, " ", key, " <value>")
			} else {
				fmt.Fprint(w, " [", key, " <value>]")
			}
		}
		description := entry.Description
		if entry.Effect == "" {
			description = "(PC-only) " + description
		}
		fmt.Fprintf(w, "\n    Description: %s\n\n", description)
	}
	fmt.Fprintln(w, "  exit\n  | find <pattern> | except <pattern> | last <lines> | match <pattern> | trim <start-column> | count\n  | display json (alone; safe presentation JSON)")
	fmt.Fprintln(w, "  All command input, including credentials, is recallable within this session; history is not written to disk.")
}

func IsHelpRune(r rune) bool { return r == '?' || r == '？' }
func IsHelpLine(line string) bool {
	line = strings.TrimRight(line, " \t\r\n")
	runes := []rune(line)
	return len(runes) > 0 && IsHelpRune(runes[len(runes)-1]) && command.HelpPosition(string(runes[:len(runes)-1]))
}
func PrintContextHelp(line string) { WriteContextHelp(os.Stdout, line) }
func WriteContextHelp(w io.Writer, line string) {
	runes := []rune(strings.TrimRight(line, "\r\n"))
	if len(runes) > 0 && IsHelpRune(runes[len(runes)-1]) {
		runes = runes[:len(runes)-1]
	}
	prefix := string(runes)
	if strings.TrimSpace(prefix) == "" {
		writeHelp(w)
		return
	}
	var rows [][2]string
	if parsed, err := ParseLine(prefix); err == nil && parsed.Kind != Help && parsed.Kind != Noop && parsed.Kind != Exit && parsed.Rejection == "" && strings.HasSuffix(prefix, " ") {
		rows = append(rows, [2]string{"<Enter>", "Execute the requested subject"})
	}
	for _, candidate := range CompleteLine(prefix) {
		lexed, _ := command.Lex(candidate, true)
		if len(lexed.Tokens) > 0 {
			description := ""
			for _, entry := range command.ShellGrammarTable {
				if entry.Path == strings.TrimSpace(candidate) {
					description = entry.Description
					break
				}
			}
			rows = append(rows, [2]string{lexed.Tokens[len(lexed.Tokens)-1].Value, description})
		}
	}
	width := 0
	for _, row := range rows {
		width = max(width, len(row[0]))
	}
	for _, row := range rows {
		if row[1] == "" {
			fmt.Fprintln(w, "  "+row[0])
		} else {
			fmt.Fprintf(w, "  %-*s  - %s\n", width, row[0], row[1])
		}
	}
}

func CompleteLine(line string) []string {
	lexed, _ := command.Lex(line, true)
	if lexed.Quote != 0 || lexed.Escaped {
		return nil
	}
	tokens := lexed.Tokens
	pipe := false
	for i, token := range slices.Backward(tokens) {
		if token.Pipe {
			tokens = tokens[i+1:]
			pipe = true
			break
		}
	}
	var args []string
	start := len(line)
	prefix := ""
	for _, t := range tokens {
		args = append(args, t.Value)
	}
	if len(tokens) > 0 && tokens[len(tokens)-1].End == len(line) {
		last := tokens[len(tokens)-1]
		if last.Literal {
			return nil
		}
		prefix = last.Value
		start = last.Start
		args = args[:len(args)-1]
	}
	var choices []string
	if pipe {
		switch {
		case len(args) == 0:
			choices = []string{"find", "except", "last", "match", "trim", "count", "display"}
		case len(args) == 1 && args[0] == "display":
			choices = []string{"json"}
		case len(args) == 1 && (args[0] == "find" || args[0] == "match" || args[0] == "except"):
			choices = []string{"<pattern>"}
		case len(args) == 1 && args[0] == "last":
			choices = []string{"<lines>"}
		case len(args) == 1 && args[0] == "trim":
			choices = []string{"<start-column>"}
		}
	} else {
		choices = command.ShellSuggestions(args)
		if len(args) == 0 {
			choices = append(choices, "exit")
		}
	}
	var out []string
	for _, choice := range choices {
		if strings.HasPrefix(choice, prefix) || IsPlaceholderCandidate(choice) {
			completion := line[:start] + choice
			if prefix == choice && !IsPlaceholderCandidate(choice) {
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
		lexed, _ := command.Lex(candidate, true)
		if len(lexed.Tokens) > 0 && IsPlaceholderCandidate(lexed.Tokens[len(lexed.Tokens)-1].Value) {
			hints = append(hints, lexed.Tokens[len(lexed.Tokens)-1].Value)
		}
	}
	return strings.Join(hints, "  ")
}
func IsPlaceholderCandidate(value string) bool {
	return strings.HasPrefix(value, "<") && strings.HasSuffix(value, ">")
}
