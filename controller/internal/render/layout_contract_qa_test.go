package render

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mi2428/dropcheck-agent/controller/internal/command"
	"github.com/mi2428/dropcheck-agent/controller/internal/control"
	"github.com/mi2428/dropcheck-agent/controller/internal/controlpb"
	"github.com/mi2428/dropcheck-agent/controller/internal/pipeline"
	"github.com/mi2428/dropcheck-agent/controller/internal/shell"
	"github.com/mi2428/dropcheck-agent/controller/internal/watchrun"
	"google.golang.org/protobuf/proto"
)

func TestLayoutContractColonAfterLabel(t *testing.T) {
	for _, width := range []int{8, 24, 40, 80, 120} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			want := "  a:    1\n  bc:   2\n  defg: 3\n"
			if width == 8 {
				want = "  a:\n    1\n  bc:\n    2\n  defg:\n    3\n"
			}
			if got := Fields([][2]string{{"a", "1"}, {"bc", "2"}, {"defg", "3"}}, Presentation{Width: width}); got != want {
				t.Errorf("got %q; want %q", got, want)
			}
		})
	}
}

// Only inspect known typed labels; never infer fields from raw values or URLs.
func layoutContractFields(t *testing.T, text string, labels ...string) {
	t.Helper()
	column := -1
	for _, label := range labels {
		found := false
		for line := range strings.SplitSeq(text, "\n") {
			plain := strings.TrimLeft(line, " ")
			if !strings.HasPrefix(plain, label) {
				continue
			}
			rest := strings.TrimPrefix(plain, label)
			if !strings.HasPrefix(strings.TrimLeft(rest, " "), ":") {
				continue
			}
			found = true
			if !strings.HasPrefix(rest, ":") {
				t.Errorf("padding before colon for %q: %q", label, line)
			}
			colon := strings.Index(line, ":")
			value := colon + 1 + len(line[colon+1:]) - len(strings.TrimLeft(line[colon+1:], " "))
			if value < len(line) {
				got := displayWidth(line[:value])
				if column >= 0 && got != column {
					t.Errorf("value column for %q = %d, want %d", label, got, column)
				}
				column = got
			}
			break
		}
		if !found {
			t.Errorf("missing typed label plus colon: %q", label)
		}
	}
}

func TestLayoutContractFieldWidthAndHanging(t *testing.T) {
	rows := [][2]string{{"短", strings.Repeat("東京e\u0301👩\u200d🔬", 6)}, {"bc", "2001:db8::1:80"}, {"defg", "0"}, {"empty", ""}, {"flag", "false"}, {"unknown", "? (partial: failed)"}, {"control", "x\x00y\x1b[31m"}}
	for _, width := range []int{8, 24, 40, 80, 120} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			out := Fields(rows, Presentation{Width: width})
			for line := range strings.SplitSeq(out, "\n") {
				if displayWidth(line) > width || strings.ContainsAny(line, "\x00\x1b\t") {
					t.Errorf("overflow or control: %q", line)
				}
			}
			if width >= 24 {
				layoutContractFields(t, out, "短", "bc", "defg", "empty", "flag", "unknown", "control")
				lines := strings.Split(out, "\n")
				start := strings.Index(lines[0], ":") + 1
				start += len(lines[0][start:]) - len(strings.TrimLeft(lines[0][start:], " "))
				for _, line := range lines[1:] {
					if strings.HasPrefix(strings.TrimLeft(line, " "), "bc") {
						break
					}
					if len(line)-len(strings.TrimLeft(line, " ")) != displayWidth(lines[0][:start]) {
						t.Errorf("continuation left value column: %q", line)
					}
				}
			}
			for _, row := range rows {
				if !strings.Contains(alignmentCompact(out), alignmentCompact(cleanDisplayCell(row[1]))) {
					t.Errorf("lost safe value %q", row[1])
				}
			}
			long := Fields([][2]string{{strings.Repeat("LongLabel", 6), "END"}}, Presentation{Width: width})
			for line := range strings.SplitSeq(long, "\n") {
				if displayWidth(line) > width {
					t.Errorf("long-label overflow: %q", line)
				}
			}
		})
	}
}

