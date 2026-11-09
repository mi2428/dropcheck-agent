// Package command builds typed control commands from dropcheck command models.
//
// ParseShellTokens parses the shared interactive hierarchy. It returns typed Operation
// values using the existing builders and validates inputs before dispatch;
// PC host flags and Shell pipelines are handled outside this package.
//
// Operation is also shared by tui YAML and the basic Go API. Callers that
// already know the command shape use its typed constructors directly.
package command
