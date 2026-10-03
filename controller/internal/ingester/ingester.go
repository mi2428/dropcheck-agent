package ingester

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"dropcheck/controller/internal/controlpb"
	"google.golang.org/protobuf/proto"
)

// Ingester receives MinIO notifications, backfills missed objects, and pushes metrics.
type Ingester struct {
	cfg        Config
	store      ObjectStore
	pusher     MetricPusher
	logger     *log.Logger
	processed  sync.Map
	batchGate  chan struct{}
	generation atomic.Uint64
	groupGate  chan struct{}
	groupTime  map[string]archiveOrder
}

type archiveOrder struct {
	at  int64
	key string
}

type processedObject struct {
	signature  string
	generation uint64
}

// New creates an Ingester using the supplied object store and metric pusher.
func New(cfg Config, store ObjectStore, pusher MetricPusher, logger *log.Logger) *Ingester {
	if logger == nil {
		logger = log.Default()
	}
	return &Ingester{
		cfg:       cfg,
		store:     store,
		pusher:    pusher,
		logger:    logger,
		batchGate: make(chan struct{}, 1),
		groupGate: make(chan struct{}, 1),
		groupTime: make(map[string]archiveOrder),
	}
}

// Run serves the notification HTTP endpoint and scheduled backfill loop until
// ctx is canceled or either path returns a fatal error.
func (i *Ingester) Run(ctx context.Context) error {
	if err := validateWebhookToken(i.cfg.WebhookToken); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// Bind before starting privileged work; a listen failure must not launch backfill.
	listener, err := net.Listen("tcp", i.cfg.ListenAddr)
	if err != nil {
		return err
	}
	runCtx, stop := context.WithCancel(ctx)
	defer stop()
	server := &http.Server{
		Handler:           i.Handler(),
		BaseContext:       func(net.Listener) context.Context { return runCtx },
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       notificationReadTimeout,
		WriteTimeout:      notificationTimeout + 5*time.Second,
		IdleTimeout:       time.Minute,
	}
	errCh := make(chan error, 2)
	var workers sync.WaitGroup
	workers.Add(2)
	go func() {
		defer workers.Done()
		i.logger.Printf("ingester listening addr=%s bucket=%s prefix=%q suffix=%q pushgateway=%s interval=%s", i.cfg.ListenAddr, i.cfg.MinIOBucket, i.cfg.MinIOPrefix, i.cfg.ObjectSuffix, i.cfg.PushgatewayURL, i.cfg.BatchInterval)
		errCh <- server.Serve(listener)
	}()
	go func() {
		defer workers.Done()
		errCh <- i.RunBatches(runCtx)
	}()

	var runErr error
	select {
	case <-ctx.Done():
		runErr = ctx.Err()
	case runErr = <-errCh:
	}
	stop() // Cancels both backfill and in-flight HTTP request contexts on every exit.
	if runErr == nil {
		runErr = ctx.Err()
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	shutdownErr := server.Shutdown(shutdownCtx)
	cancel()
	closeErr := server.Close() // Also close active connections if graceful shutdown timed out.
	workers.Wait()
	close(errCh)
	cleanupErrs := []error{shutdownErr, closeErr}
	for err := range errCh {
		if err != http.ErrServerClosed {
			cleanupErrs = append(cleanupErrs, err)
		}
	}
	if err := errors.Join(cleanupErrs...); err != nil {
		return errors.Join(runErr, fmt.Errorf("ingester cleanup: %w", err))
	}
	return runErr
}

const (
	notificationReadTimeout = 5 * time.Second
	notificationTimeout     = 30 * time.Second
)

// Handler returns the ingester HTTP routes.
//
// The handler exposes /healthz for readiness checks and /minio/events for
// MinIO webhook notifications.
func (i *Ingester) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/minio/events", i.handleNotification)
	return mux
}

func (i *Ingester) handleNotification(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		w.WriteHeader(http.StatusOK)
		return
	case http.MethodPost:
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := validateWebhookToken(i.cfg.WebhookToken); err != nil {
		http.Error(w, "webhook authentication is not configured", http.StatusServiceUnavailable)
		return
	}
	// Compare fixed-size digests, including the scheme; never log either value.
	want := sha256.Sum256([]byte("Bearer " + i.cfg.WebhookToken))
	got := sha256.Sum256([]byte(r.Header.Get("Authorization")))
	if len(r.Header.Values("Authorization")) != 1 || subtle.ConstantTimeCompare(got[:], want[:]) != 1 {
		w.Header().Set("WWW-Authenticate", "Bearer")
		http.Error(w, "unauthorized notification", http.StatusUnauthorized)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), notificationTimeout)
	defer cancel()
	if r.ContentLength > notificationBodyLimit {
		http.Error(w, errNotificationTooLarge.Error(), http.StatusRequestEntityTooLarge)
		return
	}
	objects, err := DecodeNotification(r.Body, i.cfg)
	if err != nil {
		var timeout net.Error
		status, message := http.StatusBadRequest, "invalid notification: check JSON records, bucket, object key and size"
		if errors.Is(err, errNotificationTooLarge) {
			status, message = http.StatusRequestEntityTooLarge, errNotificationTooLarge.Error()
		} else if errors.As(err, &timeout) && timeout.Timeout() {
			status, message = http.StatusRequestTimeout, "notification body read timed out"
		}
		http.Error(w, message, status)
		return
	}
	failures := 0
	for _, object := range objects {
		if err := i.ProcessObject(ctx, object); err != nil {
			if ctx.Err() != nil {
				http.Error(w, "notification processing timed out or canceled", http.StatusGatewayTimeout)
				return
			}
			failures++
			i.logger.Printf("notification ingest failed key=%q err=%v", object.Key, err)
		}
	}
	if failures > 0 {
		http.Error(w, fmt.Sprintf("failed=%d total=%d", failures, len(objects)), http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusAccepted)
	_, _ = fmt.Fprintf(w, "processed=%d\n", len(objects))
}

