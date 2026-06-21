// Package tui renders Controller TUI watch events as a Bubble Tea terminal dashboard.
//
// The package owns terminal concerns: key handling, focus and cursor state,
// responsive panel layout, lipgloss styling, and modal/detail composition.
// Watch event accumulation, retention, summaries, and histogram bucketing live
// in internal/watchstate so the rendering layer can stay focused on arranging
// already-derived data.
//
// Run is the package boundary used by the command-line application. The rest of
// the package intentionally remains unexported because the TUI is a single
// Bubble Tea program rather than a reusable widget library.
package tui
