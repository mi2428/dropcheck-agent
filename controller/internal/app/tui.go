package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"

	"dropcheck/controller/internal/harness"
	"dropcheck/controller/internal/runner"
	"dropcheck/controller/internal/session"
	"dropcheck/controller/internal/tui"
	"dropcheck/controller/internal/watch"

	"github.com/charmbracelet/x/term"
)

func runTUI(ctx context.Context, options shellOptions, args []string) error {
	return runTUIFiles(ctx, options, args, os.Stdin, os.Stdout)
}

func runTUIFiles(ctx context.Context, options shellOptions, args []string, input, output *os.File) error {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h" || args[0] == "help") {
		writeTUIHelp(output)
		return nil
	}
	if len(args) > 1 {
		return fmt.Errorf("usage: dropcheck [flags] tui [PLAN.yml]")
	}
	// Guard before reading a Plan, discovering devices or starting a session.
	// No-argument dropcheck still displays the existing top-level help.
	if !term.IsTerminal(input.Fd()) || !term.IsTerminal(output.Fd()) {
		return fmt.Errorf("dropcheck tui requires terminal input and output; run 'dropcheck tui --help' or use a one-shot command")
	}
	uiCtx, cancel := context.WithCancel(ctx)
	var mu sync.Mutex
	var controlSession *session.Session
	defer func() {
		cancel()
		mu.Lock()
		defer mu.Unlock()
		if controlSession != nil { controlSession.Close() }
	}()
	load := func(loadCtx context.Context, path string) (*harness.CompiledPlan, harness.OperationRunner, error) {
		plan, err := watch.LoadFile(path)
		if err != nil {
			// YAML decode errors may echo malformed credential values. Keep the
			// stage visible without returning unsafe parser text to the screen.
			return nil,nil,fmt.Errorf("YAML Plan could not be loaded or validated; check its path, syntax and values (credential details withheld)")
		}
		mu.Lock()
		defer mu.Unlock()
		if err := loadCtx.Err(); err != nil { return nil,nil,err }
		if controlSession == nil {
			controlSession, err = startControlSession(loadCtx, options)
			if err != nil { return nil,nil,fmt.Errorf("agent metadata session could not start; check connection and authorization") }
		}
		compiled, err := harness.Compile(plan, controlSession.Server.Agents())
		if err != nil { return nil,nil,err } // Core returns a credential-masked error.
		return compiled,runner.New(controlSession.Server),nil
	}
	path := ""
	if len(args) == 1 { path = args[0] }
	return tui.RunWorkflow(uiCtx,tui.WorkflowOptions{Path:path,Load:load})
}

func writeTUIHelp(w io.Writer) {
	_,_ = fmt.Fprintln(w,`Usage: dropcheck [flags] tui [PLAN.yml]

Requires terminal input and output. Without a path, enter an existing YAML Plan
inside the TUI. Loading validates the shared Plan and may establish an agent
metadata session; Wi-Fi mutations and probes require explicit Preview -> Start.

Selection: Tab agents/targets/checks, Space toggle, 1 Once, n N rounds, l Loop.
Enter previews; Enter again starts. q/Ctrl-C during a run stops it and waits for
cleanup, then retains review. q in review exits and restores the terminal.
GLOBAL Ctrl-Z pauses/resumes; GLOBAL Ctrl-N skips. v explicitly chooses a scope;
p/s/x pause/skip/cancel only that confirmed scope. Dashboard focus is not scope.
i inspects measurements/expectations/attempts/cleanup; r previews selected rerun,
f previews failed/missing rerun; e reselects, o explicitly reloads agent bindings.
No automatic device replacement. Existing filters and pinned details remain.
No-argument dropcheck continues to show help.`)
}
