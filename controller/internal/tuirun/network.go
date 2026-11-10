package tuirun

import (
	"fmt"
	"strconv"
	"time"

	"github.com/mi2428/dropcheck-agent/controller/internal/command"
)

// Network describes a Wi-Fi target and its connection and cleanup policies.
// Read-only targets do not acquire or clean up Wi-Fi configuration.
type Network struct {
	Name, SSID, BSSID, NetworkID                string
	PSK                                         Secret
	Security, Band, MacRandomization            string
	ConnectTimeout, WaitTimeout                 time.Duration
	RequireIP, RequireValidated                 bool
	WaitConnected, DisconnectAfter, ForgetAfter bool
	Checks                                      []Check
	Agent, ShortName, Rotation                  string
	ConnectPolicy, WaitPolicy                   Policy
	ReadOnly, ActiveProbes                      bool
}

func (n Network) displayName() string {
	switch {
	case n.Name != "":
		return n.Name
	case n.SSID != "":
		return n.SSID
	case n.BSSID != "":
		return n.BSSID
	default:
		return "wifi"
	}
}

func (n Network) connectOperation() (command.Operation, error) {
	if n.SSID == "" && n.BSSID == "" {
		return command.Operation{}, fmt.Errorf("target must set SSID or BSSID")
	}
	passphrase := n.PSK.value
	if n.PSK.env != "" {
		var err error
		passphrase, err = n.PSK.resolve()
		if err != nil {
			return command.Operation{}, err
		}
	}
	return command.WifiConnectOperation(command.WifiConnectOptions{
		SSID: n.SSID, Passphrase: passphrase, Security: n.Security,
		BSSID: n.BSSID, Band: n.Band, MacRandomization: n.MacRandomization,
		Timeout: durationMS(n.ConnectTimeout),
	})
}

func (n Network) waitOperation() (command.Operation, error) {
	return command.WifiWaitConnectedOperation(n.SSID, command.WifiExpectationOptions{
		BSSID: n.BSSID, Security: n.Security, Band: n.Band,
		RequireIP: n.RequireIP, RequireValidated: n.RequireValidated,
		Timeout: durationMS(n.WaitTimeout),
	})
}

func durationMS(value time.Duration) string {
	if value == 0 {
		return ""
	}
	return strconv.FormatInt(value.Milliseconds(), 10)
}
