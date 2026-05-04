package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"syscall"

	"dropcheck/controller/internal/ingester"
)

func main() {
	logger := log.New(os.Stdout, "", log.LstdFlags|log.Lmicroseconds)
	cfg, err := ingester.ConfigFromEnv()
	if err != nil {
		logger.Fatalf("config: %v", err)
	}
	store, err := ingester.NewMinIOStore(cfg)
	if err != nil {
		logger.Fatalf("minio: %v", err)
	}
	app := ingester.New(cfg, store, ingester.NewPushgatewayPusher(cfg.PushgatewayURL, cfg.PushJob), logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := app.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		logger.Fatalf("run: %v", err)
	}
}
