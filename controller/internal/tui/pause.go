package tui

func (m *model) pause() {
	m.paused = true
	if m.pauseControl != nil {
		m.pauseControl.Pause()
	}
}

func (m *model) resume() {
	m.paused = false
	if m.pauseControl != nil {
		m.pauseControl.Resume()
	}
}

func (m *model) skipCurrent() {
	if m.skipControl != nil {
		m.skipControl.Skip()
	}
}
