package app

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"

	"dropcheck/controller/internal/adb"
	"dropcheck/controller/internal/adbdiag"
	"dropcheck/controller/internal/command"
	"dropcheck/controller/internal/control"
	"dropcheck/controller/internal/controlpb"
	"dropcheck/controller/internal/harness"
	"dropcheck/controller/internal/runner"
	"dropcheck/controller/internal/version"
	"github.com/chzyer/readline"
	"google.golang.org/protobuf/proto"
)

func repl(ctx context.Context, state *shellState) error {
	fmt.Println("press '?' for Controller Shell context help, or type 'help' for commands")
	if useLineEditor() {
		return replLineEditor(ctx, state)
	}
	return replScanner(ctx, state)
}

func replLineEditor(ctx context.Context, state *shellState) error {
	var lineReader *readline.Instance
	cfg := &readline.Config{
		Prompt:                 state.prompt(),
		HistoryLimit:           1000,
		DisableAutoSaveHistory: true,
		AutoComplete:           shellReadlineCompleter{state: state},
		EOFPrompt:              "\n",
	}
	cfg.SetListener(func(line []rune, pos int, key rune) ([]rune, int, bool) {
		if lineReader == nil {
			return nil, 0, false
		}
		if newLine, newPos, ok := handleShellHelpKey(lineReader.Stdout(), line, pos, key, state); ok {
			return newLine, newPos, ok
		}
		return handleShellCompletionHintKey(lineReader.Stdout(), line, pos, key, state)
	})

	var err error
	lineReader, err = readline.NewEx(cfg)
	if err != nil {
		return err
	}
	defer func() {
		_ = lineReader.Close()
	}()
	for {
		lineReader.SetPrompt(state.prompt())
		line, err := lineReader.Readline()
		if err != nil {
			switch {
			case errors.Is(err, io.EOF):
				return nil
			case errors.Is(err, readline.ErrInterrupt):
				continue
			default:
				return err
			}
		}
		// Never retain credential-bearing input in readline history.
		done, err := runReplLine(ctx, state, line)
		if err != nil || done {
			return err
		}
	}
}

func replScanner(ctx context.Context, state *shellState) error {
	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print(state.prompt())
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return err
			}
			fmt.Println()
			return nil
		}
		done, err := runReplLine(ctx, state, scanner.Text())
		if err != nil || done {
			return err
		}
	}
}

type shellReadlineCompleter struct {
	state *shellState
}

func (c shellReadlineCompleter) Do(line []rune, pos int) ([][]rune, int) {
	if pos < 0 || pos > len(line) || pos != len(line) {
		return nil, 0
	}
	prefix := string(line[:pos])
	prefixRunes := []rune(prefix)
	var fullCandidates []string
	var completions [][]rune
	for _, candidate := range completeShellLine(prefix, c.state) {
		candidateRunes := []rune(candidate)
		if !hasRunePrefix(candidateRunes, prefixRunes) {
			continue
		}
		completion := append([]rune(nil), candidateRunes[len(prefixRunes):]...)
		if isPlaceholderCandidate(string(completion)) {
			continue
		}
		fullCandidates = append(fullCandidates, candidate)
		completions = append(completions, completion)
	}
	if len(completions) == 1 && len(completions[0]) > 0 && shouldAppendReadlineCompletionSpace(fullCandidates[0], c.state) {
		completions[0] = append(completions[0], ' ')
	}
	return completions, shellCompletionOffset(prefix)
}

func shouldAppendReadlineCompletionSpace(candidate string, state *shellState) bool {
	return slices.Contains(completeShellLine(candidate, state), candidate+" ")
}

func hasRunePrefix(value []rune, prefix []rune) bool {
	if len(value) < len(prefix) {
		return false
	}
	for i := range prefix {
		if value[i] != prefix[i] {
			return false
		}
	}
	return true
}

func shellCompletionOffset(line string) int {
	runes := []rune(line)
	offset := 0
	for i := range slices.Backward(runes) {
		switch runes[i] {
		case ' ', '|':
			return offset
		default:
			offset++
		}
	}
	return offset
}

func handleShellHelpKey(w io.Writer, line []rune, pos int, key rune, states ...*shellState) ([]rune, int, bool) {
	if !isShellHelpRune(key) || pos <= 0 || pos > len(line) {
		return nil, 0, false
	}
	questionIndex := pos - 1
	if !isShellHelpRune(line[questionIndex]) {
		return nil, 0, false
	}

	helpLine := append([]rune(nil), line[:pos]...)
	helpLine[len(helpLine)-1] = '?'
	var b strings.Builder
	b.WriteByte('\n')
	writeShellContextHelp(&b, string(helpLine), states...)
	_, _ = io.WriteString(w, b.String())

	newLine := append([]rune(nil), line[:questionIndex]...)
	newLine = append(newLine, line[pos:]...)
	return newLine, questionIndex, true
}

