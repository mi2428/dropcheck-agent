package ingester

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"dropcheck/controller/internal/controlpb"
	"google.golang.org/protobuf/proto"
)

// Ingester receives MinIO notifications, backfills missed objects, and pushes metrics.
type Ingester struct {
	cfg       Config
	store     ObjectStore
	pusher    MetricPusher
	logger    *log.Logger
	processed sync.Map
}

func New(cfg Config, store ObjectStore, pusher MetricPusher, logger *log.Logger) *Ingester {
	if logger == nil {
		logger = log.Default()
	}
	return &Ingester{
		cfg:    cfg,
		store:  store,
		pusher: pusher,
		logger: logger,
	}
}

func (i *Ingester) Run(ctx context.Context) error {
	server := &http.Server{
		Addr:              i.cfg.ListenAddr,
		Handler:           i.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	errCh := make(chan error, 2)
	go func() {
		i.logger.Printf("ingester listening addr=%s bucket=%s prefix=%q suffix=%q pushgateway=%s interval=%s", i.cfg.ListenAddr, i.cfg.MinIOBucket, i.cfg.MinIOPrefix, i.cfg.ObjectSuffix, i.cfg.PushgatewayURL, i.cfg.BatchInterval)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()
	go func() {
		errCh <- i.RunBatches(ctx)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return err
		}
		return ctx.Err()
	case err := <-errCh:
		if err == nil {
			return nil
		}
		return err
	}
}

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
	objects, err := DecodeNotification(r.Body, i.cfg.ObjectSuffix)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	failures := 0
	for _, object := range objects {
		if err := i.ProcessObject(r.Context(), object); err != nil {
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

func (i *Ingester) RunBatches(ctx context.Context) error {
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

func (i *Ingester) ProcessBatch(ctx context.Context) error {
	objects, err := i.store.ListObjects(ctx)
	if err != nil {
		return fmt.Errorf("list objects: %w", err)
	}
	var errs []error
	processed := 0
	for _, object := range objects {
		if err := i.ProcessObject(ctx, object); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", object.Key, err))
			continue
		}
		processed++
	}
	if len(objects) > 0 {
		i.logger.Printf("batch scanned=%d processed=%d failed=%d", len(objects), processed, len(errs))
	}
	return errors.Join(errs...)
}

func (i *Ingester) ProcessObject(ctx context.Context, object ObjectRef) error {
	if !objectMatches(object.Key, i.cfg.MinIOPrefix, i.cfg.ObjectSuffix) {
		return nil
	}
	if i.alreadyProcessed(object) {
		return nil
	}
	start := time.Now()
	data, err := i.store.GetObject(ctx, object.Key)
	if err != nil {
		pushErr := i.pusher.Push(ctx, object.Key, IngestFailureMetrics(time.Since(start), "fetch_failed", err))
		return errors.Join(fmt.Errorf("fetch object: %w", err), pushErr)
	}
	archive := &controlpb.StandaloneRunArchive{}
	if err := proto.Unmarshal(data, archive); err != nil {
		pushErr := i.pusher.Push(ctx, object.Key, IngestFailureMetrics(time.Since(start), "decode_failed", err))
		return errors.Join(fmt.Errorf("decode standalone archive: %w", err), pushErr)
	}
	if err := i.pusher.Push(ctx, object.Key, ArchiveMetrics(archive, time.Since(start))); err != nil {
		return fmt.Errorf("push metrics: %w", err)
	}
	i.markProcessed(object)
	i.logger.Printf("ingested key=%q run_id=%q bytes=%d", object.Key, archive.GetSummary().GetRunId(), len(data))
	return nil
}

func (i *Ingester) alreadyProcessed(object ObjectRef) bool {
	signature := object.signature()
	if signature == "" {
		return false
	}
	value, ok := i.processed.Load(object.Key)
	return ok && value == signature
}

func (i *Ingester) markProcessed(object ObjectRef) {
	if signature := object.signature(); signature != "" {
		i.processed.Store(object.Key, signature)
	}
}
