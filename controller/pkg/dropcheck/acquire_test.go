package dropcheck

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/mi2428/dropcheck-agent/controller/internal/acquire"
	"github.com/mi2428/dropcheck-agent/controller/internal/command"
	"github.com/mi2428/dropcheck-agent/controller/internal/control"
	"github.com/mi2428/dropcheck-agent/controller/internal/controlpb"
	"github.com/mi2428/dropcheck-agent/controller/internal/runner"
	"google.golang.org/protobuf/proto"
)

type projectionRunner struct{ raw *controlpb.CommandResult }

func (r projectionRunner) Run(context.Context, control.AgentInfo, command.Operation) (runner.Result, error) {
	return runner.Result{CommandID: "acquired-command", Result: r.raw}, nil
}

func TestPublicResultProjectsNeutralAcquisitionWithoutTUITypes(t *testing.T) {
	op, err := Traceroute(TracerouteOptions{Host: "example.test", Via: []string{"router.example.test"}})
	if err != nil {
		t.Fatal(err)
	}
	agent := control.AgentInfo{ID: "agent", SessionID: "original", Connected: time.Unix(123, 0)}
	value, err := acquire.Execute(t.Context(), projectionRunner{&controlpb.CommandResult{Status: StatusOK, Payload: &TraceroutePayload{Traceroute: &TracerouteResult{ReachedTarget: new(false)}}}}, agent, op.impl)
	if err != nil {
		t.Fatal(err)
	}
	result := publicResult(value)
	if !proto.Equal(result.Raw, value.Raw) || len(result.Parts) != 1 || len(result.Findings) != 1 {
		t.Fatal("public projection changed acquisition")
	}
	p, part := result.Parts[0], value.Parts[0]
	if p.AgentID != part.AgentID || p.SessionID != part.SessionID || p.CommandID != part.CommandID || p.Name != part.Name || p.Started != part.Started || p.Ended != part.Ended || p.Error != part.Error || !proto.Equal(p.Raw, part.Raw) {
		t.Fatal("public projection lost pinned identity/timing")
	}
	f := value.Findings[0]
	if !reflect.DeepEqual(result.Findings[0], Finding{Metric: f.Metric, Observed: f.Observed, Expected: f.Expected, Passed: f.Passed, Missing: f.Missing, Message: f.Message}) {
		t.Fatal("public projection lost typed findings")
	}
	result.Raw.GetTraceroute().Host = "edited"
	if result.Parts[0].Raw.GetTraceroute().Host == "edited" {
		t.Fatal("public Raw aliases an acquisition part")
	}
}

func TestPublicEHTAcquisitionKeepsUnfilteredRaw(t *testing.T) {
	op, err := WiFiEHT(WiFiEHTOptions{})
	if err != nil {
		t.Fatal(err)
	}
	raw := &CommandResult{Status: StatusOK, Message: "password=synthetic-only", Payload: &WiFiDiagnosticsPayload{WifiDiagnostics: &WiFiDiagnosticsData{
		Status:       &WiFiStatusData{Connection: &WiFiConnection{Ssid: " Lab "}},
		Capabilities: &WiFiCapabilitiesData{},
		Scan: &WiFiScanData{Results: []*WiFiScanResult{
			{Ssid: " Lab ", Bssid: "aa:bb:cc:dd:ee:01"},
			{Ssid: "Other", Bssid: "aa:bb:cc:dd:ee:02"},
		}},
	}}}
	value, err := acquire.Execute(t.Context(), projectionRunner{raw}, control.AgentInfo{ID: "agent", SessionID: "session"}, op.impl)
	if err != nil {
		t.Fatal(err)
	}
	result := publicResult(value)
	if len(result.Parts) != 1 || result.Parts[0].Name != "wifi.eht" || len(result.Findings) != 0 {
		t.Fatalf("unexpected EHT acquisition: parts=%d findings=%v", len(result.Parts), result.Findings)
	}
	if !proto.Equal(result.Raw, raw) || !proto.Equal(result.Parts[0].Raw, raw) {
		t.Fatal("public EHT results filtered or sanitized acquisition data")
	}
	result.Raw.GetWifiDiagnostics().Scan.Results[0].Ssid = "edited"
	if result.Parts[0].Raw.GetWifiDiagnostics().Scan.Results[0].Ssid != " Lab " || raw.GetWifiDiagnostics().Scan.Results[0].Ssid != " Lab " {
		t.Fatal("public EHT Raw aliases its part or the acquired payload")
	}
}
