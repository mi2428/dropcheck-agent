package app

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/mi2428/dropcheck-agent/controller/internal/control"
	"github.com/mi2428/dropcheck-agent/controller/internal/controlpb"
	"github.com/mi2428/dropcheck-agent/controller/internal/tuirun"
)

func TestTUITargetsDefaultToAllConnectedAgents(t *testing.T) {
	state, _, stopFirst := connectedShellStateWithStream(t)
	defer stopFirst()
	_, stopSecond := connectAdditionalTestAgent(t, state.server, "session-b", "agent-b", "serial-b", 35)
	defer stopSecond()
	for _, target := range []string{"", "all", "agent-b", "serial-b", "#1"} {
		agents, err := tuiTargetAgents(state.server, target)
		want := 1
		if target == "" || target == "all" {
			want = 2
		}
		if err != nil || len(agents) != want || target == "serial-b" && agents[0].ID != "agent-b" {
			t.Fatalf("tui target %q: agents=%v err=%v", target, agents, err)
		}
	}
	for _, target := range []string{"missing", "#0", "#3"} {
		if _, err := tuiTargetAgents(state.server, target); err == nil {
			t.Fatalf("unavailable target %q was accepted", target)
		}
	}
	if _, err := tuiTargetAgents(control.NewServer("", nil), ""); err == nil {
		t.Fatal("empty agent inventory accepted")
	}
}

func TestTUISessionLogEventFormatsWarnForTUI(t *testing.T) {
	at := time.Date(2026, 5, 16, 9, 30, 0, 0, time.UTC)
	event, ok := tuiSessionLogEvent(control.LogEvent{
		AgentID:   "agent-a",
		SessionID: "session-a",
		CommandID: "cmd-1",
		Level:     controlpb.CommandLog_LEVEL_WARN,
		Message:   "traceroute binary not available",
		Time:      at,
	})
	if !ok {
		t.Fatal("tuiSessionLogEvent filtered warn log")
	}
	if event.Kind != tuirun.EventLog || !event.Time.Equal(at) {
		t.Fatalf("event metadata = %#v", event)
	}
	if event.Agent.ID != "agent-a" || event.Agent.SessionID != "session-a" {
		t.Fatalf("agent snapshot = %#v", event.Agent)
	}
	if want := "[warn agent=agent-a command=cmd-1] traceroute binary not available"; !strings.Contains(event.Message, want) {
		t.Fatalf("message = %q, want %q", event.Message, want)
	}
}

