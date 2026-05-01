package main

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

func shortID(value string) string {
	if len(value) <= 12 {
		return value
	}
	return value[:12]
}

func empty(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
