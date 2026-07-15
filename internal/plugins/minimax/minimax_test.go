package minimax

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danstis/ai-usage-dashboard/internal/provider"
)

// TestMetadata_MatchesRegistryContract is a guard that the package-level
// Metadata() value stays byte-identical to the registry entry added in
// internal/provider/provider.go. RegisterFetcher panics if they diverge,
// and this test makes the same check at unit time without booting a
// full provider.Service.
func TestMetadata_MatchesRegistryContract(t *testing.T) {
	t.Parallel()

	want := provider.Metadata{
		ID:   "minimax",
		Name: "Minimax Token Plan",
		CredentialFields: []provider.CredentialField{
			{Name: "subscription_key", Label: "Subscription Key", Secret: true},
		},
	}

	got := (&Fetcher{}).Metadata()
	if got.ID != want.ID {
		t.Errorf("ID: got %q, want %q", got.ID, want.ID)
	}
	if got.Name != want.Name {
		t.Errorf("Name: got %q, want %q", got.Name, want.Name)
	}
	if len(got.CredentialFields) != len(want.CredentialFields) {
		t.Fatalf("CredentialFields length: got %d, want %d", len(got.CredentialFields), len(want.CredentialFields))
	}
	for i := range want.CredentialFields {
		if got.CredentialFields[i] != want.CredentialFields[i] {
			t.Errorf("CredentialFields[%d]: got %+v, want %+v", i, got.CredentialFields[i], want.CredentialFields[i])
		}
	}
}

// TestFetchUsage_Auth401WrapsErrAuth locks in the documented contract
// that an upstream 401 surfaces as a wrapped provider.ErrAuth so the
// scheduler's auth-cooldown engages (see docs/providers.md).
func TestFetchUsage_Auth401WrapsErrAuth(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid key"}`))
	}))
	t.Cleanup(srv.Close)

	f := newWithClient(srv.URL, &http.Client{})
	creds := map[string]string{"subscription_key": "sk-test"}

	_, err := f.FetchUsage(context.Background(), creds)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, provider.ErrAuth) {
		t.Fatalf("expected wrapped provider.ErrAuth, got %v", err)
	}
}

// TestFetchUsage_Auth403WrapsErrAuth mirrors TestFetchUsage_Auth401WrapsErrAuth
// for the 403 path. Both must be detected as auth failure so the
// scheduler's cooldown engages regardless of which the upstream sends.
func TestFetchUsage_Auth403WrapsErrAuth(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`forbidden`))
	}))
	t.Cleanup(srv.Close)

	f := newWithClient(srv.URL, &http.Client{})
	creds := map[string]string{"subscription_key": "sk-test"}

	_, err := f.FetchUsage(context.Background(), creds)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, provider.ErrAuth) {
		t.Fatalf("expected wrapped provider.ErrAuth, got %v", err)
	}
}

