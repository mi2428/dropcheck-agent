// Package watch loads and validates the YAML configuration for dropcheck watch.
// It compiles target/check data for internal/watchrun, which owns execution,
// policies, controls, events and cleanup. The dashboard and JSONL/no-TUI sinks
// consume that same event stream; there is no public scenario authoring API.
package watch
