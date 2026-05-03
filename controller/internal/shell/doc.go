// Package shell parses the interactive dropcheck shell language.
//
// The shell grammar favors interactive ergonomics: Junos-style operational,
// configure, and request modes, command prefixes, contextual help, completion
// candidates, and output pipelines. Agent-facing actions are returned as
// command.Operation values so execution remains shared with the Linux-style CLI
// without forcing both frontends into a single parser.
package shell
