package harness

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"sync"
	"testing"
	"time"
)

type blockedJSONLWriter struct {
	mu                                sync.Mutex
	output                            bytes.Buffer
	stage                             string
	entered, release                  chan struct{}
	once                              sync.Once
	writeErr, syncErr, closeErr       error
	writeCalls, syncCalls, closeCalls int
}

func (w *blockedJSONLWriter) block(stage string) {
	if w.stage == stage && w.release != nil {
		w.once.Do(func() { close(w.entered) })
		<-w.release
	}
}
func (w *blockedJSONLWriter) Write(data []byte) (int, error) {
	w.block("write")
	w.mu.Lock()
	defer w.mu.Unlock()
	w.writeCalls++
	if w.writeErr != nil {
		return 0, w.writeErr
	}
	return w.output.Write(data)
}
func (w *blockedJSONLWriter) Sync() error {
	w.block("sync")
	w.mu.Lock()
	defer w.mu.Unlock()
	w.syncCalls++
	return w.syncErr
}
func (w *blockedJSONLWriter) Close() error {
	w.block("close")
	w.mu.Lock()
	defer w.mu.Unlock()
	w.closeCalls++
	return w.closeErr
}

var _ io.WriteCloser = (*blockedJSONLWriter)(nil)

func TestJSONLWriterDurableOrderAndOwnedClose(t *testing.T) {
	w := &blockedJSONLWriter{}
	sink := newJSONLWriter(w, w.Close)
	for i := uint64(1); i <= 3; i++ {
		if err := sink.Emit(context.Background(), Event{Seq: i, Kind: EventLog}); err != nil {
			t.Fatal(err)
		}
	}
	if err := sink.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := sink.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if w.writeCalls != 3 || w.syncCalls != 3 || w.closeCalls != 1 || bytes.Count(w.output.Bytes(), []byte("\n")) != 3 {
		t.Fatalf("durable order/ownership writes=%d sync=%d close=%d", w.writeCalls, w.syncCalls, w.closeCalls)
	}
}

func TestNewJSONLWriterDoesNotCloseBorrowedWriter(t *testing.T) {
	w := &blockedJSONLWriter{}
	sink := NewJSONLWriter(w)
	if err := sink.Emit(context.Background(), Event{Kind: EventLog}); err != nil {
		t.Fatal(err)
	}
	if err := sink.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if w.closeCalls != 0 {
		t.Fatal("borrowed writer was closed")
	}
}

func TestOpenJSONLFileAppendsAndClosesWorker(t *testing.T) {
	path := t.TempDir() + "/events.jsonl"
	for range 2 {
		sink, err := OpenJSONLFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := sink.Emit(context.Background(), Event{Kind: EventLog, Message: "durable"}); err != nil {
			t.Fatal(err)
		}
		if err := sink.Close(context.Background()); err != nil {
			t.Fatal(err)
		}
		select {
		case <-sink.done:
		default:
			t.Fatal("worker remained after Close")
		}
	}
	data, err := os.ReadFile(path)
	if err != nil || bytes.Count(data, []byte("\n")) != 2 {
		t.Fatalf("append=%s err=%v", data, err)
	}
}

func TestJSONLWriterFailureRemainsVisible(t *testing.T) {
	failure := errors.New("synthetic I/O failure")
	for _, stage := range []string{"write", "sync", "close"} {
		t.Run(stage, func(t *testing.T) {
			w := &blockedJSONLWriter{}
			switch stage {
			case "write":
				w.writeErr = failure
			case "sync":
				w.syncErr = failure
			case "close":
				w.closeErr = failure
			}
			sink := newJSONLWriter(w, w.Close)
			err := sink.Emit(context.Background(), Event{Kind: EventLog})
			if stage != "close" && !errors.Is(err, failure) {
				t.Fatalf("Emit lost cause: %v", err)
			}
			if err := sink.Close(context.Background()); !errors.Is(err, failure) {
				t.Fatalf("Close lost cause: %v", err)
			}
		})
	}
}

func TestJSONLWriterQueueAndShutdownAreBounded(t *testing.T) {
	w := &blockedJSONLWriter{stage: "write", entered: make(chan struct{}), release: make(chan struct{})}
	sink := NewJSONLWriter(w)
	defer func() { close(w.release); _ = sink.Close(context.Background()) }()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 2*jsonlQueueSize+1)
	go func() { done <- sink.Emit(ctx, Event{Kind: EventLog}) }()
	select {
	case <-w.entered:
	case <-time.After(time.Second):
		t.Fatal("writer did not enter")
	}
	for range 2 * jsonlQueueSize {
		go func() { done <- sink.Emit(ctx, Event{Kind: EventLog}) }()
	}
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for len(sink.requests) != jsonlQueueSize {
		select {
		case <-ticker.C:
		case <-deadline.C:
			t.Fatal("bounded queue did not fill")
		}
	}
	cancel()
	for range 2*jsonlQueueSize + 1 {
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Errorf("Emit=%v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("canceled emitter remained")
		}
	}
	closeCtx, stop := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer stop()
	if err := sink.Close(closeCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("uninterruptible I/O was claimed closed: %v", err)
	}
}

func TestJSONLWriterCloseIOHonorsCallerDeadline(t *testing.T) {
	w := &blockedJSONLWriter{stage: "close", entered: make(chan struct{}), release: make(chan struct{})}
	sink := newJSONLWriter(w, w.Close)
	defer func() { close(w.release); _ = sink.Close(context.Background()) }()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := sink.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("close did not bound wait: %v", err)
	}
}

func TestJSONLWriterFailureWakesAllWaitingEmitters(t *testing.T) {
	failure := errors.New("synthetic concurrent writer failure")
	w := &blockedJSONLWriter{stage: "write", entered: make(chan struct{}), release: make(chan struct{}), writeErr: failure}
	sink := newJSONLWriter(w, w.Close)
	done := make(chan error, 2*jsonlQueueSize+1)
	go func() { done <- sink.Emit(context.Background(), Event{Kind: EventLog}) }()
	<-w.entered
	for range 2 * jsonlQueueSize {
		go func() { done <- sink.Emit(context.Background(), Event{Kind: EventLog}) }()
	}
	close(w.release)
	for range 2*jsonlQueueSize + 1 {
		select {
		case err := <-done:
			if !errors.Is(err, failure) {
				t.Errorf("cause=%v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("failed worker left emitter blocked")
		}
	}
	if err := sink.Close(context.Background()); !errors.Is(err, failure) {
		t.Fatalf("Close cause=%v", err)
	}
}

func TestJSONLWriterCloseAdmissionHonorsContextAndPreservesCause(t *testing.T) {
	w := &blockedJSONLWriter{}
	sink := NewJSONLWriter(w)
	<-sink.admission
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := sink.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("admission wait=%v", err)
	}
	sink.admission <- struct{}{}
	if err := sink.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestMultiSinkFailureDoesNotHideOtherConsumers(t *testing.T) {
	failure := errors.New("file failed")
	called := false
	sinks := MultiSink{cleanupSinkFunc(func(context.Context, Event) error { return failure }), cleanupSinkFunc(func(context.Context, Event) error { called = true; return nil })}
	if err := sinks.Emit(context.Background(), Event{}); !errors.Is(err, failure) || !called {
		t.Fatal("failed sink hid UI or cause")
	}
}
