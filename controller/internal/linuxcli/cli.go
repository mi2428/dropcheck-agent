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
	// Target prints the default selected target.
	Target
	// FestivalSync downloads stored Dropcheck Festival measurement archives.
	FestivalSync
)

// Command is the parsed non-interactive CLI command.
type Command struct {
	// Kind selects the app action to perform.
	Kind Kind
	// Operation is populated when Kind is AgentCommand.
	Operation command.Operation
	// FestivalSyncOutput is the output directory for FestivalSync.
	FestivalSyncOutput string
	// FestivalSyncLimit caps FestivalSync downloads.
	FestivalSyncLimit string
	// FestivalSyncMark marks downloaded runs as synced.
	FestivalSyncMark bool
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
		return Command{}, fmt.Errorf("usage: dropcheck [--adb adb] [--serial SERIAL] [--package PACKAGE] [--listen ADDR] [--no-adb] shell | <command>")
	}
	switch args[0] {
	case "show":
		return parseShow(args[1:])
	case "set":
		return parseSet(args[1:])
	case "request":
		return parseRequest(args[1:])
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
		op, err := parseLinuxWifi(args[1:])
		return Command{Kind: AgentCommand, Operation: op}, err
	case "ip":
		if len(args) != 1 {
			return Command{}, fmt.Errorf("usage: ip")
		}
		return Command{Kind: AgentCommand, Operation: command.IPStatusOperation()}, nil
	case "ping":
		op, err := parseLinuxPing(args[1:])
		return Command{Kind: AgentCommand, Operation: op}, err
	case "traceroute":
		op, err := parseLinuxTraceroute(args[1:])
		return Command{Kind: AgentCommand, Operation: op}, err
	case "path-mtu":
		op, err := parseLinuxPathMtu(args[1:])
		return Command{Kind: AgentCommand, Operation: op}, err
	case "global-ip":
		op, err := parseLinuxGlobalIp(args[1:])
		return Command{Kind: AgentCommand, Operation: op}, err
	case "download":
		op, err := parseLinuxDownload(args[1:])
		return Command{Kind: AgentCommand, Operation: op}, err
	case "dns":
		op, err := parseLinuxDNS(args[1:])
		return Command{Kind: AgentCommand, Operation: op}, err
	case "http":
		op, err := parseLinuxHTTP(args[1:])
		return Command{Kind: AgentCommand, Operation: op}, err
	default:
		return Command{}, fmt.Errorf("unknown command %q", args[0])
	}
}

func parseShow(args []string) (Command, error) {
	if len(args) == 0 {
		return Command{}, fmt.Errorf("usage: show <devices|target|wifi|festival|controller>")
	}
	switch args[0] {
	case "devices":
		if len(args) != 1 {
			return Command{}, fmt.Errorf("usage: show devices")
		}
		return Command{Kind: Devices}, nil
	case "target":
		if len(args) != 1 {
			return Command{}, fmt.Errorf("usage: show target")
		}
		return Command{Kind: Target}, nil
	case "wifi":
		op, err := parseLinuxShowWifi(args[1:])
		return Command{Kind: AgentCommand, Operation: op}, err
	case "festival":
		op, err := parseFestivalShow(args[1:])
		return Command{Kind: AgentCommand, Operation: op}, err
	case "controller":
		op, err := parseControllerShow(args[1:])
		return Command{Kind: AgentCommand, Operation: op}, err
	default:
		return Command{}, fmt.Errorf("unknown show command %q", args[0])
	}
}

func parseControllerShow(args []string) (command.Operation, error) {
	if len(args) != 1 {
		return command.Operation{}, fmt.Errorf("usage: show controller <endpoint|link>")
	}
	switch args[0] {
	case "endpoint":
		return command.ControllerLinkConfigOperation(), nil
	case "link":
		return command.ControllerLinkStatusOperation(), nil
	default:
		return command.Operation{}, fmt.Errorf("unknown show controller command %q", args[0])
	}
}

