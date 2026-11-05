package render

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mi2428/dropcheck-agent/controller/internal/command"
	"github.com/mi2428/dropcheck-agent/controller/internal/controlpb"
	"github.com/mi2428/dropcheck-agent/controller/internal/pipeline"
)

type rawDump struct {
	Label string `json:"label"`
	Text  string `json:"text"`
}

// Debug uses the existing captured payload, never another acquisition or scan.
func rawDebugResult(agent string, result *controlpb.CommandResult, options command.Options, format pipeline.Format, views ...Presentation) (string, error) {
	result = presentationResult(result, options)
	dumps := []rawDump{}
	var errors []string
	add := func(label, text string) {
		if text != "" {
			dumps = append(dumps, rawDump{label, text})
		}
	}
	links := func(label string, values []*controlpb.MloLinkInfo) {
		for i, link := range values {
			add(fmt.Sprintf("%s MloLink %d", label, i+1), link.GetRaw())
		}
	}
	wifi := func(label string, conn *controlpb.WifiConnection) {
		add(label+" WifiInfo", conn.GetRaw())
		links(label+" associated", conn.GetAssociatedMloLinks())
		links(label+" affiliated", conn.GetAffiliatedMloLinks())
	}
	switch options.RawView {
	case "wifi info":
		if result.GetWifiStatus() == nil {
			return "", fmt.Errorf("raw WifiInfo: wrong or missing payload")
		}
		wifi("Wi-Fi", result.GetWifiStatus().GetConnection())
	case "wifi scan":
		if result.GetWifiScan() == nil {
			return "", fmt.Errorf("raw ScanResult: wrong or missing payload")
		}
		for i, ap := range result.GetWifiScan().GetResults() {
			label := fmt.Sprintf("ScanResult %d", i+1)
			add(label, ap.GetRaw())
			links(label+" affiliated", ap.GetAffiliatedMloLinks())
		}
		errors = result.GetWifiScan().GetErrors()
	case "network capabilities", "network link-properties", "network wifi-info":
		snapshot := result.GetNetworkSnapshot()
		if snapshot == nil || len(snapshot.GetNetworks()) != 1 {
			return "", fmt.Errorf("raw Network: expected exactly one captured Network")
		}
		network := snapshot.GetNetworks()[0]
		ip := network.GetIpStatus()
		if !command.ValidNetworkID(options.RawNetworkID) || network.GetNetworkId() != options.RawNetworkID || ip == nil || ip.GetNetworkId() != options.RawNetworkID {
			return "", fmt.Errorf("raw Network: inconsistent captured Network identity")
		}
		if options.RawView == "network capabilities" {
			add("Network "+network.GetNetworkId()+" NetworkCapabilities", ip.GetRawCapabilities())
		} else if options.RawView == "network link-properties" {
			add("Network "+network.GetNetworkId()+" LinkProperties", ip.GetRawLinkProperties())
		} else {
			wifi("Network "+network.GetNetworkId(), ip.GetWifi())
		}
		errors = snapshot.GetErrors()
	default:
		return "", fmt.Errorf("unsupported raw debug view")
	}
	if len(dumps) == 0 {
		return "", fmt.Errorf("raw %s unavailable (status=%s): %s", options.RawView, resultStatus(result.GetStatus()), result.GetMessage())
	}
	if format == pipeline.FormatJSON {
		data, err := json.MarshalIndent(struct {
			Status    string    `json:"status"`
			Message   string    `json:"message,omitempty"`
			Source    string    `json:"source"`
			Target    string    `json:"target"`
			ElapsedMs int64     `json:"elapsed_ms"`
			Dumps     []rawDump `json:"dumps"`
			Errors    []string  `json:"errors,omitempty"`
		}{resultStatus(result.GetStatus()), result.GetMessage(), "android", cleanDisplayCell(agent), result.GetElapsedMs(), dumps, errors}, "", "  ")
		return string(data) + "\n", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Raw Android %s  %s\n", options.RawView, strings.ToUpper(resultStatus(result.GetStatus())))
	writeRecord(&b, "Source", "android")
	writeRecord(&b, "Target", cleanDisplayCell(agent))
	writeRecord(&b, "Latency", fmt.Sprintf("%dms", result.GetElapsedMs()))
	if result.GetMessage() != "" {
		writeRecord(&b, "Message", result.GetMessage())
	}
	for _, problem := range errors {
		writeRecord(&b, "Acquisition error", problem)
	}
	for _, dump := range dumps {
		writeSection(&b, dump.Label)
		b.WriteString(dump.Text)
		if !strings.HasSuffix(dump.Text, "\n") {
			b.WriteByte('\n')
		}
	}
	view := Presentation{}
	if len(views) > 0 {
		view = views[0]
	}
	return wrapPresentation(b.String(), view.Width), nil
}
