package acquire

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/mi2428/dropcheck-agent/controller/internal/command"
	"github.com/mi2428/dropcheck-agent/controller/internal/control"
	"github.com/mi2428/dropcheck-agent/controller/internal/controlpb"
	"github.com/mi2428/dropcheck-agent/controller/internal/runner"
	"google.golang.org/protobuf/proto"
)

type runnerFunc func(context.Context, control.AgentInfo, command.Operation) (runner.Result, error)

func (f runnerFunc) Run(ctx context.Context, agent control.AgentInfo, op command.Operation) (runner.Result, error) {
	return f(ctx, agent, op)
}

func freshScanFixture() *controlpb.WifiScan {
	scan := &controlpb.WifiScan{Results: []*controlpb.WifiScanResult{{Ssid: "Lab", ObservationAgeMs: new(uint64(0))}}}
	for _, key := range []string{"fresh_scan_receiver_registered", "fresh_scan_start_scan", "fresh_scan_broadcast_received", "fresh_scan_results_updated", "fresh_scan_wait_completed"} {
		scan.Fields = append(scan.Fields, &controlpb.DiagnosticField{Key: key, Value: "true"})
	}
	return scan
}

func diagnosticsFixture() *controlpb.CommandResult {
	return &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_OK, ElapsedMs: 11, Payload: &controlpb.CommandResult_WifiDiagnostics{WifiDiagnostics: &controlpb.WifiDiagnostics{Status: &controlpb.WifiStatus{}, Capabilities: &controlpb.WifiCapabilities{}, Scan: &controlpb.WifiScan{}}}}
}

