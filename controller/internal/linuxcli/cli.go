package linuxcli

import (
	"fmt"
	"strings"

	"dropcheck/controller/internal/command"
	"dropcheck/controller/internal/pipeline"
)

// Options contains dropcheck CLI flags that affect dispatch and presentation.
type Options struct {
	// Format selects text or JSON output. The zero value means text.
	Format pipeline.Format
	// Target selects one connected agent by ID, prefix, or adb serial.
	Target string
	// All sends agent commands to every connected agent.
	All bool
}

// Kind identifies the high-level command parsed from CLI args.
type Kind int

const (
	// AgentCommand sends an operation to one or more Android agents.
	AgentCommand Kind = iota
	// Devices lists connected agents.
	Devices
	// Config prints persisted Agent App configuration.
	Config
	// StandaloneSync downloads stored standalone measurement archives.
	StandaloneSync
)

// Command is the parsed non-interactive CLI command.
type Command struct {
	// Kind selects the app action to perform.
	Kind Kind
	// Operation is populated when Kind is AgentCommand.
	Operation command.Operation
	// ConfigScope is populated when Kind is Config.
	ConfigScope string
	// StandaloneSyncOutput is the output directory for StandaloneSync.
	StandaloneSyncOutput string
	// StandaloneSyncLimit caps StandaloneSync downloads.
	StandaloneSyncLimit string
	// StandaloneSyncMark marks downloaded runs as synced.
	StandaloneSyncMark bool
}

// ExtractOptions parses CLI-global flags and returns the remaining command
// arguments.
//
// Supported flags are --format, --target, and --all. Unknown dash-prefixed
// arguments are left in the returned rest slice so command-specific parsers can
// handle them.
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

// Parse converts non-interactive CLI args into a Command.
//
// Parse expects top-level app flags to have already been removed by
// ExtractOptions or the app package.
func Parse(args []string) (Command, error) {
	if len(args) == 0 {
		return Command{}, fmt.Errorf("usage: dropcheck [--adb adb] [--serial SERIAL] [--package PACKAGE] [--listen ADDR] shell [--target TARGET] | <command>")
	}
	switch args[0] {
	case "show":
		return parseShow(args[1:])
	case "configure":
		return parseConfigure(args[1:])
	case "clear":
		return parseClear(args[1:])
	case "sync":
		return parseSync(args[1:])
	case "request":
		return parseRequest(args[1:])
	default:
		return Command{}, fmt.Errorf("unknown command %q", args[0])
	}
}

func parseConfigure(args []string) (Command, error) {
	if len(args) == 0 {
		return Command{}, fmt.Errorf("usage: configure <set|delete> <command>")
	}
	switch args[0] {
	case "set":
		return parseSet(args[1:])
	case "delete":
		return parseDelete(args[1:])
	default:
		return Command{}, fmt.Errorf("usage: configure <set|delete> <command>")
	}
}

func parseShow(args []string) (Command, error) {
	if len(args) == 0 {
		return Command{}, fmt.Errorf("usage: show <devices|config|wifi|ip|standalone>")
	}
	switch args[0] {
	case "devices":
		if len(args) != 1 {
			return Command{}, fmt.Errorf("usage: show devices")
		}
		return Command{Kind: Devices}, nil
	case "config":
		return parseShowConfig(args[1:])
	case "wifi":
		op, err := parseLinuxShowWifi(args[1:])
		return Command{Kind: AgentCommand, Operation: op}, err
	case "ip":
		op, err := parseLinuxShowIP(args[1:])
		return Command{Kind: AgentCommand, Operation: op}, err
	case "standalone":
		op, err := parseStandaloneShow(args[1:])
		return Command{Kind: AgentCommand, Operation: op}, err
	default:
		return Command{}, fmt.Errorf("unknown show command %q", args[0])
	}
}