func parseLinuxShowWifi(args []string) (command.Operation, error) {
	if len(args) == 0 {
		return command.Operation{}, fmt.Errorf("usage: show wifi <status|diagnostics|scan|capabilities>")
	}
	switch args[0] {
	case "status", "diagnostics", "capabilities":
		if len(args) != 1 {
			return command.Operation{}, fmt.Errorf("usage: show wifi %s", args[0])
		}
		switch args[0] {
		case "status":
			return command.WifiStatusOperation(), nil
		case "diagnostics":
			return command.WifiDiagnosticsOperation(), nil
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
		return Command{}, fmt.Errorf("usage: set <festival|controller> <command>")
	}
	if args[0] == "controller" {
		op, err := parseControllerSet(args[1:])
		return Command{Kind: AgentCommand, Operation: op}, err
	}
	if args[0] != "festival" {
		return Command{}, fmt.Errorf("usage: set <festival|controller> <command>")
	}
	op, err := parseFestivalSet(args[1:])
	return Command{Kind: AgentCommand, Operation: op}, err
}

func parseControllerSet(args []string) (command.Operation, error) {
	if len(args) == 0 || args[0] != "endpoint" {
		return command.Operation{}, fmt.Errorf("usage: set controller endpoint <host:port> enabled [--min-backoff duration] [--max-backoff duration] | disabled")
	}
	if len(args) == 2 && args[1] == "disabled" {
		return command.ControllerLinkSetConfigOperation(command.ControllerLinkConfigOptions{Enabled: false})
	}
	opts, err := parseDashOptions(args[1:], map[string]dashOptionSpec{
		"endpoint":    {value: true},
		"min-backoff": {value: true},
		"max-backoff": {value: true},
	})
	if err != nil {
		return command.Operation{}, err
	}
	endpoint := opts.value("endpoint")
	positionals := append([]string(nil), opts.positionals...)
	if len(positionals) > 0 && positionals[0] != "enabled" {
		endpoint = positionals[0]
		positionals = positionals[1:]
	}
	if len(positionals) != 1 || positionals[0] != "enabled" {
		return command.Operation{}, fmt.Errorf("usage: set controller endpoint <host:port> enabled [--min-backoff duration] [--max-backoff duration]")
	}
	return command.ControllerLinkSetConfigOperation(command.ControllerLinkConfigOptions{
		Enabled:    true,
		Endpoint:   endpoint,
		MinBackoff: opts.value("min-backoff"),
		MaxBackoff: opts.value("max-backoff"),
	})
}

func parseRequest(args []string) (Command, error) {
	if len(args) == 0 {
		return Command{}, fmt.Errorf("usage: request <wifi|festival|controller> <command>")
	}
	if args[0] == "wifi" {
		op, err := parseLinuxWifi(args[1:])
		return Command{Kind: AgentCommand, Operation: op}, err
	}
	if args[0] == "controller" {
		if len(args) != 2 || args[1] != "reconnect" {
			return Command{}, fmt.Errorf("usage: request controller reconnect")
		}
		return Command{Kind: AgentCommand, Operation: command.ControllerReconnectOperation()}, nil
	}
	if args[0] != "festival" {
		return Command{}, fmt.Errorf("usage: request <wifi|festival|controller> <command>")
	}
	return parseFestivalRequest(args[1:])
}

func parseFestivalShow(args []string) (command.Operation, error) {
	if len(args) == 0 {
		return command.Operation{}, fmt.Errorf("usage: show festival <standalone|runs|run>")
	}
	switch args[0] {
	case "standalone":
		if len(args) != 2 {
			return command.Operation{}, fmt.Errorf("usage: show festival standalone <status|config>")
		}
		switch args[1] {
		case "status":
			return command.FestivalStatusOperation(), nil
		case "config":
			return command.FestivalConfigOperation(), nil
		default:
			return command.Operation{}, fmt.Errorf("unknown show festival standalone command %q", args[1])
		}
	case "runs":
		opts, err := parseDashOptions(args[1:], map[string]dashOptionSpec{
			"limit":  {value: true},
			"synced": {},
		})
		if err != nil {
			return command.Operation{}, err
		}
		if len(opts.positionals) != 0 {
			return command.Operation{}, fmt.Errorf("usage: show festival runs [--limit n] [--synced]")
		}
		return command.FestivalListRunsOperation(command.FestivalListOptions{Limit: opts.value("limit"), IncludeSynced: opts.flags["synced"]})
	case "run":
		if len(args) != 2 {
			return command.Operation{}, fmt.Errorf("usage: show festival run <run-id>")
		}
		return command.FestivalRunOperation(args[1], false)
	default:
		return command.Operation{}, fmt.Errorf("unknown show festival command %q", args[0])
	}
}

func parseFestivalSet(args []string) (command.Operation, error) {
	if len(args) == 0 || args[0] != "standalone" {
		return command.Operation{}, fmt.Errorf("usage: set festival standalone <enabled|disabled>")
	}
	if len(args) == 2 && args[1] == "disabled" {
		return command.FestivalSetConfigOperation(command.FestivalConfigOptions{Enabled: false})
	}
	opts, err := parseDashOptions(args[1:], map[string]dashOptionSpec{
		"plan":      {value: true},
		"interval":  {value: true},
		"retention": {value: true},
		"max-size":  {value: true},
	})
	if err != nil {
		return command.Operation{}, err
	}
	if len(opts.positionals) != 1 || opts.positionals[0] != "enabled" {
		return command.Operation{}, fmt.Errorf("usage: set festival standalone enabled --plan <file> [--interval duration] [--retention duration] [--max-size bytes]")
	}
	return command.FestivalSetConfigOperation(command.FestivalConfigOptions{
		Enabled:   true,
		PlanPath:  opts.value("plan"),
		Interval:  opts.value("interval"),
		Retention: opts.value("retention"),
		MaxSize:   opts.value("max-size"),
	})
}

func parseFestivalRequest(args []string) (Command, error) {
	if len(args) == 0 {
		return Command{}, fmt.Errorf("usage: request festival <run|sync|clear>")
	}
	switch args[0] {
	case "run":
		if len(args) < 2 || args[1] != "once" {
			return Command{}, fmt.Errorf("usage: request festival run once --plan <file> [--save]")
		}
		opts, err := parseDashOptions(args[2:], map[string]dashOptionSpec{
			"plan": {value: true},
			"save": {},
		})
		if err != nil {
			return Command{}, err
		}
		if len(opts.positionals) != 0 {
			return Command{}, fmt.Errorf("usage: request festival run once --plan <file> [--save]")
		}
		op, err := command.FestivalRunOnceOperation(opts.value("plan"), opts.flags["save"])
		return Command{Kind: AgentCommand, Operation: op}, err
	case "clear":
		if len(args) > 2 {
			return Command{}, fmt.Errorf("usage: request festival clear [synced|all]")
		}
		mode := "synced"
		if len(args) == 2 {
			mode = args[1]
		}
		op, err := command.FestivalClearRunsOperation(mode)
		return Command{Kind: AgentCommand, Operation: op}, err
	case "sync":
		opts, err := parseDashOptions(args[1:], map[string]dashOptionSpec{
			"output":        {value: true},
			"limit":         {value: true},
			"mark-synced":   {},
			"keep-unsynced": {},
		})
		if err != nil {
			return Command{}, err
		}
		if len(opts.positionals) != 0 {
			return Command{}, fmt.Errorf("usage: request festival sync [--output dir] [--limit n] [--mark-synced|--keep-unsynced]")
		}
		markSynced := !opts.flags["keep-unsynced"] || opts.flags["mark-synced"]
		return Command{Kind: FestivalSync, FestivalSyncOutput: opts.value("output"), FestivalSyncLimit: opts.value("limit"), FestivalSyncMark: markSynced}, nil
	default:
		return Command{}, fmt.Errorf("unknown request festival command %q", args[0])
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
		return command.Operation{}, fmt.Errorf("usage: wifi <status|diagnostics|scan|capabilities|connect|disconnect|forget|wait|assert|watch|monitor|reconnect|cycle>")
	}
	switch args[0] {
	case "status", "diagnostics", "capabilities", "disconnect":
		if len(args) != 1 {
			return command.Operation{}, fmt.Errorf("usage: wifi %s", args[0])
		}
		switch args[0] {
		case "status":
			return command.WifiStatusOperation(), nil
		case "diagnostics":
			return command.WifiDiagnosticsOperation(), nil
		case "capabilities":
			return command.WifiCapabilitiesOperation(), nil
		default:
			return command.WifiDisconnectOperation(), nil
		}
	case "scan":
		return parseLinuxWifiScan(args[1:])
	case "connect":
		return parseLinuxWifiConnect(args[1:], "connect")
	case "forget":
		if len(args) != 2 {
			return command.Operation{}, fmt.Errorf("usage: wifi forget <ssid|network_id>")
		}
		return command.WifiForgetOperation(args[1]), nil
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
		return command.Operation{}, fmt.Errorf("unknown wifi command %q", args[0])
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
			return command.Operation{}, fmt.Errorf("usage: wifi scan fresh [band] [--timeout ms]")
		}
		return command.WifiFreshScanOperation(band, opts.value("timeout"))
	case "detail":
		if opts.value("timeout") != "" {
			return command.Operation{}, fmt.Errorf("--timeout is supported only with wifi scan fresh")
		}
		if len(pos) < 2 {
			return command.Operation{}, fmt.Errorf("usage: wifi scan detail <ssid|bssid> [--band band]")
		}
		band := opts.value("band")
		if opts.value("band") != "" {
		} else if len(pos) == 3 {
			band = pos[2]
		}
		if len(pos) > 3 {
			return command.Operation{}, fmt.Errorf("usage: wifi scan detail <ssid|bssid> [band]")
		}
		return command.WifiScanDetailOperation(pos[1], band)
	default:
		if opts.value("timeout") != "" {
			return command.Operation{}, fmt.Errorf("--timeout is supported only with wifi scan fresh")
		}
		if len(pos) > 1 {
			return command.Operation{}, fmt.Errorf("usage: wifi scan [band]")
		}
		if opts.value("band") != "" {
			return command.Operation{}, fmt.Errorf("wifi scan band specified twice")
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
		return command.Operation{}, fmt.Errorf("usage: wifi %s <ssid> --passphrase <passphrase>", operation)
	}
	ssid := pos[0]
	passphrase := opts.value("passphrase")
	switch {
	case passphrase != "" && len(pos) > 1:
		return command.Operation{}, fmt.Errorf("too many positional arguments for wifi %s", operation)
	case passphrase == "" && len(pos) >= 2:
		passphrase = pos[1]
		if len(pos) > 2 {
			return command.Operation{}, fmt.Errorf("too many positional arguments for wifi %s", operation)
		}
	case passphrase == "":
		return command.Operation{}, fmt.Errorf("wifi %s requires --passphrase", operation)
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
		return command.Operation{}, fmt.Errorf("usage: wifi wait connected [ssid] [--bssid bssid] [--security security] [--band band] [--ip] [--validated] [--timeout ms]")
	}
	opts, err := parseDashOptions(args[1:], expectationDashSpecs())
	if err != nil {
		return command.Operation{}, err
	}
	if len(opts.positionals) > 1 {
		return command.Operation{}, fmt.Errorf("usage: wifi wait connected [ssid]")
	}
	waitOpts := linuxExpectationOptions(opts)
	if len(opts.positionals) == 1 {
		if waitOpts.SSID != "" {
			return command.Operation{}, fmt.Errorf("wifi wait connected ssid specified twice")
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
		return command.Operation{}, fmt.Errorf("usage: wifi assert [--ssid ssid] [--bssid bssid] [--security security] [--band band] [--ip] [--validated] [--timeout ms]")
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

func parseLinuxWifiMonitor(args []string, operation string) (command.Operation, error) {
	opts, err := parseDashOptions(args, map[string]dashOptionSpec{
		"duration": {value: true},
		"interval": {value: true},
	})
	if err != nil {
		return command.Operation{}, err
	}
	if len(opts.positionals) > 2 {
		return command.Operation{}, fmt.Errorf("usage: wifi %s [duration_ms] [interval_ms]", operation)
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
	if operation == "watch" {
		return command.WifiWatchOperation(duration, interval)
	}
	return command.WifiMonitorOperation(duration, interval)
}

func parseLinuxWifiReconnect(args []string) (command.Operation, error) {
	opts, err := parseDashOptions(args, map[string]dashOptionSpec{"timeout": {value: true}})
	if err != nil {
		return command.Operation{}, err
	}
	if len(opts.positionals) > 1 {
		return command.Operation{}, fmt.Errorf("usage: wifi reconnect [timeout_ms]")
	}
	timeout := opts.value("timeout")
	if opts.value("timeout") != "" {
	} else if len(opts.positionals) == 1 {
		timeout = opts.positionals[0]
	}
	return command.WifiReconnectOperation(timeout)
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
		return command.Operation{}, fmt.Errorf("usage: ping <host> [--count n] [--size bytes] [--timeout ms]")
	}
	if len(opts.positionals) > 2 {
		return command.Operation{}, fmt.Errorf("too many positional arguments for ping")
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
		return command.Operation{}, fmt.Errorf("usage: traceroute <host> [--max-hops n] [--via host_or_ip] [--size bytes] [--timeout ms]")
	}
	if len(opts.positionals) > 2 {
		return command.Operation{}, fmt.Errorf("too many positional arguments for traceroute")
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
		return command.Operation{}, fmt.Errorf("usage: path-mtu <host> [--min-mtu bytes] [--max-mtu bytes] [--timeout ms]")
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
		return command.Operation{}, fmt.Errorf("usage: global-ip [ipv4|ipv6|all] [--family ipv4|ipv6|all] [--timeout ms]")
	}
	if len(opts.positionals) == 1 && opts.value("family") != "" {
		return command.Operation{}, fmt.Errorf("global-ip family specified twice")
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
		return command.Operation{}, fmt.Errorf("usage: download <url> [--timeout ms]")
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
		return command.Operation{}, fmt.Errorf("usage: dns <name> [--type A|AAAA|ALL] [--timeout ms]")
	}
	if len(opts.positionals) > 2 {
		return command.Operation{}, fmt.Errorf("too many positional arguments for dns")
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
		return command.Operation{}, fmt.Errorf("usage: http <url> [--expected-status code] [--timeout ms]")
	}
	if len(opts.positionals) > 2 {
		return command.Operation{}, fmt.Errorf("too many positional arguments for http")
	}
	expectedStatus := opts.value("expected-status")
	if opts.value("expected-status") != "" {
	} else if len(opts.positionals) == 2 {
		expectedStatus = opts.positionals[1]
	}
	return command.HTTPOperation(opts.positionals[0], expectedStatus, opts.value("timeout"))
}