// TestFetchUsage_BearerHeaderSent pins the documented auth scheme:
// Authorization: Bearer <Subscription Key>. The captured request header
// is the only way the test can verify the credential is wired
// correctly without holding a real key.
func TestFetchUsage_BearerHeaderSent(t *testing.T) {
	t.Parallel()

	const want = "sk-test-abc123"
	var got string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`)) // schema not recognised; this test only inspects the request header
	}))
	t.Cleanup(srv.Close)

	f := newWithClient(srv.URL, &http.Client{})
	if _, err := f.FetchUsage(context.Background(), map[string]string{"subscription_key": want}); err == nil {
		t.Fatal("expected an error from an unrecognised schema, got nil")
	}
	if got != "Bearer "+want {
		t.Fatalf("Authorization header: got %q, want %q", got, "Bearer "+want)
	}
}

// TestFetchUsage_MissingSubscriptionKey returns a clear error rather
// than silently hitting the upstream without auth — calling
// /token_plan/remains without a key would 401 from upstream and trigger
// cooldown, hiding the real bug.
func TestFetchUsage_MissingSubscriptionKey(t *testing.T) {
	t.Parallel()

	f := newWithClient("http://should-not-be-called.invalid", &http.Client{})

	cases := []map[string]string{
		nil,
		{},
		{"api_key": "looks-like-openai-but-not-our-field"},
		{"subscription_key": "   "},
	}
	for i, creds := range cases {
		_, err := f.FetchUsage(context.Background(), creds)
		if err == nil {
			t.Fatalf("case %d: expected error for missing subscription_key, got nil", i)
		}
		if !strings.Contains(err.Error(), "subscription_key") {
			t.Errorf("case %d: expected error to mention subscription_key, got %v", i, err)
		}
	}
}

// TestFetchUsage_UnknownSchemaReturnsTypedError is the spike finding
// committed to a test: when a 2xx body parses as JSON but does not
// include any numeric field from the recognised set, FetchUsage must
// return ErrSchemaNotRecognized (not fabricate metrics from
// non-numeric fields). This is the "no fabrication" guarantee.
func TestFetchUsage_UnknownSchemaReturnsTypedError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		// No recognised numeric field. The parser must NOT map these
		// keys to UsageMetrics just because they parse as numbers in
		// some future response — that's the captured-response
		// extension flow.
		_, _ = w.Write([]byte(`{"model":"m3","plan":"plus","status":"active"}`))
	}))
	t.Cleanup(srv.Close)

	f := newWithClient(srv.URL, &http.Client{})
	creds := map[string]string{"subscription_key": "sk-test"}

	_, err := f.FetchUsage(context.Background(), creds)
	if err == nil {
		t.Fatal("expected ErrSchemaNotRecognized, got nil")
	}
	if !errors.Is(err, ErrSchemaNotRecognized) {
		t.Fatalf("expected ErrSchemaNotRecognized, got %v", err)
	}
}

// TestFetchUsage_EmptyBodyReturnsError pins the rejection of a 2xx with
// an empty body. The dashboard depends on the parser returning an error
// rather than a (zero-metric) happy path for an unparseable upstream.
func TestFetchUsage_EmptyBodyReturnsError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	f := newWithClient(srv.URL, &http.Client{})
	_, err := f.FetchUsage(context.Background(), map[string]string{"subscription_key": "sk-test"})
	if err == nil {
		t.Fatal("expected error for empty body, got nil")
	}
	if errors.Is(err, ErrSchemaNotRecognized) {
		t.Fatalf("expected a decode error, not ErrSchemaNotRecognized (distinguish empty body from unrecognised shape), got %v", err)
	}
}

// TestFetchUsage_NonJSONBodyReturnsError is the other side of the same
// guarantee for bodies that don't decode as JSON at all.
func TestFetchUsage_NonJSONBodyReturnsError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<html>not json</html>`))
	}))
	t.Cleanup(srv.Close)

	f := newWithClient(srv.URL, &http.Client{})
	_, err := f.FetchUsage(context.Background(), map[string]string{"subscription_key": "sk-test"})
	if err == nil {
		t.Fatal("expected error for non-JSON body, got nil")
	}
	if errors.Is(err, ErrSchemaNotRecognized) {
		t.Fatalf("expected a decode error, got ErrSchemaNotRecognized for an unparseable body")
	}
}

