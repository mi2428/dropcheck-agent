package render

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"unicode"

	"github.com/mi2428/dropcheck-agent/controller/internal/command"
	"github.com/mi2428/dropcheck-agent/controller/internal/controlpb"
	"github.com/mi2428/dropcheck-agent/controller/internal/pipeline"
	"google.golang.org/protobuf/proto"
)

func alignmentCompact(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, text)
}

func TestAlignmentQAEveryPayloadAndWidth(t *testing.T) {
	fields := (&controlpb.CommandResult{}).ProtoReflect().Descriptor().Oneofs().ByName("payload").Fields()
	for i := 0; i < fields.Len(); i++ {
		field := fields.Get(i)
		t.Run(string(field.Name()), func(t *testing.T) {
			result := &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_FAILED, Message: "password=alignment-private-marker"}
			message := result.ProtoReflect()
			message.Set(field, message.NewField(field))
			before := proto.Clone(result)
			for _, width := range []int{8, 24, 40, 80, 120} {
				out, err := CommandResult("target", result, command.Options{}, Presentation{Width: width})
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(alignmentCompact(out), "FAILED") || strings.Contains(out, "alignment-private-marker") || strings.ContainsRune(out, '\t') {
					t.Fatalf("width=%d lost outcome, leaked credentials or field delimiter", width)
				}
				for line := range strings.SplitSeq(out, "\n") {
					if displayWidth(line) > width {
						t.Fatalf("width=%d overflow: %q", width, line)
					}
				}
			}
			logical, err := CommandResult("target", result, command.Options{})
			if err != nil {
				t.Fatal(err)
			}
			out, err := pipeline.JSON(logical, false)
			if err != nil || !json.Valid([]byte(out)) || strings.Contains(out, "alignment-private-marker") || !proto.Equal(result, before) {
				t.Fatal("layout changed JSON, privacy or acquisition data", err)
			}
		})
	}
}

func TestAlignmentQAUnicodeTablesAcrossCommandFamilies(t *testing.T) {
	marker := "日本語e\u0301👩\u200d🔬"
	ip := "2001:db8:ffff:ffff:ffff:ffff:ffff:ffff"
	cases := []*controlpb.CommandResult{
		{Payload: &controlpb.CommandResult_Traceroute{Traceroute: &controlpb.TracerouteResult{Host: marker, Hops: []*controlpb.TracerouteHop{{Index: 1, Hostnames: []string{marker}, Addresses: []string{ip}}}}}},
		{Payload: &controlpb.CommandResult_WifiAssert{WifiAssert: &controlpb.WifiAssertResult{Checks: []*controlpb.DiagnosticCheck{{Key: marker, Actual: ip, Expected: "0", Message: "failure"}}}}},
		{Payload: &controlpb.CommandResult_WifiMonitor{WifiMonitor: &controlpb.WifiMonitorResult{Events: []*controlpb.WifiEvent{{Type: marker, Message: ip}}}}},
		{Payload: &controlpb.CommandResult_WifiCycle{WifiCycle: &controlpb.WifiCycleResult{Steps: []*controlpb.WifiCycleStep{{Index: 1, Connect: &controlpb.ConnectWifiResult{Ssid: marker}, Errors: []string{ip}}}}}},
		{Payload: &controlpb.CommandResult_GlobalIp{GlobalIp: &controlpb.GlobalIpResult{Addresses: []*controlpb.GlobalIpAddress{{Ip: ip, Error: marker}}}}},
		{Payload: &controlpb.CommandResult_ResolveDns{ResolveDns: &controlpb.ResolveDnsResult{Name: marker, Answers: []*controlpb.DnsAnswer{{Address: ip}}}}},
	}
	for _, result := range cases {
		result.Status = controlpb.CommandResult_STATUS_FAILED
		for _, width := range []int{24, 40, 80, 120} {
			out, err := CommandResult("target", result, command.Options{}, Presentation{Width: width})
			if err != nil {
				t.Fatal(err)
			}
			compact := alignmentCompact(out)
			if !strings.Contains(compact, marker) || !strings.Contains(compact, ip) {
				t.Fatalf("%T width=%d lost Unicode/IPv6 values: %s", result.Payload, width, out)
			}
			for line := range strings.SplitSeq(out, "\n") {
				if displayWidth(line) > width {
					t.Fatalf("%T width=%d table overflow: %q", result.Payload, width, line)
				}
			}
		}
	}
	var b strings.Builder
	writeDisplayTable(&b, []displayTableColumn{{header: "NAME"}, {header: "VALUE"}, {header: "AGE(s)"}}, [][]string{{"日本語", "v1", "0"}, {"ascii", "v2", "123.456"}})
	out := b.String()
	column := displayColumn(renderedLineContaining(out, "AGE(s)"), "AGE(s)")
	for _, age := range []string{"0", "123.456"} {
		if displayColumn(renderedLineContaining(out, age), age) != column {
			t.Fatal("AGE is not left-aligned with its header", out)
		}
	}
	if !strings.Contains(out, "NAME    VALUE  AGE(s)") {
		t.Fatal("table gap is not two display cells", out)
	}
}

