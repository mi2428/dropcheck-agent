package app

import (
	"dropcheck/controller/internal/control"
	"dropcheck/controller/internal/controlpb"
	"dropcheck/controller/internal/render"
)

func renderCommandResult(agent string, result *controlpb.CommandResult, options commandOptions, format outputFormat) (string, error) {
	return render.CommandResult(agent, result, options, format)
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

func renderTarget(view render.TargetView, format outputFormat) (string, error) {
	return render.Target(view, format)
}

func targetView(state *shellState) render.TargetView {
	view := render.TargetView{
		TargetAll:     state.targetAll,
		Selected:      state.selected,
		SelectedLabel: state.selectedLabel,
	}
	if info, ok := state.selectedAgentIfConnected(); ok {
		view.Agent = &info
	}
	return view
}

func agentDisplayName(info control.AgentInfo) string {
	return render.AgentDisplayName(info)
}
