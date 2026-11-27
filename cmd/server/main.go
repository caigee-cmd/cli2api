package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/caigee-cmd/cli2api/internal/app"
	"github.com/caigee-cmd/cli2api/internal/config"
	"github.com/caigee-cmd/cli2api/internal/netx"
	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load()
	// Provider clients that leave Transport nil share this process-wide pool.
	// Install the tuned transport before app.New constructs any client.
	http.DefaultTransport = netx.Transport()
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}
	application := app.New(cfg)
	defer application.Close()
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           application.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		// Bound how long a client may take to deliver a request body. This
		// covers slow uploads without touching response writes, so SSE and
		// long-running completions keep streaming after the request is read.
		ReadTimeout:    10 * time.Minute,
		IdleTimeout:    120 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		log.Printf("cli2api listening on http://%s", addr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("http server failed: %v", err)
			stop()
		}
	}()
	<-ctx.Done()
	// Stop reporting ready and reject new work before waiting for active
	// requests. Existing streams keep their slots; the listener closes when
	// Shutdown starts, and the middleware returns 503 to anything that still
	// reaches it through a keep-alive connection.
	if application.HTTP != nil {
		application.HTTP.SetDraining(true)
	}
	const shutdownGrace = 30 * time.Second
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("http shutdown did not finish within %s: %v", shutdownGrace, err)
		// Force-close the remaining connections so the process exits cleanly
		// instead of leaving handlers and their upstream streams running.
		_ = httpServer.Close()
	}
}
