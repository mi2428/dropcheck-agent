package harness

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"dropcheck/controller/internal/command"
	"dropcheck/controller/internal/control"
	"dropcheck/controller/internal/controlpb"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// NewCheck exposes every live Operation without adding another probe builder or
// evaluator. SourceID is optional compiler provenance, not a user-managed UUID.
func NewCheck(name, sourceID string, op command.Operation, policy Policy, required bool, expect ...Expectation) Check {
	return operationCheck{step{name: name, sourceID: sourceID, operation: op, policy: policy, required: required, expectations: append([]Expectation(nil), expect...)}}
}

type operationCheck struct{ value step }

func (c operationCheck) Name() string         { return c.value.name }
func (c operationCheck) build() (step, error) { return c.value, nil }

// WithPolicy applies the same policy to any typed check builder.
func WithPolicy(check Check, policy Policy, required bool) Check {
	return configuredCheck{Check: check, policy: policy, required: required}
}

type configuredCheck struct {
	Check
	policy   Policy
	required bool
}

func (c configuredCheck) build() (step, error) {
	if c.Check == nil {
		return step{}, fmt.Errorf("check is nil")
	}
	s, err := c.Check.build()
	s.policy, s.required = c.policy, c.required
	return s, err
}

// Compile is pure: it resolves secrets, validates every operation/expectation,
// assigns ordinal identities, and binds agents without performing an operation.
func Compile(plan Plan, agents []control.AgentInfo) (*CompiledPlan, error) {
	return compilePlan(plan, agents, true)
}

// Validate checks all content before transport startup without fabricating an
// agent or resolving a live selector. Compile performs strict binding later.
func Validate(plan Plan) error {
	_, err := compilePlan(plan, nil, false)
	return err
}

