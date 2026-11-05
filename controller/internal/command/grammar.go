package command

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
	"slices"
	"strings"
)

// Grammar describes Shell paths and their literal arguments.
type Grammar struct {
	Path        string
	Positionals []string
	Required    []string
	Switches    []string
	Values      []string
	Effect      string
	Description string
	Enums       map[string][]string
}

var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
var ShellGrammarTable = append(slices.Clone(portableGrammar), shellHostGrammar...)

// Parsed is a validated Shell action; local actions do not dispatch.
type Parsed struct {
	Path                         string
	Topic                        string
	Operation                    Operation
	ADBKind                      string
	Profile, SSID, Family, BSSID string
	Lab                          LabConfig
	Rejection                    string
	Passphrase                   string
	Effect                       string
	Connect                      *WifiConnectOptions
	ADBTimeoutMs                 uint32
}

// LabConfig contains only operator-supplied, non-credential endpoints.
type LabConfig struct {
	Gateway, DNS, Resolver, HTTP string
	ExpectedStatus, TimeoutMs    uint32
}

// ValidFor refuses implicit destinations and URL secrets before dispatch.
func (c LabConfig) ValidFor(family string) bool {
	ip := net.ParseIP(c.Gateway)
	u, err := url.Parse(c.HTTP)
	return (family == "ipv4" || family == "ipv6") && ip != nil && (ip.To4() != nil) == (family == "ipv4") &&
		c.DNS != "" && !strings.ContainsAny(c.DNS, " \t\r\n\x00") && !strings.HasPrefix(c.DNS, "-") &&
		c.Resolver == "selected" && err == nil && validateHTTPEndpoint(c.HTTP) == nil && u.RawQuery == "" && !u.ForceQuery && u.Fragment == "" &&
		c.ExpectedStatus >= 100 && c.ExpectedStatus <= 599 && c.TimeoutMs >= 1 && c.TimeoutMs <= 30000
}

func siblings(table []Grammar, prefix []string) []string {
	var out []string
	for _, spec := range table {
		path := strings.Fields(spec.Path)
		if len(path) > len(prefix) && slices.Equal(path[:len(prefix)], prefix) && !slices.Contains(out, path[len(prefix)]) {
			out = append(out, path[len(prefix)])
		}
	}
	return out
}

func resolve(word string, options []string) (string, error) {
	if word == "" {
		return "", fmt.Errorf("empty keyword")
	}
	if slices.Contains(options, word) {
		return word, nil
	}
	var matches []string
	for _, option := range options {
		if strings.HasPrefix(option, word) {
			matches = append(matches, option)
		}
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("unknown keyword")
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("ambiguous keyword (matches %s)", strings.Join(matches, ", "))
	}
}

func commandSpec(table []Grammar, args []string) (Grammar, int, error) {
	var path []string
	for i, word := range args {
		options := siblings(table, path)
		if len(options) == 0 {
			break
		}
		if spec, ok := exactSpec(table, path); ok && len(spec.Positionals) > 0 {
			return spec, i, nil
		}
		if _, ok := exactSpec(table, path); ok && !slices.ContainsFunc(options, func(option string) bool { return strings.HasPrefix(option, word) }) {
			break
		}
		resolved, err := resolve(word, options)
		if err != nil {
			return Grammar{}, 0, err
		}
		path = append(path, resolved)
	}
	if spec, ok := exactSpec(table, path); ok {
		return spec, len(path), nil
	}
	return Grammar{}, 0, fmt.Errorf("incomplete command (expected %s)", strings.Join(siblings(table, path), ", "))
}

func exactSpec(table []Grammar, path []string) (Grammar, bool) {
	for _, spec := range table {
		if spec.Path == strings.Join(path, " ") {
			return spec, true
		}
	}
	return Grammar{}, false
}

func parseFields(spec Grammar, args []string) (map[string]string, []string, error) {
	fields := make(map[string]string)
	pos := 0
	var via []string
	for i := 0; i < len(args); i++ {
		if pos < len(spec.Positionals) {
			fields[spec.Positionals[pos]] = args[i]
			pos++
			continue
		}
		key, err := resolve(args[i], append(slices.Clone(spec.Switches), spec.Values...))
		if err != nil {
			return nil, nil, err
		}
		if _, used := fields[key]; used && key != "via" {
			return nil, nil, fmt.Errorf("%s specified twice", key)
		}
		if slices.Contains(spec.Switches, key) {
			fields[key] = "true"
			continue
		}
		i++
		if i >= len(args) {
			return nil, nil, fmt.Errorf("%s requires a value", key)
		}
		if args[i] == "" {
			return nil, nil, fmt.Errorf("%s requires a nonempty value", key)
		}
		if key == "via" {
			via = append(via, args[i])
		} else {
			fields[key] = args[i]
		}
	}
	if pos < len(spec.Positionals) && spec.Path != "help" {
		return nil, nil, fmt.Errorf("%s requires %s", spec.Path, spec.Positionals[pos])
	}
	for _, key := range spec.Required {
		if fields[key] == "" {
			return nil, nil, fmt.Errorf("%s requires %s", spec.Path, key)
		}
	}
	return fields, via, nil
}

func ParseShellTokens(args []string) (Parsed, error) { return parseInteractiveTokens(args) }

func ShellSuggestions(args []string) []string {
	if len(args) > 0 && args[0] != "" && strings.HasPrefix("help", args[0]) {
		return shellPathSuggestions(args[1:])
	}
	if len(args) == 0 {
		return suggestedSiblings(ShellGrammarTable, nil)
	}
	spec, n, err := commandSpec(ShellGrammarTable, args)
	if err != nil {
		return shellPathSuggestions(args)
	}
	rest := args[n:]
	if len(rest) == 0 && len(spec.Positionals) > 0 {
		return []string{"<" + spec.Positionals[0] + ">"}
	}
	used := map[string]bool{}
	pos := 0
	for i := 0; i < len(rest); i++ {
		if pos < len(spec.Positionals) {
			pos++
			continue
		}
		key, err := resolve(rest[i], append(slices.Clone(spec.Switches), spec.Values...))
		if err != nil {
			return nil
		}
		used[key] = true
		if !slices.Contains(spec.Switches, key) {
			if i == len(rest)-1 {
				if values, ok := spec.Enums[key]; ok {
					return values
				}
				return []string{"<value>"}
			}
			i++
		}
	}
	if pos < len(spec.Positionals) {
		return []string{"<" + spec.Positionals[pos] + ">"}
	}
	var out []string
	if len(rest) == 0 {
		out = append(out, suggestedSiblings(ShellGrammarTable, strings.Fields(spec.Path))...)
	}
	for _, key := range append(slices.Clone(spec.Switches), spec.Values...) {
		if !used[key] || key == "via" {
			out = append(out, key)
		}
	}
	return out
}

func suggestedSiblings(table []Grammar, path []string) []string {
	var out []string
	for _, spec := range table {
		words := strings.Fields(spec.Path)
		if len(words) > len(path) && slices.Equal(words[:len(path)], path) && !slices.Contains(out, words[len(path)]) {
			out = append(out, words[len(path)])
		}
	}
	return out
}
