package app

import (
	"context"
	"strings"
	"time"

	"dropcheck/controller/internal/adb"
	"dropcheck/controller/internal/adbdiag"
	"dropcheck/controller/internal/command"
	"dropcheck/controller/internal/control"
	"dropcheck/controller/internal/controlpb"
)

type commandResultSupplements struct {
	textBlocks []string
}

func collectCommandResultSupplements(ctx context.Context, state *shellState, agent control.AgentInfo, result *controlpb.CommandResult, options commandOptions, format outputFormat) commandResultSupplements {
	if format != outputText || result == nil {
		return commandResultSupplements{}
	}
	var supplements commandResultSupplements
	if shouldCollectADBMLOSupplement(result, options) {
		supplements.addText(collectADBMLOSupplement(ctx, state, agent))
	}
	return supplements
}

func shouldCollectADBMLOSupplement(result *controlpb.CommandResult, options commandOptions) bool {
	if result == nil {
		return false
	}
	return options.WifiRenderMode == command.WifiRenderModeEHT && result.GetWifiDiagnostics() != nil
}

func collectADBMLOSupplement(ctx context.Context, state *shellState, agent control.AgentInfo) string {
	if agent.Hello == nil {
		return ""
	}
	serial := agent.Hello.GetAdbSerial()
	if serial == "" {
		return ""
	}
	summary := adbdiag.CollectMLO(ctx, adb.Client{
		Path:    state.adbPath,
		Serial:  serial,
		Timeout: 8 * time.Second,
	})
	return adbdiag.RenderMLOSummary(summary)
}

func (s *commandResultSupplements) addText(text string) {
	if text == "" {
		return
	}
	s.textBlocks = append(s.textBlocks, text)
}

func (s commandResultSupplements) appendToText(out string) string {
	for _, block := range s.textBlocks {
		if block == "" {
			continue
		}
		if !strings.HasSuffix(out, "\n") {
			out += "\n"
		}
		if !strings.HasSuffix(out, "\n\n") {
			out += "\n"
		}
		out += block
	}
	return out
}
