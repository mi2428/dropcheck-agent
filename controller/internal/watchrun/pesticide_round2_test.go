package watchrun

import (
	"context"
	"testing"

	"github.com/mi2428/dropcheck-agent/controller/internal/command"
	"github.com/mi2428/dropcheck-agent/controller/internal/control"
	"github.com/mi2428/dropcheck-agent/controller/internal/controlpb"
	"github.com/mi2428/dropcheck-agent/controller/internal/runner"
)

func TestRound2NetworkResponseIdentityAndConsistency(t *testing.T) {
	op, err := command.NetworkOperation("42")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		snapshot *controlpb.NetworkSnapshot
		valid    bool
	}{
		{&controlpb.NetworkSnapshot{}, false},
		{&controlpb.NetworkSnapshot{Networks: []*controlpb.NetworkDiagnostics{{NetworkId: "43", IpStatus: &controlpb.IpStatus{NetworkId: "43"}}}}, false},
		{&controlpb.NetworkSnapshot{Networks: []*controlpb.NetworkDiagnostics{{NetworkId: "42", IpStatus: &controlpb.IpStatus{NetworkId: "43"}}}}, false},
		{&controlpb.NetworkSnapshot{Errors: []string{"capabilities denied"}, Networks: []*controlpb.NetworkDiagnostics{{NetworkId: "42", IpStatus: &controlpb.IpStatus{NetworkId: "42"}}}}, false},
		{&controlpb.NetworkSnapshot{ObservedAtUnixMs: 1, Networks: []*controlpb.NetworkDiagnostics{{NetworkId: "42", IpStatus: &controlpb.IpStatus{NetworkId: "42", ObservationFields: []*controlpb.DiagnosticField{{Key: "capabilities.state", Value: "available"}, {Key: "link_properties.state", Value: "available"}}}}}}, true},
	} {
		result, err := ExecuteOperation(t.Context(), agentRunnerFunc(func(context.Context, control.AgentInfo, command.Operation) (runner.Result, error) {
			return runner.Result{Result: &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_OK, Payload: &controlpb.CommandResult_NetworkSnapshot{NetworkSnapshot: tc.snapshot}}}, nil
		}), control.AgentInfo{ID: "round2-agent"}, op)
		rejected := err != nil || result.Raw.GetStatus() != controlpb.CommandResult_STATUS_OK
		for _, finding := range result.Findings {
			rejected = rejected || !finding.Passed || finding.Missing
		}
		if rejected == tc.valid {
			t.Errorf("Network response valid=%v, rejected=%v: %v", tc.valid, rejected, tc.snapshot)
		}
	}
}

func TestNetworkSnapshotRotatedContract(t *testing.T) {
	network := func(id string, active bool) *controlpb.NetworkDiagnostics {
		return &controlpb.NetworkDiagnostics{NetworkId: id, Active: active, IpStatus: &controlpb.IpStatus{NetworkId: id, ObservationFields: []*controlpb.DiagnosticField{{Key: "capabilities.state", Value: "available"}, {Key: "link_properties.state", Value: "available"}}}}
	}
	one := &controlpb.NetworkSnapshot{ObservedAtUnixMs: 1, Networks: []*controlpb.NetworkDiagnostics{network("42", true)}}
	for _, query := range []*controlpb.GetNetwork{nil, {NetworkId: "42"}, {DefaultNetwork: true}} {
		if !validNetworkSnapshot(query, one) {
			t.Fatal("valid explicit/default/list response rejected", query)
		}
	}
	if !validNetworkSnapshot(nil, &controlpb.NetworkSnapshot{ObservedAtUnixMs: 1}) {
		t.Fatal("empty enumeration is a valid observation")
	}
	for _, snapshot := range []*controlpb.NetworkSnapshot{
		{ObservedAtUnixMs: 1, Networks: []*controlpb.NetworkDiagnostics{network("42", true), network("42", false)}},
		{ObservedAtUnixMs: 1, Networks: []*controlpb.NetworkDiagnostics{network("42", true), network("43", true)}},
		{ObservedAtUnixMs: 1, Networks: []*controlpb.NetworkDiagnostics{network("042", false)}},
		{ObservedAtUnixMs: 1, Networks: []*controlpb.NetworkDiagnostics{{NetworkId: "42"}}},
		{ObservedAtUnixMs: 1, Networks: []*controlpb.NetworkDiagnostics{{NetworkId: "42", IpStatus: &controlpb.IpStatus{NetworkId: "43"}}}},
		{ObservedAtUnixMs: 1, Networks: one.Networks, Errors: []string{"partial"}},
		{Networks: one.Networks},
	} {
		for _, query := range []*controlpb.GetNetwork{nil, {NetworkId: "42"}} {
			if validNetworkSnapshot(query, snapshot) {
				t.Error("invalid snapshot accepted", snapshot)
			}
		}
	}
	if validNetworkSnapshot(&controlpb.GetNetwork{DefaultNetwork: true}, &controlpb.NetworkSnapshot{ObservedAtUnixMs: 1, Networks: []*controlpb.NetworkDiagnostics{network("42", false)}}) || validNetworkSnapshot(&controlpb.GetNetwork{NetworkId: "43"}, one) {
		t.Fatal("wrong requested subject accepted")
	}
}
