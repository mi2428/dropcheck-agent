// Package e2e checks the shared command table without a device. Real execution
// remains in the e2e-tagged suite and requires explicit live opt-in.
package e2e

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	commandparse "dropcheck/controller/internal/command"
	"dropcheck/controller/internal/linuxcli"
	"dropcheck/controller/internal/shell"
)

type matrixCase struct {
	ID        string
	Title     string
	Runner    string
	Command   string
	Expect    string
	Assertion string
}

type commandResult struct {
	Output string
	Code   int
	Err    error
}

const e2eCaseCount = 282

var e2eCaseID = regexp.MustCompile(`^E2E-[0-9]{3}$`)

func loadCases(t *testing.T) []matrixCase {
	t.Helper()
	file, err := os.Open("testdata/e2e_cases.tsv")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	cases, err := readCases(file)
	if err != nil {
		t.Fatal(err)
	}
	return cases
}

func readCases(input io.Reader) ([]matrixCase, error) {
	reader := csv.NewReader(input)
	reader.Comma = '\t'
	reader.FieldsPerRecord = -1
	reader.LazyQuotes = true
	rows, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(rows) < 2 {
		return nil, fmt.Errorf("no e2e cases loaded")
	}
	if !slices.Equal(rows[0], []string{"id", "title", "runner", "command", "expect", "assertion"}) {
		return nil, fmt.Errorf("invalid e2e case header: %v", rows[0])
	}
	var cases []matrixCase
	seen := map[string]bool{}
	for i, row := range rows[1:] {
		if len(row) != 6 {
			return nil, fmt.Errorf("e2e case row %d has %d fields, want 6: %#v", i+2, len(row), row)
		}
		tc := matrixCase{
			ID: row[0], Title: row[1], Runner: row[2], Command: row[3], Expect: row[4], Assertion: row[5],
		}
		if seen[tc.ID] {
			return nil, fmt.Errorf("duplicate e2e case ID %q", tc.ID)
		}
		seen[tc.ID] = true
		cases = append(cases, tc)
	}
	return cases, nil
}

func TestE2ECaseTableSchema(t *testing.T) {
	if err := validateCases(loadCases(t)); err != nil {
		t.Fatal(err)
	}
}

func validateCases(cases []matrixCase) error {
	if len(cases) != e2eCaseCount {
		return fmt.Errorf("case count = %d, want %d", len(cases), e2eCaseCount)
	}
	titles := map[string]string{}
	previousID := ""
	for index, tc := range cases {
		// Keep surviving IDs stable when obsolete cases are removed.
		if !e2eCaseID.MatchString(tc.ID) || tc.ID <= previousID {
			return fmt.Errorf("case row %d has invalid or unordered ID %q after %q", index+2, tc.ID, previousID)
		}
		previousID = tc.ID
		if strings.TrimSpace(tc.Title) == "" {
			return fmt.Errorf("%s has an empty test title", tc.ID)
		}
		if !titleMatchesRunner(tc) {
			return fmt.Errorf("%s title %q does not match runner %q", tc.ID, tc.Title, tc.Runner)
		}
		if previousID, ok := titles[tc.Title]; ok {
			return fmt.Errorf("%s duplicates title %q from %s", tc.ID, tc.Title, previousID)
		}
		titles[tc.Title] = tc.ID
		if strings.TrimSpace(tc.Command) == "" || strings.TrimSpace(tc.Assertion) == "" {
			return fmt.Errorf("%s has an empty command or assertion", tc.ID)
		}
		switch tc.Expect {
		case "help", "ok", "error", "ok_or_clear", "advisory":
		default:
			return fmt.Errorf("%s has unknown expectation %q", tc.ID, tc.Expect)
		}
		if containsStaleCaseLanguage(tc) {
			return fmt.Errorf("%s contains stale case-management language", tc.ID)
		}
	}
	return nil
}

