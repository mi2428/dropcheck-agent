package main

import (
	"context"
	"fmt"
	"strings"

	"dropcheck/controller/internal/control"
)

type cliOptions struct {
	format outputFormat
	target string
	all    bool
}

type cliCommandKind int

const (
	cliAgentCommand cliCommandKind = iota
	cliDevices
	cliTarget
)

type cliCommand struct {
	kind      cliCommandKind
	operation Operation
}

func runCLI(ctx context.Context, opts shellOptions, rawArgs []string) error {
	cliOpts, args, err := extractCLIOptions(rawArgs)
	if err != nil {
		return err
	}
	if cliOpts.format == "" {
		cliOpts.format = outputText
	}
	command, err := parseLinuxCommand(args)
	if err != nil {
		return err
	}

	session, err := startControlSession(ctx, opts)
	if err != nil {
		return err
	}
	defer session.Close()

	state := &shellState{server: session.server}
	if len(session.agents) > 0 {
		state.setSelectedAgent(session.agents[0])
	}
	if cliOpts.all {
		state.targetAll = true
	}
	if cliOpts.target != "" {
		info, err := resolveShellAgent(state, cliOpts.target)
		if err != nil {
			return err
		}
		state.setSelectedAgent(info)
		state.targetAll = false
	}

	switch command.kind {
	case cliDevices:
		out, err := renderAgents(state, cliOpts.format)
		if err != nil {
			return err
		}
		fmt.Print(out)
		return nil
	case cliTarget:
		out, err := renderTarget(state, cliOpts.format)
		if err != nil {
			return err
		}
		fmt.Print(out)
		return nil
	default:
		agents, err := state.commandTargets()
		if err != nil {
			return err
		}
		return runOperationForAgents(ctx, state, agents, command.operation, commandOutputOptions{format: cliOpts.format, strict: true})
	}
}

func extractCLIOptions(args []string) (cliOptions, []string, error) {
	var opts cliOptions
	var rest []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			rest = append(rest, args[i+1:]...)
			break
		}
		name, value, hasValue := strings.Cut(arg, "=")
		switch name {
		case "--format":
			if !hasValue {
				if i+1 >= len(args) {
					return opts, nil, fmt.Errorf("--format requires a value")
				}
				i++
				value = args[i]
			}
			switch outputFormat(value) {
			case outputText, outputJSON:
				opts.format = outputFormat(value)
			default:
				return opts, nil, fmt.Errorf("--format must be text or json")
			}
		case "--target":
			if !hasValue {
				if i+1 >= len(args) {
					return opts, nil, fmt.Errorf("--target requires a value")
				}
				i++
				value = args[i]
			}
			opts.target = value
		case "--all":
			if hasValue {
				return opts, nil, fmt.Errorf("--all does not take a value")
			}
			opts.all = true
		default:
			rest = append(rest, arg)
		}
	}
	if opts.all && opts.target != "" {
		return opts, nil, fmt.Errorf("--all and --target cannot be used together")
	}
	return opts, rest, nil
}

func parseLinuxCommand(args []string) (cliCommand, error) {
	if len(args) == 0 {
		return cliCommand{}, usage()
	}
	switch args[0] {
	case "devices":
		if len(args) != 1 {
			return cliCommand{}, fmt.Errorf("usage: devices")
		}
		return cliCommand{kind: cliDevices}, nil
	case "target":
		if len(args) != 1 {
			return cliCommand{}, fmt.Errorf("usage: target")
		}
		return cliCommand{kind: cliTarget}, nil
	case "wifi":
		legacy, err := parseLinuxWifi(args[1:])
		return cliCommand{kind: cliAgentCommand, operation: operationFromLegacyArgs(legacy)}, err
	case "ip":
		if len(args) != 1 {
			return cliCommand{}, fmt.Errorf("usage: ip")
		}
		return cliCommand{kind: cliAgentCommand, operation: operationFromLegacyArgs([]string{"ip"})}, nil
	case "ping":
		legacy, err := parseLinuxPing(args[1:])
		return cliCommand{kind: cliAgentCommand, operation: operationFromLegacyArgs(legacy)}, err
	case "traceroute":
		legacy, err := parseLinuxTraceroute(args[1:])
		return cliCommand{kind: cliAgentCommand, operation: operationFromLegacyArgs(legacy)}, err
	case "path-mtu":
		legacy, err := parseLinuxPathMtu(args[1:])
		return cliCommand{kind: cliAgentCommand, operation: operationFromLegacyArgs(legacy)}, err
	case "global-ip":
		legacy, err := parseLinuxGlobalIp(args[1:])
		return cliCommand{kind: cliAgentCommand, operation: operationFromLegacyArgs(legacy)}, err
	case "download":
		legacy, err := parseLinuxDownload(args[1:])
		return cliCommand{kind: cliAgentCommand, operation: operationFromLegacyArgs(legacy)}, err
	case "dns":
		legacy, err := parseLinuxDNS(args[1:])
		return cliCommand{kind: cliAgentCommand, operation: operationFromLegacyArgs(legacy)}, err
	case "http":
		legacy, err := parseLinuxHTTP(args[1:])
		return cliCommand{kind: cliAgentCommand, operation: operationFromLegacyArgs(legacy)}, err
	default:
		return cliCommand{}, fmt.Errorf("unknown command %q", args[0])
	}
}

