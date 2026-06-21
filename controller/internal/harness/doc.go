// Package harness defines the Dropcheck Harness Go test harness.
//
// A Dropcheck Harness plan connects to one or more Wi-Fi networks in sequence,
// runs typed network checks such as IP provisioning, Wi-Fi link status, ping,
// DNS, global IP, path MTU, and traceroute, then evaluates metric matchers and
// custom assertions against the raw agent result.
//
// Plans can also evaluate saved standalone measurements by setting Results to
// StandaloneArchive, StandaloneArchiveBytes, or StandaloneArchiveFile sources.
// In that mode Run does not start an Android session; it replays archived
// command results through the same Check and Expect matcher path used by live
// Networks.
//
// Run integrates the plan with testing.T so callers get Go subtests, -run
// filtering, -json output, t.Cleanup, and standard failure reporting.
package harness
