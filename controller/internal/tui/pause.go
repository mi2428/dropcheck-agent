package tui

import "dropcheck/controller/internal/harness"

func (m *model) pause() {
	if m.controls != nil {
		if err := m.controls.Pause(harness.Scope{Kind: harness.ScopeRun}); err != nil {
			m.pushLog(err.Error())
			return
		}
		m.State.Phase = "pausing"
	}
}

func (m *model) resume() {
	m.paused = false
	if m.controls != nil {
		if err := m.controls.Resume(harness.Scope{Kind: harness.ScopeRun}); err != nil {
			m.pushLog(err.Error())
		}
	}
}

func (m *model) skipCurrent() {
	if m.controls != nil {
		if err := m.controls.Skip(harness.Scope{Kind: harness.ScopeRun}); err != nil {
			m.pushLog(err.Error())
		}
	}
}