type dashOptionSpec struct {
	value    bool
	multiple bool
}

type parsedDashOptions struct {
	positionals []string
	values      map[string][]string
	flags       map[string]bool
}

func parseDashOptions(args []string, specs map[string]dashOptionSpec) (parsedDashOptions, error) {
	parsed := parsedDashOptions{
		values: make(map[string][]string),
		flags:  make(map[string]bool),
	}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "--") || arg == "--" {
			parsed.positionals = append(parsed.positionals, arg)
			continue
		}
		name, value, hasValue := strings.Cut(strings.TrimPrefix(arg, "--"), "=")
		spec, ok := specs[name]
		if !ok {
			return parsedDashOptions{}, fmt.Errorf("unsupported option --%s", name)
		}
		if !spec.value {
			if hasValue {
				return parsedDashOptions{}, fmt.Errorf("--%s does not take a value", name)
			}
			parsed.flags[name] = true
			continue
		}
		if !hasValue {
			if i+1 >= len(args) {
				return parsedDashOptions{}, fmt.Errorf("--%s requires a value", name)
			}
			i++
			value = args[i]
		}
		if !spec.multiple && len(parsed.values[name]) > 0 {
			return parsedDashOptions{}, fmt.Errorf("--%s can be specified only once", name)
		}
		parsed.values[name] = append(parsed.values[name], value)
	}
	return parsed, nil
}

func (p parsedDashOptions) value(name string) string {
	values := p.values[name]
	if len(values) == 0 {
		return ""
	}
	return values[len(values)-1]
}

func parseLinuxWifi(args []string) ([]string, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("usage: wifi <status|diagnostics|scan|capabilities|connect|disconnect|forget|wait|assert|watch|monitor|reconnect|cycle>")
	}
	switch args[0] {
	case "status", "diagnostics", "capabilities", "disconnect":
		if len(args) != 1 {
			return nil, fmt.Errorf("usage: wifi %s", args[0])
		}
		return []string{"wifi", args[0]}, nil
	case "scan":
		return parseLinuxWifiScan(args[1:])
	case "connect":
		return parseLinuxWifiConnect(args[1:], "connect")
	case "forget":
		if len(args) != 2 {
			return nil, fmt.Errorf("usage: wifi forget <ssid|network_id>")
		}
		return []string{"wifi", "forget", args[1]}, nil
	case "wait":
		return parseLinuxWifiWait(args[1:])
	case "assert":
		return parseLinuxWifiAssert(args[1:])
	case "watch", "monitor":
		return parseLinuxWifiMonitor(args[1:], args[0])
	case "reconnect":
		return parseLinuxWifiReconnect(args[1:])
	case "cycle":
		return parseLinuxWifiConnect(args[1:], "cycle")
	default:
		return nil, fmt.Errorf("unknown wifi command %q", args[0])
	}
}

