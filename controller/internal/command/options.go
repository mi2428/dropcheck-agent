package command

const (
	// WifiRenderModeMLO renders WifiDiagnostics as an MLO-focused summary.
	WifiRenderModeMLO = "mlo"
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
	// WifiMLOFreshScan requests a fresh Wi-Fi scan before rendering MLO output.
	WifiMLOFreshScan bool
	// WifiMLOFreshScanTimeoutMs is the fresh-scan wait timeout in milliseconds.
	WifiMLOFreshScanTimeoutMs uint32
}