func TestAlignmentQAFieldsAndRawBoundaries(t *testing.T) {
	rows := [][2]string{{"短い", "first"}, {"Long label", "second"}, {"URL", "https://example.test/" + strings.Repeat("x", 80)}}
	wide := Fields(rows, Presentation{Width: 120})
	for i, value := range []string{"first", "second", "https://example.test/"} {
		line := renderedLineContaining(wide, value)
		if displayColumn(line, value) != 14 || !strings.HasPrefix(strings.TrimLeft(line, " "), rows[i][0]+":") {
			t.Fatalf("field value columns differ or colon is detached: %q", line)
		}
	}
	for _, width := range []int{8, 24, 40} {
		out := Fields(rows, Presentation{Width: width})
		for _, row := range rows {
			if !strings.Contains(alignmentCompact(out), row[1]) {
				t.Fatalf("field value clipped at width=%d", width)
			}
		}
	}
	narrow := Fields([][2]string{{strings.Repeat("Long label ", 8), "value"}}, Presentation{Width: 24})
	for line := range strings.SplitSeq(strings.TrimSuffix(narrow, "\n"), "\n") {
		if !strings.HasPrefix(line, "  ") {
			t.Fatalf("wrapped label escaped its indentation: %q", line)
		}
		if strings.TrimSpace(line) == ":" {
			t.Fatalf("wrapped colon detached from label: %q", line)
		}
	}
	raw := "Raw:    untouched:    spacing\nhttps://example.test/a:b"
	if got := TextBlock(raw, Presentation{}); got != raw+"\n" {
		t.Fatal("raw/URL text was guessed to be structured fields", got)
	}
}

func TestAlignmentQAKVContinuationSharesTheValueColumn(t *testing.T) {
	for _, width := range []int{24, 40, 80, 120} {
		var b strings.Builder
		value := strings.Repeat("2001:db8::1", 4) + "\nnext-value"
		writeKVRows(&b, kv("a", value), kv("longer label", "false"))
		out := wrapPresentation(b.String(), width)
		lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
		column := strings.Index(lines[0], "2001")
		if column < 0 || !strings.HasPrefix(strings.TrimLeft(lines[0], " "), "a:") {
			t.Fatalf("missing first field: %q", out)
		}
		for _, line := range lines[1 : len(lines)-1] {
			if len(line)-len(strings.TrimLeft(line, " ")) != column || displayWidth(line) > width {
				t.Fatalf("width=%d continuation escaped its value column: %q", width, line)
			}
		}
		if !strings.Contains(alignmentCompact(out), alignmentCompact(value)) || displayColumn(lines[len(lines)-1], "false") != column {
			t.Fatalf("width=%d value lost or columns differ: %q", width, out)
		}
	}
}

