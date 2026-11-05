package watchrun

import (
	"encoding/json"
	"fmt"

	"github.com/mi2428/dropcheck-agent/controller/internal/safevalue"
	"google.golang.org/protobuf/proto"
)

// Export is a separate, owned projection. Acquisition, evaluation and other
// sinks retain their original typed data.
func exportEvent(event Event) (Event, error) {
	event = snapshotEvent(event)
	event.Plan, event.Message = safevalue.Text(event.Plan), safevalue.Text(event.Message)
	event.Agent = exportAgent(event.Agent)
	event.Target.Name, event.Target.ShortName = safevalue.Text(event.Target.Name), safevalue.Text(event.Target.ShortName)
	event.Target.SSID, event.Target.BSSID = safevalue.Normalize(event.Target.SSID), safevalue.Normalize(event.Target.BSSID)
	event.Step.Name = safevalue.Text(event.Step.Name)
	event.Step.Message, event.Step.Error = safevalue.Text(event.Step.Message), safevalue.Text(event.Step.Error)
	for i := range event.Agents {
		event.Agents[i].Agent = exportAgent(event.Agents[i].Agent)
		event.Agents[i].Phase = safevalue.Text(event.Agents[i].Phase)
	}
	if event.Attempt != nil {
		if err := exportAttempt(event.Attempt); err != nil {
			return Event{}, err
		}
	}
	if event.Report != nil {
		event.Report.Name, event.Report.Reason = safevalue.Text(event.Report.Name), safevalue.Text(event.Report.Reason)
		for i := range event.Report.Attempts {
			if err := exportAttempt(&event.Report.Attempts[i]); err != nil {
				return Event{}, err
			}
		}
	}
	if event.Finding != nil {
		if err := exportFinding(event.Finding); err != nil {
			return Event{}, err
		}
	}
	return event, nil
}

func exportAgent(agent AgentSnapshot) AgentSnapshot {
	agent.Name, agent.DeviceModel = safevalue.Text(agent.Name), safevalue.Text(agent.DeviceModel)
	return agent
}

func exportAttempt(attempt *Attempt) error {
	attempt.Reason = safevalue.Text(attempt.Reason)
	result := &attempt.Result
	safevalue.SanitizeProto(result.Raw)
	for i := range result.Parts {
		part := &result.Parts[i]
		part.Name, part.Error = safevalue.Text(part.Name), safevalue.Text(part.Error)
		safevalue.SanitizeProto(part.Raw)
	}
	options := &result.Options
	options.RawView, options.RawNetworkID, options.WifiRenderMode = safevalue.Text(options.RawView), safevalue.Text(options.RawNetworkID), safevalue.Text(options.WifiRenderMode)
	options.WifiEHTSSID, options.WifiEHTBSSID = safevalue.Normalize(options.WifiEHTSSID), safevalue.Normalize(options.WifiEHTBSSID)
	options.WifiScanSSID, options.WifiScanBSSID = safevalue.Normalize(options.WifiScanSSID), safevalue.Normalize(options.WifiScanBSSID)
	for i := range options.TracerouteRequiredHops {
		options.TracerouteRequiredHops[i] = safevalue.Text(options.TracerouteRequiredHops[i])
	}
	for _, findings := range [][]Finding{attempt.Findings, result.Findings} {
		for i := range findings {
			if err := exportFinding(&findings[i]); err != nil {
				return err
			}
		}
	}
	return nil
}

func exportFinding(finding *Finding) error {
	finding.Target, finding.Check, finding.Message = safevalue.Text(finding.Target), safevalue.Text(finding.Check), safevalue.Text(finding.Message)
	// SSID/BSSID metrics are literal identities, not credential-shaped prose.
	label := ""
	if finding.Metric == "ssid" || finding.Metric == "bssid" {
		label = finding.Metric
	}
	finding.Metric = safevalue.Text(finding.Metric)
	finding.Observed = safevalue.Normalize(safevalue.Field(label, finding.Observed))
	finding.Expected = safevalue.Normalize(safevalue.Field(label, finding.Expected))
	var err error
	finding.ObservedValue, err = exportValue(label, finding.ObservedValue)
	if err != nil {
		return err
	}
	finding.ExpectedValue, err = exportValue(label, finding.ExpectedValue)
	return err
}

func exportValue(label string, value any) (any, error) {
	if safevalue.Field(label, "") == "<redacted>" {
		return "<redacted>", nil
	}
	switch v := value.(type) {
	case nil, bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64, json.Number:
		return value, nil
	case string:
		return safevalue.Field(label, v), nil
	case []string:
		values := make([]string, len(v))
		for i, item := range v {
			values[i] = safevalue.Field(label, item)
		}
		return values, nil
	case []any:
		values := make([]any, len(v))
		for i, item := range v {
			var err error
			values[i], err = exportValue(label, item)
			if err != nil {
				return nil, err
			}
		}
		return values, nil
	case map[string]any:
		values := make(map[string]any, len(v))
		for key, item := range v {
			safe, err := exportValue(key, item)
			if err != nil {
				return nil, err
			}
			values[safevalue.Text(key)] = safe
		}
		return values, nil
	case proto.Message:
		return safevalue.Proto(v), nil
	default:
		return nil, fmt.Errorf("unsupported watch export value type %T", value)
	}
}