func compilePlan(plan Plan, agents []control.AgentInfo, bind bool) (*CompiledPlan, error) {
	p := &CompiledPlan{name: plan.Name, roundInterval: plan.RoundInterval, agents: append([]control.AgentInfo(nil), agents...)}
	if plan.RoundInterval < 0 {
		return nil, fmt.Errorf("negative round interval")
	}
	if len(plan.Networks) == 0 {
		return nil, fmt.Errorf("plan must set networks")
	}
	for i, network := range plan.Networks {
		// Owned connections still require an SSID; optional BSSID/Network ID
		// strengthen selection-time identity without promising roam continuity.
		if strings.TrimSpace(network.ssid) == "" {
			return nil, fmt.Errorf("target %d: SSID is required to pin network-bound probes", i)
		}
		if !network.readOnly && network.networkID != "" {
			return nil, fmt.Errorf("target %d: an owned connection cannot preselect its Android Network ID", i)
		}
		if network.readOnly && (network.psk.value != "" || network.psk.env != "" || network.band != "" || network.security != "" || network.macRandomization != "" || network.rotation != "" || network.waitConnected || network.disconnectAfter || network.forgetAfter || network.connectTimeout != 0 || network.waitTimeout != 0 || network.requireIP || network.requireValidated || network.connectPolicy != (Policy{}) || network.waitPolicy != (Policy{})) {
			return nil, fmt.Errorf("target %d: read-only target cannot request connection or cleanup", i)
		}
		if network.connectTimeout < 0 || network.waitTimeout < 0 {
			return nil, fmt.Errorf("target %d: negative timeout", i)
		}
		passphrase := network.psk.value
		if network.psk.env != "" {
			var err error
			passphrase, err = network.psk.resolve()
			if err != nil {
				return nil, fmt.Errorf("target %d: %w", i, err)
			}
		}
		if passphrase != "" {
			p.secrets = append(p.secrets, passphrase)
		}
		network.psk = SecretValue(passphrase)
		rotation := strings.ReplaceAll(network.rotation, "-", "_")
		switch rotation {
		case "", "none":
			rotation = "none"
		case "per_target", "per_round":
			if network.macRandomization == "" {
				network.macRandomization = "non-persistent"
			}
			if network.macRandomization != "non-persistent" {
				return nil, fmt.Errorf("target %d: MAC rotation requires non-persistent randomization", i)
			}
			network.disconnectAfter = true
			if rotation == "per_target" {
				network.forgetAfter = true
			}
		default:
			return nil, fmt.Errorf("target %d: unknown MAC rotation", i)
		}
		network.rotation = rotation
		id := fmt.Sprintf("target/%d", i)
		target := compiledTarget{network: network, preview: Target{ID: id, Name: network.displayName(), ShortName: network.shortName, Agent: network.agent, SSID: network.ssid, BSSID: network.bssid, NetworkID: network.networkID, Band: network.band, DisconnectAfter: new(network.disconnectAfter), ForgetAfter: new(network.forgetAfter), SecretPresent: passphrase != ""}}
		var err error
		if !network.readOnly {
			connect, buildErr := network.connectOperation()
			if buildErr != nil {
				return nil, fmt.Errorf("target %d: %s", i, p.redact(buildErr.Error()))
			}
			target.connect, err = compileStep(step{name: "connect", operation: connect, policy: network.connectPolicy, required: true}, "connect", "connect")
			if err != nil {
				return nil, fmt.Errorf("target %d connect: %s", i, p.redact(err.Error()))
			}
		}
		if network.waitConnected {
			wait, buildErr := network.waitOperation()
			if buildErr != nil {
				return nil, fmt.Errorf("target %d wait: %s", i, p.redact(buildErr.Error()))
			}
			target.wait, err = compileStep(step{name: "wait_connected", operation: wait, policy: network.waitPolicy, required: true}, "wait_connected", "wait_connected")
			if err != nil {
				return nil, fmt.Errorf("target %d wait: %s", i, p.redact(err.Error()))
			}
		}
		checks := append(append([]Check(nil), plan.Checks...), network.checks...)
		for j, check := range checks {
			if nilValue(check) {
				return nil, fmt.Errorf("target %d check %d: nil check", i, j)
			}
			s, buildErr := check.build()
			if buildErr != nil {
				return nil, fmt.Errorf("target %d check %d: %s", i, j, p.redact(buildErr.Error()))
			}
			checkID := s.sourceID
			if checkID == "" {
				if j < len(plan.Checks) {
					checkID = fmt.Sprintf("check/%d", j)
				} else {
					checkID = fmt.Sprintf("%s/check/%d", id, j-len(plan.Checks))
				}
			}
			for _, existing := range target.checks {
				if existing.id == checkID {
					return nil, fmt.Errorf("target %d: duplicate check source identity", i)
				}
			}
			typeName := strings.ReplaceAll(strings.ReplaceAll(s.operation.Name, ".", "_"), "-", "_")
			if s.gateway != nil {
				typeName = "gateway_ping"
			}
			compiled, compileErr := compileStep(s, checkID, typeName)
			if compileErr != nil {
				return nil, fmt.Errorf("target %d check %d: %s", i, j, p.redact(compileErr.Error()))
			}
			if network.readOnly && !network.activeProbes && (compiled.gateway != nil || compiled.operation.Command.GetGetWifiStatus() == nil && compiled.operation.Command.GetGetIpStatus() == nil) {
				return nil, fmt.Errorf("target %d check %d: read-only target supports Wi-Fi/IP status observations only", i, j)
			}
			if network.activeProbes {
				if compiled.gateway != nil || !activeObservationCommand(compiled.operation.Command) {
					return nil, fmt.Errorf("target %d check %d: active observation permits probes/status only; no Wi-Fi mutation or wait", i, j)
				}
			}
			if compiled.gateway == nil {
				compiled.operation, err = bindSelector(compiled.operation, network)
				if err != nil {
					return nil, fmt.Errorf("target %d check %d: %w", i, j, err)
				}
			}
			target.checks = append(target.checks, compiled)
			target.preview.Checks = append(target.preview.Checks, stepInfo(compiled))
		}
		if network.readOnly && len(target.checks) == 0 {
			return nil, fmt.Errorf("target %d: read-only target must have a check", i)
		}
		if bind {
			target.agents, err = bindAgents(network.agent, agents)
			if err != nil {
				return nil, fmt.Errorf("target %d: %w", i, err)
			}
			if network.agent != "" && len(target.agents) == 1 {
				target.preview.Agent = target.agents[0].Hello.GetAdbSerial()
			}
		}
		p.targets = append(p.targets, target)
	}
	return p, nil
}

