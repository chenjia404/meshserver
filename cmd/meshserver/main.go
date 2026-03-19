package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"meshserver/internal/app"
	"meshserver/internal/config"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	application, err := app.New(cfg)
	if err != nil {
		log.Fatalf("create app: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := application.Start(ctx); err != nil {
		log.Fatalf("start app: %v", err)
	}

	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := application.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("shutdown app: %v", err)
	}
}