func titleMatchesRunner(tc matrixCase) bool {
	switch tc.Runner {
	case "shell":
		return strings.HasPrefix(tc.Title, "Shell ")
	case "shell-parser":
		return strings.HasPrefix(tc.Title, "Parser ")
	case "cli":
		return strings.HasPrefix(tc.Title, "CLI ")
	default:
		return false
	}
}

func containsStaleCaseLanguage(tc matrixCase) bool {
	text := strings.ToLower(strings.Join([]string{tc.ID, tc.Title, tc.Runner, tc.Command, tc.Expect, tc.Assertion}, "\n"))
	staleTerms := []string{
		"test2", "mer" + "ged", "manual" + " matrix", "resolved" + " anom" + "aly",
		"regression" + ":", "cur" + "rently", "decide" + " whether", "documented" + " as", "last" + "-wins",
	}
	for _, term := range staleTerms {
		if strings.Contains(text, term) {
			return true
		}
	}
	return false
}

func TestE2ECaseTableParsesShellAndCLIExpectations(t *testing.T) {
	for _, tc := range loadCases(t) {
		if tc.Runner != "shell" && tc.Runner != "cli" {
			continue
		}
		t.Run(tc.testName(), func(t *testing.T) {
			commandLine := expandParserPlaceholders(tc.Command)
			var res commandResult
			switch tc.Runner {
			case "shell":
				res = runShellParser(commandLine)
			case "cli":
				res = runCLIParser(commandLine)
			}
			if err := parserResultError(tc.Expect, res); err != nil {
				t.Fatalf("%s command %q: %v", tc.Runner, tc.Command, err)
			}
		})
	}
}

func TestE2EShellParserRows(t *testing.T) {
	for _, tc := range loadCases(t) {
		if tc.Runner != "shell-parser" {
			continue
		}
		t.Run(tc.testName(), func(t *testing.T) {
			if err := parserResultError(tc.Expect, runShellParser(expandParserPlaceholders(tc.Command))); err != nil {
				t.Fatalf("command %q: %v", tc.Command, err)
			}
		})
	}
}

func parserResultError(expect string, res commandResult) error {
	lower := strings.ToLower(res.Output)
	if strings.Contains(lower, "panic:") || strings.Contains(lower, "fatal error:") {
		return fmt.Errorf("parser produced panic/fatal output: %s", res.Output)
	}
	switch expect {
	case "help":
		if res.Err != nil || res.Code != 0 || strings.TrimSpace(res.Output) == "" {
			return fmt.Errorf("expected help entries, got code=%d err=%v output=%q", res.Code, res.Err, res.Output)
		}
	case "ok", "ok_or_clear", "advisory":
		if res.Err != nil || res.Code != 0 {
			return fmt.Errorf("expected parse ok, got code=%d err=%v", res.Code, res.Err)
		}
	case "error":
		if res.Err == nil || res.Code == 0 {
			return fmt.Errorf("expected parse error, got code=%d err=%v", res.Code, res.Err)
		}
	default:
		return fmt.Errorf("unknown expectation %q", expect)
	}
	return nil
}

func TestE2ECaseTableCoversControllerCommandSurface(t *testing.T) {
	if err := validateCommandCoverage(loadCases(t)); err != nil {
		t.Fatal(err)
	}
}

