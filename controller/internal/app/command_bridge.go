package app

import (
	"time"

	"dropcheck/controller/internal/command"
	"dropcheck/controller/internal/controlpb"
)

type Operation = command.Operation
type commandOptions = command.Options

func buildRunCommand(op Operation) (*controlpb.RunCommand, commandOptions, error) {
	return command.BuildRunCommand(op)
}

func buildCommand(args []string) (*controlpb.RunCommand, error) {
	return command.BuildCommand(args)
}

func buildCommandWithOptions(args []string) (*controlpb.RunCommand, commandOptions, error) {
	return command.BuildCommandWithOptions(args)
}

func timeoutFor(cmd *controlpb.RunCommand) time.Duration {
	return command.TimeoutFor(cmd)
}

func splitArgs(line string) ([]string, error) {
	return command.SplitArgs(line)
}

func redactedCommand(cmd *controlpb.RunCommand) *controlpb.RunCommand {
	return command.RedactedCommand(cmd)
}

func parseSecurity(value string) (controlpb.ConnectWifi_Security, error) {
	return command.ParseSecurity(value)
}

func parseWifiBand(value string) (controlpb.WifiBand, error) {
	return command.ParseWifiBand(value)
}

func parseMacRandomization(value string) (controlpb.ConnectWifi_MacRandomization, error) {
	return command.ParseMacRandomization(value)
}

func parseQTypes(value string) ([]controlpb.DnsRecordType, error) {
	return command.ParseQTypes(value)
}
