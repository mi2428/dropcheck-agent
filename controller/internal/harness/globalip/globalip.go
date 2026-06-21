// Package globalip provides Dropcheck Harness expectations for global-ip checks.
package globalip

import (
	"fmt"
	"time"

	"dropcheck/controller/internal/controlpb"
	"dropcheck/controller/internal/harness"
)

// Result is the global-IP-specific view passed to custom assertions.
type Result struct {
	// Raw is the original protobuf payload for callers that need every field.
	Raw *controlpb.GlobalIpResult
	// Status is the outer command status returned by the agent.
	Status controlpb.CommandResult_Status
	// Service is the public-IP service used by the agent.
	Service string
	// RequestedFamily is the requested IP family.
	RequestedFamily controlpb.IpFamily
	// Addresses are the public IP observations returned by the agent.
	Addresses []*controlpb.GlobalIpAddress
	// Elapsed is the agent-reported command duration.
	Elapsed time.Duration
	// Interface is the network interface used by the agent when known.
	Interface string
	// Error is the agent-provided global-IP error text, when any.
	Error string
}

// AddressCount matches the number of returned global IP addresses.
func AddressCount() harness.OrderedMetric[int] {
	return harness.Ordered[int]("global_ip.address_count", func(result harness.Result) (int, bool, string) {
		global, ok, reason := from(result)
		return len(global.Addresses), ok, reason
	})
}

// Elapsed matches the agent-reported elapsed time.
func Elapsed() harness.OrderedMetric[time.Duration] {
	return harness.Ordered[time.Duration]("global_ip.elapsed", func(result harness.Result) (time.Duration, bool, string) {
		global, ok, reason := from(result)
		return global.Elapsed, ok, reason
	})
}

// Assert evaluates a custom global-IP assertion against the typed result view.
func Assert(name string, fn func(Result) error) harness.Expectation {
	return assertion{name: name, fn: fn}
}

type assertion struct {
	name string
	fn   func(Result) error
}

func (a assertion) Evaluate(result harness.Result) []harness.Finding {
	global, ok, reason := from(result)
	metric := "global_ip.assert." + a.name
	if !ok {
		return []harness.Finding{harness.Fail(metric, "<missing>", "custom assertion passed", reason)}
	}
	if err := a.fn(global); err != nil {
		return []harness.Finding{harness.Fail(metric, "failed", "custom assertion passed", err.Error())}
	}
	return []harness.Finding{harness.Pass(metric, "passed", "custom assertion passed")}
}

func from(result harness.Result) (Result, bool, string) {
	raw := result.Run.Raw
	if raw == nil {
		return Result{}, false, "raw result is nil"
	}
	global := raw.GetGlobalIp()
	if global == nil {
		return Result{}, false, fmt.Sprintf("command payload is %T, not global-ip", raw.GetPayload())
	}
	return Result{
		Raw:             global,
		Status:          raw.GetStatus(),
		Service:         global.GetService(),
		RequestedFamily: global.GetRequestedFamily(),
		Addresses:       global.GetAddresses(),
		Elapsed:         time.Duration(global.GetElapsedMs()) * time.Millisecond,
		Interface:       global.GetInterfaceName(),
		Error:           global.GetError(),
	}, true, ""
}
