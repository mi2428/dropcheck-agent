package command

import (
	"fmt"
	"strings"
	"time"
)

const defaultStandaloneFestaInterval = 30 * time.Second

// StandaloneSetEdits parses tokens after "set standalone".
func StandaloneSetEdits(args []string) ([]StandaloneEdit, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("usage: set standalone <enabled|disabled|retention|max-size|upload|festa>")
	}
	switch args[0] {
	case "enabled":
		if len(args) != 1 {
			return nil, fmt.Errorf("usage: set standalone enabled")
		}
		return []StandaloneEdit{StandaloneSetBoolEdit([]string{"enabled"}, true)}, nil
	case "disabled":
		if len(args) != 1 {
			return nil, fmt.Errorf("usage: set standalone disabled")
		}
		return []StandaloneEdit{StandaloneSetBoolEdit([]string{"enabled"}, false)}, nil
	case "retention":
		if len(args) != 2 {
			return nil, fmt.Errorf("usage: set standalone retention <duration>")
		}
		edit, err := StandaloneSetMillisEdit([]string{"retention_ms"}, args[1], DefaultStandaloneRetention())
		return singleEdit(edit, err)
	case "max-size":
		if len(args) != 2 {
			return nil, fmt.Errorf("usage: set standalone max-size <bytes>")
		}
		edit, err := StandaloneSetBytesEdit([]string{"max_bytes"}, args[1], DefaultStandaloneMaxBytes())
		return singleEdit(edit, err)
	case "upload":
		return standaloneSetUploadEdits(args[1:])
	case "festa":
		return standaloneSetFestaEdits(args[1:])
	default:
		return nil, fmt.Errorf("unknown set standalone command %q", args[0])
	}
}

// StandaloneDeleteEdits parses tokens after "delete standalone".
func StandaloneDeleteEdits(args []string) ([]StandaloneEdit, error) {
	if len(args) == 0 {
		return []StandaloneEdit{StandaloneDeleteEdit([]string{"standalone"})}, nil
	}
	switch args[0] {
	case "enabled", "retention", "max-size":
		if len(args) != 1 {
			return nil, fmt.Errorf("usage: delete standalone %s", args[0])
		}
		path := args[0]
		if path == "retention" {
			path = "retention_ms"
		}
		if path == "max-size" {
			path = "max_bytes"
		}
		return []StandaloneEdit{StandaloneDeleteEdit([]string{path})}, nil
	case "upload":
		return standaloneDeleteUploadEdits(args[1:])
	case "festa":
		if len(args) < 2 {
			return nil, fmt.Errorf("usage: delete standalone festa <name> [wifi <name>|check <dns|ping|http>]")
		}
		festa := args[1]
		if len(args) == 2 {
			return []StandaloneEdit{StandaloneDeleteEdit([]string{"festa", festa})}, nil
		}
		switch args[2] {
		case "wifi":
			if len(args) != 4 {
				return nil, fmt.Errorf("usage: delete standalone festa <name> wifi <name>")
			}
			return []StandaloneEdit{StandaloneDeleteEdit([]string{"festa", festa, "wifi", args[3]})}, nil
		case "check":
			if len(args) != 4 || !standaloneCheckType(args[3]) {
				return nil, fmt.Errorf("usage: delete standalone festa <name> check <dns|ping|http>")
			}
			return []StandaloneEdit{StandaloneDeleteEdit([]string{"festa", festa, "check", args[3]})}, nil
		default:
			return nil, fmt.Errorf("unknown delete standalone festa command %q", args[2])
		}
	default:
		return nil, fmt.Errorf("unknown delete standalone command %q", args[0])
	}
}

func standaloneSetUploadEdits(args []string) ([]StandaloneEdit, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("usage: set standalone upload <to|via wifi>")
	}
	switch args[0] {
	case "to":
		if len(args) != 2 {
			return nil, fmt.Errorf("usage: set standalone upload to <url>")
		}
		return []StandaloneEdit{StandaloneSetStringEdit([]string{"upload", "url"}, args[1])}, nil
	case "via":
		if len(args) < 2 || args[1] != "wifi" {
			return nil, fmt.Errorf("usage: set standalone upload via wifi essid <ssid> passphrase <passphrase> [security <auto|wpa2|wpa3|transition>] [bssid <bssid>] [band <all|2.4ghz|5ghz|6ghz|60ghz>] [mac-randomization <auto|none|persistent|non-persistent>] [timeout <duration>]")
		}
		return standaloneSetUploadWifiEdits(args[2:])
	default:
		return nil, fmt.Errorf("unknown set standalone upload command %q", args[0])
	}
}

