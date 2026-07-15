package provider

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

// fakeFetcher is a deterministic in-tree Fetcher used as the reference
// implementation other P2 stages (S5 scheduler, acceptance tests) plug into.
// It returns the metrics scripted at construction time and records calls so
// tests can assert FetchUsage is invoked with the credentials the caller
// resolved from the credential store.
type fakeFetcher struct {
	meta      Metadata
	metrics   []UsageMetric
	err       error
	mu        sync.Mutex
	calls     int
	lastCreds map[string]string
}

func newFakeFetcher(meta Metadata, metrics []UsageMetric) *fakeFetcher {
	return &fakeFetcher{meta: meta, metrics: metrics}
}

func (f *fakeFetcher) Metadata() Metadata { return f.meta }

func (f *fakeFetcher) FetchUsage(_ context.Context, creds map[string]string) ([]UsageMetric, error) {
	f.mu.Lock()
	f.calls++
	f.lastCreds = creds
	f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	out := make([]UsageMetric, len(f.metrics))
	copy(out, f.metrics)
	return out, nil
}

func (f *fakeFetcher) callsCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func TestUsageMetric_FieldsAreDocumentedContract(t *testing.T) {
	t.Parallel()

	reset := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	limit := int64(100)
	remaining := int64(40)
	m := UsageMetric{
		Name:      "monthly_spend",
		Window:    "month",
		Unit:      "usd_cents",
		Used:      60,
		Limit:     &limit,
		Remaining: &remaining,
		ResetAt:   &reset,
	}
	if m.Name == "" || m.Window == "" || m.Unit == "" {
		t.Fatalf("required string fields must be non-empty, got %+v", m)
	}
	if m.Limit == nil || *m.Limit != 100 {
		t.Errorf("Limit should be 100, got %v", m.Limit)
	}
	if m.Remaining == nil || *m.Remaining != 40 {
		t.Errorf("Remaining should be 40, got %v", m.Remaining)
	}
	if m.ResetAt == nil || !m.ResetAt.Equal(reset) {
		t.Errorf("ResetAt should be %v, got %v", reset, m.ResetAt)
	}
}

func TestUsageMetric_NilPointersAllowed(t *testing.T) {
	t.Parallel()

	m := UsageMetric{
		Name:   "monthly_spend",
		Window: "month",
		Unit:   "usd_cents",
		Used:   60,
	}
	if m.Limit != nil || m.Remaining != nil || m.ResetAt != nil {
		t.Fatalf("Limit/Remaining/ResetAt must default to nil (unlimited/unknown), got %+v", m)
	}
}

func TestFakeFetcher_FetchUsageReturnsDeterministicMetrics(t *testing.T) {
	t.Parallel()

	reset := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	limit := int64(1000)
	remaining := int64(400)
	meta := Metadata{ID: "fake-fetcher", Name: "Fake Fetcher", CredentialFields: []CredentialField{
		{Name: "api_key", Label: "API Key", Secret: true},
	}}
	metrics := []UsageMetric{
		{Name: "monthly_spend", Window: "month", Unit: "usd_cents", Used: 600, Limit: &limit, Remaining: &remaining, ResetAt: &reset},
		{Name: "monthly_tokens", Window: "month", Unit: "tokens", Used: 9000},
	}
	f := newFakeFetcher(meta, metrics)

	got, err := f.FetchUsage(context.Background(), map[string]string{"api_key": "secret"})
	if err != nil {
		t.Fatalf("FetchUsage() returned error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 metrics, got %d: %+v", len(got), got)
	}

	want0 := metrics[0]
	if got[0].Name != want0.Name || got[0].Window != want0.Window || got[0].Unit != want0.Unit {
		t.Errorf("metric[0] scalar fields mismatch: got %+v", got[0])
	}
	if got[0].Used != 600 || got[0].Limit == nil || *got[0].Limit != 1000 || got[0].Remaining == nil || *got[0].Remaining != 400 {
		t.Errorf("metric[0] numeric fields mismatch: got %+v", got[0])
	}
	if got[0].ResetAt == nil || !got[0].ResetAt.Equal(reset) {
		t.Errorf("metric[0] ResetAt mismatch: got %v", got[0].ResetAt)
	}

	if got[1].Name != "monthly_tokens" || got[1].Used != 9000 {
		t.Errorf("metric[1] mismatch: got %+v", got[1])
	}
	if got[1].Limit != nil || got[1].Remaining != nil || got[1].ResetAt != nil {
		t.Errorf("metric[1] nullable fields must be nil, got %+v", got[1])
	}
}

func TestFakeFetcher_RecordsCallCredentials(t *testing.T) {
	t.Parallel()

	f := newFakeFetcher(Metadata{ID: "x"}, nil)
	creds := map[string]string{"api_key": "abc123"}
	if _, err := f.FetchUsage(context.Background(), creds); err != nil {
		t.Fatalf("FetchUsage() returned error: %v", err)
	}
	if f.callsCount() != 1 {
		t.Errorf("expected 1 call, got %d", f.callsCount())
	}
}

func TestFakeFetcher_PropagatesError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("upstream down")
	f := newFakeFetcher(Metadata{ID: "x"}, nil)
	f.err = wantErr

	got, err := f.FetchUsage(context.Background(), nil)
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected error %v, got %v", wantErr, err)
	}
	if got != nil {
		t.Errorf("expected nil metrics on error, got %+v", got)
	}
}

func TestFakeFetcher_ReturnsCopyOfMetrics(t *testing.T) {
	t.Parallel()

	original := []UsageMetric{{Name: "n", Window: "w", Unit: "u", Used: 1}}
	f := newFakeFetcher(Metadata{ID: "x"}, original)

	got, err := f.FetchUsage(context.Background(), nil)
	if err != nil {
		t.Fatalf("FetchUsage() returned error: %v", err)
	}
	got[0].Used = 999

	if original[0].Used != 1 {
		t.Errorf("fake provider leaked mutation back into scripted metrics; original=%+v", original)
	}
}

