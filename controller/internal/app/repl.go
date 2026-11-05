package app

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/chzyer/readline"
	"github.com/mi2428/dropcheck-agent/controller/internal/acquire"
	"github.com/mi2428/dropcheck-agent/controller/internal/adb"
	"github.com/mi2428/dropcheck-agent/controller/internal/adbdiag"
	"github.com/mi2428/dropcheck-agent/controller/internal/command"
	"github.com/mi2428/dropcheck-agent/controller/internal/control"
	"github.com/mi2428/dropcheck-agent/controller/internal/controlpb"
	"github.com/mi2428/dropcheck-agent/controller/internal/pipeline"
	"github.com/mi2428/dropcheck-agent/controller/internal/render"
	"github.com/mi2428/dropcheck-agent/controller/internal/runner"
	"github.com/mi2428/dropcheck-agent/controller/internal/shell"
	"github.com/mi2428/dropcheck-agent/controller/internal/version"
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
	defer func() { state.defaultPassphrase = "" }()
	input := readline.NewCancelableStdin(os.Stdin)
	defer input.Close()
	var lineReader *readline.Instance
	var activeMu sync.Mutex
	var activeCancel context.CancelFunc
	cfg := &readline.Config{
		Stdin:        input,
		Prompt:       state.prompt(),
		HistoryLimit: 1000,
		AutoComplete: shellReadlineCompleter{},
		EOFPrompt:    "\n",
		FuncFilterInputRune: func(key rune) (rune, bool) {
			activeMu.Lock()
			cancel := activeCancel
			activeMu.Unlock()
			if cancel != nil {
				if key == readline.CharInterrupt || key == 0 {
					cancel()
				}
				return 0, false
			}
			return key, true
		},
	}
	cfg.SetListener(func(line []rune, pos int, key rune) ([]rune, int, bool) {
		if lineReader == nil {
			return nil, 0, false
		}
		newLine, newPos, ok := handleShellHelpKey(lineReader.Stdout(), line, pos, key)
		if !ok {
			newLine, newPos, ok = handleShellCompletionHintKey(lineReader.Stdout(), line, pos, key)
		}
		return newLine, newPos, ok
	})

	var err error
	lineReader, err = readline.NewEx(cfg)
	if err != nil {
		return err
	}
	defer func() {
		_ = input.Close() // FillableStdin.Close does not close its underlying reader.
		_ = lineReader.Close()
	}()
	// Establish an empty scratch entry before readline's first history.Update.
	_ = lineReader.SaveHistory("")
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
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
		commandCtx, cancel := context.WithCancel(ctx)
		activeMu.Lock()
		activeCancel = cancel
		activeMu.Unlock()
		// Readline restores cooked mode when Readline returns and stops its
		// terminal reader at Enter. Keep keys under our control during execution:
		// Ctrl-C cancels this command, and all other typeahead is discarded.
		if err := lineReader.Terminal.EnterRawMode(); err != nil {
			cancel()
			return err
		}
		lineReader.SetPrompt("")
		lineReader.Terminal.KickRead()
		done, err := runReplLine(commandCtx, state, line)
		restoreErr := lineReader.Terminal.ExitRawMode()
		activeMu.Lock()
		activeCancel = nil
		activeMu.Unlock()
		cancel()
		if restoreErr != nil {
			return restoreErr
		}
		if err != nil || done {
			return err
		}
	}
}

