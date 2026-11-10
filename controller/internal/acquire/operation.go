package acquire

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/mi2428/dropcheck-agent/controller/internal/command"
	"github.com/mi2428/dropcheck-agent/controller/internal/control"
	"github.com/mi2428/dropcheck-agent/controller/internal/controlpb"
	"google.golang.org/protobuf/proto"
)

// Execute acquires one validated atomic/compound operation on a pinned agent.
// It applies no retry, expectation policy, round, presentation or cleanup logic.
// Returned Raw/Parts are independently owned clones, including on partial failure.
// Transport/context errors retain their causes; agent failures remain in Raw.
func Execute(ctx context.Context, r Runner, agent control.AgentInfo, op command.Operation) (Result, error) {
	if r == nil {
		return Result{}, fmt.Errorf("operation runner is nil")
	}
	if err := command.ValidateOperation(op); err != nil {
		return Result{}, err
	}
	if f := op.Command.GetForgetWifi(); f != nil && f.Selector != nil && !slices.Contains(agent.Hello.GetCapabilities(), "wifi.forget.typed") {
		return Result{}, fmt.Errorf("selected agent does not advertise typed Wi-Fi forgetting")
	}
	cmd, options, err := command.BuildRunCommand(op)
	if err != nil {
		return Result{}, err
	}
	options.TracerouteRequiredHops = slices.Clone(options.TracerouteRequiredHops)
	result := Result{Options: options}
	run := func(operation command.Operation) (*controlpb.CommandResult, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		wire, _, buildErr := command.BuildRunCommand(operation)
		if buildErr != nil {
			return nil, buildErr
		}
		started := time.Now()
		runCtx, cancel := context.WithTimeout(ctx, command.TimeoutFor(wire))
		exec, runErr := r.Run(runCtx, agent, operation)
		cancel()
		record := Part{AgentID: agent.ID, SessionID: agent.SessionID, Connected: agent.Connected, Name: operation.Name, CommandID: exec.CommandID, Started: started, Ended: time.Now(), Raw: cloneRaw(exec.Result)}
		if runErr != nil {
			record.Error = runErr.Error()
		}
		result.Parts = append(result.Parts, record)
		return cloneRaw(exec.Result), runErr
	}
	var fresh *controlpb.CommandResult
	if options.WifiEHTFreshScan {
		band := options.WifiEHTFreshScanBand
		if band == "" {
			band = "all"
		}
		freshOp, buildErr := command.WifiFreshScanOperation(band, fmt.Sprint(options.WifiEHTFreshScanTimeoutMs))
		if buildErr != nil {
			return result, buildErr
		}
		fresh, err = run(freshOp)
		if err != nil {
			return result, err
		}
		if fresh == nil {
			return result, fmt.Errorf("fresh scan returned no result")
		}
		if fresh.Status == controlpb.CommandResult_STATUS_OK && !PayloadMatches(freshOp, fresh) {
			fresh.Status = controlpb.CommandResult_STATUS_FAILED
			fresh.Message = "fresh scan evidence is missing or inconsistent; retained data is reference"
		}
		if scan := fresh.GetWifiScan(); scan != nil {
			source := "fresh"
			if fresh.Status != controlpb.CommandResult_STATUS_OK {
				source = "cached (refresh failed)"
			}
			scan.Fields = append(scan.Fields, &controlpb.DiagnosticField{Key: "scan_source", Value: source})
		}
		if fresh.Status == controlpb.CommandResult_STATUS_CANCELED || fresh.GetWifiScan() == nil {
			result.Raw = fresh
			return result, nil
		}
	}
	// Local options stay on the result; the atomic runner does not re-acquire EHT.
	atomic := command.NewOperation(op.Name, cmd, command.Options{})
	result.Raw, err = run(atomic)
	if err != nil {
		return result, err
	}
	if result.Raw != nil && result.Raw.GetStatus() == controlpb.CommandResult_STATUS_OK && !PayloadMatches(atomic, result.Raw) {
		result.Findings = append(result.Findings, Finding{Metric: "payload", Observed: "<invalid>", Expected: "consistent typed payload", Missing: true, Message: "wrong, missing or inconsistent payload"})
	}
	if options.WifiEHTFreshScan {
		if result.Raw != nil && result.Raw.GetWifiDiagnostics() != nil && fresh.GetWifiScan() != nil {
			result.Raw.GetWifiDiagnostics().Scan = proto.Clone(fresh.GetWifiScan()).(*controlpb.WifiScan)
			result.Raw.ElapsedMs += fresh.GetElapsedMs()
		}
		if fresh == nil || fresh.GetWifiScan() == nil || fresh.GetStatus() != controlpb.CommandResult_STATUS_OK {
			result.Findings = append(result.Findings, Finding{Metric: "fresh_scan", Observed: "<missing>", Expected: "fresh acquisition succeeded", Missing: true, Message: "requested fresh acquisition did not succeed"})
			if fresh != nil && fresh.GetStatus() != controlpb.CommandResult_STATUS_OK {
				result.Findings[len(result.Findings)-1] = Finding{Metric: "fresh_scan", Observed: fresh.GetStatus().String(), Expected: "STATUS_OK", Message: fresh.GetMessage()}
			}
			if result.Raw != nil {
				diagnosticsStatus := result.Raw.Status
				if result.Raw.Status == controlpb.CommandResult_STATUS_OK {
					result.Raw.Status = fresh.Status
				}
				result.Raw.Message = "fresh scan: " + fresh.Status.String() + ": " + fresh.Message + "; diagnostics: " + diagnosticsStatus.String() + ": " + result.Raw.Message
			}
		}
	}
	if len(options.TracerouteRequiredHops) > 0 {
		trace := result.Raw.GetTraceroute()
		for _, required := range options.TracerouteRequiredHops {
			if trace == nil || trace.GetObservationError() != "" || trace.ReachedTarget == nil {
				result.Findings = append(result.Findings, Finding{Metric: "trace.via", Observed: "<missing>", Expected: required, Missing: true, Message: "typed hop observations unavailable"})
				continue
			}
			found := false
			for _, hop := range trace.Hops {
				if slices.Contains(hop.Addresses, required) || slices.Contains(hop.Hostnames, required) {
					found = true
					break
				}
			}
			if found {
				result.Findings = append(result.Findings, Finding{Metric: "trace.via", Observed: required, Expected: "required hop present", Passed: true})
			} else {
				result.Findings = append(result.Findings, Finding{Metric: "trace.via", Observed: "not observed", Expected: required, Message: "required hop not present"})
			}
		}
	}
	return result, nil
}

