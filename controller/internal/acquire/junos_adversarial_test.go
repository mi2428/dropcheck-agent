package acquire

import (
	"context"
	"testing"

	"github.com/mi2428/dropcheck-agent/controller/internal/command"
	"github.com/mi2428/dropcheck-agent/controller/internal/control"
	"github.com/mi2428/dropcheck-agent/controller/internal/controlpb"
	"github.com/mi2428/dropcheck-agent/controller/internal/runner"
	"google.golang.org/protobuf/proto"
)

func TestJunosQATypedForgetAndCancellationNeverDispatch(t *testing.T) {
	op := command.NewOperation("wifi.forget", &controlpb.RunCommand{Command: &controlpb.RunCommand_ForgetWifi{ForgetWifi: &controlpb.ForgetWifi{Selector: &controlpb.ForgetWifi_Ssid{Ssid: "123"}}}}, command.Options{})
	calls := 0
	r := runnerFunc(func(context.Context, control.AgentInfo, command.Operation) (runner.Result, error) {
		calls++
		return runner.Result{}, nil
	})
	if _, err := Execute(context.Background(), r, control.AgentInfo{}, op); err == nil {
		t.Fatal("missing capability accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Execute(ctx, r, control.AgentInfo{}, command.WifiStatusOperation()); err == nil {
		t.Fatal("canceled acquisition accepted")
	}
	if calls != 0 {
		t.Fatal("unsafe dispatches", calls)
	}
}

func TestTypedForgetUsesCapabilityAndRetainsKnownConfigIDZero(t *testing.T) {
	op := command.NewOperation("wifi.forget", &controlpb.RunCommand{Command: &controlpb.RunCommand_ForgetWifi{ForgetWifi: &controlpb.ForgetWifi{Selector: &controlpb.ForgetWifi_ConfigId{ConfigId: 0}}}}, command.Options{})
	calls := 0
	result, err := Execute(t.Context(), runnerFunc(func(_ context.Context, _ control.AgentInfo, got command.Operation) (runner.Result, error) {
		calls++
		f := got.Command.GetForgetWifi()
		if _, ok := f.GetSelector().(*controlpb.ForgetWifi_ConfigId); !ok || f.GetConfigId() != 0 || f.GetTarget() != "" {
			t.Fatal("typed zero was converted to a legacy string target")
		}
		return runner.Result{Result: &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_OK, Payload: &controlpb.CommandResult_WifiOperation{WifiOperation: &controlpb.WifiOperationResult{Ok: true}}}}, nil
	}), control.AgentInfo{ID: "agent", SessionID: "original", Hello: &controlpb.AgentHello{Capabilities: []string{"wifi.forget.typed"}}}, op)
	if err != nil || calls != 1 || len(result.Findings) != 0 {
		t.Fatalf("typed forget failed: %+v %v", result, err)
	}
}

func TestJunosQAFreshEvidenceMustBeUniqueAndTrue(t *testing.T) {
	op, err := command.WifiFreshScanOperation("all", "1000")
	if err != nil {
		t.Fatal(err)
	}
	valid := &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_OK, Payload: &controlpb.CommandResult_WifiScan{WifiScan: freshScanFixture()}}
	if !PayloadMatches(op, valid) {
		t.Fatal("invalid positive control")
	}
	for _, key := range []string{"fresh_scan_receiver_registered", "fresh_scan_start_scan", "fresh_scan_broadcast_received", "fresh_scan_results_updated", "fresh_scan_wait_completed"} {
		for _, mode := range []string{"false", "missing", "duplicate"} {
			raw := proto.Clone(valid).(*controlpb.CommandResult)
			scan := raw.GetWifiScan()
			for i, field := range scan.Fields {
				if field.Key == key {
					switch mode {
					case "false":
						field.Value = "false"
					case "missing":
						scan.Fields = append(scan.Fields[:i], scan.Fields[i+1:]...)
					case "duplicate":
						scan.Fields = append(scan.Fields, proto.Clone(field).(*controlpb.DiagnosticField))
					}
					break
				}
			}
			if PayloadMatches(op, raw) {
				t.Fatal("forged success accepted", key, mode)
			}
		}
	}
}
