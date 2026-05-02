// Package festival defines a Go DSL for Wi-Fi festival integration tests.
//
// A festival plan connects to one or more Wi-Fi networks in sequence, runs typed
// network checks such as ping, DNS, global IP, path MTU, and traceroute, then
// evaluates metric matchers and custom assertions against the raw agent result.
// Run integrates the plan with testing.T so callers get Go subtests, -run
// filtering, -json output, t.Cleanup, and standard failure reporting.
package festival
