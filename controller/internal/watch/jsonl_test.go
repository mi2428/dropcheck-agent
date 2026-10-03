package watch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"dropcheck/controller/internal/command"
	"dropcheck/controller/internal/control"
	"dropcheck/controller/internal/controlpb"
	"dropcheck/controller/internal/runner"
)

type blockedJSONLWriter struct {
	mu         sync.Mutex
	output     bytes.Buffer
	stage      string
	blockKind  EventKind
	entered    chan struct{}
	release    chan struct{}
	blockSync  bool
	syncCalls  int
	closeCalls int
	sequence   []string
	writeErr   error
	syncErr    error
	closeErr   error
	shortWrite bool
}

func (w *blockedJSONLWriter) Write(data []byte) (int, error) {
	var event Event
	if err := json.Unmarshal(data, &event); err != nil {
		return 0, err
	}
	block := event.Kind == EventStepStarted && event.Step.Type == "cleanup" && event.Step.Name == "disconnect"
	if w.blockKind != "" {
		block = event.Kind == w.blockKind && event.Status == "skipped"
	}
	if block && w.stage == "write" {
		close(w.entered)
		<-w.release
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.sequence = append(w.sequence, "write:"+event.Message)
	if w.writeErr != nil {
		return 0, w.writeErr
	}
	if w.shortWrite {
		return len(data) - 1, nil
	}
	w.blockSync = block && w.stage == "sync"
	return w.output.Write(data)
}

func (w *blockedJSONLWriter) Sync() error {
	w.mu.Lock()
	block := w.blockSync
	w.blockSync = false
	w.syncCalls++
	w.sequence = append(w.sequence, "sync")
	w.mu.Unlock()
	if block {
		close(w.entered)
		<-w.release
	}
	return w.syncErr
}

func (w *blockedJSONLWriter) Close() error {
	w.mu.Lock()
	w.closeCalls++
	w.sequence = append(w.sequence, "close")
	w.mu.Unlock()
	if w.stage == "close" {
		close(w.entered)
		<-w.release
	}
	return w.closeErr
}

func TestStoppedCleanupDoesNotWaitForJSONLIO(t *testing.T) {
	for _, stage := range []string{"write", "sync"} {
		t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			writer := &blockedJSONLWriter{stage: stage, entered: make(chan struct{}), release: make(chan struct{})}
			log := newJSONLWriter(writer, writer.Close)
			progress := make(chan Event, 32)
			cleanups := make(chan string, 2)
			done := make(chan error, 1)
			opRunner := cleanupRunnerFunc(func(_ context.Context, op command.Operation) (runner.Result, error) {
				if op.Name == "wifi.wait" {
					cancel()
					return runner.Result{}, context.Canceled
				}
				if op.Name == "wifi.disconnect" || op.Name == "wifi.forget" {
					cleanups <- op.Name
				}
				return runner.Result{Result: &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_OK}}, nil
			})
			returned := false
			defer func() {
				close(writer.release)
				if !returned {
					select {
					case <-done:
					case <-time.After(2 * time.Second):
						t.Error("watch did not finish after releasing fake I/O")
					}
				}
				closeCtx, stop := context.WithTimeout(context.Background(), 2*time.Second)
				defer stop()
				if err := log.Close(closeCtx); err != nil {
					t.Errorf("JSONL did not drain/close after fake I/O release: %v", err)
				}
				writer.mu.Lock()
				closed := writer.closeCalls
				writer.mu.Unlock()
				if closed != 1 {
					t.Errorf("owned writer close calls=%d, want 1", closed)
				}
			}()
			go func() {
				done <- RunWithOptions(ctx, Plan{Targets: []Target{{SSID: "Test Network", ForgetAfter: new(true)}}}, opRunner, control.AgentInfo{}, MultiSink{log, ChannelSink{C: progress}}, RunOptions{})
			}()
			select {
			case <-writer.entered:
			case <-time.After(time.Second):
				t.Fatal("cleanup did not reach real JSONL I/O")
			}
			select {
			case err := <-done:
				returned = true
				if err == nil || !errors.Is(err, context.DeadlineExceeded) {
					t.Errorf("blocked JSONL delivery failure was not preserved: %v", err)
				}
			case <-time.After(cleanupTimeout + 200*time.Millisecond):
				t.Error("watch remained blocked beyond cleanup budget")
			}
			if len(cleanups) != 2 {
				t.Errorf("logging blocked requested cleanup: calls=%d", len(cleanups))
			}
		})
	}
}

