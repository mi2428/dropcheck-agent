package linuxcli

import (
	"fmt"
	"strings"

	"dropcheck/controller/internal/command"
	"dropcheck/controller/internal/pipeline"
)

type Options struct {
	Format pipeline.Format
	Target string
	All    bool
}

type Kind int

const (
	AgentCommand Kind = iota
	Devices
	Target
)

type Command struct {
	Kind      Kind
	Operation command.Operation
}

func ExtractOptions(args []string) (Options, []string, error) {
	var opts Options
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
			switch pipeline.Format(value) {
			case pipeline.FormatText, pipeline.FormatJSON:
				opts.Format = pipeline.Format(value)
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
			opts.Target = value
		case "--all":
			if hasValue {
				return opts, nil, fmt.Errorf("--all does not take a value")
			}
			opts.All = true
		default:
			rest = append(rest, arg)
		}
	}
	if opts.All && opts.Target != "" {
		return opts, nil, fmt.Errorf("--all and --target cannot be used together")
	}
	return opts, rest, nil
}

func Parse(args []string) (Command, error) {
	if len(args) == 0 {
		return Command{}, fmt.Errorf("usage: dropcheck [--adb adb] [--serial SERIAL] [--package PACKAGE] shell | <command>")
	}
	switch args[0] {
	case "devices":
		if len(args) != 1 {
			return Command{}, fmt.Errorf("usage: devices")
		}
		return Command{Kind: Devices}, nil
	case "target":
		if len(args) != 1 {
			return Command{}, fmt.Errorf("usage: target")
		}
		return Command{Kind: Target}, nil
	case "wifi":
		commandArgs, err := parseLinuxWifi(args[1:])
		return Command{Kind: AgentCommand, Operation: command.OperationFromCommandArgs(commandArgs)}, err
	case "ip":
		if len(args) != 1 {
			return Command{}, fmt.Errorf("usage: ip")
		}
		return Command{Kind: AgentCommand, Operation: command.OperationFromCommandArgs([]string{"ip"})}, nil
	case "ping":
		commandArgs, err := parseLinuxPing(args[1:])
		return Command{Kind: AgentCommand, Operation: command.OperationFromCommandArgs(commandArgs)}, err
	case "traceroute":
		commandArgs, err := parseLinuxTraceroute(args[1:])
		return Command{Kind: AgentCommand, Operation: command.OperationFromCommandArgs(commandArgs)}, err
	case "path-mtu":
		commandArgs, err := parseLinuxPathMtu(args[1:])
		return Command{Kind: AgentCommand, Operation: command.OperationFromCommandArgs(commandArgs)}, err
	case "global-ip":
		commandArgs, err := parseLinuxGlobalIp(args[1:])
		return Command{Kind: AgentCommand, Operation: command.OperationFromCommandArgs(commandArgs)}, err
	case "download":
		commandArgs, err := parseLinuxDownload(args[1:])
		return Command{Kind: AgentCommand, Operation: command.OperationFromCommandArgs(commandArgs)}, err
	case "dns":
		commandArgs, err := parseLinuxDNS(args[1:])
		return Command{Kind: AgentCommand, Operation: command.OperationFromCommandArgs(commandArgs)}, err
	case "http":
		commandArgs, err := parseLinuxHTTP(args[1:])
		return Command{Kind: AgentCommand, Operation: command.OperationFromCommandArgs(commandArgs)}, err
	default:
		return Command{}, fmt.Errorf("unknown command %q", args[0])
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
	commandArgs := []string{"wifi", "scan"}
	if len(pos) == 0 {
		if opts.value("timeout") != "" {
			return nil, fmt.Errorf("--timeout is supported only with wifi scan fresh")
		}
		if opts.value("band") != "" {
			commandArgs = append(commandArgs, opts.value("band"))
		}
		return commandArgs, nil
	}
	switch pos[0] {
	case "fresh":
		commandArgs = append(commandArgs, "fresh")
		if opts.value("band") != "" {
			commandArgs = append(commandArgs, opts.value("band"))
		} else if len(pos) >= 2 {
			commandArgs = append(commandArgs, pos[1])
		}
		if len(pos) > 2 {
			return nil, fmt.Errorf("usage: wifi scan fresh [band] [--timeout ms]")
		}
		if opts.value("timeout") != "" {
			commandArgs = append(commandArgs, "--timeout", opts.value("timeout"))
		}
		return commandArgs, nil
	case "detail":
		if opts.value("timeout") != "" {
			return nil, fmt.Errorf("--timeout is supported only with wifi scan fresh")
		}
		if len(pos) < 2 {
			return nil, fmt.Errorf("usage: wifi scan detail <ssid|bssid> [--band band]")
		}
		commandArgs = append(commandArgs, "detail", pos[1])
		if opts.value("band") != "" {
			commandArgs = append(commandArgs, opts.value("band"))
		} else if len(pos) == 3 {
			commandArgs = append(commandArgs, pos[2])
		}
		if len(pos) > 3 {
			return nil, fmt.Errorf("usage: wifi scan detail <ssid|bssid> [band]")
		}
		return commandArgs, nil
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
		return append(commandArgs, pos[0]), nil
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
	commandArgs := []string{"wifi", command, ssid, passphrase}
	if opts.value("security") != "" {
		commandArgs = append(commandArgs, opts.value("security"))
	}
	for _, key := range []string{"count", "bssid", "band", "mac-randomization", "ping", "http", "pause", "timeout"} {
		if value := opts.value(key); value != "" {
			commandArgs = append(commandArgs, "--"+key, value)
		}
	}
	if opts.flags["forget"] {
		commandArgs = append(commandArgs, "--forget")
	}
	return commandArgs, nil
}

func parseLinuxWifiWait(args []string) ([]string, error) {
	if len(args) == 0 || args[0] != "connected" {
		return nil, fmt.Errorf("usage: wifi wait connected [ssid] [--bssid bssid] [--security security] [--band band] [--ip] [--validated] [--timeout ms]")
	}
	opts, err := parseDashOptions(args[1:], expectationDashSpecs())
	if err != nil {
		return nil, err
	}
	commandArgs := []string{"wifi", "wait", "connected"}
	if len(opts.positionals) > 1 {
		return nil, fmt.Errorf("usage: wifi wait connected [ssid]")
	}
	if len(opts.positionals) == 1 {
		commandArgs = append(commandArgs, opts.positionals[0])
	}
	return appendExpectationOptions(commandArgs, opts), nil
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

func appendExpectationOptions(commandArgs []string, opts parsedDashOptions) []string {
	for _, key := range []string{"ssid", "bssid", "security", "band", "timeout"} {
		if value := opts.value(key); value != "" {
			commandArgs = append(commandArgs, "--"+key, value)
		}
	}
	for _, key := range []string{"ip", "validated"} {
		if opts.flags[key] {
			commandArgs = append(commandArgs, "--"+key)
		}
	}
	return commandArgs
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
	commandArgs := []string{"wifi", command}
	if opts.value("duration") != "" {
		commandArgs = append(commandArgs, opts.value("duration"))
	} else if len(opts.positionals) >= 1 {
		commandArgs = append(commandArgs, opts.positionals[0])
	}
	if opts.value("interval") != "" {
		if len(commandArgs) == 2 {
			commandArgs = append(commandArgs, "10000")
		}
		commandArgs = append(commandArgs, opts.value("interval"))
	} else if len(opts.positionals) == 2 {
		commandArgs = append(commandArgs, opts.positionals[1])
	}
	return commandArgs, nil
}

func parseLinuxWifiReconnect(args []string) ([]string, error) {
	opts, err := parseDashOptions(args, map[string]dashOptionSpec{"timeout": {value: true}})
	if err != nil {
		return nil, err
	}
	if len(opts.positionals) > 1 {
		return nil, fmt.Errorf("usage: wifi reconnect [timeout_ms]")
	}
	commandArgs := []string{"wifi", "reconnect"}
	if opts.value("timeout") != "" {
		commandArgs = append(commandArgs, opts.value("timeout"))
	} else if len(opts.positionals) == 1 {
		commandArgs = append(commandArgs, opts.positionals[0])
	}
	return commandArgs, nil
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
	commandArgs := []string{"ping", opts.positionals[0]}
	if opts.value("count") != "" {
		commandArgs = append(commandArgs, opts.value("count"))
	} else if len(opts.positionals) == 2 {
		commandArgs = append(commandArgs, opts.positionals[1])
	}
	for _, key := range []string{"size", "timeout"} {
		if value := opts.value(key); value != "" {
			commandArgs = append(commandArgs, "--"+key, value)
		}
	}
	return commandArgs, nil
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
	commandArgs := []string{"traceroute", opts.positionals[0]}
	if opts.value("max-hops") != "" {
		commandArgs = append(commandArgs, opts.value("max-hops"))
	} else if len(opts.positionals) == 2 {
		commandArgs = append(commandArgs, opts.positionals[1])
	}
	for _, value := range opts.values["via"] {
		commandArgs = append(commandArgs, "--via", value)
	}
	for _, key := range []string{"size", "timeout"} {
		if value := opts.value(key); value != "" {
			commandArgs = append(commandArgs, "--"+key, value)
		}
	}
	return commandArgs, nil
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
	commandArgs := []string{"path-mtu", opts.positionals[0]}
	for _, key := range []string{"min-mtu", "max-mtu", "timeout"} {
		if value := opts.value(key); value != "" {
			commandArgs = append(commandArgs, "--"+key, value)
		}
	}
	return commandArgs, nil
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
	commandArgs := []string{"global-ip"}
	if opts.value("family") != "" {
		commandArgs = append(commandArgs, opts.value("family"))
	} else if len(opts.positionals) == 1 {
		commandArgs = append(commandArgs, opts.positionals[0])
	}
	if opts.value("timeout") != "" {
		commandArgs = append(commandArgs, "--timeout", opts.value("timeout"))
	}
	return commandArgs, nil
}

func parseLinuxDownload(args []string) ([]string, error) {
	opts, err := parseDashOptions(args, map[string]dashOptionSpec{"timeout": {value: true}})
	if err != nil {
		return nil, err
	}
	if len(opts.positionals) != 1 {
		return nil, fmt.Errorf("usage: download <url> [--timeout ms]")
	}
	commandArgs := []string{"download", opts.positionals[0]}
	if opts.value("timeout") != "" {
		commandArgs = append(commandArgs, "--timeout", opts.value("timeout"))
	}
	return commandArgs, nil
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
	commandArgs := []string{"dns", opts.positionals[0]}
	if opts.value("type") != "" {
		commandArgs = append(commandArgs, opts.value("type"))
	} else if len(opts.positionals) == 2 {
		commandArgs = append(commandArgs, opts.positionals[1])
	}
	if opts.value("timeout") != "" {
		commandArgs = append(commandArgs, "--timeout", opts.value("timeout"))
	}
	return commandArgs, nil
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
	commandArgs := []string{"http", opts.positionals[0]}
	if opts.value("expected-status") != "" {
		commandArgs = append(commandArgs, opts.value("expected-status"))
	} else if len(opts.positionals) == 2 {
		commandArgs = append(commandArgs, opts.positionals[1])
	}
	if opts.value("timeout") != "" {
		commandArgs = append(commandArgs, "--timeout", opts.value("timeout"))
	}
	return commandArgs, nil
}