func parseLinuxWifiScan(args []string) ([]string, error) {
	opts, err := parseDashOptions(args, map[string]dashOptionSpec{
		"band":    {value: true},
		"timeout": {value: true},
	})
	if err != nil {
		return nil, err
	}
	pos := opts.positionals
	legacy := []string{"wifi", "scan"}
	if len(pos) == 0 {
		if opts.value("timeout") != "" {
			return nil, fmt.Errorf("--timeout is supported only with wifi scan fresh")
		}
		if opts.value("band") != "" {
			legacy = append(legacy, opts.value("band"))
		}
		return legacy, nil
	}
	switch pos[0] {
	case "fresh":
		legacy = append(legacy, "fresh")
		if opts.value("band") != "" {
			legacy = append(legacy, opts.value("band"))
		} else if len(pos) >= 2 {
			legacy = append(legacy, pos[1])
		}
		if len(pos) > 2 {
			return nil, fmt.Errorf("usage: wifi scan fresh [band] [--timeout ms]")
		}
		if opts.value("timeout") != "" {
			legacy = append(legacy, "--timeout", opts.value("timeout"))
		}
		return legacy, nil
	case "detail":
		if opts.value("timeout") != "" {
			return nil, fmt.Errorf("--timeout is supported only with wifi scan fresh")
		}
		if len(pos) < 2 {
			return nil, fmt.Errorf("usage: wifi scan detail <ssid|bssid> [--band band]")
		}
		legacy = append(legacy, "detail", pos[1])
		if opts.value("band") != "" {
			legacy = append(legacy, opts.value("band"))
		} else if len(pos) == 3 {
			legacy = append(legacy, pos[2])
		}
		if len(pos) > 3 {
			return nil, fmt.Errorf("usage: wifi scan detail <ssid|bssid> [band]")
		}
		return legacy, nil
	default:
		if opts.value("timeout") != "" {
			return nil, fmt.Errorf("--timeout is supported only with wifi scan fresh")
		}
		if len(pos) > 1 {
			return nil, fmt.Errorf("usage: wifi scan [band]")
		}
		if opts.value("band") != "" {
			return nil, fmt.Errorf("wifi scan band specified twice")
		}
		return append(legacy, pos[0]), nil
	}
}

func parseLinuxWifiConnect(args []string, command string) ([]string, error) {
	specs := map[string]dashOptionSpec{
		"passphrase":        {value: true},
		"security":          {value: true},
		"bssid":             {value: true},
		"band":              {value: true},
		"mac-randomization": {value: true},
		"timeout":           {value: true},
	}
	if command == "cycle" {
		specs["count"] = dashOptionSpec{value: true}
		specs["ping"] = dashOptionSpec{value: true}
		specs["http"] = dashOptionSpec{value: true}
		specs["pause"] = dashOptionSpec{value: true}
		specs["forget"] = dashOptionSpec{}
	}
	opts, err := parseDashOptions(args, specs)
	if err != nil {
		return nil, err
	}
	pos := opts.positionals
	if len(pos) == 0 {
		return nil, fmt.Errorf("usage: wifi %s <ssid> --passphrase <passphrase>", command)
	}
	ssid := pos[0]
	passphrase := opts.value("passphrase")
	if passphrase == "" && len(pos) >= 2 {
		passphrase = pos[1]
	}
	if passphrase == "" {
		return nil, fmt.Errorf("wifi %s requires --passphrase", command)
	}
	if len(pos) > 2 {
		return nil, fmt.Errorf("too many positional arguments for wifi %s", command)
	}
	legacy := []string{"wifi", command, ssid, passphrase}
	if opts.value("security") != "" {
		legacy = append(legacy, opts.value("security"))
	}
	for _, key := range []string{"count", "bssid", "band", "mac-randomization", "ping", "http", "pause", "timeout"} {
		if value := opts.value(key); value != "" {
			legacy = append(legacy, "--"+key, value)
		}
	}
	if opts.flags["forget"] {
		legacy = append(legacy, "--forget")
	}
	return legacy, nil
}

func parseLinuxWifiWait(args []string) ([]string, error) {
	if len(args) == 0 || args[0] != "connected" {
		return nil, fmt.Errorf("usage: wifi wait connected [ssid] [--bssid bssid] [--security security] [--band band] [--ip] [--validated] [--timeout ms]")
	}
	opts, err := parseDashOptions(args[1:], expectationDashSpecs())
	if err != nil {
		return nil, err
	}
	legacy := []string{"wifi", "wait", "connected"}
	if len(opts.positionals) > 1 {
		return nil, fmt.Errorf("usage: wifi wait connected [ssid]")
	}
	if len(opts.positionals) == 1 {
		legacy = append(legacy, opts.positionals[0])
	}
	return appendExpectationOptions(legacy, opts), nil
}

func parseLinuxWifiAssert(args []string) ([]string, error) {
	opts, err := parseDashOptions(args, expectationDashSpecs())
	if err != nil {
		return nil, err
	}
	if len(opts.positionals) != 0 {
		return nil, fmt.Errorf("usage: wifi assert [--ssid ssid] [--bssid bssid] [--security security] [--band band] [--ip] [--validated] [--timeout ms]")
	}
	return appendExpectationOptions([]string{"wifi", "assert"}, opts), nil
}