// Keep the fixture interface explicit: both Write and Sync must be exercised.
var _ io.WriteCloser = (*blockedJSONLWriter)(nil)

func TestOperatorSkipJSONLCleanupWithLiveParent(t *testing.T) {
	for _, mode := range []string{"pause", "wait", "check"} {
		for _, stage := range []string{"ready", "write", "sync"} {
			t.Run(mode+"/"+stage, func(t *testing.T) {
				ctx := context.Background() // Skip must not cancel the parent to escape I/O.
				pause, skip := NewPauseController(), NewSkipController()
				kind := EventStepFinished
				if mode == "pause" {
					kind = EventTargetFinished
				}
				writer := &blockedJSONLWriter{stage: stage, blockKind: kind, entered: make(chan struct{}), release: make(chan struct{})}
				log := NewJSONLWriter(writer)
				boundary, cleanups := make(chan struct{}, 1), make(chan string, 2)
				done := make(chan error, 1)
				var releaseOnce sync.Once
				release := func() { releaseOnce.Do(func() { close(writer.release) }) }
				returned := false
				defer func() {
					release()
					if !returned {
						select {
						case <-done:
						case <-time.After(2 * time.Second):
							t.Error("watch remained after releasing skipped JSONL I/O")
						}
					}
					closeCtx, stop := context.WithTimeout(ctx, 2*time.Second)
					defer stop()
					if err := log.Close(closeCtx); err != nil {
						t.Errorf("released JSONL did not drain: %v", err)
					}
				}()
				var operations []string
				activeOp := map[string]string{"wait": "wifi.wait", "check": "ping"}[mode]
				opRunner := cleanupRunnerFunc(func(opCtx context.Context, op command.Operation) (runner.Result, error) {
					operations = append(operations, op.Name)
					if op.Name == activeOp {
						boundary <- struct{}{}
						<-opCtx.Done()
						return runner.Result{}, opCtx.Err()
					}
					if op.Name == "wifi.disconnect" || op.Name == "wifi.forget" {
						deadline, bounded := opCtx.Deadline()
						if opCtx.Err() != nil || !bounded || time.Until(deadline) < cleanupTimeout-time.Second {
							t.Errorf("cleanup lost its independent operation budget: err=%v deadline=%v", opCtx.Err(), deadline)
						}
						cleanups <- op.Name
					}
					return runner.Result{Result: &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_OK}}, nil
				})
				var progress []Event
				roundDone := errors.New("test round finished")
				sink := MultiSink{log, cleanupSinkFunc(func(_ context.Context, event Event) error {
					writer.mu.Lock()
					synced := writer.syncCalls
					writer.mu.Unlock()
					if synced != len(progress)+1 {
						return errors.New("progress preceded durable JSONL ack")
					}
					progress = append(progress, event)
					if mode == "pause" && event.Kind == EventStepFinished && event.Step.Name == "connect" {
						pause.Pause()
						boundary <- struct{}{}
					}
					if event.Kind == EventRoundFinished {
						return roundDone // End the test round without canceling the parent.
					}
					return nil
				})}
				go func() {
					plan := Plan{Targets: []Target{{SSID: "Test Network", ForgetAfter: new(true)}}, Checks: []Check{{Name: "first", Type: "ping", Host: "example.test"}, {Name: "later", Type: "dns", Query: "example.test"}}}
					done <- RunWithOptions(ctx, plan, opRunner, control.AgentInfo{}, sink, RunOptions{Pause: pause, Skip: skip})
				}()
				select {
				case <-boundary:
				case <-time.After(time.Second):
					t.Fatal("watch did not reach Skip boundary")
				}
				gateTimer, ticker := time.NewTimer(time.Second), time.NewTicker(time.Millisecond)
				defer gateTimer.Stop()
				defer ticker.Stop()
				for {
					skip.mu.Lock()
					active := len(skip.active)
					skip.mu.Unlock()
					if active == 1 {
						break
					}
					select {
					case <-ticker.C:
					case <-gateTimer.C:
						t.Fatal("Skip context was not registered")
					}
				}
				skip.Skip()
				if stage != "ready" {
					select {
					case <-writer.entered:
					case <-time.After(time.Second):
						t.Fatal("skipped terminal event did not reach real JSONL I/O")
					}
				}
				cleanupTimer := time.NewTimer(2 * time.Second)
				defer cleanupTimer.Stop()
				for _, want := range []string{"wifi.disconnect", "wifi.forget"} {
					select {
					case got := <-cleanups:
						if got != want {
							t.Fatalf("cleanup=%s, want %s", got, want)
						}
					case <-cleanupTimer.C:
						t.Fatal("live-parent Skip left requested cleanup behind blocked JSONL")
					}
				}
				select {
				case err := <-done:
					returned = true
					if stage == "ready" && err != roundDone || stage != "ready" && !errors.Is(err, context.DeadlineExceeded) {
						t.Fatalf("Skip result lost normal progress or finite I/O error: %v", err)
					}
				case <-time.After(cleanupTimeout + time.Second):
					t.Fatal("Skip remained blocked after cleanup")
				}
				wantOps := []string{"wifi.capabilities", "wifi.connect"}
				if mode != "pause" {
					wantOps = append(wantOps, "wifi.wait")
				}
				if mode == "check" {
					wantOps = append(wantOps, "ping")
				}
				wantOps = append(wantOps, "wifi.disconnect", "wifi.forget")
				if !slices.Equal(operations, wantOps) || ctx.Err() != nil {
					t.Fatalf("Skip ran later probes, repeated cleanup, or canceled parent: %v", operations)
				}
				release()
				closeCtx, stop := context.WithTimeout(ctx, 2*time.Second)
				defer stop()
				if err := log.Close(closeCtx); err != nil {
					t.Fatal(err)
				}
				writer.mu.Lock()
				data := append([]byte(nil), writer.output.Bytes()...)
				synced, closed := writer.syncCalls, writer.closeCalls
				writer.mu.Unlock()
				if synced != bytes.Count(data, []byte("\n")) || closed != 0 || len(log.requests) != 0 {
					t.Fatal("JSONL drain/durable ack/borrowed ownership changed")
				}
				if stage == "ready" {
					var want bytes.Buffer
					for _, event := range progress {
						encoded, err := json.Marshal(event)
						if err != nil {
							t.Fatal(err)
						}
						want.Write(append(encoded, '\n'))
					}
					if !bytes.Equal(data, want.Bytes()) || len(progress) < 6 || progress[len(progress)-6].Kind != EventTargetFinished || progress[len(progress)-6].Status != "skipped" {
						t.Fatal("normal Skip lost JSONL/progress order or terminal event")
					}
				} else {
					for _, event := range progress {
						if event.Status == "skipped" {
							t.Fatal("unacknowledged skipped JSONL advanced progress")
						}
					}
				}
				select {
				case <-log.done:
				default:
					t.Fatal("JSONL worker remained after I/O release")
				}
			})
		}
	}
}

