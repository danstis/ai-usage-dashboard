package main

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

func TestLoadConfig_Defaults(t *testing.T) {
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
	if cfg.masterKey != nil {
		t.Errorf("expected no master key by default, got %d bytes", len(cfg.masterKey))
	}
}

func TestLoadConfig_HTTPPortOverride(t *testing.T) {
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
	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("expected no error when AUD_MASTER_KEY is unset, got: %v", err)
	}
	if cfg.masterKey != nil {
		t.Errorf("expected nil master key, got %d bytes", len(cfg.masterKey))
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
