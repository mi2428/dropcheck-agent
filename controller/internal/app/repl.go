package app

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"

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
	defer lineReader.Close()
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
		completions = append(completions, completion)
	}
	return completions, shellCompletionOffset(prefix)
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
	prefix := "dropcheck"
	if s.requestMode {
		prefix = "dropcheck/request"
	}
	if s.targetAll {
		return prefix + "[all]> "
	}
	if info, ok := s.selectedAgentIfConnected(); ok {
		s.selectedLabel = agentDisplayName(info)
		return fmt.Sprintf("%s[%s]> ", prefix, agentDisplayName(info))
	}
	if s.selectedLabel != "" {
		return fmt.Sprintf("%s[%s]> ", prefix, s.selectedLabel)
	}
	return prefix + "> "
}

func (s *shellState) selectedAgentIfConnected() (control.AgentInfo, bool) {
	if s.selected == "" {
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
	if state.requestMode {
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
		state.requestMode = false
		return false, nil
	case shellExit:
		return true, nil
	case shellHelp:
		printShellHelp()
		return false, nil
	case shellEnterRequestMode:
		state.requestMode = true
		return false, nil
	case shellShowDevices:
		return false, printLocalOutput(command, func(format outputFormat) (string, error) {
			return renderAgents(agentListView(state), format)
		})
	case shellShowTarget:
		return false, printLocalOutput(command, func(format outputFormat) (string, error) {
			return renderTarget(targetView(state), format)
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
	case shellSetTarget:
		if command.targetAll {
			state.targetAll = true
			fmt.Println("Target: all agents")
			return false, nil
		}
		info, err := resolveShellAgent(state, command.target)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			return false, nil
		}
		state.setSelectedAgent(info)
		state.targetAll = false
		fmt.Printf("Target: %s\n", agentDisplayName(info))
		return false, nil
	case shellClearTarget:
		state.targetAll = false
		if info, err := selectedAgent(state); err == nil {
			state.setSelectedAgent(info)
		}
		out, err := renderTarget(targetView(state), command.pipeline.format(outputText))
		if err != nil {
			return false, err
		}
		out, err = command.pipeline.apply(out)
		if err != nil {
			return false, err
		}
		fmt.Print(out)
		return false, nil
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
		return control.AgentInfo{}, errors.New("no selected Android agent; run show devices and set target <agent>")
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
	var wg sync.WaitGroup
	errCh := make(chan error, len(agents))
	for _, agent := range agents {
		// Each goroutine receives its own command clone. The server and render
		// path currently treat commands as immutable, but cloning here keeps
		// broadcast execution safe if per-agent metadata is added later.
		agentCmd := proto.Clone(cmd).(*controlpb.RunCommand)
		prepareCommandForAgent(state, agent, agentCmd)
		wg.Go(func() {
			if err := runCommandForAgent(ctx, state, agent, agentCmd, options, output, &outputMu); err != nil {
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

func runConfigForAgents(ctx context.Context, state *shellState, agents []control.AgentInfo, scope string, output commandOutputOptions) error {
	if len(agents) == 0 {
		fmt.Fprintln(os.Stderr, "no Android agents connected")
		return nil
	}
	if output.format == "" {
		output.format = outputText
	}
	includeAgentHeader := len(agents) > 1
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
				out = fmt.Sprintf("Agent: %s\n%s", agentDisplayName(agent), out)
			}
		}
		if err != nil {
			return err
		}
		out, err = output.pipeline.apply(out)
		if err != nil {
			return err
		}
		fmt.Print(out)
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
	if scope == "all" || scope == "controller_endpoint" {
		result, err := fetchOperationResult(ctx, state, agent, command.ControllerLinkConfigOperation())
		if err != nil {
			return view, err
		}
		view.ControllerEndpoint = result.GetControllerLinkConfig()
	}
	return view, nil
}

func fetchOperationResult(ctx context.Context, state *shellState, agent control.AgentInfo, op Operation) (*controlpb.CommandResult, error) {
	cmd, _, err := buildRunCommand(op)
	if err != nil {
		return nil, err
	}
	prepareCommandForAgent(state, agent, cmd)
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

func prepareCommandForAgent(state *shellState, agent control.AgentInfo, cmd *controlpb.RunCommand) {
	link := cmd.GetSetControllerLinkConfig()
	if link == nil || link.Config == nil || !link.Config.GetEnabled() {
		return
	}
	// The parser deliberately cannot know the live session token or resolved
	// agent identity. Inject them immediately before dispatch so set/show
	// semantics stay typed while secrets never appear in shell history.
	link.Config.Token = state.controllerToken
	link.Config.AgentId = agent.ID
	link.Config.AdbSerial = agent.Hello.GetAdbSerial()
}

func runCommandForAgent(ctx context.Context, state *shellState, agent control.AgentInfo, cmd *controlpb.RunCommand, options commandOptions, output commandOutputOptions, outputMu *sync.Mutex) error {
	commandID, err := control.RandomHex(8)
	if err != nil {
		return err
	}

	runCtx, cancel := context.WithTimeout(ctx, timeoutFor(cmd))
	result, err := state.server.Run(runCtx, agent.ID, commandID, cmd)
	cancel()

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
		fmt.Print(out)
		return nil
	}
	var out string
	if output.format == outputJSON && output.includeAgentHeader {
		out, err = renderCommandResultEnvelope(agentDisplayName(agent), commandID, result)
	} else {
		out, err = renderCommandResult(agentDisplayName(agent), result, options, output.format)
		if err == nil && output.includeAgentHeader && output.format == outputText {
			out = fmt.Sprintf("Agent: %s\n%s", agentDisplayName(agent), out)
		}
	}
	if err != nil {
		return err
	}
	out, err = output.pipeline.apply(out)
	if err != nil {
		return err
	}
	fmt.Print(out)
	return nil
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
