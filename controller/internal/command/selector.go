package command

import "github.com/mi2428/dropcheck-agent/controller/internal/controlpb"

// NetworkSelectorField returns the selector slot for IP inspection or a probe.
// Nil means that the command does not support network selection.
func NetworkSelectorField(cmd *controlpb.RunCommand) **controlpb.NetworkSelector {
	if cmd == nil {
		return nil
	}
	switch c := cmd.Command.(type) {
	case *controlpb.RunCommand_GetIpStatus:
		return &c.GetIpStatus.Selector
	case *controlpb.RunCommand_Ping:
		return &c.Ping.Selector
	case *controlpb.RunCommand_Traceroute:
		return &c.Traceroute.Selector
	case *controlpb.RunCommand_PathMtu:
		return &c.PathMtu.Selector
	case *controlpb.RunCommand_GlobalIp:
		return &c.GlobalIp.Selector
	case *controlpb.RunCommand_ResolveDns:
		return &c.ResolveDns.Selector
	case *controlpb.RunCommand_HttpCheck:
		return &c.HttpCheck.Selector
	case *controlpb.RunCommand_Wget:
		return &c.Wget.Selector
	default:
		return nil
	}
}
