package command

import (
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"unicode"

	"github.com/mi2428/dropcheck-agent/controller/internal/controlpb"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// ValidateOperation validates already-built commands as well as frontend input.
// It is intentionally independent of transport so a whole tui plan can be
// rejected before its first operation.
func ValidateOperation(op Operation) error {
	cmd, _, err := BuildRunCommand(op)
	if err != nil {
		return err
	}
	if cmd.Command == nil {
		return fmt.Errorf("command is not set")
	}
	if err := validateMessage(cmd.ProtoReflect()); err != nil {
		return err
	}
	host := func(value string) error {
		if value == "" || strings.HasPrefix(value, "-") || strings.ContainsAny(value, " \t\r\n\x00") {
			return fmt.Errorf("invalid probe host")
		}
		if strings.Contains(value, ":") {
			if ip, err := netip.ParseAddr(value); err != nil || !ip.Is6() {
				return fmt.Errorf("invalid IPv6 address")
			}
		} else if strings.Contains(value, ".") && strings.Trim(value, "0123456789.") == "" {
			parts := strings.Split(value, ".")
			if len(parts) != 4 {
				return fmt.Errorf("invalid IPv4 address")
			}
			for _, part := range parts {
				if _, err := strconv.ParseUint(part, 10, 8); err != nil || len(part) > 3 {
					return fmt.Errorf("invalid IPv4 address")
				}
			}
		}
		return nil
	}
	for _, hop := range op.Options.TracerouteRequiredHops {
		if err := host(hop); err != nil {
			return err
		}
	}
	address := validateBSSID
	if err := address(op.Options.WifiEHTBSSID); err != nil {
		return err
	}
	selector := func(value *controlpb.NetworkSelector) error {
		if value == nil {
			return nil
		}
		if err := address(value.GetBssid()); err != nil {
			return err
		}
		if value.GetNetworkId() != "" {
			id, err := strconv.ParseUint(value.GetNetworkId(), 10, 31)
			if err != nil || id == 0 || strconv.FormatUint(id, 10) != value.GetNetworkId() {
				return fmt.Errorf("invalid Android Network ID")
			}
		}
		return nil
	}
	switch c := cmd.Command.(type) {
	case *controlpb.RunCommand_GetNetwork:
		if c.GetNetwork.GetDefaultNetwork() == (c.GetNetwork.GetNetworkId() != "") || !c.GetNetwork.GetDefaultNetwork() && !ValidNetworkID(c.GetNetwork.GetNetworkId()) {
			return fmt.Errorf("specify exactly one of default Network or a canonical Network ID")
		}
	case *controlpb.RunCommand_GetIpStatus:
		err = selector(c.GetIpStatus.GetSelector())
	case *controlpb.RunCommand_Ping:
		err = selector(c.Ping.GetSelector())
	case *controlpb.RunCommand_Traceroute:
		err = selector(c.Traceroute.GetSelector())
	case *controlpb.RunCommand_PathMtu:
		err = selector(c.PathMtu.GetSelector())
	case *controlpb.RunCommand_GlobalIp:
		err = selector(c.GlobalIp.GetSelector())
	case *controlpb.RunCommand_ResolveDns:
		err = selector(c.ResolveDns.GetSelector())
	case *controlpb.RunCommand_HttpCheck:
		err = selector(c.HttpCheck.GetSelector())
	case *controlpb.RunCommand_Wget:
		err = selector(c.Wget.GetSelector())
	}
	if err != nil {
		return err
	}
	switch c := cmd.Command.(type) {
	case *controlpb.RunCommand_ConnectWifi:
		return validateConnect(c.ConnectWifi)
	case *controlpb.RunCommand_WaitWifiConnected:
		return address(c.WaitWifiConnected.GetBssid())
	case *controlpb.RunCommand_AssertWifi:
		return address(c.AssertWifi.GetBssid())
	case *controlpb.RunCommand_ForgetWifi:
		f := c.ForgetWifi
		if f == nil || f.Selector == nil || f.Target != "" {
			return fmt.Errorf("forget requires only a typed SSID or configuration ID")
		}
		switch selector := f.Selector.(type) {
		case *controlpb.ForgetWifi_Ssid:
			if selector.Ssid == "" || strings.ContainsFunc(selector.Ssid, unicode.IsControl) {
				return fmt.Errorf("invalid forget SSID")
			}
		case *controlpb.ForgetWifi_ConfigId:
			if selector.ConfigId < 0 {
				return fmt.Errorf("invalid Wi-Fi configuration ID")
			}
		}
	case *controlpb.RunCommand_CycleWifi:
		if err := validateConnect(c.CycleWifi.GetConnect()); err != nil {
			return err
		}
		if c.CycleWifi.Count > 100 || c.CycleWifi.PauseMs > 60000 {
			return fmt.Errorf("Wi-Fi cycle count or pause exceeds agent limit")
		}
		if c.CycleWifi.PingHost != "" {
			if err := host(c.CycleWifi.PingHost); err != nil {
				return err
			}
		}
		if c.CycleWifi.HttpUrl != "" {
			return validateHTTPEndpoint(c.CycleWifi.HttpUrl)
		}
	case *controlpb.RunCommand_MonitorWifi:
		if c.MonitorWifi.DurationMs > 120000 || c.MonitorWifi.IntervalMs != 0 && (c.MonitorWifi.IntervalMs < 100 || c.MonitorWifi.IntervalMs > 60000) {
			return fmt.Errorf("Wi-Fi monitor duration must be 1..120000 ms and interval 100..60000 ms (zero uses defaults)")
		}
	case *controlpb.RunCommand_Ping:
		return host(c.Ping.Host)
	case *controlpb.RunCommand_Traceroute:
		if c.Traceroute.GetMaxHops() > 255 {
			return fmt.Errorf("traceroute max hops exceeds 255")
		}
		return host(c.Traceroute.Host)
	case *controlpb.RunCommand_PathMtu:
		if err := host(c.PathMtu.Host); err != nil {
			return err
		}
		if c.PathMtu.MinMtuBytes != 0 && c.PathMtu.MaxMtuBytes != 0 && c.PathMtu.MinMtuBytes > c.PathMtu.MaxMtuBytes {
			return fmt.Errorf("invalid MTU bounds")
		}
	case *controlpb.RunCommand_ResolveDns:
		if err := host(c.ResolveDns.Name); err != nil {
			return err
		}
		for _, record := range c.ResolveDns.Qtypes {
			if record != controlpb.DnsRecordType_DNS_RECORD_TYPE_A && record != controlpb.DnsRecordType_DNS_RECORD_TYPE_AAAA {
				return fmt.Errorf("invalid DNS record type")
			}
		}
	case *controlpb.RunCommand_HttpCheck:
		if err := validateHTTPEndpoint(c.HttpCheck.Url); err != nil {
			return err
		}
		if c.HttpCheck.ExpectedStatus != 0 && (c.HttpCheck.ExpectedStatus < 100 || c.HttpCheck.ExpectedStatus > 599) {
			return fmt.Errorf("invalid HTTP status")
		}
	case *controlpb.RunCommand_Wget:
		return validateHTTPEndpoint(c.Wget.Url)
	case *controlpb.RunCommand_GetWifiScanDetail:
		if c.GetWifiScanDetail.Target == "" {
			return fmt.Errorf("scan detail target is required")
		}
	}
	if TimeoutFor(cmd) <= 0 {
		return fmt.Errorf("operation deadline overflow")
	}
	return nil
}

func validateBSSID(value string) error {
	if value == "" {
		return nil
	}
	mac, err := net.ParseMAC(value)
	if err != nil || len(mac) != 6 || mac.String() != strings.ToLower(value) {
		return fmt.Errorf("invalid BSSID")
	}
	return nil
}

func validateWifiTarget(c *controlpb.ConnectWifi) error {
	if c == nil || c.Ssid == "" && c.Bssid == "" {
		return fmt.Errorf("Wi-Fi target is required")
	}
	return validateBSSID(c.Bssid)
}

func validateConnect(c *controlpb.ConnectWifi) error {
	if err := validateWifiTarget(c); err != nil {
		return err
	}
	// Auto selects PSK/SAE from Android's scan cache; it does not mean open Wi-Fi.
	if !ValidPassphrase(c.Passphrase) {
		return fmt.Errorf("passphrase must be 8..63 UTF-8 bytes without controls or 64 hex digits")
	}
	return nil
}

func validateHTTPEndpoint(value string) error {
	u, err := url.Parse(value)
	if err != nil || u.Hostname() == "" || u.User != nil || u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("invalid HTTP endpoint")
	}
	if port := u.Port(); port != "" {
		if _, err := strconv.ParseUint(port, 10, 16); err != nil {
			return fmt.Errorf("invalid HTTP port")
		}
	}
	return nil
}

func validateMessage(message protoreflect.Message) error {
	if !message.IsValid() {
		return fmt.Errorf("command payload is missing")
	}
	var problem error
	message.Range(func(field protoreflect.FieldDescriptor, value protoreflect.Value) bool {
		check := func(v protoreflect.Value) error {
			switch field.Kind() {
			case protoreflect.Uint32Kind:
				// Android's generated Java uint32 accessors return signed Int.
				// Values above this bound become negative/defaulted on the agent.
				if v.Uint() > 1<<31-1 {
					return fmt.Errorf("%s exceeds Android signed int range", field.Name())
				}
			case protoreflect.EnumKind:
				if field.Enum().Values().ByNumber(v.Enum()) == nil {
					return fmt.Errorf("unknown enum %s", field.Name())
				}
			case protoreflect.MessageKind:
				return validateMessage(v.Message())
			}
			return nil
		}
		if field.IsList() {
			list := value.List()
			for i := 0; i < list.Len(); i++ {
				if problem = check(list.Get(i)); problem != nil {
					return false
				}
			}
		} else {
			problem = check(value)
		}
		return problem == nil
	})
	return problem
}