// PayloadMatches checks payload types and, for successful results, NetworkSnapshot
// identity, Wi-Fi diagnostic structure and fresh-scan metadata. Failed results
// need only the matching payload type. It does not evaluate tui expectations.
func PayloadMatches(op command.Operation, raw *controlpb.CommandResult) bool {
	if op.Command == nil || raw == nil || raw.Payload == nil {
		return false
	}
	failed := raw.Status != controlpb.CommandResult_STATUS_OK
	switch op.Command.Command.(type) {
	case *controlpb.RunCommand_GetNetworks, *controlpb.RunCommand_GetNetwork:
		return raw.GetNetworkSnapshot() != nil && (failed || validNetworkSnapshot(op.Command.GetGetNetwork(), raw.GetNetworkSnapshot()))
	case *controlpb.RunCommand_GetPermissions:
		return raw.GetPermissionSnapshot() != nil
	case *controlpb.RunCommand_GetWifiStatus:
		return raw.GetWifiStatus() != nil
	case *controlpb.RunCommand_GetWifiDiagnostics:
		return raw.GetWifiDiagnostics() != nil && (failed || validWifiDiagnostics(raw.GetWifiDiagnostics()))
	case *controlpb.RunCommand_GetWifiScan:
		return raw.GetWifiScan() != nil && (failed || len(raw.GetWifiScan().GetErrors()) == 0)
	case *controlpb.RunCommand_GetFreshWifiScan:
		scan := raw.GetWifiScan()
		if failed {
			return scan != nil
		}
		if scan == nil || len(scan.GetErrors()) != 0 {
			return false
		}
		for _, key := range []string{"fresh_scan_receiver_registered", "fresh_scan_start_scan", "fresh_scan_broadcast_received", "fresh_scan_results_updated", "fresh_scan_wait_completed"} {
			count := 0
			for _, field := range scan.GetFields() {
				if field.GetKey() == key {
					if field.GetValue() != "true" {
						return false
					}
					count++
				}
			}
			if count != 1 {
				return false
			}
		}
		return true
	case *controlpb.RunCommand_GetWifiScanDetail:
		return raw.GetWifiScanDetail() != nil
	case *controlpb.RunCommand_GetWifiCapabilities:
		return raw.GetWifiCapabilities() != nil && (failed || len(raw.GetWifiCapabilities().GetErrors()) == 0)
	case *controlpb.RunCommand_ConnectWifi:
		return raw.GetConnectWifi() != nil && (failed || raw.GetConnectWifi().GetConnected())
	case *controlpb.RunCommand_DisconnectWifi, *controlpb.RunCommand_ForgetWifi, *controlpb.RunCommand_ReconnectWifi:
		return raw.GetWifiOperation() != nil && (failed || raw.GetWifiOperation().GetOk() && len(raw.GetWifiOperation().GetErrors()) == 0)
	case *controlpb.RunCommand_WaitWifiConnected, *controlpb.RunCommand_AssertWifi:
		if failed {
			return raw.GetWifiAssert() != nil
		}
		if raw.GetWifiAssert() == nil || !raw.GetWifiAssert().GetPassed() || len(raw.GetWifiAssert().GetErrors()) != 0 {
			return false
		}
		for _, check := range raw.GetWifiAssert().GetChecks() {
			if !check.GetPassed() {
				return false
			}
		}
		return true
	case *controlpb.RunCommand_MonitorWifi:
		return raw.GetWifiMonitor() != nil
	case *controlpb.RunCommand_CycleWifi:
		return raw.GetWifiCycle() != nil
	case *controlpb.RunCommand_GetIpStatus:
		return raw.GetIpStatus() != nil
	case *controlpb.RunCommand_Ping:
		return raw.GetPing() != nil
	case *controlpb.RunCommand_Traceroute:
		return raw.GetTraceroute() != nil
	case *controlpb.RunCommand_PathMtu:
		return raw.GetPathMtu() != nil
	case *controlpb.RunCommand_GlobalIp:
		return raw.GetGlobalIp() != nil
	case *controlpb.RunCommand_ResolveDns:
		return raw.GetResolveDns() != nil
	case *controlpb.RunCommand_HttpCheck:
		return raw.GetHttpCheck() != nil
	case *controlpb.RunCommand_Wget:
		return raw.GetWget() != nil
	}
	return false
}

