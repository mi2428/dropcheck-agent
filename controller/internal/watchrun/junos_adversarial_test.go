package watchrun

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
	r := cleanupRunnerFunc(func(context.Context, command.Operation) (runner.Result, error) { calls++; return runner.Result{}, nil })
	if _, err := ExecuteOperation(context.Background(), r, control.AgentInfo{}, op); err == nil {
		t.Fatal("missing capability accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ExecuteOperation(ctx, r, control.AgentInfo{}, command.WifiStatusOperation()); err == nil {
		t.Fatal("canceled acquisition accepted")
	}
	if calls != 0 {
		t.Fatal("unsafe dispatches", calls)
	}
}

func TestJunosQAFreshEvidenceMustBeUniqueAndTrue(t *testing.T) {
	op, err := command.WifiFreshScanOperation("all", "1000")
	if err != nil {
		t.Fatal(err)
	}
	valid := runtimeFixture(op)
	if !payloadMatches(op, valid) {
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
			if payloadMatches(op, raw) {
				t.Fatal("forged success accepted", key, mode)
			}
		}
	}
}