func validateCommandCoverage(cases []matrixCase) error {
	required := []struct {
		name   string
		runner string
		text   string
	}{
		{name: "shell help", runner: "shell", text: "help"},
		{name: "shell show devices", runner: "shell", text: "show devices"},
		{name: "shell pipeline", runner: "shell", text: "| match"},
		{name: "shell wifi status", runner: "shell", text: "show wifi status"},
		{name: "shell ip status", runner: "shell", text: "show ip status"},
		{name: "shell wifi diagnostics", runner: "shell", text: "show wifi diagnostics"},
		{name: "shell wifi eht", runner: "shell", text: "show wifi eht"},
		{name: "shell wifi eht fresh", runner: "shell", text: "show wifi eht fresh"},
		{name: "shell wifi capabilities", runner: "shell", text: "show wifi capabilities"},
		{name: "shell wifi scan", runner: "shell", text: "show wifi scan"},
		{name: "shell wifi fresh scan", runner: "shell", text: "show wifi scan fresh"},
		{name: "shell wifi scan detail", runner: "shell", text: "show wifi scan detail"},
		{name: "shell wifi connect", runner: "shell", text: "request> wifi connect"},
		{name: "shell wifi wait", runner: "shell", text: "request> wifi wait"},
		{name: "shell wifi assert", runner: "shell", text: "request> wifi assert"},
		{name: "shell wifi reconnect", runner: "shell", text: "request> wifi reconnect"},
		{name: "shell wifi monitor", runner: "shell", text: "request> monitor wifi"},
		{name: "shell wifi cycle", runner: "shell", text: "request> wifi cycle"},
		{name: "shell wifi disconnect", runner: "shell", text: "request> wifi disconnect"},
		{name: "shell wifi forget", runner: "shell", text: "request> wifi forget"},
		{name: "shell ping", runner: "shell", text: "request> ping"},
		{name: "shell traceroute", runner: "shell", text: "request> traceroute"},
		{name: "shell path mtu", runner: "shell", text: "request> path-mtu"},
		{name: "shell global ip", runner: "shell", text: "request> global-ip"},
		{name: "shell dns", runner: "shell", text: "request> dns"},
		{name: "shell http", runner: "shell", text: "request> http"},
		{name: "shell download", runner: "shell", text: "request> download"},
		{name: "cli show devices", runner: "cli", text: "dropcheck --serial"},
		{name: "cli ip status", runner: "cli", text: "dropcheck show ip status"},
		{name: "cli wifi eht", runner: "cli", text: "dropcheck show wifi eht"},
		{name: "cli wifi scan", runner: "cli", text: "dropcheck show wifi scan"},
		{name: "cli wifi connect", runner: "cli", text: "dropcheck request wifi connect"},
		{name: "cli wifi wait", runner: "cli", text: "dropcheck request wifi wait"},
		{name: "cli wifi assert", runner: "cli", text: "dropcheck request wifi assert"},
		{name: "cli wifi monitor", runner: "cli", text: "dropcheck request monitor wifi"},
		{name: "cli wifi reconnect", runner: "cli", text: "dropcheck request wifi reconnect"},
		{name: "cli wifi cycle", runner: "cli", text: "dropcheck request wifi cycle"},
		{name: "cli ping", runner: "cli", text: "dropcheck request ping"},
		{name: "cli traceroute", runner: "cli", text: "dropcheck request traceroute"},
		{name: "cli path mtu", runner: "cli", text: "dropcheck request path-mtu"},
		{name: "cli global ip", runner: "cli", text: "dropcheck request global-ip"},
		{name: "cli dns", runner: "cli", text: "dropcheck request dns"},
		{name: "cli http", runner: "cli", text: "dropcheck request http"},
		{name: "cli download", runner: "cli", text: "dropcheck request download"},
	}
	for _, want := range required {
		if !e2eTableHasCommand(cases, want.runner, want.text) {
			return fmt.Errorf("missing E2E coverage for %s: runner=%s command contains %q", want.name, want.runner, want.text)
		}
	}
	for _, tc := range cases {
		commandLine := e2eComparableCommand(tc.Command)
		if strings.Contains(commandLine, "wifi watch") || strings.Contains(commandLine, "watch wifi") {
			return fmt.Errorf("%s still references removed wifi watch command: %s", tc.ID, tc.Command)
		}
		if strings.Contains(commandLine, "standalone") || strings.Contains(commandLine, "show config") {
			return fmt.Errorf("%s still references removed standalone control: %s", tc.ID, tc.Command)
		}
	}
	return nil
}

