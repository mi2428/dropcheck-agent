// Package ip provides festival expectations for IP status checks.
package ip

import (
	"fmt"
	"net/netip"
	"slices"
	"strings"

	"dropcheck/controller/internal/controlpb"
	"dropcheck/controller/internal/festival"
)

// AssignedAddress is a parsed address entry reported by the Android agent.
type AssignedAddress struct {
	// Raw is the original address string from IpStatus.
	Raw string
	// Addr is the parsed IP address.
	Addr netip.Addr
	// Prefix is the parsed CIDR prefix when Raw included prefix length.
	Prefix netip.Prefix
	// HasPrefix reports whether Raw included CIDR prefix length.
	HasPrefix bool
}

// Result is the IP-status-specific view passed to custom assertions.
type Result struct {
	// Raw is the original protobuf payload for callers that need every field.
	Raw *controlpb.IpStatus
	// Status is the outer command status returned by the agent.
	Status controlpb.CommandResult_Status
	// NetworkID is Android's active network identifier.
	NetworkID string
	// Transports are Android network transports, such as WIFI.
	Transports []string
	// Validated reports Android validated internet access.
	Validated bool
	// Internet reports whether the network advertises internet capability.
	Internet bool
	// Interface is the network interface name.
	Interface string
	// MTU is the interface MTU in bytes.
	MTU uint32
	// AddressStrings are the raw address strings reported by the agent.
	AddressStrings []string
	// Addresses are parsed address entries.
	Addresses []AssignedAddress
	// DNSServers are the raw DNS server strings reported by the agent.
	DNSServers []string
	// DHCPServer is the DHCP server address when Android reports one.
	DHCPServer string
	// Routes are the raw route strings reported by the agent.
	Routes []string
	// Capabilities are Android network capabilities.
	Capabilities []string
	// NAT64Prefix is Android's NAT64 prefix when present.
	NAT64Prefix string
	// PrivateDNSActive reports whether Android Private DNS is active.
	PrivateDNSActive bool
	// PrivateDNSServerName is the configured Private DNS hostname.
	PrivateDNSServerName string
	// RawLinkProperties is Android's raw LinkProperties string.
	RawLinkProperties string
}

// AddressCount matches the number of assigned addresses.
func AddressCount() festival.OrderedMetric[int] {
	return festival.Ordered[int]("ip.address_count", func(result festival.Result) (int, bool, string) {
		ip, ok, reason := from(result)
		return len(ip.Addresses), ok, reason
	})
}

// IPv4AddressCount matches the number of assigned IPv4 addresses.
func IPv4AddressCount() festival.OrderedMetric[int] {
	return festival.Ordered[int]("ip.ipv4_address_count", func(result festival.Result) (int, bool, string) {
		ip, ok, reason := from(result)
		return countAddresses(ip.Addresses, func(addr netip.Addr) bool { return addr.Is4() }), ok, reason
	})
}

// IPv6AddressCount matches the number of assigned IPv6 addresses.
func IPv6AddressCount() festival.OrderedMetric[int] {
	return festival.Ordered[int]("ip.ipv6_address_count", func(result festival.Result) (int, bool, string) {
		ip, ok, reason := from(result)
		return countAddresses(ip.Addresses, func(addr netip.Addr) bool { return addr.Is6() }), ok, reason
	})
}

// MTU matches the interface MTU.
func MTU() festival.OrderedMetric[uint32] {
	return festival.Ordered[uint32]("ip.mtu", func(result festival.Result) (uint32, bool, string) {
		ip, ok, reason := from(result)
		return ip.MTU, ok, reason
	})
}

// Validated matches Android's validated internet state.
func Validated() festival.BoolMetric {
	return festival.Bool("ip.validated", func(result festival.Result) (bool, bool, string) {
		ip, ok, reason := from(result)
		return ip.Validated, ok, reason
	})
}

// Internet matches Android's internet capability state.
func Internet() festival.BoolMetric {
	return festival.Bool("ip.internet", func(result festival.Result) (bool, bool, string) {
		ip, ok, reason := from(result)
		return ip.Internet, ok, reason
	})
}

// DHCPServer matches the DHCP server address string exactly.
func DHCPServer() festival.OrderedMetric[string] {
	return festival.Ordered[string]("ip.dhcp_server", func(result festival.Result) (string, bool, string) {
		ip, ok, reason := from(result)
		if !ok {
			return "", false, reason
		}
		if ip.DHCPServer == "" {
			return "", false, "dhcp server is empty"
		}
		return ip.DHCPServer, true, ""
	})
}