func replScanner(ctx context.Context, state *shellState) error {
	defer func() { state.defaultPassphrase = "" }()
	scanner := bufio.NewScanner(os.Stdin)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
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

type shellReadlineCompleter struct{}

func (c shellReadlineCompleter) Do(line []rune, pos int) ([][]rune, int) {
	if pos < 0 || pos > len(line) {
		return nil, 0
	}
	prefix := string(line[:pos])
	end, ok := command.CompletionEnd(string(line), len(prefix))
	if !ok {
		return nil, 0
	}
	tail := string(line)[len(prefix):end]
	prefixRunes := []rune(prefix)
	var fullCandidates []string
	var completions [][]rune
	for _, candidate := range shell.CompleteLine(prefix) {
		if pos < len(line) {
			candidate = strings.TrimSuffix(candidate, " ")
		}
		candidateRunes := []rune(candidate)
		if !hasRunePrefix(candidateRunes, prefixRunes) {
			continue
		}
		// Readline can only insert. The listener replaces nonmatching token tails.
		completion, matches := strings.CutSuffix(string(candidateRunes[len(prefixRunes):]), tail)
		if !matches {
			continue
		}
		if shell.IsPlaceholderCandidate(completion) {
			continue
		}
		fullCandidates = append(fullCandidates, candidate)
		completions = append(completions, []rune(completion))
	}
	if pos == len(line) && len(completions) == 1 && len(completions[0]) > 0 && shouldAppendReadlineCompletionSpace(fullCandidates[0]) {
		completions[0] = append(completions[0], ' ')
	}
	return completions, shellCompletionOffset(prefix)
}

func shouldAppendReadlineCompletionSpace(candidate string) bool {
	return slices.Contains(shell.CompleteLine(candidate), candidate+" ")
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

func handleShellHelpKey(w io.Writer, line []rune, pos int, key rune) ([]rune, int, bool) {
	if !shell.IsHelpRune(key) || pos <= 0 || pos > len(line) {
		return nil, 0, false
	}
	questionIndex := pos - 1
	if !shell.IsHelpRune(line[questionIndex]) {
		return nil, 0, false
	}
	if !command.HelpPosition(string(line[:questionIndex])) {
		return nil, 0, false
	}

	helpLine := append([]rune(nil), line[:pos]...)
	helpLine[len(helpLine)-1] = '?'
	var b strings.Builder
	b.WriteByte('\n')
	shell.WriteContextHelp(&b, string(helpLine))
	_, _ = io.WriteString(w, b.String())

	newLine := append([]rune(nil), line[:questionIndex]...)
	newLine = append(newLine, line[pos:]...)
	return newLine, questionIndex, true
}

func handleShellCompletionHintKey(w io.Writer, line []rune, pos int, key rune) ([]rune, int, bool) {
	if key != readline.CharTab || pos < 0 || pos > len(line) {
		return nil, 0, false
	}
	if pos < len(line) {
		prefix := string(line[:pos])
		end, ok := command.CompletionEnd(string(line), len(prefix))
		if !ok {
			return nil, 0, false
		}
		var choices []string
		for _, candidate := range shell.CompleteLine(prefix) {
			if !strings.HasSuffix(candidate, ">") {
				choices = append(choices, strings.TrimSuffix(candidate, " "))
			}
		}
		if len(choices) == 1 {
			return []rune(choices[0] + string(line)[end:]), len([]rune(choices[0])), true
		}
	}
	hint := shell.CompletionHintLine(string(line[:pos]))
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
	return fmt.Sprintf("%s> ", label)
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
	if shell.IsHelpLine(line) {
		shell.PrintContextHelp(line)
		return false, nil
	}
	command, err := shell.ParseLine(line)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return false, nil
	}
	switch command.Kind {
	case shell.SetDefaultPassphrase:
		state.defaultPassphrase = command.Passphrase
		return false, printLocalOutput(command, func() (string, error) {
			if command.Passphrase == "" {
				return "default passphrase cleared\n", nil
			}
			return "default passphrase updated (session only)\n", nil
		})
	case shell.ClearDefaultPassphrase:
		state.defaultPassphrase = ""
		return false, printLocalOutput(command, func() (string, error) { return "default passphrase cleared\n", nil })
	case shell.Connect:
		return false, connectShell(ctx, state, command)
	case shell.Noop:
		return false, nil
	case shell.Exit:
		return true, nil
	case shell.Help:
		return false, printLocalOutput(command, func() (string, error) {
			var b strings.Builder
			if command.HelpTopic == "" {
				shell.WriteContextHelp(&b, "?")
			} else {
				shell.WriteHelpTopic(&b, command.HelpTopic)
			}
			return b.String(), nil
		})
	case shell.Version:
		return false, printLocalOutput(command, func() (string, error) { return "Controller version: " + version.Version + "\n", nil })
	case shell.CLI:
		return false, printLocalOutput(command, func() (string, error) { return renderShellSession(state), nil })
	case shell.ShowDevices:
		return false, printLocalOutput(command, func() (string, error) {
			return render.Agents(agentListView(state))
		})
	case shell.Profiles:
		return false, printLocalOutput(command, showProfiles)
	case shell.LastReport:
		return false, printLocalOutput(command, func() (string, error) {
			view := terminalPresentation()
			if command.Pipeline.StageCount() > 0 || command.Pipeline.DisplayJSON() {
				view.Width = 0
			}
			return renderLastShellTest(state, view)
		})
	case shell.Profile:
		state.lastWifiTest = nil
		if command.ProfileName == "" {
			return false, printLocalOutput(command, showProfiles)
		}
		err := state.runCheck(ctx, command.ProfileName, command.SSID, command.Family, command.BSSID, command.Rejection, command.Pipeline, command.Lab)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
		return false, nil
	case shell.AgentCommand, shell.WifiTest:
		if command.Kind == shell.WifiTest && !prepareWifiTest(state, command) {
			return false, nil
		}
		agents, err := state.commandTargets()
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			return false, nil
		}
		return false, runOperationForAgents(ctx, state, agents, command.Operation, commandOutputOptions{
			pipeline:     command.Pipeline,
			singleTarget: command.Effect == "mutation" || command.Kind == shell.WifiTest,
			wifiTest:     command.Kind == shell.WifiTest,
		})
	case shell.ADBDiagnostics:
		agents, err := state.commandTargets()
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			return false, nil
		}
		return false, runADBDiagnosticsForAgents(ctx, state, agents, command.ADBDiagnosticsKind, commandOutputOptions{
			pipeline:     command.Pipeline,
			adbTimeoutMs: command.ADBTimeoutMs,
		})
	default:
		return false, nil
	}
}

