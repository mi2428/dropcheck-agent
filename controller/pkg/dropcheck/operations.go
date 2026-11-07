package dropcheck

import (
	"fmt"
	"strconv"
	"time"

	"github.com/mi2428/dropcheck-agent/controller/internal/command"
	"github.com/mi2428/dropcheck-agent/controller/internal/controlpb"
)

// Operation is an immutable, opaque command created by this package. Its zero
// value is invalid. Copies may be reused concurrently; no transport, raw command,
// credentials or presentation options are exposed. Constructors validate input
// without contacting an agent, and Run revalidates before dispatch.
type Operation struct{ impl command.Operation }

// Name returns the stable dotted operation name, or empty for a zero value.
func (op Operation) Name() string { return op.impl.Name }

// String returns only the operation name, never credentials.
func (op Operation) String() string { return op.Name() }

// GoString returns only the operation name, never credentials.
func (op Operation) GoString() string { return op.Name() }

// NetworkSelector constrains IP inspection or a probe at selection time; it
// does not prove uninterrupted association. Zero selects the agent's default
// network. SSID is literal (including spaces, Unicode and numeric names).
type NetworkSelector struct {
	// SSID optionally requires an exact SSID.
	SSID string
	// BSSID optionally requires an access point MAC address.
	BSSID string
	// NetworkID is a positive Android Network ID; zero leaves it unspecified.
	// This is not a saved Wi-Fi configuration ID.
	NetworkID int32
}

// WithNetwork returns a new operation bound to selector. It supports IPStatus,
// Ping, Traceroute, PathMTU, GlobalIP, DNS, HTTP and Download; other operations
// return an error. It replaces, rather than merges, any previous selector.
func (op Operation) WithNetwork(selector NetworkSelector) (Operation, error) {
	if selector.NetworkID < 0 {
		return Operation{}, fmt.Errorf("NetworkID must be positive or zero (unspecified)")
	}
	cmd, options, err := command.BuildRunCommand(op.impl)
	if err != nil {
		return Operation{}, err
	}
	field := command.NetworkSelectorField(cmd)
	if field == nil {
		return Operation{}, fmt.Errorf("operation %q does not support network selection", op.Name())
	}
	value := &controlpb.NetworkSelector{Ssid: selector.SSID, Bssid: selector.BSSID}
	if selector.NetworkID != 0 {
		value.NetworkId = strconv.FormatInt(int64(selector.NetworkID), 10)
	}
	*field = value
	return validated(command.NewOperation(op.Name(), cmd, options), nil)
}

// WiFiConnectOptions configures a privileged PSK/SAE connection. SSID and
// Passphrase are literal UTF-8 bytes, never trimmed or interpreted as IDs.
type WiFiConnectOptions struct {
	// SSID is the network name.
	SSID string
	// Passphrase is required: 8..63 UTF-8 bytes without controls or 64 hex digits.
	// The operation retains its own value; labels never include the credential.
	Passphrase string
	// Security is "auto" (also empty), "wpa2", "wpa3", or "transition".
	// Auto infers PSK/SAE from Android's scan cache; it does not mean open Wi-Fi.
	Security string
	// BSSID optionally pins an access point.
	BSSID string
	// Band is empty/"all", "2.4ghz", "5ghz", "6ghz", or "60ghz".
	Band string
	// MacRandomization is empty (agent default), "auto", "none", "persistent",
	// or "non-persistent".
	MacRandomization string
	// Timeout is zero for 45s; otherwise positive whole milliseconds, at most
	// 2147483647ms. The same duration rules apply to all options in this package.
	Timeout time.Duration
}

// WiFiCycleOptions configures repeated connect/probe/disconnect cycles.
type WiFiCycleOptions struct {
	// WiFiConnectOptions supplies the connection and its timeout.
	WiFiConnectOptions
	// Count is zero for 3 cycles; otherwise 1..100.
	Count uint32
	// PingHost optionally probes after each connection.
	PingHost string
	// HTTPURL optionally probes an absolute HTTP(S) URL after each connection.
	HTTPURL string
	// ForgetAfterEach explicitly removes the profile after every cycle.
	ForgetAfterEach bool
	// Pause is zero for 1s; otherwise 1..60000 whole milliseconds.
	Pause time.Duration
}