func activeObservationCommand(cmd *controlpb.RunCommand) bool {
	if cmd == nil {
		return false
	}
	switch cmd.Command.(type) {
	case *controlpb.RunCommand_GetWifiStatus, *controlpb.RunCommand_GetIpStatus,
		*controlpb.RunCommand_GetWifiDiagnostics, *controlpb.RunCommand_GetWifiScan,
		*controlpb.RunCommand_GetWifiCapabilities, *controlpb.RunCommand_GetFreshWifiScan,
		*controlpb.RunCommand_GetWifiScanDetail, *controlpb.RunCommand_Ping,
		*controlpb.RunCommand_ResolveDns, *controlpb.RunCommand_HttpCheck,
		*controlpb.RunCommand_Traceroute, *controlpb.RunCommand_PathMtu,
		*controlpb.RunCommand_GlobalIp, *controlpb.RunCommand_Wget:
		return true
	}
	return false
}

func compileStep(s step, id, typeName string) (compiledStep, error) {
	if strings.TrimSpace(s.name) == "" {
		return compiledStep{}, fmt.Errorf("check name is required")
	}
	timeout := 15 * time.Second
	if s.gateway == nil {
		if err := command.ValidateOperation(s.operation); err != nil {
			return compiledStep{}, err
		}
		cmd, options, err := command.BuildRunCommand(s.operation)
		if err != nil {
			return compiledStep{}, err
		}
		options.TracerouteRequiredHops = append([]string(nil), options.TracerouteRequiredHops...)
		s.operation = command.NewOperation(s.operation.Name, cmd, options)
		timeout = command.TimeoutFor(cmd)
		if options.WifiEHTFreshScan {
			fresh, err := command.WifiFreshScanOperation("all", fmt.Sprint(options.WifiEHTFreshScanTimeoutMs))
			if err != nil {
				return compiledStep{}, err
			}
			timeout += command.TimeoutFor(fresh.Command)
		}
	} else {
		var err error
		timeout, err = s.gateway.operationTimeout()
		if err != nil {
			return compiledStep{}, err
		}
	}
	for _, expectation := range s.expectations {
		if nilValue(expectation) {
			return compiledStep{}, fmt.Errorf("expectation is nil")
		}
		if validator, ok := expectation.(interface{ Validate() error }); ok {
			if err := validator.Validate(); err != nil {
				return compiledStep{}, err
			}
		}
	}
	var err error
	s.policy, err = normalizePolicy(s.policy, timeout)
	if err != nil {
		return compiledStep{}, err
	}
	return compiledStep{id: id, typeName: typeName, operationTimeout: timeout, step: s}, nil
}

func nilValue(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Pointer, reflect.Func, reflect.Interface:
		return v.IsNil()
	}
	return false
}

func bindAgents(selector string, agents []control.AgentInfo) ([]control.AgentInfo, error) {
	if len(agents) == 0 {
		return nil, fmt.Errorf("no selected agents")
	}
	if selector == "" {
		return append([]control.AgentInfo(nil), agents...), nil
	}
	var exact, prefix []control.AgentInfo
	for _, agent := range agents {
		serial := agent.Hello.GetAdbSerial()
		if serial == selector {
			exact = append(exact, agent)
		} else if serial != "" && strings.HasPrefix(serial, selector) {
			prefix = append(prefix, agent)
		}
	}
	if len(exact) == 1 {
		return exact, nil
	}
	if len(exact) > 1 || len(prefix) > 1 {
		return nil, fmt.Errorf("agent selector is ambiguous")
	}
	if len(prefix) == 1 {
		return prefix, nil
	}
	return nil, fmt.Errorf("selected agent is not connected")
}

