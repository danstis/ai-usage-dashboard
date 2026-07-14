package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/danstis/ai-usage-dashboard/internal/provider"
)

// setValidMasterKey sets AUD_MASTER_KEY to a well-formed (all-zero) 32-byte
// key so tests that don't care about master-key behaviour can still reach
// loadConfig's other branches now that the key is required at boot.
func setValidMasterKey(t *testing.T) {
	t.Helper()
	t.Setenv("AUD_MASTER_KEY", base64.StdEncoding.EncodeToString(make([]byte, masterKeyLen)))
}

func TestLoadConfig_Defaults(t *testing.T) {
	setValidMasterKey(t)

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if cfg.port != defaultPort {
		t.Errorf("expected default port %q, got %q", defaultPort, cfg.port)
	}
	if cfg.pollInterval != defaultPollInterval {
		t.Errorf("expected default poll interval %v, got %v", defaultPollInterval, cfg.pollInterval)
	}
	if cfg.dbPath != defaultDBPath {
		t.Errorf("expected default db path %q, got %q", defaultDBPath, cfg.dbPath)
	}
	if len(cfg.masterKey) != masterKeyLen {
		t.Errorf("expected a %d-byte master key, got %d bytes", masterKeyLen, len(cfg.masterKey))
	}
}

func TestLoadConfig_HTTPPortOverride(t *testing.T) {
	setValidMasterKey(t)
	t.Setenv("AUD_HTTP_PORT", "9090")

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if cfg.port != "9090" {
		t.Errorf("expected port %q, got %q", "9090", cfg.port)
	}
}

func TestLoadConfig_DBPathOverride(t *testing.T) {
	setValidMasterKey(t)
	t.Setenv("AUD_DB_PATH", "/tmp/custom.db")

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if cfg.dbPath != "/tmp/custom.db" {
		t.Errorf("expected db path %q, got %q", "/tmp/custom.db", cfg.dbPath)
	}
}

func TestLoadConfig_PollIntervalOverride(t *testing.T) {
	setValidMasterKey(t)
	t.Setenv("AUD_POLL_INTERVAL", "10s")

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if cfg.pollInterval != 10*time.Second {
		t.Errorf("expected poll interval %v, got %v", 10*time.Second, cfg.pollInterval)
	}
}

func TestLoadConfig_PollIntervalInvalid(t *testing.T) {
	t.Setenv("AUD_POLL_INTERVAL", "not-a-duration")

	_, err := loadConfig()
	if err == nil {
		t.Fatal("expected an error for an invalid AUD_POLL_INTERVAL, got nil")
	}
}

func TestLoadConfig_MasterKeyAbsent(t *testing.T) {
	_, err := loadConfig()
	if err == nil {
		t.Fatal("expected an error when AUD_MASTER_KEY is unset, got nil")
	}
}

func TestLoadConfig_MasterKeyValid(t *testing.T) {
	key := make([]byte, masterKeyLen)
	t.Setenv("AUD_MASTER_KEY", base64.StdEncoding.EncodeToString(key))

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if len(cfg.masterKey) != masterKeyLen {
		t.Errorf("expected master key of %d bytes, got %d", masterKeyLen, len(cfg.masterKey))
	}
}

func TestLoadConfig_MasterKeyInvalidEncoding(t *testing.T) {
	t.Setenv("AUD_MASTER_KEY", "not-valid-base64!!!")

	_, err := loadConfig()
	if err == nil {
		t.Fatal("expected an error for invalid AUD_MASTER_KEY encoding, got nil")
	}
	if strings.Contains(err.Error(), "not-valid-base64!!!") {
		t.Fatalf("error must not contain the raw key value, got: %v", err)
	}
}

func TestLoadConfig_MasterKeyWrongLength(t *testing.T) {
	key := make([]byte, 16)
	t.Setenv("AUD_MASTER_KEY", base64.StdEncoding.EncodeToString(key))

	_, err := loadConfig()
	if err == nil {
		t.Fatal("expected an error for a wrong-length AUD_MASTER_KEY, got nil")
	}
}

// freePort returns a TCP port that was free at the moment of the call. The
// listener is closed immediately; a small race window exists before the test
// re-binds it, which is acceptable for in-process test isolation.
func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen on free port: %v", err)
	}
	addr := l.Addr().(*net.TCPAddr)
	port := strconv.Itoa(addr.Port)
	if err := l.Close(); err != nil {
		t.Fatalf("close free-port listener: %v", err)
	}
	return port
}