func TestJSONLWriterDurableOrderAndOwnedClose(t *testing.T) {
	writer := &blockedJSONLWriter{}
	log := newJSONLWriter(writer, writer.Close)
	defer func() { _ = log.Close(context.Background()) }()
	progress := make(chan Event, 2)
	for i, message := range []string{"first", "second"} {
		if err := (MultiSink{log, ChannelSink{C: progress}}).Emit(context.Background(), Event{Kind: EventLog, Message: message}); err != nil {
			t.Fatal(err)
		}
		if event := <-progress; event.Message != message {
			t.Fatalf("progress event=%v", event)
		}
		writer.mu.Lock()
		synced := writer.syncCalls
		writer.mu.Unlock()
		if synced != i+1 {
			t.Fatalf("Emit returned before durable Sync: calls=%d", synced)
		}
	}
	if err := log.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	writer.mu.Lock()
	sequence := append([]string(nil), writer.sequence...)
	closed := writer.closeCalls
	writer.mu.Unlock()
	if !slices.Equal(sequence, []string{"write:first", "sync", "write:second", "sync", "close"}) || closed != 1 {
		t.Fatalf("writer sequence=%v closeCalls=%d", sequence, closed)
	}
	if err := log.Emit(context.Background(), Event{}); !errors.Is(err, errJSONLClosed) {
		t.Fatalf("Emit after Close error=%v", err)
	}
	select {
	case <-log.done:
	default:
		t.Fatal("writer worker did not finish")
	}
	if len(log.requests) != 0 {
		t.Fatal("accepted queue did not drain")
	}
}

