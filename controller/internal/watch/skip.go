package watch

import (
	"context"
	"errors"
	"sync"
)

// ErrSkipRequested marks an operator-requested skip of the currently running
// watch operation.
var ErrSkipRequested = errors.New("watch operation skipped by operator")

// SkipController coordinates operator-requested skips across active watch
// operation contexts.
type SkipController struct {
	mu       sync.Mutex
	nextID   uint64
	requests uint64
	active   map[uint64]context.CancelCauseFunc
}

// NewSkipController constructs a controller with no pending skip requests.
func NewSkipController() *SkipController {
	return &SkipController{}
}

// Skip asks all currently active watch operations to stop.
func (s *SkipController) Skip() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests++
	for _, cancel := range s.active {
		cancel(ErrSkipRequested)
	}
}

// Requests reports how many skip requests have been made.
func (s *SkipController) Requests() uint64 {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.requests
}

func (s *SkipController) operationContext(ctx context.Context) (context.Context, func()) {
	if s == nil {
		return ctx, func() {}
	}
	opCtx, cancel := context.WithCancelCause(ctx)
	s.mu.Lock()
	if s.active == nil {
		s.active = map[uint64]context.CancelCauseFunc{}
	}
	id := s.nextID
	s.nextID++
	s.active[id] = cancel
	s.mu.Unlock()
	return opCtx, func() {
		s.mu.Lock()
		delete(s.active, id)
		s.mu.Unlock()
		cancel(nil)
	}
}

func operationSkipped(ctx context.Context) bool {
	return errors.Is(context.Cause(ctx), ErrSkipRequested)
}
