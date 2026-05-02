// Package dns provides Dropcheck Festival expectations for DNS checks.
package dns

import (
	"fmt"
	"time"

	"dropcheck/controller/internal/controlpb"
	"dropcheck/controller/internal/festival"
)

// Result is the DNS-specific view passed to custom assertions.
type Result struct {
	// Raw is the original protobuf payload for callers that need every field.
	Raw *controlpb.ResolveDnsResult
	// Status is the outer command status returned by the agent.
	Status controlpb.CommandResult_Status
	// Name is the queried DNS name.
	Name string
	// Answers are the DNS answers returned by the agent.
	Answers []*controlpb.DnsAnswer
	// Elapsed is the agent-reported command duration.
	Elapsed time.Duration
	// Error is the agent-provided DNS error text, when any.
	Error string
}

// AnswerCount matches the number of returned DNS answers.
func AnswerCount() festival.OrderedMetric[int] {
	return festival.Ordered[int]("dns.answer_count", func(result festival.Result) (int, bool, string) {
		dns, ok, reason := from(result)
		return len(dns.Answers), ok, reason
	})
}

// Elapsed matches the agent-reported DNS elapsed time.
func Elapsed() festival.OrderedMetric[time.Duration] {
	return festival.Ordered[time.Duration]("dns.elapsed", func(result festival.Result) (time.Duration, bool, string) {
		dns, ok, reason := from(result)
		return dns.Elapsed, ok, reason
	})
}

// Assert evaluates a custom DNS assertion against the typed result view.
func Assert(name string, fn func(Result) error) festival.Expectation {
	return assertion{name: name, fn: fn}
}

type assertion struct {
	name string
	fn   func(Result) error
}

func (a assertion) Evaluate(result festival.Result) []festival.Finding {
	dns, ok, reason := from(result)
	metric := "dns.assert." + a.name
	if !ok {
		return []festival.Finding{festival.Fail(metric, "<missing>", "custom assertion passed", reason)}
	}
	if err := a.fn(dns); err != nil {
		return []festival.Finding{festival.Fail(metric, "failed", "custom assertion passed", err.Error())}
	}
	return []festival.Finding{festival.Pass(metric, "passed", "custom assertion passed")}
}

func from(result festival.Result) (Result, bool, string) {
	raw := result.Run.Raw
	if raw == nil {
		return Result{}, false, "raw result is nil"
	}
	dns := raw.GetResolveDns()
	if dns == nil {
		return Result{}, false, fmt.Sprintf("command payload is %T, not dns", raw.GetPayload())
	}
	return Result{
		Raw:     dns,
		Status:  raw.GetStatus(),
		Name:    dns.GetName(),
		Answers: dns.GetAnswers(),
		Elapsed: time.Duration(dns.GetElapsedMs()) * time.Millisecond,
		Error:   dns.GetError(),
	}, true, ""
}