// WiFiExpectationOptions defines wait/assert constraints. Empty strings and
// false flags mean "not required", not a request to observe false values.
type WiFiExpectationOptions struct {
	// SSID optionally requires an exact literal SSID.
	SSID string
	// BSSID optionally requires an access point MAC address.
	BSSID string
	// Security is empty (no constraint), "auto", "wpa2", "wpa3", or "transition".
	Security string
	// Band is empty/"all" or a band accepted by WiFiConnectOptions.
	Band string
	// RequireIP requires an assigned IP address.
	RequireIP bool
	// RequireValidated requires Android validated internet access.
	RequireValidated bool
	// Timeout is zero for 30s in WiFiWaitConnected, or an immediate WiFiAssert.
	// Nonzero values follow WiFiConnectOptions.Timeout's duration rules.
	Timeout time.Duration
}

// WiFiEHTOptions configures EHT diagnostics, optionally preceded by a fresh scan.
type WiFiEHTOptions struct {
	// Fresh requests a fresh scan before diagnostics.
	Fresh bool
	// Timeout is zero for 10s. A nonzero timeout requires Fresh and follows
	// WiFiConnectOptions.Timeout's duration rules.
	Timeout time.Duration
}

// PingOptions configures ICMP ping. Numeric zero fields select the documented
// defaults; all nonzero numbers must fit Android's signed 32-bit range.
type PingOptions struct {
	// Host is a hostname or IP address, not a command-line option.
	Host string
	// Count is zero for 3 packets; otherwise positive.
	Count uint32
	// Size is payload bytes; zero leaves size to the agent.
	Size uint32
	// Family is empty/"auto", "all", "ipv4", or "ipv6".
	Family string
	// Timeout is zero for Count*2s+3s (using default Count if zero).
	// Nonzero values follow WiFiConnectOptions.Timeout's duration rules.
	Timeout time.Duration
}

// TracerouteOptions configures traceroute and optional typed hop checks.
type TracerouteOptions struct {
	// Host is a hostname or IP address.
	Host string
	// MaxHops is zero for 30 hops; otherwise 1..255.
	MaxHops uint32
	// Via is copied at construction. Each address/hostname must occur in typed
	// hop observations; missing observation data is not a failed observed match.
	Via []string
	// Size is payload bytes; zero leaves size to the agent.
	Size uint32
	// Family accepts the same values as PingOptions.Family.
	Family string
	// Timeout is zero for 60s; nonzero follows WiFiConnectOptions.Timeout.
	Timeout time.Duration
}

// PathMTUOptions configures path-MTU discovery.
type PathMTUOptions struct {
	// Host is a hostname or IP address.
	Host string
	// MinMTU and MaxMTU are IP MTU bytes; zero lets the agent choose each bound.
	// Nonzero values must fit signed 32-bit; MaxMTU must not be below MinMTU.
	MinMTU, MaxMTU uint32
	// Family accepts the same values as PingOptions.Family.
	Family string
	// Timeout is zero for 30s; nonzero follows WiFiConnectOptions.Timeout.
	Timeout time.Duration
}

// WiFiStatus inspects current Wi-Fi state.
func WiFiStatus() Operation { return Operation{command.WifiStatusOperation()} }

// WiFiDiagnostics acquires status, capabilities, scan and network diagnostics.
func WiFiDiagnostics() Operation { return Operation{command.WifiDiagnosticsOperation()} }

// WiFiCapabilities inspects supported Wi-Fi capabilities.
func WiFiCapabilities() Operation { return Operation{command.WifiCapabilitiesOperation()} }

// WiFiDisconnect explicitly disconnects Wi-Fi.
func WiFiDisconnect() Operation { return Operation{command.WifiDisconnectOperation()} }

// IPStatus inspects the default network. Use WithNetwork for a typed selector.
func IPStatus() Operation { return Operation{command.IPStatusOperation()} }

// Networks inspects all Android networks.
func Networks() Operation { return Operation{command.NetworksOperation()} }

// Permissions inspects agent permissions.
func Permissions() Operation { return Operation{command.PermissionsOperation()} }

// DefaultNetwork inspects the active Android network.
func DefaultNetwork() Operation { op, _ := command.NetworkOperation("default"); return Operation{op} }

// WiFiScan reads the cached scan; band accepts WiFiConnectOptions.Band values.
func WiFiScan(band string) (Operation, error) { return validated(command.WifiScanOperation(band)) }

// WiFiFreshScan requests fresh scan evidence. Zero timeout uses 10s; nonzero
// follows WiFiConnectOptions.Timeout. Band accepts WiFiConnectOptions.Band values.
func WiFiFreshScan(band string, timeout time.Duration) (Operation, error) {
	d, err := durationTokens(timeout)
	if err != nil {
		return Operation{}, err
	}
	return validated(command.WifiFreshScanOperation(band, d[0]))
}

