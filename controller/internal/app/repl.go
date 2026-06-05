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

	"dropcheck/controller/internal/adb"
	"dropcheck/controller/internal/adbdiag"
	"dropcheck/controller/internal/command"
	"dropcheck/controller/internal/control"
	"dropcheck/controller/internal/controlpb"
	"github.com/chzyer/readline"
	"google.golang.org/protobuf/proto"
)

func repl(ctx context.Context, state *shellState) error {
	fmt.Println("press '?' for context help, or type 'help' for commands")
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
		if strings.TrimSpace(line) != "" {
			_ = lineReader.SaveHistory(line)
		}
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
	for i := len(runes) - 1; i >= 0; i-- {
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
	writeShellContextHelp(&b, string(helpLine), optionalShellState(states))
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
	switch s.mode {
	case shellModeConfigure:
		return fmt.Sprintf("%s(config)# ", label)
	case shellModeRequest:
		return fmt.Sprintf("%s(request)# ", label)
	default:
		return fmt.Sprintf("%s# ", label)
	}
}

func (s *shellState) selectedAgentIfConnected() (control.AgentInfo, bool) {
	if s.selected == "" || s.server == nil {
		return control.AgentInfo{}, false
	}
	info, err := s.server.ResolveAgent(s.selected)
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
	parse := parseShellLine
	switch state.mode {
	case shellModeConfigure:
		parse = parseShellConfigureLine
	case shellModeRequest:
		parse = parseShellRequestLine
	}
	command, err := parse(line)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return false, nil
	}
	switch command.kind {
	case shellNoop:
		return false, nil
	case shellExitMode:
		state.mode = shellModeOperational
		return false, nil
	case shellExit:
		return true, nil
	case shellHelp:
		printShellHelp()
		return false, nil
	case shellEnterConfigureMode:
		state.mode = shellModeConfigure
		return false, nil
	case shellEnterRequestMode:
		state.mode = shellModeRequest
		return false, nil
	case shellShowDevices:
		return false, printLocalOutput(command, func(format outputFormat) (string, error) {
			return renderAgents(agentListView(state), format)
		})
	case shellShowConfig:
		agents, err := state.commandTargets()
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			return false, nil
		}
		return false, runConfigForAgents(ctx, state, agents, command.configScope, commandOutputOptions{
			format:   command.pipeline.format(outputText),
			pipeline: command.pipeline,
		})
	case shellAgentCommand:
		agents, err := state.commandTargets()
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			return false, nil
		}
		return false, runOperationForAgents(ctx, state, agents, command.operation, commandOutputOptions{
			format:   command.pipeline.format(outputText),
			pipeline: command.pipeline,
		})
	case shellADBDiagnostics:
		agents, err := state.commandTargets()
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			return false, nil
		}
		return false, runADBDiagnosticsForAgents(ctx, state, agents, command.adbKind, commandOutputOptions{
			format:   command.pipeline.format(outputText),
			pipeline: command.pipeline,
		})
	case shellStandaloneSync:
		if err := syncStandaloneRuns(ctx, state, standaloneSyncOptions{
			OutputDir:  command.syncOutput,
			Limit:      command.syncLimit,
			MarkSynced: command.syncMark,
		}); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
		}
		return false, nil
	default:
		return false, nil
	}
}

