package command

import (
	"fmt"
	"net"
	"net/url"
	"strings"

	"dropcheck/controller/internal/controlpb"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// ValidateOperation validates already-built commands as well as frontend input.
// It is intentionally independent of transport so a whole scenario can be
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
		if value == "" || strings.ContainsAny(value, " \t\r\n\x00") {
			return fmt.Errorf("invalid probe host")
		}
		return nil
	}
	address := func(value string) error {
		if value == "" {
			return nil
		}
		mac, err := net.ParseMAC(value)
		if err != nil || len(mac) != 6 {
			return fmt.Errorf("invalid BSSID")
		}
		return nil
	}
	endpoint := func(value string) error {
		u, err := url.Parse(value)
		if err != nil || u.Hostname() == "" || u.User != nil || u.Scheme != "http" && u.Scheme != "https" {
			return fmt.Errorf("invalid HTTP endpoint")
		}
		return nil
	}
	connect := func(c *controlpb.ConnectWifi) error {
		if c == nil || c.Ssid == "" && c.Bssid == "" {
			return fmt.Errorf("Wi-Fi target is required")
		}
		if err := address(c.Bssid); err != nil {
			return err
		}
		if c.Security != controlpb.ConnectWifi_SECURITY_UNSPECIFIED && c.Passphrase == "" {
			return fmt.Errorf("passphrase is required for explicit security")
		}
		return nil
	}
	switch c := cmd.Command.(type) {
	case *controlpb.RunCommand_ConnectWifi:
		return connect(c.ConnectWifi)
	case *controlpb.RunCommand_WaitWifiConnected:
		return address(c.WaitWifiConnected.GetBssid())
	case *controlpb.RunCommand_AssertWifi:
		return address(c.AssertWifi.GetBssid())
	case *controlpb.RunCommand_CycleWifi:
		if err := connect(c.CycleWifi.GetConnect()); err != nil {
			return err
		}
		if c.CycleWifi.PingHost != "" {
			if err := host(c.CycleWifi.PingHost); err != nil {
				return err
			}
		}
		if c.CycleWifi.HttpUrl != "" {
			return endpoint(c.CycleWifi.HttpUrl)
		}
	case *controlpb.RunCommand_Ping:
		return host(c.Ping.Host)
	case *controlpb.RunCommand_Traceroute:
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
		if err := endpoint(c.HttpCheck.Url); err != nil {
			return err
		}
		if c.HttpCheck.ExpectedStatus != 0 && (c.HttpCheck.ExpectedStatus < 100 || c.HttpCheck.ExpectedStatus > 599) {
			return fmt.Errorf("invalid HTTP status")
		}
	case *controlpb.RunCommand_Wget:
		return endpoint(c.Wget.Url)
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