func standaloneSetUploadWifiEdits(args []string) ([]StandaloneEdit, error) {
	values, err := parseStandaloneKeyValues(args, map[string]bool{
		"essid":             true,
		"passphrase":        true,
		"security":          true,
		"bssid":             true,
		"band":              true,
		"mac-randomization": true,
		"timeout":           true,
	})
	if err != nil {
		return nil, err
	}
	if values["essid"] == "" {
		return nil, fmt.Errorf("set standalone upload via wifi requires essid <ssid>")
	}
	if values["passphrase"] == "" {
		return nil, fmt.Errorf("set standalone upload via wifi requires passphrase <passphrase>")
	}
	edits := []StandaloneEdit{
		StandaloneDeleteEdit([]string{"upload", "wifi"}),
		StandaloneSetStringEdit([]string{"upload", "wifi", "ssid"}, values["essid"]),
		StandaloneSetStringEdit([]string{"upload", "wifi", "passphrase"}, values["passphrase"]),
	}
	if values["security"] != "" {
		security, err := normalizeStandaloneSecurity(values["security"])
		if err != nil {
			return nil, err
		}
		edits = append(edits, StandaloneSetStringEdit([]string{"upload", "wifi", "security"}, security))
	}
	if values["bssid"] != "" {
		edits = append(edits, StandaloneSetStringEdit([]string{"upload", "wifi", "bssid"}, values["bssid"]))
	}
	if values["band"] != "" {
		band, err := normalizeStandaloneBand(values["band"])
		if err != nil {
			return nil, err
		}
		edits = append(edits, StandaloneSetStringEdit([]string{"upload", "wifi", "band"}, band))
	}
	if values["mac-randomization"] != "" {
		macRandomization, err := normalizeStandaloneMacRandomization(values["mac-randomization"])
		if err != nil {
			return nil, err
		}
		edits = append(edits, StandaloneSetStringEdit([]string{"upload", "wifi", "mac_randomization"}, macRandomization))
	}
	if values["timeout"] != "" {
		timeoutEdit, err := StandaloneSetMillisEdit([]string{"upload", "wifi", "timeout_ms"}, values["timeout"], 45*time.Second)
		if err != nil {
			return nil, err
		}
		edits = append(edits, timeoutEdit)
	}
	return edits, nil
}

func standaloneDeleteUploadEdits(args []string) ([]StandaloneEdit, error) {
	if len(args) == 0 {
		return []StandaloneEdit{StandaloneDeleteEdit([]string{"upload"})}, nil
	}
	switch args[0] {
	case "to":
		if len(args) != 1 {
			return nil, fmt.Errorf("usage: delete standalone upload to")
		}
		return []StandaloneEdit{StandaloneDeleteEdit([]string{"upload", "url"})}, nil
	case "via":
		if len(args) != 2 || args[1] != "wifi" {
			return nil, fmt.Errorf("usage: delete standalone upload via wifi")
		}
		return []StandaloneEdit{StandaloneDeleteEdit([]string{"upload", "wifi"})}, nil
	default:
		return nil, fmt.Errorf("unknown delete standalone upload command %q", args[0])
	}
}

func standaloneSetFestaEdits(args []string) ([]StandaloneEdit, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("usage: set standalone festa <name> <enabled|disabled|interval|wifi|check>")
	}
	festa := args[0]
	switch args[1] {
	case "enabled":
		if len(args) != 2 {
			return nil, fmt.Errorf("usage: set standalone festa <name> enabled")
		}
		return []StandaloneEdit{StandaloneSetBoolEdit([]string{"festa", festa, "enabled"}, true)}, nil
	case "disabled":
		if len(args) != 2 {
			return nil, fmt.Errorf("usage: set standalone festa <name> disabled")
		}
		return []StandaloneEdit{StandaloneSetBoolEdit([]string{"festa", festa, "enabled"}, false)}, nil
	case "interval":
		if len(args) != 3 {
			return nil, fmt.Errorf("usage: set standalone festa <name> interval <duration>")
		}
		edit, err := StandaloneSetMillisEdit([]string{"festa", festa, "interval_ms"}, args[2], defaultStandaloneFestaInterval)
		return singleEdit(edit, err)
	case "wifi":
		return standaloneSetWifiEdits(festa, args[2:])
	case "check":
		return standaloneSetCheckEdits(festa, args[2:])
	default:
		return nil, fmt.Errorf("unknown set standalone festa command %q", args[1])
	}
}

