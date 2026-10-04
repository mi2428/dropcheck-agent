package harness

import (
	"context"
	"fmt"
	"slices"
	"time"

	"dropcheck/controller/internal/command"
	"dropcheck/controller/internal/control"
	"dropcheck/controller/internal/controlpb"
	"google.golang.org/protobuf/proto"
)

// ExecuteOperation is the common one-shot acquisition boundary. Scenario
// policy calls it once per attempt; it contains no retries or test/UI logic.
func ExecuteOperation(ctx context.Context, r OperationRunner, agent control.AgentInfo, op command.Operation) (OperationResult, error) {
	if r == nil {
		return OperationResult{}, fmt.Errorf("operation runner is nil")
	}
	if err := command.ValidateOperation(op); err != nil {
		return OperationResult{}, err
	}
	cmd, options, err := command.BuildRunCommand(op)
	if err != nil {
		return OperationResult{}, err
	}
	result := OperationResult{Options: options}
	run := func(operation command.Operation) (*controlpb.CommandResult, error) {
		wire, _, buildErr := command.BuildRunCommand(operation)
		if buildErr != nil {
			return nil, buildErr
		}
		started := time.Now()
		runCtx, cancel := context.WithTimeout(ctx, command.TimeoutFor(wire))
		exec, runErr := r.Run(runCtx, agent, operation)
		cancel()
		record := OperationRecord{Scope: Scope{Kind: ScopeAgent, AgentID: agentKey(agent)}, Name: operation.Name, CommandID: exec.CommandID, Started: started, Ended: time.Now(), Raw: cloneRaw(exec.Result)}
		if runErr != nil {
			record.Error = runErr.Error()
		}
		result.Parts = append(result.Parts, record)
		return cloneRaw(exec.Result), runErr
	}
	var fresh *controlpb.CommandResult
	if options.WifiEHTFreshScan {
		freshOp, buildErr := command.WifiFreshScanOperation("all", fmt.Sprint(options.WifiEHTFreshScanTimeoutMs))
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
	if options.WifiEHTFreshScan {
		if result.Raw != nil && result.Raw.GetWifiDiagnostics() != nil && fresh.GetWifiScan() != nil {
			result.Raw.GetWifiDiagnostics().Scan = proto.Clone(fresh.GetWifiScan()).(*controlpb.WifiScan)
			result.Raw.ElapsedMs += fresh.GetElapsedMs()
		}
		if fresh == nil || fresh.GetWifiScan() == nil || fresh.GetStatus() != controlpb.CommandResult_STATUS_OK {
			result.Findings = append(result.Findings, MissingFinding("fresh_scan", "<missing>", "fresh acquisition succeeded", "requested fresh acquisition did not succeed"))
			if fresh != nil && fresh.GetStatus() != controlpb.CommandResult_STATUS_OK {
				result.Findings[len(result.Findings)-1] = Fail("fresh_scan", fresh.GetStatus().String(), "STATUS_OK", fresh.GetMessage())
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
				result.Findings = append(result.Findings, MissingFinding("trace.via", "<missing>", required, "typed hop observations unavailable"))
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
				result.Findings = append(result.Findings, Pass("trace.via", required, "required hop present"))
			} else {
				result.Findings = append(result.Findings, Fail("trace.via", "not observed", required, "required hop not present"))
			}
		}
	}
	return result, nil
}

func payloadMatches(op command.Operation, raw *controlpb.CommandResult) bool {
	if raw == nil || raw.Payload == nil {
		return false
	}
	switch op.Command.Command.(type) {
	case *controlpb.RunCommand_GetWifiStatus:
		return raw.GetWifiStatus() != nil
	case *controlpb.RunCommand_GetWifiDiagnostics:
		return raw.GetWifiDiagnostics() != nil
	case *controlpb.RunCommand_GetWifiScan, *controlpb.RunCommand_GetFreshWifiScan:
		return raw.GetWifiScan() != nil
	case *controlpb.RunCommand_GetWifiScanDetail:
		return raw.GetWifiScanDetail() != nil
	case *controlpb.RunCommand_GetWifiCapabilities:
		return raw.GetWifiCapabilities() != nil
	case *controlpb.RunCommand_ConnectWifi:
		return raw.GetConnectWifi() != nil
	case *controlpb.RunCommand_DisconnectWifi, *controlpb.RunCommand_ForgetWifi, *controlpb.RunCommand_ReconnectWifi:
		return raw.GetWifiOperation() != nil
	case *controlpb.RunCommand_WaitWifiConnected, *controlpb.RunCommand_AssertWifi:
		return raw.GetWifiAssert() != nil
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
