package tuirun

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/mi2428/dropcheck-agent/controller/internal/command"
	"github.com/mi2428/dropcheck-agent/controller/internal/controlpb"
	"github.com/mi2428/dropcheck-agent/controller/internal/runner"
	"google.golang.org/protobuf/proto"
)

type exportBoundaryExpectation struct{ seen bool }

func (e *exportBoundaryExpectation) Evaluate(result Result) []Finding {
	e.seen = strings.Contains(result.Run.Raw.Message, "query-secret")
	f := Fail("error", result.Run.Raw.Message, "Authorization: Bearer expected-secret", "password=finding-secret")
	f.ObservedValue = map[string]any{"Authorization": "Bearer nested-secret", "ssid": "password=Guest", "zero": 0, "false": false, "raw": result.Run.Raw}
	return []Finding{f}
}

func TestExecuteRealJSONLExportProtectsOwnedProjection(t *testing.T) {
	expect := &exportBoundaryExpectation{}
	plan := compiledFixture(t, Plan{Networks: []Network{{SSID: "password=Guest", PSK: SecretValue("configured-secret"), DisconnectAfter: true}}, Checks: []Check{NewCheck("diagnostic", "", command.WifiDiagnosticsOperation(), Policy{}, false, expect)}})
	var out bytes.Buffer
	writer := NewJSONLWriter(&out)
	var acquired *controlpb.CommandResult
	var retained Event
	observer := cleanupSinkFunc(func(_ context.Context, event Event) error {
		if event.Attempt != nil && event.Attempt.Result.Raw.GetWifiDiagnostics() != nil {
			retained = event
		}
		return nil
	})
	_, err := Execute(context.Background(), plan, cleanupRunnerFunc(func(_ context.Context, op command.Operation) (runner.Result, error) {
		raw := runtimeFixture(op)
		if op.Name == "wifi.diagnostics" {
			raw.Message = "configured-secret https://user-secret:pass-secret@example.test/path?%74oken=query-secret#access_token=fragment-secret Authorization: Bearer auth-secret"
			d := raw.GetWifiDiagnostics()
			d.Status.Connection.Ssid = "password=Guest"
			d.Status.Connection.Bssid = "02:00:00:00:00:01"
			d.Status.ObservationFields = append(d.Status.ObservationFields, &controlpb.DiagnosticField{Key: "password", Value: "field-secret"}, &controlpb.DiagnosticField{Key: "authorization", Value: "Bearer field-auth-secret"}, &controlpb.DiagnosticField{Key: "ssid", Value: "password=Guest"})
			d.Scan.Results = []*controlpb.WifiScanResult{{Ssid: "password=Guest", Bssid: "02:00:00:00:00:01", ObservationAgeMs: proto.Uint64(0), InformationElements: []*controlpb.WifiInformationElement{{Id: 48, BytesHex: "70617373776f72643d4775657374"}}, SecurityDetails: &controlpb.WifiSecurityDetails{RsnCapabilities: 128, RsnCapabilitiesHex: "0080"}}}
			acquired = raw
		}
		return runner.Result{Result: raw}, nil
	}), ExecuteOptions{Rounds: 1, Sinks: []Sink{writer, observer}})
	if err != nil || !expect.seen || acquired == nil || retained.Attempt == nil {
		t.Fatalf("execution err=%v evaluated original=%v retained=%v", err, expect.seen, retained.Attempt != nil)
	}
	text := out.String()
	for _, secret := range []string{"configured-secret", "user-secret", "pass-secret", "query-secret", "fragment-secret", "auth-secret", "field-secret", "field-auth-secret", "expected-secret", "finding-secret", "nested-secret"} {
		if strings.Contains(text, secret) {
			t.Errorf("JSONL disclosed %s", secret)
		}
	}
	if !strings.Contains(acquired.Message, "configured-secret") || !strings.Contains(retained.Attempt.Result.Raw.Message, "query-secret") {
		t.Fatal("export changed acquisition or sibling sink")
	}
	decoded := false
	for line := range bytes.SplitSeq(bytes.TrimSpace(out.Bytes()), []byte("\n")) {
		var event struct {
			Attempt *struct {
				Result struct {
					Raw struct {
						Payload struct{ WifiDiagnostics *controlpb.WifiDiagnostics }
					}
				}
				Findings []Finding
			}
		}
		if err := json.Unmarshal(line, &event); err != nil {
			t.Fatal(err)
		}
		if event.Attempt == nil || event.Attempt.Result.Raw.Payload.WifiDiagnostics == nil {
			continue
		}
		decoded = true
		d := event.Attempt.Result.Raw.Payload.WifiDiagnostics
		ap := d.Scan.Results[0]
		if ap.Ssid != "password=Guest" || ap.Bssid != "02:00:00:00:00:01" || ap.ObservationAgeMs == nil || *ap.ObservationAgeMs != 0 || ap.InformationElements[0].BytesHex != "70617373776f72643d4775657374" || ap.SecurityDetails.RsnCapabilities != 128 || ap.SecurityDetails.RsnCapabilitiesHex != "0080" || d.Status.ObservationFields[len(d.Status.ObservationFields)-1].Value != "password=Guest" {
			t.Fatalf("literal identity/known zero changed: %v", d)
		}
		values := event.Attempt.Findings[0].ObservedValue.(map[string]any)
		if values["zero"] != float64(0) || values["false"] != false || values["ssid"] != "password=Guest" {
			t.Fatalf("typed values changed: %v", values)
		}
	}
	if !decoded {
		t.Fatal("actual writer emitted no diagnostic attempt")
	}
}

func TestJSONLExportProtectsPartErrorsAndRejectsUnknownValues(t *testing.T) {
	var out bytes.Buffer
	writer := NewJSONLWriter(&out)
	defer func() { _ = writer.Close(context.Background()) }()
	event := Event{Attempt: &Attempt{Result: OperationResult{Parts: []OperationRecord{{Error: "password=part-secret", Raw: &controlpb.CommandResult{Message: "Authorization: Bearer part-auth"}}}}}}
	if err := writer.Emit(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "part-secret") || strings.Contains(out.String(), "part-auth") || event.Attempt.Result.Parts[0].Error != "password=part-secret" {
		t.Fatal("part export leaked or mutated source")
	}
	event.Finding = &Finding{ObservedValue: struct{ Password string }{"opaque-secret"}}
	if err := writer.Emit(context.Background(), event); err == nil || strings.Contains(out.String(), "opaque-secret") {
		t.Fatal("unknown value bypassed safe export")
	}
}

func TestExecuteJSONLExportKeepsTransportCauseAndCleanup(t *testing.T) {
	failure := errors.New("Authorization: Bearer transport-secret")
	plan := compiledFixture(t, Plan{Networks: []Network{{SSID: "Lab", PSK: SecretValue("synthetic-passphrase"), DisconnectAfter: true}}})
	var out bytes.Buffer
	cleaned := false
	report, err := Execute(context.Background(), plan, cleanupRunnerFunc(func(_ context.Context, op command.Operation) (runner.Result, error) {
		if op.Name == "wifi.connect" {
			return runner.Result{}, failure
		}
		cleaned = op.Name == "wifi.disconnect"
		return runner.Result{Result: runtimeFixture(op)}, nil
	}), ExecuteOptions{Rounds: 1, Sinks: []Sink{NewJSONLWriter(&out)}})
	if !errors.Is(err, failure) || report.Passed() || !cleaned || strings.Contains(out.String(), "transport-secret") {
		t.Fatalf("cause/cleanup/export invariant err=%v cleaned=%v", err, cleaned)
	}
}
