package minimax

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danstis/ai-usage-dashboard/internal/plugins/plugintest"
	"github.com/danstis/ai-usage-dashboard/internal/provider"
	"github.com/danstis/ai-usage-dashboard/internal/providertest"
)

func TestMetadata_ID(t *testing.T) {
	t.Parallel()

	m := Metadata()
	if m.ID != "minimax" {
		t.Fatalf("expected Metadata().ID == %q, got %q", "minimax", m.ID)
	}
	if m.Name == "" {
		t.Errorf("expected Metadata().Name to be non-empty, got %q", m.Name)
	}
	if len(m.CredentialFields) != 1 {
		t.Fatalf("expected exactly one CredentialField, got %d: %+v", len(m.CredentialFields), m.CredentialFields)
	}
	field := m.CredentialFields[0]
	if field.Name != "subscription_key" || !field.Secret {
		t.Errorf("expected subscription_key (Secret=true), got %+v", field)
	}
}

func TestNew_UsesDefaultClientAndSameMetadata(t *testing.T) {
	t.Parallel()

	a := New().Metadata()
	b := NewWithClient(http.DefaultClient).Metadata()
	if a.ID != b.ID || a.Name != b.Name {
		t.Errorf("expected Metadata() to be stable across constructors, got %+v vs %+v", a, b)
	}
}

// fakeClock returns a deterministic time so ResetAt clamping tests don't
// flake on wall-clock drift.
func fakeClock(t time.Time) func() time.Time { return func() time.Time { return t } }

func newTestFetcher(t *testing.T, srv *httptest.Server, now time.Time) *fetcher {
	t.Helper()
	f := NewWithClient(srv.Client()).(*fetcher)
	f.endpoint = srv.URL
	f.now = fakeClock(now)
	return f
}

func TestFetchUsage_SuccessMapsToTwoWindowMetrics(t *testing.T) {
	t.Parallel()

	reset5h := time.Date(2026, 7, 15, 17, 0, 0, 0, time.UTC)
	resetWeek := time.Date(2026, 7, 20, 17, 0, 0, 0, time.UTC)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("expected Bearer test-key, got %q", got)
		}
		_, _ = io.WriteString(w, `{
  "base_resp": {"status_code": 0, "status_msg": "success"},
  "data": {
    "current_5h": {"remains": 4000, "limit": 5000, "reset_at": "2026-07-15T17:00:00Z"},
    "current_week": {"remains": 40000, "limit": 50000, "reset_at": "2026-07-20T17:00:00Z"}
  }
}`)
	}))
	defer srv.Close()

	f := newTestFetcher(t, srv, time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC))

	got, err := f.FetchUsage(context.Background(), map[string]string{"subscription_key": "test-key"})
	if err != nil {
		t.Fatalf("FetchUsage() returned error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 metrics, got %d: %+v", len(got), got)
	}

	t.Run("5h_metric_identity", func(t *testing.T) {
		assertMetric(t, got[0], metricExpectation{
			Name:   "token_plan_5h",
			Window: "5h",
			Unit:   "prompts",
			Used:   1000,
			Limit:  ptr(int64(5000)),
		})
	})

	t.Run("5h_metric_reset", func(t *testing.T) {
		if got[0].ResetAt == nil || !got[0].ResetAt.Equal(reset5h) {
			t.Errorf("metric[0] ResetAt = %v, want %v", got[0].ResetAt, reset5h)
		}
	})

	t.Run("5h_metric_remaining", func(t *testing.T) {
		if got[0].Remaining == nil || *got[0].Remaining != 4000 {
			t.Errorf("metric[0] Remaining = %v, want 4000", got[0].Remaining)
		}
	})

	t.Run("weekly_metric_identity", func(t *testing.T) {
		assertMetric(t, got[1], metricExpectation{
			Name:   "token_plan_weekly",
			Window: "week",
			Unit:   "prompts",
			Used:   10000,
			Limit:  ptr(int64(50000)),
		})
	})

	t.Run("weekly_metric_remaining", func(t *testing.T) {
		if got[1].Remaining == nil || *got[1].Remaining != 40000 {
			t.Errorf("metric[1] Remaining = %v, want 40000", got[1].Remaining)
		}
	})

	t.Run("weekly_metric_reset", func(t *testing.T) {
		if got[1].ResetAt == nil || !got[1].ResetAt.Equal(resetWeek) {
			t.Errorf("metric[1] ResetAt = %v, want %v", got[1].ResetAt, resetWeek)
		}
	})
}

// metricExpectation captures the per-metric assertions the success-path
// sub-tests reuse. ResetAt is asserted separately in its own sub-test
// because it's the only time-sensitive field and a separate scenario
// makes a failure easier to localise.
type metricExpectation struct {
	Name   string
	Window string
	Unit   string
	Used   int64
	Limit  *int64
}