func TestNewJSONLWriterDoesNotCloseBorrowedWriter(t *testing.T) {
	writer := &blockedJSONLWriter{}
	log := NewJSONLWriter(writer)
	defer func() { _ = log.Close(context.Background()) }()
	if err := log.Emit(context.Background(), Event{Kind: EventLog, Message: "borrowed"}); err != nil {
		t.Fatal(err)
	}
	if err := log.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	writer.mu.Lock()
	closed, synced := writer.closeCalls, writer.syncCalls
	writer.mu.Unlock()
	if closed != 0 || synced != 1 {
		t.Fatalf("borrowed writer ownership changed: closed=%d synced=%d", closed, synced)
	}
	select {
	case <-log.done:
	default:
		t.Fatal("borrowed writer worker remained after drain")
	}
}

func TestOpenJSONLFileAppendsAndClosesWorker(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	for _, message := range []string{"first", "second"} {
		log, err := OpenJSONLFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := log.Emit(context.Background(), Event{Kind: EventLog, Message: message}); err != nil {
			_ = log.Close(context.Background())
			t.Fatal(err)
		}
		if err := log.Close(context.Background()); err != nil {
			t.Fatal(err)
		}
		select {
		case <-log.done:
		default:
			t.Fatal("owned file worker did not close")
		}
	}
	data, err := os.ReadFile(path)
	if err != nil || bytes.Count(data, []byte("\n")) != 2 || !bytes.Contains(data, []byte(`"message":"second"`)) {
		t.Fatalf("owned append/flush contract failed: %v", err)
	}
}

func TestJSONLWriterCloseAdmissionHonorsContextAndPreservesCause(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(strconv.FormatBool(fail), func(t *testing.T) {
			failure := errors.New("synthetic prior write failure")
			writer := &blockedJSONLWriter{}
			if fail {
				writer.writeErr = failure
			}
			log := NewJSONLWriter(writer)
			defer func() { _ = log.Close(context.Background()) }()
			if fail {
				if err := log.Emit(context.Background(), Event{Kind: EventLog}); !errors.Is(err, failure) {
					t.Fatalf("write failure missing: %v", err)
				}
			}
			<-log.admission
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			defer cancel()
			err := log.Close(ctx)
			log.unlockAdmission()
			if !errors.Is(err, context.DeadlineExceeded) || fail && !errors.Is(err, failure) {
				t.Fatalf("Close admission ignored deadline or cause: %v", err)
			}
			canceledCtx, cancelNow := context.WithCancel(context.Background())
			cancelNow()
			if err := log.Close(canceledCtx); !errors.Is(err, context.Canceled) || fail && !errors.Is(err, failure) {
				t.Fatalf("Close ignored canceled admission or cause: %v", err)
			}
			if log.closed {
				t.Fatal("canceled Close stopped admission")
			}
		})
	}
}

