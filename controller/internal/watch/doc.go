// Package watch loads watch plans and runs repeated connectivity checks.
//
// A watch plan comes from the YAML config consumed by `dropcheck watch -c`.
// The runner connects to each target, waits for Android network readiness, runs
// configured checks, emits structured events, and optionally records findings
// when observed metrics do not match expectations.
//
// This package does not render the live dashboard. Events are emitted to Sink
// implementations so callers can choose JSONL output, a TUI, tests, or another
// presentation without changing the watch execution path.
package watch
