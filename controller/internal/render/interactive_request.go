package render

import (
	"fmt"
	"slices"
	"strings"

	"github.com/mi2428/dropcheck-agent/controller/internal/command"
	"github.com/mi2428/dropcheck-agent/controller/internal/controlpb"
)

// Typed Shell records/tables; native output remains a separate reference block.
func renderInteractiveRequest(b *strings.Builder, result *controlpb.CommandResult, options command.Options, view Presentation) bool {
	field := func(label string, value any) { writeRecord(b, label, value) }
	optional := func(present bool, value any) string {
		if !present {
			return "?"
		}
		return fmt.Sprint(value)
	}
	switch payload := result.Payload.(type) {
	case *controlpb.CommandResult_Ping:
		p := payload.Ping
		field("Host", p.GetHost())
		field("Transmitted", optional(p.Transmitted != nil, p.GetTransmitted()))
		field("Received", optional(p.Received != nil, p.GetReceived()))
		field("Packet loss (%)", optional(p.PacketLossPercent != nil, p.GetPacketLossPercent()))
		field("Min/avg/max (ms)", optional(p.MinMs != nil, p.GetMinMs())+"/"+optional(p.AvgMs != nil, p.GetAvgMs())+"/"+optional(p.MaxMs != nil, p.GetMaxMs()))
		field("Payload bytes", p.GetSizeBytes())
		field("Interface", empty(p.GetInterfaceName(), "default"))
		field("Exit code", p.GetExitCode())
		if p.GetOutput() != "" {
			writeSection(b, "Native output (reference; not a separate verdict)")
			b.WriteString(strings.TrimRight(p.GetOutput(), "\n") + "\n")
		}
	case *controlpb.CommandResult_Traceroute:
		p := payload.Traceroute
		field("Host", p.GetHost())
		field("Max hops", p.GetMaxHops())
		field("Payload bytes", p.GetSizeBytes())
		field("Exit code", p.GetExitCode())
		field("Reached target", optional(p.ReachedTarget != nil, p.GetReachedTarget()))
		field("Interface", empty(p.GetInterfaceName(), "default"))
		field("Executable", empty(p.GetExecutable(), "unknown"))
		field("Error", p.GetError())
		if p.GetObservationError() != "" {
			field("Observation", p.GetObservationError())
		}
		if len(options.TracerouteRequiredHops) > 0 {
			var matched, missing []string
			for _, required := range options.TracerouteRequiredHops {
				found := slices.ContainsFunc(p.GetHops(), func(hop *controlpb.TracerouteHop) bool {
					return slices.Contains(hop.GetAddresses(), required) || slices.Contains(hop.GetHostnames(), required)
				})
				if found {
					matched = append(matched, required)
				} else {
					missing = append(missing, required)
				}
			}
			field("Required hops", strings.Join(options.TracerouteRequiredHops, ","))
			field("Matched hops", strings.Join(matched, ","))
			field("Missing hops", strings.Join(missing, ","))
		}
		var rows [][]string
		for _, hop := range p.GetHops() {
			var rtts []string
			for _, rtt := range hop.GetRttMs() {
				rtts = append(rtts, fmt.Sprint(rtt))
			}
			rows = append(rows, []string{fmt.Sprint(hop.GetIndex()), strings.Join(hop.GetHostnames(), ","), strings.Join(hop.GetAddresses(), ","), strings.Join(rtts, ","), fmt.Sprint(hop.GetTimedOut())})
		}
		writeDisplayTable(b, []displayTableColumn{{header: "HOP"}, {header: "HOST"}, {header: "ADDRESS"}, {header: "RTT(ms)"}, {header: "TIMEOUT"}}, rows, view.Width)
		if p.GetOutput() != "" {
			writeSection(b, "Native output (reference; not a separate verdict)")
			b.WriteString(strings.TrimRight(p.GetOutput(), "\n") + "\n")
		}
	case *controlpb.CommandResult_PathMtu:
		p := payload.PathMtu
		field("Host", p.GetHost())
		field("Discovered", p.GetDiscovered())
		field("Path MTU", p.GetPathMtuBytes())
		field("Payload bytes", p.GetPayloadSizeBytes())
		field("Range", fmt.Sprintf("%d-%d", p.GetMinMtuBytes(), p.GetMaxMtuBytes()))
		field("IP overhead", p.GetIpOverheadBytes())
		field("Interface", empty(p.GetInterfaceName(), "default"))
		field("Error", p.GetError())
		var rows [][]string
		for _, probe := range p.GetProbes() {
			rows = append(rows, []string{fmt.Sprint(probe.GetMtuBytes()), fmt.Sprint(probe.GetPayloadSizeBytes()), fmt.Sprint(probe.GetPassed()), fmt.Sprint(probe.GetExitCode()), fmt.Sprintf("%dms", probe.GetElapsedMs())})
		}
		writeDisplayTable(b, []displayTableColumn{{header: "MTU"}, {header: "PAYLOAD"}, {header: "PASS"}, {header: "EXIT"}, {header: "ELAPSED"}}, rows, view.Width)
	case *controlpb.CommandResult_GlobalIp:
		p := payload.GlobalIp
		field("Service", empty(p.GetService(), "ifconfig.me"))
		field("Requested family", ipFamilyName(p.GetRequestedFamily()))
		field("Interface", empty(p.GetInterfaceName(), "default"))
		field("Error", p.GetError())
		var rows [][]string
		for _, address := range p.GetAddresses() {
			rows = append(rows, []string{ipFamilyName(address.GetFamily()), address.GetIp(), fmt.Sprint(address.GetGlobal()), fmt.Sprint(address.GetStatus()), fmt.Sprintf("%dms", address.GetElapsedMs()), address.GetError()})
		}
		writeDisplayTable(b, []displayTableColumn{{header: "FAMILY"}, {header: "IP"}, {header: "GLOBAL"}, {header: "STATUS"}, {header: "ELAPSED"}, {header: "ERROR"}}, rows, view.Width)
	case *controlpb.CommandResult_ResolveDns:
		p := payload.ResolveDns
		field("Name", p.GetName())
		field("Error", p.GetError())
		var rows [][]string
		for _, answer := range p.GetAnswers() {
			rows = append(rows, []string{dnsTypeName(answer.GetType()), answer.GetAddress()})
		}
		writeDisplayTable(b, []displayTableColumn{{header: "TYPE"}, {header: "ADDRESS"}}, rows, view.Width)
	case *controlpb.CommandResult_HttpCheck:
		p := payload.HttpCheck
		field("URL", p.GetUrl())
		field("Status", p.GetStatus())
		field("Expected status", p.GetExpectedStatus())
		field("Matched", p.GetMatched())
		field("Error", p.GetError())
	case *controlpb.CommandResult_Wget:
		p := payload.Wget
		field("URL", p.GetUrl())
		field("Status", p.GetStatus())
		field("Bytes read", p.GetBytesRead())
		field("Content length", p.GetContentLength())
		field("Content type", p.GetContentType())
		field("Throughput (bps)", fmt.Sprintf("%.0f", p.GetThroughputBps()))
		field("Interface", p.GetInterfaceName())
		field("Error", p.GetError())
	default:
		return false
	}
	return true
}
