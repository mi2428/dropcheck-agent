// Package shell parses the Controller Shell language.
//
// The Controller Shell grammar favors interactive ergonomics: Junos-style operational,
// configure, and request modes, command prefixes, contextual help, completion
// candidates, and output pipelines including JSON, set-command rendering, and
// text filters. Agent-facing actions are returned as command.Operation values
// so execution remains shared with the Linux-style CLI without forcing both
// frontends into a single parser.
package shell