// assertMetric is the shared single-scenario assertion helper for
// TestFetchUsage_SuccessMapsToTwoWindowMetrics. Kept tiny on purpose —
// every per-field check that goes through this helper stays under the
// S3776 cognitive-complexity threshold for the caller, and the helper
// itself is a flat field-by-field comparison.
func assertMetric(t *testing.T, m provider.UsageMetric, want metricExpectation) {
	t.Helper()
	if m.Name != want.Name || m.Window != want.Window {
		t.Errorf("identity mismatch: got {Name:%q Window:%q}, want {Name:%q Window:%q}", m.Name, m.Window, want.Name, want.Window)
	}
	if m.Unit != want.Unit {
		t.Errorf("Unit = %q, want %q", m.Unit, want.Unit)
	}
	if m.Used != want.Used {
		t.Errorf("Used = %d, want %d", m.Used, want.Used)
	}
	if (m.Limit == nil) != (want.Limit == nil) {
		t.Fatalf("Limit nilness mismatch: got %v, want %v", m.Limit, want.Limit)
	}
	if m.Limit != nil && *m.Limit != *want.Limit {
		t.Errorf("Limit = %d, want %d", *m.Limit, *want.Limit)
	}
}

func TestFetchUsage_HandlesMissingResetAt(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{
  "base_resp": {"status_code": 0, "status_msg": "success"},
  "data": {
    "current_5h": {"remains": 1, "limit": 2},
    "current_week": {"remains": 1, "limit": 2}
  }
}`)
	}))
	defer srv.Close()

	f := newTestFetcher(t, srv, time.Now().UTC())

	got, err := f.FetchUsage(context.Background(), map[string]string{"subscription_key": "k"})
	if err != nil {
		t.Fatalf("FetchUsage() returned error: %v", err)
	}
	if got[0].ResetAt != nil || got[1].ResetAt != nil {
		t.Errorf("expected nil ResetAt when absent from payload, got %+v", got)
	}
}

func TestFetchUsage_ClampsPastResetAtToNil(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{
  "base_resp": {"status_code": 0, "status_msg": "success"},
  "data": {
    "current_5h": {"remains": 0, "limit": 0, "reset_at": "2026-07-15T08:00:00Z"},
    "current_week": {"remains": 0, "limit": 0, "reset_at": "2026-07-14T00:00:00Z"}
  }
}`)
	}))
	defer srv.Close()

	f := newTestFetcher(t, srv, now)

	got, err := f.FetchUsage(context.Background(), map[string]string{"subscription_key": "k"})
	if err != nil {
		t.Fatalf("FetchUsage() returned error: %v", err)
	}
	if got[0].ResetAt != nil {
		t.Errorf("expected 5h ResetAt clamped to nil (already past), got %v", got[0].ResetAt)
	}
	if got[1].ResetAt != nil {
		t.Errorf("expected weekly ResetAt clamped to nil (already past), got %v", got[1].ResetAt)
	}
}

func TestFetchUsage_ClampsNegativeUsedToZero(t *testing.T) {
	t.Parallel()

	// Transient upstream inconsistency: remains > limit. We must not
	// surface a negative used; clamp at zero instead of failing the tick.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{
  "base_resp": {"status_code": 0, "status_msg": "success"},
  "data": {
    "current_5h": {"remains": 100, "limit": 50, "reset_at": "2026-07-15T17:00:00Z"},
    "current_week": {"remains": 1, "limit": 2, "reset_at": "2026-07-20T17:00:00Z"}
  }
}`)
	}))
	defer srv.Close()

	f := newTestFetcher(t, srv, time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC))

	got, err := f.FetchUsage(context.Background(), map[string]string{"subscription_key": "k"})
	if err != nil {
		t.Fatalf("FetchUsage() returned error: %v", err)
	}
	if got[0].Used != 0 {
		t.Errorf("expected 5h Used clamped to 0 (remains > limit), got %d", got[0].Used)
	}
	if got[1].Used != 1 {
		t.Errorf("expected weekly Used unchanged, got %d", got[1].Used)
	}
}

func TestFetchUsage_WrapsAuthFailureFromBaseRespStatusCode(t *testing.T) {
	t.Parallel()

	// Real Minimax auth-failure shape, captured against the live endpoint.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"base_resp":{"status_code":1004,"status_msg":"login fail: bad key"}}`)
	}))
	defer srv.Close()

	f := newTestFetcher(t, srv, time.Now().UTC())

	_, err := f.FetchUsage(context.Background(), map[string]string{"subscription_key": "bad"})
	if !errors.Is(err, provider.ErrAuth) {
		t.Fatalf("expected wrapped provider.ErrAuth, got %v", err)
	}
}

