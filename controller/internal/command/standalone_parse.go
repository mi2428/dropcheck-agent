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
		return nil, fmt.Errorf("usage: set standalone <enabled|disabled|retention|max-size|festa>")
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
	case "festa":
		if len(args) < 2 {
			return nil, fmt.Errorf("usage: delete standalone festa <name> [wifi-group <name>|check <dns|ping|http>]")
		}
		festa := args[1]
		if len(args) == 2 {
			return []StandaloneEdit{StandaloneDeleteEdit([]string{"festa", festa})}, nil
		}
		switch args[2] {
		case "wifi-group":
			if len(args) != 4 {
				return nil, fmt.Errorf("usage: delete standalone festa <name> wifi-group <name>")
			}
			return []StandaloneEdit{StandaloneDeleteEdit([]string{"festa", festa, "wifi-group", args[3]})}, nil
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

func standaloneSetFestaEdits(args []string) ([]StandaloneEdit, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("usage: set standalone festa <name> <enabled|disabled|interval|wifi-group|check>")
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
	case "wifi-group":
		return standaloneSetWifiGroupEdits(festa, args[2:])
	case "check":
		return standaloneSetCheckEdits(festa, args[2:])
	default:
		return nil, fmt.Errorf("unknown set standalone festa command %q", args[1])
	}
}

func standaloneSetWifiGroupEdits(festa string, args []string) ([]StandaloneEdit, error) {
	if len(args) < 3 {
		return nil, fmt.Errorf("usage: set standalone festa <name> wifi-group <name> <match|credential|security|band|wait|timeout>")
	}
	group := args[0]
	base := []string{"festa", festa, "wifi-group", group}
	switch args[1] {
	case "match":
		if len(args) != 4 || (args[2] != "essid" && args[2] != "bssid") {
			return nil, fmt.Errorf("usage: set standalone festa <name> wifi-group <name> match <essid|bssid> <value>")
		}
		return []StandaloneEdit{StandaloneSetStringEdit(appendPath(base, "match", args[2]), args[3])}, nil
	case "credential":
		if len(args) != 4 || args[2] != "passphrase" {
			return nil, fmt.Errorf("usage: set standalone festa <name> wifi-group <name> credential passphrase <value>")
		}
		return []StandaloneEdit{StandaloneSetStringEdit(appendPath(base, "credential", "passphrase"), args[3])}, nil
	case "security":
		if len(args) != 3 {
			return nil, fmt.Errorf("usage: set standalone festa <name> wifi-group <name> security <auto|wpa2|wpa3|transition>")
		}
		security, err := normalizeStandaloneSecurity(args[2])
		if err != nil {
			return nil, err
		}
		return []StandaloneEdit{StandaloneSetStringEdit(appendPath(base, "security"), security)}, nil
	case "band":
		if len(args) != 3 {
			return nil, fmt.Errorf("usage: set standalone festa <name> wifi-group <name> band <all|2.4ghz|5ghz|6ghz|60ghz>")
		}
		band, err := normalizeStandaloneBand(args[2])
		if err != nil {
			return nil, err
		}
		return []StandaloneEdit{StandaloneSetStringEdit(appendPath(base, "band"), band)}, nil
	case "wait":
		if len(args) != 3 || (args[2] != "ip" && args[2] != "validated") {
			return nil, fmt.Errorf("usage: set standalone festa <name> wifi-group <name> wait <ip|validated>")
		}
		return []StandaloneEdit{StandaloneSetBoolEdit(appendPath(base, "wait", args[2]), true)}, nil
	case "timeout":
		if len(args) != 3 {
			return nil, fmt.Errorf("usage: set standalone festa <name> wifi-group <name> timeout <duration>")
		}
		edit, err := StandaloneSetMillisEdit(appendPath(base, "timeout_ms"), args[2], 35*time.Second)
		return singleEdit(edit, err)
	default:
		return nil, fmt.Errorf("unknown set standalone wifi-group command %q", args[1])
	}
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
		return nil, fmt.Errorf("standalone check options must be key/value pairs")
	}
	values := map[string]string{}
	for i := 0; i < len(args); i += 2 {
		key := args[i]
		if !allowed[key] {
			return nil, fmt.Errorf("unknown standalone check option %q", key)
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