func handleShellCompletionHintKey(w io.Writer, line []rune, pos int, key rune, state *shellState) ([]rune, int, bool) {
	if key != readline.CharTab || pos < 0 || pos > len(line) || pos != len(line) {
		return nil, 0, false
	}
	hint := shellCompletionHintLine(string(line[:pos]), state)
	if hint == "" {
		return nil, 0, false
	}
	_, _ = io.WriteString(w, "\n  "+hint+"\n")
	newLine := append([]rune(nil), line...)
	return newLine, pos, true
}

func (s *shellState) prompt() string {
	label := "dropcheck"
	if s.targetAll {
		label = "all"
	} else if info, ok := s.selectedAgentIfConnected(); ok {
		s.selectedLabel = agentDisplayName(info)
		label = s.selectedLabel
	} else if s.selectedLabel != "" {
		label = s.selectedLabel
	}
	return fmt.Sprintf("%s# ", label)
}

func (s *shellState) selectedAgentIfConnected() (control.AgentInfo, bool) {
	if s.selected == "" || s.server == nil {
		return control.AgentInfo{}, false
	}
	info, err := selectedAgent(s)
	return info, err == nil
}

func runReplLine(ctx context.Context, state *shellState, rawLine string) (bool, error) {
	line := strings.TrimSpace(rawLine)
	if line == "" {
		return false, nil
	}
	if isHelpLine(line) {
		printShellContextHelp(line, state)
		return false, nil
	}
	command, err := parseShellLine(line)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return false, nil
	}
	switch command.kind {
	case shellNoop:
		return false, nil
	case shellExit:
		return true, nil
	case shellHelp:
		if command.helpTopic == "" {
			printShellHelp()
		} else {
			writeCommandHelp(os.Stdout, command.helpTopic)
		}
		return false, nil
	case shellVersion:
		fmt.Println(version.Version)
		return false, nil
	case shellShowDevices:
		return false, printLocalOutput(command, func(format outputFormat) (string, error) {
			return renderAgents(agentListView(state), format)
		})
	case shellAgentCommand:
		agents, err := state.commandTargets()
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			return false, nil
		}
		return false, runOperationForAgents(ctx, state, agents, command.operation, commandOutputOptions{
			format:   command.pipeline.format(),
			pipeline: command.pipeline,
		})
	case shellADBDiagnostics:
		agents, err := state.commandTargets()
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			return false, nil
		}
		return false, runADBDiagnosticsForAgents(ctx, state, agents, command.adbKind, commandOutputOptions{
			format:   command.pipeline.format(),
			pipeline: command.pipeline,
		})
	default:
		return false, nil
	}
}

func printLocalOutput(command shellCommand, render func(outputFormat) (string, error)) error {
	out, err := render(command.pipeline.format())
	if err != nil {
		return err
	}
	out, err = command.pipeline.apply(out)
	if err != nil {
		return err
	}
	fmt.Print(out)
	return nil
}

func selectedAgent(state *shellState) (control.AgentInfo, error) {
	agents := state.server.Agents()
	if state.selected != "" {
		// Selection is pinned to an ID, not a selector that may match a new peer.
		for _, info := range agents {
			if info.ID == state.selected {
				return info, nil
			}
		}
		return control.AgentInfo{}, fmt.Errorf("selected Android agent %q is not connected", state.selected)
	}
	switch len(agents) {
	case 0:
		return control.AgentInfo{}, errors.New("no Android agents connected")
	case 1:
		// Auto-select the only connected agent to keep the common single-device
		// flow terse, but require an explicit choice once there is ambiguity.
		state.setSelectedAgent(agents[0])
		return agents[0], nil
	default:
		return control.AgentInfo{}, errors.New("multiple Android agents connected; select --target <agent|serial|number> or deliberate broadcast (--all / --target all)")
	}
}

func resolveShellAgent(state *shellState, target string) (control.AgentInfo, error) {
	if index, err := strconv.Atoi(target); err == nil {
		agents := state.server.Agents()
		if index < 1 || index > len(agents) {
			return control.AgentInfo{}, fmt.Errorf("agent number %d is out of range", index)
		}
		return agents[index-1], nil
	}
	return state.server.ResolveAgent(target)
}

type commandOutputOptions struct {
	format             outputFormat
	pipeline           pipePipeline
	includeAgentHeader bool
	strict             bool
}

func separateTextBlock(out string, printedAny bool) string {
	if out == "" {
		return out
	}
	var b strings.Builder
	if printedAny {
		b.WriteByte('\n')
	}
	b.WriteString(out)
	if !strings.HasSuffix(out, "\n") {
		b.WriteByte('\n')
	}
	return b.String()
}