func TestPrintTUINoTUIEventPrintsRequiredStepFailure(t *testing.T) {
	var out bytes.Buffer
	printTUINoTUIEvent(&out, tuirun.Event{
		Time:  time.Date(2026, 5, 19, 10, 11, 12, 0, time.UTC),
		Kind:  tuirun.EventFinding,
		Round: 4,
		Agent: tuirun.AgentSnapshot{ADBSerial: "R5CT12345"},
		Target: tuirun.TargetSnapshot{
			Name: "lab-6g",
			SSID: "Lab",
		},
		Step: tuirun.StepSnapshot{
			Name:    "wait_connected",
			Type:    "wait_connected",
			Status:  "failed",
			Message: "wifi condition timeout",
		},
		Status:  "failed",
		Finding: &tuirun.Finding{Target: "lab-6g", Check: "wait_connected", Metric: "status", Observed: "failed", Expected: "== ok", Message: "wifi condition timeout"},
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

func TestPrintTUINoTUIEventIgnoresPassingStep(t *testing.T) {
	var out bytes.Buffer
	printTUINoTUIEvent(&out, tuirun.Event{
		Kind:   tuirun.EventStepFinished,
		Step:   tuirun.StepSnapshot{Name: "ping", Type: "ping", Status: "ok"},
		Status: "ok",
	}, false)

	if out.Len() != 0 {
		t.Fatalf("no-tui output = %q, want empty", out.String())
	}
}

func TestPrintTUINoTUIEventSanitizesOnlyPresentation(t *testing.T) {
	for _, tc := range []struct {
		metric, value, want, forbidden string
	}{
		{"ssid", "Guest\x1b[2J\nforged-line", "Guest forged-line", "\x1b"},
		{"ssid", "password=Guest", "password=Guest", "observed=<redacted>"},
		{"bssid", "aa:bb:cc:dd:ee:ff", "aa:bb:cc:dd:ee:ff", "observed=<redacted>"},
		{"transport", "password=synthetic-only\nforged-line", "password=<redacted>", "synthetic-only"},
	} {
		finding := tuirun.Finding{Target: "target\nname", Check: "check\x1b[2J", Metric: tc.metric, Observed: tc.value, Expected: tc.value}
		event := tuirun.Event{Kind: tuirun.EventFinding, Agent: tuirun.AgentSnapshot{Name: "model\npassword=synthetic-agent"}, Finding: &finding}
		var out bytes.Buffer
		printTUINoTUIEvent(&out, event, true)
		text := out.String()
		if strings.Contains(text, tc.forbidden) || strings.ContainsAny(text, "\x1b\r") || strings.Count(text, "\n") != 1 || strings.Contains(text, "synthetic-agent") || !strings.Contains(text, tc.want) {
			t.Fatalf("unsafe or corrupted presentation: %q", text)
		}
		if finding.Observed != tc.value || finding.Expected != tc.value || finding.Target != "target\nname" {
			t.Fatal("presentation changed the original finding")
		}
	}
}

func TestTUISessionLogEventFiltersDebugForTUI(t *testing.T) {
	_, ok := tuiSessionLogEvent(control.LogEvent{
		AgentID: "agent-a",
		Level:   controlpb.CommandLog_LEVEL_DEBUG,
		Message: "debug detail",
	})
	if ok {
		t.Fatal("debug log should be filtered")
	}
}

func TestTUIAgentPlansAssignTargetsBySerial(t *testing.T) {
	agents := []control.AgentInfo{
		{
			ID: "agent-a",
			Hello: &controlpb.AgentHello{
				AdbSerial: "FAKE123456789",
				Device:    &controlpb.DeviceInfo{Model: "Pixel 7a"},
			},
		},
		{
			ID: "agent-b",
			Hello: &controlpb.AgentHello{
				AdbSerial: "FAKE987654321",
				Device:    &controlpb.DeviceInfo{Model: "Pixel 9"},
			},
		},
	}
	plan := tuirun.Plan{
		Name: "assigned-tui",
		Networks: []tuirun.Network{
			{Name: "ap-5g", Agent: "FAKE123456789", SSID: "Lab", PSK: tuirun.SecretValue("synthetic-passphrase"), Band: "5ghz", WaitConnected: true, RequireIP: true, DisconnectAfter: true},
			{Name: "ap-6g", Agent: "FAKE987654321", SSID: "Lab", PSK: tuirun.SecretValue("synthetic-passphrase"), Band: "6ghz", WaitConnected: true, RequireIP: true, DisconnectAfter: true},
			{Name: "shared", SSID: "Lab", PSK: tuirun.SecretValue("synthetic-passphrase"), WaitConnected: true, RequireIP: true, DisconnectAfter: true},
		},
	}

	compiled, err := tuirun.Compile(plan, agents)
	if err != nil {
		t.Fatalf("tuiAgentPlans() error = %v", err)
	}
	uiPlan := compiled.Preview()
	if uiPlan.Targets[0].Agent != "FAKE123456789" || uiPlan.Targets[1].Agent != "FAKE987654321" || uiPlan.Targets[2].Agent != "" {
		t.Fatalf("ui target agents = %#v", uiPlan.Targets)
	}
}

func TestTUIAgentPlansRejectDeviceModelAgentSelector(t *testing.T) {
	agents := []control.AgentInfo{
		{
			ID: "agent-a",
			Hello: &controlpb.AgentHello{
				AdbSerial: "FAKE123456789",
				Device:    &controlpb.DeviceInfo{Model: "Pixel 7a"},
			},
		},
	}
	plan := tuirun.Plan{
		Name: "assigned-tui",
		Networks: []tuirun.Network{
			{Name: "ap-5g", Agent: "Pixel 7a", SSID: "Lab", PSK: tuirun.SecretValue("synthetic-passphrase"), Band: "5ghz", WaitConnected: true, RequireIP: true, DisconnectAfter: true},
		},
	}

	if _, err := tuirun.Compile(plan, agents); err == nil {
		t.Fatalf("tuiAgentPlans() error = %v, want agent serial error", err)
	}
}
