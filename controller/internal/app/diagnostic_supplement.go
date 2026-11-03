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
	"dropcheck/controller/internal/render"
)

type commandResultSupplements struct {
	textBlocks []string
}

func collectCommandResultSupplements(ctx context.Context, state *shellState, agent control.AgentInfo, result *controlpb.CommandResult, options commandOptions, format outputFormat, interactive ...bool) commandResultSupplements {
	if format != outputText || result == nil {
		return commandResultSupplements{}
	}
	var supplements commandResultSupplements
	reportUnavailable := len(interactive) > 0 && interactive[0]
	if shouldCollectADBIPv6RASupplement(result) || options.WifiRenderMode == command.WifiRenderModeFull && result.GetWifiDiagnostics().GetStatus() != nil || reportUnavailable && result.GetIpStatus() != nil {
		supplements.addText(collectADBIPv6RASupplement(ctx, state, agent, result, reportUnavailable))
	}
	if shouldCollectADBMLOSupplement(result, options) {
		supplements.addText(collectADBMLOSupplement(ctx, state, agent, reportUnavailable))
	}
	return supplements
}

func shouldCollectADBIPv6RASupplement(result *controlpb.CommandResult) bool {
	if result == nil {
		return false
	}
	return result.GetWifiStatus() != nil
}

func shouldCollectADBMLOSupplement(result *controlpb.CommandResult, options commandOptions) bool {
	if result == nil {
		return false
	}
	return (options.WifiRenderMode == command.WifiRenderModeEHT || options.WifiRenderMode == command.WifiRenderModeFull) && result.GetWifiDiagnostics() != nil
}

func collectADBIPv6RASupplement(ctx context.Context, state *shellState, agent control.AgentInfo, result *controlpb.CommandResult, reportUnavailable ...bool) string {
	visible := len(reportUnavailable) != 0 && reportUnavailable[0]
	if agent.Hello == nil || result == nil {
		return ""
	}
	serial := agent.Hello.GetAdbSerial()
	if serial == "" {
		if !visible {
			return ""
		}
		return sourcedADBSupplement("IPv6 RA supplement\nAvailability: unavailable (no pinned ADB serial)\n")
	}
	status := result.GetWifiStatus()
	if result.GetWifiDiagnostics() != nil {
		status = result.GetWifiDiagnostics().GetStatus()
	}
	ip := result.GetIpStatus()
	if ip == nil {
		ip = status.GetIpStatus()
	}
	iface := ip.GetInterfaceName()
	if iface == "" {
		if !visible {
			return ""
		}
		return sourcedADBSupplement("IPv6 RA supplement\nAvailability: unavailable (no selected physical Wi-Fi interface)\n")
	}
	summary := adbdiag.CollectIPv6RA(ctx, adb.Client{
		Path:    state.adbPath,
		Serial:  serial,
		Timeout: 8 * time.Second,
	}, iface)
	text := adbdiag.RenderIPv6RASummary(summary)
	if visible && text == "" {
		text = "IPv6 RA supplement\nAvailability: no host evidence returned\n"
	}
	if visible {
		for _, problem := range summary.Errors {
			text += "Acquisition error: " + problem + "\n"
		}
	}
	return sourcedADBSupplement(text)
}

func collectADBMLOSupplement(ctx context.Context, state *shellState, agent control.AgentInfo, reportUnavailable ...bool) string {
	visible := len(reportUnavailable) != 0 && reportUnavailable[0]
	if agent.Hello == nil {
		return ""
	}
	serial := agent.Hello.GetAdbSerial()
	if serial == "" {
		if !visible {
			return ""
		}
		return sourcedADBSupplement("MLO supplement\nAvailability: unavailable (no pinned ADB serial)\n")
	}
	summary := adbdiag.CollectMLO(ctx, adb.Client{
		Path:    state.adbPath,
		Serial:  serial,
		Timeout: 8 * time.Second,
	})
	text := adbdiag.RenderMLOSummary(summary)
	if text == "" && visible {
		text = "MLO supplement\nAvailability: no host evidence returned; not an Android observation\n"
	}
	if visible {
		for _, problem := range summary.Errors {
			text += "Acquisition error: " + problem + "\n"
		}
	}
	return sourcedADBSupplement(text)
}

func sourcedADBSupplement(text string) string {
	if text == "" {
		return ""
	}
	lines := strings.SplitN(text, "\n", 2)
	header := lines[0] + "\nSource: adb (host supplement)\nReceived on controller: " + time.Now().Format(time.RFC3339Nano) + " (not Android observation time)\n"
	if len(lines) > 1 {
		header += lines[1]
	}
	return render.TextBlock(header, render.Presentation{})
}

func (s *commandResultSupplements) addText(text string) {
	if text == "" {
		return
	}
	s.textBlocks = append(s.textBlocks, text)
}

func (s commandResultSupplements) appendToText(out string, views ...render.Presentation) string {
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
	view := terminalPresentation()
	if len(views) > 0 {
		view = views[0]
	}
	return render.LayoutText(out, view)
}
