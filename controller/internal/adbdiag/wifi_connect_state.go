package adbdiag

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"dropcheck/controller/internal/adb"
)

var (
	wifiConnectStateFieldREs = map[string]*regexp.Regexp{
		"bssid":      regexp.MustCompile(`(?i)\bBSSID:\s*([^,\s]+)`),
		"ip":         regexp.MustCompile(`(?i)\bIP:\s*([^,]+)`),
		"ssid":       regexp.MustCompile(`(?i)\bSSID:\s*"([^"]*)"`),
		"supplicant": regexp.MustCompile(`(?i)\bSupplicant state:\s*([^,]+)`),
	}
	wifiSupplicantEventRE = regexp.MustCompile(`supplicantStateChangeEvents:\s*\{\s*([^}]*)\}`)
)

const wifiConnectEventShell = `dumpsys wifi | grep -E 'supplicantStateChangeEvents|WifiInfo:|Supplicant state:' | tail -n 40`

// WifiConnectState is the compact Wi-Fi association phase shown by the watch TUI.
type WifiConnectState struct {
	Supplicant string
	SSID       string
	BSSID      string
	IP         string
}

// CollectWifiConnectState reads Android's current supplicant phase through adb.
func CollectWifiConnectState(ctx context.Context, client adb.Client) WifiConnectState {
	if state := CollectWifiConnectStatusState(ctx, client); state.Supplicant != "" {
		return state
	}
	return CollectWifiConnectEventState(ctx, client)
}

// CollectWifiConnectEventState reads the recent supplicant event history from dumpsys wifi.
func CollectWifiConnectEventState(ctx context.Context, client adb.Client) WifiConnectState {
	out, err := client.Output(ctx, "shell", wifiConnectEventShell)
	if err != nil && strings.TrimSpace(out) == "" {
		return WifiConnectState{}
	}
	return ParseWifiConnectEventState(out)
}

// CollectWifiConnectStatusState reads the lightweight current Wi-Fi status.
func CollectWifiConnectStatusState(ctx context.Context, client adb.Client) WifiConnectState {
	out, err := client.Output(ctx, "shell", "cmd", "wifi", "status")
	if err != nil && strings.TrimSpace(out) == "" {
		return WifiConnectState{}
	}
	return ParseWifiConnectStatusState(out)
}

// ParseWifiConnectState extracts a supplicant phase from either event history or current status output.
func ParseWifiConnectState(text string) WifiConnectState {
	if state := ParseWifiConnectEventState(text); state.Supplicant != "" {
		return state
	}
	return ParseWifiConnectStatusState(text)
}

// ParseWifiConnectEventState extracts the newest supplicant phase from event history.
func ParseWifiConnectEventState(text string) WifiConnectState {
	supplicant := wifiConnectStateEventSupplicant(text)
	if supplicant == "" {
		return WifiConnectState{}
	}
	return normalizeWifiConnectState(WifiConnectState{
		Supplicant: supplicant,
		SSID:       wifiConnectStateField(text, "ssid"),
		BSSID:      wifiConnectStateField(text, "bssid"),
		IP:         strings.TrimPrefix(wifiConnectStateField(text, "ip"), "/"),
	})
}

// ParseWifiConnectStatusState extracts the current supplicant phase from cmd wifi status.
func ParseWifiConnectStatusState(text string) WifiConnectState {
	state := WifiConnectState{
		Supplicant: wifiConnectStateField(text, "supplicant"),
		SSID:       wifiConnectStateField(text, "ssid"),
		BSSID:      wifiConnectStateField(text, "bssid"),
		IP:         strings.TrimPrefix(wifiConnectStateField(text, "ip"), "/"),
	}
	return normalizeWifiConnectState(state)
}

func normalizeWifiConnectState(state WifiConnectState) WifiConnectState {
	if strings.EqualFold(state.BSSID, "null") || strings.EqualFold(state.BSSID, "<none>") {
		state.BSSID = ""
	}
	if strings.EqualFold(state.IP, "null") || strings.EqualFold(state.IP, "<none>") {
		state.IP = ""
	}
	return state
}

// LogMessage renders state as stable key=value text for watch events.
func (state WifiConnectState) LogMessage() string {
	if state.Supplicant == "" {
		return ""
	}
	fields := []string{"wifi connect state:"}
	fields = append(fields, "supplicant="+state.Supplicant)
	appendConnectStateField(&fields, "ssid", state.SSID)
	appendConnectStateField(&fields, "bssid", state.BSSID)
	appendConnectStateField(&fields, "ip", state.IP)
	return strings.Join(fields, " ")
}

func wifiConnectStateField(text string, name string) string {
	re, ok := wifiConnectStateFieldREs[name]
	if !ok {
		return ""
	}
	matches := re.FindStringSubmatch(text)
	if len(matches) < 2 {
		return ""
	}
	return strings.TrimSpace(matches[1])
}

func wifiConnectStateEventSupplicant(text string) string {
	var latest string
	for _, match := range wifiSupplicantEventRE.FindAllStringSubmatch(text, -1) {
		if len(match) < 2 {
			continue
		}
		fields := strings.Fields(match[1])
		if len(fields) > 0 {
			latest = fields[len(fields)-1]
		}
	}
	return latest
}

func appendConnectStateField(fields *[]string, key string, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	if strings.ContainsAny(value, " \t\r\n\"") {
		value = fmt.Sprintf("%q", value)
	}
	*fields = append(*fields, key+"="+value)
}
