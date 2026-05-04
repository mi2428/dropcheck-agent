package ingester

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"dropcheck/controller/internal/controlpb"
	"google.golang.org/protobuf/proto"
)

const (
	triggerBatch        = "batch"
	triggerNotification = "notification"
	reasonDecode        = "decode"
	reasonFetch         = "fetch"
)

// Ingester receives MinIO object notifications and backfills missed archives.
type Ingester struct {
	cfg    Config
	logger *log.Logger
	store  objectStore
	pusher metricsPusher
	queue  chan objectTask

	seenMu sync.Mutex
	seen   map[string]struct{}
}

type objectTask struct {
	key     string
	trigger string
}

// New constructs an Ingester using MinIO and Pushgateway clients.
func New(cfg Config, logger *log.Logger) (*Ingester, error) {
	if logger == nil {
		logger = log.Default()
	}
	store, err := newMinIOStore(cfg)
	if err != nil {
		return nil, err
	}
	return &Ingester{
		cfg:    cfg,
		logger: logger,
		store:  store,
		pusher: newPushGatewayPusher(cfg),
		queue:  make(chan objectTask, cfg.QueueSize),
		seen:   map[string]struct{}{},
	}, nil
}

// Run starts the notification HTTP server, workers, and scheduled backfill loop.
func (i *Ingester) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var workers sync.WaitGroup
	for worker := 0; worker < i.cfg.Workers; worker++ {
		workers.Add(1)
		go i.worker(ctx, &workers)
	}
	workers.Add(1)
	go i.batchLoop(ctx, &workers)

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", i.handleHealth)
	mux.HandleFunc("/minio/events", i.handleMinIOEvents)
	server := &http.Server{
		Addr:              i.cfg.ListenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		i.logger.Printf("listening addr=%s bucket=%s prefix=%q pushgateway=%s poll_interval=%s", i.cfg.ListenAddr, i.cfg.MinIOBucket, i.cfg.MinIOPrefix, i.cfg.PushgatewayURL, i.cfg.PollInterval)
		errCh <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
	case err := <-errCh:
		cancel()
		workers.Wait()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown http server: %w", err)
	}
	workers.Wait()
	return nil
}

func (i *Ingester) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

func (i *Ingester) handleMinIOEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		http.Error(w, "read notification", http.StatusBadRequest)
		return
	}
	events, err := ParseMinIONotification(data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	enqueued := 0
	for _, event := range events {
		if event.Bucket != "" && event.Bucket != i.cfg.MinIOBucket {
			continue
		}
		if !i.store.Accepts(event.Key) {
			continue
		}
		select {
		case i.queue <- objectTask{key: event.Key, trigger: triggerNotification}:
			enqueued++
		default:
			http.Error(w, "ingest queue full", http.StatusServiceUnavailable)
			return
		}
	}
	w.WriteHeader(http.StatusAccepted)
	_, _ = fmt.Fprintf(w, "accepted=%d\n", enqueued)
}

func (i *Ingester) worker(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case task := <-i.queue:
			i.processObject(ctx, task.key, task.trigger)
		}
	}
}

func (i *Ingester) batchLoop(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	i.runBatch(ctx)
	ticker := time.NewTicker(i.cfg.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			i.runBatch(ctx)
		}
	}
}

func (i *Ingester) runBatch(ctx context.Context) {
	start := time.Now()
	objects, err := i.store.List(ctx)
	if err != nil {
		i.logger.Printf("batch list failed err=%v", err)
		return
	}
	for _, object := range objects {
		if i.isSeen(object.Signature()) {
			continue
		}
		i.processObject(ctx, object.Key, triggerBatch)
	}
	i.logger.Printf("batch complete objects=%d duration=%s", len(objects), time.Since(start).Round(time.Millisecond))
}

func (i *Ingester) processObject(parent context.Context, key, trigger string) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(parent, 2*time.Minute)
	defer cancel()
	data, meta, err := i.store.Read(ctx, key)
	if err != nil {
		meta.Key = key
		i.pushStatus(ctx, PushInput{
			Meta:           meta,
			Trigger:        trigger,
			IngestSuccess:  false,
			IngestReason:   reasonFetch,
			IngestDuration: time.Since(start),
		})
		i.logger.Printf("ingest fetch failed key=%q trigger=%s err=%v", key, trigger, err)
		return
	}
	signature := meta.Signature()
	if i.isSeen(signature) {
		return
	}
	archive := &controlpb.StandaloneRunArchive{}
	if err := proto.Unmarshal(data, archive); err != nil {
		if i.pushStatus(ctx, PushInput{
			Meta:           meta,
			Trigger:        trigger,
			IngestSuccess:  false,
			IngestReason:   reasonDecode,
			IngestDuration: time.Since(start),
		}) == nil {
			i.markSeen(signature)
		}
		i.logger.Printf("ingest decode failed key=%q trigger=%s bytes=%d err=%v", key, trigger, len(data), err)
		return
	}
	input := PushInput{
		Archive:        archive,
		Meta:           meta,
		Trigger:        trigger,
		IngestSuccess:  true,
		IngestDuration: time.Since(start),
	}
	if err := i.pusher.Push(ctx, input); err != nil {
		i.logger.Printf("ingest push failed key=%q trigger=%s run_id=%q err=%v", key, trigger, archive.GetSummary().GetRunId(), err)
		return
	}
	i.markSeen(signature)
	i.logger.Printf("ingest ok key=%q trigger=%s run_id=%q steps=%d failed=%d duration=%s", key, trigger, archive.GetSummary().GetRunId(), archive.GetSummary().GetStepCount(), archive.GetSummary().GetFailedStepCount(), time.Since(start).Round(time.Millisecond))
}

func (i *Ingester) pushStatus(ctx context.Context, input PushInput) error {
	if err := i.pusher.Push(ctx, input); err != nil {
		i.logger.Printf("ingest status push failed key=%q reason=%s err=%v", input.Meta.Key, input.IngestReason, err)
		return err
	}
	return nil
}

func (i *Ingester) isSeen(signature string) bool {
	i.seenMu.Lock()
	defer i.seenMu.Unlock()
	_, ok := i.seen[signature]
	return ok
}

func (i *Ingester) markSeen(signature string) {
	i.seenMu.Lock()
	defer i.seenMu.Unlock()
	i.seen[signature] = struct{}{}
}
