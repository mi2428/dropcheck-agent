// Package tui renders Controller TUI tui events as a Bubble Tea terminal dashboard.
//
// The package owns terminal concerns: key handling, focus and cursor state,
// responsive panel layout, lipgloss styling, and modal/detail composition.
// TUI event accumulation, retention, summaries, and histogram bucketing live
// in internal/tuistate so the rendering layer can stay focused on arranging
// already-derived data.
//
// RunWithControls is the boundary used by dropcheck tui. The rest of
// the package intentionally remains unexported because the TUI is a single
// Bubble Tea program rather than a reusable widget library.
package tui