func TestOneShotAcquisitionReportsWrongPayload(t *testing.T) {
	if PayloadMatches(command.Operation{}, &controlpb.CommandResult{Payload: &controlpb.CommandResult_IpStatus{IpStatus: &controlpb.IpStatus{}}}) {
		t.Fatal("zero operation accepted a payload")
	}
	op, err := command.PingOperation(command.PingOptions{Host: "example.test"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := Execute(t.Context(), runnerFunc(func(context.Context, control.AgentInfo, command.Operation) (runner.Result, error) {
		return runner.Result{Result: &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_OK, Payload: &controlpb.CommandResult_IpStatus{IpStatus: &controlpb.IpStatus{}}}}, nil
	}), control.AgentInfo{ID: "a"}, op)
	if err != nil || len(result.Findings) != 1 || !result.Findings[0].Missing {
		t.Fatalf("wrong payload accepted: %+v (%v)", result.Findings, err)
	}
}

func TestFailedPayloadsNeedTheRightTypeButNotSuccessfulObservations(t *testing.T) {
	for _, tc := range []struct {
		wire *controlpb.RunCommand
		raw  *controlpb.CommandResult
	}{
		{&controlpb.RunCommand{Command: &controlpb.RunCommand_ConnectWifi{ConnectWifi: &controlpb.ConnectWifi{}}}, &controlpb.CommandResult{Payload: &controlpb.CommandResult_ConnectWifi{ConnectWifi: &controlpb.ConnectWifiResult{}}}},
		{&controlpb.RunCommand{Command: &controlpb.RunCommand_DisconnectWifi{DisconnectWifi: &controlpb.DisconnectWifi{}}}, &controlpb.CommandResult{Payload: &controlpb.CommandResult_WifiOperation{WifiOperation: &controlpb.WifiOperationResult{}}}},
		{&controlpb.RunCommand{Command: &controlpb.RunCommand_WaitWifiConnected{WaitWifiConnected: &controlpb.WaitWifiConnected{}}}, &controlpb.CommandResult{Payload: &controlpb.CommandResult_WifiAssert{WifiAssert: &controlpb.WifiAssertResult{}}}},
		{&controlpb.RunCommand{Command: &controlpb.RunCommand_GetFreshWifiScan{GetFreshWifiScan: &controlpb.GetFreshWifiScan{}}}, &controlpb.CommandResult{Payload: &controlpb.CommandResult_WifiScan{WifiScan: &controlpb.WifiScan{}}}},
		{&controlpb.RunCommand{Command: &controlpb.RunCommand_GetWifiDiagnostics{GetWifiDiagnostics: &controlpb.GetWifiDiagnostics{}}}, &controlpb.CommandResult{Payload: &controlpb.CommandResult_WifiDiagnostics{WifiDiagnostics: &controlpb.WifiDiagnostics{}}}},
		{&controlpb.RunCommand{Command: &controlpb.RunCommand_GetNetworks{GetNetworks: &controlpb.GetNetworks{}}}, &controlpb.CommandResult{Payload: &controlpb.CommandResult_NetworkSnapshot{NetworkSnapshot: &controlpb.NetworkSnapshot{}}}},
	} {
		op := command.NewOperation("test", tc.wire, command.Options{})
		tc.raw.Status = controlpb.CommandResult_STATUS_FAILED
		if !PayloadMatches(op, tc.raw) {
			t.Fatalf("failed typed evidence treated as missing: %T", tc.wire.Command)
		}
		tc.raw.Status = controlpb.CommandResult_STATUS_OK
		if PayloadMatches(op, tc.raw) {
			t.Fatalf("contradictory success accepted: %T", tc.wire.Command)
		}
		tc.raw.Status = controlpb.CommandResult_STATUS_FAILED
		tc.raw.Payload = &controlpb.CommandResult_IpStatus{IpStatus: &controlpb.IpStatus{}}
		if PayloadMatches(op, tc.raw) {
			t.Fatal("wrong failure payload accepted")
		}
		tc.raw.Payload = nil
		if PayloadMatches(op, tc.raw) {
			t.Fatal("missing failure payload accepted")
		}
	}
}

func TestDiagnosticsPayloadRejectsInconsistentNetworkEvidence(t *testing.T) {
	op := command.WifiDiagnosticsOperation()
	valid := diagnosticsFixture()
	valid.GetWifiDiagnostics().Networks = []*controlpb.NetworkDiagnostics{{NetworkId: "42", IpStatus: &controlpb.IpStatus{NetworkId: "42", ObservationFields: []*controlpb.DiagnosticField{{Key: "capabilities.state", Value: "available"}, {Key: "link_properties.state", Value: "available"}}}}}
	if !PayloadMatches(op, valid) {
		t.Fatal("valid Network diagnostics rejected")
	}
	for _, mutate := range []func(*controlpb.WifiDiagnostics){
		func(d *controlpb.WifiDiagnostics) { d.Status = nil },
		func(d *controlpb.WifiDiagnostics) { d.Capabilities.Errors = []string{"partial"} },
		func(d *controlpb.WifiDiagnostics) { d.Scan.Errors = []string{"partial"} },
		func(d *controlpb.WifiDiagnostics) { d.Networks[0].NetworkId = "042" },
		func(d *controlpb.WifiDiagnostics) { d.Networks[0].IpStatus.NetworkId = "43" },
		func(d *controlpb.WifiDiagnostics) { d.Networks[0].IpStatus.ObservationFields = nil },
		func(d *controlpb.WifiDiagnostics) { d.Networks = append(d.Networks, d.Networks[0]) },
	} {
		raw := cloneRaw(valid)
		mutate(raw.GetWifiDiagnostics())
		if PayloadMatches(op, raw) {
			t.Fatal("partial/inconsistent Network diagnostics accepted")
		}
	}
}

func TestCompoundFreshOutcomesRetainReferenceAndIndependentParts(t *testing.T) {
	op, err := command.WifiEHTOperationWithOptions(command.WifiEHTOptions{Fresh: true, Timeout: "9000"})
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"success", "failed cached", "failed no payload", "canceled", "wrong payload", "forged success", "nil", "transport"} {
		t.Run(mode, func(t *testing.T) {
			fresh := &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_OK, ElapsedMs: 7, Payload: &controlpb.CommandResult_WifiScan{WifiScan: freshScanFixture()}}
			transport := errors.New("synthetic transport failure")
			var runErr error
			switch mode {
			case "failed cached":
				fresh.Status, fresh.Message = controlpb.CommandResult_STATUS_FAILED, "synthetic refresh failure"
			case "failed no payload":
				fresh.Status, fresh.Payload = controlpb.CommandResult_STATUS_FAILED, nil
			case "canceled":
				fresh.Status = controlpb.CommandResult_STATUS_CANCELED
			case "wrong payload":
				fresh.Payload = &controlpb.CommandResult_IpStatus{IpStatus: &controlpb.IpStatus{}}
			case "forged success":
				fresh.GetWifiScan().Fields = nil
			case "nil":
				fresh = nil
			case "transport":
				runErr = transport
			}
			original := cloneRaw(fresh)
			diagnostics := diagnosticsFixture()
			var names []string
			agent := control.AgentInfo{ID: "agent", SessionID: "pinned", Connected: time.Unix(123, 0)}
			result, err := Execute(t.Context(), runnerFunc(func(ctx context.Context, got control.AgentInfo, operation command.Operation) (runner.Result, error) {
				if !reflect.DeepEqual(got, agent) {
					t.Fatal("connection identity changed between parts")
				}
				if _, ok := ctx.Deadline(); !ok {
					t.Fatal("part has no deadline")
				}
				names = append(names, operation.Name)
				if len(names) == 1 {
					if req := operation.Command.GetGetFreshWifiScan(); req == nil || req.TimeoutMs != 9000 {
						t.Fatal("fresh timeout lost")
					}
					return runner.Result{CommandID: "scan-command", Result: fresh}, runErr
				}
				if operation.Options.WifiEHTFreshScan {
					t.Fatal("atomic runner would acquire the fresh scan twice")
				}
				return runner.Result{CommandID: "diagnostics-command", Result: diagnostics}, nil
			}), agent, op)
			compound := mode == "success" || mode == "failed cached" || mode == "forged success"
			wantParts := 1
			if compound {
				wantParts = 2
			}
			if len(result.Parts) != wantParts {
				t.Fatalf("parts=%d calls=%v", len(result.Parts), names)
			}
			if (mode == "nil" || mode == "transport") != (err != nil) {
				t.Fatalf("error=%v mode=%s", err, mode)
			}
			if mode == "transport" && !errors.Is(err, transport) {
				t.Fatal("transport cause lost")
			}
			for _, part := range result.Parts {
				if part.AgentID != agent.ID || part.SessionID != agent.SessionID || !part.Connected.Equal(agent.Connected) || part.CommandID == "" || part.Ended.Before(part.Started) {
					t.Fatal("part identity/timing lost")
				}
			}
			if !proto.Equal(original, fresh) {
				t.Fatal("fresh processing mutated runner data")
			}
			if compound {
				if result.Raw.ElapsedMs != 18 || !proto.Equal(result.Parts[1].Raw, diagnostics) {
					t.Fatal("compound view damaged part snapshots")
				}
				want := "fresh"
				if mode != "success" {
					want = "cached (refresh failed)"
				}
				fields := result.Raw.GetWifiDiagnostics().GetScan().GetFields()
				if fields[len(fields)-1].GetValue() != want {
					t.Fatal("fresh/cached provenance lost")
				}
				if mode == "success" && (result.Raw.Status != controlpb.CommandResult_STATUS_OK || len(result.Findings) != 0) {
					t.Fatal("valid acquisition rejected")
				}
				if mode != "success" && (result.Raw.Status != controlpb.CommandResult_STATUS_FAILED || len(result.Findings) == 0 || result.Findings[0].Passed || result.Findings[0].Missing) {
					t.Fatal("refresh failure hidden")
				}
				result.Raw.GetWifiDiagnostics().Scan.Results[0].Ssid = "edited"
				if result.Parts[0].Raw.GetWifiScan().Results[0].Ssid != "Lab" || len(diagnostics.GetWifiDiagnostics().Scan.Results) != 0 {
					t.Fatal("compound payloads share ownership")
				}
			}
		})
	}
}