func TestLayoutContractSummaryAndLegacyRows(t *testing.T) {
	for _, width := range []int{40, 80, 120} {
		t.Run(fmt.Sprintf("summary/%d", width), func(t *testing.T) {
			var b strings.Builder
			writeWifiSummaryGroup(&b, "  Identity", Presentation{Width: width}, kv("a", "1"), kv("bc", "2"), kv("defg", "3\n2001:db8::1"))
			out := wrapPresentation(b.String(), width)
			layoutContractFields(t, out, "a", "bc", "defg")
			layoutContractSummaryContinuation(t, out, width)
		})
	}
	t.Run("legacy-KV", func(t *testing.T) {
		var b strings.Builder
		writeKVRows(&b, kv("a", "1"), kv("bc", "2"), kv("defg", "3"))
		layoutContractFields(t, wrapPresentation(b.String(), 120), "a", "bc", "defg")
	})
}

func TestLayoutContractTableEdges(t *testing.T) {
	columns := []displayTableColumn{{header: "H"}, {header: "V"}}
	for _, tc := range []struct {
		name, prefix, want string
		rows               [][]string
		adjacent           bool
	}{
		{"start-end", "", "\nH  V\nx  0\n\n", [][]string{{"x", "0"}}, false},
		{"text", "before\n", "before\n\nH  V\nx  0\n\n", [][]string{{"x", "0"}}, false},
		{"existing-blank", "before\n\n", "before\n\nH  V\nx  0\n\n", [][]string{{"x", "0"}}, false},
		{"existing-two-blanks", "before\n\n\n", "before\n\nH  V\nx  0\n\n", [][]string{{"x", "0"}}, false},
		{"adjacent", "", "\nH  V\nx  0\n\nH  V\nx  0\n\n", [][]string{{"x", "0"}}, true},
		{"empty", "", "\nH  V\n\n", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var b strings.Builder
			b.WriteString(tc.prefix)
			writeDisplayTable(&b, columns, tc.rows)
			if tc.adjacent {
				writeDisplayTable(&b, columns, tc.rows)
			}
			if b.String() != tc.want {
				t.Errorf("got %q; want %q", b.String(), tc.want)
			}
		})
	}
	t.Run("narrow-records", func(t *testing.T) {
		var b strings.Builder
		writeDisplayTable(&b, []displayTableColumn{{header: "LONGNAME"}, {header: "VALUE"}}, [][]string{{"x", "y"}}, 8)
		out := wrapPresentation(b.String(), 8)
		if !strings.HasPrefix(out, "\n") || !strings.HasSuffix(out, "\n\n") {
			t.Errorf("record layout lacks table boundary: %q", out)
		}
	})
}

func layoutContractTableBoundary(t *testing.T, out, header string, rows int) {
	t.Helper()
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	for i, line := range lines {
		if fields := strings.Fields(line); len(fields) > 0 && fields[0] == header {
			before, after := 0, 0
			for j := i - 1; j >= 0 && lines[j] == ""; j-- {
				before++
			}
			for j := i + rows + 1; j < len(lines) && lines[j] == ""; j++ {
				after++
			}
			if before != 1 || after != 1 {
				t.Errorf("table %s: blank lines above header/below last row = %d/%d, want 1/1", header, before, after)
			}
			return
		}
	}
	t.Errorf("missing table header %s", header)
}