func TestMetadataRegistry_StillCarriesCredentialFields(t *testing.T) {
	t.Parallel()

	if len(Registry) == 0 {
		t.Fatal("Registry must list at least one provider for P2 acceptance")
	}
	for _, m := range Registry {
		if m.ID == "" || m.Name == "" {
			t.Errorf("registry entry missing id/name: %+v", m)
		}
		if len(m.CredentialFields) == 0 {
			t.Errorf("registry entry %q has no CredentialFields; P1 API contract depends on them", m.ID)
		}
	}
}

func TestRuntimeRegistry_RegisterAndLookup(t *testing.T) {
	t.Parallel()

	meta := Metadata{ID: "fake-registry", Name: "Fake Registry", CredentialFields: []CredentialField{
		{Name: "api_key", Label: "API Key", Secret: true},
	}}
	reg := newRuntimeRegistry()

	reg.register(meta.ID, newFakeFetcher(meta, nil))

	f, err := reg.lookup(meta.ID)
	if err != nil {
		t.Fatalf("lookup() returned error: %v", err)
	}
	if f.Metadata().ID != meta.ID {
		t.Errorf("lookup returned wrong metadata: got %+v", f.Metadata())
	}
}

func TestRuntimeRegistry_UnknownIDReturnsErrFetcherNotFound(t *testing.T) {
	t.Parallel()

	reg := newRuntimeRegistry()
	_, err := reg.lookup("not-registered")
	if !errors.Is(err, ErrFetcherNotFound) {
		t.Fatalf("expected ErrFetcherNotFound, got %v", err)
	}
}

func TestRuntimeRegistry_ReregisteringSameIDPanics(t *testing.T) {
	t.Parallel()

	meta := Metadata{ID: "dup", Name: "Dup"}
	reg := newRuntimeRegistry()
	reg.register(meta.ID, newFakeFetcher(meta, nil))

	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on duplicate register, got none")
		}
	}()
	reg.register(meta.ID, newFakeFetcher(meta, nil))
}

func TestService_RegisterFetcherPanicsOnUnknownID(t *testing.T) {
	t.Parallel()

	svc := NewService(newFakeRepo(), fakeRegistry)

	defer func() {
		if recover() == nil {
			t.Fatal("expected panic when registering a Fetcher whose id is not in the metadata registry, got none")
		}
	}()
	svc.RegisterFetcher(newFakeFetcher(Metadata{ID: "typo-id"}, nil))
}

func TestService_PollingFailsWhenNoFetcherRegistered(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	if _, err := repo.Create(context.Background(), "fake-provider", true); err != nil {
		t.Fatalf("seed Create() returned error: %v", err)
	}
	svc := NewService(repo, fakeRegistry)

	if _, err := svc.FetchUsage(context.Background(), "fake-provider", nil); !errors.Is(err, ErrFetcherNotFound) {
		t.Fatalf("expected ErrFetcherNotFound for metadata-only provider, got %v", err)
	}
}

func TestService_FetchUsageUsesRegisteredFetcher(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	svc := NewService(repo, fakeRegistry)

	meta := fakeRegistry[0]
	want := []UsageMetric{{Name: "n", Window: "w", Unit: "u", Used: 1}}
	f := newFakeFetcher(meta, want)
	svc.RegisterFetcher(f)

	got, err := svc.FetchUsage(context.Background(), meta.ID, map[string]string{"api_key": "x"})
	if err != nil {
		t.Fatalf("FetchUsage() returned error: %v", err)
	}
	if len(got) != 1 || got[0].Name != "n" || got[0].Used != 1 {
		t.Fatalf("unexpected metrics: %+v", got)
	}
}

func TestService_FetchUsageUnknownProvider(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	svc := NewService(repo, fakeRegistry)

	_, err := svc.FetchUsage(context.Background(), "does-not-exist", nil)
	if !errors.Is(err, ErrFetcherNotFound) {
		t.Fatalf("expected ErrFetcherNotFound, got %v", err)
	}
}

func TestService_FetchUsagePropagatesFetcherError(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	svc := NewService(repo, fakeRegistry)

	meta := fakeRegistry[0]
	upstream := errors.New("network down")
	f := newFakeFetcher(meta, nil)
	f.err = upstream
	svc.RegisterFetcher(f)

	_, err := svc.FetchUsage(context.Background(), meta.ID, nil)
	if !errors.Is(err, upstream) {
		t.Fatalf("expected upstream error, got %v", err)
	}
}

func TestService_HasFetcher(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	svc := NewService(repo, fakeRegistry)

	registered := fakeRegistry[0]
	svc.RegisterFetcher(newFakeFetcher(registered, nil))

	if !svc.HasFetcher(registered.ID) {
		t.Errorf("HasFetcher(%q) = false, want true for a registered id", registered.ID)
	}
	scaffolded := fakeRegistry[1]
	if svc.HasFetcher(scaffolded.ID) {
		t.Errorf("HasFetcher(%q) = true, want false for a scaffolded id with no Fetcher", scaffolded.ID)
	}
	if svc.HasFetcher("does-not-exist") {
		t.Error("HasFetcher(unknown id) = true, want false")
	}
}

func TestErrAuth_IsDetectable(t *testing.T) {
	t.Parallel()

	wrapped := fmt.Errorf("fetch: %w", ErrAuth)
	if !errors.Is(wrapped, ErrAuth) {
		t.Fatalf("expected errors.Is(wrapped, ErrAuth) to be true, got false for %v", wrapped)
	}
}
