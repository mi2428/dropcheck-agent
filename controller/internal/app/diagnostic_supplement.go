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
	textBlocks []func(render.Presentation) string
}

func collectCommandResultSupplements(ctx context.Context, state *shellState, agent control.AgentInfo, result *controlpb.CommandResult, options commandOptions, format outputFormat, interactive ...bool) commandResultSupplements {
	if format != outputText || result == nil || options.RawView != "" || options.WifiRenderMode == command.WifiRenderModeStatus {
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

func collectADBIPv6RASupplement(ctx context.Context, state *shellState, agent control.AgentInfo, result *controlpb.CommandResult, reportUnavailable ...bool) func(render.Presentation) string {
	visible := len(reportUnavailable) != 0 && reportUnavailable[0]
	if agent.Hello == nil || result == nil {
		return nil
	}
	serial := agent.Hello.GetAdbSerial()
	if serial == "" {
		if !visible {
			return nil
		}
		text := sourcedADBSupplement("IPv6 RA supplement\n" + render.SourceFields([][2]string{{"Availability", "unavailable (no pinned ADB serial)"}}))
		return func(render.Presentation) string { return text }
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
			return nil
		}
		text := sourcedADBSupplement("IPv6 RA supplement\n" + render.SourceFields([][2]string{{"Availability", "unavailable (no selected physical Wi-Fi interface)"}}))
		return func(render.Presentation) string { return text }
	}
	summary := adbdiag.CollectIPv6RA(ctx, adb.Client{
		Path:    state.adbPath,
		Serial:  serial,
		Timeout: 8 * time.Second,
	}, iface)
	if summary.Empty() && !visible {
		return nil
	}
	received := time.Now()
	return func(render.Presentation) string {
		text := adbdiag.IPv6RASummaryText(summary)
		if visible && text == "" {
			text = "IPv6 RA supplement\n" + render.SourceFields([][2]string{{"Availability", "no host evidence returned"}})
		}
		if visible {
			for _, problem := range summary.Errors {
				text += render.SourceFields([][2]string{{"Acquisition error", problem}})
			}
		}
		return sourcedADBSupplement(text, received)
	}
}

func collectADBMLOSupplement(ctx context.Context, state *shellState, agent control.AgentInfo, reportUnavailable ...bool) func(render.Presentation) string {
	visible := len(reportUnavailable) != 0 && reportUnavailable[0]
	if agent.Hello == nil {
		return nil
	}
	serial := agent.Hello.GetAdbSerial()
	if serial == "" {
		if !visible {
			return nil
		}
		text := sourcedADBSupplement("MLO supplement\n" + render.SourceFields([][2]string{{"Availability", "unavailable (no pinned ADB serial)"}}))
		return func(render.Presentation) string { return text }
	}
	summary := adbdiag.CollectMLO(ctx, adb.Client{
		Path:    state.adbPath,
		Serial:  serial,
		Timeout: 8 * time.Second,
	})
	if summary.Empty() && !visible {
		return nil
	}
	received := time.Now()
	return func(view render.Presentation) string {
		text := adbdiag.MLOSummaryText(summary, view)
		if text == "" && visible {
			text = "MLO supplement\n" + render.SourceFields([][2]string{{"Availability", "no host evidence returned; not an Android observation"}})
		}
		if visible {
			for _, problem := range summary.Errors {
				text += render.SourceFields([][2]string{{"Acquisition error", problem}})
			}
		}
		return sourcedADBSupplement(text, received)
	}
}

func sourcedADBSupplement(text string, receivedAt ...time.Time) string {
	if text == "" {
		return ""
	}
	lines := strings.SplitN(text, "\n", 2)
	received := time.Now()
	if len(receivedAt) > 0 {
		received = receivedAt[0]
	}
	// Body records are sanitized at their source; retain their owned boundaries until layout.
	header := render.TextBlock(lines[0], render.Presentation{}) + render.SourceFields([][2]string{
		{"Source", "adb (host supplement)"},
		{"Received on controller", received.Format(time.RFC3339Nano) + " (not Android observation time)"},
	})
	if len(lines) > 1 {
		header += lines[1]
	}
	return header
}

func (s *commandResultSupplements) addText(text func(render.Presentation) string) {
	if text == nil {
		return
	}
	s.textBlocks = append(s.textBlocks, text)
}

func (s commandResultSupplements) appendToText(out string, views ...render.Presentation) string {
	view := terminalPresentation()
	if len(views) > 0 {
		view = views[0]
	}
	for _, text := range s.textBlocks {
		block := text(view)
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
	return render.LayoutText(out, view)
}
