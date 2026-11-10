// Package e2e checks the shared command table without a device. Real execution
// remains in the e2e-tagged suite and requires explicit live opt-in.
package e2e

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/mi2428/dropcheck-agent/controller/internal/command"
	"github.com/mi2428/dropcheck-agent/controller/internal/controlpb"
	"github.com/mi2428/dropcheck-agent/controller/internal/pipeline"
	"github.com/mi2428/dropcheck-agent/controller/internal/render"
	"github.com/mi2428/dropcheck-agent/controller/internal/shell"
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

// Keep live-result validation in the device-free suite so ordinary CI exercises it.
func processResultError(expect string, res commandResult) error {
	jsonOK := true
	if slices.ContainsFunc(strings.Split(res.Output, "\n"), func(line string) bool { return strings.HasPrefix(strings.TrimSpace(line), "{") }) {
		var err error
		jsonOK, res.Output, err = shellEnvelopeOutput(res.Output)
		if err != nil {
			return err
		}
	}
	lower := strings.ToLower(res.Output)
	if strings.Contains(lower, "panic:") || strings.Contains(lower, "fatal error:") {
		return fmt.Errorf("process produced panic/fatal output")
	}
	success := jsonOK && res.Err == nil && res.Code == 0 && !isFailureStatusOutput(res.Output) && !strings.Contains(lower, "error parsing regexp")
	clearErrors := true
	for line := range strings.SplitSeq(res.Output, "\n") {
		line = strings.TrimSpace(line)
		if message, ok := strings.CutPrefix(line, "Error:"); ok && strings.TrimSpace(message) != "" {
			success = false
			clearErrors = clearErrors && isClearRuntimeFailure(message)
		}
	}
	switch expect {
	case "help":
		if !success || !strings.Contains(res.Output, "commands:") && !strings.Contains(res.Output, "<cr>") {
			return fmt.Errorf("expected help output")
		}
	case "ok":
		if !success {
			return fmt.Errorf("expected success")
		}
	case "error":
		if !isShellErrorOutput(res.Output) {
			return fmt.Errorf("expected shell error output")
		}
	case "ok_or_clear":
		if !clearErrors {
			return fmt.Errorf("unexpected shell error")
		}
		if !success && !isClearRuntimeFailure(res.Output) {
			return fmt.Errorf("expected success or clear runtime failure")
		}
	case "advisory":
	default:
		return fmt.Errorf("unknown expectation %q", expect)
	}
	return nil
}

func isShellErrorOutput(output string) bool {
	if isFailureStatusOutput(output) || strings.Contains(strings.ToLower(output), "error parsing regexp") {
		return true
	}
	for line := range strings.SplitSeq(output, "\n") {
		if message, ok := strings.CutPrefix(strings.TrimSpace(line), "Error:"); ok && strings.TrimSpace(message) != "" {
			return true
		}
	}
	return false
}

func isClearRuntimeFailure(output string) bool {
	lower := strings.ToLower(output)
	return strings.Contains(lower, "wait for android agents") ||
		strings.Contains(lower, "agent disconnected") ||
		strings.Contains(lower, "network not available") ||
		strings.Contains(lower, "did not match requested") ||
		strings.Contains(lower, "process_timeout=") ||
		strings.Contains(lower, "dns_timeout=") ||
		strings.Contains(lower, "scan_broadcast_timeout=") ||
		strings.Contains(lower, "wifi condition timeout") ||
		strings.Contains(lower, "probe deadline exceeded") ||
		strings.Contains(lower, "context deadline exceeded") ||
		strings.Contains(lower, "timeoutexception") ||
		strings.Contains(lower, "i/o timeout") ||
		strings.Contains(lower, "timed out") ||
		strings.Contains(lower, "no_dns_address_for_family") ||
		strings.Contains(lower, "dns resolution failed") ||
		strings.Contains(lower, "one_or_more_families_failed") ||
		strings.Contains(lower, "not a device owner, profile owner, system app, or privileged app") ||
		strings.Contains(lower, "profile unsupported") ||
		strings.Contains(lower, "pinning is unsupported") ||
		strings.Contains(lower, "connectivity completed with failed checks")
}

func isFailureStatusOutput(output string) bool {
	return failureStatus.MatchString(output)
}

var failureStatus = regexp.MustCompile(`(?i)\bstatus(?:_(?:failed|canceled)\b|[:=]\s*(?:failed|canceled)\b)`)

