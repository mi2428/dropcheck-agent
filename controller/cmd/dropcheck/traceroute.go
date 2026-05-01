package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"dropcheck/controller/internal/controlpb"
)

type tracerouteAnalysis struct {
	Status              string          `json:"status"`
	Message             string          `json:"message"`
	AgentStatus         string          `json:"agent_status"`
	Host                string          `json:"host"`
	MaxHops             uint32          `json:"max_hops"`
	SizeBytes           uint32          `json:"size_bytes,omitempty"`
	ElapsedMs           int64           `json:"elapsed_ms"`
	ExitCode            int32           `json:"exit_code"`
	InterfaceName       string          `json:"interface_name,omitempty"`
	Executable          string          `json:"executable,omitempty"`
	Hops                []tracerouteHop `json:"hops"`
	RequiredHops        []string        `json:"required_hops,omitempty"`
	MatchedRequiredHops []string        `json:"matched_required_hops,omitempty"`
	MissingRequiredHops []string        `json:"missing_required_hops,omitempty"`
	RequiredHopsPassed  bool            `json:"required_hops_passed"`
	ReachedTarget       bool            `json:"reached_target"`
	Error               string          `json:"error,omitempty"`
}

type tracerouteHop struct {
	Index         uint32    `json:"index"`
	Host          string    `json:"host,omitempty"`
	Address       string    `json:"address,omitempty"`
	RttMs         []float64 `json:"rtt_ms,omitempty"`
	TimedOut      bool      `json:"timed_out,omitempty"`
	ReachedTarget bool      `json:"reached_target,omitempty"`
	Raw           string    `json:"raw,omitempty"`
	matchText     string
}

var (
	tracerouteHopLine    = regexp.MustCompile(`^\s*(\d{1,3})\s+(.+?)\s*$`)
	tracerouteRTT        = regexp.MustCompile(`(?i)(?:time=)?(\d+(?:\.\d+)?)\s*ms`)
	tracerouteHostAddr   = regexp.MustCompile(`^([^\s()]+)\s+\(([^)]+)\)`)
	tracerouteAttachedIP = regexp.MustCompile(`^([^()]+)\(([^)]+)\)`)
)

func printCommandAnalysis(commandID string, agent string, result *controlpb.CommandResult, options commandOptions) {
	if ping := result.GetPing(); ping != nil {
		analysis := analyzePing(ping, result.GetStatus())
		printAnalysisJSON(commandID, agent, map[string]pingAnalysis{"ping": analysis})
	}
	trace := result.GetTraceroute()
	if trace == nil {
		return
	}
	analysis := analyzeTraceroute(trace, options.tracerouteRequiredHops, result.GetStatus())
	printAnalysisJSON(commandID, agent, map[string]tracerouteAnalysis{"traceroute": analysis})
}

func printAnalysisJSON(commandID string, agent string, value any) {
	fmt.Printf("analysis agent=%s command_id=%s\n", agent, commandID)
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		fmt.Printf("{\"error\":\"marshal analysis: %s\"}\n", err)
		return
	}
	fmt.Println(string(data))
}

func analyzeTraceroute(result *controlpb.TracerouteResult, requiredHops []string, agentStatus controlpb.CommandResult_Status) tracerouteAnalysis {
	hops := parseTracerouteOutput(result.GetOutput(), result.GetHost())
	matched := make([]string, 0, len(requiredHops))
	for _, required := range requiredHops {
		if tracerouteContainsHop(hops, required) {
			matched = append(matched, required)
		}
	}
	missing := make([]string, 0, len(requiredHops)-len(matched))
	for _, required := range requiredHops {
		if !containsString(matched, required) {
			missing = append(missing, required)
		}
	}
	requiredPassed := len(missing) == 0
	reached := false
	for _, hop := range hops {
		if hop.ReachedTarget {
			reached = true
			break
		}
	}
	status := "ok"
	message := "traceroute analysis passed"
	if agentStatus != controlpb.CommandResult_STATUS_OK {
		status = "failed"
		message = "agent traceroute failed"
	} else if !requiredPassed {
		status = "failed"
		message = "traceroute missing required hop"
	}
	return tracerouteAnalysis{
		Status:              status,
		Message:             message,
		AgentStatus:         resultStatus(agentStatus),
		Host:                result.GetHost(),
		MaxHops:             result.GetMaxHops(),
		SizeBytes:           result.GetSizeBytes(),
		ElapsedMs:           result.GetElapsedMs(),
		ExitCode:            result.GetExitCode(),
		InterfaceName:       result.GetInterfaceName(),
		Executable:          result.GetExecutable(),
		Hops:                hops,
		RequiredHops:        requiredHops,
		MatchedRequiredHops: matched,
		MissingRequiredHops: missing,
		RequiredHopsPassed:  requiredPassed,
		ReachedTarget:       reached,
		Error:               result.GetError(),
	}
}

