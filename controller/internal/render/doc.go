// Package render formats controller and agent data for human, JSON, and
// copy-pasteable configuration output.
//
// The package consumes plain view structs and protocol-buffer command results.
// It does not depend on shell state or session orchestration, which keeps
// rendering reusable from both the interactive shell and the Linux-style CLI.
package render
