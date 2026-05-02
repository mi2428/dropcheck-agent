package command

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"dropcheck/controller/internal/controlpb"
)

const (
	defaultControllerLinkMinBackoff = time.Second
	defaultControllerLinkMaxBackoff = 30 * time.Second
)

// ControllerLinkConfigOptions describes the persistent controller reconnect
// endpoint stored on the Android agent.
type ControllerLinkConfigOptions struct {
	// Enabled controls whether the Android agent should reconnect by direct TCP.
	Enabled bool
	// Endpoint is the controller gRPC address as host:port.
	Endpoint string
	// MinBackoff is the first reconnect delay. Empty uses a conservative default.
	MinBackoff string
	// MaxBackoff bounds reconnect delay growth. Empty uses a conservative default.
	MaxBackoff string
}

// ControllerLinkSetConfigOperation builds a persistent controller endpoint
// update. The session token and per-agent identity are injected by the app layer
// immediately before dispatch, because they belong to the running controller.
func ControllerLinkSetConfigOperation(opts ControllerLinkConfigOptions) (Operation, error) {
	config := &controlpb.ControllerLinkConfig{Enabled: opts.Enabled}
	if opts.Enabled {
		host, port, err := parseControllerEndpoint(opts.Endpoint)
		if err != nil {
			return Operation{}, err
		}
		minBackoff, err := parseMillisToken(opts.MinBackoff, "min-backoff", defaultControllerLinkMinBackoff)
		if err != nil {
			return Operation{}, err
		}
		maxBackoff, err := parseMillisToken(opts.MaxBackoff, "max-backoff", defaultControllerLinkMaxBackoff)
		if err != nil {
			return Operation{}, err
		}
		if maxBackoff < minBackoff {
			return Operation{}, fmt.Errorf("max-backoff must be greater than or equal to min-backoff")
		}
		config.Host = host
		config.Port = port
		config.MinBackoffMs = minBackoff
		config.MaxBackoffMs = maxBackoff
	}
	label := "controller endpoint disabled"
	if opts.Enabled {
		label = "controller endpoint " + config.GetHost() + ":" + strconv.FormatUint(uint64(config.GetPort()), 10)
	}
	return NewOperation("controller.link.set", &controlpb.RunCommand{
		Label: label,
		Command: &controlpb.RunCommand_SetControllerLinkConfig{
			SetControllerLinkConfig: &controlpb.SetControllerLinkConfig{Config: config},
		},
	}, Options{}), nil
}

// ControllerLinkConfigOperation retrieves the persistent controller endpoint.
func ControllerLinkConfigOperation() Operation {
	return NewOperation("controller.link.config", &controlpb.RunCommand{
		Label:   "controller endpoint",
		Command: &controlpb.RunCommand_GetControllerLinkConfig{GetControllerLinkConfig: &controlpb.GetControllerLinkConfig{}},
	}, Options{})
}

// ControllerLinkStatusOperation retrieves the live controller connection state.
func ControllerLinkStatusOperation() Operation {
	return NewOperation("controller.link.status", &controlpb.RunCommand{
		Label:   "controller link",
		Command: &controlpb.RunCommand_GetControllerLinkStatus{GetControllerLinkStatus: &controlpb.GetControllerLinkStatus{}},
	}, Options{})
}

// ControllerReconnectOperation asks the agent to close the current gRPC stream
// after replying so the stored endpoint retry loop can take over.
func ControllerReconnectOperation() Operation {
	return NewOperation("controller.link.reconnect", &controlpb.RunCommand{
		Label:   "controller reconnect",
		Command: &controlpb.RunCommand_ReconnectController{ReconnectController: &controlpb.ReconnectController{}},
	}, Options{})
}

func parseControllerEndpoint(value string) (string, uint32, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", 0, fmt.Errorf("controller endpoint is required")
	}
	host, portText, err := net.SplitHostPort(value)
	if err != nil {
		return "", 0, fmt.Errorf("controller endpoint must be host:port: %w", err)
	}
	host = strings.TrimSpace(host)
	if host == "" {
		return "", 0, fmt.Errorf("controller endpoint host is required")
	}
	port, err := strconv.ParseUint(portText, 10, 32)
	if err != nil || port == 0 || port > 65535 {
		return "", 0, fmt.Errorf("controller endpoint port must be 1-65535")
	}
	return host, uint32(port), nil
}