func bindSelector(op command.Operation, network Network) (command.Operation, error) {
	cmd, options, err := command.BuildRunCommand(op)
	if err != nil {
		return command.Operation{}, err
	}
	var selector **controlpb.NetworkSelector
	switch c := cmd.Command.(type) {
	case *controlpb.RunCommand_GetIpStatus:
		selector = &c.GetIpStatus.Selector
	case *controlpb.RunCommand_Ping:
		selector = &c.Ping.Selector
	case *controlpb.RunCommand_Traceroute:
		selector = &c.Traceroute.Selector
	case *controlpb.RunCommand_PathMtu:
		selector = &c.PathMtu.Selector
	case *controlpb.RunCommand_GlobalIp:
		selector = &c.GlobalIp.Selector
	case *controlpb.RunCommand_ResolveDns:
		selector = &c.ResolveDns.Selector
	case *controlpb.RunCommand_HttpCheck:
		selector = &c.HttpCheck.Selector
	case *controlpb.RunCommand_Wget:
		selector = &c.Wget.Selector
	}
	if selector == nil && (network.bssid != "" || network.networkID != "") {
		return command.Operation{}, fmt.Errorf("check cannot apply an explicit BSSID or Network ID")
	}
	if selector != nil {
		previous := *selector
		if previous != nil && previous.Ssid != "" && network.ssid != "" && previous.Ssid != network.ssid {
			return command.Operation{}, fmt.Errorf("probe selector differs from target")
		}
		if previous.GetBssid() != "" && network.bssid != "" && !strings.EqualFold(previous.GetBssid(), network.bssid) || previous.GetNetworkId() != "" && network.networkID != "" && previous.GetNetworkId() != network.networkID {
			return command.Operation{}, fmt.Errorf("probe selector differs from target")
		}
		bound := &controlpb.NetworkSelector{Ssid: previous.GetSsid(), Bssid: previous.GetBssid(), NetworkId: previous.GetNetworkId()}
		if network.ssid != "" {
			bound.Ssid = network.ssid
		}
		if network.bssid != "" {
			bound.Bssid = network.bssid
		}
		if network.networkID != "" {
			bound.NetworkId = network.networkID
		}
		*selector = bound
	}
	if err := command.ValidateOperation(command.NewOperation(op.Name, cmd, options)); err != nil {
		return command.Operation{}, err
	}
	return command.NewOperation(op.Name, cmd, options), nil
}

func stepInfo(s compiledStep) CheckInfo {
	info := CheckInfo{ID: s.id, Name: s.name, Type: s.typeName, Operation: s.operation.Name, Required: s.required, Policy: s.policy, OperationTimeout: s.operationTimeout, Operations: []string{s.operation.Name}}
	if s.gateway != nil {
		info.Operation, info.Operations = "gateway.ping", []string{"ip.status", "ping"}
		info.Destination = "selected Network's observed default gateway (" + firstNonEmpty(s.gateway.Family, "ipv4") + ")"
		packets := s.gateway.Count
		if packets == 0 {
			packets = 3 // PingOperation's default.
		}
		info.Traffic = fmt.Sprintf("up to %d ICMP packets per attempt, after an IP lookup", packets)
	}
	if s.operation.Command != nil {
		if s.operation.Options.WifiEHTFreshScan {
			info.Operations = []string{"wifi.scan.fresh", s.operation.Name}
			info.Traffic = "one active Wi-Fi scan, then on-device diagnostics per attempt"
		}
		switch cmd := s.operation.Command.Command.(type) {
		case *controlpb.RunCommand_Ping:
			info.Destination, info.Traffic = cmd.Ping.Host, fmt.Sprintf("%d ICMP packets per attempt", cmd.Ping.Count)
			info.Traffic += "; family=" + strings.ToLower(strings.TrimPrefix(cmd.Ping.Family.String(), "IP_FAMILY_"))
		case *controlpb.RunCommand_Traceroute:
			info.Destination, info.Traffic = cmd.Traceroute.Host, fmt.Sprintf("up to %d hops per attempt", cmd.Traceroute.MaxHops)
		case *controlpb.RunCommand_PathMtu:
			info.Destination, info.Traffic = cmd.PathMtu.Host, "multiple MTU probes per attempt"
		case *controlpb.RunCommand_ResolveDns:
			info.Destination = cmd.ResolveDns.Name
			info.Traffic = fmt.Sprintf("%d DNS qtype request(s) per attempt via selected Network configured resolver: %v", len(cmd.ResolveDns.Qtypes), cmd.ResolveDns.Qtypes)
		case *controlpb.RunCommand_HttpCheck:
			info.Destination, info.Traffic = cmd.HttpCheck.Url, fmt.Sprintf("HTTP request per attempt; expected status=%d; redirects disabled", cmd.HttpCheck.ExpectedStatus)
		case *controlpb.RunCommand_Wget:
			info.Destination, info.Traffic = cmd.Wget.Url, "HTTP download per attempt; response bytes not bounded by Plan"
		case *controlpb.RunCommand_CycleWifi:
			cycles := max(uint32(1), cmd.CycleWifi.GetCount())
			cleanup := "disconnect"
			if cmd.CycleWifi.ForgetAfterEach {
				cleanup = "forget"
			}
			info.Traffic = fmt.Sprintf("%d connect/wait/%s cycles per attempt", cycles, cleanup)
			if cmd.CycleWifi.PingHost != "" {
				info.Destination = cmd.CycleWifi.PingHost
				info.Traffic += "; up to " + fmt.Sprint(cycles*3) + " ICMP packets"
			}
			if cmd.CycleWifi.HttpUrl != "" {
				info.Destination = strings.TrimSpace(info.Destination + " " + cmd.CycleWifi.HttpUrl)
				info.Traffic += fmt.Sprintf("; up to %d HTTP requests", cycles)
			}
		case *controlpb.RunCommand_GlobalIp:
			info.Destination, info.Traffic = "http://ifconfig.me/ip", "HTTP request per selected address family per attempt"
		}
	}
	for _, expectation := range s.expectations {
		if described, ok := expectation.(interface{ Description() string }); ok {
			info.Expectations = append(info.Expectations, described.Description())
		} else {
			info.Expectations = append(info.Expectations, reflect.TypeOf(expectation).String()+" (Go-only predicate)")
		}
	}
	return info
}

