package main

import (
	"slices"
	"testing"

	"dropcheck/controller/internal/controlpb"
)

func TestParseNativeTracerouteOutput(t *testing.T) {
	output := `
traceroute to 8.8.8.8 (8.8.8.8), 12 hops max
 1  router.local (192.168.23.254)  1.234 ms  1.456 ms
 2  * * *
 3  dns.google (8.8.8.8)  18.100 ms
`
	hops := parseTracerouteOutput(output, "8.8.8.8")

	if len(hops) != 3 {
		t.Fatalf("hops = %d, want 3: %#v", len(hops), hops)
	}
	if hops[0].Host != "router.local" || hops[0].Address != "192.168.23.254" {
		t.Fatalf("hop 1 = %#v", hops[0])
	}
	if !slices.Equal(hops[0].RttMs, []float64{1.234, 1.456}) {
		t.Fatalf("hop 1 rtt = %#v", hops[0].RttMs)
	}
	if !hops[1].TimedOut {
		t.Fatalf("hop 2 timedOut = false")
	}
	if !hops[2].ReachedTarget {
		t.Fatalf("hop 3 reachedTarget = false")
	}
}

func TestAnalyzeTracerouteRequiredHops(t *testing.T) {
	result := &controlpb.TracerouteResult{
		Host:       "8.8.8.8",
		MaxHops:    4,
		ExitCode:   0,
		Output:     "1  router.local (192.168.23.254)  1.234 ms\n",
		Executable: "traceroute",
	}

	analysis := analyzeTraceroute(result, []string{"192.168.23.254", "203.0.113.254"}, controlpb.CommandResult_STATUS_OK)

	if analysis.Status != "failed" {
		t.Fatalf("status = %q, want failed", analysis.Status)
	}
	if !slices.Equal(analysis.MatchedRequiredHops, []string{"192.168.23.254"}) {
		t.Fatalf("matched = %#v", analysis.MatchedRequiredHops)
	}
	if !slices.Equal(analysis.MissingRequiredHops, []string{"203.0.113.254"}) {
		t.Fatalf("missing = %#v", analysis.MissingRequiredHops)
	}
}