func parseLinuxShowIP(args []string) (command.Operation, error) {
	if len(args) != 1 || args[0] != "status" {
		return command.Operation{}, fmt.Errorf("usage: show ip status")
	}
	return command.IPStatusOperation(), nil
}

func parseShowConfig(args []string) (Command, error) {
	switch len(args) {
	case 0:
		return Command{Kind: Config, ConfigScope: "all"}, nil
	case 1:
		switch args[0] {
		case "standalone":
			return Command{Kind: Config, ConfigScope: "standalone"}, nil
		default:
			return Command{}, fmt.Errorf("usage: show config [standalone]")
		}
	default:
		return Command{}, fmt.Errorf("usage: show config [standalone]")
	}
}

func parseLinuxShowWifi(args []string) (command.Operation, error) {
	if len(args) == 0 {
		return command.Operation{}, fmt.Errorf("usage: show wifi <status|diagnostics|mlo|scan|capabilities>")
	}
	switch args[0] {
	case "status", "diagnostics", "mlo", "capabilities":
		if len(args) != 1 {
			return command.Operation{}, fmt.Errorf("usage: show wifi %s", args[0])
		}
		switch args[0] {
		case "status":
			return command.WifiStatusOperation(), nil
		case "diagnostics":
			return command.WifiDiagnosticsOperation(), nil
		case "mlo":
			return command.WifiMLOOperation(), nil
		default:
			return command.WifiCapabilitiesOperation(), nil
		}
	case "scan":
		return parseLinuxWifiScan(args[1:])
	default:
		return command.Operation{}, fmt.Errorf("unknown show wifi command %q", args[0])
	}
}

func parseSet(args []string) (Command, error) {
	if len(args) == 0 {
		return Command{}, fmt.Errorf("usage: set standalone <command>")
	}
	if args[0] != "standalone" {
		return Command{}, fmt.Errorf("usage: set standalone <command>")
	}
	edits, err := command.StandaloneSetEdits(args[1:])
	if err != nil {
		return Command{}, err
	}
	op, err := command.StandaloneEditOperation(edits)
	return Command{Kind: AgentCommand, Operation: op}, err
}

func parseDelete(args []string) (Command, error) {
	if len(args) == 0 || args[0] != "standalone" {
		return Command{}, fmt.Errorf("usage: delete standalone [festa <name>|...]")
	}
	edits, err := command.StandaloneDeleteEdits(args[1:])
	if err != nil {
		return Command{}, err
	}
	op, err := command.StandaloneEditOperation(edits)
	return Command{Kind: AgentCommand, Operation: op}, err
}

func parseClear(args []string) (Command, error) {
	if len(args) < 2 || args[0] != "standalone" || args[1] != "runs" || len(args) > 3 {
		return Command{}, fmt.Errorf("usage: clear standalone runs [synced|all]")
	}
	mode := "synced"
	if len(args) == 3 {
		mode = args[2]
	}
	op, err := command.StandaloneClearRunsOperation(mode)
	return Command{Kind: AgentCommand, Operation: op}, err
}

func parseSync(args []string) (Command, error) {
	if len(args) < 2 || args[0] != "standalone" || args[1] != "runs" {
		return Command{}, fmt.Errorf("usage: sync standalone runs [--output dir] [--limit n] [--mark-synced|--keep-unsynced]")
	}
	opts, err := parseDashOptions(args[2:], map[string]dashOptionSpec{
		"output":        {value: true},
		"limit":         {value: true},
		"mark-synced":   {},
		"keep-unsynced": {},
	})
	if err != nil {
		return Command{}, err
	}
	if len(opts.positionals) != 0 {
		return Command{}, fmt.Errorf("usage: sync standalone runs [--output dir] [--limit n] [--mark-synced|--keep-unsynced]")
	}
	if opts.flags["mark-synced"] && opts.flags["keep-unsynced"] {
		return Command{}, fmt.Errorf("--mark-synced and --keep-unsynced cannot be used together")
	}
	markSynced := !opts.flags["keep-unsynced"]
	return Command{Kind: StandaloneSync, StandaloneSyncOutput: opts.value("output"), StandaloneSyncLimit: opts.value("limit"), StandaloneSyncMark: markSynced}, nil
}