func (p *CompiledPlan) Preview() Preview {
	view := Preview{Name: p.redact(p.name), RoundInterval: p.roundInterval}
	seen := map[string]bool{}
	for _, target := range p.targets {
		t := target.preview
		if target.connect.id != "" {
			connect := stepInfo(target.connect)
			t.Connect = &connect
		}
		if target.wait.id != "" {
			wait := stepInfo(target.wait)
			t.Wait = &wait
		}
		t.BoundAgentIDs = nil
		for _, agent := range target.agents {
			t.BoundAgentIDs = append(t.BoundAgentIDs, agentKey(agent))
		}
		if target.network.band != "" && target.network.band != "all" {
			t.CapabilityPreflight = []CapabilityPreflight{{Operation: "wifi.capabilities", Band: target.network.band, State: "pending", Reason: "support observed only after explicit Start; unknown is not unsupported"}}
		}
		t.Checks = append([]CheckInfo(nil), t.Checks...)
		t.Name, t.ShortName, t.SSID, t.BSSID, t.NetworkID, t.Band = p.redact(t.Name), p.redact(t.ShortName), p.redact(t.SSID), p.redact(t.BSSID), p.redact(t.NetworkID), p.redact(t.Band)
		for i := range t.Checks {
			t.Checks[i].Name = p.redact(t.Checks[i].Name)
			t.Checks[i].Operation = p.redact(t.Checks[i].Operation)
			t.Checks[i].Operations = append([]string(nil), t.Checks[i].Operations...)
			for j := range t.Checks[i].Operations {
				t.Checks[i].Operations[j] = p.redact(t.Checks[i].Operations[j])
			}
			t.Checks[i].Destination, t.Checks[i].Traffic = p.redact(t.Checks[i].Destination), p.redact(t.Checks[i].Traffic)
			t.Checks[i].Expectations = append([]string(nil), t.Checks[i].Expectations...)
			for j := range t.Checks[i].Expectations {
				t.Checks[i].Expectations[j] = p.redact(t.Checks[i].Expectations[j])
			}
		}
		view.Targets = append(view.Targets, t)
		for _, c := range t.Checks {
			if !seen[c.ID] {
				seen[c.ID] = true
				view.Checks = append(view.Checks, c)
			}
		}
	}
	for _, agent := range p.agents {
		view.Agents = append(view.Agents, AgentSnapshotFromInfo(agent))
	}
	return view
}