func expectationDashSpecs() map[string]dashOptionSpec {
	return map[string]dashOptionSpec{
		"ssid":      {value: true},
		"bssid":     {value: true},
		"security":  {value: true},
		"band":      {value: true},
		"timeout":   {value: true},
		"ip":        {},
		"validated": {},
	}
}

func appendExpectationOptions(legacy []string, opts parsedDashOptions) []string {
	for _, key := range []string{"ssid", "bssid", "security", "band", "timeout"} {
		if value := opts.value(key); value != "" {
			legacy = append(legacy, "--"+key, value)
		}
	}
	for _, key := range []string{"ip", "validated"} {
		if opts.flags[key] {
			legacy = append(legacy, "--"+key)
		}
	}
	return legacy
}

func parseLinuxWifiMonitor(args []string, command string) ([]string, error) {
	opts, err := parseDashOptions(args, map[string]dashOptionSpec{
		"duration": {value: true},
		"interval": {value: true},
	})
	if err != nil {
		return nil, err
	}
	if len(opts.positionals) > 2 {
		return nil, fmt.Errorf("usage: wifi %s [duration_ms] [interval_ms]", command)
	}
	legacy := []string{"wifi", command}
	if opts.value("duration") != "" {
		legacy = append(legacy, opts.value("duration"))
	} else if len(opts.positionals) >= 1 {
		legacy = append(legacy, opts.positionals[0])
	}
	if opts.value("interval") != "" {
		if len(legacy) == 2 {
			legacy = append(legacy, "10000")
		}
		legacy = append(legacy, opts.value("interval"))
	} else if len(opts.positionals) == 2 {
		legacy = append(legacy, opts.positionals[1])
	}
	return legacy, nil
}

func parseLinuxWifiReconnect(args []string) ([]string, error) {
	opts, err := parseDashOptions(args, map[string]dashOptionSpec{"timeout": {value: true}})
	if err != nil {
		return nil, err
	}
	if len(opts.positionals) > 1 {
		return nil, fmt.Errorf("usage: wifi reconnect [timeout_ms]")
	}
	legacy := []string{"wifi", "reconnect"}
	if opts.value("timeout") != "" {
		legacy = append(legacy, opts.value("timeout"))
	} else if len(opts.positionals) == 1 {
		legacy = append(legacy, opts.positionals[0])
	}
	return legacy, nil
}

func parseLinuxPing(args []string) ([]string, error) {
	opts, err := parseDashOptions(args, map[string]dashOptionSpec{
		"count":   {value: true},
		"size":    {value: true},
		"timeout": {value: true},
	})
	if err != nil {
		return nil, err
	}
	if len(opts.positionals) == 0 {
		return nil, fmt.Errorf("usage: ping <host> [--count n] [--size bytes] [--timeout ms]")
	}
	if len(opts.positionals) > 2 {
		return nil, fmt.Errorf("too many positional arguments for ping")
	}
	legacy := []string{"ping", opts.positionals[0]}
	if opts.value("count") != "" {
		legacy = append(legacy, opts.value("count"))
	} else if len(opts.positionals) == 2 {
		legacy = append(legacy, opts.positionals[1])
	}
	for _, key := range []string{"size", "timeout"} {
		if value := opts.value(key); value != "" {
			legacy = append(legacy, "--"+key, value)
		}
	}
	return legacy, nil
}

func parseLinuxTraceroute(args []string) ([]string, error) {
	opts, err := parseDashOptions(args, map[string]dashOptionSpec{
		"max-hops": {value: true},
		"via":      {value: true, multiple: true},
		"size":     {value: true},
		"timeout":  {value: true},
	})
	if err != nil {
		return nil, err
	}
	if len(opts.positionals) == 0 {
		return nil, fmt.Errorf("usage: traceroute <host> [--max-hops n] [--via host_or_ip] [--size bytes] [--timeout ms]")
	}
	if len(opts.positionals) > 2 {
		return nil, fmt.Errorf("too many positional arguments for traceroute")
	}
	legacy := []string{"traceroute", opts.positionals[0]}
	if opts.value("max-hops") != "" {
		legacy = append(legacy, opts.value("max-hops"))
	} else if len(opts.positionals) == 2 {
		legacy = append(legacy, opts.positionals[1])
	}
	for _, value := range opts.values["via"] {
		legacy = append(legacy, "--via", value)
	}
	for _, key := range []string{"size", "timeout"} {
		if value := opts.value(key); value != "" {
			legacy = append(legacy, "--"+key, value)
		}
	}
	return legacy, nil
}

