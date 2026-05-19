// Package watchstate maintains UI-neutral state derived from watch events.
//
// The package is the boundary between the watch runner's event stream and any
// presentation layer. It applies events to target and step state, keeps bounded
// histories, builds passing and failed check summaries, ranks failure hotspots,
// and produces fixed-width time histograms. It deliberately avoids Bubble Tea,
// lipgloss, ANSI width handling, and terminal key state.
//
// Callers are expected to keep their own view state such as cursor positions,
// filters, and panel focus. State exposes summary methods that accept a filter
// string where needed so callers do not have to duplicate aggregation logic.
package watchstate
