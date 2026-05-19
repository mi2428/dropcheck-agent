package watch

import (
	"context"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"dropcheck/controller/internal/command"
	"dropcheck/controller/internal/control"
	"dropcheck/controller/internal/controlpb"
	"dropcheck/controller/internal/runner"
)

const gatewayPingCheckType = "gateway_ping"

func isGatewayPingCheck(check Check) bool {
	return strings.EqualFold(strings.TrimSpace(check.Type), gatewayPingCheckType)
}

func runGatewayPingCheckWithSkip(ctx context.Context, opRunner OperationRunner, agent control.AgentInfo, round uint64, target Target, check Check, skip *SkipController, emit func(Event) error) (bool, bool, error) {
	step := StepSnapshot{Name: check.DisplayName(), Type: check.Type, Operation: "gateway.ping"}
	targetSnapshot := snapshotTarget(target)
	started := time.Now()
	maxAttempts := operationMaxAttempts(step)
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		exec, runErr, err, skipped := runGatewayPingAttempt(ctx, opRunner, agent, round, target, check, step, attempt, maxAttempts, skip, emit)
		if err != nil {
			return false, false, err
		}
		if skipped {
			return false, true, emitOperatorSkippedStep(round, targetSnapshot, step, started, emit)
		}
		failedStep, operationFailed := operationFailureStep(step, exec, runErr)
		findings := checkFindings(target, check, exec)
		if !operationFailed && len(findings) == 0 {
			if err := emitRetrySucceeded(round, target, step, attempt, maxAttempts, emit); err != nil {
				return false, false, err
			}
			step.Status = "ok"
			if attempt > 1 {
				step.Message = retrySucceededMessage(step, attempt, maxAttempts)
			}
			return true, false, emit(Event{
				Kind:     EventStepFinished,
				Round:    round,
				Target:   targetSnapshot,
				Step:     step,
				Status:   "ok",
				Message:  step.Message,
				Duration: time.Since(started).Milliseconds(),
			})
		}
		reason := checkFailureMessage(failedStep, findings)
		if attempt < maxAttempts {
			if err := emitRetrying(round, target, step, attempt, maxAttempts, reason, emit); err != nil {
				return false, false, err
			}
			continue
		}
		failedStep.Status = "failed"
		failedStep.Message = firstNonEmpty(failedStep.Message, failedStep.Error, reason)
		if err := emit(Event{
			Kind:     EventStepFinished,
			Round:    round,
			Target:   targetSnapshot,
			Step:     failedStep,
			Status:   "failed",
			Message:  failedStep.Message,
			Duration: time.Since(started).Milliseconds(),
		}); err != nil {
			return false, false, err
		}
		for _, item := range findings {
			if err := emit(Event{
				Kind:    EventFinding,
				Round:   round,
				Target:  targetSnapshot,
				Step:    failedStep,
				Finding: &item,
				Status:  "failed",
				Message: item.Message,
			}); err != nil {
				return false, false, err
			}
		}
		return false, false, nil
	}
	return false, false, nil
}

func runGatewayPingAttempt(ctx context.Context, opRunner OperationRunner, agent control.AgentInfo, round uint64, target Target, check Check, step StepSnapshot, attempt int, maxAttempts int, skip *SkipController, emit func(Event) error) (runner.Result, error, error, bool) {
	targetSnapshot := snapshotTarget(target)
	attemptStep := step
	attemptStep.Status = "running"
	if attempt > 1 {
		attemptStep.Message = retryAttemptMessage(step, attempt, maxAttempts)
	}
	if err := emit(Event{Kind: EventStepStarted, Round: round, Target: targetSnapshot, Step: attemptStep, Status: "running", Message: attemptStep.Message}); err != nil {
		return runner.Result{}, nil, err, false
	}
	opCtx, finish := skip.operationContext(ctx)
	defer finish()

	ipExec, ipErr := opRunner.Run(opCtx, agent, command.IPStatusOperation())
	if operationSkipped(opCtx) {
		return ipExec, ipErr, nil, true
	}
	if ipErr != nil && ctx.Err() != nil {
		return ipExec, ipErr, ctx.Err(), false
	}
	if failedStep, failed := operationFailureStep(step, ipExec, ipErr); failed {
		failedStep.Message = firstNonEmpty(failedStep.Message, failedStep.Error, "ip status failed")
		return ipExec, ipErr, nil, false
	}

	host, ok, reason := gatewayPingHostFromStatus(ipExec.Result.GetIpStatus(), check.Family)
	if !ok {
		return gatewayPingFailureResult(reason), nil, nil, false
	}
	pingOp, err := command.PingOperation(command.PingOptions{
		Host:    host,
		Count:   number(check.Count),
		Size:    number(check.SizeBytes),
		Timeout: durationMillis(check.Timeout),
	})
	if err != nil {
		return runner.Result{}, nil, err, false
	}
	pingExec, pingErr := opRunner.Run(opCtx, agent, pingOp)
	if operationSkipped(opCtx) {
		return pingExec, pingErr, nil, true
	}
	if pingErr != nil && ctx.Err() != nil {
		return pingExec, pingErr, ctx.Err(), false
	}
	return pingExec, pingErr, nil, false
}

