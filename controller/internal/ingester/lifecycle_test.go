package ingester

import (
	"context"
	"errors"
	"io"
	"iter"
	"log"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunListenFailureDoesNotStartBackfill(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	cfg := testConfig()
	cfg.ListenAddr, cfg.BatchInterval = listener.Addr().String(), time.Hour
	store := &lifecycleStore{listed: make(chan struct{}), listStopped: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ing := New(cfg, store, discardPusher{}, log.New(io.Discard, "", 0))
	if err := ing.Run(ctx); err == nil {
		t.Fatal("occupied listener was accepted")
	}
	select {
	case <-store.listed:
		t.Fatal("backfill started despite fatal listen failure")
	case <-time.After(100 * time.Millisecond):
	}
	if ctx.Err() != nil {
		t.Fatal("Run canceled its caller's context")
	}
}

func TestRunBatchFailureClosesListener(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	cfg := testConfig()
	cfg.ListenAddr, cfg.BatchInterval = listener.Addr().String(), 0
	_ = listener.Close()
	store := &lifecycleStore{listed: make(chan struct{}), listStopped: make(chan struct{})}
	ing := New(cfg, store, discardPusher{}, log.New(io.Discard, "", 0))
	if err := ing.Run(context.Background()); err == nil {
		t.Fatal("invalid backfill interval was accepted")
	}
	select {
	case <-store.listed:
		t.Fatal("invalid backfill launched privileged work")
	default:
	}
	if conn, err := net.DialTimeout("tcp", cfg.ListenAddr, time.Second); err == nil {
		_ = conn.Close()
		t.Fatal("listener remained reachable after fatal batch exit")
	}
}

func TestCanceledBatchWaitDoesNotCancelLiveBatch(t *testing.T) {
	for _, scheduled := range []bool{false, true} {
		name := "ProcessBatch"
		if scheduled {
			name = "RunBatches"
		}
		t.Run(name, func(t *testing.T) {
			cfg := testConfig()
			cfg.BatchInterval = time.Hour
			store := &lifecycleStore{listed: make(chan struct{}), listStopped: make(chan struct{})}
			ing := New(cfg, store, discardPusher{}, log.New(io.Discard, "", 0))
			liveCtx, stopLive := context.WithCancel(context.Background())
			liveDone := make(chan error, 1)
			go func() { defer close(liveDone); liveDone <- ing.ProcessBatch(liveCtx) }()
			waitCtx, stopWait := context.WithCancel(context.Background())
			waitDone := make(chan error, 1)
			t.Cleanup(func() {
				stopWait()
				stopLive()
				for _, done := range []<-chan error{liveDone, waitDone} {
					select {
					case <-done:
					case <-time.After(time.Second):
						t.Error("batch worker did not stop")
					}
				}
			})
			waitLifecycleSignal(t, store.listed) // The independent live caller owns the batch slot.
			go func() {
				defer close(waitDone)
				if scheduled {
					waitDone <- ing.RunBatches(waitCtx)
				} else {
					waitDone <- ing.ProcessBatch(waitCtx)
				}
			}()
			stopWait()
			select {
			case err := <-waitDone:
				if (!scheduled && err != context.Canceled) || (scheduled && err != nil) {
					t.Fatalf("canceled batch wait returned %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("canceled batch acquisition remained blocked behind another context")
			}
			if liveCtx.Err() != nil || store.listCalls.Load() != 1 {
				t.Fatal("canceled waiter affected the independent live caller or entered the store")
			}
			select {
			case <-liveDone:
				t.Fatal("independent live batch was stopped by the waiting caller")
			default:
			}
		})
	}
}

func TestRunCancellationOwnsRequestsAndReportsCleanupFailure(t *testing.T) {
	for _, blockCleanup := range []bool{false, true} {
		name := "cooperative_request"
		if blockCleanup {
			name = "independent_shutdown_timeout"
		}
		t.Run(name, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			cfg := testConfig()
			cfg.ListenAddr, cfg.BatchInterval = listener.Addr().String(), time.Hour
			_ = listener.Close()
			var hold chan struct{}
			if blockCleanup {
				hold = make(chan struct{})
			}
			store := &lifecycleStore{
				listed: make(chan struct{}), listStopped: make(chan struct{}),
				fetched: make(chan struct{}), fetchStopped: make(chan struct{}), holdFetch: hold,
			}
			ing := New(cfg, store, discardPusher{}, log.New(io.Discard, "", 0))
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan error, 1)
			go func() { defer close(done); done <- ing.Run(ctx) }()
			clientCtx, cancelClient := context.WithCancel(context.Background())
			t.Cleanup(func() {
				cancel()
				cancelClient()
				if hold != nil {
					close(hold)
				}
				select {
				case <-done:
				case <-time.After(2 * time.Second):
					t.Error("Run did not finish cleanup")
				}
			})
			readyClient := &http.Client{Timeout: time.Second}
			deadline := time.Now().Add(3 * time.Second)
			for {
				resp, err := readyClient.Get("http://" + cfg.ListenAddr + "/healthz")
				if err == nil {
					_ = resp.Body.Close()
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("server did not become ready")
				}
				time.Sleep(10 * time.Millisecond)
			}
			req, err := http.NewRequestWithContext(clientCtx, http.MethodPost, "http://"+cfg.ListenAddr+"/minio/events", strings.NewReader(validNotification))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Authorization", "Bearer "+cfg.WebhookToken)
			requestDone := make(chan struct{})
			go func() {
				defer close(requestDone)
				resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
				if err == nil {
					_ = resp.Body.Close()
				}
			}()
			waitLifecycleSignal(t, store.listed)
			waitLifecycleSignal(t, store.fetched)
			cancel()
			wait := 2 * time.Second
			if blockCleanup {
				wait = 12 * time.Second
			}
			select {
			case err := <-done:
				if !blockCleanup && err != context.Canceled {
					t.Fatalf("clean cancellation error=%v, want exact context.Canceled", err)
				}
				if blockCleanup && (err == context.Canceled || !errors.Is(err, context.Canceled) || !errors.Is(err, context.DeadlineExceeded)) {
					t.Fatalf("independent cleanup error was lost: %v", err)
				}
			case <-time.After(wait):
				t.Fatal("Run did not stop its owned request and batch")
			}
			if hold != nil {
				close(hold)
				hold = nil
			}
			waitLifecycleSignal(t, store.listStopped)
			waitLifecycleSignal(t, store.fetchStopped)
			waitLifecycleSignal(t, requestDone)
			if conn, err := net.DialTimeout("tcp", cfg.ListenAddr, time.Second); err == nil {
				_ = conn.Close()
				t.Fatal("listener remained reachable after Run returned")
			}
		})
	}
}

func waitLifecycleSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatal("owned work did not reach the expected lifecycle boundary")
	}
}

type lifecycleStore struct {
	listed, listStopped, fetched, fetchStopped chan struct{}
	holdFetch                                  <-chan struct{}
	listCalls                                  atomic.Int32
}

func (s *lifecycleStore) ListObjects(ctx context.Context) iter.Seq2[ObjectRef, error] {
	return func(func(ObjectRef, error) bool) {
		first := s.listCalls.Add(1) == 1
		if first {
			close(s.listed)
		}
		<-ctx.Done()
		if first {
			close(s.listStopped)
		}
	}
}

func (s *lifecycleStore) GetObject(ctx context.Context, _ string) ([]byte, error) {
	close(s.fetched)
	if s.holdFetch != nil {
		// Deliberately violate cancellation to exercise the independent Shutdown deadline.
		<-s.holdFetch
	} else {
		<-ctx.Done()
	}
	close(s.fetchStopped)
	return nil, ctx.Err()
}