func cloneRaw(result *controlpb.CommandResult) *controlpb.CommandResult {
	if result == nil {
		return nil
	}
	return proto.Clone(result).(*controlpb.CommandResult)
}

func validWifiDiagnostics(value *controlpb.WifiDiagnostics) bool {
	if value == nil || value.Status == nil || value.Capabilities == nil || value.Scan == nil || len(value.Capabilities.Errors) != 0 || len(value.Scan.Errors) != 0 {
		return false
	}
	seen := map[string]bool{}
	for _, network := range value.Networks {
		id := network.GetNetworkId()
		if !command.ValidNetworkID(id) || seen[id] || network.GetIpStatus() == nil || network.GetIpStatus().GetNetworkId() != id {
			return false
		}
		seen[id] = true
		for _, field := range []string{"capabilities", "link_properties"} {
			if available, _ := Availability(network.GetIpStatus().GetObservationFields(), field); !available {
				return false
			}
		}
	}
	return true
}

func validNetworkSnapshot(query *controlpb.GetNetwork, snapshot *controlpb.NetworkSnapshot) bool {
	if snapshot == nil || snapshot.ObservedAtUnixMs <= 0 || len(snapshot.Errors) != 0 || query != nil && len(snapshot.Networks) != 1 {
		return false
	}
	seen := make(map[string]bool)
	active := 0
	for _, network := range snapshot.Networks {
		id := network.GetNetworkId()
		ip := network.GetIpStatus()
		if !command.ValidNetworkID(id) || seen[id] || ip == nil || ip.NetworkId != id {
			return false
		}
		seen[id] = true
		if query != nil && (query.DefaultNetwork && !network.Active || !query.DefaultNetwork && query.NetworkId != id) {
			return false
		}
		for _, group := range []string{"capabilities", "link_properties"} {
			if available, _ := Availability(ip.ObservationFields, group); !available {
				return false
			}
		}
		if network.Active {
			active++
		}
	}
	return active <= 1
}
