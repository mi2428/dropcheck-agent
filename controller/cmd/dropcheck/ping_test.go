package main

import "testing"

func TestParsePingOutputLinuxStats(t *testing.T) {
	output := `
PING example.test (93.184.216.34) 56(84) bytes of data.
64 bytes from 93.184.216.34: icmp_seq=1 ttl=57 time=12.3 ms

--- example.test ping statistics ---
3 packets transmitted, 3 received, 0% packet loss, time 2002ms
rtt min/avg/max/mdev = 12.345/20.456/33.789/1.000 ms
`
	stats, ok := parsePingOutput(output)

	if !ok {
		t.Fatalf("parsePingOutput() ok = false")
	}
	if stats.transmitted != 3 || stats.received != 3 {
		t.Fatalf("packets = %d/%d", stats.transmitted, stats.received)
	}
	if stats.lossPercent != 0 {
		t.Fatalf("loss = %f", stats.lossPercent)
	}
	if stats.minMs != 12.345 || stats.avgMs != 20.456 || stats.maxMs != 33.789 {
		t.Fatalf("rtt = %f/%f/%f", stats.minMs, stats.avgMs, stats.maxMs)
	}
}

func TestParsePingOutputToyboxSummary(t *testing.T) {
	output := "2 packets transmitted, 1 packets received, 50% packet loss"
	stats, ok := parsePingOutput(output)

	if !ok {
		t.Fatalf("parsePingOutput() ok = false")
	}
	if stats.transmitted != 2 || stats.received != 1 || stats.lossPercent != 50 {
		t.Fatalf("stats = %#v", stats)
	}
}

func TestParsePingOutputMissingSummary(t *testing.T) {
	if _, ok := parsePingOutput("permission denied"); ok {
		t.Fatalf("parsePingOutput() ok = true")
	}
}
