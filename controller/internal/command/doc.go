// Package command builds typed control commands from dropcheck command models.
//
// The package is the shared command boundary between the Linux-style CLI and
// the interactive shell. Parsers in those packages translate their own UX into
// Operation values; this package validates command-specific options, applies
// defaults, normalizes short prefixes, redacts secrets, and produces
// controlpb.RunCommand messages for execution by the control server.
//
// Operation is the preferred intermediate representation. The older argv-shaped
// adapter is intentionally not part of this API: callers should construct
// operations with the typed builder functions whenever they already understand
// the command shape.
package command
