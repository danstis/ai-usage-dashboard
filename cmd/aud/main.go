// Command aud runs the AI Usage Dashboard HTTP service.
package main

import (
	"context"
	"encoding/base64"
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
	defaultPort         = "8080"
	defaultPollInterval = 5 * time.Minute
	defaultDBPath       = "./data/aud.db"
	masterKeyLen        = 32 // AES-256
	readHeaderTimeout   = 5 * time.Second
	readTimeout         = 10 * time.Second
	writeTimeout        = 10 * time.Second
	idleTimeout         = 60 * time.Second
	shutdownTimeout     = 10 * time.Second
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	if err := run(); err != nil {
		logger.Error("service exited with error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	slog.Info("configuration loaded", "config", cfg)

	httpServer := &http.Server{
		Addr:              ":" + cfg.port,
		Handler:           server.New(),
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	serveErr := make(chan error, 1)
	go func() {
		slog.Info("starting server", "addr", httpServer.Addr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
			return
		}
		serveErr <- nil
	}()

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
		slog.Info("shutdown signal received")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		return err
	}

	slog.Info("server stopped cleanly")
	return nil
}

type config struct {
	port         string
	masterKey    []byte
	pollInterval time.Duration
	dbPath       string
}

// LogValue redacts the master key so config is never logged with the raw
// key bytes, even if a caller logs the struct directly (gosec).
func (c config) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("port", c.port),
		slog.Bool("masterKeySet", len(c.masterKey) > 0),
		slog.Duration("pollInterval", c.pollInterval),
		slog.String("dbPath", c.dbPath),
	)
}

func loadConfig() (config, error) {
	cfg := config{
		port:         defaultPort,
		pollInterval: defaultPollInterval,
		dbPath:       defaultDBPath,
	}

	if v := strings.TrimSpace(os.Getenv("AUD_HTTP_PORT")); v != "" {
		cfg.port = v
	}

	if v := strings.TrimSpace(os.Getenv("AUD_DB_PATH")); v != "" {
		cfg.dbPath = v
	}

	if v := strings.TrimSpace(os.Getenv("AUD_POLL_INTERVAL")); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return config{}, fmt.Errorf("parse AUD_POLL_INTERVAL: %w", err)
		}
		cfg.pollInterval = d
	}

	if v := os.Getenv("AUD_MASTER_KEY"); v != "" {
		key, err := base64.StdEncoding.DecodeString(v)
		if err != nil {
			return config{}, errors.New("AUD_MASTER_KEY: invalid base64 encoding")
		}
		if len(key) != masterKeyLen {
			return config{}, fmt.Errorf("AUD_MASTER_KEY: must decode to %d bytes, got %d", masterKeyLen, len(key))
		}
		cfg.masterKey = key
	}

	return cfg, nil
}
