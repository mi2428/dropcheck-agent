package app

import (
	"strings"
	"testing"
	"time"

	"dropcheck/controller/internal/control"
	"dropcheck/controller/internal/controlpb"
	"dropcheck/controller/internal/watch"
)

func TestWatchSessionLogEventFormatsWarnForTUI(t *testing.T) {
	at := time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC)
	event, ok := watchSessionLogEvent(control.LogEvent{
		AgentID:   "agent-a",
		SessionID: "session-a",
		CommandID: "cmd-1",
		Level:     controlpb.CommandLog_LEVEL_WARN,
		Message:   "traceroute binary not available",
		Time:      at,
	})
	if !ok {
		t.Fatal("watchSessionLogEvent filtered warn log")
	}
	if event.Kind != watch.EventLog || !event.Time.Equal(at) {
		t.Fatalf("event metadata = %#v", event)
	}
	if event.Agent.ID != "agent-a" || event.Agent.SessionID != "session-a" {
		t.Fatalf("agent snapshot = %#v", event.Agent)
	}
	if want := "[warn agent=agent-a command=cmd-1] traceroute binary not available"; !strings.Contains(event.Message, want) {
		t.Fatalf("message = %q, want %q", event.Message, want)
	}
}

func TestWatchSessionLogEventFiltersDebugForTUI(t *testing.T) {
	_, ok := watchSessionLogEvent(control.LogEvent{
		AgentID: "agent-a",
		Level:   controlpb.CommandLog_LEVEL_DEBUG,
		Message: "debug detail",
	})
	if ok {
		t.Fatal("debug log should be filtered")
	}
}

func TestWatchAgentPlansAssignTargetsBySerial(t *testing.T) {
	agents := []control.AgentInfo{
		{
			ID: "agent-a",
			Hello: &controlpb.AgentHello{
				AdbSerial: "35251JEHN00258",
				Device:    &controlpb.DeviceInfo{Model: "Pixel 7a"},
			},
		},
		{
			ID: "agent-b",
			Hello: &controlpb.AgentHello{
				AdbSerial: "45240DLAQ007HG",
				Device:    &controlpb.DeviceInfo{Model: "Pixel 9"},
			},
		},
	}
	plan := watch.Plan{
		Name: "assigned-watch",
		Targets: []watch.Target{
			{Name: "ap-5g", Agent: "35251JEHN00258", SSID: "Lab", Band: "5ghz"},
			{Name: "ap-6g", Agent: "45240DLAQ007HG", SSID: "Lab", Band: "6ghz"},
			{Name: "shared", SSID: "Lab"},
		},
		Checks: []watch.Check{{Name: "connect", Type: "connect"}},
	}

	agentPlans, uiPlan, err := watchAgentPlans(plan, agents)
	if err != nil {
		t.Fatalf("watchAgentPlans() error = %v", err)
	}
	if len(agentPlans) != 2 {
		t.Fatalf("agentPlans len = %d, want 2", len(agentPlans))
	}
	if got := targetNames(agentPlans[0].Plan.Targets); strings.Join(got, ",") != "ap-5g,shared" {
		t.Fatalf("pixel7a targets = %#v", got)
	}
	if got := targetNames(agentPlans[1].Plan.Targets); strings.Join(got, ",") != "ap-6g,shared" {
		t.Fatalf("pixel9 targets = %#v", got)
	}
	if uiPlan.Targets[0].Agent != "35251JEHN00258" || uiPlan.Targets[1].Agent != "45240DLAQ007HG" || uiPlan.Targets[2].Agent != "" {
		t.Fatalf("ui target agents = %#v", uiPlan.Targets)
	}
}

func TestWatchAgentPlansRejectDeviceModelAgentSelector(t *testing.T) {
	agents := []control.AgentInfo{
		{
			ID: "agent-a",
			Hello: &controlpb.AgentHello{
				AdbSerial: "35251JEHN00258",
				Device:    &controlpb.DeviceInfo{Model: "Pixel 7a"},
			},
		},
	}
	plan := watch.Plan{
		Name: "assigned-watch",
		Targets: []watch.Target{
			{Name: "ap-5g", Agent: "Pixel 7a", SSID: "Lab", Band: "5ghz"},
		},
		Checks: []watch.Check{{Name: "connect", Type: "connect"}},
	}

	if _, _, err := watchAgentPlans(plan, agents); err == nil || !strings.Contains(err.Error(), "agent serial") {
		t.Fatalf("watchAgentPlans() error = %v, want agent serial error", err)
	}
}

func targetNames(targets []watch.Target) []string {
	names := make([]string, 0, len(targets))
	for _, target := range targets {
		names = append(names, target.Name)
	}
	return names
}