// WiFiScanDetail inspects a literal SSID or BSSID. Band accepts the usual values.
func WiFiScanDetail(target, band string) (Operation, error) {
	return validated(command.WifiScanDetailOperation(target, band))
}

// WiFiEHT acquires unfiltered EHT diagnostics.
// Fresh retains both acquisitions in Parts.
func WiFiEHT(opts WiFiEHTOptions) (Operation, error) {
	d, err := durationTokens(opts.Timeout)
	if err != nil {
		return Operation{}, err
	}
	return validated(command.WifiEHTOperationWithOptions(command.WifiEHTOptions{Fresh: opts.Fresh, Timeout: d[0]}))
}

// WiFiConnect validates a credential and builds a connection request.
func WiFiConnect(opts WiFiConnectOptions) (Operation, error) {
	input, err := connectOptions(opts)
	if err != nil {
		return Operation{}, err
	}
	return validated(command.WifiConnectOperation(input))
}

// WiFiCycle validates a repeated connect/probe/disconnect request.
func WiFiCycle(opts WiFiCycleOptions) (Operation, error) {
	input, err := connectOptions(opts.WiFiConnectOptions)
	if err != nil {
		return Operation{}, err
	}
	d, err := durationTokens(opts.Pause)
	if err != nil {
		return Operation{}, err
	}
	return validated(command.WifiCycleOperation(command.WifiCycleOptions{WifiConnectOptions: input, Count: number(opts.Count), Pause: d[0], PingHost: opts.PingHost, HTTPURL: opts.HTTPURL, ForgetAfterEach: opts.ForgetAfterEach}))
}

// WiFiWaitConnected waits for constraints; nonempty ssid overrides opts.SSID.
func WiFiWaitConnected(ssid string, opts WiFiExpectationOptions) (Operation, error) {
	input, err := expectationOptions(opts)
	if err != nil {
		return Operation{}, err
	}
	return validated(command.WifiWaitConnectedOperation(ssid, input))
}

// WiFiAssert checks constraints immediately unless Timeout is nonzero.
func WiFiAssert(opts WiFiExpectationOptions) (Operation, error) {
	input, err := expectationOptions(opts)
	if err != nil {
		return Operation{}, err
	}
	return validated(command.WifiAssertOperation(input))
}

// WiFiMonitor monitors events; zero duration/interval default to 10s/1s.
// Nonzero values must be whole milliseconds: duration 1..120000ms and interval
// 100..60000ms, matching the Android monitor bounds without silent clamping.
func WiFiMonitor(duration, interval time.Duration) (Operation, error) {
	d, err := durationTokens(duration, interval)
	if err != nil {
		return Operation{}, err
	}
	return validated(command.WifiMonitorOperation(d[0], d[1]))
}

// WiFiReconnect reconnects the active network; zero timeout uses 30s.
// Nonzero timeout follows WiFiConnectOptions.Timeout's duration rules.
func WiFiReconnect(timeout time.Duration) (Operation, error) {
	d, err := durationTokens(timeout)
	if err != nil {
		return Operation{}, err
	}
	return validated(command.WifiReconnectOperation(d[0]))
}

// WiFiForgetSSID forgets a literal SSID, never reinterpreting numeric names.
// Typed forgetting requires the agent's wifi.forget.typed capability.
func WiFiForgetSSID(ssid string) (Operation, error) {
	return validated(command.WifiForgetSSIDOperation(ssid), nil)
}

// WiFiForgetConfigID forgets a saved configuration ID (including known ID zero).
// Negative IDs are invalid; the typed-forget capability is required at Run.
func WiFiForgetConfigID(id int32) (Operation, error) {
	return validated(command.WifiForgetConfigIDOperation(id), nil)
}

// Network inspects one positive Android Network ID, not a configuration ID.
func Network(id int32) (Operation, error) {
	return validated(command.NetworkOperation(strconv.FormatInt(int64(id), 10)))
}

// Ping validates an ICMP probe; use WithNetwork for explicit network binding.
func Ping(opts PingOptions) (Operation, error) {
	d, err := durationTokens(opts.Timeout)
	if err != nil {
		return Operation{}, err
	}
	return validated(command.PingOperation(command.PingOptions{Host: opts.Host, Count: number(opts.Count), Size: number(opts.Size), Family: opts.Family, Timeout: d[0]}))
}

