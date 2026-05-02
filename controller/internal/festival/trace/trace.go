// Package trace provides festival expectations for traceroute checks.
package trace

import (
	"fmt"
	"strings"
	"time"

	"dropcheck/controller/internal/controlpb"
	"dropcheck/controller/internal/festival"
)

// Result is the traceroute-specific view passed to custom assertions.
type Result struct {
	// Raw is the original protobuf payload for callers that need every field.
	Raw *controlpb.TracerouteResult
	// Status is the outer command status returned by the agent.
	Status controlpb.CommandResult_Status
	// Host is the traceroute destination.
	Host string
	// MaxHops is the max-hop value reported by the agent.
	MaxHops uint32
	// SizeBytes is the probe payload size.
	SizeBytes uint32
	// Elapsed is the agent-reported command duration.
	Elapsed time.Duration
	// ExitCode is the traceroute process exit code.
	ExitCode int32
	// Interface is the network interface used by the agent when known.
	Interface string
	// Output is the raw traceroute command output.
	Output string
	// Error is the agent-provided traceroute error text, when any.
	Error string
	// Executable is the traceroute implementation used by the agent.
	Executable string
}

// MaxHops matches the configured traceroute max-hop value reported by the agent.
func MaxHops() festival.OrderedMetric[uint32] {
	return festival.Ordered[uint32]("trace.max_hops", func(result festival.Result) (uint32, bool, string) {
		trace, ok, reason := from(result)
		return trace.MaxHops, ok, reason
	})
}

// ExitCode matches the traceroute process exit code.
func ExitCode() festival.OrderedMetric[int32] {
	return festival.Ordered[int32]("trace.exit_code", func(result festival.Result) (int32, bool, string) {
		trace, ok, reason := from(result)
		return trace.ExitCode, ok, reason
	})
}

// Elapsed matches the agent-reported elapsed time.
func Elapsed() festival.OrderedMetric[time.Duration] {
	return festival.Ordered[time.Duration]("trace.elapsed", func(result festival.Result) (time.Duration, bool, string) {
		trace, ok, reason := from(result)
		return trace.Elapsed, ok, reason
	})
}

// OutputContains requires the raw traceroute output to contain value.
func OutputContains(value string) festival.Expectation {
	return outputContains{value: value}
}

type outputContains struct {
	value string
}

func (e outputContains) Evaluate(result festival.Result) []festival.Finding {
	trace, ok, reason := from(result)
	metric := "trace.output_contains"
	if !ok {
		return []festival.Finding{festival.Fail(metric, "<missing>", e.value, reason)}
	}
	if strings.Contains(trace.Output, e.value) {
		return []festival.Finding{festival.Pass(metric, e.value, "contains "+e.value)}
	}
	return []festival.Finding{festival.Fail(metric, "<not found>", "contains "+e.value, "output did not contain value")}
}

// Assert evaluates a custom traceroute assertion against the typed result view.
func Assert(name string, fn func(Result) error) festival.Expectation {
	return assertion{name: name, fn: fn}
}

type assertion struct {
	name string
	fn   func(Result) error
}

func (a assertion) Evaluate(result festival.Result) []festival.Finding {
	trace, ok, reason := from(result)
	metric := "trace.assert." + a.name
	if !ok {
		return []festival.Finding{festival.Fail(metric, "<missing>", "custom assertion passed", reason)}
	}
	if err := a.fn(trace); err != nil {
		return []festival.Finding{festival.Fail(metric, "failed", "custom assertion passed", err.Error())}
	}
	return []festival.Finding{festival.Pass(metric, "passed", "custom assertion passed")}
}

func from(result festival.Result) (Result, bool, string) {
	raw := result.Run.Raw
	if raw == nil {
		return Result{}, false, "raw result is nil"
	}
	value := raw.GetTraceroute()
	if value == nil {
		return Result{}, false, fmt.Sprintf("command payload is %T, not traceroute", raw.GetPayload())
	}
	return Result{
		Raw:        value,
		Status:     raw.GetStatus(),
		Host:       value.GetHost(),
		MaxHops:    value.GetMaxHops(),
		SizeBytes:  value.GetSizeBytes(),
		Elapsed:    time.Duration(value.GetElapsedMs()) * time.Millisecond,
		ExitCode:   value.GetExitCode(),
		Interface:  value.GetInterfaceName(),
		Output:     value.GetOutput(),
		Error:      value.GetError(),
		Executable: value.GetExecutable(),
	}, true, ""
}
