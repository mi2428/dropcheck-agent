// Package tui renders controller events as a Bubble Tea terminal dashboard.
//
// The package owns terminal concerns: key handling, focus and cursor state,
// responsive panel layout, lipgloss styling, and action-dialog/inspector composition.
// TUI event accumulation, retention, summaries, and histogram bucketing live
// in internal/tuistate so the rendering layer can stay focused on arranging
// already-derived data.
package tui
