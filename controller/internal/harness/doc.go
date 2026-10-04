// Package harness defines the common, testing.T-independent scenario runtime.
//
// A Dropcheck Harness plan connects to one or more Wi-Fi networks in sequence,
// runs typed network checks such as IP provisioning, Wi-Fi link status, ping,
// DNS, global IP, path MTU, and traceroute, then evaluates metric matchers and
// custom assertions against the raw agent result.
//
// WithRunner injects an operation runner for device-free tests or callers that
// already manage a live session. Retry, Repeat, and StableFor take new measurements.
//
// Compile freezes and validates the whole Plan, and Preview/Select operate
// without dispatch. Execute returns an immutable typed report and ordered
// events. Run is a thin testing.T adapter over those same functions.
//
// Policy.Attempts includes the first attempt; Repeat is independent. Eventually
// bounds polling until the first pass, then StableFor samples over a window and
// permits retry recovery within each sample. This is not a zero-interruption
// guarantee. Go-only custom assertions and raw payload access remain available;
// they are not serialized as YAML or Android callbacks.
package harness
