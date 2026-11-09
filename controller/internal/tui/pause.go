package tui

import "github.com/mi2428/dropcheck-agent/controller/internal/tuirun"

func (m *model) pause() {
	if m.controls != nil {
		if err := m.controls.Pause(tuirun.Scope{Kind: tuirun.ScopeRun}); err != nil {
			m.pushLog(err.Error())
			return
		}
		m.State.Phase = "pausing"
	}
}

func (m *model) resume() {
	m.paused = false
	if m.controls != nil {
		if err := m.controls.Resume(tuirun.Scope{Kind: tuirun.ScopeRun}); err != nil {
			m.pushLog(err.Error())
		}
	}
}

func (m *model) skipCurrent() {
	if m.controls != nil {
		if err := m.controls.Skip(tuirun.Scope{Kind: tuirun.ScopeRun}); err != nil {
			m.pushLog(err.Error())
		}
	}
}