func TestAcquisitionContextCauseAndPartialPayloadArePreserved(t *testing.T) {
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
		ctx, cancel := context.WithCancel(t.Context())
		started := make(chan struct{})
		done := make(chan struct {
			result Result
			err    error
		}, 1)
		go func() {
			result, err := Execute(ctx, runnerFunc(func(runCtx context.Context, _ control.AgentInfo, _ command.Operation) (runner.Result, error) {
				close(started)
				<-runCtx.Done()
				return runner.Result{CommandID: "partial", Result: diagnosticsFixture()}, cause
			}), control.AgentInfo{ID: "agent", SessionID: "original"}, command.WifiDiagnosticsOperation())
			done <- struct {
				result Result
				err    error
			}{result, err}
		}()
		select {
		case <-started:
		case <-time.After(time.Second):
			cancel()
			t.Fatal("fake acquisition did not start")
		}
		cancel()
		select {
		case got := <-done:
			if !errors.Is(got.err, cause) || len(got.result.Parts) != 1 || got.result.Parts[0].Error == "" || got.result.Raw == nil {
				t.Fatalf("partial/cause lost: %+v %v", got.result, got.err)
			}
			got.result.Raw.Message = "edited"
			if got.result.Parts[0].Raw.Message == "edited" {
				t.Fatal("partial Raw aliases its Part")
			}
		case <-time.After(time.Second):
			t.Fatal("canceled acquisition did not stop")
		}
	}
}

func TestAcquisitionOptionsAndUnknownAvailabilityStayOwned(t *testing.T) {
	op, err := command.TracerouteOperation(command.TracerouteOptions{Host: "example.test", Via: []string{"192.0.2.1"}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := Execute(t.Context(), runnerFunc(func(context.Context, control.AgentInfo, command.Operation) (runner.Result, error) {
		return runner.Result{}, nil
	}), control.AgentInfo{}, op)
	if err != nil {
		t.Fatal(err)
	}
	result.Options.TracerouteRequiredHops[0] = "edited"
	if op.Options.TracerouteRequiredHops[0] != "192.0.2.1" {
		t.Fatal("acquisition returned borrowed options")
	}
	for _, state := range []string{"", "unavailable", "invalid", "available"} {
		fields := []*controlpb.DiagnosticField{{Key: "identity.state", Value: state}, {Key: "identity.reason", Value: "synthetic reason"}}
		known, reason := Availability(fields, "identity")
		if known != (state == "available") || reason != "synthetic reason" {
			t.Fatal("unknown/known availability changed")
		}
	}
	if known, reason := Availability(nil, "identity"); known || reason != "availability not reported" {
		t.Fatal("missing metadata became known")
	}
}
