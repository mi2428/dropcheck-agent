//go:build integration && minionotify

package ingester_test

import (
	"context"
	"log"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strings"
	"testing"
	"time"

	core "dropcheck/controller/internal/ingester"
	"github.com/minio/minio-go/v7/pkg/notification"
)

func init() {
	// Fail before TestMain can start the default Docker helper.
	minIONotifyAddress()
}

func minIONotifyAddress() string {
	if strings.TrimSpace(os.Getenv("DROPCHECK_INGESTER_INTEGRATION_MINIO_ENDPOINT")) == "" {
		log.Fatal("minionotify requires an explicitly provisioned external MinIO endpoint")
	}
	address := strings.TrimSpace(os.Getenv("DROPCHECK_INGESTER_INTEGRATION_WEBHOOK_ADDR"))
	parsed, err := netip.ParseAddrPort(address)
	if err != nil || !parsed.Addr().IsLoopback() || parsed.Port() == 0 {
		log.Fatal("minionotify requires an explicit numeric loopback webhook address with a nonzero port")
	}
	return address
}

func TestMinIOSenderAuthenticatesNotificationAndPushesMetrics(t *testing.T) {
	env := newIntegrationEnv(t, "authenticated-sender")
	ing := env.newIngester(t, "incoming")
	delivered := make(chan bool, 1)
	handler := ing.Handler()
	listener, err := net.Listen("tcp", minIONotifyAddress())
	if err != nil {
		t.Fatal("cannot bind integration webhook receiver")
	}
	server := &http.Server{
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       5 * time.Second,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			handler.ServeHTTP(w, r)
			if r.Method == http.MethodPost {
				select {
				case delivered <- len(r.Header.Values("Authorization")) == 1 && r.Header.Get("Authorization") == "Bearer "+webhookToken:
				default:
				}
			}
		}),
	}
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			t.Error(err)
		}
		if err := server.Close(); err != nil {
			t.Error(err)
		}
		if err := <-served; err != http.ErrServerClosed {
			t.Error(err)
		}
	})
	queue := notification.NewConfig(notification.NewArn("minio", "sqs", "", "INGESTER", "webhook"))
	queue.AddEvents(notification.ObjectCreatedPut)
	queue.AddFilterPrefix("incoming/")
	queue.AddFilterSuffix(".pb")
	bucketEvents := notification.Configuration{}
	bucketEvents.AddQueue(queue)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := env.client.SetBucketNotification(ctx, env.bucket, bucketEvents); err != nil {
		t.Fatalf("configure bucket notification: %v", err)
	}
	env.putArchive(t, "incoming/device/run-live-event.pb", standaloneArchiveFixture("run-live-event", "ok"))
	select {
	case authorized := <-delivered:
		if !authorized {
			t.Fatal("MinIO sender did not supply the matched bearer credential")
		}
	case <-ctx.Done():
		t.Fatal("MinIO notification did not reach the authenticated receiver before deadline")
	}
	pushes := env.pushgateway.requests()
	if len(pushes) != 1 {
		t.Fatalf("live notification pushes=%d, want 1 (no backfill was run)", len(pushes))
	}
	assertGauge(t, pushes[0].metricFamilies(t), core.MetricSuccess, nil, 1)
	assertGauge(t, pushes[0].metricFamilies(t), core.MetricDNSSuccess, map[string]string{"target": "example.com"}, 1)
}
