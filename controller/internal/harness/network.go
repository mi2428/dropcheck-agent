package harness

import (
	"fmt"
	"time"

	"dropcheck/controller/internal/command"
)

// Network describes one Wi-Fi network visited by a Dropcheck Harness plan.
type Network struct {
	name             string
	ssid             string
	bssid            string
	psk              Secret
	security         string
	band             string
	macRandomization string
	connectTimeout   time.Duration
	waitTimeout      time.Duration
	requireIP        bool
	requireValidated bool
	waitConnected    bool
	disconnectAfter  bool
	forgetAfter      bool
	checks           []Check
	agent            string
	shortName        string
	rotation         string
	connectPolicy    Policy
	waitPolicy       Policy
	readOnly         bool
}

// WiFi starts a network builder.
func WiFi(name string) Network {
	return Network{name: name, waitConnected: true, requireIP: true, disconnectAfter: true}
}

// ObservedWiFi checks an already-connected physical Wi-Fi Network. It never
// owns the connection, waits for association, or performs cleanup.
func ObservedWiFi(name string) Network { return Network{name: name, readOnly: true} }

// SSID sets the ESSID used for connection and wait checks.
func (n Network) SSID(value string) Network {
	n.ssid = value
	return n
}

// BSSID pins the connection or wait check to one AP.
func (n Network) BSSID(value string) Network {
	n.bssid = value
	return n
}

// PSK sets the passphrase directly.
func (n Network) PSK(value string) Network {
	n.psk = SecretValue(value)
	return n
}

// PSKEnv loads the passphrase from name when the plan runs.
func (n Network) PSKEnv(name string) Network {
	n.psk = SecretEnv(name)
	return n
}

// Security sets the Wi-Fi security token, such as "auto", "wpa2", or "wpa3".
func (n Network) Security(value string) Network {
	n.security = value
	return n
}

// Band sets the Wi-Fi band token used by connect and wait operations.
func (n Network) Band(value string) Network {
	n.band = value
	return n
}

// MacRandomization sets the Android MAC randomization mode.
func (n Network) MacRandomization(value string) Network {
	n.macRandomization = value
	return n
}

// ConnectTimeout sets the Wi-Fi connect timeout.
func (n Network) ConnectTimeout(value time.Duration) Network {
	n.connectTimeout = value
	return n
}

// WaitTimeout sets the post-connect wait timeout.
func (n Network) WaitTimeout(value time.Duration) Network {
	n.waitTimeout = value
	return n
}

// RequireIP controls whether wait connected requires an IP address.
func (n Network) RequireIP(value bool) Network {
	n.requireIP = value
	return n
}

// RequireValidated controls whether wait connected requires Android validation.
func (n Network) RequireValidated(value bool) Network {
	n.requireValidated = value
	return n
}

// WaitConnected controls whether Run performs a post-connect wait operation.
func (n Network) WaitConnected(value bool) Network {
	n.waitConnected = value
	return n
}

// DisconnectAfter controls whether Run disconnects from Wi-Fi during cleanup.
func (n Network) DisconnectAfter(value bool) Network {
	n.disconnectAfter = value
	return n
}

// ForgetAfter controls whether Run forgets the network during cleanup.
func (n Network) ForgetAfter(value bool) Network {
	n.forgetAfter = value
	return n
}

// Checks appends checks that run only for this network.
func (n Network) Checks(checks ...Check) Network {
	n.checks = append(n.checks, checks...)
	return n
}

func (n Network) displayName() string {
	switch {
	case n.name != "":
		return n.name
	case n.ssid != "":
		return n.ssid
	case n.bssid != "":
		return n.bssid
	default:
		return "wifi"
	}
}

func (n Network) connectOperation() (command.Operation, error) {
	if n.ssid == "" && n.bssid == "" {
		return command.Operation{}, fmt.Errorf("target must set SSID or BSSID")
	}
	passphrase := n.psk.value
	if n.psk.env != "" {
		var err error
		passphrase, err = n.psk.resolve()
		if err != nil {
			return command.Operation{}, err
		}
	}
	op, err := command.WifiConnectOperation(command.WifiConnectOptions{
		SSID:             n.ssid,
		Passphrase:       passphrase,
		Security:         n.security,
		BSSID:            n.bssid,
		Band:             n.band,
		MacRandomization: n.macRandomization,
		Timeout:          durationMS(n.connectTimeout),
	})
	return op, err
}

func (n Network) waitOperation() (command.Operation, error) {
	op, err := command.WifiWaitConnectedOperation(n.ssid, command.WifiExpectationOptions{
		BSSID:            n.bssid,
		Security:         n.security,
		Band:             n.band,
		RequireIP:        n.requireIP,
		RequireValidated: n.requireValidated,
		Timeout:          durationMS(n.waitTimeout),
	})
	return op, err
}

func (n Network) Agent(selector string) Network   { n.agent = selector; return n }
func (n Network) ShortName(name string) Network   { n.shortName = name; return n }
func (n Network) ConnectPolicy(p Policy) Network  { n.connectPolicy = p; return n }
func (n Network) WaitPolicy(p Policy) Network     { n.waitPolicy = p; return n }
func (n Network) MACRotation(mode string) Network { n.rotation = mode; return n }