func gatewayPingFailureResult(message string) runner.Result {
	return runner.Result{Result: &controlpb.CommandResult{
		Status:  controlpb.CommandResult_STATUS_FAILED,
		Message: firstNonEmpty(message, "gateway not found"),
	}}
}

func gatewayPingHostFromStatus(status *controlpb.IpStatus, familyValue string) (string, bool, string) {
	if status == nil {
		return "", false, "ip status missing gateway routes"
	}
	family, err := gatewayPingFamily(familyValue)
	if err != nil {
		return "", false, err.Error()
	}
	route, ok := defaultGatewayRoute(status.GetRoutes(), family)
	if !ok {
		return "", false, fmt.Sprintf("default gateway not found family=%s", family)
	}
	host := route.gateway.String()
	if route.gateway.Is6() && route.gateway.IsLinkLocalUnicast() && route.iface != "" {
		host += "%" + route.iface
	}
	return host, true, ""
}

func gatewayPingFamily(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "ipv4", "4":
		return "ipv4", nil
	case "ipv6", "6":
		return "ipv6", nil
	case "any", "all":
		return "any", nil
	default:
		return "", fmt.Errorf("unsupported gateway_ping family %q", value)
	}
}

type gatewayRoute struct {
	gateway netip.Addr
	iface   string
	family  string
}

func defaultGatewayRoute(routes []string, family string) (gatewayRoute, bool) {
	if family == "any" {
		if route, ok := defaultGatewayRoute(routes, "ipv4"); ok {
			return route, true
		}
		return defaultGatewayRoute(routes, "ipv6")
	}
	for _, raw := range routes {
		route, ok := parseGatewayRoute(raw)
		if !ok || route.family != family {
			continue
		}
		return route, true
	}
	return gatewayRoute{}, false
}

func parseGatewayRoute(raw string) (gatewayRoute, bool) {
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return gatewayRoute{}, false
	}
	family, defaultRoute := routeDestinationFamily(fields[0])
	if !defaultRoute {
		return gatewayRoute{}, false
	}
	var gateway netip.Addr
	var hasGateway bool
	gatewayIndex := -1
	iface := ""
	for i, field := range fields {
		switch strings.ToLower(strings.Trim(field, ",")) {
		case "->", "via":
			if i+1 < len(fields) {
				if addr, err := netip.ParseAddr(routeAddressToken(fields[i+1])); err == nil {
					gateway = addr
					hasGateway = true
					gatewayIndex = i + 1
				}
			}
		case "dev":
			if i+1 < len(fields) {
				iface = strings.Trim(fields[i+1], ",")
			}
		}
	}
	if !hasGateway {
		return gatewayRoute{}, false
	}
	if family == "" {
		family = gatewayFamily(gateway)
	}
	if iface == "" {
		iface = routeInterfaceAfterGateway(fields, gatewayIndex)
	}
	if family == "" {
		return gatewayRoute{}, false
	}
	return gatewayRoute{gateway: gateway, iface: iface, family: family}, true
}

func routeDestinationFamily(value string) (family string, defaultRoute bool) {
	value = routeAddressToken(value)
	if strings.EqualFold(value, "default") {
		return "", true
	}
	prefix, err := netip.ParsePrefix(value)
	if err != nil || !prefix.IsValid() {
		return "", false
	}
	if prefix.Bits() != 0 {
		return "", false
	}
	addr := prefix.Addr()
	switch {
	case addr.Is4():
		return "ipv4", true
	case addr.Is6():
		return "ipv6", true
	default:
		return "", false
	}
}

func gatewayFamily(addr netip.Addr) string {
	switch {
	case addr.Is4():
		return "ipv4"
	case addr.Is6():
		return "ipv6"
	default:
		return ""
	}
}

func routeInterfaceAfterGateway(fields []string, gatewayIndex int) string {
	if gatewayIndex < 0 {
		return ""
	}
	for i := gatewayIndex + 1; i < len(fields); i++ {
		field := strings.Trim(fields[i], ",")
		if field == "" {
			continue
		}
		if routeMetadataField(field) {
			return ""
		}
		if routeInterfaceToken(field) {
			return field
		}
	}
	return ""
}

func routeAddressToken(value string) string {
	return strings.TrimPrefix(strings.Trim(value, ","), "/")
}

func routeMetadataField(value string) bool {
	switch strings.ToLower(value) {
	case "mtu", "src", "metric", "table", "proto", "scope":
		return true
	default:
		return false
	}
}

func routeInterfaceToken(value string) bool {
	if value == "" || value == "->" || strings.EqualFold(value, "via") || strings.EqualFold(value, "dev") {
		return false
	}
	if strings.EqualFold(value, "default") || strings.Contains(value, "/") {
		return false
	}
	if _, err := netip.ParseAddr(routeAddressToken(value)); err == nil {
		return false
	}
	return true
}