// NAT64Prefix matches Android's NAT64 prefix exactly.
func NAT64Prefix() festival.OrderedMetric[string] {
	return festival.Ordered[string]("ip.nat64_prefix", func(result festival.Result) (string, bool, string) {
		ip, ok, reason := from(result)
		if !ok {
			return "", false, reason
		}
		if ip.NAT64Prefix == "" {
			return "", false, "nat64 prefix is empty"
		}
		return ip.NAT64Prefix, true, ""
	})
}

// DefaultRoute matches whether any default route is present.
func DefaultRoute() festival.BoolMetric {
	return festival.Bool("ip.default_route", func(result festival.Result) (bool, bool, string) {
		ip, ok, reason := from(result)
		return hasDefaultRoute(ip.Routes, familyAny), ok, reason
	})
}

// IPv4DefaultRoute matches whether an IPv4 default route is present.
func IPv4DefaultRoute() festival.BoolMetric {
	return festival.Bool("ip.ipv4_default_route", func(result festival.Result) (bool, bool, string) {
		ip, ok, reason := from(result)
		return hasDefaultRoute(ip.Routes, familyIPv4), ok, reason
	})
}

// IPv6DefaultRoute matches whether an IPv6 default route is present.
func IPv6DefaultRoute() festival.BoolMetric {
	return festival.Bool("ip.ipv6_default_route", func(result festival.Result) (bool, bool, string) {
		ip, ok, reason := from(result)
		return hasDefaultRoute(ip.Routes, familyIPv6), ok, reason
	})
}

// Address returns matchers for any assigned address.
func Address() AddressSelector {
	return AddressSelector{metric: "ip.address", family: familyAny}
}

// IPv4Address returns matchers for assigned IPv4 addresses.
func IPv4Address() AddressSelector {
	return AddressSelector{metric: "ip.ipv4_address", family: familyIPv4}
}

// IPv6Address returns matchers for assigned IPv6 addresses.
func IPv6Address() AddressSelector {
	return AddressSelector{metric: "ip.ipv6_address", family: familyIPv6}
}

// Prefix returns matchers for any assigned address prefix.
func Prefix() PrefixSelector {
	return PrefixSelector{metric: "ip.prefix", family: familyAny}
}

// IPv4Prefix returns matchers for assigned IPv4 prefixes.
func IPv4Prefix() PrefixSelector {
	return PrefixSelector{metric: "ip.ipv4_prefix", family: familyIPv4}
}

// IPv6Prefix returns matchers for assigned IPv6 prefixes.
func IPv6Prefix() PrefixSelector {
	return PrefixSelector{metric: "ip.ipv6_prefix", family: familyIPv6}
}

// DNSServer returns matchers for DNS server addresses.
func DNSServer() StringListSelector {
	return StringListSelector{metric: "ip.dns_server", values: func(r Result) []string { return r.DNSServers }}
}

// Transport returns matchers for Android network transport strings.
func Transport() StringListSelector {
	return StringListSelector{metric: "ip.transport", values: func(r Result) []string { return r.Transports }}
}

// Route returns matchers for route strings.
func Route() StringListSelector {
	return StringListSelector{metric: "ip.route", values: func(r Result) []string { return r.Routes }}
}

// Capability returns matchers for Android network capabilities.
func Capability() StringListSelector {
	return StringListSelector{metric: "ip.capability", values: func(r Result) []string { return r.Capabilities }}
}

// RawLinkProperties returns matchers for Android's raw LinkProperties string.
func RawLinkProperties() TextSelector {
	return TextSelector{metric: "ip.raw_link_properties", value: func(r Result) string { return r.RawLinkProperties }}
}

// Assert evaluates a custom IP status assertion against the typed result view.
func Assert(name string, fn func(Result) error) festival.Expectation {
	return assertion{name: name, fn: fn}
}

type assertion struct {
	name string
	fn   func(Result) error
}

func (a assertion) Evaluate(result festival.Result) []festival.Finding {
	ip, ok, reason := from(result)
	metric := "ip.assert." + a.name
	if !ok {
		return []festival.Finding{festival.Fail(metric, "<missing>", "custom assertion passed", reason)}
	}
	if err := a.fn(ip); err != nil {
		return []festival.Finding{festival.Fail(metric, "failed", "custom assertion passed", err.Error())}
	}
	return []festival.Finding{festival.Pass(metric, "passed", "custom assertion passed")}
}

