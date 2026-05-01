package command

import (
	"fmt"
	"strings"

	"dropcheck/controller/internal/controlpb"
	"google.golang.org/protobuf/proto"
)

type TargetSpec struct {
	Selector string
	All      bool
}

type Operation struct {
	Name    string
	Command *controlpb.RunCommand
	Options Options
	Args    map[string]string
	Flags   map[string]bool
	Target  TargetSpec

	buildErr error
}

func newOperation(name string, cmd *controlpb.RunCommand, options Options, args map[string]string, flags map[string]bool, buildErr error) Operation {
	if args == nil {
		args = map[string]string{}
	}
	if flags == nil {
		flags = map[string]bool{}
	}
	return Operation{
		Name:     name,
		Command:  cmd,
		Options:  options,
		Args:     args,
		Flags:    flags,
		buildErr: buildErr,
	}
}

func NewOperation(name string, cmd *controlpb.RunCommand, options Options, args map[string]string, flags map[string]bool) Operation {
	return newOperation(name, cloneRunCommand(cmd), options, args, flags, nil)
}

func OperationFromCommandArgs(commandArgs []string) Operation {
	args := map[string]string{}
	flags := map[string]bool{}
	name := operationNameFromCommandArgs(commandArgs)
	collectOperationArgs(commandArgs, args, flags)
	cmd, options, err := BuildCommandWithOptions(commandArgs)
	return newOperation(name, cmd, options, args, flags, err)
}

func BuildRunCommand(op Operation) (*controlpb.RunCommand, Options, error) {
	if op.buildErr != nil {
		return nil, Options{}, op.buildErr
	}
	if op.Command == nil {
		return nil, Options{}, fmt.Errorf("operation %q has no command adapter", op.Name)
	}
	return cloneRunCommand(op.Command), op.Options, nil
}

func cloneRunCommand(cmd *controlpb.RunCommand) *controlpb.RunCommand {
	if cmd == nil {
		return nil
	}
	return proto.Clone(cmd).(*controlpb.RunCommand)
}

func operationNameFromCommandArgs(args []string) string {
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

func collectOperationArgs(commandArgs []string, args map[string]string, flags map[string]bool) {
	if len(commandArgs) == 0 {
		return
	}
	switch commandArgs[0] {
	case "wifi":
		collectWifiOperationArgs(commandArgs, args, flags)
	case "ping":
		if len(commandArgs) >= 2 {
			args["host"] = commandArgs[1]
		}
		if len(commandArgs) >= 3 && !strings.HasPrefix(commandArgs[2], "--") {
			args["count"] = commandArgs[2]
			collectLongOptions(commandArgs[3:], args, flags)
			return
		}
		collectLongOptions(commandArgs[2:], args, flags)
	case "traceroute":
		if len(commandArgs) >= 2 {
			args["host"] = commandArgs[1]
		}
		if len(commandArgs) >= 3 && !strings.HasPrefix(commandArgs[2], "--") {
			args["max-hops"] = commandArgs[2]
			collectLongOptions(commandArgs[3:], args, flags)
			return
		}
		collectLongOptions(commandArgs[2:], args, flags)
	case "path-mtu":
		if len(commandArgs) >= 2 {
			args["host"] = commandArgs[1]
		}
		collectLongOptions(commandArgs[2:], args, flags)
	case "global-ip":
		if len(commandArgs) >= 2 && !strings.HasPrefix(commandArgs[1], "--") {
			args["family"] = commandArgs[1]
			collectLongOptions(commandArgs[2:], args, flags)
			return
		}
		collectLongOptions(commandArgs[1:], args, flags)
	case "download":
		if len(commandArgs) >= 2 {
			args["url"] = commandArgs[1]
		}
		collectLongOptions(commandArgs[2:], args, flags)
	case "dns":
		if len(commandArgs) >= 2 {
			args["name"] = commandArgs[1]
		}
		if len(commandArgs) >= 3 && !strings.HasPrefix(commandArgs[2], "--") {
			args["type"] = commandArgs[2]
			collectLongOptions(commandArgs[3:], args, flags)
			return
		}
		collectLongOptions(commandArgs[2:], args, flags)
	case "http":
		if len(commandArgs) >= 2 {
			args["url"] = commandArgs[1]
		}
		if len(commandArgs) >= 3 && !strings.HasPrefix(commandArgs[2], "--") {
			args["expected-status"] = commandArgs[2]
			collectLongOptions(commandArgs[3:], args, flags)
			return
		}
		collectLongOptions(commandArgs[2:], args, flags)
	}
}

func collectWifiOperationArgs(commandArgs []string, args map[string]string, flags map[string]bool) {
	if len(commandArgs) < 2 {
		return
	}
	switch commandArgs[1] {
	case "connect", "cycle":
		if len(commandArgs) >= 3 {
			args["ssid"] = commandArgs[2]
		}
		if len(commandArgs) >= 4 {
			args["passphrase"] = commandArgs[3]
		}
		rest := commandArgs[4:]
		if len(rest) > 0 && !strings.HasPrefix(rest[0], "--") {
			args["security"] = rest[0]
			rest = rest[1:]
		}
		collectLongOptions(rest, args, flags)
	case "scan":
		if len(commandArgs) >= 3 {
			switch commandArgs[2] {
			case "fresh":
				args["scan-mode"] = "fresh"
				if len(commandArgs) >= 4 && !strings.HasPrefix(commandArgs[3], "--") {
					args["band"] = commandArgs[3]
					collectLongOptions(commandArgs[4:], args, flags)
					return
				}
				collectLongOptions(commandArgs[3:], args, flags)
			case "detail":
				args["scan-mode"] = "detail"
				if len(commandArgs) >= 4 {
					args["target"] = commandArgs[3]
				}
				if len(commandArgs) >= 5 {
					args["band"] = commandArgs[4]
				}
			default:
				args["band"] = commandArgs[2]
			}
		}
	case "forget":
		if len(commandArgs) >= 3 {
			args["target"] = commandArgs[2]
		}
	case "wait":
		if len(commandArgs) >= 3 {
			args["state"] = commandArgs[2]
		}
		if len(commandArgs) >= 4 && !strings.HasPrefix(commandArgs[3], "--") {
			args["ssid"] = commandArgs[3]
			collectLongOptions(commandArgs[4:], args, flags)
			return
		}
		collectLongOptions(commandArgs[3:], args, flags)
	case "assert":
		collectLongOptions(commandArgs[2:], args, flags)
	case "watch", "monitor":
		if len(commandArgs) >= 3 {
			args["duration"] = commandArgs[2]
		}
		if len(commandArgs) >= 4 {
			args["interval"] = commandArgs[3]
		}
	case "reconnect":
		if len(commandArgs) >= 3 {
			args["timeout"] = commandArgs[2]
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