func TestFetchUsage_WrapsAuthFailureFromHTTPStatus(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()

	f := newTestFetcher(t, srv, time.Now().UTC())

	_, err := f.FetchUsage(context.Background(), map[string]string{"subscription_key": "bad"})
	if !errors.Is(err, provider.ErrAuth) {
		t.Fatalf("expected wrapped provider.ErrAuth on HTTP 401, got %v", err)
	}
}

func TestFetchUsage_PropagatesNonAuthUpstreamError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"base_resp":{"status_code":2000,"status_msg":"rate limited"}}`)
	}))
	defer srv.Close()

	f := newTestFetcher(t, srv, time.Now().UTC())

	_, err := f.FetchUsage(context.Background(), map[string]string{"subscription_key": "k"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if errors.Is(err, provider.ErrAuth) {
		t.Fatalf("non-auth upstream error must not wrap ErrAuth, got %v", err)
	}
	if !strings.Contains(err.Error(), "2000") {
		t.Errorf("expected error to surface status_code 2000, got %v", err)
	}
}

func TestFetchUsage_HTTPErrorStatus(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	f := newTestFetcher(t, srv, time.Now().UTC())

	_, err := f.FetchUsage(context.Background(), map[string]string{"subscription_key": "k"})
	if err == nil {
		t.Fatal("expected error on HTTP 500, got nil")
	}
	if errors.Is(err, provider.ErrAuth) {
		t.Fatalf("5xx must not be classified as auth, got %v", err)
	}
}

func TestFetchUsage_MalformedJSON(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{not json`)
	}))
	defer srv.Close()

	f := newTestFetcher(t, srv, time.Now().UTC())

	_, err := f.FetchUsage(context.Background(), map[string]string{"subscription_key": "k"})
	if err == nil || !strings.Contains(err.Error(), "parse response") {
		t.Fatalf("expected parse error, got %v", err)
	}
}

func TestFetchUsage_MissingCredential(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Errorf("upstream should never be called when the credential is missing")
	}))
	defer srv.Close()

	f := newTestFetcher(t, srv, time.Now().UTC())

	if _, err := f.FetchUsage(context.Background(), nil); err == nil {
		t.Fatal("expected error when credential map is nil")
	}
	if _, err := f.FetchUsage(context.Background(), map[string]string{"api_key": "x"}); err == nil {
		t.Fatal("expected error when subscription_key is missing from creds")
	}
	if _, err := f.FetchUsage(context.Background(), map[string]string{"subscription_key": ""}); err == nil {
		t.Fatal("expected error when subscription_key is empty")
	}
}

func TestFetchUsage_MissingBothFields(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{
  "base_resp": {"status_code": 0, "status_msg": "success"},
  "data": {
    "current_5h": {},
    "current_week": {"remains": 1, "limit": 2, "reset_at": "2026-07-20T17:00:00Z"}
  }
}`)
	}))
	defer srv.Close()

	f := newTestFetcher(t, srv, time.Now().UTC())

	_, err := f.FetchUsage(context.Background(), map[string]string{"subscription_key": "k"})
	if err == nil || !strings.Contains(err.Error(), "5h window") {
		t.Fatalf("expected 5h-window error, got %v", err)
	}
}

func TestFetchUsage_WeeklyWindowMissing(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{
  "base_resp": {"status_code": 0, "status_msg": "success"},
  "data": {
    "current_5h": {"remains": 1, "limit": 2, "reset_at": "2026-07-15T17:00:00Z"},
    "current_week": {}
  }
}`)
	}))
	defer srv.Close()

	f := newTestFetcher(t, srv, time.Now().UTC())

	_, err := f.FetchUsage(context.Background(), map[string]string{"subscription_key": "k"})
	if err == nil || !strings.Contains(err.Error(), "weekly window") {
		t.Fatalf("expected weekly-window error, got %v", err)
	}
}

func TestFetchUsage_TruncatesOversizedErrorBody(t *testing.T) {
	t.Parallel()

	// 1KB error body — must surface a truncated preview rather than the
	// raw blob (which can leak internals into logs).
	big := strings.Repeat("x", 1024)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, big)
	}))
	defer srv.Close()

	f := newTestFetcher(t, srv, time.Now().UTC())

	_, err := f.FetchUsage(context.Background(), map[string]string{"subscription_key": "k"})
	if err == nil {
		t.Fatal("expected error on 502")
	}
	if !strings.Contains(err.Error(), "...") {
		t.Errorf("expected truncated preview marker in error, got %q", err.Error())
	}
	if strings.Contains(err.Error(), big) {
		t.Errorf("expected error to truncate oversized body, got full payload")
	}
}

