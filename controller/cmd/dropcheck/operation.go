package main

import (
	"fmt"
	"strings"

	"dropcheck/controller/internal/controlpb"
)

type TargetSpec struct {
	Selector string
	All      bool
}

type Operation struct {
	Name   string
	Args   map[string]string
	Flags  map[string]bool
	Target TargetSpec

	legacyArgs []string
}

func newOperation(name string, legacyArgs []string, args map[string]string, flags map[string]bool) Operation {
	if args == nil {
		args = map[string]string{}
	}
	if flags == nil {
		flags = map[string]bool{}
	}
	return Operation{
		Name:       name,
		Args:       args,
		Flags:      flags,
		legacyArgs: append([]string(nil), legacyArgs...),
	}
}

func operationFromLegacyArgs(legacyArgs []string) Operation {
	args := map[string]string{}
	flags := map[string]bool{}
	name := operationNameFromLegacyArgs(legacyArgs)
	collectOperationArgs(legacyArgs, args, flags)
	return newOperation(name, legacyArgs, args, flags)
}

func buildRunCommand(op Operation) (*controlpb.RunCommand, commandOptions, error) {
	if len(op.legacyArgs) == 0 {
		return nil, commandOptions{}, fmt.Errorf("operation %q has no command adapter", op.Name)
	}
	return buildCommandWithOptions(op.legacyArgs)
}

func (op Operation) legacyCommandArgs() []string {
	return append([]string(nil), op.legacyArgs...)
}

func operationNameFromLegacyArgs(args []string) string {
	if len(args) == 0 {
		return ""
	}
	switch args[0] {
	case "wifi":
		if len(args) < 2 {
			return "wifi"
		}
		if args[1] == "scan" && len(args) >= 3 {
			switch args[2] {
			case "fresh":
				return "wifi.scan.fresh"
			case "detail":
				return "wifi.scan.detail"
			}
		}
		return "wifi." + args[1]
	case "ip":
		return "ip.status"
	case "download":
		return "download"
	case "dns":
		return "dns"
	case "http":
		return "http"
	default:
		return args[0]
	}
}

func collectOperationArgs(legacy []string, args map[string]string, flags map[string]bool) {
	if len(legacy) == 0 {
		return
	}
	switch legacy[0] {
	case "wifi":
		collectWifiOperationArgs(legacy, args, flags)
	case "ping":
		if len(legacy) >= 2 {
			args["host"] = legacy[1]
		}
		if len(legacy) >= 3 && !strings.HasPrefix(legacy[2], "--") {
			args["count"] = legacy[2]
			collectLongOptions(legacy[3:], args, flags)
			return
		}
		collectLongOptions(legacy[2:], args, flags)
	case "traceroute":
		if len(legacy) >= 2 {
			args["host"] = legacy[1]
		}
		if len(legacy) >= 3 && !strings.HasPrefix(legacy[2], "--") {
			args["max-hops"] = legacy[2]
			collectLongOptions(legacy[3:], args, flags)
			return
		}
		collectLongOptions(legacy[2:], args, flags)
	case "path-mtu":
		if len(legacy) >= 2 {
			args["host"] = legacy[1]
		}
		collectLongOptions(legacy[2:], args, flags)
	case "global-ip":
		if len(legacy) >= 2 && !strings.HasPrefix(legacy[1], "--") {
			args["family"] = legacy[1]
			collectLongOptions(legacy[2:], args, flags)
			return
		}
		collectLongOptions(legacy[1:], args, flags)
	case "download":
		if len(legacy) >= 2 {
			args["url"] = legacy[1]
		}
		collectLongOptions(legacy[2:], args, flags)
	case "dns":
		if len(legacy) >= 2 {
			args["name"] = legacy[1]
		}
		if len(legacy) >= 3 && !strings.HasPrefix(legacy[2], "--") {
			args["type"] = legacy[2]
			collectLongOptions(legacy[3:], args, flags)
			return
		}
		collectLongOptions(legacy[2:], args, flags)
	case "http":
		if len(legacy) >= 2 {
			args["url"] = legacy[1]
		}
		if len(legacy) >= 3 && !strings.HasPrefix(legacy[2], "--") {
			args["expected-status"] = legacy[2]
			collectLongOptions(legacy[3:], args, flags)
			return
		}
		collectLongOptions(legacy[2:], args, flags)
	}
}

func collectWifiOperationArgs(legacy []string, args map[string]string, flags map[string]bool) {
	if len(legacy) < 2 {
		return
	}
	switch legacy[1] {
	case "connect", "cycle":
		if len(legacy) >= 3 {
			args["ssid"] = legacy[2]
		}
		if len(legacy) >= 4 {
			args["passphrase"] = legacy[3]
		}
		rest := legacy[4:]
		if len(rest) > 0 && !strings.HasPrefix(rest[0], "--") {
			args["security"] = rest[0]
			rest = rest[1:]
		}
		collectLongOptions(rest, args, flags)
	case "scan":
		if len(legacy) >= 3 {
			switch legacy[2] {
			case "fresh":
				args["scan-mode"] = "fresh"
				if len(legacy) >= 4 && !strings.HasPrefix(legacy[3], "--") {
					args["band"] = legacy[3]
					collectLongOptions(legacy[4:], args, flags)
					return
				}
				collectLongOptions(legacy[3:], args, flags)
			case "detail":
				args["scan-mode"] = "detail"
				if len(legacy) >= 4 {
					args["target"] = legacy[3]
				}
				if len(legacy) >= 5 {
					args["band"] = legacy[4]
				}
			default:
				args["band"] = legacy[2]
			}
		}
	case "forget":
		if len(legacy) >= 3 {
			args["target"] = legacy[2]
		}
	case "wait":
		if len(legacy) >= 3 {
			args["state"] = legacy[2]
		}
		if len(legacy) >= 4 && !strings.HasPrefix(legacy[3], "--") {
			args["ssid"] = legacy[3]
			collectLongOptions(legacy[4:], args, flags)
			return
		}
		collectLongOptions(legacy[3:], args, flags)
	case "assert":
		collectLongOptions(legacy[2:], args, flags)
	case "watch", "monitor":
		if len(legacy) >= 3 {
			args["duration"] = legacy[2]
		}
		if len(legacy) >= 4 {
			args["interval"] = legacy[3]
		}
	case "reconnect":
		if len(legacy) >= 3 {
			args["timeout"] = legacy[2]
		}
	}
}

func collectLongOptions(values []string, args map[string]string, flags map[string]bool) {
	for i := 0; i < len(values); i++ {
		value := values[i]
		if !strings.HasPrefix(value, "--") {
			continue
		}
		name := strings.TrimPrefix(value, "--")
		if i+1 < len(values) && !strings.HasPrefix(values[i+1], "--") {
			args[name] = values[i+1]
			i++
			continue
		}
		flags[name] = true
	}
}
