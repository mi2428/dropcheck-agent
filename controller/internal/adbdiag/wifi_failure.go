package adbdiag

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"dropcheck/controller/internal/adb"
)

var (
	wifiStatusRE = regexp.MustCompile(`(?i)\bstatus(?:Code)?\s*(?:=|:)\s*([0-9]+)(?::([A-Z0-9_]+))?`)
	wifiFieldREs = map[string]*regexp.Regexp{
		"bssid":      regexp.MustCompile(`(?i)\bbssid\s*(?:=|:)\s*("[^"]*"|[^,\s]+)`),
		"durationMs": regexp.MustCompile(`(?i)\bdurationMs\s*(?:=|:)\s*("[^"]*"|[^,\s]+)`),
		"reason":     regexp.MustCompile(`(?i)\breason\s*(?:=|:)\s*("[^"]*"|[^,\s]+)`),
		"ssid":       regexp.MustCompile(`(?i)\bssid\s*(?:=|:)\s*("[^"]*"|[^,]+)`),
		"timedOut":   regexp.MustCompile(`(?i)\btimedOut\s*(?:=|:)\s*("[^"]*"|[^,\s]+)`),
	}
)

// CollectWifiFailureCause returns the most useful recent Wi-Fi failure cause
// visible from Android's host-side diagnostics.
func CollectWifiFailureCause(ctx context.Context, client adb.Client) string {
	out, err := client.Output(ctx, "shell", "dumpsys", "wifi")
	if err != nil && strings.TrimSpace(out) == "" {
		return ""
	}
	return ParseWifiFailureCause(out)
}

// ParseWifiFailureCause extracts compact cause text from dumpsys wifi output.
func ParseWifiFailureCause(text string) string {
	var association string
	var authentication string
	var blocklist string
	var disconnect string
	for raw := range strings.SplitSeq(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		upper := strings.ToUpper(line)
		switch {
		case strings.Contains(upper, "ASSOCIATION_REJECTION_EVENT"):
			association = renderAssociationRejection(line)
		case strings.Contains(upper, "AUTHENTICATION_FAILURE_EVENT"):
			authentication = renderAuthenticationFailure(line)
		case strings.Contains(line, "WifiBlocklistMonitor") && strings.Contains(line, "addToBlocklist"):
			blocklist = renderBlocklist(line)
		case strings.Contains(upper, "NETWORK_DISCONNECTION_EVENT"):
			disconnect = renderDisconnect(line)
		}
	}
	parts := make([]string, 0, 3)
	if association != "" {
		parts = append(parts, association)
	}
	if authentication != "" {
		parts = append(parts, authentication)
	}
	if blocklist != "" {
		parts = append(parts, blocklist)
	}
	if len(parts) == 0 && disconnect != "" {
		parts = append(parts, disconnect)
	}
	return strings.Join(parts, "; ")
}

func renderAssociationRejection(line string) string {
	fields := []string{"association rejected"}
	status, reason := wifiStatus(line)
	appendField(&fields, "status", status)
	appendField(&fields, "reason", reason)
	appendField(&fields, "timed_out", wifiField(line, "timedOut"))
	appendField(&fields, "bssid", wifiField(line, "bssid"))
	appendField(&fields, "ssid", wifiField(line, "ssid"))
	return strings.Join(fields, " ")
}

func renderAuthenticationFailure(line string) string {
	fields := []string{"authentication failed"}
	appendField(&fields, "reason", wifiField(line, "reason"))
	appendField(&fields, "bssid", wifiField(line, "bssid"))
	appendField(&fields, "ssid", wifiField(line, "ssid"))
	return strings.Join(fields, " ")
}

func renderBlocklist(line string) string {
	fields := []string{"android blocklisted"}
	appendField(&fields, "reason", wifiField(line, "reason"))
	appendField(&fields, "duration_ms", wifiField(line, "durationMs"))
	appendField(&fields, "bssid", wifiField(line, "bssid"))
	appendField(&fields, "ssid", wifiField(line, "ssid"))
	return strings.Join(fields, " ")
}

func renderDisconnect(line string) string {
	fields := []string{"disconnected"}
	appendField(&fields, "reason", wifiField(line, "reason"))
	appendField(&fields, "bssid", wifiField(line, "bssid"))
	appendField(&fields, "ssid", wifiField(line, "ssid"))
	return strings.Join(fields, " ")
}

func wifiStatus(line string) (string, string) {
	matches := wifiStatusRE.FindStringSubmatch(line)
	if len(matches) == 0 {
		return "", ""
	}
	status := matches[1]
	reason := ""
	if len(matches) > 2 {
		reason = matches[2]
	}
	return status, reason
}

func wifiField(line string, name string) string {
	re, ok := wifiFieldREs[name]
	if !ok {
		return ""
	}
	matches := re.FindStringSubmatch(line)
	if len(matches) < 2 {
		return ""
	}
	value := strings.TrimSpace(matches[1])
	value = strings.Trim(value, `"'`)
	return strings.TrimSpace(value)
}

func appendField(fields *[]string, key string, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	if strings.ContainsAny(value, " \t\r\n") {
		value = strconv.Quote(value)
	}
	*fields = append(*fields, key+"="+value)
}