func parseRequest(args []string) (Command, error) {
	if len(args) == 0 {
		return Command{}, fmt.Errorf("usage: request <wifi|standalone|monitor|ping|traceroute|path-mtu|global-ip|dns|http|download> <command>")
	}
	if args[0] == "wifi" {
		op, err := parseLinuxWifi(args[1:])
		return Command{Kind: AgentCommand, Operation: op}, err
	}
	if args[0] == "monitor" {
		op, err := parseLinuxMonitor(args[1:])
		return Command{Kind: AgentCommand, Operation: op}, err
	}
	if args[0] == "ping" {
		op, err := parseLinuxPing(args[1:])
		return Command{Kind: AgentCommand, Operation: op}, err
	}
	if args[0] == "traceroute" {
		op, err := parseLinuxTraceroute(args[1:])
		return Command{Kind: AgentCommand, Operation: op}, err
	}
	if args[0] == "path-mtu" {
		op, err := parseLinuxPathMtu(args[1:])
		return Command{Kind: AgentCommand, Operation: op}, err
	}
	if args[0] == "global-ip" {
		op, err := parseLinuxGlobalIp(args[1:])
		return Command{Kind: AgentCommand, Operation: op}, err
	}
	if args[0] == "dns" {
		op, err := parseLinuxDNS(args[1:])
		return Command{Kind: AgentCommand, Operation: op}, err
	}
	if args[0] == "http" {
		op, err := parseLinuxHTTP(args[1:])
		return Command{Kind: AgentCommand, Operation: op}, err
	}
	if args[0] == "download" {
		op, err := parseLinuxDownload(args[1:])
		return Command{Kind: AgentCommand, Operation: op}, err
	}
	if args[0] != "standalone" {
		return Command{}, fmt.Errorf("usage: request <wifi|standalone|monitor|ping|traceroute|path-mtu|global-ip|dns|http|download> <command>")
	}
	return parseStandaloneRequest(args[1:])
}

func parseStandaloneShow(args []string) (command.Operation, error) {
	if len(args) == 0 {
		return command.Operation{}, fmt.Errorf("usage: show standalone <status|runs|run>")
	}
	switch args[0] {
	case "status":
		if len(args) != 1 {
			return command.Operation{}, fmt.Errorf("usage: show standalone status")
		}
		return command.StandaloneStatusOperation(), nil
	case "runs":
		opts, err := parseDashOptions(args[1:], map[string]dashOptionSpec{
			"limit":  {value: true},
			"synced": {},
		})
		if err != nil {
			return command.Operation{}, err
		}
		if len(opts.positionals) != 0 {
			return command.Operation{}, fmt.Errorf("usage: show standalone runs [--limit n] [--synced]")
		}
		return command.StandaloneListRunsOperation(command.StandaloneListOptions{Limit: opts.value("limit"), IncludeSynced: opts.flags["synced"]})
	case "run":
		if len(args) != 2 {
			return command.Operation{}, fmt.Errorf("usage: show standalone run <run-id>")
		}
		return command.StandaloneRunOperation(args[1], false)
	default:
		return command.Operation{}, fmt.Errorf("unknown show standalone command %q", args[0])
	}
}