func parseLinuxPathMtu(args []string) ([]string, error) {
	opts, err := parseDashOptions(args, map[string]dashOptionSpec{
		"min-mtu": {value: true},
		"max-mtu": {value: true},
		"timeout": {value: true},
	})
	if err != nil {
		return nil, err
	}
	if len(opts.positionals) != 1 {
		return nil, fmt.Errorf("usage: path-mtu <host> [--min-mtu bytes] [--max-mtu bytes] [--timeout ms]")
	}
	legacy := []string{"path-mtu", opts.positionals[0]}
	for _, key := range []string{"min-mtu", "max-mtu", "timeout"} {
		if value := opts.value(key); value != "" {
			legacy = append(legacy, "--"+key, value)
		}
	}
	return legacy, nil
}

func parseLinuxGlobalIp(args []string) ([]string, error) {
	opts, err := parseDashOptions(args, map[string]dashOptionSpec{
		"family":  {value: true},
		"timeout": {value: true},
	})
	if err != nil {
		return nil, err
	}
	if len(opts.positionals) > 1 {
		return nil, fmt.Errorf("usage: global-ip [ipv4|ipv6|all] [--family ipv4|ipv6|all] [--timeout ms]")
	}
	if len(opts.positionals) == 1 && opts.value("family") != "" {
		return nil, fmt.Errorf("global-ip family specified twice")
	}
	legacy := []string{"global-ip"}
	if opts.value("family") != "" {
		legacy = append(legacy, opts.value("family"))
	} else if len(opts.positionals) == 1 {
		legacy = append(legacy, opts.positionals[0])
	}
	if opts.value("timeout") != "" {
		legacy = append(legacy, "--timeout", opts.value("timeout"))
	}
	return legacy, nil
}

func parseLinuxDownload(args []string) ([]string, error) {
	opts, err := parseDashOptions(args, map[string]dashOptionSpec{"timeout": {value: true}})
	if err != nil {
		return nil, err
	}
	if len(opts.positionals) != 1 {
		return nil, fmt.Errorf("usage: download <url> [--timeout ms]")
	}
	legacy := []string{"download", opts.positionals[0]}
	if opts.value("timeout") != "" {
		legacy = append(legacy, "--timeout", opts.value("timeout"))
	}
	return legacy, nil
}

func parseLinuxDNS(args []string) ([]string, error) {
	opts, err := parseDashOptions(args, map[string]dashOptionSpec{
		"type":    {value: true},
		"timeout": {value: true},
	})
	if err != nil {
		return nil, err
	}
	if len(opts.positionals) == 0 {
		return nil, fmt.Errorf("usage: dns <name> [--type A|AAAA|ALL] [--timeout ms]")
	}
	if len(opts.positionals) > 2 {
		return nil, fmt.Errorf("too many positional arguments for dns")
	}
	legacy := []string{"dns", opts.positionals[0]}
	if opts.value("type") != "" {
		legacy = append(legacy, opts.value("type"))
	} else if len(opts.positionals) == 2 {
		legacy = append(legacy, opts.positionals[1])
	}
	if opts.value("timeout") != "" {
		legacy = append(legacy, "--timeout", opts.value("timeout"))
	}
	return legacy, nil
}

func parseLinuxHTTP(args []string) ([]string, error) {
	opts, err := parseDashOptions(args, map[string]dashOptionSpec{
		"expected-status": {value: true},
		"timeout":         {value: true},
	})
	if err != nil {
		return nil, err
	}
	if len(opts.positionals) == 0 {
		return nil, fmt.Errorf("usage: http <url> [--expected-status code] [--timeout ms]")
	}
	if len(opts.positionals) > 2 {
		return nil, fmt.Errorf("too many positional arguments for http")
	}
	legacy := []string{"http", opts.positionals[0]}
	if opts.value("expected-status") != "" {
		legacy = append(legacy, opts.value("expected-status"))
	} else if len(opts.positionals) == 2 {
		legacy = append(legacy, opts.positionals[1])
	}
	if opts.value("timeout") != "" {
		legacy = append(legacy, "--timeout", opts.value("timeout"))
	}
	return legacy, nil
}

func (s *shellState) commandTargets() ([]control.AgentInfo, error) {
	if s.targetAll {
		agents := s.server.Agents()
		if len(agents) == 0 {
			return nil, fmt.Errorf("no Android agents connected")
		}
		return agents, nil
	}
	info, err := selectedAgent(s)
	if err != nil {
		return nil, err
	}
	return []control.AgentInfo{info}, nil
}