func agentTextBlock(agent string, out string, printedAny bool) string {
	return separateTextBlock(fmt.Sprintf("Agent: %s\n%s", agent, out), printedAny)
}

func runOperationForAgents(ctx context.Context, state *shellState, agents []control.AgentInfo, op Operation, output commandOutputOptions) error {
	if len(agents) == 0 {
		fmt.Fprintln(os.Stderr, "no Android agents connected")
		if output.strict {
			return errors.New("no Android agents connected")
		}
		return nil
	}
	if output.format == "" {
		output.format = outputText
	}
	if len(agents) > 1 {
		output.includeAgentHeader = true
	}
	if err := command.ValidateOperation(op); err != nil {
		if output.strict {
			return err
		}
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return nil
	}

	var outputMu sync.Mutex
	var printedAny bool
	var wg sync.WaitGroup
	errCh := make(chan error, len(agents))
	for _, agent := range agents {
		wg.Go(func() {
			if err := runCommandForAgent(ctx, state, agent, op, output, &outputMu, &printedAny); err != nil {
				errCh <- err
			}
		})
	}
	wg.Wait()
	close(errCh)
	var failures []error
	for err := range errCh {
		failures = append(failures, err)
	}
	return errors.Join(failures...)
}

func runADBDiagnosticsForAgents(ctx context.Context, state *shellState, agents []control.AgentInfo, kind string, output commandOutputOptions) error {
	if len(agents) == 0 {
		fmt.Fprintln(os.Stderr, "no Android agents connected")
		return nil
	}
	if output.format == "" {
		output.format = outputText
	}

	var outputMu sync.Mutex
	var printedAny bool
	var wg sync.WaitGroup
	errCh := make(chan error, len(agents))
	for _, agent := range agents {
		wg.Go(func() {
			if err := runADBDiagnosticsForAgent(ctx, state, agent, kind, output, &outputMu, &printedAny); err != nil {
				errCh <- err
			}
		})
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			return err
		}
	}
	return nil
}

func runADBDiagnosticsForAgent(ctx context.Context, state *shellState, agent control.AgentInfo, kind string, output commandOutputOptions, outputMu *sync.Mutex, printedAny *bool) error {
	serial := agent.Hello.GetAdbSerial()
	if serial == "" {
		outputMu.Lock()
		defer outputMu.Unlock()
		err := fmt.Errorf("%s: adb serial is not available for diagnostics", agentDisplayName(agent))
		fmt.Fprintln(os.Stderr, err)
		if output.strict {
			return err
		}
		return nil
	}
	bundle, err := adbdiag.Collect(ctx, adb.Client{Path: state.adbPath, Serial: serial}, agentDisplayName(agent), kind)
	outputMu.Lock()
	defer outputMu.Unlock()
	if err != nil {
		if output.strict {
			return err
		}
		fmt.Fprintf(os.Stderr, "%s: %v\n", agentDisplayName(agent), err)
		return nil
	}
	out, err := adbdiag.Render(bundle, output.format)
	if err != nil {
		return err
	}
	out, err = output.pipeline.apply(out)
	if err != nil {
		return err
	}
	if output.format == outputText {
		out = separateTextBlock(out, *printedAny)
	}
	fmt.Print(out)
	if output.format == outputText {
		*printedAny = true
	}
	return nil
}

func resultStatusLabel(status controlpb.CommandResult_Status) string {
	switch status {
	case controlpb.CommandResult_STATUS_OK:
		return "OK"
	case controlpb.CommandResult_STATUS_FAILED:
		return "FAILED"
	case controlpb.CommandResult_STATUS_CANCELED:
		return "CANCELED"
	default:
		return status.String()
	}
}