// waitForServer polls addr until a successful response is observed or the
// timeout elapses. Used to synchronize the test goroutine with the listener
// inside run().
func waitForServer(t *testing.T, addr string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	url := "http://" + addr + "/healthz"
	for time.Now().Before(deadline) {
		resp, err := http.Get(url) //nolint:gosec // test-only loopback call
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("server at %s did not become ready within %s", addr, timeout)
}

func TestConfig_LogValue_RedactsKey(t *testing.T) {
	key := make([]byte, masterKeyLen)
	for i := range key {
		key[i] = byte(i)
	}
	cfg := config{
		port:         "9090",
		masterKey:    key,
		pollInterval: 30 * time.Second,
		dbPath:       "/tmp/test.db",
	}

	rendered := cfg.LogValue().String()

	if strings.Contains(rendered, base64.StdEncoding.EncodeToString(key)) {
		t.Errorf("LogValue must not contain the encoded master key, got: %s", rendered)
	}
	for _, b := range key {
		if b == 0 {
			continue
		}
		if strings.ContainsRune(rendered, rune(b)) {
			t.Errorf("LogValue must not contain raw key byte %d, got: %s", b, rendered)
		}
	}
	if !strings.Contains(rendered, "port=9090") {
		t.Errorf("expected port field in LogValue, got: %s", rendered)
	}
	if !strings.Contains(rendered, "masterKeySet=true") {
		t.Errorf("expected masterKeySet=true in LogValue, got: %s", rendered)
	}
	if !strings.Contains(rendered, "pollInterval=30s") {
		t.Errorf("expected pollInterval=30s in LogValue, got: %s", rendered)
	}
	if !strings.Contains(rendered, "dbPath=/tmp/test.db") {
		t.Errorf("expected dbPath in LogValue, got: %s", rendered)
	}
}

func TestConfig_LogValue_KeyAbsent(t *testing.T) {
	cfg := config{
		port:         defaultPort,
		masterKey:    nil,
		pollInterval: defaultPollInterval,
		dbPath:       defaultDBPath,
	}

	rendered := cfg.LogValue().String()

	if !strings.Contains(rendered, "masterKeySet=false") {
		t.Errorf("expected masterKeySet=false in LogValue, got: %s", rendered)
	}
	if strings.Contains(rendered, "masterKeySet=true") {
		t.Errorf("did not expect masterKeySet=true for nil key, got: %s", rendered)
	}
}

func TestConfig_LogValue_EmptyKeyStillFalse(t *testing.T) {
	cfg := config{
		port:      defaultPort,
		masterKey: []byte{},
	}

	rendered := cfg.LogValue().String()

	if !strings.Contains(rendered, "masterKeySet=false") {
		t.Errorf("expected masterKeySet=false for empty key slice, got: %s", rendered)
	}
}

func TestConfig_LogValue_StructLogRender(t *testing.T) {
	cfg := config{
		port:         "1234",
		pollInterval: 7 * time.Minute,
		dbPath:       "/var/data.db",
	}

	var buf strings.Builder
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	logger.Info("config", "config", cfg)

	out := buf.String()
	if !strings.Contains(out, "port=1234") {
		t.Errorf("expected port in slog output, got: %s", out)
	}
	if !strings.Contains(out, "pollInterval=7m0s") {
		t.Errorf("expected pollInterval in slog output, got: %s", out)
	}
	if !strings.Contains(out, "dbPath=/var/data.db") {
		t.Errorf("expected dbPath in slog output, got: %s", out)
	}
}

func TestRun_ConfigErrorReturnsEarly(t *testing.T) {
	t.Setenv("AUD_MASTER_KEY", "not-valid-base64!!!")

	err := run(context.Background())
	if err == nil {
		t.Fatal("expected run() to return error when AUD_MASTER_KEY is invalid, got nil")
	}
	if strings.Contains(err.Error(), "not-valid-base64!!!") {
		t.Fatalf("error must not echo the raw key value, got: %v", err)
	}
}

func TestRun_StoreOpenErrorReturnsEarly(t *testing.T) {
	setValidMasterKey(t)
	// A db path nested under a regular file can never have its parent
	// directory created, forcing sqlite.New to fail before the listener
	// starts.
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("create blocker file: %v", err)
	}
	t.Setenv("AUD_DB_PATH", filepath.Join(blocker, "nested", "aud.db"))

	err := run(context.Background())
	if err == nil {
		t.Fatal("expected run() to return error when the store fails to open, got nil")
	}
}

