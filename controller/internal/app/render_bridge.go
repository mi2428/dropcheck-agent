package app

import (
	"dropcheck/controller/internal/control"
	"dropcheck/controller/internal/controlpb"
	"dropcheck/controller/internal/render"
)

type configView = render.ConfigView

func renderCommandResult(agent string, result *controlpb.CommandResult, options commandOptions, format outputFormat) (string, error) {
	return render.CommandResult(agent, result, options, format)
}

func renderCommandResultEnvelope(agent string, commandID string, result *controlpb.CommandResult) (string, error) {
	return render.CommandResultEnvelope(agent, commandID, result)
}

func renderCommandError(agent string, commandID string, err error, format outputFormat, includeAgent bool) (string, error) {
	return render.CommandError(agent, commandID, err, format, includeAgent)
}

func renderConfig(view render.ConfigView, format outputFormat) (string, error) {
	return render.Config(view, format)
}

func renderConfigEnvelope(agent string, view render.ConfigView) (string, error) {
	return render.ConfigEnvelope(agent, view)
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