// RunBatches runs an immediate backfill and then repeats it on BatchInterval.
func (i *Ingester) RunBatches(ctx context.Context) error {
	if i.cfg.BatchInterval <= 0 {
		return fmt.Errorf("batch interval must be positive")
	}
	if err := i.ProcessBatch(ctx); err != nil {
		i.logger.Printf("initial batch failed: %v", err)
	}
	ticker := time.NewTicker(i.cfg.BatchInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := i.ProcessBatch(ctx); err != nil {
				i.logger.Printf("scheduled batch failed: %v", err)
			}
		}
	}
}

// ProcessBatch scans the configured object prefix and processes every matching
// result archive that has not already been seen with the same object signature.
func (i *Ingester) ProcessBatch(ctx context.Context) error {
	select {
	case i.batchGate <- struct{}{}:
		defer func() { <-i.batchGate }()
	case <-ctx.Done():
		return ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	generation := i.generation.Add(1)
	// ponytail: keep 10 error samples; scoped diagnostics if larger failure sets need detail.
	const maxBatchErrors = 10
	var errs []error
	scanned, processed, failed := 0, 0, 0
	for object, err := range i.store.ListObjects(ctx) {
		if err != nil {
			return fmt.Errorf("list objects after scanned=%d failed=%d (showing first %d object errors): %w", scanned, failed, len(errs), errors.Join(append(errs, err)...))
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		scanned++
		if err := i.ProcessObject(ctx, object); err != nil {
			failed++
			if len(errs) < maxBatchErrors {
				errs = append(errs, fmt.Errorf("%s: %w", object.Key, err))
			}
			continue
		}
		processed++
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// A complete listing is the retention boundary. CAS preserves entries
	// refreshed by concurrent notifications; incomplete listings never prune.
	i.processed.Range(func(key, value any) bool {
		if value.(processedObject).generation < generation {
			i.processed.CompareAndDelete(key, value)
		}
		return true
	})
	if scanned > 0 {
		i.logger.Printf("batch scanned=%d processed=%d failed=%d", scanned, processed, failed)
	}
	if failed > 0 {
		return fmt.Errorf("batch failed=%d total=%d (showing first %d errors): %w", failed, scanned, len(errs), errors.Join(errs...))
	}
	return nil
}

// ProcessObject parses one object and pushes its metrics batches.
//
// Callers must be trusted in-process code or authenticated notification handlers.
// Every reference must name the configured bucket. Valid objects outside the
// configured prefix/suffix are ignored; invalid references are rejected first.
func (i *Ingester) ProcessObject(ctx context.Context, object ObjectRef) error {
	if err := validateObject(i.cfg, object); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !objectMatches(object.Key, i.cfg.MinIOPrefix, i.cfg.ObjectSuffix) {
		return nil
	}
	if err := validateArchiveSize(i.cfg, object); err != nil {
		return err
	}
	if i.alreadyProcessed(object) {
		return nil
	}
	start := time.Now()
	data, err := i.store.GetObject(ctx, object.Key)
	if err != nil {
		return fmt.Errorf("fetch object: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	archive := &controlpb.StandaloneRunArchive{}
	if err := proto.Unmarshal(data, archive); err != nil {
		return fmt.Errorf("decode standalone archive: %w", err)
	}
	var pushErrs []error
	batches := ArchiveMetricBatches(archive)
	// ponytail: one lock serializes group writes; per-group locks if throughput requires it.
	select {
	case i.groupGate <- struct{}{}:
		defer func() { <-i.groupGate }()
	case <-ctx.Done():
		return ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	finished := archive.GetSummary().GetFinishedUnixMs()
	if finished <= 0 {
		finished = archive.GetSummary().GetStartedUnixMs()
	}
	for _, batch := range batches {
		if err := ctx.Err(); err != nil {
			return err
		}
		key := groupingKey(batch.Grouping)
		// Undated legacy archives may initialize a group, but never replace a
		// dated measurement. Equal timestamps use the object key as a stable tie.
		if previous := i.groupTime[key]; previous.at > 0 &&
			(previous.at > finished || (previous.at == finished && previous.key > object.Key)) {
			continue
		}
		if err := i.pusher.Push(ctx, batch); err != nil {
			pushErrs = append(pushErrs, err)
			continue
		}
		if finished > 0 {
			i.groupTime[key] = archiveOrder{at: finished, key: object.Key}
		}
	}
	if len(pushErrs) > 0 {
		return fmt.Errorf("push metrics: %w", errors.Join(pushErrs...))
	}
	i.markProcessed(object)
	i.logger.Printf("ingested key=%q run_id=%q bytes=%d batches=%d duration=%s", object.Key, archive.GetSummary().GetRunId(), len(data), len(batches), time.Since(start))
	return nil
}

func (i *Ingester) alreadyProcessed(object ObjectRef) bool {
	signature := object.signature()
	if signature == "" {
		return false
	}
	value, ok := i.processed.Load(object.Key)
	if !ok {
		return false
	}
	entry := value.(processedObject)
	i.processed.CompareAndSwap(object.Key, entry, processedObject{signature: entry.signature, generation: i.generation.Load()})
	return entry.signature == signature
}

func (i *Ingester) markProcessed(object ObjectRef) {
	if signature := object.signature(); signature != "" {
		i.processed.Store(object.Key, processedObject{signature: signature, generation: i.generation.Load()})
	}
}