func TestAlignmentQAEmptyDNSRetainsNarrowHeaders(t *testing.T) {
	result := &controlpb.CommandResult{Payload: &controlpb.CommandResult_ResolveDns{ResolveDns: &controlpb.ResolveDnsResult{}}}
	for _, width := range []int{8, 24, 80} {
		out, err := CommandResult("self", result, command.Options{}, Presentation{Width: width})
		if err != nil || !strings.Contains(alignmentCompact(out), "TYPE") || !strings.Contains(alignmentCompact(out), "ADDRESS") || !strings.HasSuffix(out, "\n\n") {
			t.Fatalf("width=%d empty DNS headers or boundary lost: %q %v", width, out, err)
		}
		for line := range strings.SplitSeq(out, "\n") {
			if displayWidth(line) > width {
				t.Fatalf("width=%d empty DNS overflow: %q", width, line)
			}
		}
	}
}

func TestAlignmentQAExactSecondsAndSecurityAliases(t *testing.T) {
	for ms, want := range map[uint64]string{0: "0", 1: "0.001", 10: "0.01", 999: "0.999", 1000: "1", 1001: "1.001", math.MaxUint64: "18446744073709551.615"} {
		if got := scanAgeSeconds(&controlpb.WifiScanResult{ObservationAgeMs: &ms}); got != want {
			t.Fatalf("%dms -> %s, want %s", ms, got, want)
		}
	}
	if scanAgeSeconds(&controlpb.WifiScanResult{}) != "?" {
		t.Fatal("missing age became zero")
	}
	if got := compactSecuritySet([]string{"eap_wpa3_enterprise", "future_Mode"}); got != "wpa3-eap+future_Mode" {
		t.Fatalf("security spelling lost: %s", got)
	}
}

func TestAlignmentQALiteralSelectionKeepsOneDetachedContext(t *testing.T) {
	wanted := &controlpb.WifiConnection{Ssid: "Guest\t東京", Bssid: "02:00:00:11:22:01"}
	other := &controlpb.WifiConnection{Ssid: "Guest 東京", Bssid: "02:00:00:11:22:02"}
	result := &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_FAILED, Payload: &controlpb.CommandResult_WifiDiagnostics{WifiDiagnostics: &controlpb.WifiDiagnostics{
		Status: &controlpb.WifiStatus{Connection: wanted, IpStatus: &controlpb.IpStatus{Wifi: other}},
		Scan: &controlpb.WifiScan{Results: []*controlpb.WifiScanResult{{Ssid: wanted.Ssid, Bssid: wanted.Bssid}, {Ssid: other.Ssid, Bssid: other.Bssid}},
			Fields: []*controlpb.DiagnosticField{{Key: "scan_result_total_count", Value: "100"}}},
		Networks: []*controlpb.NetworkDiagnostics{{IpStatus: &controlpb.IpStatus{Wifi: wanted}}, {IpStatus: &controlpb.IpStatus{Wifi: other}}, nil},
	}}}
	before := proto.Clone(result)
	for _, identity := range []*controlpb.WifiConnection{wanted, other} {
		selected := presentationResult(result, command.Options{WifiRenderMode: command.WifiRenderModeEHT, WifiEHTSSID: identity.Ssid})
		d := selected.GetWifiDiagnostics()
		if len(d.Scan.Results) != 1 || d.Scan.Results[0].Bssid != identity.Bssid || len(d.Networks) != 1 || d.Networks[0].GetIpStatus().GetWifi().Bssid != identity.Bssid {
			t.Fatal("selection included another observation")
		}
		if identity == wanted && (d.Status.Connection == nil || d.Status.IpStatus != nil) || identity == other && (d.Status.Connection != nil || d.Status.IpStatus == nil) {
			t.Fatal("connection and independent IP context did not retain literal selection")
		}
		if selected.Status != result.Status || diagnosticFieldMap(d.Scan.Fields)["scan_result_total_count"] != "100" || !proto.Equal(result, before) {
			t.Fatal("presentation changed acquisition outcome, totals or original data")
		}
	}
}
