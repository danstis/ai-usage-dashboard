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

	"github.com/danstis/ai-usage-dashboard/internal/api"
	"github.com/danstis/ai-usage-dashboard/internal/provider"
	"github.com/danstis/ai-usage-dashboard/internal/server"
	"github.com/danstis/ai-usage-dashboard/internal/store/sqlite"
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
	if err := bootstrap(context.Background()); err != nil {
		// bootstrap() already logs the error; main() only sets the exit code.
		os.Exit(1)
	}
}

// bootstrap is the testable body of main(): it configures the default logger,
// wires SIGINT/SIGTERM to a cancellable context (derived from parent), and
// invokes run. Tests call it with an already-cancelled parent context to
// exercise the clean-shutdown return path without invoking main()'s os.Exit
// branch.
func bootstrap(parent context.Context) error {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx); err != nil {
		logger.Error("service exited with error", "error", err)
		return err
	}
	return nil
}

// run starts the HTTP server and blocks until ctx is cancelled or the
// listener returns a non-graceful error. It returns nil on a clean shutdown
// or the listener error otherwise.
func run(ctx context.Context) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	slog.Info("configuration loaded", "config", cfg)

	// Boot uses its own background context rather than ctx: ctx only signals
	// "stop serving" (SIGINT/SIGTERM) and may already be cancelled before the
	// listener ever starts (see TestRun_AlreadyCancelledContext), which must
	// not abort opening/migrating the database or reconciling providers.
	db, err := sqlite.New(context.Background(), cfg.dbPath)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			slog.Error("close store", "error", err)
		}
	}()

	providerSvc := provider.NewService(db, provider.Registry)
	if err := providerSvc.Reconcile(context.Background()); err != nil { //nolint:godre // boot-time; see comment above
		return fmt.Errorf("reconcile providers: %w", err)
	}

	httpServer := &http.Server{
		Addr:              ":" + cfg.port,
		Handler:           server.New(api.NewProviderRepository(providerSvc)),
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}

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
