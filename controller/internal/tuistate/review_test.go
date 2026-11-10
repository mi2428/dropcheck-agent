package tuistate

import (
	"github.com/mi2428/dropcheck-agent/controller/internal/tuirun"
	"testing"
	"time"
)

func TestReviewAggregateRoundStartResetsEveryAgent(t *testing.T) {
	agents := []tuirun.AgentSnapshot{{ID: "a"}, {ID: "b"}}
	target := tuirun.Target{ID: "target/0", Name: "lab", SSID: "Lab"}
	s := New([]tuirun.Target{target}, nil, agents, time.Now())
	for _, agent := range agents {
		s.Apply(tuirun.Event{Kind: tuirun.EventStepStarted, Round: 1, Agent: agent, Target: tuirun.TargetSnapshot{ID: target.ID, Name: target.Name}, Step: tuirun.StepSnapshot{ID: "wait_connected", Name: "wait_connected", Status: "running"}})
		s.Apply(tuirun.Event{Kind: tuirun.EventStepFinished, Round: 1, Agent: agent, Target: tuirun.TargetSnapshot{ID: target.ID, Name: target.Name}, Step: tuirun.StepSnapshot{ID: "wait_connected", Name: "wait_connected", Status: "ok"}})
	}
	s.Apply(tuirun.Event{Kind: tuirun.EventRoundStarted, Round: 2})
	for _, state := range s.Targets {
		if len(state.Steps) != 0 || state.CurrentStep != "" || state.CurrentStepID != "" || state.Status != "pending" {
			t.Fatalf("aggregate start retained current state: %+v", state)
		}
	}
	for _, agent := range agents {
		result := s.CheckStatusAgentResult(agent, tuirun.TargetSnapshot{ID: target.ID}, "wait_connected")
		if result.Status == "ok" && !result.Stale {
			t.Fatalf("old success is current for %s", agent.ID)
		}
	}
}
