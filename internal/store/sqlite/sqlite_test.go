package sqlite

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/danstis/ai-usage-dashboard/internal/store"
)

func TestNew_CreatesFileAndMigrates(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "aud.db")

	s, err := New(ctx, dbPath)
	if err != nil {
		t.Fatalf("New() returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("Close() returned error: %v", err)
		}
	})

	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("expected db file at %s: %v", dbPath, err)
	}

	providers, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List() returned error on freshly migrated db: %v", err)
	}
	if len(providers) != 0 {
		t.Fatalf("expected no rows on a fresh db, got %d", len(providers))
	}
}

func TestNew_MigrateIsIdempotent(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "aud.db")

	s1, err := New(ctx, dbPath)
	if err != nil {
		t.Fatalf("first New() returned error: %v", err)
	}
	if err := s1.Close(); err != nil {
		t.Fatalf("close first store: %v", err)
	}

	s2, err := New(ctx, dbPath)
	if err != nil {
		t.Fatalf("second New() against an already-migrated file returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := s2.Close(); err != nil {
			t.Errorf("Close() returned error: %v", err)
		}
	})
}

func TestNew_CreatesParentDirectory(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "nested", "dir", "aud.db")

	s, err := New(ctx, dbPath)
	if err != nil {
		t.Fatalf("New() returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("Close() returned error: %v", err)
		}
	})

	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("expected db file at %s: %v", dbPath, err)
	}
}

func newTestStore(t *testing.T) store.Store {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "aud.db")
	s, err := New(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("New() returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("Close() returned error: %v", err)
		}
	})
	return s
}

func TestCreateGetList(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	created, err := s.Create(ctx, "openai", false)
	if err != nil {
		t.Fatalf("Create() returned error: %v", err)
	}
	if created.ID != "openai" || created.Enabled {
		t.Fatalf("unexpected created row: %+v", created)
	}
	if created.CreatedAt.IsZero() || created.UpdatedAt.IsZero() {
		t.Fatalf("expected non-zero timestamps, got: %+v", created)
	}

	got, err := s.Get(ctx, "openai")
	if err != nil {
		t.Fatalf("Get() returned error: %v", err)
	}
	if got.ID != "openai" || got.Enabled {
		t.Fatalf("unexpected row from Get(): %+v", got)
	}
	if !got.CreatedAt.Equal(created.CreatedAt) {
		t.Errorf("expected CreatedAt %v, got %v", created.CreatedAt, got.CreatedAt)
	}

	if _, err := s.Create(ctx, "anthropic", true); err != nil {
		t.Fatalf("Create() returned error: %v", err)
	}

	list, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List() returned error: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 rows, got %d: %+v", len(list), list)
	}
	if list[0].ID != "anthropic" || list[1].ID != "openai" {
		t.Fatalf("expected rows ordered by id, got: %+v", list)
	}
	if !list[0].Enabled {
		t.Errorf("expected anthropic to be enabled, got: %+v", list[0])
	}
}

func TestGet_MissingID_ReturnsErrNotFound(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	_, err := s.Get(ctx, "does-not-exist")
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got: %v", err)
	}
}

func TestSetEnabled_UnknownID_ReturnsErrNotFound(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	err := s.SetEnabled(ctx, "does-not-exist", true)
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got: %v", err)
	}
}

func TestSetEnabled_PersistsAcrossReopen(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "aud.db")

	s1, err := New(ctx, dbPath)
	if err != nil {
		t.Fatalf("New() returned error: %v", err)
	}
	if _, err := s1.Create(ctx, "openai", false); err != nil {
		t.Fatalf("Create() returned error: %v", err)
	}
	if err := s1.SetEnabled(ctx, "openai", true); err != nil {
		t.Fatalf("SetEnabled() returned error: %v", err)
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

	got, err := s2.Get(ctx, "openai")
	if err != nil {
		t.Fatalf("Get() after reopen returned error: %v", err)
	}
	if !got.Enabled {
		t.Fatalf("expected enabled state to persist across reopen, got: %+v", got)
	}
	if !got.UpdatedAt.After(got.CreatedAt) && !got.UpdatedAt.Equal(got.CreatedAt) {
		t.Errorf("expected UpdatedAt >= CreatedAt, got created=%v updated=%v", got.CreatedAt, got.UpdatedAt)
	}
}

func TestSetEnabled_Toggle(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	if _, err := s.Create(ctx, "openai", true); err != nil {
		t.Fatalf("Create() returned error: %v", err)
	}

	if err := s.SetEnabled(ctx, "openai", false); err != nil {
		t.Fatalf("SetEnabled(false) returned error: %v", err)
	}
	got, err := s.Get(ctx, "openai")
	if err != nil {
		t.Fatalf("Get() returned error: %v", err)
	}
	if got.Enabled {
		t.Fatalf("expected disabled after SetEnabled(false), got: %+v", got)
	}

	if err := s.SetEnabled(ctx, "openai", true); err != nil {
		t.Fatalf("SetEnabled(true) returned error: %v", err)
	}
	got, err = s.Get(ctx, "openai")
	if err != nil {
		t.Fatalf("Get() returned error: %v", err)
	}
	if !got.Enabled {
		t.Fatalf("expected enabled after SetEnabled(true), got: %+v", got)
	}
}

func TestNew_RejectsUnwritablePath(t *testing.T) {
	ctx := context.Background()
	// A path nested under a regular file (not a directory) can never be
	// created, which exercises the MkdirAll error branch deterministically
	// regardless of the process's filesystem permissions.
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("create blocker file: %v", err)
	}

	_, err := New(ctx, filepath.Join(blocker, "nested", "aud.db"))
	if err == nil {
		t.Fatal("expected error when db directory cannot be created, got nil")
	}
}

func TestNew_ContextCanceledBeforeOpen(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	dbPath := filepath.Join(t.TempDir(), "aud.db")
	_, err := New(ctx, dbPath)
	if err == nil {
		t.Fatal("expected error for an already-cancelled context, got nil")
	}
}

func TestClose_IsIdempotentSafeOnce(t *testing.T) {
	// Close is documented to be called exactly once by callers (main.go's
	// defer); this test only asserts the first call succeeds cleanly.
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "aud.db")
	s, err := New(ctx, dbPath)
	if err != nil {
		t.Fatalf("New() returned error: %v", err)
	}
	start := time.Now()
	if err := s.Close(); err != nil {
		t.Fatalf("Close() returned error: %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("Close() took too long")
	}
}
