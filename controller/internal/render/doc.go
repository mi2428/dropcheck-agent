// Package render formats safe, lossless controller and agent text.
//
// The package consumes plain view structs and protocol-buffer command results.
// It does not depend on shell state or session orchestration, which keeps
// rendering reusable from Controller Shell and historical reports. The Shell
// pipeline owns its JSON envelope; safevalue is shared with tui JSONL export.
package render