func TestJSONLWriterFailureRemainsVisible(t *testing.T) {
	failure := errors.New("synthetic JSONL I/O failure")
	closeFailure := errors.New("synthetic independent Close failure")
	for _, stage := range []string{"write", "sync", "short write", "close", "write and close", "independent I/O cancel"} {
		t.Run(stage, func(t *testing.T) {
			writer := &blockedJSONLWriter{}
			want := failure
			switch stage {
			case "write":
				writer.writeErr = failure
			case "sync":
				writer.syncErr = failure
			case "short write":
				writer.shortWrite = true
				want = io.ErrShortWrite
			case "close":
				writer.closeErr = failure
			case "write and close":
				writer.writeErr = failure
				writer.closeErr = closeFailure
			case "independent I/O cancel":
				writer.writeErr = context.Canceled
				want = context.Canceled
			}
			log := newJSONLWriter(writer, writer.Close)
			defer func() { _ = log.Close(context.Background()) }()
			err := log.Emit(context.Background(), Event{Kind: EventLog, Message: "first"})
			if stage == "close" {
				if err != nil {
					t.Fatal(err)
				}
			} else {
				if !errors.Is(err, want) || onlyContextError(err, context.Canceled) {
					t.Fatalf("I/O failure missing or treated as caller cancel: %v", err)
				}
				if err := log.Emit(context.Background(), Event{Kind: EventLog, Message: "second"}); !errors.Is(err, want) {
					t.Fatalf("sticky failure lost: %v", err)
				}
			}
			if err := log.Close(context.Background()); !errors.Is(err, want) || stage == "write and close" && !errors.Is(err, closeFailure) {
				t.Fatalf("Close lost writer failure: %v", err)
			}
			writer.mu.Lock()
			writes := 0
			for _, call := range writer.sequence {
				if strings.HasPrefix(call, "write:") {
					writes++
				}
			}
			closed := writer.closeCalls
			writer.mu.Unlock()
			if writes != 1 || closed != 1 {
				t.Fatalf("failed writer was reused or not closed: writes=%d close=%d", writes, closed)
			}
		})
	}
}

