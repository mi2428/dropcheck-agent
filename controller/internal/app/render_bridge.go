package app

import (
	"os"
	"strings"

	"dropcheck/controller/internal/control"
	"dropcheck/controller/internal/render"

	"github.com/charmbracelet/x/term"
)

func terminalPresentation() render.Presentation {
	view := render.Presentation{}
	fd := os.Stdout.Fd()
	if term.IsTerminal(fd) {
		if width, _, err := term.GetSize(fd); err == nil {
			view.Width = width
		}
	}
	return view
}

func safeCommandErrorText(text string) string {
	return strings.TrimSpace(render.TextBlock(text, render.Presentation{}))
}

func agentListView(state *shellState) render.AgentListView {
	return render.AgentListView{
		Agents:    state.server.Agents(),
		Selected:  state.selected,
		TargetAll: state.targetAll,
	}
}

func agentDisplayName(info control.AgentInfo) string {
	return render.AgentDisplayName(info)
}