func printLocalOutput(command shellCommand, render func(outputFormat) (string, error)) error {
	out, err := render(command.pipeline.format(outputText))
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
	if state.selected != "" {
		if info, err := state.server.ResolveAgent(state.selected); err == nil {
			return info, nil
		}
	}
	agents := state.server.Agents()
	switch len(agents) {
	case 0:
		return control.AgentInfo{}, errors.New("no Android agents connected")
	case 1:
		// Auto-select the only connected agent to keep the common single-device
		// flow terse, but require an explicit choice once there is ambiguity.
		state.setSelectedAgent(agents[0])
		return agents[0], nil
	default:
		return control.AgentInfo{}, errors.New("no selected Android agent; restart shell with --target <agent|serial|number|all>")
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
		return nil
	}
	if output.format == "" {
		output.format = outputText
	}
	if len(agents) > 1 {
		output.includeAgentHeader = true
	}
	cmd, options, err := buildRunCommand(op)
	if err != nil {
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
		// Each goroutine receives its own command clone. The server and render
		// path currently treat commands as immutable, but cloning here keeps
		// broadcast execution isolated across agents.
		agentCmd := proto.Clone(cmd).(*controlpb.RunCommand)
		wg.Go(func() {
			if err := runCommandForAgent(ctx, state, agent, agentCmd, options, output, &outputMu, &printedAny); err != nil {
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
		fmt.Fprintf(os.Stderr, "%s: adb serial is not available for diagnostics\n", agentDisplayName(agent))
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

func runConfigForAgents(ctx context.Context, state *shellState, agents []control.AgentInfo, scope string, output commandOutputOptions) error {
	if len(agents) == 0 {
		fmt.Fprintln(os.Stderr, "no Android agents connected")
		return nil
	}
	if output.format == "" {
		output.format = outputText
	}
	includeAgentHeader := len(agents) > 1
	var printedAny bool
	for _, agent := range agents {
		view, err := fetchConfigView(ctx, state, agent, scope)
		if err != nil {
			if output.strict {
				return err
			}
			fmt.Fprintf(os.Stderr, "%s: %v\n", agentDisplayName(agent), err)
			continue
		}
		var out string
		if output.format == outputJSON && includeAgentHeader {
			out, err = renderConfigEnvelope(agentDisplayName(agent), view)
		} else {
			out, err = renderConfig(view, output.format)
			if err == nil && includeAgentHeader && output.format == outputText {
				out = agentTextBlock(agentDisplayName(agent), out, printedAny)
			}
		}
		if err != nil {
			return err
		}
		out, err = output.pipeline.apply(out)
		if err != nil {
			return err
		}
		if output.format == outputText && !includeAgentHeader {
			out = separateTextBlock(out, printedAny)
		}
		fmt.Print(out)
		if output.format == outputText {
			printedAny = true
		}
	}
	return nil
}

func fetchConfigView(ctx context.Context, state *shellState, agent control.AgentInfo, scope string) (configView, error) {
	var view configView
	if scope == "" {
		scope = "all"
	}
	if scope == "all" || scope == "standalone" {
		result, err := fetchOperationResult(ctx, state, agent, command.StandaloneConfigOperation())
		if err != nil {
			return view, err
		}
		view.Standalone = result.GetStandaloneConfig()
	}
	return view, nil
}

func fetchOperationResult(ctx context.Context, state *shellState, agent control.AgentInfo, op Operation) (*controlpb.CommandResult, error) {
	cmd, _, err := buildRunCommand(op)
	if err != nil {
		return nil, err
	}
	commandID, err := control.RandomHex(8)
	if err != nil {
		return nil, err
	}
	runCtx, cancel := context.WithTimeout(ctx, timeoutFor(cmd))
	result, err := state.server.Run(runCtx, agent.ID, commandID, cmd)
	cancel()
	if err != nil {
		return nil, err
	}
	if result.GetStatus() != controlpb.CommandResult_STATUS_OK {
		return nil, fmt.Errorf("%s: %s", resultStatusLabel(result.GetStatus()), result.GetMessage())
	}
	return result, nil
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

func runCommandForAgent(ctx context.Context, state *shellState, agent control.AgentInfo, cmd *controlpb.RunCommand, options commandOptions, output commandOutputOptions, outputMu *sync.Mutex, printedAny *bool) error {
	commandID, err := control.RandomHex(8)
	if err != nil {
		return err
	}

	var freshScan *controlpb.WifiScan
	if options.WifiEHTFreshScan {
		freshScan, err = runWifiEHTFreshScan(ctx, state, agent, options)
	}
	var result *controlpb.CommandResult
	if err == nil {
		runCtx, cancel := context.WithTimeout(ctx, timeoutFor(cmd))
		result, err = state.server.Run(runCtx, agent.ID, commandID, cmd)
		cancel()
		if err == nil {
			applyWifiEHTFreshScan(result, freshScan)
		}
	}
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
		out, renderErr = output.pipeline.apply(out)
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
	out, err = output.pipeline.apply(out)
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
	return nil
}

func runWifiEHTFreshScan(ctx context.Context, state *shellState, agent control.AgentInfo, options commandOptions) (*controlpb.WifiScan, error) {
	commandID, err := control.RandomHex(8)
	if err != nil {
		return nil, err
	}
	cmd := &controlpb.RunCommand{
		Label: "wifi eht fresh scan",
		Command: &controlpb.RunCommand_GetFreshWifiScan{GetFreshWifiScan: &controlpb.GetFreshWifiScan{
			Band:      controlpb.WifiBand_WIFI_BAND_ALL,
			TimeoutMs: options.WifiEHTFreshScanTimeoutMs,
		}},
	}
	runCtx, cancel := context.WithTimeout(ctx, timeoutFor(cmd))
	result, err := state.server.Run(runCtx, agent.ID, commandID, cmd)
	cancel()
	if err != nil {
		return nil, fmt.Errorf("wifi eht fresh scan: %w", err)
	}
	scan := result.GetWifiScan()
	if scan == nil {
		return nil, fmt.Errorf("wifi eht fresh scan: agent returned %s without wifi scan", resultPayloadLabel(result))
	}
	return scan, nil
}

func applyWifiEHTFreshScan(result *controlpb.CommandResult, freshScan *controlpb.WifiScan) {
	if freshScan == nil {
		return
	}
	diagnostics := result.GetWifiDiagnostics()
	if diagnostics == nil {
		return
	}
	diagnostics.Scan = proto.Clone(freshScan).(*controlpb.WifiScan)
}

func resultPayloadLabel(result *controlpb.CommandResult) string {
	if result == nil {
		return "empty result"
	}
	if result.GetPayload() == nil {
		return "empty payload"
	}
	return fmt.Sprintf("%T", result.GetPayload())
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