func TestRun_ListenErrorReturnsError(t *testing.T) {
	setValidMasterKey(t)
	t.Setenv("AUD_HTTP_PORT", "not-a-valid-port-format-at-all")
	t.Setenv("AUD_DB_PATH", filepath.Join(t.TempDir(), "aud.db"))

	err := run(context.Background())
	if err == nil {
		t.Fatal("expected run() to return error when the listener fails to bind, got nil")
	}
}

func TestRun_GracefulShutdown(t *testing.T) {
	port := freePort(t)
	t.Setenv("AUD_HTTP_PORT", port)
	t.Setenv("AUD_MASTER_KEY", base64.StdEncoding.EncodeToString(make([]byte, masterKeyLen)))
	t.Setenv("AUD_DB_PATH", filepath.Join(t.TempDir(), "aud.db"))

	ctx, cancel := context.WithCancel(context.Background())
	runErr := make(chan error, 1)
	go func() {
		runErr <- run(ctx)
	}()

	waitForServer(t, "127.0.0.1:"+port, 5*time.Second)

	cancel()

	select {
	case err := <-runErr:
		if err != nil {
			t.Fatalf("run() returned error on clean shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("run() did not return within 5s of context cancellation")
	}
}

// TestRun_ReconcilesAndServesProviders is the P1 acceptance path exercised
// end-to-end through run(): boot reconciles the compiled-in registry into a
// fresh store, and GET /api/v1/providers lists every seeded provider,
// disabled by default.
func TestRun_ReconcilesAndServesProviders(t *testing.T) {
	setValidMasterKey(t)
	port := freePort(t)
	t.Setenv("AUD_HTTP_PORT", port)
	t.Setenv("AUD_DB_PATH", filepath.Join(t.TempDir(), "aud.db"))

	ctx, cancel := context.WithCancel(context.Background())
	runErr := make(chan error, 1)
	go func() {
		runErr <- run(ctx)
	}()

	waitForServer(t, "127.0.0.1:"+port, 5*time.Second)

	resp, err := http.Get("http://127.0.0.1:" + port + "/api/v1/providers") //nolint:gosec // test-only loopback call
	if err != nil {
		t.Fatalf("GET /api/v1/providers: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, resp.StatusCode)
	}

	var providers []struct {
		ID      string `json:"id"`
		Enabled bool   `json:"enabled"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&providers); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(providers) != len(provider.Registry) {
		t.Fatalf("expected %d seeded providers, got %d: %+v", len(provider.Registry), len(providers), providers)
	}
	for _, p := range providers {
		if p.Enabled {
			t.Errorf("expected provider %q to default to disabled, got enabled", p.ID)
		}
	}

	cancel()
	select {
	case err := <-runErr:
		if err != nil {
			t.Fatalf("run() returned error on shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("run() did not return within 5s of context cancellation")
	}
}

func TestRun_AlreadyCancelledContext(t *testing.T) {
	setValidMasterKey(t)
	t.Setenv("AUD_HTTP_PORT", freePort(t))
	t.Setenv("AUD_DB_PATH", filepath.Join(t.TempDir(), "aud.db"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan error, 1)
	go func() {
		done <- run(ctx)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run() returned error when started with already-cancelled context: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("run() did not return within 5s for an already-cancelled context")
	}
}

// TestBootstrap_ConfigErrorReturnsBeforeServer exercises the main() body
// without invoking main() (which calls os.Exit on error and would tear down
// the test process). It verifies that bootstrap() returns the underlying
// loadConfig() error rather than silently starting a listener.
func TestBootstrap_ConfigErrorReturnsBeforeServer(t *testing.T) {
	t.Setenv("AUD_MASTER_KEY", "not-valid-base64!!!")

	err := bootstrap(context.Background())
	if err == nil {
		t.Fatal("expected bootstrap() to return error when config is invalid, got nil")
	}
	if strings.Contains(err.Error(), "not-valid-base64!!!") {
		t.Fatalf("error must not echo the raw key value, got: %v", err)
	}
}

// TestBootstrap_CleanShutdownReturnsNil exercises the happy path of
// bootstrap() by passing a pre-cancelled parent context. The signal-aware
// child context is therefore immediately done, run() tears the listener down
// cleanly, and bootstrap() returns nil — the same path main() takes after a
// SIGINT/SIGTERM.
func TestBootstrap_CleanShutdownReturnsNil(t *testing.T) {
	setValidMasterKey(t)
	t.Setenv("AUD_HTTP_PORT", freePort(t))
	t.Setenv("AUD_DB_PATH", filepath.Join(t.TempDir(), "aud.db"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := bootstrap(ctx)
	if err != nil {
		t.Fatalf("expected bootstrap() to return nil after clean shutdown, got: %v", err)
	}
}