func TestJSONLWriterQueueAndShutdownAreBounded(t *testing.T) {
	writer := &blockedJSONLWriter{stage: "write", entered: make(chan struct{}), release: make(chan struct{})}
	log := newJSONLWriter(writer, writer.Close)
	defer func() {
		close(writer.release)
		closeCtx, stop := context.WithTimeout(context.Background(), 2*time.Second)
		defer stop()
		if err := log.Close(closeCtx); err != nil {
			t.Errorf("released writer did not drain/close: %v", err)
		}
		select {
		case <-log.done:
		default:
			t.Error("released writer worker remained alive")
		}
		writer.mu.Lock()
		data := append([]byte(nil), writer.output.Bytes()...)
		synced, closed := writer.syncCalls, writer.closeCalls
		writer.mu.Unlock()
		if bytes.Contains(data, []byte("overflow")) || bytes.Contains(data, []byte("late")) || synced != jsonlQueueSize+1 || closed != 1 || len(log.requests) != 0 {
			t.Errorf("accepted drain invalid: synced=%d closed=%d queue=%d", synced, closed, len(log.requests))
		}
	}()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, jsonlQueueSize+1)
	go func() {
		done <- log.Emit(ctx, Event{Kind: EventStepStarted, Step: StepSnapshot{Type: "cleanup", Name: "disconnect"}})
	}()
	select {
	case <-writer.entered:
	case <-time.After(time.Second):
		t.Fatal("writer did not reach controlled I/O")
	}
	for i := range jsonlQueueSize {
		go func() { done <- log.Emit(ctx, Event{Kind: EventLog, Message: strconv.Itoa(i)}) }()
	}
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for len(log.requests) != jsonlQueueSize {
		select {
		case <-ticker.C:
		case <-deadline.C:
			t.Fatal("accepted queue did not fill")
		}
	}
	overflowCtx, stop := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer stop()
	if err := log.Emit(overflowCtx, Event{Kind: EventLog, Message: "overflow"}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("full queue did not honor context: %v", err)
	}
	if len(log.requests) != jsonlQueueSize {
		t.Fatal("queue exceeded its capacity")
	}
	late := make(chan error, 1)
	go func() { late <- log.Emit(context.Background(), Event{Kind: EventLog, Message: "late"}) }()
	closeCtx, end := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer end()
	if err := log.Close(closeCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("blocked I/O shutdown was reported as success: %v", err)
	}
	select {
	case err := <-late:
		if !errors.Is(err, errJSONLClosed) {
			t.Fatalf("concurrent waiting Emit did not observe closed admission: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("concurrent Emit stayed queued after Close")
	}
	cancel()
	for range jsonlQueueSize + 1 {
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("Emit did not return caller cancel: %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("Emit stayed blocked after context cancellation")
		}
	}
	if err := log.Emit(context.Background(), Event{}); !errors.Is(err, errJSONLClosed) {
		t.Fatalf("closed admission accepted another event: %v", err)
	}
}

func TestJSONLWriterCloseIOHonorsCallerDeadline(t *testing.T) {
	writer := &blockedJSONLWriter{stage: "close", entered: make(chan struct{}), release: make(chan struct{})}
	log := newJSONLWriter(writer, writer.Close)
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(writer.release) }) }
	defer func() {
		release()
		closeCtx, stop := context.WithTimeout(context.Background(), time.Second)
		defer stop()
		if err := log.Close(closeCtx); err != nil {
			t.Errorf("released Close did not finish: %v", err)
		}
	}()
	firstClose := make(chan error, 1)
	go func() { firstClose <- log.Close(context.Background()) }()
	select {
	case <-writer.entered:
	case <-time.After(time.Second):
		t.Fatal("owned Close was not attempted")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := log.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("blocked Close was reported as success: %v", err)
	}
	release()
	select {
	case err := <-firstClose:
		if err != nil {
			t.Fatalf("concurrent Close error=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("concurrent Close did not finish after I/O resumed")
	}
	if err := log.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	writer.mu.Lock()
	closed := writer.closeCalls
	writer.mu.Unlock()
	if closed != 1 {
		t.Fatalf("concurrent Close closed owned output %d times", closed)
	}
	select {
	case <-log.done:
	default:
		t.Fatal("worker remained after Close resumed")
	}
}

func TestNormalCleanupPublishesStartedBeforeOperation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	writer := &blockedJSONLWriter{}
	log := NewJSONLWriter(writer)
	defer func() { _ = log.Close(context.Background()) }()
	started := map[string]bool{}
	opRunner := cleanupRunnerFunc(func(_ context.Context, op command.Operation) (runner.Result, error) {
		if op.Name == "wifi.disconnect" || op.Name == "wifi.forget" {
			if !started[strings.TrimPrefix(op.Name, "wifi.")] {
				return runner.Result{}, errors.New("normal cleanup progress was not published before operation")
			}
		}
		return runner.Result{Result: &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_OK}}, nil
	})
	progress := cleanupSinkFunc(func(_ context.Context, event Event) error {
		if event.Kind == EventStepStarted && event.Step.Type == "cleanup" {
			started[event.Step.Name] = true
		}
		if event.Kind == EventRoundFinished {
			cancel()
		}
		return nil
	})
	if err := RunWithOptions(ctx, Plan{Targets: []Target{{SSID: "Test Network", ForgetAfter: new(true)}}}, opRunner, control.AgentInfo{}, MultiSink{log, progress}, RunOptions{}); err != nil {
		t.Fatal(err)
	}
	if !started["disconnect"] || !started["forget"] {
		t.Fatal("normal cleanup did not publish TUI progress")
	}
}

