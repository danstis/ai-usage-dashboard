package sqlite

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/danstis/ai-usage-dashboard/internal/store"
)

func TestCredentials_UpsertThenGetSecretRoundTrips(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	blob := []byte{0x01, 0xAA, 0xBB, 0xCC}
	if err := s.Upsert(ctx, "openai", "api_key", blob); err != nil {
		t.Fatalf("Upsert() returned error: %v", err)
	}

	got, err := s.GetSecret(ctx, "openai", "api_key")
	if err != nil {
		t.Fatalf("GetSecret() returned error: %v", err)
	}
	if !bytes.Equal(got, blob) {
		t.Fatalf("GetSecret() = %x, want %x", got, blob)
	}
}

func TestCredentials_UpsertOverwritesExistingValue(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	if err := s.Upsert(ctx, "openai", "api_key", []byte("v1")); err != nil {
		t.Fatalf("first Upsert() returned error: %v", err)
	}
	if err := s.Upsert(ctx, "openai", "api_key", []byte("v2")); err != nil {
		t.Fatalf("second Upsert() returned error: %v", err)
	}

	got, err := s.GetSecret(ctx, "openai", "api_key")
	if err != nil {
		t.Fatalf("GetSecret() returned error: %v", err)
	}
	if !bytes.Equal(got, []byte("v2")) {
		t.Fatalf("GetSecret() = %q, want %q", got, "v2")
	}
}

func TestCredentials_GetSecret_UnknownReturnsErrNotFound(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	if _, err := s.GetSecret(ctx, "openai", "api_key"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got: %v", err)
	}
}

func TestCredentials_Presence(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	if err := s.Upsert(ctx, "openai", "api_key", []byte("v1")); err != nil {
		t.Fatalf("Upsert() returned error: %v", err)
	}
	if err := s.Upsert(ctx, "openai", "org_id", []byte("v2")); err != nil {
		t.Fatalf("Upsert() returned error: %v", err)
	}
	if err := s.Upsert(ctx, "anthropic", "api_key", []byte("v3")); err != nil {
		t.Fatalf("Upsert() returned error: %v", err)
	}

	presence, err := s.Presence(ctx, "openai")
	if err != nil {
		t.Fatalf("Presence() returned error: %v", err)
	}
	if !presence["api_key"] || !presence["org_id"] {
		t.Fatalf("expected both openai fields present, got: %+v", presence)
	}
	if presence["unrelated"] {
		t.Fatalf("expected no presence for an unrelated field, got: %+v", presence)
	}
	if len(presence) != 2 {
		t.Fatalf("expected presence scoped to openai only (2 fields), got: %+v", presence)
	}
}

func TestCredentials_Presence_UnknownProviderReturnsEmptyMap(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	presence, err := s.Presence(ctx, "does-not-exist")
	if err != nil {
		t.Fatalf("Presence() returned error: %v", err)
	}
	if len(presence) != 0 {
		t.Fatalf("expected empty presence map, got: %+v", presence)
	}
}

func TestCredentials_DeleteClearsAllFields(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	if err := s.Upsert(ctx, "openai", "api_key", []byte("v1")); err != nil {
		t.Fatalf("Upsert() returned error: %v", err)
	}
	if err := s.Upsert(ctx, "openai", "org_id", []byte("v2")); err != nil {
		t.Fatalf("Upsert() returned error: %v", err)
	}

	if err := s.Delete(ctx, "openai"); err != nil {
		t.Fatalf("Delete() returned error: %v", err)
	}

	presence, err := s.Presence(ctx, "openai")
	if err != nil {
		t.Fatalf("Presence() returned error: %v", err)
	}
	if len(presence) != 0 {
		t.Fatalf("expected no fields present after Delete, got: %+v", presence)
	}
	if _, err := s.GetSecret(ctx, "openai", "api_key"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound after Delete, got: %v", err)
	}
}

func TestCredentials_Delete_UnknownProviderIsNoop(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	if err := s.Delete(ctx, "does-not-exist"); err != nil {
		t.Fatalf("Delete() on unknown provider returned error: %v", err)
	}
}

func TestCredentials_DeleteDoesNotAffectOtherProviders(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	if err := s.Upsert(ctx, "openai", "api_key", []byte("v1")); err != nil {
		t.Fatalf("Upsert() returned error: %v", err)
	}
	if err := s.Upsert(ctx, "anthropic", "api_key", []byte("v2")); err != nil {
		t.Fatalf("Upsert() returned error: %v", err)
	}

	if err := s.Delete(ctx, "openai"); err != nil {
		t.Fatalf("Delete() returned error: %v", err)
	}

	got, err := s.GetSecret(ctx, "anthropic", "api_key")
	if err != nil {
		t.Fatalf("GetSecret() for anthropic returned error: %v", err)
	}
	if !bytes.Equal(got, []byte("v2")) {
		t.Fatalf("GetSecret() = %q, want %q", got, "v2")
	}
}

func TestCredentials_PersistAcrossReopen(t *testing.T) {
	ctx := context.Background()
	dbPath := t.TempDir() + "/aud.db"

	s1, err := New(ctx, dbPath)
	if err != nil {
		t.Fatalf("New() returned error: %v", err)
	}
	if err := s1.Upsert(ctx, "openai", "api_key", []byte("v1")); err != nil {
		t.Fatalf("Upsert() returned error: %v", err)
	}
	if err := s1.Close(); err != nil {
		t.Fatalf("close first store: %v", err)
	}

	s2, err := New(ctx, dbPath)
	if err != nil {
		t.Fatalf("reopen New() returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := s2.Close(); err != nil {
			t.Errorf("Close() returned error: %v", err)
		}
	})

	got, err := s2.GetSecret(ctx, "openai", "api_key")
	if err != nil {
		t.Fatalf("GetSecret() after reopen returned error: %v", err)
	}
	if !bytes.Equal(got, []byte("v1")) {
		t.Fatalf("GetSecret() after reopen = %q, want %q", got, "v1")
	}
}
