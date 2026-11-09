// Package app wires the dropcheck command-line entry point to the controller
// packages.
//
// This package owns process-level concerns: top-level flag parsing, target
// selection, control-session startup, Shell/tui dispatch, and output
// printing. Domain parsing, rendering, gRPC control, and Android process
// management are delegated to the internal command, render, control, and
// session packages so cmd/dropcheck can remain a thin executable wrapper.
package app