// TestFetchUsage_PlausibleShapeMapsToMetrics exercises the mapped-metric
// path against a JSON shape drawn from the documented "5-hour + weekly
// window" model. A real captured response would land in a follow-up
// PR and possibly extend the recognised key set; this test pins the
// current best-effort mapping so a key-set extension can't silently
// change it.
func TestFetchUsage_PlausibleShapeMapsToMetrics(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"current_interval_5h_count": 1234,
			"current_interval_week_count": 56789,
			"interval_limit_5h": 5000,
			"interval_limit_week": 200000,
			"reset_time": "2026-07-15T17:00:00Z"
		}`))
	}))
	t.Cleanup(srv.Close)

	f := newWithClient(srv.URL, &http.Client{})
	creds := map[string]string{"subscription_key": "sk-test"}

	metrics, err := f.FetchUsage(context.Background(), creds)
	if err != nil {
		t.Fatalf("FetchUsage returned error: %v", err)
	}
	if len(metrics) == 0 {
		t.Fatalf("expected at least one metric from the recognised numeric set, got none")
	}

	// The mapping is deterministic: 5h_count -> minimax_5h, week_count ->
	// minimax_weekly, etc. Sort by Name for stable ordering assertions.
	got := map[string]provider.UsageMetric{}
	for _, m := range metrics {
		got[m.Name] = m
	}

	for _, want := range []struct {
		name string
		used int64
	}{
		{"minimax_5h", 1234},
		{"minimax_weekly", 56789},
		{"minimax_5h_limit", 5000},
		{"minimax_weekly_limit", 200000},
	} {
		m, ok := got[want.name]
		if !ok {
			t.Errorf("missing metric %q in %+v", want.name, metrics)
			continue
		}
		if m.Used != want.used {
			t.Errorf("metric %q: Used got %d, want %d", want.name, m.Used, want.used)
		}
		if m.Unit != "tokens" {
			t.Errorf("metric %q: Unit got %q, want %q", want.name, m.Unit, "tokens")
		}
	}

	if resetMetrics, ok := got["minimax_5h"]; ok && resetMetrics.ResetAt == nil {
		t.Errorf("expected reset_time to populate ResetAt, got nil")
	}
}

// TestFetchUsage_WrappedDataEnvelope walks the same plausible shape
// under a {"data": {...}} wrapper, which is common in adjacent AI
// platform APIs. The parser must tolerate the wrapper without losing
// the recognised fields.
func TestFetchUsage_WrappedDataEnvelope(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code":0,"data":{"current_count":42},"msg":"ok"}`))
	}))
	t.Cleanup(srv.Close)

	f := newWithClient(srv.URL, &http.Client{})
	metrics, err := f.FetchUsage(context.Background(), map[string]string{"subscription_key": "sk-test"})
	if err != nil {
		t.Fatalf("FetchUsage returned error: %v", err)
	}
	if len(metrics) != 1 || metrics[0].Used != 42 {
		t.Fatalf("expected one metric with Used=42 from wrapped envelope, got %+v", metrics)
	}
}

// TestFetchUsage_5xxNonAuthSurfacesUpstreamError confirms a non-auth
// upstream error (e.g. 500) does NOT engage ErrAuth — the scheduler's
// cooldown is auth-specific, and only ErrAuth should trigger it.
func TestFetchUsage_5xxNonAuthSurfacesUpstreamError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`upstream busy`))
	}))
	t.Cleanup(srv.Close)

	f := newWithClient(srv.URL, &http.Client{})
	_, err := f.FetchUsage(context.Background(), map[string]string{"subscription_key": "sk-test"})
	if err == nil {
		t.Fatal("expected error for upstream 500, got nil")
	}
	if errors.Is(err, provider.ErrAuth) {
		t.Fatalf("must not wrap provider.ErrAuth on non-auth upstream errors, got %v", err)
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("expected error to surface 500 status, got %v", err)
	}
}

// TestFetchUsage_DoesNotLeakCredentialInError guards the write-only
// credential contract: the Subscription Key is never echoed in any
// error path (auth, parse, network). Any future edit that surfaces it
// would breach the credential write-only posture.
func TestFetchUsage_DoesNotLeakCredentialInError(t *testing.T) {
	t.Parallel()

	const want = "sk-test-deadbeef"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`invalid key`))
	}))
	t.Cleanup(srv.Close)

	f := newWithClient(srv.URL, &http.Client{})
	_, err := f.FetchUsage(context.Background(), map[string]string{"subscription_key": want})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if strings.Contains(err.Error(), want) {
		t.Fatalf("error must not echo the credential value, got %v", err)
	}
	if strings.Contains(err.Error(), "Bearer "+want) {
		t.Fatalf("error must not echo the Authorization header value, got %v", err)
	}
}
