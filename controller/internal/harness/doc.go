// Package harness defines the Dropcheck Harness Go test harness.
//
// A Dropcheck Harness plan connects to one or more Wi-Fi networks in sequence,
// runs typed network checks such as IP provisioning, Wi-Fi link status, ping,
// DNS, global IP, path MTU, and traceroute, then evaluates metric matchers and
// custom assertions against the raw agent result.
//
// WithRunner injects an operation runner for device-free tests or callers that
// already manage a live session. Retry, Repeat, and StableFor take new measurements.
//
// Run integrates the plan with testing.T so callers get Go subtests, -run
// filtering, -json output, t.Cleanup, and standard failure reporting.
package harness
