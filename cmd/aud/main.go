// Package main starts the AI Usage Dashboard HTTP service.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/danstis/ai-usage-dashboard/internal/server"
)

const (
	defaultHTTPPort = "8080"
	shutdownTimeout = 10 * time.Second
)

type config struct {
	addr string
}

func main() {
	if err := run(); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg := loadConfig()
	httpServer := &http.Server{
		Addr:              cfg.addr,
		Handler:           server.New(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	errs := make(chan error, 1)
	go func() {
		slog.Info("starting HTTP server", "addr", cfg.addr)
		errs <- httpServer.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown server: %w", err)
		}
		slog.Info("HTTP server stopped")
		return nil
	case err := <-errs:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("listen and serve: %w", err)
	}
}

func loadConfig() config {
	port := strings.TrimSpace(os.Getenv("AUD_HTTP_PORT"))
	if port == "" {
		port = defaultHTTPPort
	}

	return config{addr: ":" + port}
}