func e2eTableHasCommand(cases []matrixCase, runner string, text string) bool {
	needle := e2eComparableCommand(text)
	for _, tc := range cases {
		if tc.Runner == runner && strings.Contains(e2eComparableCommand(tc.Command), needle) {
			return true
		}
	}
	return false
}

func e2eComparableCommand(value string) string {
	return strings.Join(strings.Fields(strings.ToLower(value)), " ")
}

func expandParserPlaceholders(commandLine string) string {
	return strings.NewReplacer(
		"<serial>", "SERIAL",
		"<ssid>", "Lab",
		"<psk>", "secret",
		"<bssid>", "00:11:22:33:44:55",
	).Replace(commandLine)
}

func runShellParser(commandLine string) commandResult {
	requestLine, requestMode := requestModeCommand(commandLine)
	configureLine, configureMode := configureModeCommand(commandLine)
	parseLine := commandLine
	if requestMode {
		parseLine = requestLine
	} else if configureMode {
		parseLine = configureLine
	}
	if shell.IsHelpLine(parseLine) {
		var out bytes.Buffer
		switch {
		case requestMode:
			shell.WriteRequestContextHelp(&out, parseLine)
		case configureMode:
			shell.WriteConfigureContextHelp(&out, parseLine)
		default:
			shell.WriteContextHelp(&out, parseLine)
		}
		if strings.TrimSpace(out.String()) == "" {
			return commandResult{Output: "help output: <empty>", Code: 1, Err: errors.New("empty help output")}
		}
		return commandResult{Output: out.String(), Code: 0}
	}
	var err error
	if requestMode {
		_, err = shell.ParseRequestLine(parseLine)
	} else if configureMode {
		_, err = shell.ParseConfigureLine(parseLine)
	} else {
		_, err = shell.ParseLine(parseLine)
	}
	if err != nil {
		return commandResult{Output: err.Error(), Code: 1, Err: err}
	}
	return commandResult{Output: "parse ok\n", Code: 0}
}

func requestModeCommand(commandLine string) (string, bool) {
	const marker = "request> "
	if after, ok := strings.CutPrefix(commandLine, marker); ok {
		return after, true
	}
	return commandLine, false
}

func configureModeCommand(commandLine string) (string, bool) {
	const marker = "config> "
	if after, ok := strings.CutPrefix(commandLine, marker); ok {
		return after, true
	}
	return commandLine, false
}

func runCLIParser(commandLine string) commandResult {
	args, err := commandparse.SplitArgs(commandLine)
	if err != nil {
		return commandResult{Output: err.Error(), Code: 1, Err: err}
	}
	if len(args) > 0 && args[0] == "dropcheck" {
		args = args[1:]
	}
	args, err = stripAppFlags(args)
	if err != nil {
		return commandResult{Output: err.Error(), Code: 1, Err: err}
	}
	_, args, err = linuxcli.ExtractOptions(args)
	if err != nil {
		return commandResult{Output: err.Error(), Code: 1, Err: err}
	}
	if _, err := linuxcli.Parse(args); err != nil {
		return commandResult{Output: err.Error(), Code: 1, Err: err}
	}
	return commandResult{Output: "parse ok\n", Code: 0}
}

func stripAppFlags(args []string) ([]string, error) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			return append([]string(nil), args[i+1:]...), nil
		}
		if !strings.HasPrefix(arg, "-") {
			return append([]string(nil), args[i:]...), nil
		}
		name, _, hasValue := strings.Cut(arg, "=")
		switch name {
		case "--adb", "-adb", "--serial", "-serial", "--package", "-package", "--listen":
			if !hasValue {
				if i+1 >= len(args) {
					return nil, fmt.Errorf("%s requires a value", name)
				}
				i++
			}
		default:
			return append([]string(nil), args[i:]...), nil
		}
	}
	return nil, nil
}