func TestLayoutContractCommandTables(t *testing.T) {
	for name, tc := range map[string]struct {
		result *controlpb.CommandResult
		header string
	}{
		"scan":    {&controlpb.CommandResult{Payload: &controlpb.CommandResult_WifiScan{WifiScan: &controlpb.WifiScan{Results: []*controlpb.WifiScanResult{{Ssid: "東京e\u0301👩\u200d🔬", Bssid: "02:00:00:00:00:01"}}}}}, "AP"},
		"request": {&controlpb.CommandResult{Payload: &controlpb.CommandResult_ResolveDns{ResolveDns: &controlpb.ResolveDnsResult{Answers: []*controlpb.DnsAnswer{{Address: "2001:db8::1"}}}}}, "TYPE"},
		"test":    {&controlpb.CommandResult{Payload: &controlpb.CommandResult_WifiAssert{WifiAssert: &controlpb.WifiAssertResult{Checks: []*controlpb.DiagnosticCheck{{Key: "link", Actual: "0", Expected: "false", Message: "partial"}}}}}, "CHECK"},
		"monitor": {&controlpb.CommandResult{Payload: &controlpb.CommandResult_WifiMonitor{WifiMonitor: &controlpb.WifiMonitorResult{Events: []*controlpb.WifiEvent{{Type: "link", Message: "partial: failed"}}}}}, "TIME"},
	} {
		t.Run(name, func(t *testing.T) {
			tc.result.Status = controlpb.CommandResult_STATUS_FAILED
			tc.result.Message = "partial: failed"
			out, err := CommandResult("self", tc.result, command.Options{WifiScanBrief: true}, pipeline.FormatText, Presentation{Interactive: true, Width: 120})
			if err != nil {
				t.Fatal(err)
			}
			layoutContractTableBoundary(t, out, tc.header, 1)
		})
	}
	t.Run("adjacent-capability-tables", func(t *testing.T) {
		out, err := CommandResult("self", &controlpb.CommandResult{Payload: &controlpb.CommandResult_WifiCapabilities{WifiCapabilities: &controlpb.WifiCapabilities{}}}, command.Options{}, pipeline.FormatText, Presentation{Interactive: true, Width: 120})
		if err != nil {
			t.Fatal(err)
		}
		for header, rows := range map[string]int{"Band": 4, "PHY": 4, "Security": 4, "Features": 1} {
			layoutContractTableBoundary(t, out, header, rows)
		}
	})
}

func TestLayoutContractReportAndNoninteractiveFields(t *testing.T) {
	t.Run("report", func(t *testing.T) {
		out, err := Report(watchrun.Report{}, Presentation{Interactive: true, Width: 120})
		if err != nil {
			t.Fatal(err)
		}
		layoutContractFields(t, out, "Overall outcome (core)", "Core passed", "State", "Counts")
	})
	t.Run("noninteractive", func(t *testing.T) {
		out, err := CommandResult("self", &controlpb.CommandResult{Payload: &controlpb.CommandResult_PermissionSnapshot{PermissionSnapshot: &controlpb.PermissionSnapshot{}}}, command.Options{}, pipeline.FormatText, Presentation{Width: 120})
		if err != nil {
			t.Fatal(err)
		}
		layoutContractFields(t, out, "Latency", "Source", "Observed at unix ms", "Device Owner")
	})
}

