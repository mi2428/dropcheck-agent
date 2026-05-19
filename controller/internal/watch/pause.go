package watch

import (
	"context"
	"sync"
)

// PauseController coordinates operator-requested watch pauses across one or
// more agent runners. A pause takes effect at operation boundaries.
type PauseController struct {
	mu      sync.RWMutex
	paused  bool
	resumed chan struct{}
}

// NewPauseController constructs an unpaused controller.
func NewPauseController() *PauseController {
	return &PauseController{}
}

// Pause blocks future operation-boundary gates until Resume is called.
func (p *PauseController) Pause() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.paused {
		return
	}
	p.paused = true
	p.resumed = make(chan struct{})
}

// Resume releases all goroutines waiting at a pause gate.
func (p *PauseController) Resume() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.paused {
		return
	}
	close(p.resumed)
	p.resumed = nil
	p.paused = false
}

// Paused reports the current pause state.
func (p *PauseController) Paused() bool {
	if p == nil {
		return false
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.paused
}

// Wait blocks while the controller is paused or until ctx is canceled.
func (p *PauseController) Wait(ctx context.Context) error {
	if p == nil {
		return nil
	}
	for {
		p.mu.RLock()
		paused := p.paused
		resumed := p.resumed
		p.mu.RUnlock()
		if !paused {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-resumed:
		}
	}
}