// TestStack_LiveMockShape — covered via plugintest per docs/providers.md:
// once the plugin's Fetcher is registered, FetchUsage resolves creds
// through the real credential.Service the same way Collector does. The
// HTTP parsing contract is covered by the dedicated httptest-backed unit
// tests above; this test only asserts the wiring (register + cred
// resolution + dispatch) reaches the Fetcher.
func TestStack_LiveMockShape(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	stack := plugintest.NewStack(Metadata())

	mock := providertest.NewFetcher(Metadata(), []provider.UsageMetric{
		{Name: "token_plan_5h", Window: "5h", Unit: "prompts", Used: 1000},
		{Name: "token_plan_weekly", Window: "week", Unit: "prompts", Used: 10000},
	})
	stack.Providers.RegisterFetcher(mock)

	creds, err := stack.Reveal(ctx, "minimax", map[string]string{"subscription_key": "sk-test"})
	if err != nil {
		t.Fatalf("Reveal() returned error: %v", err)
	}

	got, err := stack.Providers.FetchUsage(ctx, "minimax", creds)
	if err != nil {
		t.Fatalf("FetchUsage() returned error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 metrics, got %d: %+v", len(got), got)
	}
	if mock.LastCreds()["subscription_key"] != "sk-test" {
		t.Fatalf("expected the resolved credential to reach the Fetcher, got %+v", mock.LastCreds())
	}
}

// TestStack_ScaffoldedMissingFetcherShape — the state the plugin ships in
// before main.go is wired: the metadata-only entry is present but no
// Fetcher is registered, so FetchUsage returns ErrFetcherNotFound rather
// than silently failing.
func TestStack_ScaffoldedMissingFetcherShape(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	stack := plugintest.NewStack(Metadata())

	if stack.Providers.HasFetcher("minimax") {
		t.Fatal("expected a Stack with no registered Fetcher to be scaffolded, not live")
	}

	_, err := stack.Providers.FetchUsage(ctx, "minimax", nil)
	if !errors.Is(err, provider.ErrFetcherNotFound) {
		t.Fatalf("expected ErrFetcherNotFound for a scaffolded provider, got %v", err)
	}
}

func TestWindowQuota_ToMetric_FieldIsolation(t *testing.T) {
	t.Parallel()

	// Internal contract: a windowQuota with only remains (no limit) must
	// still produce a metric — Limit stays nil per the UsageMetric contract.
	w := windowQuota{Remains: ptr(int64(7))}
	m, err := w.toMetric("name", "window", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("toMetric() returned error: %v", err)
	}
	if m.Limit != nil {
		t.Errorf("Limit should be nil when only remains is set, got %v", *m.Limit)
	}
	if m.Remaining == nil || *m.Remaining != 7 {
		t.Errorf("Remaining should be 7, got %v", m.Remaining)
	}
}

func TestWindowQuota_ToMetric_BothFieldsMissingErrors(t *testing.T) {
	t.Parallel()

	if _, err := (windowQuota{}).toMetric("n", "w", time.Now()); err == nil {
		t.Fatal("expected error when both remains and limit are missing")
	}
}

func TestResponse_JSONRoundTrip(t *testing.T) {
	t.Parallel()

	// Guards against accidental struct rename breaking the wire contract.
	body := []byte(`{"base_resp":{"status_code":0,"status_msg":"ok"},"data":{"current_5h":{"remains":1,"limit":2,"reset_at":"2026-01-01T00:00:00Z"},"current_week":{"remains":3,"limit":4,"reset_at":"2026-01-08T00:00:00Z"}}}`)
	var r response
	if err := json.Unmarshal(body, &r); err != nil {
		t.Fatalf("Unmarshal() returned error: %v", err)
	}
	if r.BaseResp.StatusCode != 0 || r.BaseResp.StatusMsg != "ok" {
		t.Errorf("BaseResp round-trip mismatch: got %+v", r.BaseResp)
	}
	if r.Data.Current5h.Remains == nil || *r.Data.Current5h.Remains != 1 {
		t.Errorf("Current5h.Remains round-trip mismatch: got %+v", r.Data.Current5h)
	}
}

func TestIsAuthError(t *testing.T) {
	t.Parallel()

	cases := []struct {
		code int
		want bool
	}{
		{0, false},
		{999, false},
		{1004, true},
		{1999, true},
		{2000, false},
	}
	for _, tc := range cases {
		if got := isAuthError(tc.code); got != tc.want {
			t.Errorf("isAuthError(%d) = %v, want %v", tc.code, got, tc.want)
		}
	}
}

func ptr[T any](v T) *T { return &v }