func (tc matrixCase) testName() string {
	slug := slugForTestName(tc.Title)
	if slug == "" {
		return tc.ID
	}
	return tc.ID + "_" + slug
}

func slugForTestName(value string) string {
	value = strings.ToLower(value)
	var b strings.Builder
	lastUnderscore := false
	for _, r := range value {
		isWord := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if isWord {
			b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore && b.Len() > 0 {
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	slug := strings.Trim(b.String(), "_")
	if len(slug) > 72 {
		slug = strings.TrimRight(slug[:72], "_")
	}
	return slug
}

func TestE2ECaseTableRejectsInvalidRows(t *testing.T) {
	cases := loadCases(t)
	for _, tc := range []struct {
		name   string
		mutate func(*matrixCase)
	}{
		{"invalid ID", func(tc *matrixCase) { tc.ID = "invalid" }},
		{"duplicate ID", func(tc *matrixCase) { tc.ID = cases[0].ID }},
		{"unordered ID", func(tc *matrixCase) { tc.ID = "E2E-000" }},
		{"empty title", func(tc *matrixCase) { tc.Title = "" }},
		{"duplicate title", func(tc *matrixCase) { tc.Title = cases[0].Title }},
		{"title runner mismatch", func(tc *matrixCase) { tc.Title = "Parser wrong surface" }},
		{"unsupported runner", func(tc *matrixCase) { tc.Runner = "unknown" }},
		{"unsupported expectation", func(tc *matrixCase) { tc.Expect = "unknown" }},
		{"empty command", func(tc *matrixCase) { tc.Command = "" }},
		{"empty assertion", func(tc *matrixCase) { tc.Assertion = "" }},
		{"stale language", func(tc *matrixCase) { tc.Title = "Shell currently unavailable" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			invalid := slices.Clone(cases)
			tc.mutate(&invalid[1])
			if err := validateCases(invalid); err == nil {
				t.Fatal("invalid table passed")
			}
		})
	}
	if err := validateCases(cases[:len(cases)-1]); err == nil {
		t.Fatal("missing row passed")
	}
	if err := validateCommandCoverage(nil); err == nil || !strings.Contains(err.Error(), "missing E2E coverage") {
		t.Fatalf("missing coverage error = %v", err)
	}
	invalid := slices.Clone(cases)
	invalid[1].Command = "show devices show standalone status"
	if err := validateCommandCoverage(invalid); err == nil || !strings.Contains(err.Error(), "removed standalone control") {
		t.Fatalf("removed command error = %v", err)
	}
}

func TestE2ECaseLoaderRejectsInvalidInput(t *testing.T) {
	const header = "id\ttitle\trunner\tcommand\texpect\tassertion\n"
	const row = "E2E-001\tShell help\tshell\thelp\thelp\tHelp output\n"
	for _, tc := range []struct {
		name  string
		input string
	}{
		{"empty", ""},
		{"header only", header},
		{"wrong header", "wrong\theader\n" + row},
		{"wrong field count", header + "E2E-001\tShell help\n"},
		{"duplicate ID", header + row + row},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := readCases(strings.NewReader(tc.input)); err == nil {
				t.Fatal("invalid TSV passed")
			}
		})
	}
}

func TestE2EParserRejectsExpectationMismatch(t *testing.T) {
	for _, tc := range []struct {
		expect string
		result commandResult
	}{
		{"error", commandResult{}},
		{"error", commandResult{Err: errors.New("inconsistent success")}},
		{"ok", commandResult{Err: errors.New("parse failed")}},
		{"ok_or_clear", commandResult{Err: errors.New("parse failed")}},
		{"help", commandResult{}},
		{"advisory", commandResult{Code: 1}},
		{"unknown", commandResult{}},
		{"ok", commandResult{Output: "panic: parser failed"}},
	} {
		if err := parserResultError(tc.expect, tc.result); err == nil {
			t.Fatalf("%s mismatch passed: %+v", tc.expect, tc.result)
		}
	}
}
