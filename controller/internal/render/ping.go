package render

import (
	"regexp"
	"strconv"

	"dropcheck/controller/internal/controlpb"
)

type pingAnalysis struct {
	Status            string  `json:"status"`
	Message           string  `json:"message"`
	AgentStatus       string  `json:"agent_status"`
	Host              string  `json:"host"`
	Count             uint32  `json:"count"`
	SizeBytes         uint32  `json:"size_bytes,omitempty"`
	Transmitted       uint32  `json:"transmitted,omitempty"`
	Received          uint32  `json:"received,omitempty"`
	PacketLossPercent float64 `json:"packet_loss_percent,omitempty"`
	MinMs             float64 `json:"min_ms,omitempty"`
	AvgMs             float64 `json:"avg_ms,omitempty"`
	MaxMs             float64 `json:"max_ms,omitempty"`
	ElapsedMs         int64   `json:"elapsed_ms"`
	ExitCode          int32   `json:"exit_code"`
	InterfaceName     string  `json:"interface_name,omitempty"`
	Parsed            bool    `json:"parsed"`
}

type pingStats struct {
	transmitted uint32
	received    uint32
	lossPercent float64
	minMs       float64
	avgMs       float64
	maxMs       float64
}

var (
	pingSummary = regexp.MustCompile(`(?m)(\d+)\s+packets transmitted,\s+(\d+)\s+(?:packets\s+)?received,.*?([0-9.]+)%\s+packet loss`)
	pingRTT     = regexp.MustCompile(`(?m)(?:rtt|round-trip) min/avg/max/(?:mdev|stddev) = ([0-9.]+)/([0-9.]+)/([0-9.]+)/[0-9.]+ ms`)
)

func analyzePing(result *controlpb.PingResult, agentStatus controlpb.CommandResult_Status) pingAnalysis {
	stats, parsed := parsePingOutput(result.GetOutput())
	status := "ok"
	message := "ping analysis passed"
	if agentStatus != controlpb.CommandResult_STATUS_OK {
		status = "failed"
		message = "agent ping failed"
	} else if parsed && stats.received == 0 {
		// Some ping implementations return a command-level success even when all
		// probes are lost. Treat the parsed packet summary as the user-visible
		// source of truth when it is available.
		status = "failed"
		message = "ping received no replies"
	}
	analysis := pingAnalysis{
		Status:        status,
		Message:       message,
		AgentStatus:   resultStatus(agentStatus),
		Host:          result.GetHost(),
		Count:         result.GetCount(),
		SizeBytes:     result.GetSizeBytes(),
		ElapsedMs:     result.GetElapsedMs(),
		ExitCode:      result.GetExitCode(),
		InterfaceName: result.GetInterfaceName(),
		Parsed:        parsed,
	}
	if parsed {
		analysis.Transmitted = stats.transmitted
		analysis.Received = stats.received
		analysis.PacketLossPercent = stats.lossPercent
		analysis.MinMs = stats.minMs
		analysis.AvgMs = stats.avgMs
		analysis.MaxMs = stats.maxMs
	}
	return analysis
}

func parsePingOutput(output string) (pingStats, bool) {
	summary := pingSummary.FindStringSubmatch(output)
	if summary == nil {
		return pingStats{}, false
	}
	// Android devices may expose either toybox or iputils ping. The summary
	// regex captures the common packet-loss line and the RTT regex is optional
	// because failed runs often omit timing statistics.
	stats := pingStats{
		transmitted: parseUint32Default(summary[1]),
		received:    parseUint32Default(summary[2]),
		lossPercent: parseFloatDefault(summary[3]),
	}
	if rtt := pingRTT.FindStringSubmatch(output); rtt != nil {
		stats.minMs = parseFloatDefault(rtt[1])
		stats.avgMs = parseFloatDefault(rtt[2])
		stats.maxMs = parseFloatDefault(rtt[3])
	}
	return stats, true
}

func parseUint32Default(value string) uint32 {
	parsed, err := strconv.ParseUint(value, 10, 32)
	if err != nil {
		return 0
	}
	return uint32(parsed)
}

func parseFloatDefault(value string) float64 {
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0
	}
	return parsed
}
