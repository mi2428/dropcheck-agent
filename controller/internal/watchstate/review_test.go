package watchstate

import (
	"github.com/mi2428/dropcheck-agent/controller/internal/watchrun"
	"testing"
	"time"
)

func TestReviewAggregateRoundStartResetsEveryAgent(t *testing.T) {
	agents := []watchrun.AgentSnapshot{{ID: "a"}, {ID: "b"}}
	target := watchrun.Target{ID: "target/0", Name: "lab", SSID: "Lab"}
	s := New([]watchrun.Target{target}, nil, agents, time.Now())
	for _, agent := range agents {
		s.Apply(watchrun.Event{Kind: watchrun.EventStepStarted, Round: 1, Agent: agent, Target: watchrun.TargetSnapshot{ID: target.ID, Name: target.Name}, Step: watchrun.StepSnapshot{ID: "wait_connected", Name: "wait_connected", Status: "running"}})
		s.Apply(watchrun.Event{Kind: watchrun.EventStepFinished, Round: 1, Agent: agent, Target: watchrun.TargetSnapshot{ID: target.ID, Name: target.Name}, Step: watchrun.StepSnapshot{ID: "wait_connected", Name: "wait_connected", Status: "ok"}})
	}
	s.Apply(watchrun.Event{Kind: watchrun.EventRoundStarted, Round: 2})
	for _, state := range s.Targets {
		if len(state.Steps) != 0 || state.CurrentStep != "" || state.CurrentStepID != "" || state.Status != "pending" {
			t.Fatalf("aggregate start retained current state: %+v", state)
		}
	}
	for _, agent := range agents {
		result := s.CheckStatusAgentResult(agent, watchrun.TargetSnapshot{ID: target.ID}, "wait_connected")
		if result.Status == "ok" && !result.Stale {
			t.Fatalf("old success is current for %s", agent.ID)
		}
	}
}
