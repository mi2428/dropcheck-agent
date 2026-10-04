// Package command builds typed control commands from dropcheck command models.
//
// ParseTokens is the common argv/Shell grammar. It returns typed Operation
// values using the existing builders and validates inputs before dispatch;
// PC host flags and Shell pipelines are handled outside this package.
//
// Operation is the preferred intermediate representation. The older argv-shaped
// adapter is intentionally not part of this API: callers should construct
// operations with the typed builder functions whenever they already understand
// the command shape.
package command
