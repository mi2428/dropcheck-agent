package harness

import (
	"context"
	"fmt"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"dropcheck/controller/internal/command"
	"dropcheck/controller/internal/control"
)

type GatewayPingOptions struct {
	Count, SizeBytes uint32
	Family           string
	Timeout          time.Duration
}

func GatewayPing(name, sourceID string, opts GatewayPingOptions, policy Policy, required bool, expect ...Expectation) Check {
	return operationCheck{step{name: name, sourceID: sourceID, gateway: &opts, policy: policy, required: required, expectations: append([]Expectation(nil), expect...)}}
}

func (o GatewayPingOptions) validate() error {
	if o.Timeout < 0 {
		return fmt.Errorf("negative gateway timeout")
	}
	if _, err := gatewayFamily(o.Family); err != nil {
		return err
	}
	_, err := command.PingOperation(command.PingOptions{Host: "validation.invalid", Count: optionalNumber(o.Count), Size: optionalNumber(o.SizeBytes), Timeout: durationMS(o.Timeout)})
	return err
}

func optionalNumber(v uint32) string {
	if v == 0 {
		return ""
	}
	return strconv.FormatUint(uint64(v), 10)
}

func executeGateway(ctx context.Context, r OperationRunner, agent control.AgentInfo, target Network, opts GatewayPingOptions) (OperationResult, error) {
	ipOp, err := bindSelector(command.IPStatusOperation(), target.ssid)
	if err != nil {
		return OperationResult{}, err
	}
	ip, err := ExecuteOperation(ctx, r, agent, ipOp)
	if err != nil || ip.Raw == nil || ip.Raw.GetIpStatus() == nil || ip.Raw.GetStatus().String() != "STATUS_OK" {
		return ip, err
	}
	family, err := gatewayFamily(opts.Family)
	if err != nil {
		return ip, err
	}
	route, found := DefaultGateway(ip.Raw.GetIpStatus().GetRoutes(), family)
	if !found {
		ip.Raw = nil
		ip.Findings = append(ip.Findings, MissingFinding("gateway", "<missing>", "selected Network default gateway", "default gateway is unavailable"))
		return ip, nil
	}
	host := route.Gateway.String()
	if route.Gateway.Is6() && route.Gateway.IsLinkLocalUnicast() {
		if route.Interface == "" {
			ip.Raw = nil
			ip.Findings = append(ip.Findings, MissingFinding("gateway", "<missing>", "IPv6 gateway zone", "gateway interface is unavailable"))
			return ip, nil
		}
		host += "%" + route.Interface
	}
	probeFamily := "ipv4"
	if route.Gateway.Is6() {
		probeFamily = "ipv6"
	}
	op, err := command.PingOperation(command.PingOptions{Host: host, Count: optionalNumber(opts.Count), Size: optionalNumber(opts.SizeBytes), Family: probeFamily, Timeout: durationMS(opts.Timeout)})
	if err != nil {
		return ip, err
	}
	op, err = bindSelector(op, target.ssid)
	if err != nil {
		return ip, err
	}
	ping, err := ExecuteOperation(ctx, r, agent, op)
	ping.Parts = append(ip.Parts, ping.Parts...)
	return ping, err
}

type GatewayRoute struct {
	Gateway           netip.Addr
	Interface, Family string
}

func gatewayFamily(value string) (string, error) {
	switch value {
	case "", "ipv4", "4":
		return "ipv4", nil
	case "ipv6", "6":
		return "ipv6", nil
	case "auto", "any", "all":
		return "any", nil
	}
	return "", fmt.Errorf("invalid gateway family")
}

// DefaultGateway understands both LinkProperties and ip-route forms and keeps
// the interface required for an IPv6 link-local zone.
func DefaultGateway(routes []string, family string) (GatewayRoute, bool) {
	if family == "any" {
		if route, ok := DefaultGateway(routes, "ipv4"); ok {
			return route, true
		}
		return DefaultGateway(routes, "ipv6")
	}
	for _, raw := range routes {
		fields := strings.Fields(raw)
		if len(fields) == 0 {
			continue
		}
		destination := strings.Trim(fields[0], ",")
		if destination != "default" {
			prefix, err := netip.ParsePrefix(destination)
			if err != nil || prefix.Bits() != 0 {
				continue
			}
		}
		route := GatewayRoute{}
		gatewayIndex := -1
		for i, token := range fields {
			if i+1 >= len(fields) {
				continue
			}
			switch token {
			case "->", "via":
				route.Gateway, _ = netip.ParseAddr(strings.Trim(fields[i+1], ","))
				gatewayIndex = i + 1
			case "dev":
				route.Interface = strings.Trim(fields[i+1], ",")
			}
		}
		if !route.Gateway.IsValid() {
			continue
		}
		route.Family = "ipv4"
		if route.Gateway.Is6() {
			route.Family = "ipv6"
		}
		if route.Family != family {
			continue
		}
		if route.Interface == "" && gatewayIndex+1 < len(fields) {
			candidate := strings.Trim(fields[gatewayIndex+1], ",")
			switch candidate {
			case "mtu", "src", "metric", "table", "proto", "scope":
			default:
				route.Interface = candidate
			}
		}
		return route, true
	}
	return GatewayRoute{}, false
}

func routeDestinationFamily(value string) (string, bool) {
	value = strings.Trim(value, ",")
	if value == "default" {
		return "", true
	}
	prefix, err := netip.ParsePrefix(value)
	if err != nil || prefix.Bits() != 0 {
		return "", false
	}
	if prefix.Addr().Is4() {
		return "ipv4", true
	}
	return "ipv6", true
}
