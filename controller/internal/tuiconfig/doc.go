// Package tuiconfig loads and validates the YAML configuration for dropcheck tui.
// It compiles target/check data for internal/tuirun, which owns execution,
// policies, controls, events and cleanup. The dashboard and JSONL/no-TUI sinks
// consume that same event stream.
package tuiconfig
