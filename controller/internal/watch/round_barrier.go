package watch

import (
	"context"
	"sync"
)

// RoundBarrier synchronizes multiple agent runners at the end of each round.
type RoundBarrier struct {
	parties int
	mu      sync.Mutex
	arrived int
	wait    chan struct{}
}

// NewRoundBarrier constructs a reusable round barrier for parties runners.
func NewRoundBarrier(parties int) *RoundBarrier {
	return &RoundBarrier{parties: parties, wait: make(chan struct{})}
}

// Wait blocks until all parties have reached the same round boundary.
func (b *RoundBarrier) Wait(ctx context.Context) error {
	if b == nil || b.parties <= 1 {
		return nil
	}
	b.mu.Lock()
	ch := b.wait
	b.arrived++
	if b.arrived >= b.parties {
		close(ch)
		b.arrived = 0
		b.wait = make(chan struct{})
		b.mu.Unlock()
		return nil
	}
	b.mu.Unlock()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-ch:
		return nil
	}
}
