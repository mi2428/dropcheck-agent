// Package pmtu provides Dropcheck Festival expectations for path-MTU checks.
package pmtu

import (
	"fmt"
	"time"

	"dropcheck/controller/internal/controlpb"
	"dropcheck/controller/internal/festival"
)

// Result is the path-MTU-specific view passed to custom assertions.
type Result struct {
	// Raw is the original protobuf payload for callers that need every field.
	Raw *controlpb.PathMtuResult
	// Status is the outer command status returned by the agent.
	Status controlpb.CommandResult_Status
	// Host is the path-MTU destination.
	Host string
	// Discovered reports whether MTU discovery converged.
	Discovered bool
	// PathMTU is the largest passing IP MTU in bytes.
	PathMTU uint32
	// PayloadSize is the ping payload size corresponding to PathMTU.
	PayloadSize uint32
	// MinMTU is the lower search bound in bytes.
	MinMTU uint32
	// MaxMTU is the upper search bound in bytes.
	MaxMTU uint32
	// Elapsed is the agent-reported command duration.
	Elapsed time.Duration
	// Interface is the network interface used by the agent when known.
	Interface string
	// Error is the agent-provided path-MTU error text, when any.
	Error string
	// Probes are the individual MTU probes performed by the agent.
	Probes []*controlpb.PathMtuProbe
	// ProbeCount is len(Probes), exposed for matcher convenience.
	ProbeCount int
	// PassedProbes is the number of probes that succeeded.
	PassedProbes int
	// FailedProbes is the number of probes that failed.
	FailedProbes int
	// OverheadBytes is the IP+ICMP overhead used for payload sizing.
	OverheadBytes uint32
}

// Discovered matches whether path-MTU discovery converged.
func Discovered() festival.BoolMetric {
	return festival.Bool("pmtu.discovered", func(result festival.Result) (bool, bool, string) {
		pmtu, ok, reason := from(result)
		return pmtu.Discovered, ok, reason
	})
}

// PathMTU matches the discovered MTU in bytes.
func PathMTU() festival.OrderedMetric[uint32] {
	return festival.Ordered[uint32]("pmtu.path_mtu_bytes", func(result festival.Result) (uint32, bool, string) {
		pmtu, ok, reason := from(result)
		return pmtu.PathMTU, ok, reason
	})
}

// ProbeCount matches the number of MTU probes performed.
func ProbeCount() festival.OrderedMetric[int] {
	return festival.Ordered[int]("pmtu.probe_count", func(result festival.Result) (int, bool, string) {
		pmtu, ok, reason := from(result)
		return pmtu.ProbeCount, ok, reason
	})
}

// Elapsed matches the agent-reported elapsed time.
func Elapsed() festival.OrderedMetric[time.Duration] {
	return festival.Ordered[time.Duration]("pmtu.elapsed", func(result festival.Result) (time.Duration, bool, string) {
		pmtu, ok, reason := from(result)
		return pmtu.Elapsed, ok, reason
	})
}

// Assert evaluates a custom path-MTU assertion against the typed result view.
func Assert(name string, fn func(Result) error) festival.Expectation {
	return assertion{name: name, fn: fn}
}

type assertion struct {
	name string
	fn   func(Result) error
}

func (a assertion) Evaluate(result festival.Result) []festival.Finding {
	pmtu, ok, reason := from(result)
	metric := "pmtu.assert." + a.name
	if !ok {
		return []festival.Finding{festival.Fail(metric, "<missing>", "custom assertion passed", reason)}
	}
	if err := a.fn(pmtu); err != nil {
		return []festival.Finding{festival.Fail(metric, "failed", "custom assertion passed", err.Error())}
	}
	return []festival.Finding{festival.Pass(metric, "passed", "custom assertion passed")}
}

func from(result festival.Result) (Result, bool, string) {
	raw := result.Run.Raw
	if raw == nil {
		return Result{}, false, "raw result is nil"
	}
	value := raw.GetPathMtu()
	if value == nil {
		return Result{}, false, fmt.Sprintf("command payload is %T, not path-mtu", raw.GetPayload())
	}
	passed := 0
	for _, probe := range value.GetProbes() {
		if probe.GetPassed() {
			passed++
		}
	}
	probes := value.GetProbes()
	return Result{
		Raw:           value,
		Status:        raw.GetStatus(),
		Host:          value.GetHost(),
		Discovered:    value.GetDiscovered(),
		PathMTU:       value.GetPathMtuBytes(),
		PayloadSize:   value.GetPayloadSizeBytes(),
		MinMTU:        value.GetMinMtuBytes(),
		MaxMTU:        value.GetMaxMtuBytes(),
		Elapsed:       time.Duration(value.GetElapsedMs()) * time.Millisecond,
		Interface:     value.GetInterfaceName(),
		Error:         value.GetError(),
		Probes:        probes,
		ProbeCount:    len(probes),
		PassedProbes:  passed,
		FailedProbes:  len(probes) - passed,
		OverheadBytes: value.GetIpOverheadBytes(),
	}, true, ""
}
