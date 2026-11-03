package shell

import (
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"dropcheck/controller/internal/command"
)

func PrintHelp() { writeHelp(os.Stdout) }
func writeHelp(w io.Writer) {
	fmt.Fprintln(w, "Controller Shell (pinned Android target; PC-only ADB/device commands are labelled)")
	for _, entry := range command.ShellGrammarTable {
		if entry.Unsupported {
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
		if entry.Effect == "" {
			fmt.Fprint(w, "  (PC-only)")
		} else {
			fmt.Fprint(w, "  ", entry.Description)
		}
		fmt.Fprintln(w)
	}
	fmt.Fprintln(w, "  exit | quit\n  | begin <pattern> | match <pattern> | except <pattern> | count\n  | display json (alone; safe presentation JSON)")
	fmt.Fprintln(w, "  Defaults and command history are session-only; secret input is never retained in history.")
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
	for _, candidate := range CompleteLine(prefix) {
		lexed, _ := command.Lex(candidate, true)
		if len(lexed.Tokens) > 0 {
			fmt.Fprintln(w, "  ", lexed.Tokens[len(lexed.Tokens)-1].Value)
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
			choices = []string{"begin", "match", "except", "count", "display"}
		case len(args) == 1 && args[0] == "display":
			choices = []string{"json"}
		case len(args) == 1 && (args[0] == "begin" || args[0] == "match" || args[0] == "except"):
			choices = []string{"<pattern>"}
		}
	} else {
		choices = command.ShellSuggestions(args)
		if len(args) == 0 {
			choices = append(choices, "exit", "quit")
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

// Only successfully parsed, read-only input is eligible for recall. URL/query
// and caller-supplied literals in probes may also contain credentials.
func HistorySafe(line string) bool {
	parsed, err := ParseLine(line)
	if err != nil {
		return false
	}
	if parsed.Pipeline.DisplayJSON() || parsed.Pipeline.StageCount() > 0 {
		return false
	}
	if parsed.Kind == Version || parsed.Kind == Profiles || parsed.Kind == LastReport || parsed.Kind == Help || parsed.Kind == ShowDevices {
		return true
	}
	if parsed.Kind != AgentCommand || parsed.Operation.Command == nil {
		return false
	}
	return strings.HasPrefix(parsed.Operation.Name, "wifi.status") || parsed.Operation.Name == "wifi.capabilities" || parsed.Operation.Name == "network.list" || parsed.Operation.Name == "permissions" || parsed.Operation.Name == "ip.status"
}
