// Package ping provides Dropcheck Harness expectations for ping checks.
package ping

import (
	"fmt"
	"time"

	"dropcheck/controller/internal/controlpb"
	"dropcheck/controller/internal/harness"
)

// Result is the ping-specific view passed to custom assertions.
type Result struct {
	// Raw is the original protobuf payload for callers that need every field.
	Raw *controlpb.PingResult
	// Status is the outer command status returned by the agent.
	Status controlpb.CommandResult_Status
	// Host is the ping destination.
	Host string
	// Count is the requested packet count.
	Count uint32
	// Transmitted is the number of packets the agent reports as sent.
	Transmitted uint32
	// Received is the number of replies the agent reports as received.
	Received uint32
	// LossPercent is the packet-loss percentage reported by the agent.
	LossPercent float64
	// MinLatency is the minimum RTT.
	MinLatency time.Duration
	// AvgLatency is the average RTT.
	AvgLatency time.Duration
	// MaxLatency is the maximum RTT.
	MaxLatency time.Duration
	// Elapsed is the agent-reported command duration.
	Elapsed time.Duration
	// ExitCode is the ping process exit code.
	ExitCode int32
	// Interface is the network interface used by the agent when known.
	Interface string
	// Output is the raw ping command output.
	Output string
}

// Transmitted matches the number of transmitted ping packets.
func Transmitted() harness.OrderedMetric[uint32] {
	return harness.Ordered[uint32]("ping.transmitted", func(result harness.Result) (uint32, bool, string) {
		ping, ok, reason := from(result)
		return ping.Transmitted, ok, reason
	})
}

// Received matches the number of received ping replies.
func Received() harness.OrderedMetric[uint32] {
	return harness.Ordered[uint32]("ping.received", func(result harness.Result) (uint32, bool, string) {
		ping, ok, reason := from(result)
		return ping.Received, ok, reason
	})
}

// LossPercent matches the packet loss percentage.
func LossPercent() harness.OrderedMetric[float64] {
	return harness.Ordered[float64]("ping.loss_percent", func(result harness.Result) (float64, bool, string) {
		ping, ok, reason := from(result)
		return ping.LossPercent, ok, reason
	})
}

// MinLatency matches the minimum ping latency.
func MinLatency() harness.OrderedMetric[time.Duration] {
	return durationMetric("ping.min_latency", func(result Result) time.Duration { return result.MinLatency })
}

// AvgLatency matches the average ping latency.
func AvgLatency() harness.OrderedMetric[time.Duration] {
	return durationMetric("ping.avg_latency", func(result Result) time.Duration { return result.AvgLatency })
}

// MaxLatency matches the maximum ping latency.
func MaxLatency() harness.OrderedMetric[time.Duration] {
	return durationMetric("ping.max_latency", func(result Result) time.Duration { return result.MaxLatency })
}

// Elapsed matches the agent-reported elapsed time.
func Elapsed() harness.OrderedMetric[time.Duration] {
	return durationMetric("ping.elapsed", func(result Result) time.Duration { return result.Elapsed })
}

// ExitCode matches the ping process exit code.
func ExitCode() harness.OrderedMetric[int32] {
	return harness.Ordered[int32]("ping.exit_code", func(result harness.Result) (int32, bool, string) {
		ping, ok, reason := from(result)
		return ping.ExitCode, ok, reason
	})
}

// Assert evaluates a custom ping assertion against the typed result view.
func Assert(name string, fn func(Result) error) harness.Expectation {
	return assertion{name: name, fn: fn}
}

type assertion struct {
	name string
	fn   func(Result) error
}

func (a assertion) Evaluate(result harness.Result) []harness.Finding {
	ping, ok, reason := from(result)
	metric := "ping.assert." + a.name
	if !ok {
		return []harness.Finding{harness.Fail(metric, "<missing>", "custom assertion passed", reason)}
	}
	if err := a.fn(ping); err != nil {
		return []harness.Finding{harness.Fail(metric, "failed", "custom assertion passed", err.Error())}
	}
	return []harness.Finding{harness.Pass(metric, "passed", "custom assertion passed")}
}

func durationMetric(name string, observe func(Result) time.Duration) harness.OrderedMetric[time.Duration] {
	return harness.Ordered[time.Duration](name, func(result harness.Result) (time.Duration, bool, string) {
		ping, ok, reason := from(result)
		return observe(ping), ok, reason
	})
}

func from(result harness.Result) (Result, bool, string) {
	raw := result.Run.Raw
	if raw == nil {
		return Result{}, false, "raw result is nil"
	}
	ping := raw.GetPing()
	if ping == nil {
		return Result{}, false, fmt.Sprintf("command payload is %T, not ping", raw.GetPayload())
	}
	return Result{
		Raw:         ping,
		Status:      raw.GetStatus(),
		Host:        ping.GetHost(),
		Count:       ping.GetCount(),
		Transmitted: ping.GetTransmitted(),
		Received:    ping.GetReceived(),
		LossPercent: ping.GetPacketLossPercent(),
		MinLatency:  durationFromMillis(ping.GetMinMs()),
		AvgLatency:  durationFromMillis(ping.GetAvgMs()),
		MaxLatency:  durationFromMillis(ping.GetMaxMs()),
		Elapsed:     time.Duration(ping.GetElapsedMs()) * time.Millisecond,
		ExitCode:    ping.GetExitCode(),
		Interface:   ping.GetInterfaceName(),
		Output:      ping.GetOutput(),
	}, true, ""
}

func durationFromMillis(value float64) time.Duration {
	return time.Duration(value * float64(time.Millisecond))
}
