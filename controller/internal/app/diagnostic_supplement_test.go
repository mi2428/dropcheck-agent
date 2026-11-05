package app

import (
	"context"
	"strings"
	"testing"

	"github.com/mi2428/dropcheck-agent/controller/internal/command"
	"github.com/mi2428/dropcheck-agent/controller/internal/control"
	"github.com/mi2428/dropcheck-agent/controller/internal/controlpb"
	"github.com/mi2428/dropcheck-agent/controller/internal/render"
)

func TestFullRetainsHostSupplementAvailability(t *testing.T) {
	result := &controlpb.CommandResult{Payload: &controlpb.CommandResult_WifiDiagnostics{WifiDiagnostics: &controlpb.WifiDiagnostics{Status: &controlpb.WifiStatus{}}}}
	agent := control.AgentInfo{Hello: &controlpb.AgentHello{}}
	got := collectCommandResultSupplements(t.Context(), &shellState{}, agent, result, commandOptions{WifiRenderMode: command.WifiRenderModeFull})
	if len(got.textBlocks) != 2 {
		t.Fatalf("full lost host supplement availability: blocks=%d", len(got.textBlocks))
	}
	for _, block := range got.textBlocks {
		text := render.LayoutText(block(render.Presentation{}), render.Presentation{})
		facts := strings.Join(strings.Fields(text), " ")
		if !strings.Contains(facts, "Source: adb (host supplement)") || !strings.Contains(facts, "Availability: unavailable (no pinned ADB serial)") {
			t.Fatalf("full lost source/availability: %q", text)
		}
	}
}

func TestShouldCollectADBMLOSupplement(t *testing.T) {
	tests := []struct {
		name    string
		result  *controlpb.CommandResult
		options commandOptions
		want    bool
	}{
		{
			name: "nil result",
			want: false,
		},
		{
			name: "wifi status skips adb EHT supplement",
			result: &controlpb.CommandResult{
				Payload: &controlpb.CommandResult_WifiStatus{
					WifiStatus: &controlpb.WifiStatus{},
				},
			},
			want: false,
		},
		{
			name: "wifi diagnostics in default render mode",
			result: &controlpb.CommandResult{
				Payload: &controlpb.CommandResult_WifiDiagnostics{
					WifiDiagnostics: &controlpb.WifiDiagnostics{},
				},
			},
			want: false,
		},
		{
			name: "wifi diagnostics in EHT render mode",
			result: &controlpb.CommandResult{
				Payload: &controlpb.CommandResult_WifiDiagnostics{
					WifiDiagnostics: &controlpb.WifiDiagnostics{},
				},
			},
			options: commandOptions{WifiRenderMode: command.WifiRenderModeEHT},
			want:    true,
		},
		{
			name:   "non wifi payload",
			result: &controlpb.CommandResult{Message: "ok"},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldCollectADBMLOSupplement(tt.result, tt.options); got != tt.want {
				t.Fatalf("shouldCollectADBMLOSupplement() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestShouldCollectADBIPv6RASupplement(t *testing.T) {
	tests := []struct {
		name   string
		result *controlpb.CommandResult
		want   bool
	}{
		{
			name: "nil result",
			want: false,
		},
		{
			name: "wifi status",
			result: &controlpb.CommandResult{
				Payload: &controlpb.CommandResult_WifiStatus{
					WifiStatus: &controlpb.WifiStatus{},
				},
			},
			want: true,
		},
		{
			name: "wifi diagnostics",
			result: &controlpb.CommandResult{
				Payload: &controlpb.CommandResult_WifiDiagnostics{
					WifiDiagnostics: &controlpb.WifiDiagnostics{},
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldCollectADBIPv6RASupplement(tt.result); got != tt.want {
				t.Fatalf("shouldCollectADBIPv6RASupplement() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRawDebugNeverCollectsOrAppendsHostSupplements(t *testing.T) {
	result := &controlpb.CommandResult{Payload: &controlpb.CommandResult_WifiStatus{WifiStatus: &controlpb.WifiStatus{}}}
	agent := control.AgentInfo{Hello: &controlpb.AgentHello{AdbSerial: "test-only"}}
	got := collectCommandResultSupplements(t.Context(), &shellState{adbPath: "must-not-run"}, agent, result, commandOptions{RawView: "wifi info"})
	if len(got.textBlocks) != 0 {
		t.Fatalf("debug appended host supplements: %d", len(got.textBlocks))
	}
}

func TestCollectCommandResultSupplementsSkipsMissingADBSerial(t *testing.T) {
	result := &controlpb.CommandResult{
		Payload: &controlpb.CommandResult_WifiStatus{
			WifiStatus: &controlpb.WifiStatus{},
		},
	}
	got := collectCommandResultSupplements(context.Background(), &shellState{}, control.AgentInfo{}, result, commandOptions{})
	if len(got.textBlocks) != 0 {
		t.Fatalf("collectCommandResultSupplements() textBlocks = %d, want none", len(got.textBlocks))
	}
}

func TestCommandResultSupplementsAppendToText(t *testing.T) {
	supplements := commandResultSupplements{}
	supplements.addText(func(render.Presentation) string { return "ADB MLO Snapshot\n  tid_to_link  false\n" })
	supplements.addText(nil)

	tests := []struct {
		name string
		out  string
		want string
	}{
		{
			name: "adds separator after trailing newline",
			out:  "Wi-Fi\n",
			want: "Wi-Fi\n\nADB MLO Snapshot\n  tid_to_link  false\n",
		},
		{
			name: "adds separator after unterminated output",
			out:  "Wi-Fi",
			want: "Wi-Fi\n\nADB MLO Snapshot\n  tid_to_link  false\n",
		},
		{
			name: "keeps existing paragraph separator",
			out:  "Wi-Fi\n\n",
			want: "Wi-Fi\n\nADB MLO Snapshot\n  tid_to_link  false\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := supplements.appendToText(tt.out); got != tt.want {
				t.Fatalf("appendToText() = %q, want %q", got, tt.want)
			}
		})
	}
}
