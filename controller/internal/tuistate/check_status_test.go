package tuistate

import (
	"testing"
	"time"

	"github.com/mi2428/dropcheck-agent/controller/internal/tuirun"
)

func TestCheckStatusTargetCellKeepsHistoricalResultUntilCurrentStepArrives(t *testing.T) {
	now := time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC)
	agentA := tuirun.AgentSnapshot{Name: "agent-a"}
	agentB := tuirun.AgentSnapshot{Name: "agent-b"}
	target := tuirun.TargetSnapshot{Name: "ap-1", SSID: "shownet"}
	state := State{
		Targets: []TargetState{
			{Agent: agentA, Target: target, Status: "running"},
			{Agent: agentB, Target: target, Status: "running"},
		},
		PassingChecks: []PassingCheck{{
			When:   now.Add(-2 * time.Second),
			Agent:  agentA,
			Target: target,
			Step:   tuirun.StepSnapshot{Name: "ping"},
		}},
		FailedChecks: []FailedCheck{{
			When:   now.Add(-time.Second),
			Agent:  agentB,
			Target: target,
			Finding: tuirun.Finding{
				Check:    "ping",
				Metric:   "loss",
				Expected: "0",
				Observed: "100",
			},
		}},
	}

	cell := state.CheckStatusTargetCell("ping", target, []tuirun.AgentSnapshot{agentA, agentB})
	if cell.Status != "failed" || cell.Count != 1 || cell.Failed != 1 || cell.Total != 2 || !cell.Stale {
		t.Fatalf("stale aggregate = %#v, want historical failed result across two agents", cell)
	}
}

func TestCheckStatusTargetCellUsesHistoricalResultWithoutCurrentState(t *testing.T) {
	now := time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC)
	agentA := tuirun.AgentSnapshot{Name: "agent-a"}
	agentB := tuirun.AgentSnapshot{Name: "agent-b"}
	target := tuirun.TargetSnapshot{Name: "ap-1", SSID: "shownet"}
	state := State{
		PassingChecks: []PassingCheck{{
			When:   now.Add(-2 * time.Second),
			Agent:  agentA,
			Target: target,
			Step:   tuirun.StepSnapshot{Name: "ping"},
		}},
		FailedChecks: []FailedCheck{{
			When:   now.Add(-time.Second),
			Agent:  agentB,
			Target: target,
			Finding: tuirun.Finding{
				Check:    "ping",
				Metric:   "loss",
				Expected: "0",
				Observed: "100",
			},
		}},
	}

	cell := state.CheckStatusTargetCell("ping", target, []tuirun.AgentSnapshot{agentA, agentB})
	if cell.Status != "failed" || cell.Count != 1 || cell.Failed != 1 || cell.Total != 2 || !cell.Stale {
		t.Fatalf("history-only aggregate = %#v, want one stale failed result across two agents", cell)
	}
}

func TestCheckStatusTargetCellPrefersCurrentResultOverHistory(t *testing.T) {
	now := time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC)
	agent := tuirun.AgentSnapshot{Name: "agent-a"}
	target := tuirun.TargetSnapshot{Name: "ap-1", SSID: "shownet"}
	state := State{
		Targets: []TargetState{{
			Agent:       agent,
			Target:      target,
			Status:      "running",
			CurrentStep: "ping",
			Steps:       []StepState{{Name: "ping", Status: "pending"}},
		}},
		FailedChecks: []FailedCheck{{
			When:   now.Add(-time.Second),
			Agent:  agent,
			Target: target,
			Finding: tuirun.Finding{
				Check:    "ping",
				Metric:   "loss",
				Expected: "0",
				Observed: "100",
			},
		}},
	}

	cell := state.CheckStatusTargetCell("ping", target, []tuirun.AgentSnapshot{agent})
	if cell.Status != "running" || cell.Count != 1 || cell.Failed != 0 || cell.Total != 1 || cell.Stale {
		t.Fatalf("current aggregate = %#v, want active running result to hide stale failure", cell)
	}
}

func TestCheckStatusTargetCellCountsReachedAgentsForRunning(t *testing.T) {
	agentA := tuirun.AgentSnapshot{Name: "agent-a"}
	agentB := tuirun.AgentSnapshot{Name: "agent-b"}
	target := tuirun.TargetSnapshot{Name: "ap-1", SSID: "shownet"}
	state := State{Targets: []TargetState{
		{
			Agent:  agentA,
			Target: target,
			Status: "running",
			Steps:  []StepState{{Name: "ping", Status: "ok"}},
		},
		{
			Agent:       agentB,
			Target:      target,
			Status:      "running",
			CurrentStep: "ping",
			Steps:       []StepState{{Name: "ping", Status: "pending"}},
		},
	}}

	cell := state.CheckStatusTargetCell("ping", target, []tuirun.AgentSnapshot{agentA, agentB})
	if cell.Status != "running" || cell.Count != 2 || cell.Total != 2 {
		t.Fatalf("running aggregate = %#v, want second reached agent to render RUN2/2", cell)
	}
}

func TestNewPreservesConfiguredTargetOrderAcrossAssignedAgents(t *testing.T) {
	agents := []tuirun.AgentSnapshot{
		{ID: "agent-a", ADBSerial: "FAKE123456789", DeviceModel: "Pixel 7a"},
		{ID: "agent-b", ADBSerial: "FAKE987654321", DeviceModel: "Pixel 9"},
	}
	state := New([]tuirun.Target{
		{Name: "n1(5G)", Agent: "FAKE123456789", SSID: "Lab"},
		{Name: "n1(6G)", Agent: "FAKE987654321", SSID: "Lab"},
		{Name: "n2(5G)", Agent: "FAKE123456789", SSID: "Lab"},
		{Name: "n2(6G)", Agent: "FAKE987654321", SSID: "Lab"},
	}, []tuirun.CheckInfo{}, agents, time.Now())

	var got []string
	for _, target := range state.CheckStatusTargets() {
		got = append(got, target.Name)
	}
	want := []string{"n1(5G)", "n1(6G)", "n2(5G)", "n2(6G)"}
	if len(got) != len(want) {
		t.Fatalf("target order = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("target order = %#v, want %#v", got, want)
		}
	}
}
