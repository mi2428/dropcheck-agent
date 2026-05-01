package render

import (
	"fmt"
	"strings"

	"dropcheck/controller/internal/control"
	"dropcheck/controller/internal/controlpb"
)

func resultStatus(status controlpb.CommandResult_Status) string {
	return strings.ToLower(strings.TrimPrefix(status.String(), "STATUS_"))
}

func dnsTypeName(t controlpb.DnsRecordType) string {
	return strings.TrimPrefix(t.String(), "DNS_RECORD_TYPE_")
}

func formatMillis(values []float64) string {
	if len(values) == 0 {
		return "-"
	}
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, fmt.Sprintf("%.2fms", value))
	}
	return strings.Join(parts, "/")
}

func agentDisplayName(info control.AgentInfo) string {
	if serial := info.Hello.GetAdbSerial(); serial != "" {
		return serial
	}
	if info.ID != "" {
		return shortID(info.ID)
	}
	return shortID(info.SessionID)
}

// AgentDisplayName returns the most useful short label for an agent.
//
// ADB serial is preferred for user-facing output. If it is unavailable, the
// function falls back to a shortened agent ID or session ID.
func AgentDisplayName(info control.AgentInfo) string {
	return agentDisplayName(info)
}

func shortID(value string) string {
	if len(value) <= 12 {
		return value
	}
	return value[:12]
}

// ShortID returns value shortened to the display prefix length.
//
// Values at or below the display length are returned unchanged.
func ShortID(value string) string {
	return shortID(value)
}

func empty(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

// Empty returns fallback when value is empty.
func Empty(value, fallback string) string {
	return empty(value, fallback)
}
