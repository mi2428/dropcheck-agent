// Package linuxcli parses the non-interactive dropcheck command form.
//
// The package models the CLI UX where flags use Linux-style dash options and
// commands execute immediately. It deliberately stays separate from the
// interactive shell parser because the shell accepts a more conversational
// grammar, contextual help, and pipelines. Both frontends converge on
// command.Operation for agent execution.
package linuxcli
