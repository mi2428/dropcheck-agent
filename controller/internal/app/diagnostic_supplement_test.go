package app

import (
	"context"
	"testing"

	"dropcheck/controller/internal/command"
	"dropcheck/controller/internal/control"
	"dropcheck/controller/internal/controlpb"
)

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
			name: "wifi status skips adb mlo supplement",
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
			name: "wifi diagnostics in mlo render mode",
			result: &controlpb.CommandResult{
				Payload: &controlpb.CommandResult_WifiDiagnostics{
					WifiDiagnostics: &controlpb.WifiDiagnostics{},
				},
			},
			options: commandOptions{WifiRenderMode: command.WifiRenderModeMLO},
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

func TestCollectCommandResultSupplementsSkipsNonTextOutput(t *testing.T) {
	result := &controlpb.CommandResult{
		Payload: &controlpb.CommandResult_WifiStatus{
			WifiStatus: &controlpb.WifiStatus{},
		},
	}
	got := collectCommandResultSupplements(context.Background(), &shellState{}, control.AgentInfo{}, result, commandOptions{}, outputJSON)
	if len(got.textBlocks) != 0 {
		t.Fatalf("collectCommandResultSupplements() textBlocks = %q, want none", got.textBlocks)
	}
}

func TestCollectCommandResultSupplementsSkipsMissingADBSerial(t *testing.T) {
	result := &controlpb.CommandResult{
		Payload: &controlpb.CommandResult_WifiStatus{
			WifiStatus: &controlpb.WifiStatus{},
		},
	}
	got := collectCommandResultSupplements(context.Background(), &shellState{}, control.AgentInfo{}, result, commandOptions{}, outputText)
	if len(got.textBlocks) != 0 {
		t.Fatalf("collectCommandResultSupplements() textBlocks = %q, want none", got.textBlocks)
	}
}

func TestCommandResultSupplementsAppendToText(t *testing.T) {
	supplements := commandResultSupplements{}
	supplements.addText("ADB MLO Snapshot\n  tid_to_link  false\n")
	supplements.addText("")

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