func (p *CompiledPlan) Select(selection Selection) (*CompiledPlan, error) {
	if len(selection.Scopes) > 0 {
		if len(selection.AgentIDs)+len(selection.TargetIDs)+len(selection.CheckIDs) != 0 {
			return nil, fmt.Errorf("exact scopes cannot be mixed with flat selection")
		}
		return p.selectScopes(selection.Scopes)
	}
	if len(selection.AgentIDs) == 0 {
		return nil, fmt.Errorf("select at least one agent")
	}
	if len(selection.TargetIDs) == 0 {
		return nil, fmt.Errorf("select at least one target")
	}
	result := &CompiledPlan{name: p.name, roundInterval: p.roundInterval, secrets: append([]string(nil), p.secrets...)}
	knownAgents := map[string]bool{}
	for _, agent := range p.agents {
		id := agentKey(agent)
		knownAgents[id] = true
		if slices.Contains(selection.AgentIDs, id) {
			result.agents = append(result.agents, agent)
		}
	}
	for _, id := range selection.AgentIDs {
		if !knownAgents[id] {
			return nil, fmt.Errorf("unknown agent identity")
		}
	}
	knownTargets, knownChecks := map[string]bool{}, map[string]bool{}
	matchedCheck := false
	for _, t := range p.targets {
		knownTargets[t.preview.ID] = true
		for _, c := range t.checks {
			knownChecks[c.id] = true
		}
		if !slices.Contains(selection.TargetIDs, t.preview.ID) {
			continue
		}
		bindings := []control.AgentInfo{}
		for _, agent := range t.agents {
			if slices.Contains(selection.AgentIDs, agentKey(agent)) {
				bindings = append(bindings, agent)
			}
		}
		if len(bindings) == 0 {
			return nil, fmt.Errorf("selected target lost its explicitly bound agent; select a valid target and agent")
		}
		t.agents = bindings
		if len(selection.CheckIDs) > 0 {
			t = selectedChecks(t, selection.CheckIDs, false)
			matchedCheck = matchedCheck || len(t.checks) > 0
		}
		result.targets = append(result.targets, t)
	}
	for _, id := range selection.TargetIDs {
		if !knownTargets[id] {
			return nil, fmt.Errorf("unknown target identity")
		}
	}
	for _, id := range selection.CheckIDs {
		if !knownChecks[id] {
			return nil, fmt.Errorf("unknown check identity")
		}
	}
	if len(selection.CheckIDs) > 0 && !matchedCheck {
		return nil, fmt.Errorf("selected checks do not belong to selected targets")
	}
	return result, nil
}

func selectedChecks(target compiledTarget, ids []string, all bool) compiledTarget {
	if all {
		return target
	}
	last := -1
	for i, check := range target.checks {
		if slices.Contains(ids, check.id) {
			last = i
		}
	}
	checks := []compiledStep{}
	for i, check := range target.checks {
		if slices.Contains(ids, check.id) || i < last && check.required {
			checks = append(checks, check)
		}
	}
	target.checks, target.preview.Checks = checks, nil
	for _, check := range checks {
		target.preview.Checks = append(target.preview.Checks, stepInfo(check))
	}
	return target
}

