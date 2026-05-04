package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"dropcheck/controller/internal/ingester"
)

func main() {
	logger := log.New(os.Stdout, "dropcheck-ingester ", log.LstdFlags|log.LUTC)
	cfg, err := ingester.ConfigFromEnv()
	if err != nil {
		logger.Fatalf("config: %v", err)
	}
	app, err := ingester.New(cfg, logger)
	if err != nil {
		logger.Fatalf("init: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := app.Run(ctx); err != nil {
		logger.Fatalf("run: %v", err)
	}
}