func parseStandaloneRequest(args []string) (Command, error) {
	if len(args) == 0 {
		return Command{}, fmt.Errorf("usage: request standalone run once [--festa name] [--save]")
	}
	switch args[0] {
	case "run":
		if len(args) < 2 || args[1] != "once" {
			return Command{}, fmt.Errorf("usage: request standalone run once [--festa name] [--save]")
		}
		opts, err := parseDashOptions(args[2:], map[string]dashOptionSpec{
			"festa": {value: true},
			"save":  {},
		})
		if err != nil {
			return Command{}, err
		}
		if len(opts.positionals) != 0 {
			return Command{}, fmt.Errorf("usage: request standalone run once [--festa name] [--save]")
		}
		op, err := command.StandaloneRunOnceOperation(command.StandaloneRunOptions{Festa: opts.value("festa"), Save: opts.flags["save"]})
		return Command{Kind: AgentCommand, Operation: op}, err
	default:
		return Command{}, fmt.Errorf("unknown request standalone command %q", args[0])
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
			// Linux-style command parsers keep positionals in the order provided
			// and validate command-specific placement after option extraction.
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
			// Values may be separated by a space or supplied as --name=value.
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

func parseLinuxWifi(args []string) (command.Operation, error) {
	if len(args) == 0 {
		return command.Operation{}, fmt.Errorf("usage: request wifi <connect|disconnect|forget|wait|assert|reconnect|cycle>")
	}
	switch args[0] {
	case "disconnect":
		if len(args) != 1 {
			return command.Operation{}, fmt.Errorf("usage: request wifi disconnect")
		}
		return command.WifiDisconnectOperation(), nil
	case "connect":
		return parseLinuxWifiConnect(args[1:], "connect")
	case "forget":
		if len(args) != 2 {
			return command.Operation{}, fmt.Errorf("usage: request wifi forget <ssid|network_id>")
		}
		return command.WifiForgetOperation(args[1]), nil
	case "wait":
		return parseLinuxWifiWait(args[1:])
	case "assert":
		return parseLinuxWifiAssert(args[1:])
	case "reconnect":
		return parseLinuxWifiReconnect(args[1:])
	case "cycle":
		return parseLinuxWifiConnect(args[1:], "cycle")
	default:
		return command.Operation{}, fmt.Errorf("unknown request wifi command %q", args[0])
	}
}

func parseLinuxWifiScan(args []string) (command.Operation, error) {
	opts, err := parseDashOptions(args, map[string]dashOptionSpec{
		"band":    {value: true},
		"timeout": {value: true},
	})
	if err != nil {
		return command.Operation{}, err
	}
	pos := opts.positionals
	if len(pos) == 0 {
		if opts.value("timeout") != "" {
			return command.Operation{}, fmt.Errorf("--timeout is supported only with wifi scan fresh")
		}
		return command.WifiScanOperation(opts.value("band"))
	}
	switch pos[0] {
	case "fresh":
		band := opts.value("band")
		if opts.value("band") != "" {
		} else if len(pos) >= 2 {
			band = pos[1]
		}
		if len(pos) > 2 {
			return command.Operation{}, fmt.Errorf("usage: show wifi scan fresh [band] [--timeout ms]")
		}
		return command.WifiFreshScanOperation(band, opts.value("timeout"))
	case "detail":
		if opts.value("timeout") != "" {
			return command.Operation{}, fmt.Errorf("--timeout is supported only with wifi scan fresh")
		}
		if len(pos) < 2 {
			return command.Operation{}, fmt.Errorf("usage: show wifi scan detail <ssid|bssid> [--band band]")
		}
		band := opts.value("band")
		if opts.value("band") != "" {
		} else if len(pos) == 3 {
			band = pos[2]
		}
		if len(pos) > 3 {
			return command.Operation{}, fmt.Errorf("usage: show wifi scan detail <ssid|bssid> [band]")
		}
		return command.WifiScanDetailOperation(pos[1], band)
	default:
		if opts.value("timeout") != "" {
			return command.Operation{}, fmt.Errorf("--timeout is supported only with wifi scan fresh")
		}
		if len(pos) > 1 {
			return command.Operation{}, fmt.Errorf("usage: show wifi scan [band]")
		}
		if opts.value("band") != "" {
			return command.Operation{}, fmt.Errorf("show wifi scan band specified twice")
		}
		return command.WifiScanOperation(pos[0])
	}
}

func parseLinuxWifiConnect(args []string, operation string) (command.Operation, error) {
	specs := map[string]dashOptionSpec{
		"passphrase":        {value: true},
		"security":          {value: true},
		"bssid":             {value: true},
		"band":              {value: true},
		"mac-randomization": {value: true},
		"timeout":           {value: true},
	}
	if operation == "cycle" {
		specs["count"] = dashOptionSpec{value: true}
		specs["ping"] = dashOptionSpec{value: true}
		specs["http"] = dashOptionSpec{value: true}
		specs["pause"] = dashOptionSpec{value: true}
		specs["forget"] = dashOptionSpec{}
	}
	opts, err := parseDashOptions(args, specs)
	if err != nil {
		return command.Operation{}, err
	}
	pos := opts.positionals
	if len(pos) == 0 {
		return command.Operation{}, fmt.Errorf("usage: request wifi %s <ssid> --passphrase <passphrase>", operation)
	}
	ssid := pos[0]
	passphrase := opts.value("passphrase")
	switch {
	case passphrase != "" && len(pos) > 1:
		return command.Operation{}, fmt.Errorf("too many positional arguments for request wifi %s", operation)
	case passphrase == "" && len(pos) >= 2:
		passphrase = pos[1]
		if len(pos) > 2 {
			return command.Operation{}, fmt.Errorf("too many positional arguments for request wifi %s", operation)
		}
	case passphrase == "":
		return command.Operation{}, fmt.Errorf("request wifi %s requires --passphrase", operation)
	}
	connectOpts := command.WifiConnectOptions{
		SSID:             ssid,
		Passphrase:       passphrase,
		Security:         opts.value("security"),
		BSSID:            opts.value("bssid"),
		Band:             opts.value("band"),
		MacRandomization: opts.value("mac-randomization"),
		Timeout:          opts.value("timeout"),
	}
	if operation == "cycle" {
		return command.WifiCycleOperation(command.WifiCycleOptions{
			WifiConnectOptions: connectOpts,
			Count:              opts.value("count"),
			PingHost:           opts.value("ping"),
			HTTPURL:            opts.value("http"),
			ForgetAfterEach:    opts.flags["forget"],
			Pause:              opts.value("pause"),
		})
	}
	return command.WifiConnectOperation(connectOpts)
}

func parseLinuxWifiWait(args []string) (command.Operation, error) {
	if len(args) == 0 || args[0] != "connected" {
		return command.Operation{}, fmt.Errorf("usage: request wifi wait connected [ssid] [--bssid bssid] [--security security] [--band band] [--ip] [--validated] [--timeout ms]")
	}
	opts, err := parseDashOptions(args[1:], expectationDashSpecs())
	if err != nil {
		return command.Operation{}, err
	}
	if len(opts.positionals) > 1 {
		return command.Operation{}, fmt.Errorf("usage: request wifi wait connected [ssid]")
	}
	waitOpts := linuxExpectationOptions(opts)
	if len(opts.positionals) == 1 {
		if waitOpts.SSID != "" {
			return command.Operation{}, fmt.Errorf("request wifi wait connected ssid specified twice")
		}
		waitOpts.SSID = opts.positionals[0]
	}
	return command.WifiWaitConnectedOperation("", waitOpts)
}

func parseLinuxWifiAssert(args []string) (command.Operation, error) {
	opts, err := parseDashOptions(args, expectationDashSpecs())
	if err != nil {
		return command.Operation{}, err
	}
	if len(opts.positionals) != 0 {
		return command.Operation{}, fmt.Errorf("usage: request wifi assert [--ssid ssid] [--bssid bssid] [--security security] [--band band] [--ip] [--validated] [--timeout ms]")
	}
	return command.WifiAssertOperation(linuxExpectationOptions(opts))
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

func linuxExpectationOptions(opts parsedDashOptions) command.WifiExpectationOptions {
	return command.WifiExpectationOptions{
		SSID:             opts.value("ssid"),
		BSSID:            opts.value("bssid"),
		Security:         opts.value("security"),
		Band:             opts.value("band"),
		Timeout:          opts.value("timeout"),
		RequireIP:        opts.flags["ip"],
		RequireValidated: opts.flags["validated"],
	}
}

func parseLinuxMonitorWifi(args []string) (command.Operation, error) {
	opts, err := parseDashOptions(args, map[string]dashOptionSpec{
		"duration": {value: true},
		"interval": {value: true},
	})
	if err != nil {
		return command.Operation{}, err
	}
	if len(opts.positionals) > 2 {
		return command.Operation{}, fmt.Errorf("usage: request monitor wifi [duration_ms] [interval_ms]")
	}
	var duration string
	if opts.value("duration") != "" {
		duration = opts.value("duration")
	} else if len(opts.positionals) >= 1 {
		duration = opts.positionals[0]
	}
	var interval string
	if opts.value("interval") != "" {
		if duration == "" {
			duration = "10000"
		}
		interval = opts.value("interval")
	} else if len(opts.positionals) == 2 {
		interval = opts.positionals[1]
	}
	return command.WifiMonitorOperation(duration, interval)
}

func parseLinuxWifiReconnect(args []string) (command.Operation, error) {
	opts, err := parseDashOptions(args, map[string]dashOptionSpec{"timeout": {value: true}})
	if err != nil {
		return command.Operation{}, err
	}
	if len(opts.positionals) > 1 {
		return command.Operation{}, fmt.Errorf("usage: request wifi reconnect [timeout_ms]")
	}
	timeout := opts.value("timeout")
	if opts.value("timeout") != "" {
	} else if len(opts.positionals) == 1 {
		timeout = opts.positionals[0]
	}
	return command.WifiReconnectOperation(timeout)
}

func parseLinuxMonitor(args []string) (command.Operation, error) {
	if len(args) == 0 || args[0] != "wifi" {
		return command.Operation{}, fmt.Errorf("usage: request monitor wifi [duration_ms] [interval_ms]")
	}
	return parseLinuxMonitorWifi(args[1:])
}

func parseLinuxPing(args []string) (command.Operation, error) {
	opts, err := parseDashOptions(args, map[string]dashOptionSpec{
		"count":   {value: true},
		"size":    {value: true},
		"timeout": {value: true},
	})
	if err != nil {
		return command.Operation{}, err
	}
	if len(opts.positionals) == 0 {
		return command.Operation{}, fmt.Errorf("usage: request ping <host> [--count n] [--size bytes] [--timeout ms]")
	}
	if len(opts.positionals) > 2 {
		return command.Operation{}, fmt.Errorf("too many positional arguments for request ping")
	}
	count := opts.value("count")
	if opts.value("count") != "" {
	} else if len(opts.positionals) == 2 {
		count = opts.positionals[1]
	}
	return command.PingOperation(command.PingOptions{Host: opts.positionals[0], Count: count, Size: opts.value("size"), Timeout: opts.value("timeout")})
}

func parseLinuxTraceroute(args []string) (command.Operation, error) {
	opts, err := parseDashOptions(args, map[string]dashOptionSpec{
		"max-hops": {value: true},
		"via":      {value: true, multiple: true},
		"size":     {value: true},
		"timeout":  {value: true},
	})
	if err != nil {
		return command.Operation{}, err
	}
	if len(opts.positionals) == 0 {
		return command.Operation{}, fmt.Errorf("usage: request traceroute <host> [--max-hops n] [--via host_or_ip] [--size bytes] [--timeout ms]")
	}
	if len(opts.positionals) > 2 {
		return command.Operation{}, fmt.Errorf("too many positional arguments for request traceroute")
	}
	maxHops := opts.value("max-hops")
	if opts.value("max-hops") != "" {
	} else if len(opts.positionals) == 2 {
		maxHops = opts.positionals[1]
	}
	return command.TracerouteOperation(command.TracerouteOptions{
		Host: opts.positionals[0], MaxHops: maxHops, Via: opts.values["via"], Size: opts.value("size"), Timeout: opts.value("timeout"),
	})
}

func parseLinuxPathMtu(args []string) (command.Operation, error) {
	opts, err := parseDashOptions(args, map[string]dashOptionSpec{
		"min-mtu": {value: true},
		"max-mtu": {value: true},
		"timeout": {value: true},
	})
	if err != nil {
		return command.Operation{}, err
	}
	if len(opts.positionals) != 1 {
		return command.Operation{}, fmt.Errorf("usage: request path-mtu <host> [--min-mtu bytes] [--max-mtu bytes] [--timeout ms]")
	}
	return command.PathMTUOperation(command.PathMTUOptions{
		Host: opts.positionals[0], MinMTU: opts.value("min-mtu"), MaxMTU: opts.value("max-mtu"), Timeout: opts.value("timeout"),
	})
}

func parseLinuxGlobalIp(args []string) (command.Operation, error) {
	opts, err := parseDashOptions(args, map[string]dashOptionSpec{
		"family":  {value: true},
		"timeout": {value: true},
	})
	if err != nil {
		return command.Operation{}, err
	}
	if len(opts.positionals) > 1 {
		return command.Operation{}, fmt.Errorf("usage: request global-ip [ipv4|ipv6|all] [--family ipv4|ipv6|all] [--timeout ms]")
	}
	if len(opts.positionals) == 1 && opts.value("family") != "" {
		return command.Operation{}, fmt.Errorf("request global-ip family specified twice")
	}
	family := opts.value("family")
	if opts.value("family") != "" {
	} else if len(opts.positionals) == 1 {
		family = opts.positionals[0]
	}
	return command.GlobalIPOperation(family, opts.value("timeout"))
}

func parseLinuxDownload(args []string) (command.Operation, error) {
	opts, err := parseDashOptions(args, map[string]dashOptionSpec{"timeout": {value: true}})
	if err != nil {
		return command.Operation{}, err
	}
	if len(opts.positionals) != 1 {
		return command.Operation{}, fmt.Errorf("usage: request download <url> [--timeout ms]")
	}
	return command.DownloadOperation(opts.positionals[0], opts.value("timeout"))
}

func parseLinuxDNS(args []string) (command.Operation, error) {
	opts, err := parseDashOptions(args, map[string]dashOptionSpec{
		"type":    {value: true},
		"timeout": {value: true},
	})
	if err != nil {
		return command.Operation{}, err
	}
	if len(opts.positionals) == 0 {
		return command.Operation{}, fmt.Errorf("usage: request dns <name> [--type A|AAAA|ALL] [--timeout ms]")
	}
	if len(opts.positionals) > 2 {
		return command.Operation{}, fmt.Errorf("too many positional arguments for request dns")
	}
	qtype := opts.value("type")
	if opts.value("type") != "" {
	} else if len(opts.positionals) == 2 {
		qtype = opts.positionals[1]
	}
	return command.DNSOperation(opts.positionals[0], qtype, opts.value("timeout"))
}

func parseLinuxHTTP(args []string) (command.Operation, error) {
	opts, err := parseDashOptions(args, map[string]dashOptionSpec{
		"expected-status": {value: true},
		"timeout":         {value: true},
	})
	if err != nil {
		return command.Operation{}, err
	}
	if len(opts.positionals) == 0 {
		return command.Operation{}, fmt.Errorf("usage: request http <url> [--expected-status code] [--timeout ms]")
	}
	if len(opts.positionals) > 2 {
		return command.Operation{}, fmt.Errorf("too many positional arguments for request http")
	}
	expectedStatus := opts.value("expected-status")
	if opts.value("expected-status") != "" {
	} else if len(opts.positionals) == 2 {
		expectedStatus = opts.positionals[1]
	}
	return command.HTTPOperation(opts.positionals[0], expectedStatus, opts.value("timeout"))
}