// Traceroute validates a route probe and copies Via for later evaluation.
func Traceroute(opts TracerouteOptions) (Operation, error) {
	d, err := durationTokens(opts.Timeout)
	if err != nil {
		return Operation{}, err
	}
	return validated(command.TracerouteOperation(command.TracerouteOptions{Host: opts.Host, MaxHops: number(opts.MaxHops), Size: number(opts.Size), Via: opts.Via, Family: opts.Family, Timeout: d[0]}))
}

// PathMTU validates path-MTU bounds and a probe target.
func PathMTU(opts PathMTUOptions) (Operation, error) {
	d, err := durationTokens(opts.Timeout)
	if err != nil {
		return Operation{}, err
	}
	return validated(command.PathMTUOperation(command.PathMTUOptions{Host: opts.Host, MinMTU: number(opts.MinMTU), MaxMTU: number(opts.MaxMTU), Family: opts.Family, Timeout: d[0]}))
}

// GlobalIP discovers public addresses. Family is empty/"all", "ipv4" or "ipv6";
// zero timeout uses 5s; nonzero follows WiFiConnectOptions.Timeout.
func GlobalIP(family string, timeout time.Duration) (Operation, error) {
	d, err := durationTokens(timeout)
	if err != nil {
		return Operation{}, err
	}
	return validated(command.GlobalIPOperation(family, d[0]))
}

// DNS resolves name. Record is empty/"ALL", "A" or "AAAA"; zero timeout uses 5s.
// Nonzero timeout follows WiFiConnectOptions.Timeout.
func DNS(name, record string, timeout time.Duration) (Operation, error) {
	d, err := durationTokens(timeout)
	if err != nil {
		return Operation{}, err
	}
	return validated(command.DNSOperation(name, record, d[0]))
}

// HTTP checks an HTTP(S) endpoint (missing scheme means HTTPS). Zero expected
// status uses 200; otherwise 100..599. Zero timeout uses 5s; nonzero follows
// WiFiConnectOptions.Timeout. URL userinfo is rejected, not silently removed.
func HTTP(url string, expectedStatus uint32, timeout time.Duration) (Operation, error) {
	d, err := durationTokens(timeout)
	if err != nil {
		return Operation{}, err
	}
	return validated(command.HTTPOperation(url, number(expectedStatus), d[0]))
}

// Download downloads an absolute HTTP(S) URL. Zero timeout uses 60s; nonzero
// follows WiFiConnectOptions.Timeout. Use WithNetwork for explicit binding.
func Download(url string, timeout time.Duration) (Operation, error) {
	d, err := durationTokens(timeout)
	if err != nil {
		return Operation{}, err
	}
	return validated(command.DownloadOperation(url, d[0]))
}

func validated(op command.Operation, err error) (Operation, error) {
	if err != nil {
		return Operation{}, err
	}
	if err := command.ValidateOperation(op); err != nil {
		return Operation{}, err
	}
	return Operation{op}, nil
}

func number(value uint32) string {
	if value == 0 {
		return ""
	}
	return strconv.FormatUint(uint64(value), 10)
}

func durationTokens(values ...time.Duration) ([]string, error) {
	tokens := make([]string, len(values))
	for i, value := range values {
		if value < 0 || value%time.Millisecond != 0 || value/time.Millisecond > 1<<31-1 {
			return nil, fmt.Errorf("duration must be zero (default) or 1..2147483647 whole milliseconds")
		}
		if value != 0 {
			tokens[i] = strconv.FormatInt(int64(value/time.Millisecond), 10)
		}
	}
	return tokens, nil
}

func connectOptions(opts WiFiConnectOptions) (command.WifiConnectOptions, error) {
	d, err := durationTokens(opts.Timeout)
	if err != nil {
		return command.WifiConnectOptions{}, err
	}
	return command.WifiConnectOptions{SSID: opts.SSID, Passphrase: opts.Passphrase, Security: opts.Security, BSSID: opts.BSSID, Band: opts.Band, MacRandomization: opts.MacRandomization, Timeout: d[0]}, nil
}

func expectationOptions(opts WiFiExpectationOptions) (command.WifiExpectationOptions, error) {
	d, err := durationTokens(opts.Timeout)
	if err != nil {
		return command.WifiExpectationOptions{}, err
	}
	return command.WifiExpectationOptions{SSID: opts.SSID, BSSID: opts.BSSID, Security: opts.Security, Band: opts.Band, RequireIP: opts.RequireIP, RequireValidated: opts.RequireValidated, Timeout: d[0]}, nil
}
