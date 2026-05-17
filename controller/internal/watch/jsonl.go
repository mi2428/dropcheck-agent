package watch

import (
	"context"
	"encoding/json"
	"io"
	"sync"
)

type JSONLWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func NewJSONLWriter(w io.Writer) *JSONLWriter {
	return &JSONLWriter{w: w}
}

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

type MultiSink []Sink

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

type ChannelSink struct {
	C chan<- Event
}

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