type addressFamily int

const (
	familyAny addressFamily = iota
	familyIPv4
	familyIPv6
)

// AddressSelector matches assigned addresses.
type AddressSelector struct {
	metric string
	family addressFamily
}

// InCIDR requires at least one selected address to be inside cidr.
func (s AddressSelector) InCIDR(cidr string) festival.Expectation {
	return addressInCIDR{selector: s, cidr: cidr}
}

type addressInCIDR struct {
	selector AddressSelector
	cidr     string
}

func (e addressInCIDR) Evaluate(result festival.Result) []festival.Finding {
	ip, ok, reason := from(result)
	if !ok {
		return []festival.Finding{festival.Fail(e.selector.metric, "<missing>", "in "+e.cidr, reason)}
	}
	prefix, err := netip.ParsePrefix(e.cidr)
	if err != nil {
		return []festival.Finding{festival.Fail(e.selector.metric, "<invalid cidr>", "valid CIDR", err.Error())}
	}
	for _, address := range ip.Addresses {
		if !e.selector.accepts(address.Addr) {
			continue
		}
		if prefix.Contains(address.Addr) {
			return []festival.Finding{festival.Pass(e.selector.metric, address.Raw, "in "+e.cidr)}
		}
	}
	return []festival.Finding{festival.Fail(e.selector.metric, strings.Join(ip.AddressStrings, ","), "in "+e.cidr, "no selected address was inside CIDR")}
}

func (s AddressSelector) accepts(addr netip.Addr) bool {
	switch s.family {
	case familyIPv4:
		return addr.Is4()
	case familyIPv6:
		return addr.Is6()
	default:
		return true
	}
}

// PrefixSelector matches assigned address prefixes.
type PrefixSelector struct {
	metric string
	family addressFamily
}

// Within requires at least one assigned prefix to be contained by cidr.
func (s PrefixSelector) Within(cidr string) festival.Expectation {
	return prefixWithin{selector: s, cidr: cidr}
}

type prefixWithin struct {
	selector PrefixSelector
	cidr     string
}

func (e prefixWithin) Evaluate(result festival.Result) []festival.Finding {
	ip, ok, reason := from(result)
	if !ok {
		return []festival.Finding{festival.Fail(e.selector.metric, "<missing>", "within "+e.cidr, reason)}
	}
	expected, err := netip.ParsePrefix(e.cidr)
	if err != nil {
		return []festival.Finding{festival.Fail(e.selector.metric, "<invalid cidr>", "valid CIDR", err.Error())}
	}
	for _, address := range ip.Addresses {
		if !address.HasPrefix || !e.selector.accepts(address.Addr) {
			continue
		}
		if prefixContainsPrefix(expected, address.Prefix) {
			return []festival.Finding{festival.Pass(e.selector.metric, address.Prefix.String(), "within "+e.cidr)}
		}
	}
	return []festival.Finding{festival.Fail(e.selector.metric, strings.Join(ip.AddressStrings, ","), "within "+e.cidr, "no selected assigned prefix was contained by CIDR")}
}

func (s PrefixSelector) accepts(addr netip.Addr) bool {
	switch s.family {
	case familyIPv4:
		return addr.Is4()
	case familyIPv6:
		return addr.Is6()
	default:
		return true
	}
}

// StringListSelector matches a string list in IpStatus.
type StringListSelector struct {
	metric string
	values func(Result) []string
}

// Contains requires the selected list to contain value exactly.
func (s StringListSelector) Contains(value string) festival.Expectation {
	return stringListContains{selector: s, value: value}
}

// ContainsPrefix requires one selected value to have prefix.
func (s StringListSelector) ContainsPrefix(prefix string) festival.Expectation {
	return stringListPrefix{selector: s, prefix: prefix}
}

type stringListContains struct {
	selector StringListSelector
	value    string
}

func (e stringListContains) Evaluate(result festival.Result) []festival.Finding {
	ip, ok, reason := from(result)
	if !ok {
		return []festival.Finding{festival.Fail(e.selector.metric, "<missing>", "contains "+e.value, reason)}
	}
	values := e.selector.values(ip)
	if slices.Contains(values, e.value) {
		return []festival.Finding{festival.Pass(e.selector.metric, e.value, "contains "+e.value)}
	}
	return []festival.Finding{festival.Fail(e.selector.metric, strings.Join(values, ","), "contains "+e.value, "value not found")}
}

