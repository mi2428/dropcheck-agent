package tui

import (
	"fmt"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/mi2428/dropcheck-agent/controller/internal/tuirun"
)

func TestHistoryCacheDoesNotMutateRowsWhenFiltering(t *testing.T) {
	m := frameHistoryModel(16)
	m.focus = focusPassingChecks
	m.passingSearchQuery = "ping-0"
	if rows := m.filteredPassingCheckSummaries(); len(rows) != 1 || rows[0].Count != 2 {
		t.Fatalf("filtered rows=%+v", rows)
	}
	m.passingSearchQuery = ""
	if rows := m.filteredPassingCheckSummaries(); len(rows) != 8 {
		t.Fatalf("filter mutated the unfiltered rows: %+v", rows)
	}
	before := m.frame
	m.apply(tuirun.Event{Kind: tuirun.EventStepFinished, Round: 3, Time: m.Now,
		Agent: m.Agents[0], Target: m.Targets[0].Target,
		Step: tuirun.StepSnapshot{ID: "check/0", Name: "ping-0", Type: "ping", Status: "ok"}, Status: "ok"})
	if m.frame == before {
		t.Fatal("history mutation reused stale derived rows")
	}
	m.passingSearchQuery = "ping-0"
	if rows := m.filteredPassingCheckSummaries(); len(rows) != 1 || rows[0].Count != 3 {
		t.Fatalf("history update was not reflected: %+v", rows)
	}
}

func TestHistoryCacheSurvivesRepaintAndClockButNotNewHistory(t *testing.T) {
	m := frameHistoryModel(2000)
	_ = m.View()
	before := m.frame
	passing := &m.frame.passing[0]
	_ = m.View()
	if &m.frame.passing[0] != passing {
		t.Fatal("repaint rebuilt unchanged passing summaries")
	}
	updated, _ := m.Update(tickMsg(m.Now.Add(time.Second)))
	m = updated.(model)
	if m.frame != before || !m.frame.passingReady || m.frame.hotspotsReady || m.frame.causesReady {
		t.Fatal("clock tick did not retain timeless rows and expire time-window summaries")
	}
	m.apply(tuirun.Event{Kind: tuirun.EventLog, Time: m.Now, Message: "synthetic log"})
	if m.frame != before || !m.frame.passingReady {
		t.Fatal("log event rebuilt unrelated outcome history")
	}
	m.apply(tuirun.Event{Kind: tuirun.EventFinding, Time: m.Now, Round: 4,
		Agent: m.Agents[0], Target: m.Targets[0].Target,
		Step:    tuirun.StepSnapshot{ID: "check/0", Name: "ping-0", Type: "ping"},
		Finding: &tuirun.Finding{CheckID: "check/0", Check: "ping-0", Metric: "status", Observed: "failed"}})
	if m.frame == before {
		t.Fatal("new finding did not invalidate derived history")
	}
}

func frameHistoryModel(n int) model {
	now := time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
	agent := tuirun.AgentSnapshot{ID: "frame-agent", DeviceModel: "synthetic"}
	target := tuirun.Target{ID: "target/0", Name: "synthetic", SSID: "synthetic"}
	checks := make([]tuirun.CheckInfo, 8)
	for i := range checks {
		checks[i] = tuirun.CheckInfo{ID: fmt.Sprintf("check/%d", i), Name: fmt.Sprintf("ping-%d", i), Type: "ping"}
	}
	m := newModelWithChecks("frame", []tuirun.Target{target}, checks, nil, []tuirun.AgentSnapshot{agent})
	m.width, m.height, m.Now, m.Round = 160, 48, now, 1
	snapshot := m.Targets[0].Target
	m.PassingChecks = make([]passingCheckState, n)
	for i := range m.PassingChecks {
		check := checks[i%len(checks)]
		m.PassingChecks[i] = passingCheckState{Round: uint64(i/8 + 1), When: now.Add(-time.Duration(n-i) * time.Millisecond), Agent: agent, Target: snapshot,
			Step: tuirun.StepSnapshot{ID: check.ID, Name: check.Name, Type: check.Type, Status: "ok"}, Duration: 10, DurationKnown: true}
	}
	m.FailedChecks = make([]failedCheckState, n/10)
	for i := range m.FailedChecks {
		check := checks[i%len(checks)]
		m.FailedChecks[i] = failedCheckState{Round: uint64(i/8 + 1), When: now.Add(-time.Duration(n-i) * time.Millisecond), Agent: agent, Target: snapshot,
			Finding: tuirun.Finding{TargetID: snapshot.ID, CheckID: check.ID, Target: snapshot.Name, Check: check.Name, Metric: "status", Observed: "failed", Expected: "ok"}}
	}
	return m
}

func BenchmarkTUIFrame(b *testing.B) {
	for _, n := range []int{2000, 20000} {
		b.Run(fmt.Sprintf("Cold/%d", n), func(b *testing.B) {
			m := frameHistoryModel(n)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				m.frame = &frameData{}
				_ = m.View()
			}
		})
		b.Run(fmt.Sprintf("Repaint/%d", n), func(b *testing.B) {
			m := frameHistoryModel(n)
			_ = m.View()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = m.View()
			}
		})
		b.Run(fmt.Sprintf("Tab/%d", n), func(b *testing.B) {
			m := frameHistoryModel(n)
			m.normalizeCursors()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyTab}))
			}
		})
	}
}
