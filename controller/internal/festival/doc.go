// Package festival defines a Go DSL for Dropcheck Festival integration tests.
//
// A Dropcheck Festival plan connects to one or more Wi-Fi networks in sequence,
// runs typed network checks such as IP provisioning, Wi-Fi link status, ping,
// DNS, global IP, path MTU, and traceroute, then evaluates metric matchers and
// custom assertions against the raw agent result. Run integrates the plan with
// testing.T so callers get Go subtests, -run filtering, -json output,
// t.Cleanup, and standard failure reporting.
package festival
