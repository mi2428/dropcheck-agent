package watch

import (
	"context"
	"fmt"
	"strings"

	"dropcheck/controller/internal/command"
	"dropcheck/controller/internal/control"
	"dropcheck/controller/internal/controlpb"
)

type bandSupport struct {
	supported   map[string]struct{}
	unsupported map[string]struct{}
}

func detectBandSupport(ctx context.Context, opRunner OperationRunner, agent control.AgentInfo) (bandSupport, error) {
	exec, err := opRunner.Run(ctx, agent, command.WifiCapabilitiesOperation())
	if err != nil {
		return bandSupport{}, err
	}
	if exec.Result == nil {
		return bandSupport{}, nil
	}
	if exec.Result.GetStatus() != controlpb.CommandResult_STATUS_OK {
		return bandSupport{}, fmt.Errorf("wifi capabilities status=%s: %s", statusName(exec.Result.GetStatus()), exec.Result.GetMessage())
	}
	return bandSupportFromCapabilities(exec.Result.GetWifiCapabilities()), nil
}

func bandSupportFromCapabilities(capabilities *controlpb.WifiCapabilities) bandSupport {
	if capabilities == nil {
		return bandSupport{}
	}
	support := bandSupport{
		supported:   make(map[string]struct{}),
		unsupported: make(map[string]struct{}),
	}
	for _, band := range capabilities.GetSupportedBands() {
		if normalized := normalizeCapabilityBand(band); normalized != "" {
			support.supported[normalized] = struct{}{}
		}
	}
	for _, band := range capabilities.GetUnsupportedBands() {
		if normalized := normalizeCapabilityBand(band); normalized != "" {
			support.unsupported[normalized] = struct{}{}
		}
	}
	return support
}

func (support bandSupport) skipReason(band string) (string, bool) {
	normalized := normalizeCapabilityBand(band)
	if normalized == "" || normalized == "all" {
		return "", false
	}
	if _, ok := support.unsupported[normalized]; ok {
		return fmt.Sprintf("device does not support %s Wi-Fi", displayBand(normalized)), true
	}
	if len(support.supported) > 0 {
		if _, ok := support.supported[normalized]; !ok {
			return fmt.Sprintf("device does not report support for %s Wi-Fi", displayBand(normalized)), true
		}
	}
	return "", false
}

func normalizeCapabilityBand(band string) string {
	normalized := strings.ToLower(strings.TrimSpace(band))
	normalized = strings.ReplaceAll(normalized, " ", "")
	normalized = strings.ReplaceAll(normalized, "_", "")
	normalized = strings.ReplaceAll(normalized, "-", "")
	switch normalized {
	case "", "all":
		return normalized
	case "2.4", "2.4g", "2.4ghz", "24g", "24ghz":
		return "2.4ghz"
	case "5", "5g", "5ghz":
		return "5ghz"
	case "6", "6g", "6ghz":
		return "6ghz"
	case "60", "60g", "60ghz":
		return "60ghz"
	default:
		return normalized
	}
}

func displayBand(band string) string {
	switch band {
	case "2.4ghz":
		return "2.4GHz"
	case "5ghz":
		return "5GHz"
	case "6ghz":
		return "6GHz"
	case "60ghz":
		return "60GHz"
	default:
		return band
	}
}
