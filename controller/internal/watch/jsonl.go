package watch

import (
	"context"
	"encoding/json"
	"io"
	"sync"
)

// JSONLWriter writes watch events as newline-delimited JSON.
type JSONLWriter struct {
	mu sync.Mutex
	w  io.Writer
}

// NewJSONLWriter returns a sink that writes one JSON event per line to w.
func NewJSONLWriter(w io.Writer) *JSONLWriter {
	return &JSONLWriter{w: w}
}

// Emit writes event as one JSON line.
func (w *JSONLWriter) Emit(_ context.Context, event Event) error {
	if w == nil || w.w == nil {
		return nil
	}
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, err := w.w.Write(append(data, '\n')); err != nil {
		return err
	}
	if flusher, ok := w.w.(interface{ Sync() error }); ok {
		return flusher.Sync()
	}
	return nil
}

// MultiSink emits each event to multiple sinks in order.
type MultiSink []Sink

// Emit sends event to every non-nil sink and stops at the first error.
func (sinks MultiSink) Emit(ctx context.Context, event Event) error {
	for _, sink := range sinks {
		if sink == nil {
			continue
		}
		if err := sink.Emit(ctx, event); err != nil {
			return err
		}
	}
	return nil
}

// ChannelSink sends watch events to a channel until the context is canceled.
type ChannelSink struct {
	C chan<- Event
}

// Emit sends event to C or returns ctx.Err when ctx is canceled first.
func (s ChannelSink) Emit(ctx context.Context, event Event) error {
	if s.C == nil {
		return nil
	}
	select {
	case s.C <- event:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