type stringListPrefix struct {
	selector StringListSelector
	prefix   string
}

func (e stringListPrefix) Evaluate(result festival.Result) []festival.Finding {
	ip, ok, reason := from(result)
	if !ok {
		return []festival.Finding{festival.Fail(e.selector.metric, "<missing>", "contains prefix "+e.prefix, reason)}
	}
	values := e.selector.values(ip)
	for _, value := range values {
		if strings.HasPrefix(value, e.prefix) {
			return []festival.Finding{festival.Pass(e.selector.metric, value, "contains prefix "+e.prefix)}
		}
	}
	return []festival.Finding{festival.Fail(e.selector.metric, strings.Join(values, ","), "contains prefix "+e.prefix, "prefix not found")}
}

// TextSelector matches a string field in IpStatus.
type TextSelector struct {
	metric string
	value  func(Result) string
}

// Contains requires the selected text to contain value.
func (s TextSelector) Contains(value string) festival.Expectation {
	return textContains{selector: s, value: value}
}

type textContains struct {
	selector TextSelector
	value    string
}

func (e textContains) Evaluate(result festival.Result) []festival.Finding {
	ip, ok, reason := from(result)
	if !ok {
		return []festival.Finding{festival.Fail(e.selector.metric, "<missing>", "contains "+e.value, reason)}
	}
	value := e.selector.value(ip)
	if strings.Contains(value, e.value) {
		return []festival.Finding{festival.Pass(e.selector.metric, e.value, "contains "+e.value)}
	}
	return []festival.Finding{festival.Fail(e.selector.metric, value, "contains "+e.value, "value not found")}
}

func from(result festival.Result) (Result, bool, string) {
	raw := result.Run.Raw
	if raw == nil {
		return Result{}, false, "raw result is nil"
	}
	status := raw.GetIpStatus()
	if status == nil {
		return Result{}, false, fmt.Sprintf("command payload is %T, not ip status", raw.GetPayload())
	}
	return Result{
		Raw:                  status,
		Status:               raw.GetStatus(),
		NetworkID:            status.GetNetworkId(),
		Transports:           status.GetTransports(),
		Validated:            status.GetValidated(),
		Internet:             status.GetInternet(),
		Interface:            status.GetInterfaceName(),
		MTU:                  status.GetMtu(),
		AddressStrings:       status.GetAddresses(),
		Addresses:            parseAddresses(status.GetAddresses()),
		DNSServers:           status.GetDnsServers(),
		DHCPServer:           status.GetDhcpServer(),
		Routes:               status.GetRoutes(),
		Capabilities:         status.GetCapabilities(),
		NAT64Prefix:          status.GetNat64Prefix(),
		PrivateDNSActive:     status.GetPrivateDnsActive(),
		PrivateDNSServerName: status.GetPrivateDnsServerName(),
		RawLinkProperties:    status.GetRawLinkProperties(),
	}, true, ""
}

func parseAddresses(values []string) []AssignedAddress {
	addresses := make([]AssignedAddress, 0, len(values))
	for _, value := range values {
		if prefix, err := netip.ParsePrefix(value); err == nil {
			addresses = append(addresses, AssignedAddress{Raw: value, Addr: prefix.Addr(), Prefix: prefix, HasPrefix: true})
			continue
		}
		if addr, err := netip.ParseAddr(value); err == nil {
			addresses = append(addresses, AssignedAddress{Raw: value, Addr: addr})
		}
	}
	return addresses
}

func countAddresses(addresses []AssignedAddress, keep func(netip.Addr) bool) int {
	count := 0
	for _, address := range addresses {
		if keep(address.Addr) {
			count++
		}
	}
	return count
}

func prefixContainsPrefix(container netip.Prefix, contained netip.Prefix) bool {
	return container.Contains(contained.Addr()) &&
		container.Bits() <= contained.Bits()
}

func hasDefaultRoute(routes []string, family addressFamily) bool {
	for _, route := range routes {
		normalized := strings.ToLower(route)
		switch family {
		case familyIPv4:
			if strings.Contains(normalized, "0.0.0.0/0") {
				return true
			}
		case familyIPv6:
			if strings.Contains(normalized, "::/0") {
				return true
			}
		default:
			if strings.Contains(normalized, "0.0.0.0/0") || strings.Contains(normalized, "::/0") {
				return true
			}
		}
	}
	return false
}