func standaloneSetWifiEdits(festa string, args []string) ([]StandaloneEdit, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("usage: set standalone festa <name> wifi <name> <match|passphrase|band|wait|timeout>")
	}
	group := args[0]
	base := []string{"festa", festa, "wifi", group}
	switch args[1] {
	case "match":
		return standaloneSetWifiMatchEdits(base, args[2:])
	case "passphrase":
		return standaloneSetWifiPassphraseEdits(base, args[2:])
	case "band":
		if len(args) != 3 {
			return nil, fmt.Errorf("usage: set standalone festa <name> wifi <name> band <all|2.4ghz|5ghz|6ghz|60ghz>")
		}
		band, err := normalizeStandaloneBand(args[2])
		if err != nil {
			return nil, err
		}
		return []StandaloneEdit{StandaloneSetStringEdit(appendPath(base, "band"), band)}, nil
	case "wait":
		if len(args) != 3 || (args[2] != "ip" && args[2] != "validated") {
			return nil, fmt.Errorf("usage: set standalone festa <name> wifi <name> wait <ip|validated>")
		}
		return []StandaloneEdit{StandaloneSetBoolEdit(appendPath(base, "wait", args[2]), true)}, nil
	case "timeout":
		if len(args) != 3 {
			return nil, fmt.Errorf("usage: set standalone festa <name> wifi <name> timeout <duration>")
		}
		edit, err := StandaloneSetMillisEdit(appendPath(base, "timeout_ms"), args[2], 35*time.Second)
		return singleEdit(edit, err)
	default:
		return nil, fmt.Errorf("unknown set standalone wifi command %q", args[1])
	}
}

func standaloneSetWifiMatchEdits(base []string, args []string) ([]StandaloneEdit, error) {
	if len(args) < 2 || (args[0] != "essid" && args[0] != "bssid") {
		return nil, fmt.Errorf("usage: set standalone festa <name> wifi <name> match <essid|bssid> <value> [mac-randomization <auto|none|persistent|non-persistent>]")
	}
	values, err := parseStandaloneKeyValues(args[2:], map[string]bool{"mac-randomization": true})
	if err != nil {
		return nil, err
	}
	edits := []StandaloneEdit{StandaloneSetStringEdit(appendPath(base, "match", args[0]), args[1])}
	if values["mac-randomization"] != "" {
		macRandomization, err := normalizeStandaloneMacRandomization(values["mac-randomization"])
		if err != nil {
			return nil, err
		}
		edits = append(edits, StandaloneSetStringEdit(appendPath(base, "mac_randomization"), macRandomization))
	}
	return edits, nil
}

func standaloneSetWifiPassphraseEdits(base []string, args []string) ([]StandaloneEdit, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("usage: set standalone festa <name> wifi <name> passphrase <passphrase> [security <auto|wpa2|wpa3|transition>]")
	}
	values, err := parseStandaloneKeyValues(args[1:], map[string]bool{"security": true})
	if err != nil {
		return nil, err
	}
	edits := []StandaloneEdit{StandaloneSetStringEdit(appendPath(base, "passphrase"), args[0])}
	if values["security"] != "" {
		security, err := normalizeStandaloneSecurity(values["security"])
		if err != nil {
			return nil, err
		}
		edits = append(edits, StandaloneSetStringEdit(appendPath(base, "security"), security))
	}
	return edits, nil
}

func standaloneSetCheckEdits(festa string, args []string) ([]StandaloneEdit, error) {
	if len(args) == 0 || !standaloneCheckType(args[0]) {
		return nil, fmt.Errorf("usage: set standalone festa <name> check <dns|ping|http>")
	}
	switch args[0] {
	case "dns":
		return standaloneSetDNSCheckEdits(festa, args[1:])
	case "ping":
		return standaloneSetPingCheckEdits(festa, args[1:])
	case "http":
		return standaloneSetHTTPCheckEdits(festa, args[1:])
	default:
		return nil, fmt.Errorf("unsupported standalone check %q", args[0])
	}
}

func standaloneSetDNSCheckEdits(festa string, args []string) ([]StandaloneEdit, error) {
	values, err := parseStandaloneKeyValues(args, map[string]bool{"name": true, "type": true, "timeout": true})
	if err != nil {
		return nil, err
	}
	if values["name"] == "" {
		return nil, fmt.Errorf("set standalone festa <name> check dns requires name <domain>")
	}
	qtype, err := normalizeDNSQType(values["type"])
	if err != nil {
		return nil, err
	}
	timeoutEdit, err := StandaloneSetMillisEdit([]string{"festa", festa, "check", "dns", "timeout_ms"}, values["timeout"], 10*time.Second)
	if err != nil {
		return nil, err
	}
	return []StandaloneEdit{
		StandaloneSetBoolEdit([]string{"festa", festa, "check", "dns", "enabled"}, true),
		StandaloneSetStringEdit([]string{"festa", festa, "check", "dns", "name"}, values["name"]),
		StandaloneSetStringEdit([]string{"festa", festa, "check", "dns", "qtypes"}, qtype),
		timeoutEdit,
	}, nil
}