func shellEnvelopeOutput(output string) (bool, string, error) {
	count := 0
	ok := false
	var lines []string
	for line := range strings.SplitSeq(output, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "{") {
			lines = append(lines, line)
			continue
		}
		var envelope struct {
			OK    *bool    `json:"ok"`
			Lines []string `json:"lines"`
		}
		if err := json.Unmarshal([]byte(line), &envelope); err != nil || envelope.OK == nil {
			return false, "", fmt.Errorf("invalid Shell JSON envelope")
		}
		ok = *envelope.OK
		lines = append(lines, envelope.Lines...)
		count++
	}
	if count != 1 {
		return false, "", fmt.Errorf("expected exactly one Shell JSON envelope")
	}
	return ok, strings.Join(lines, "\n"), nil
}

func TestLiveResultRejectsUnexpectedShellErrorsWithoutADevice(t *testing.T) {
	for _, expect := range []string{"ok", "ok_or_clear"} {
		for _, output := range []string{"Error: deliberately invalid request", "Error: unsupported command", "Error: timeout is required\ntimeout_ms=5000", "Error: no matching command", "Error: invalid request\nError: network not available", "Status: failed  Message: wifi ssid is required", "Status: canceled", "match regex: error parsing regexp: missing closing ]"} {
			if err := processResultError(expect, commandResult{Output: output}); err == nil {
				t.Fatalf("%s accepted %q", expect, output)
			}
		}
		if err := processResultError(expect, commandResult{Output: "Status: ok\ncapability: unknown (unsupported feature)"}); err != nil {
			t.Fatalf("ordinary capability data mistaken for an error: %v", err)
		}
	}
	for _, output := range []string{"Error: network not available", "Status: failed  Message: dns resolution failed", "Error: agent disconnected", "Status: failed  Message: timed out"} {
		if err := processResultError("ok_or_clear", commandResult{Output: output, Code: 1}); err != nil {
			t.Fatalf("clear runtime failure rejected: %s: %v", output, err)
		}
	}
	if processResultError("ok_or_clear", commandResult{Code: 1}) == nil || processResultError("advisory", commandResult{Output: "panic: timeout"}) == nil {
		t.Fatal("unknown process failure or panic accepted")
	}
}

func TestLiveResultAcceptsRenderedEmptyErrorFieldsAndHonorsJSONVerdicts(t *testing.T) {
	text, err := render.CommandResult("qa", &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_OK,
		Payload: &controlpb.CommandResult_HttpCheck{HttpCheck: &controlpb.HttpCheckResult{Url: "https://example.test", Status: 200, ExpectedStatus: 200, Matched: true}}}, command.Options{})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := pipeline.JSON(text, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, output := range []string{text, encoded, "capability: unknown (unsupported feature)"} {
		if processResultError("error", commandResult{Output: output}) == nil {
			t.Fatalf("valid output mistaken for expected error: %s", output)
		}
	}
	for _, expect := range []string{"ok", "ok_or_clear"} {
		for _, output := range []string{text, encoded} {
			if err := processResultError(expect, commandResult{Output: output}); err != nil {
				t.Fatalf("valid rendered result rejected: %v\n%s", err, output)
			}
		}
		for _, output := range []string{`{"ok":false,"lines":[]}`, `{"ok":false,"lines":["Error: deliberately invalid request"]}`, `{"ok":false,"lines":["Error: invalid request","Error: network not available"]}`} {
			if processResultError(expect, commandResult{Output: output}) == nil {
				t.Fatalf("negative JSON verdict accepted: %s", output)
			}
		}
		for _, status := range []controlpb.CommandResult_Status{controlpb.CommandResult_STATUS_FAILED, controlpb.CommandResult_STATUS_CANCELED} {
			output, err := render.CommandResult("qa", &controlpb.CommandResult{Status: status, Payload: &controlpb.CommandResult_Ping{Ping: &controlpb.PingResult{ExitCode: 1}}}, command.Options{})
			if err != nil {
				t.Fatal(err)
			}
			if processResultError(expect, commandResult{Output: output}) == nil {
				t.Fatalf("rendered failure accepted: %s", output)
			}
		}
	}
	if processResultError("ok_or_clear", commandResult{Output: `{"ok":false,"lines":["Error: network not available"]}`}) != nil {
		t.Fatal("clear JSON runtime failure rejected")
	}
}

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
	if len(cases) == 0 {
		return fmt.Errorf("no e2e cases loaded")
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
		if tc.Runner != "shell" && tc.Runner != "shell-parser" {
			return fmt.Errorf("%s has unknown runner %q", tc.ID, tc.Runner)
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
	}
	return nil
}