func TestLayoutContractFilteredTable(t *testing.T) {
	var b strings.Builder
	writeDisplayTable(&b, []displayTableColumn{{header: "H"}, {header: "V"}}, [][]string{{"x", "0"}})
	for _, tc := range []struct{ stage, want string }{
		{"match .*", "\nH  V\nx  0\n\n"},
		{"find ^H", "H  V\nx  0\n\n"},
		{"except ^$", "H  V\nx  0\n"},
	} {
		t.Run(tc.stage, func(t *testing.T) {
			p, err := pipeline.Parse([]string{tc.stage})
			if err != nil {
				t.Fatal(err)
			}
			out, err := p.Apply(LayoutText(b.String(), Presentation{}))
			if err != nil {
				t.Fatal(err)
			}
			if got := LayoutText(out, Presentation{Width: 80}); got != tc.want {
				t.Errorf("explicit filter output = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestLayoutContractRawAndJSONBoundary(t *testing.T) {
	raw := "Raw:    untouched:    spacing\nhttps://example.test/a:b\n2001:db8::1"
	if got := TextBlock(raw, Presentation{}); got != raw+"\n" {
		t.Errorf("raw colon reparsed: %q", got)
	}
	result := &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_FAILED, Payload: &controlpb.CommandResult_HttpCheck{HttpCheck: &controlpb.HttpCheckResult{Url: "https://example.test/a:b", Error: "partial: failed"}}}
	before := proto.Clone(result)
	for _, width := range []int{8, 24, 40, 80, 120} {
		out, err := CommandResult("self", result, command.Options{}, pipeline.FormatJSON, Presentation{Width: width, Interactive: true})
		if err != nil || !json.Valid([]byte(out)) || !strings.Contains(out, "https://example.test/a:b") || !proto.Equal(before, result) {
			t.Errorf("width=%d: JSON or typed input changed: %v", width, err)
		}
	}
}

func TestLayoutContractSummaryHangingWithinBudget(t *testing.T) {
	var b strings.Builder
	writeWifiSummaryGroup(&b, "  Identity", Presentation{Width: 40}, kv("defg", "3\n2001:db8::1"))
	layoutContractSummaryContinuation(t, wrapPresentation(b.String(), 40), 40)
}

func layoutContractSummaryContinuation(t *testing.T, out string, width int) {
	t.Helper()
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	for _, line := range lines {
		if displayWidth(line) > width {
			t.Errorf("summary overflow: %q", line)
		}
	}
	for i, line := range lines {
		if !strings.HasPrefix(strings.TrimLeft(line, " "), "defg") {
			continue
		}
		start := strings.LastIndex(line, "3")
		if start < 0 || line[start:] != "3" {
			t.Fatalf("missing first summary value: %q", line)
		}
		column := displayWidth(line[:start])
		var value strings.Builder
		for _, continuation := range lines[i+1:] {
			indent := len(continuation) - len(strings.TrimLeft(continuation, " "))
			if indent != column {
				t.Errorf("continuation column = %d, want %d: %q", indent, column, continuation)
			}
			value.WriteString(continuation[indent:])
		}
		if value.String() != "2001:db8::1" {
			t.Errorf("wrapped IPv6 = %q, want complete original value", value.String())
		}
		return
	}
	t.Fatal("missing typed defg summary field")
}

func TestLayoutContractMLOTable(t *testing.T) {
	d := &controlpb.WifiDiagnostics{Scan: &controlpb.WifiScan{Results: []*controlpb.WifiScanResult{{Ssid: "Lab", Bssid: "02:00:00:00:00:01", WifiStandard: "802.11be", ApMldMacAddress: "02:00:00:00:00:02"}}}}
	out, err := CommandResult("self", &controlpb.CommandResult{Payload: &controlpb.CommandResult_WifiDiagnostics{WifiDiagnostics: d}}, command.Options{WifiRenderMode: command.WifiRenderModeEHT}, pipeline.FormatText, Presentation{Interactive: true, Width: 120})
	if err != nil {
		t.Fatal(err)
	}
	layoutContractTableBoundary(t, out, "SSID", 1)
}

func TestLayoutContractHelpAndLocalMetadata(t *testing.T) {
	var help strings.Builder
	shell.WriteHelpTopic(&help, "show wifi status")
	for _, width := range []int{8, 24, 40, 80, 120} {
		out := LayoutText(help.String(), Presentation{Width: width})
		if !strings.Contains(alignmentCompact(out), "Description:") {
			t.Errorf("help description lost at width %d", width)
		}
		for line := range strings.SplitSeq(out, "\n") {
			if displayWidth(line) > width {
				t.Errorf("help overflow: %q", line)
			}
		}
	}
	layoutContractFields(t, Fields([][2]string{{"Owner", "Controller"}, {"Target", "self"}, {"Broadcast", "false"}, {"Layout width", "0"}, {"History", "all commands; session-only"}}, Presentation{}), "Owner", "Target", "Broadcast", "Layout width", "History")
}

func TestLayoutContractAgentsTable(t *testing.T) {
	out, err := Agents(AgentListView{Selected: "qa", Agents: []control.AgentInfo{{
		ID: "qa", Connected: time.Unix(0, 0).UTC(),
		Hello: &controlpb.AgentHello{AdbSerial: "literal:serial", Device: &controlpb.DeviceInfo{Model: "東京e\u0301👩\u200d🔬"}},
	}}}, pipeline.FormatText)
	if err != nil {
		t.Fatal(err)
	}
	layoutContractTableBoundary(t, out, "SEL", 1)
	if !strings.Contains(out, "literal:serial") || !strings.Contains(out, "東京e\u0301👩\u200d🔬") {
		t.Errorf("agents table changed literal identity: %q", out)
	}
}

func TestLayoutContractMultilineKVAndLabelEdges(t *testing.T) {
	value := "0\nfalse\n" + strings.Repeat("東京e\u0301👩\u200d🔬", 6) + "\n? (unknown)"
	for _, width := range []int{8, 24, 40, 80, 120} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			var b strings.Builder
			writeKVRows(&b, kv("a", value))
			out := wrapPresentation(b.String(), width)
			lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
			column := 5
			if width == 8 {
				if lines[0] != "  a:" {
					t.Fatalf("narrow KV label = %q", lines[0])
				}
				lines = lines[1:]
				column = 4
			} else if !strings.HasPrefix(lines[0], "  a: ") {
				t.Fatalf("KV colon prefix = %q", lines[0])
			}
			var joined strings.Builder
			for i, line := range lines {
				if displayWidth(line) > width || len(line) < column {
					t.Fatalf("KV width or value column: %q", line)
				}
				if (width == 8 || i > 0) && line[:column] != strings.Repeat(" ", column) {
					t.Errorf("KV continuation left value column: %q", line)
				}
				joined.WriteString(line[column:])
			}
			if want := strings.ReplaceAll(value, "\n", ""); joined.String() != want {
				t.Errorf("multiline KV changed: %q, want %q", joined.String(), want)
			}
			for _, label := range []string{"", "a", "abcdef", strings.Repeat("L", 32), "東京", "e\u0301", "👩\u200d🔬"} {
				out := Fields([][2]string{{label, "0"}}, Presentation{Width: width})
				for line := range strings.SplitSeq(out, "\n") {
					if displayWidth(line) > width || label != "" && strings.TrimSpace(line) == ":" {
						t.Errorf("label %q: overflow or detached colon: %q", label, line)
					}
				}
				if strings.Count(out, ":") != 1 || strings.ReplaceAll(alignmentCompact(out), ":", "") != label+"0" {
					t.Errorf("label or value changed: label=%q output=%q", label, out)
				}
			}
		})
	}
}

func TestLayoutContractEmptyNarrowTableKeepsHeader(t *testing.T) {
	var b strings.Builder
	writeDisplayTable(&b, []displayTableColumn{{header: "LONGNAME"}, {header: "VALUE"}}, nil, 8)
	out := wrapPresentation(b.String(), 8)
	if compact := alignmentCompact(out); !strings.Contains(compact, "LONGNAME") || !strings.Contains(compact, "VALUE") {
		t.Errorf("empty narrow table lost known headers: %q", out)
	}
	if !strings.HasPrefix(out, "\n") || !strings.HasSuffix(out, "\n\n") {
		t.Errorf("empty narrow table lacks outer blank boundaries: %q", out)
	}
	for line := range strings.SplitSeq(out, "\n") {
		if displayWidth(line) > 8 {
			t.Errorf("empty narrow table overflow: %q", line)
		}
	}
}
