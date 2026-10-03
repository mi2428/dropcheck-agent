package watch

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
)

const jsonlQueueSize = 16

var errJSONLClosed = errors.New("JSONL writer is closed")

type jsonlRequest struct {
	data []byte
	ack  chan error
}

// JSONLWriter serializes watch events through one worker.
// Close must be called to drain accepted events and release the worker.
type JSONLWriter struct {
	admission chan struct{}
	requests  chan jsonlRequest
	available chan struct{}
	stop      chan struct{}
	failed    chan struct{}
	done      chan struct{}
	closed    bool
	writeErr  error // Published once by closing failed.
	closeErr  error // Published once by closing done.
}

// NewJSONLWriter borrows w; the caller remains responsible for closing it.
// Each successful Emit acknowledges both Write and optional Sync.
func NewJSONLWriter(w io.Writer) *JSONLWriter {
	return newJSONLWriter(w, nil)
}

// OpenJSONLFile opens an append-only JSONL file owned by the returned sink.
// Unlike NewJSONLWriter, its Close also closes the file after draining writes.
func OpenJSONLFile(path string) (*JSONLWriter, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	return newJSONLWriter(file, file.Close), nil
}

func newJSONLWriter(w io.Writer, closeOutput func() error) *JSONLWriter {
	sink := &JSONLWriter{
		admission: make(chan struct{}, 1),
		requests:  make(chan jsonlRequest, jsonlQueueSize),
		available: make(chan struct{}, 1),
		stop:      make(chan struct{}),
		failed:    make(chan struct{}),
		done:      make(chan struct{}),
	}
	sink.admission <- struct{}{}
	if w == nil {
		sink.writeErr = errors.New("JSONL writer is nil")
		sink.closed = true
		close(sink.stop)
		close(sink.failed)
		close(sink.done)
		return sink
	}
	go sink.writeLoop(w, closeOutput)
	return sink
}

// Emit waits for durable delivery or ctx. An already accepted event is not
// discarded when ctx expires; Close drains it, while Emit reports the failure.
func (w *JSONLWriter) Emit(ctx context.Context, event Event) error {
	if w == nil || w.requests == nil {
		return errors.New("JSONL writer is not initialized")
	}
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	request := jsonlRequest{data: append(data, '\n'), ack: make(chan error, 1)}
	for {
		if err := ctx.Err(); err != nil {
			return w.contextError(ctx)
		}
		if err := w.lockAdmission(ctx); err != nil {
			return errors.Join(err, w.currentError())
		}
		if err := w.currentError(); err != nil {
			w.unlockAdmission()
			return err
		}
		if w.closed {
			w.unlockAdmission()
			return errJSONLClosed
		}
		select {
		case w.requests <- request:
			w.unlockAdmission()
			select {
			case err := <-request.ack:
				return err
			case <-ctx.Done():
				return w.contextError(ctx)
			}
		default:
			w.unlockAdmission()
		}
		select {
		case <-w.available:
		case <-w.stop:
		case <-w.failed:
		case <-ctx.Done():
			return w.contextError(ctx)
		}
	}
}

// Close stops admission and waits for accepted writes, Sync, and the worker.
// It closes only a file created by OpenJSONLFile, never a borrowed writer.
// Go cannot interrupt arbitrary kernel I/O: on deadline, this returns an error
// and the single worker (and owned file, if any) remain until that I/O returns.
func (w *JSONLWriter) Close(ctx context.Context) error {
	if w == nil || w.requests == nil {
		return errors.New("JSONL writer is not initialized")
	}
	select {
	case <-w.done:
		return w.currentError()
	default:
	}
	if err := w.lockAdmission(ctx); err != nil {
		return errors.Join(err, w.currentError())
	}
	if !w.closed {
		w.closed = true
		close(w.stop)
	}
	w.unlockAdmission()
	select {
	case <-w.done:
		return w.currentError()
	case <-ctx.Done():
		return w.contextError(ctx)
	}
}

func (w *JSONLWriter) writeLoop(output io.Writer, closeOutput func() error) {
	defer close(w.done)
	write := func(request jsonlRequest) {
		select {
		case w.available <- struct{}{}:
		default:
		}
		err := w.currentError()
		if err == nil {
			written, writeErr := output.Write(request.data)
			if writeErr == nil && written != len(request.data) {
				writeErr = io.ErrShortWrite
			}
			err = writeErr
			if err == nil {
				if flusher, ok := output.(interface{ Sync() error }); ok {
					err = flusher.Sync()
				}
			}
			if err != nil {
				w.writeErr = errors.Join(errors.New("JSONL Write/Sync failed"), err)
				close(w.failed)
				err = w.writeErr
			}
		}
		request.ack <- err
	}
	for {
		select {
		case request := <-w.requests:
			write(request)
		case <-w.stop:
			for {
				select {
				case request := <-w.requests:
					write(request)
				default:
					if closeOutput != nil {
						if err := closeOutput(); err != nil {
							w.closeErr = errors.Join(errors.New("JSONL Close failed"), err)
						}
					}
					return
				}
			}
		}
	}
}

func (w *JSONLWriter) lockAdmission(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-w.admission:
		if err := ctx.Err(); err != nil {
			w.unlockAdmission()
			return err
		}
		return nil
	}
}

func (w *JSONLWriter) unlockAdmission() { w.admission <- struct{}{} }

func (w *JSONLWriter) currentError() error {
	select {
	case <-w.done:
		return errors.Join(w.writeErr, w.closeErr)
	default:
	}
	select {
	case <-w.failed:
		return w.writeErr
	default:
		return nil
	}
}

func (w *JSONLWriter) contextError(ctx context.Context) error {
	return errors.Join(ctx.Err(), w.currentError())
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