func TestE2ECaseTableParsesShellExpectations(t *testing.T) {
	for _, tc := range loadCases(t) {
		if tc.Runner != "shell" {
			continue
		}
		t.Run(tc.testName(), func(t *testing.T) {
			commandLine := expandParserPlaceholders(tc.Command)
			res := runShellParser(commandLine)
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
		{name: "shell ip status", runner: "shell", text: "show wifi status ip"},
		{name: "shell wifi diagnostics", runner: "shell", text: "show wifi status"},
		{name: "shell wifi eht", runner: "shell", text: "show wifi status eht"},
		{name: "shell wifi eht fresh", runner: "shell", text: "request wifi scan eht"},
		{name: "shell wifi capabilities", runner: "shell", text: "show wifi status capabilities"},
		{name: "shell wifi scan", runner: "shell", text: "show wifi scan"},
		{name: "shell wifi fresh scan", runner: "shell", text: "request wifi scan"},
		{name: "shell wifi scan filter", runner: "shell", text: "show wifi scan ssid"},
		{name: "shell wifi connect", runner: "shell", text: "request wifi connect"},
		{name: "shell wifi evaluation", runner: "shell", text: "test wifi"},
		{name: "shell wifi reconnect", runner: "shell", text: "request wifi reconnect"},
		{name: "shell wifi monitor", runner: "shell", text: "monitor wifi"},
		{name: "shell wifi cycle", runner: "shell", text: "request wifi cycle"},
		{name: "shell wifi disconnect", runner: "shell", text: "request wifi disconnect"},
		{name: "shell wifi forget", runner: "shell", text: "request wifi forget ssid"},
		{name: "shell ping", runner: "shell", text: "request ping"},
		{name: "shell traceroute", runner: "shell", text: "request traceroute"},
		{name: "shell path mtu", runner: "shell", text: "request pmtu"},
		{name: "shell external ip", runner: "shell", text: "request ip external"},
		{name: "shell dns", runner: "shell", text: "request dns resolve"},
		{name: "shell http", runner: "shell", text: "request http check"},
		{name: "shell download", runner: "shell", text: "request http download"},
	}
	for _, want := range required {
		if !e2eTableHasCommand(cases, want.runner, want.text) {
			return fmt.Errorf("missing E2E coverage for %s: runner=%s command contains %q", want.name, want.runner, want.text)
		}
	}
	for _, tc := range cases {
		commandLine := e2eComparableCommand(tc.Command)
		if strings.Contains(commandLine, "request>") || strings.Contains(commandLine, "config>") || strings.Contains(commandLine, "dropcheck request ") || strings.Contains(commandLine, "run request ") || strings.Contains(commandLine, "run show ") || strings.Contains(commandLine, "--probe-option") {
			return fmt.Errorf("%s still references removed command grammar", tc.ID)
		}
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
		"<psk>", "00000000",
		"<bssid>", "00:11:22:33:44:55",
	).Replace(commandLine)
}

func runShellParser(commandLine string) commandResult {
	if shell.IsHelpLine(commandLine) {
		var out bytes.Buffer
		shell.WriteContextHelp(&out, commandLine)
		if strings.TrimSpace(out.String()) == "" {
			return commandResult{Output: "help output: <empty>", Code: 1, Err: errors.New("empty help output")}
		}
		return commandResult{Output: out.String(), Code: 0}
	}
	parsed, err := shell.ParseLine(commandLine)
	if err == nil && parsed.Rejection != "" {
		err = errors.New(parsed.Rejection)
	}
	if err != nil {
		return commandResult{Output: err.Error(), Code: 1, Err: err}
	}
	return commandResult{Output: "parse ok\n", Code: 0}
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
		{"unsupported runner", func(tc *matrixCase) { tc.Runner = "unknown" }},
		{"unsupported expectation", func(tc *matrixCase) { tc.Expect = "unknown" }},
		{"empty command", func(tc *matrixCase) { tc.Command = "" }},
		{"empty assertion", func(tc *matrixCase) { tc.Assertion = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			invalid := slices.Clone(cases)
			tc.mutate(&invalid[1])
			if err := validateCases(invalid); err == nil {
				t.Fatal("invalid table passed")
			}
		})
	}
	if err := validateCases(nil); err == nil {
		t.Fatal("empty table passed")
	}
	if err := validateCases([]matrixCase{{ID: "E2E-001", Title: "Help remains available", Runner: "shell", Command: "help", Expect: "help", Assertion: "Help output"}}); err != nil {
		t.Fatalf("valid independent case rejected: %v", err)
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