func (p *CompiledPlan) selectScopes(scopes []Scope) (*CompiledPlan, error) {
	type pair struct{ agent, target string }
	type requested struct {
		checks []string
		all    bool
	}
	groups := map[pair]requested{}
	for _, scope := range scopes {
		if scope.AgentID == "" || scope.TargetID == "" || scope.Kind != ScopeCheck && scope.Kind != ScopeTarget || scope.Kind == ScopeCheck && scope.CheckID == "" || scope.Kind == ScopeTarget && scope.CheckID != "" {
			return nil, fmt.Errorf("invalid exact selection scope")
		}
		found := false
		for _, target := range p.targets {
			if target.preview.ID != scope.TargetID {
				continue
			}
			bound := false
			for _, agent := range target.agents {
				if agentKey(agent) == scope.AgentID {
					bound = true
					break
				}
			}
			if !bound {
				continue
			}
			if scope.Kind == ScopeTarget {
				found = true
			} else {
				for _, check := range target.checks {
					if check.id == scope.CheckID {
						found = true
						break
					}
				}
			}
		}
		if !found {
			return nil, fmt.Errorf("unknown or unbound exact selection scope")
		}
		key := pair{scope.AgentID, scope.TargetID}
		request := groups[key]
		if scope.Kind == ScopeTarget {
			request.all = true
		} else if !slices.Contains(request.checks, scope.CheckID) {
			request.checks = append(request.checks, scope.CheckID)
		}
		groups[key] = request
	}
	result := &CompiledPlan{name: p.name, roundInterval: p.roundInterval, secrets: append([]string(nil), p.secrets...)}
	active := map[string]bool{}
	for _, target := range p.targets {
		for _, agent := range target.agents {
			request, ok := groups[pair{agentKey(agent), target.preview.ID}]
			if !ok {
				continue
			}
			copy := selectedChecks(target, request.checks, request.all)
			copy.agents = []control.AgentInfo{agent}
			result.targets = append(result.targets, copy)
			active[agentKey(agent)] = true
		}
	}
	for _, agent := range p.agents {
		if active[agentKey(agent)] {
			result.agents = append(result.agents, agent)
		}
	}
	if len(result.targets) == 0 || len(result.agents) == 0 {
		return nil, fmt.Errorf("select at least one exact scope")
	}
	return result, nil
}

func (p *CompiledPlan) redact(text string) string {
	for _, secret := range p.secrets {
		if secret != "" {
			text = strings.ReplaceAll(text, secret, "<redacted>")
		}
	}
	return text
}

func cloneRaw(result *controlpb.CommandResult) *controlpb.CommandResult {
	if result == nil {
		return nil
	}
	return proto.Clone(result).(*controlpb.CommandResult)
}

// sanitizeRaw operates on an owned clone: known credentials are removed from
// all free-text protobuf fields before reports, JSONL or renderers receive it.
func (p *CompiledPlan) sanitizeRaw(raw *controlpb.CommandResult) *controlpb.CommandResult {
	result := cloneRaw(raw)
	if result == nil {
		return nil
	}
	var walk func(protoreflect.Message)
	walk = func(message protoreflect.Message) {
		message.Range(func(field protoreflect.FieldDescriptor, value protoreflect.Value) bool {
			if field.IsList() {
				list := value.List()
				for i := 0; i < list.Len(); i++ {
					switch field.Kind() {
					case protoreflect.StringKind:
						list.Set(i, protoreflect.ValueOfString(p.redact(list.Get(i).String())))
					case protoreflect.MessageKind:
						walk(list.Get(i).Message())
					}
				}
			} else {
				switch field.Kind() {
				case protoreflect.StringKind:
					message.Set(field, protoreflect.ValueOfString(p.redact(value.String())))
				case protoreflect.MessageKind:
					walk(value.Message())
				}
			}
			return true
		})
	}
	walk(result.ProtoReflect())
	return result
}

func (p *CompiledPlan) sanitizeResult(result OperationResult) OperationResult {
	result.Raw = p.sanitizeRaw(result.Raw)
	result.Parts = append([]OperationRecord(nil), result.Parts...)
	for i := range result.Parts {
		result.Parts[i].Raw = p.sanitizeRaw(result.Parts[i].Raw)
		result.Parts[i].Name = p.redact(result.Parts[i].Name)
		result.Parts[i].Error = p.redact(result.Parts[i].Error)
	}
	result.Findings = snapshotFindings(result.Findings)
	for i := range result.Findings {
		f := &result.Findings[i]
		f.Target, f.Check, f.Metric = p.redact(f.Target), p.redact(f.Check), p.redact(f.Metric)
		f.Observed, f.Expected, f.Message = p.redact(f.Observed), p.redact(f.Expected), p.redact(f.Message)
		f.ObservedValue, f.ExpectedValue = p.safeValue(f.ObservedValue), p.safeValue(f.ExpectedValue)
	}
	return result
}

func (p *CompiledPlan) safeValue(value any) any {
	switch v := value.(type) {
	case string:
		return p.redact(v)
	case []string:
		copy := append([]string(nil), v...)
		for i := range copy {
			copy[i] = p.redact(copy[i])
		}
		return copy
	}
	return value
}
