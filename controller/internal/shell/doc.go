// Package shell parses the interactive dropcheck shell language.
//
// The shell grammar favors interactive ergonomics: command prefixes, contextual
// help, completion candidates, target-management commands, and output pipelines.
// Agent-facing actions are returned as command.Operation values so execution
// remains shared with the Linux-style CLI without forcing both frontends into a
// single parser.
package shell
