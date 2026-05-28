package command

const (
	// WifiRenderModeEHT renders WifiDiagnostics as an EHT-focused summary.
	WifiRenderModeEHT = "eht"
)

// Options describes controller-local behavior attached to an Operation.
//
// These options are intentionally kept outside controlpb.RunCommand because
// they affect presentation or validation in the controller rather than work the
// Android agent performs.
type Options struct {
	// TracerouteRequiredHops lists hop hostnames or addresses that should appear
	// in rendered traceroute output.
	TracerouteRequiredHops []string
	// WifiRenderMode selects a controller-only Wi-Fi diagnostics presentation.
	WifiRenderMode string
	// WifiEHTFreshScan requests a fresh Wi-Fi scan before rendering EHT output.
	WifiEHTFreshScan bool
	// WifiEHTFreshScanTimeoutMs is the fresh-scan wait timeout in milliseconds.
	WifiEHTFreshScanTimeoutMs uint32
	// WifiEHTSSID filters the EHT render view to scan/current entries for one SSID.
	WifiEHTSSID string
	// WifiEHTBSSID filters the EHT render view to scan/current entries for one BSSID.
	WifiEHTBSSID string
}
