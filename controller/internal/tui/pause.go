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