func parseTracerouteOutput(output string, target string) []tracerouteHop {
	return parseNativeTracerouteOutput(output, target)
}

func parseNativeTracerouteOutput(output string, target string) []tracerouteHop {
	var hops []tracerouteHop
	for _, line := range strings.Split(output, "\n") {
		match := tracerouteHopLine.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		index := parseHopIndex(match[1])
		if index == 0 {
			continue
		}
		body := strings.TrimSpace(match[2])
		if strings.HasPrefix(body, "/") && strings.Contains(body, "ping") {
			continue
		}
		if strings.HasPrefix(strings.ToLower(body), "traceroute") {
			continue
		}
		rtts := parseTracerouteRTTs(body)
		identity := tracerouteRTT.ReplaceAllString(body, " ")
		identity = regexp.MustCompile(`!\S+`).ReplaceAllString(identity, " ")
		identity = strings.TrimSpace(strings.ReplaceAll(identity, "*", " "))
		host, address := splitTraceHostAddress(identity)
		timedOut := host == "" && address == ""
		hops = append(hops, tracerouteHop{
			Index:         index,
			Host:          host,
			Address:       address,
			RttMs:         rtts,
			TimedOut:      timedOut,
			ReachedTarget: !timedOut && traceHopMatchesFields(host, address, line, target),
			Raw:           line,
			matchText:     line,
		})
	}
	return hops
}

func parseHopIndex(value string) uint32 {
	parsed, err := strconv.ParseUint(value, 10, 32)
	if err != nil {
		return 0
	}
	return uint32(parsed)
}

func parseTracerouteRTTs(value string) []float64 {
	matches := tracerouteRTT.FindAllStringSubmatch(value, -1)
	rtts := make([]float64, 0, len(matches))
	for _, match := range matches {
		parsed, err := strconv.ParseFloat(match[1], 64)
		if err == nil {
			rtts = append(rtts, parsed)
		}
	}
	return rtts
}

func tracerouteContainsHop(hops []tracerouteHop, value string) bool {
	for _, hop := range hops {
		if traceHopMatches(hop, value) {
			return true
		}
	}
	return false
}

func traceHopMatches(hop tracerouteHop, value string) bool {
	return traceHopMatchesFields(hop.Host, hop.Address, hop.matchText, value)
}

func traceHopMatchesFields(host string, address string, text string, value string) bool {
	needle := strings.ToLower(strings.TrimSpace(value))
	if needle == "" {
		return false
	}
	return strings.ToLower(host) == needle ||
		strings.ToLower(address) == needle ||
		strings.Contains(strings.ToLower(text), needle)
}

func splitTraceHostAddress(value string) (string, string) {
	cleaned := strings.Trim(strings.TrimSpace(value), ",;:")
	if cleaned == "" {
		return "", ""
	}
	if match := tracerouteHostAddr.FindStringSubmatch(cleaned); match != nil {
		return match[1], match[2]
	}
	if match := tracerouteAttachedIP.FindStringSubmatch(cleaned); match != nil {
		return strings.TrimSpace(match[1]), strings.TrimSpace(match[2])
	}
	token := strings.Trim(strings.Fields(cleaned)[0], ",;:")
	if isLikelyAddress(token) {
		return "", token
	}
	return token, ""
}

func isLikelyAddress(value string) bool {
	if strings.Count(value, ".") == 3 {
		for _, r := range value {
			if (r < '0' || r > '9') && r != '.' {
				return false
			}
		}
		return true
	}
	if !strings.Contains(value, ":") {
		return false
	}
	hasDigit := false
	for _, r := range strings.ToLower(value) {
		if r >= '0' && r <= '9' {
			hasDigit = true
		}
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || r == ':' || r == '%') {
			return false
		}
	}
	return hasDigit
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
