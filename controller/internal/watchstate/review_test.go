package watchstate

import (
	"dropcheck/controller/internal/harness"
	"testing"
	"time"
)

func TestReviewAggregateRoundStartResetsEveryAgent(t *testing.T) {
	agents := []harness.AgentSnapshot{{ID: "a"}, {ID: "b"}}
	target := harness.Target{ID: "target/0", Name: "lab", SSID: "Lab"}
	s := New([]harness.Target{target}, nil, agents, time.Now())
	for _, agent := range agents {
		s.Apply(harness.Event{Kind: harness.EventStepStarted, Round: 1, Agent: agent, Target: harness.TargetSnapshot{ID: target.ID, Name: target.Name}, Step: harness.StepSnapshot{ID: "wait_connected", Name: "wait_connected", Status: "running"}})
		s.Apply(harness.Event{Kind: harness.EventStepFinished, Round: 1, Agent: agent, Target: harness.TargetSnapshot{ID: target.ID, Name: target.Name}, Step: harness.StepSnapshot{ID: "wait_connected", Name: "wait_connected", Status: "ok"}})
	}
	s.Apply(harness.Event{Kind: harness.EventRoundStarted, Round: 2})
	for _, state := range s.Targets {
		if len(state.Steps) != 0 || state.CurrentStep != "" || state.CurrentStepID != "" || state.Status != "pending" {
			t.Fatalf("aggregate start retained current state: %+v", state)
		}
	}
	for _, agent := range agents {
		result := s.CheckStatusAgentResult(agent, harness.TargetSnapshot{ID: target.ID}, "wait_connected")
		if result.Status == "ok" && !result.Stale {
			t.Fatalf("old success is current for %s", agent.ID)
		}
	}
}
