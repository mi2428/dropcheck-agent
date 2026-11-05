package app

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/mi2428/dropcheck-agent/controller/internal/control"
	"github.com/mi2428/dropcheck-agent/controller/internal/controlpb"
	"github.com/mi2428/dropcheck-agent/controller/internal/watchrun"
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
	if event.Kind != watchrun.EventLog || !event.Time.Equal(at) {
		t.Fatalf("event metadata = %#v", event)
	}
	if event.Agent.ID != "agent-a" || event.Agent.SessionID != "session-a" {
		t.Fatalf("agent snapshot = %#v", event.Agent)
	}
	if want := "[warn agent=agent-a command=cmd-1] traceroute binary not available"; !strings.Contains(event.Message, want) {
		t.Fatalf("message = %q, want %q", event.Message, want)
	}
}

func TestPrintWatchNoTUIEventPrintsRequiredStepFailure(t *testing.T) {
	var out bytes.Buffer
	printWatchNoTUIEvent(&out, watchrun.Event{
		Time:  time.Date(2026, 5, 19, 10, 11, 12, 0, time.UTC),
		Kind:  watchrun.EventFinding,
		Round: 4,
		Agent: watchrun.AgentSnapshot{ADBSerial: "R5CT12345"},
		Target: watchrun.TargetSnapshot{
			Name: "lab-6g",
			SSID: "Lab",
		},
		Step: watchrun.StepSnapshot{
			Name:    "wait_connected",
			Type:    "wait_connected",
			Status:  "failed",
			Message: "wifi condition timeout",
		},
		Status:  "failed",
		Finding: &watchrun.Finding{Target: "lab-6g", Check: "wait_connected", Metric: "status", Observed: "failed", Expected: "== ok", Message: "wifi condition timeout"},
	}, true)

	got := out.String()
	for _, want := range []string{
		"10:11:12 agent=R5CT12345 round=4",
		"target=lab-6g",
		"check=wait_connected",
		"metric=status",
		"observed=failed",
		"expected=== ok",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("no-tui output = %q, missing %q", got, want)
		}
	}
}

func TestPrintWatchNoTUIEventIgnoresPassingStep(t *testing.T) {
	var out bytes.Buffer
	printWatchNoTUIEvent(&out, watchrun.Event{
		Kind:   watchrun.EventStepFinished,
		Step:   watchrun.StepSnapshot{Name: "ping", Type: "ping", Status: "ok"},
		Status: "ok",
	}, false)

	if out.Len() != 0 {
		t.Fatalf("no-tui output = %q, want empty", out.String())
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
	plan := watchrun.Plan{
		Name: "assigned-watch",
		Networks: []watchrun.Network{
			{Name: "ap-5g", Agent: "35251JEHN00258", SSID: "Lab", PSK: watchrun.SecretValue("synthetic-passphrase"), Band: "5ghz", WaitConnected: true, RequireIP: true, DisconnectAfter: true},
			{Name: "ap-6g", Agent: "45240DLAQ007HG", SSID: "Lab", PSK: watchrun.SecretValue("synthetic-passphrase"), Band: "6ghz", WaitConnected: true, RequireIP: true, DisconnectAfter: true},
			{Name: "shared", SSID: "Lab", PSK: watchrun.SecretValue("synthetic-passphrase"), WaitConnected: true, RequireIP: true, DisconnectAfter: true},
		},
	}

	compiled, err := watchrun.Compile(plan, agents)
	if err != nil {
		t.Fatalf("watchAgentPlans() error = %v", err)
	}
	uiPlan := compiled.Preview()
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
	plan := watchrun.Plan{
		Name: "assigned-watch",
		Networks: []watchrun.Network{
			{Name: "ap-5g", Agent: "Pixel 7a", SSID: "Lab", PSK: watchrun.SecretValue("synthetic-passphrase"), Band: "5ghz", WaitConnected: true, RequireIP: true, DisconnectAfter: true},
		},
	}

	if _, err := watchrun.Compile(plan, agents); err == nil {
		t.Fatalf("watchAgentPlans() error = %v, want agent serial error", err)
	}
}