func TestJSONLWriterOwnsFileDescriptor(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "events-*.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	log := newJSONLWriter(file, file.Close)
	defer func() { _ = log.Close(context.Background()) }()
	if err := log.Emit(context.Background(), Event{Kind: EventLog, Message: "durable"}); err != nil {
		t.Fatal(err)
	}
	if err := log.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte("unexpected")); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("owned descriptor is still open: %v", err)
	}
	data, err := os.ReadFile(file.Name())
	if err != nil || !bytes.Contains(data, []byte(`"message":"durable"`)) || bytes.Count(data, []byte("\n")) != 1 {
		t.Fatalf("closed JSONL contents invalid: error=%v", err)
	}
}

func TestJSONLWriterFailureWakesAllWaitingEmitters(t *testing.T) {
	failure := errors.New("synthetic concurrent writer failure")
	writer := &blockedJSONLWriter{stage: "write", entered: make(chan struct{}), release: make(chan struct{}), writeErr: failure}
	log := newJSONLWriter(writer, writer.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(writer.release) }) }
	defer func() {
		release()
		if err := log.Close(context.Background()); !errors.Is(err, failure) {
			t.Errorf("concurrent writer failure lost on Close: %v", err)
		}
	}()
	count := 2*jsonlQueueSize + 1
	done := make(chan error, count)
	go func() {
		done <- log.Emit(ctx, Event{Kind: EventStepStarted, Step: StepSnapshot{Type: "cleanup", Name: "disconnect"}})
	}()
	select {
	case <-writer.entered:
	case <-ctx.Done():
		t.Fatal("controlled writer did not start")
	}
	for i := 1; i < count; i++ {
		go func() { done <- log.Emit(ctx, Event{Kind: EventLog, Message: strconv.Itoa(i)}) }()
	}
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for len(log.requests) != jsonlQueueSize {
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal("concurrent accepted queue did not fill")
		}
	}
	release()
	for range count {
		select {
		case err := <-done:
			if !errors.Is(err, failure) || ctx.Err() != nil {
				t.Fatalf("emitter did not promptly receive writer failure: %v", err)
			}
		case <-ctx.Done():
			t.Fatal("writer failure left emitters waiting")
		}
	}
}

func TestNormalWatchCancellationFlushesFinalJSONLEvents(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	writer := &blockedJSONLWriter{}
	log := NewJSONLWriter(writer)
	defer func() { _ = log.Close(context.Background()) }()
	progress := make(chan Event, 32)
	opRunner := cleanupRunnerFunc(func(_ context.Context, op command.Operation) (runner.Result, error) {
		if op.Name == "wifi.wait" {
			cancel()
			return runner.Result{}, context.Canceled
		}
		return runner.Result{Result: &controlpb.CommandResult{Status: controlpb.CommandResult_STATUS_OK}}, nil
	})
	err := RunWithOptions(ctx, Plan{Targets: []Target{{SSID: "Test Network", ForgetAfter: new(true)}}}, opRunner, control.AgentInfo{}, MultiSink{log, ChannelSink{C: progress}}, RunOptions{})
	if err != nil {
		t.Fatalf("normal cancellation error=%v", err)
	}
	closeCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer stop()
	if err := log.Close(closeCtx); err != nil {
		t.Fatal(err)
	}
	writer.mu.Lock()
	data := append([]byte(nil), writer.output.Bytes()...)
	writer.mu.Unlock()
	var events []Event
	decoder := json.NewDecoder(bytes.NewReader(data))
	for decoder.More() {
		var event Event
		if err := decoder.Decode(&event); err != nil {
			t.Fatal(err)
		}
		events = append(events, event)
	}
	if len(events) < 4 {
		t.Fatalf("terminal events missing: %v", events)
	}
	final := events[len(events)-4:]
	for i, name := range []string{"disconnect", "disconnect", "forget", "forget"} {
		kind := EventStepStarted
		if i%2 == 1 {
			kind = EventStepFinished
		}
		if final[i].Kind != kind || final[i].Step.Name != name || final[i].Time.IsZero() {
			t.Fatalf("final JSONL event %d=%v", i, final[i])
		}
	}
	if len(progress) != len(events) {
		t.Fatalf("normal TUI progress lost: progress=%d JSONL=%d", len(progress), len(events))
	}
}