func standaloneSetPingCheckEdits(festa string, args []string) ([]StandaloneEdit, error) {
	values, err := parseStandaloneKeyValues(args, map[string]bool{"host": true, "count": true, "timeout": true, "size": true})
	if err != nil {
		return nil, err
	}
	if values["host"] == "" {
		return nil, fmt.Errorf("set standalone festa <name> check ping requires host <host>")
	}
	count := values["count"]
	if count == "" {
		count = "3"
	}
	if _, err := parseUint32(count, "count"); err != nil {
		return nil, err
	}
	timeoutEdit, err := StandaloneSetMillisEdit([]string{"festa", festa, "check", "ping", "timeout_ms"}, values["timeout"], 10*time.Second)
	if err != nil {
		return nil, err
	}
	edits := []StandaloneEdit{
		StandaloneSetBoolEdit([]string{"festa", festa, "check", "ping", "enabled"}, true),
		StandaloneSetStringEdit([]string{"festa", festa, "check", "ping", "host"}, values["host"]),
		StandaloneSetStringEdit([]string{"festa", festa, "check", "ping", "count"}, count),
		timeoutEdit,
	}
	if values["size"] != "" {
		if _, err := parseUint32(values["size"], "size"); err != nil {
			return nil, err
		}
		edits = append(edits, StandaloneSetStringEdit([]string{"festa", festa, "check", "ping", "size_bytes"}, values["size"]))
	}
	return edits, nil
}

func standaloneSetHTTPCheckEdits(festa string, args []string) ([]StandaloneEdit, error) {
	values, err := parseStandaloneKeyValues(args, map[string]bool{"url": true, "expected-status": true, "timeout": true})
	if err != nil {
		return nil, err
	}
	if values["url"] == "" {
		return nil, fmt.Errorf("set standalone festa <name> check http requires url <url>")
	}
	status := values["expected-status"]
	if status == "" {
		status = "204"
	}
	if _, err := parseUint32(status, "expected-status"); err != nil {
		return nil, err
	}
	timeoutEdit, err := StandaloneSetMillisEdit([]string{"festa", festa, "check", "http", "timeout_ms"}, values["timeout"], 10*time.Second)
	if err != nil {
		return nil, err
	}
	return []StandaloneEdit{
		StandaloneSetBoolEdit([]string{"festa", festa, "check", "http", "enabled"}, true),
		StandaloneSetStringEdit([]string{"festa", festa, "check", "http", "url"}, values["url"]),
		StandaloneSetStringEdit([]string{"festa", festa, "check", "http", "expected_status"}, status),
		timeoutEdit,
	}, nil
}

func parseStandaloneKeyValues(args []string, allowed map[string]bool) (map[string]string, error) {
	if len(args)%2 != 0 {
		return nil, fmt.Errorf("standalone options must be key/value pairs")
	}
	values := map[string]string{}
	for i := 0; i < len(args); i += 2 {
		key := args[i]
		if !allowed[key] {
			return nil, fmt.Errorf("unknown standalone option %q", key)
		}
		if values[key] != "" {
			return nil, fmt.Errorf("%s specified twice", key)
		}
		values[key] = args[i+1]
	}
	return values, nil
}

func standaloneCheckType(value string) bool {
	return value == "dns" || value == "ping" || value == "http"
}

func normalizeStandaloneSecurity(value string) (string, error) {
	switch strings.ToLower(value) {
	case "", "auto":
		return "auto", nil
	case "wpa2", "wpa3", "transition":
		return strings.ToLower(value), nil
	default:
		return "", fmt.Errorf("unsupported wifi security %q", value)
	}
}

func normalizeStandaloneBand(value string) (string, error) {
	switch _, err := parseWifiBand(value); {
	case err != nil:
		return "", err
	case value == "":
		return "all", nil
	default:
		return strings.ToLower(value), nil
	}
}

func normalizeStandaloneMacRandomization(value string) (string, error) {
	switch _, err := parseMacRandomization(value); {
	case err != nil:
		return "", err
	case value == "":
		return "", nil
	default:
		return strings.ToLower(value), nil
	}
}

func singleEdit(edit StandaloneEdit, err error) ([]StandaloneEdit, error) {
	if err != nil {
		return nil, err
	}
	return []StandaloneEdit{edit}, nil
}

func appendPath(base []string, parts ...string) []string {
	path := append([]string(nil), base...)
	return append(path, parts...)
}