func runCommandForAgent(ctx context.Context, state *shellState, agent control.AgentInfo, op Operation, output commandOutputOptions, outputMu *sync.Mutex, printedAny *bool) error {
	exec, err := harness.ExecuteOperation(ctx, runner.New(state.server), agent, op)
	commandID := ""
	if len(exec.Parts) != 0 {
		commandID = exec.Parts[len(exec.Parts)-1].CommandID
	}
	result := exec.Raw
	options := exec.Options
	if err == nil && result == nil {
		err = errors.New("agent returned no result")
	}
	if err == nil && result.GetStatus() == controlpb.CommandResult_STATUS_OK && result.GetPayload() == nil {
		err = errors.New("agent returned no payload")
	}
	if err == nil && op.Name == "wifi.eht" && len(exec.Parts) > 1 && result.GetWifiDiagnostics() == nil {
		err = errors.New("wifi eht diagnostics: wrong payload")
	}
	// Findings from the common typed evaluator are authoritative even when the
	// agent acquisition itself returned OK. Do not reparse renderer text.
	failedFinding := false
	for _, finding := range exec.Findings {
		if !finding.Passed || finding.Missing {
			failedFinding = true
		}
	}
	if err == nil && failedFinding && result.GetStatus() == controlpb.CommandResult_STATUS_OK {
		result = proto.Clone(result).(*controlpb.CommandResult)
		result.Status = controlpb.CommandResult_STATUS_FAILED
		result.Message = "local operation assertion failed"
	}
	// The old text renderer inspects untrusted process output for via. Core's
	// typed hop findings, not that heuristic, determine both result and exit.
	options.TracerouteRequiredHops = nil
	supplements := commandResultSupplements{}
	if err == nil {
		supplements = collectCommandResultSupplements(ctx, state, agent, result, options, output.format)
	}

	// Multiple agents execute concurrently, but terminal output is a shared
	// stream. Serialize rendering and printing so multi-line text and JSON
	// envelopes remain intact.
	outputMu.Lock()
	defer outputMu.Unlock()
	if err != nil {
		out, renderErr := renderCommandError(agentDisplayName(agent), commandID, err, output.format, output.includeAgentHeader)
		if renderErr != nil {
			return renderErr
		}
		out, renderErr = output.pipeline.apply(redactOperationSecret(op, out))
		if renderErr != nil {
			return renderErr
		}
		if output.includeAgentHeader && output.format == outputText {
			out = separateTextBlock(out, *printedAny)
		}
		fmt.Print(out)
		if output.format == outputText {
			*printedAny = true
		}
		if output.strict {
			return fmt.Errorf("%s: %s", agentDisplayName(agent), redactOperationSecret(op, safeCommandErrorText(err.Error())))
		}
		return nil
	}
	var out string
	if output.format == outputJSON && output.includeAgentHeader {
		out, err = renderCommandResultEnvelope(agentDisplayName(agent), commandID, result)
	} else {
		out, err = renderCommandResult(agentDisplayName(agent), result, options, output.format)
		if err == nil {
			out = supplements.appendToText(out)
		}
		if err == nil && output.includeAgentHeader && output.format == outputText {
			out = agentTextBlock(agentDisplayName(agent), out, *printedAny)
		}
	}
	if err != nil {
		return err
	}
	if len(exec.Findings) != 0 {
		if output.format == outputJSON {
			var body map[string]json.RawMessage
			if err := json.Unmarshal([]byte(out), &body); err != nil {
				return err
			}
			findings, err := json.Marshal(exec.Findings)
			if err != nil {
				return err
			}
			body["findings"] = findings
			data, err := json.Marshal(body)
			if err != nil {
				return err
			}
			out = string(data) + "\n"
		} else {
			for _, finding := range exec.Findings {
				status := "PASS"
				if finding.Missing {
					status = "MISSING"
				} else if !finding.Passed {
					status = "FAIL"
				}
				out += fmt.Sprintf("%s %s: %s\n", status, finding.Metric, finding.Message)
			}
		}
	}
	if output.format == outputText && op.Name == "wifi.eht" && len(exec.Parts) > 0 && exec.Parts[0].Raw.GetWifiScan() != nil && exec.Parts[0].Raw.GetStatus() != controlpb.CommandResult_STATUS_OK {
		out += "Fresh scan: cached (refresh failed); scan data is reference\n"
	}
	out, err = output.pipeline.apply(redactOperationSecret(op, out))
	if err != nil {
		return err
	}
	if output.format == outputText && !output.includeAgentHeader {
		out = separateTextBlock(out, *printedAny)
	}
	fmt.Print(out)
	if output.format == outputText {
		*printedAny = true
	}
	if output.strict && result.GetStatus() != controlpb.CommandResult_STATUS_OK {
		return fmt.Errorf("%s: %s: %s", agentDisplayName(agent), resultStatusLabel(result.GetStatus()), redactOperationSecret(op, safeCommandErrorText(result.GetMessage())))
	}
	return nil
}

func redactOperationSecret(op Operation, text string) string {
	if op.Command == nil {
		return text
	}
	secret := op.Command.GetConnectWifi().GetPassphrase()
	if cycle := op.Command.GetCycleWifi(); cycle != nil {
		secret = cycle.GetConnect().GetPassphrase()
	}
	if secret != "" {
		text = strings.ReplaceAll(text, secret, "<redacted>")
	}
	return text
}

func useLineEditor() bool {
	if !readline.DefaultIsTerminal() {
		return false
	}
	stdin, err := os.Stdin.Stat()
	if err != nil || stdin.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	stdout, err := os.Stdout.Stat()
	if err != nil || stdout.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	return true
}
