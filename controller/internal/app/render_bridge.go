package app

import (
	"os"

	"dropcheck/controller/internal/control"
	"dropcheck/controller/internal/controlpb"
	"dropcheck/controller/internal/render"

	"github.com/charmbracelet/x/term"
)

func renderCommandResult(agent string, result *controlpb.CommandResult, options commandOptions, format outputFormat) (string, error) {
	return render.CommandResult(agent, result, options, format, terminalPresentation())
}

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

func renderCommandResultEnvelope(agent string, commandID string, result *controlpb.CommandResult) (string, error) {
	return render.CommandResultEnvelope(agent, commandID, result)
}

func renderCommandError(agent string, commandID string, err error, format outputFormat, includeAgent bool) (string, error) {
	return render.CommandError(agent, commandID, err, format, includeAgent)
}

func renderAgents(view render.AgentListView, format outputFormat) (string, error) {
	return render.Agents(view, format)
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
