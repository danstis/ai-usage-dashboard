package provider

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/danstis/ai-usage-dashboard/internal/store"
)

// fakeRepo is an in-memory store.ProviderRepository test double, isolating
// Service tests from any real database.
type fakeRepo struct {
	rows map[string]store.Provider
	err  error
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{rows: map[string]store.Provider{}}
}

func (f *fakeRepo) List(_ context.Context) ([]store.Provider, error) {
	if f.err != nil {
		return nil, f.err
	}
	rows := make([]store.Provider, 0, len(f.rows))
	for _, r := range f.rows {
		rows = append(rows, r)
	}
	return rows, nil
}

func (f *fakeRepo) Get(_ context.Context, id string) (store.Provider, error) {
	if f.err != nil {
		return store.Provider{}, f.err
	}
	row, ok := f.rows[id]
	if !ok {
		return store.Provider{}, store.ErrNotFound
	}
	return row, nil
}

func (f *fakeRepo) Create(_ context.Context, id string, enabled bool) (store.Provider, error) {
	if f.err != nil {
		return store.Provider{}, f.err
	}
	now := time.Now().UTC()
	row := store.Provider{ID: id, Enabled: enabled, CreatedAt: now, UpdatedAt: now}
	f.rows[id] = row
	return row, nil
}

func (f *fakeRepo) SetEnabled(_ context.Context, id string, enabled bool) error {
	if f.err != nil {
		return f.err
	}
	row, ok := f.rows[id]
	if !ok {
		return store.ErrNotFound
	}
	row.Enabled = enabled
	f.rows[id] = row
	return nil
}

// fakeRegistry is a small fixture registry used by tests so they don't
// depend on the production Registry's exact contents.
var fakeRegistry = []Metadata{
	{
		ID:   "fake-provider",
		Name: "Fake Provider",
		CredentialFields: []CredentialField{
			{Name: "api_key", Label: "API Key", Secret: true},
		},
	},
	{ID: "fake-provider-2", Name: "Fake Provider 2"},
}

func TestService_List_MergesRegistryWithPersistedState(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	if _, err := repo.Create(context.Background(), "fake-provider", true); err != nil {
		t.Fatalf("seed Create() returned error: %v", err)
	}
	svc := NewService(repo, fakeRegistry)

	providers, err := svc.List(context.Background())
	if err != nil {
		t.Fatalf("List() returned error: %v", err)
	}
	if len(providers) != 2 {
		t.Fatalf("expected 2 providers, got %d: %+v", len(providers), providers)
	}
	if providers[0].ID != "fake-provider" || !providers[0].Enabled {
		t.Errorf("expected fake-provider enabled, got %+v", providers[0])
	}
	if providers[1].ID != "fake-provider-2" || providers[1].Enabled {
		t.Errorf("expected fake-provider-2 present and disabled (no persisted row), got %+v", providers[1])
	}
}

func TestService_List_RepositoryError(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	repo.err = errors.New("boom")
	svc := NewService(repo, fakeRegistry)

	if _, err := svc.List(context.Background()); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestService_Get_UnknownRegistryID(t *testing.T) {
	t.Parallel()

	svc := NewService(newFakeRepo(), fakeRegistry)

	_, err := svc.Get(context.Background(), "does-not-exist")
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got: %v", err)
	}
}

func TestService_Get_KnownButNoPersistedRow(t *testing.T) {
	t.Parallel()

	svc := NewService(newFakeRepo(), fakeRegistry)

	_, err := svc.Get(context.Background(), "fake-provider")
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got: %v", err)
	}
}

func TestService_Get_ReturnsMergedProvider(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	if _, err := repo.Create(context.Background(), "fake-provider", false); err != nil {
		t.Fatalf("seed Create() returned error: %v", err)
	}
	svc := NewService(repo, fakeRegistry)

	got, err := svc.Get(context.Background(), "fake-provider")
	if err != nil {
		t.Fatalf("Get() returned error: %v", err)
	}
	if got.Name != "Fake Provider" || got.Enabled {
		t.Fatalf("unexpected provider: %+v", got)
	}
}

func TestService_SetEnabled_UnknownRegistryID(t *testing.T) {
	t.Parallel()

	svc := NewService(newFakeRepo(), fakeRegistry)

	_, err := svc.SetEnabled(context.Background(), "does-not-exist", true)
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got: %v", err)
	}
}

func TestService_SetEnabled_IsIdempotent(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	if _, err := repo.Create(context.Background(), "fake-provider", false); err != nil {
		t.Fatalf("seed Create() returned error: %v", err)
	}
	svc := NewService(repo, fakeRegistry)

	for i := range 2 {
		got, err := svc.SetEnabled(context.Background(), "fake-provider", true)
		if err != nil {
			t.Fatalf("SetEnabled() call %d returned error: %v", i, err)
		}
		if !got.Enabled {
			t.Fatalf("SetEnabled() call %d: expected enabled, got %+v", i, got)
		}
	}
}

func TestService_Reconcile_CreatesMissingRows(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	svc := NewService(repo, fakeRegistry)

	if err := svc.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile() returned error: %v", err)
	}

	rows, err := repo.List(context.Background())
	if err != nil {
		t.Fatalf("List() returned error: %v", err)
	}
	if len(rows) != len(fakeRegistry) {
		t.Fatalf("expected %d rows after reconcile, got %d: %+v", len(fakeRegistry), len(rows), rows)
	}
	for _, row := range rows {
		if row.Enabled {
			t.Errorf("expected reconciled row %q to default to disabled, got enabled", row.ID)
		}
	}
}

func TestService_Reconcile_IsIdempotentAndPreservesEnabledState(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	svc := NewService(repo, fakeRegistry)

	if err := svc.Reconcile(context.Background()); err != nil {
		t.Fatalf("first Reconcile() returned error: %v", err)
	}
	if _, err := svc.SetEnabled(context.Background(), "fake-provider", true); err != nil {
		t.Fatalf("SetEnabled() returned error: %v", err)
	}

	if err := svc.Reconcile(context.Background()); err != nil {
		t.Fatalf("second Reconcile() returned error: %v", err)
	}

	got, err := svc.Get(context.Background(), "fake-provider")
	if err != nil {
		t.Fatalf("Get() returned error: %v", err)
	}
	if !got.Enabled {
		t.Fatal("expected re-running Reconcile() to preserve the enabled state, got disabled")
	}

	rows, err := repo.List(context.Background())
	if err != nil {
		t.Fatalf("List() returned error: %v", err)
	}
	if len(rows) != len(fakeRegistry) {
		t.Fatalf("expected reconcile to stay idempotent at %d rows, got %d", len(fakeRegistry), len(rows))
	}
}

func TestService_Reconcile_LeavesUnknownRowsInPlace(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	if _, err := repo.Create(context.Background(), "no-longer-in-registry", true); err != nil {
		t.Fatalf("seed Create() returned error: %v", err)
	}
	svc := NewService(repo, fakeRegistry)

	if err := svc.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile() returned error: %v", err)
	}

	row, err := repo.Get(context.Background(), "no-longer-in-registry")
	if err != nil {
		t.Fatalf("expected the unknown row to survive reconcile untouched, Get() returned error: %v", err)
	}
	if !row.Enabled {
		t.Fatalf("expected the unknown row's enabled state to be left untouched, got %+v", row)
	}
}

func TestService_Reconcile_ListError(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	repo.err = errors.New("boom")
	svc := NewService(repo, fakeRegistry)

	if err := svc.Reconcile(context.Background()); err == nil {
		t.Fatal("expected error, got nil")
	}
}
