// Package ip provides Dropcheck Festival expectations for IP status checks.
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
	// Family is "ipv4" or "ipv6".
	Family string
	// Scope is a controller-derived scope such as "global" or "link-local".
	Scope string
}

// RouteEntry is a parsed Android route string.
type RouteEntry struct {
	// Raw is the original route string from IpStatus.
	Raw string
	// Destination is the route destination prefix.
	Destination netip.Prefix
	// HasDestination reports whether Destination was parsed.
	HasDestination bool
	// Gateway is the route gateway address when Android reported one.
	Gateway netip.Addr
	// HasGateway reports whether Gateway was parsed.
	HasGateway bool
	// Interface is the route interface name when Android reported one.
	Interface string
	// Family is "ipv4" or "ipv6" when Destination or Gateway identifies it.
	Family string
	// Default reports whether Destination is 0.0.0.0/0 or ::/0.
	Default bool
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
	// DNSAddresses are parsed DNS server addresses.
	DNSAddresses []netip.Addr
	// DHCPServer is the DHCP server address when Android reports one.
	DHCPServer string
	// Routes are the raw route strings reported by the agent.
	Routes []string
	// RouteEntries are parsed route entries.
	RouteEntries []RouteEntry
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

// PrivateDNSActive matches Android's Private DNS active state.
func PrivateDNSActive() festival.BoolMetric {
	return festival.Bool("ip.private_dns_active", func(result festival.Result) (bool, bool, string) {
		ip, ok, reason := from(result)
		return ip.PrivateDNSActive, ok, reason
	})
}

// PrivateDNSServerName matches Android's Private DNS server name.
func PrivateDNSServerName() festival.OrderedMetric[string] {
	return festival.Ordered[string]("ip.private_dns_server_name", func(result festival.Result) (string, bool, string) {
		ip, ok, reason := from(result)
		if !ok {
			return "", false, reason
		}
		if ip.PrivateDNSServerName == "" {
			return "", false, "private dns server name is empty"
		}
		return ip.PrivateDNSServerName, true, ""
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

// DNSServerCount matches the number of DNS server addresses.
func DNSServerCount() festival.OrderedMetric[int] {
	return festival.Ordered[int]("ip.dns_server_count", func(result festival.Result) (int, bool, string) {
		ip, ok, reason := from(result)
		return len(ip.DNSAddresses), ok, reason
	})
}

// IPv4DNSServerCount matches the number of IPv4 DNS server addresses.
func IPv4DNSServerCount() festival.OrderedMetric[int] {
	return festival.Ordered[int]("ip.ipv4_dns_server_count", func(result festival.Result) (int, bool, string) {
		ip, ok, reason := from(result)
		return countAddrs(ip.DNSAddresses, func(addr netip.Addr) bool { return addr.Is4() }), ok, reason
	})
}

// IPv6DNSServerCount matches the number of IPv6 DNS server addresses.
func IPv6DNSServerCount() festival.OrderedMetric[int] {
	return festival.Ordered[int]("ip.ipv6_dns_server_count", func(result festival.Result) (int, bool, string) {
		ip, ok, reason := from(result)
		return countAddrs(ip.DNSAddresses, func(addr netip.Addr) bool { return addr.Is6() }), ok, reason
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

// ParsedRoute returns structured matchers for any parsed route.
func ParsedRoute() RouteSelector {
	return RouteSelector{metric: "ip.route_entry", family: familyAny}
}

// IPv4ParsedRoute returns structured matchers for parsed IPv4 routes.
func IPv4ParsedRoute() RouteSelector {
	return RouteSelector{metric: "ip.ipv4_route_entry", family: familyIPv4}
}

// IPv6ParsedRoute returns structured matchers for parsed IPv6 routes.
func IPv6ParsedRoute() RouteSelector {
	return RouteSelector{metric: "ip.ipv6_route_entry", family: familyIPv6}
}

// DefaultParsedRoute returns structured matchers for default routes.
func DefaultParsedRoute() RouteSelector {
	return RouteSelector{metric: "ip.default_route_entry", family: familyAny, defaultOnly: true}
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

// DNSServerAddress returns matchers for parsed DNS server addresses.
func DNSServerAddress() DNSAddressSelector {
	return DNSAddressSelector{metric: "ip.dns_server_address", family: familyAny}
}

// IPv4DNSServerAddress returns matchers for parsed IPv4 DNS server addresses.
func IPv4DNSServerAddress() DNSAddressSelector {
	return DNSAddressSelector{metric: "ip.ipv4_dns_server_address", family: familyIPv4}
}

// IPv6DNSServerAddress returns matchers for parsed IPv6 DNS server addresses.
func IPv6DNSServerAddress() DNSAddressSelector {
	return DNSAddressSelector{metric: "ip.ipv6_dns_server_address", family: familyIPv6}
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

// Scope requires at least one selected address to have scope.
func (s AddressSelector) Scope(scope string) festival.Expectation {
	return addressScope{selector: s, scope: normalizeScope(scope)}
}

type addressInCIDR struct {
	selector AddressSelector
	cidr     string
}

type addressScope struct {
	selector AddressSelector
	scope    string
}

func (e addressScope) Evaluate(result festival.Result) []festival.Finding {
	ip, ok, reason := from(result)
	if !ok {
		return []festival.Finding{festival.Fail(e.selector.metric, "<missing>", "scope "+e.scope, reason)}
	}
	for _, address := range ip.Addresses {
		if !e.selector.accepts(address.Addr) {
			continue
		}
		if address.Scope == e.scope {
			return []festival.Finding{festival.Pass(e.selector.metric, address.Raw, "scope "+e.scope)}
		}
	}
	return []festival.Finding{festival.Fail(e.selector.metric, strings.Join(ip.AddressStrings, ","), "scope "+e.scope, "no selected address had scope")}
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

// DNSAddressSelector matches parsed DNS server addresses.
type DNSAddressSelector struct {
	metric string
	family addressFamily
}

// InCIDR requires at least one selected DNS server address to be inside cidr.
func (s DNSAddressSelector) InCIDR(cidr string) festival.Expectation {
	return dnsAddressInCIDR{selector: s, cidr: cidr}
}

type dnsAddressInCIDR struct {
	selector DNSAddressSelector
	cidr     string
}

func (e dnsAddressInCIDR) Evaluate(result festival.Result) []festival.Finding {
	ip, ok, reason := from(result)
	if !ok {
		return []festival.Finding{festival.Fail(e.selector.metric, "<missing>", "in "+e.cidr, reason)}
	}
	prefix, err := netip.ParsePrefix(e.cidr)
	if err != nil {
		return []festival.Finding{festival.Fail(e.selector.metric, "<invalid cidr>", "valid CIDR", err.Error())}
	}
	for _, address := range ip.DNSAddresses {
		if !e.selector.accepts(address) {
			continue
		}
		if prefix.Contains(address) {
			return []festival.Finding{festival.Pass(e.selector.metric, address.String(), "in "+e.cidr)}
		}
	}
	return []festival.Finding{festival.Fail(e.selector.metric, strings.Join(ip.DNSServers, ","), "in "+e.cidr, "no selected DNS server was inside CIDR")}
}

func (s DNSAddressSelector) accepts(addr netip.Addr) bool {
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

// RouteSelector matches parsed Android route entries.
type RouteSelector struct {
	metric      string
	family      addressFamily
	destination *netip.Prefix
	gateway     *netip.Addr
	iface       string
	defaultOnly bool
	err         error
}

// Destination restricts matches to one destination prefix.
func (s RouteSelector) Destination(cidr string) RouteSelector {
	prefix, err := netip.ParsePrefix(cidr)
	if err != nil {
		s.err = fmt.Errorf("invalid route destination %q: %w", cidr, err)
		return s
	}
	s.destination = &prefix
	return s
}

// Gateway restricts matches to one gateway address.
func (s RouteSelector) Gateway(value string) RouteSelector {
	addr, err := netip.ParseAddr(value)
	if err != nil {
		s.err = fmt.Errorf("invalid route gateway %q: %w", value, err)
		return s
	}
	s.gateway = &addr
	return s
}

// Interface restricts matches to one network interface.
func (s RouteSelector) Interface(value string) RouteSelector {
	s.iface = value
	return s
}

// Default restricts matches to default routes.
func (s RouteSelector) Default() RouteSelector {
	s.defaultOnly = true
	return s
}

// Exists requires at least one parsed route to match the selector.
func (s RouteSelector) Exists() festival.Expectation {
	return routeExists{selector: s}
}

// Count matches the number of parsed routes selected by the selector.
func (s RouteSelector) Count() festival.OrderedMetric[int] {
	return festival.Ordered[int](s.metric+"_count", func(result festival.Result) (int, bool, string) {
		if s.err != nil {
			return 0, false, s.err.Error()
		}
		ip, ok, reason := from(result)
		if !ok {
			return 0, false, reason
		}
		return len(s.matches(ip.RouteEntries)), true, ""
	})
}

type routeExists struct {
	selector RouteSelector
}

func (e routeExists) Evaluate(result festival.Result) []festival.Finding {
	if e.selector.err != nil {
		return []festival.Finding{festival.Fail(e.selector.metric, "<invalid selector>", "exists", e.selector.err.Error())}
	}
	ip, ok, reason := from(result)
	if !ok {
		return []festival.Finding{festival.Fail(e.selector.metric, "<missing>", "exists", reason)}
	}
	matches := e.selector.matches(ip.RouteEntries)
	if len(matches) > 0 {
		return []festival.Finding{festival.Pass(e.selector.metric, matches[0].Raw, "exists")}
	}
	return []festival.Finding{festival.Fail(e.selector.metric, strings.Join(ip.Routes, ","), "exists", "no parsed route matched selector")}
}

func (s RouteSelector) matches(routes []RouteEntry) []RouteEntry {
	matches := make([]RouteEntry, 0, len(routes))
	for _, route := range routes {
		if s.matchesOne(route) {
			matches = append(matches, route)
		}
	}
	return matches
}

func (s RouteSelector) matchesOne(route RouteEntry) bool {
	if !s.acceptsFamily(route.Family) {
		return false
	}
	if s.defaultOnly && !route.Default {
		return false
	}
	if s.destination != nil && (!route.HasDestination || route.Destination != *s.destination) {
		return false
	}
	if s.gateway != nil && (!route.HasGateway || route.Gateway != *s.gateway) {
		return false
	}
	if s.iface != "" && route.Interface != s.iface {
		return false
	}
	return true
}

func (s RouteSelector) acceptsFamily(family string) bool {
	switch s.family {
	case familyIPv4:
		return family == "ipv4"
	case familyIPv6:
		return family == "ipv6"
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
		DNSAddresses:         parseDNSAddresses(status.GetDnsServers()),
		DHCPServer:           status.GetDhcpServer(),
		Routes:               status.GetRoutes(),
		RouteEntries:         parseRoutes(status.GetRoutes()),
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
			addr := prefix.Addr()
			addresses = append(addresses, AssignedAddress{Raw: value, Addr: addr, Prefix: prefix, HasPrefix: true, Family: familyName(addr), Scope: addressScopeName(addr)})
			continue
		}
		if addr, err := netip.ParseAddr(value); err == nil {
			addresses = append(addresses, AssignedAddress{Raw: value, Addr: addr, Family: familyName(addr), Scope: addressScopeName(addr)})
		}
	}
	return addresses
}

func parseDNSAddresses(values []string) []netip.Addr {
	addresses := make([]netip.Addr, 0, len(values))
	for _, value := range values {
		if addr, err := netip.ParseAddr(value); err == nil {
			addresses = append(addresses, addr)
		}
	}
	return addresses
}

func parseRoutes(values []string) []RouteEntry {
	routes := make([]RouteEntry, 0, len(values))
	for _, value := range values {
		routes = append(routes, parseRoute(value))
	}
	return routes
}

func parseRoute(value string) RouteEntry {
	route := RouteEntry{Raw: value}
	fields := strings.Fields(value)
	if len(fields) == 0 {
		return route
	}
	if destination, err := netip.ParsePrefix(fields[0]); err == nil {
		route.Destination = destination
		route.HasDestination = true
		route.Family = familyName(destination.Addr())
		route.Default = isDefaultPrefix(destination)
	}
	for i, field := range fields {
		switch strings.ToLower(field) {
		case "->", "via":
			if i+1 < len(fields) {
				if gateway, err := netip.ParseAddr(strings.Trim(fields[i+1], ",")); err == nil {
					route.Gateway = gateway
					route.HasGateway = true
					if route.Family == "" {
						route.Family = familyName(gateway)
					}
				}
			}
		case "dev":
			if i+1 < len(fields) {
				route.Interface = strings.Trim(fields[i+1], ",")
			}
		}
	}
	if route.Interface == "" && len(fields) > 0 {
		last := strings.Trim(fields[len(fields)-1], ",")
		if last != "" && last != "unreachable" {
			if _, err := netip.ParseAddr(last); err != nil && !strings.Contains(last, "/") && last != "->" {
				route.Interface = last
			}
		}
	}
	return route
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

func countAddrs(addresses []netip.Addr, keep func(netip.Addr) bool) int {
	count := 0
	for _, address := range addresses {
		if keep(address) {
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
	for _, route := range parseRoutes(routes) {
		if !route.Default {
			continue
		}
		switch family {
		case familyIPv4:
			if route.Family == "ipv4" {
				return true
			}
		case familyIPv6:
			if route.Family == "ipv6" {
				return true
			}
		default:
			return true
		}
	}
	return false
}

func familyName(addr netip.Addr) string {
	switch {
	case addr.Is4():
		return "ipv4"
	case addr.Is6():
		return "ipv6"
	default:
		return ""
	}
}

func addressScopeName(addr netip.Addr) string {
	switch {
	case !addr.IsValid():
		return "invalid"
	case addr.IsUnspecified():
		return "unspecified"
	case addr.IsLoopback():
		return "loopback"
	case addr.IsLinkLocalUnicast():
		return "link-local"
	case addr.IsMulticast():
		return "multicast"
	case addr.IsPrivate():
		return "private"
	case addr.IsGlobalUnicast():
		return "global"
	default:
		return "unknown"
	}
}

func normalizeScope(scope string) string {
	scope = strings.ToLower(strings.TrimSpace(scope))
	scope = strings.ReplaceAll(scope, "_", "-")
	switch scope {
	case "linklocal":
		return "link-local"
	default:
		return scope
	}
}

func isDefaultPrefix(prefix netip.Prefix) bool {
	return prefix.Bits() == 0 && (prefix.Addr() == netip.IPv4Unspecified() || prefix.Addr() == netip.IPv6Unspecified())
}
