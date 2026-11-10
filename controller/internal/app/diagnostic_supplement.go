package app

import (
	"context"
	"strings"
	"time"

	"github.com/mi2428/dropcheck-agent/controller/internal/adb"
	"github.com/mi2428/dropcheck-agent/controller/internal/adbdiag"
	"github.com/mi2428/dropcheck-agent/controller/internal/command"
	"github.com/mi2428/dropcheck-agent/controller/internal/control"
	"github.com/mi2428/dropcheck-agent/controller/internal/controlpb"
	"github.com/mi2428/dropcheck-agent/controller/internal/render"
)

type commandResultSupplements struct {
	textBlocks []func(render.Presentation) string
}

func collectCommandResultSupplements(ctx context.Context, state *shellState, agent control.AgentInfo, result *controlpb.CommandResult, options commandOptions) commandResultSupplements {
	if result == nil || options.RawView != "" || options.WifiRenderMode == command.WifiRenderModeStatus {
		return commandResultSupplements{}
	}
	var supplements commandResultSupplements
	if shouldCollectADBIPv6RASupplement(result) || options.WifiRenderMode == command.WifiRenderModeFull && result.GetWifiDiagnostics().GetStatus() != nil || result.GetIpStatus() != nil {
		supplements.addText(collectADBIPv6RASupplement(ctx, state, agent, result))
	}
	if shouldCollectADBMLOSupplement(result, options) {
		supplements.addText(collectADBMLOSupplement(ctx, state, agent))
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

func collectADBIPv6RASupplement(ctx context.Context, state *shellState, agent control.AgentInfo, result *controlpb.CommandResult) func(render.Presentation) string {
	if agent.Hello == nil || result == nil {
		return nil
	}
	serial := agent.Hello.GetAdbSerial()
	if serial == "" {
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
		text := sourcedADBSupplement("IPv6 RA supplement\n" + render.SourceFields([][2]string{{"Availability", "unavailable (no selected physical Wi-Fi interface)"}}))
		return func(render.Presentation) string { return text }
	}
	summary := adbdiag.CollectIPv6RA(ctx, adb.Client{
		Path:    state.adbPath,
		Serial:  serial,
		Timeout: 8 * time.Second,
	}, iface)
	received := time.Now()
	return func(render.Presentation) string {
		text := adbdiag.IPv6RASummaryText(summary)
		if text == "" {
			text = "IPv6 RA supplement\n" + render.SourceFields([][2]string{{"Availability", "no host evidence returned"}})
		}
		for _, problem := range summary.Errors {
			text += render.SourceFields([][2]string{{"Acquisition error", problem}})
		}
		return sourcedADBSupplement(text, received)
	}
}

func collectADBMLOSupplement(ctx context.Context, state *shellState, agent control.AgentInfo) func(render.Presentation) string {
	if agent.Hello == nil {
		return nil
	}
	serial := agent.Hello.GetAdbSerial()
	if serial == "" {
		text := sourcedADBSupplement("MLO supplement\n" + render.SourceFields([][2]string{{"Availability", "unavailable (no pinned ADB serial)"}}))
		return func(render.Presentation) string { return text }
	}
	summary := adbdiag.CollectMLO(ctx, adb.Client{
		Path:    state.adbPath,
		Serial:  serial,
		Timeout: 8 * time.Second,
	})
	received := time.Now()
	return func(view render.Presentation) string {
		text := adbdiag.MLOSummaryText(summary, view)
		if text == "" {
			text = "MLO supplement\n" + render.SourceFields([][2]string{{"Availability", "no host evidence returned; not an Android observation"}})
		}
		for _, problem := range summary.Errors {
			text += render.SourceFields([][2]string{{"Acquisition error", problem}})
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
