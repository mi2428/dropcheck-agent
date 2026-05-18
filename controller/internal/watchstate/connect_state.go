package watchstate

import (
	"regexp"
	"strconv"
	"strings"

	"dropcheck/controller/internal/watch"
)

var connectStateFieldREs = map[string]*regexp.Regexp{
	"bssid":      regexp.MustCompile(`(?:^|\s)bssid=("[^"]*"|\S+)`),
	"ip":         regexp.MustCompile(`(?:^|\s)ip=("[^"]*"|\S+)`),
	"ssid":       regexp.MustCompile(`(?:^|\s)ssid=("[^"]*"|\S+)`),
	"supplicant": regexp.MustCompile(`(?:^|\s)supplicant=("[^"]*"|\S+)`),
}

// RecordConnectState updates the latest live association phase from an event log.
func (s *State) RecordConnectState(event watch.Event) {
	state, ok := ParseConnectStateEvent(event)
	if !ok {
		return
	}
	key := RoundAgentKey(state.Agent)
	for i := range s.ConnectStates {
		if RoundAgentKey(s.ConnectStates[i].Agent) == key {
			s.ConnectStates[i] = state
			return
		}
	}
	s.ConnectStates = append(s.ConnectStates, state)
}

// ParseConnectStateEvent extracts a ConnectState from watch's live Wi-Fi status log.
func ParseConnectStateEvent(event watch.Event) (ConnectState, bool) {
	message := strings.TrimSpace(event.Message)
	if !strings.HasPrefix(message, "wifi connect state:") {
		return ConnectState{}, false
	}
	supplicant := connectStateField(message, "supplicant")
	if supplicant == "" {
		return ConnectState{}, false
	}
	return ConnectState{
		Last:       EventTime(event),
		Agent:      event.Agent,
		Target:     event.Target,
		Supplicant: supplicant,
		SSID:       connectStateField(message, "ssid"),
		BSSID:      connectStateField(message, "bssid"),
		IP:         connectStateField(message, "ip"),
	}, true
}

func connectStateField(message string, key string) string {
	re, ok := connectStateFieldREs[key]
	if !ok {
		return ""
	}
	matches := re.FindStringSubmatch(message)
	if len(matches) < 2 {
		return ""
	}
	value := strings.TrimSpace(matches[1])
	if unquoted, err := strconv.Unquote(value); err == nil {
		return unquoted
	}
	return strings.Trim(value, `"`)
}
