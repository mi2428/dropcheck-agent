package tui

import "dropcheck/controller/internal/watchrun"

func (m *model) pause() {
	if m.controls != nil {
		if err := m.controls.Pause(watchrun.Scope{Kind: watchrun.ScopeRun}); err != nil {
			m.pushLog(err.Error())
			return
		}
		m.State.Phase = "pausing"
	}
}

func (m *model) resume() {
	m.paused = false
	if m.controls != nil {
		if err := m.controls.Resume(watchrun.Scope{Kind: watchrun.ScopeRun}); err != nil {
			m.pushLog(err.Error())
		}
	}
}

func (m *model) skipCurrent() {
	if m.controls != nil {
		if err := m.controls.Skip(watchrun.Scope{Kind: watchrun.ScopeRun}); err != nil {
			m.pushLog(err.Error())
		}
	}
}