func printLocalOutput(command shell.Command, render func() (string, error)) error {
	out, err := render()
	if err != nil {
		return err
	}
	out, err = shellOutput(command.Pipeline, out, true)
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
		return control.AgentInfo{}, errors.New("multiple Android agents connected; select --target <agent|serial|#number> or deliberate broadcast (--all / --target all)")
	}
}

func resolveShellAgent(state *shellState, target string) (control.AgentInfo, error) {
	if after, ok := strings.CutPrefix(target, "#"); ok {
		index, err := strconv.Atoi(after)
		if err != nil {
			return control.AgentInfo{}, fmt.Errorf("invalid agent number %q", target)
		}
		agents := state.server.Agents()
		if index < 1 || index > len(agents) {
			return control.AgentInfo{}, fmt.Errorf("agent number %d is out of range", index)
		}
		return agents[index-1], nil
	}
	return state.server.ResolveAgent(target)
}

type commandOutputOptions struct {
	pipeline           pipeline.Pipeline
	includeAgentHeader bool
	use                bool
	singleTarget       bool
	wifiTest           bool
	adbTimeoutMs       uint32
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
	if output.singleTarget && (state.targetAll || len(agents) != 1) {
		fmt.Fprintln(os.Stderr, "this operation requires one pinned agent")
		return nil
	}
	if output.use && len(agents) != 1 {
		return fmt.Errorf("use/connect requires one selected agent")
	}
	if output.use {
		state.failedUse = true // Before dispatch, not after a possibly failed connection.
	}
	if len(agents) == 0 {
		fmt.Fprintln(os.Stderr, "no Android agents connected")
		return nil
	}
	if len(agents) > 1 {
		output.includeAgentHeader = true
	}
	if err := command.ValidateOperation(op); err != nil {
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
		return nil
	}
	client := adb.Client{Path: state.adbPath, Serial: serial}
	bundle, err := adbdiag.CollectInteractive(ctx, client, agentDisplayName(agent), kind, time.Duration(output.adbTimeoutMs)*time.Millisecond, func(index, total int, name string) {
		outputMu.Lock()
		defer outputMu.Unlock()
		fmt.Fprintf(os.Stderr, "%s: ADB %d/%d %s\n", strings.TrimSpace(render.TextBlock(agentDisplayName(agent), render.Presentation{})), index, total, name)
	})
	outputMu.Lock()
	defer outputMu.Unlock()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", agentDisplayName(agent), err)
		return nil
	}
	view := terminalPresentation()
	if output.pipeline.StageCount() > 0 || output.pipeline.DisplayJSON() {
		view.Width = 0
	}
	out, err := adbdiag.Render(bundle, view)
	if err != nil {
		return err
	}
	out, err = shellOutput(output.pipeline, out, bundle.OK())
	if err != nil {
		return err
	}
	out = separateTextBlock(out, *printedAny)
	fmt.Print(out)
	if !bundle.OK() {
		fmt.Fprintf(os.Stderr, "%s: ADB diagnostics FAILED\n", agentDisplayName(agent))
	}
	*printedAny = true
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
	exec, err := acquire.Execute(ctx, runner.New(state.server), agent, op)
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
	supplements := commandResultSupplements{}
	if err == nil {
		supplements = collectCommandResultSupplements(ctx, state, agent, result, options)
	}
	if ctx.Err() != nil && result != nil {
		result = proto.Clone(result).(*controlpb.CommandResult)
		result.Status = controlpb.CommandResult_STATUS_CANCELED
		result.Message = "collection canceled; retained data is reference, completed device actions are not rolled back"
	}
	if output.wifiTest && result != nil && result.GetWifiAssert() != nil {
		state.lastWifiTest = render.SanitizedResult(result)
		state.lastWifiTestAgent = agentDisplayName(agent)
	}

	// Multiple agents execute concurrently, but terminal output is a shared
	// stream. Serialize rendering and printing so multi-line text and JSON
	// envelopes remain intact.
	outputMu.Lock()
	defer outputMu.Unlock()
	if err != nil {
		out, renderErr := render.CommandError(agentDisplayName(agent), err, output.includeAgentHeader)
		if renderErr != nil {
			return renderErr
		}
		out, renderErr = shellOutput(output.pipeline, redactOperationSecret(op, out), false)
		if renderErr != nil {
			return renderErr
		}
		if output.includeAgentHeader {
			out = separateTextBlock(out, *printedAny)
		}
		fmt.Print(out)
		fmt.Fprintf(os.Stderr, "%s: command FAILED: %s\n", agentDisplayName(agent), redactOperationSecret(op, safeCommandErrorText(err.Error())))
		*printedAny = true
		return nil
	}
	view := terminalPresentation()
	if output.pipeline.StageCount() > 0 || output.pipeline.DisplayJSON() {
		view.Width = 0
	}
	view.Detail = options.Detail
	view.DetailAvailable = true
	out, err := render.CommandResult(agentDisplayName(agent), result, options, view)
	if err == nil {
		if output.pipeline.StageCount() > 0 || output.pipeline.DisplayJSON() {
			out = supplements.appendToText(out, render.Presentation{})
		} else {
			out = supplements.appendToText(out)
		}
	}
	if err == nil && output.includeAgentHeader {
		out = agentTextBlock(agentDisplayName(agent), out, *printedAny)
	}
	if err != nil {
		return err
	}
	for _, finding := range exec.Findings {
		status := "PASS"
		if finding.Missing {
			status = "MISSING"
		} else if !finding.Passed {
			status = "FAIL"
		}
		out += fmt.Sprintf("%s %s: %s\n", status, finding.Metric, finding.Message)
	}
	if op.Name == "wifi.eht" && len(exec.Parts) > 0 && exec.Parts[0].Raw.GetWifiScan() != nil && exec.Parts[0].Raw.GetStatus() != controlpb.CommandResult_STATUS_OK {
		out += "Fresh scan: cached (refresh failed); scan data is reference\n"
	}
	out, err = shellOutput(output.pipeline, redactOperationSecret(op, out), result.GetStatus() == controlpb.CommandResult_STATUS_OK)
	if err != nil {
		return err
	}
	if !output.includeAgentHeader {
		out = separateTextBlock(out, *printedAny)
	}
	fmt.Print(out)
	if result.GetStatus() != controlpb.CommandResult_STATUS_OK {
		fmt.Fprintf(os.Stderr, "%s: %s\n", agentDisplayName(agent), resultStatusLabel(result.GetStatus()))
	}
	*printedAny = true
	if output.use && result.GetStatus() == controlpb.CommandResult_STATUS_OK {
		state.failedUse = false
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
