package ingester

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/push"
)

type metricsPusher interface {
	Push(ctx context.Context, input PushInput) error
}

type pushGatewayPusher struct {
	url    string
	job    string
	client *http.Client
}

func newPushGatewayPusher(cfg Config) *pushGatewayPusher {
	return &pushGatewayPusher{
		url: cfg.PushgatewayURL,
		job: cfg.JobName,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (p *pushGatewayPusher) Push(ctx context.Context, input PushInput) error {
	registry, err := BuildRegistry(input)
	if err != nil {
		return fmt.Errorf("build metrics registry: %w", err)
	}
	pusher := push.New(p.url, p.job).
		Client(p.client).
		Gatherer(registry).
		Grouping("object_hash", input.Meta.ObjectHash())
	if runID := input.Archive.GetSummary().GetRunId(); runID != "" {
		pusher = pusher.Grouping("run_id", runID)
	}
	if err := pusher.PushContext(ctx); err != nil {
		return fmt.Errorf("push metrics: %w", err)
	}
	return nil
}
