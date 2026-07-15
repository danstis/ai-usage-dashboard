// Package minimax implements the provider.Fetcher for the Minimax Token
// Plan subscription, the first live in-tree plugin (BSOD-68). It calls
// the documented `GET https://www.minimax.io/v1/token_plan/remains`
// endpoint with a `Subscription Key` credential and maps the response to
// one UsageMetric per quota window — the 5-hour rolling window and the
// weekly window — which the dashboard surfaces as the 5h/weekly bars the
// user asked for.
//
// Endpoint reference:
//   - Token Plan FAQ: https://platform.minimax.io/docs/token-plan/faq
//
// The credential field name is `subscription_key` (Secret = true); see
// the Registry entry in internal/provider/provider.go.
package minimax

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/danstis/ai-usage-dashboard/internal/provider"
)

// subscriptionKeyCredential is the only credential field the plugin
// declares. The Subscription Key is supplied via the existing write-only
// CredentialField{Secret: true} shape — see docs/providers.md.
const subscriptionKeyCredential = "subscription_key"

// endpointURL is the documented customer-tier usage endpoint. The path
// is stable per the Token Plan FAQ; only the host could vary (we hardcode
// the www. host the docs example uses).
const endpointURL = "https://www.minimax.io/v1/token_plan/remains"

// Metadata is the static, compiled-in description of the Minimax Token
// Plan provider. It matches the entry appended to provider.Registry in
// internal/provider/provider.go — the runtime Service validates that the
// two stay in sync (RegisterFetcher panics on a mismatch).
func Metadata() provider.Metadata {
	return provider.Metadata{
		ID:   "minimax",
		Name: "Minimax",
		CredentialFields: []provider.CredentialField{
			{Name: subscriptionKeyCredential, Label: "Subscription Key", Secret: true},
		},
	}
}

// HTTPClient is the subset of *http.Client the plugin uses. It exists so
// tests can inject httptest.Server-backed transports without touching the
// global DefaultClient.
type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

// fetcher is the production provider.Fetcher implementation for Minimax.
type fetcher struct {
	meta     provider.Metadata
	client   HTTPClient
	endpoint string
	now      func() time.Time
}

// New returns a provider.Fetcher backed by the package-level http.DefaultClient.
// main() calls this once at boot and registers it via
// provider.Service.RegisterFetcher — see the register-at-boot pattern in
// docs/providers.md.
func New() provider.Fetcher {
	return NewWithClient(http.DefaultClient)
}

// NewWithClient returns a provider.Fetcher backed by client. Tests use it
// to inject httptest.Server-backed transports; production callers use New.
func NewWithClient(client HTTPClient) provider.Fetcher {
	return &fetcher{
		meta:     Metadata(),
		client:   client,
		endpoint: endpointURL,
		now:      func() time.Time { return time.Now().UTC() },
	}
}

// Metadata satisfies provider.Fetcher. It returns the same value the id
// resolves to in provider.Registry; the runtime Service checks this on
// RegisterFetcher.
func (f *fetcher) Metadata() provider.Metadata { return f.meta }

// FetchUsage calls GET /v1/token_plan/remains with the user's Subscription
// Key and maps the response to two UsageMetric values: one for the 5-hour
// rolling window and one for the weekly window. HTTP 4xx/5xx and the
// provider's own base_resp.status_code != 0 are surfaced as errors; auth
// failures (status_code 1004 per the docs, or HTTP 401/403) are wrapped in
// provider.ErrAuth so the scheduler engages the auth-failure backoff.
func (f *fetcher) FetchUsage(ctx context.Context, creds map[string]string) ([]provider.UsageMetric, error) {
	key, ok := creds[subscriptionKeyCredential]
	if !ok || key == "" {
		return nil, fmt.Errorf("minimax: missing %q credential", subscriptionKeyCredential)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("minimax: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("minimax: request: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("minimax: read body: %w", err)
	}

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("minimax: authentication failed (status %d): %w", resp.StatusCode, provider.ErrAuth)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("minimax: unexpected status %d: %s", resp.StatusCode, truncate(body, 256))
	}

	var payload response
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("minimax: parse response: %w", err)
	}

	if payload.BaseResp.StatusCode != 0 {
		if isAuthError(payload.BaseResp.StatusCode) {
			return nil, fmt.Errorf("minimax: authentication failed (status_code %d: %s): %w",
				payload.BaseResp.StatusCode, payload.BaseResp.StatusMsg, provider.ErrAuth)
		}
		return nil, fmt.Errorf("minimax: upstream error status_code %d: %s",
			payload.BaseResp.StatusCode, payload.BaseResp.StatusMsg)
	}

	now := f.now()
	fiveHour, err := payload.Data.Current5h.toMetric("token_plan_5h", "5h", now)
	if err != nil {
		return nil, fmt.Errorf("minimax: 5h window: %w", err)
	}
	weekly, err := payload.Data.CurrentWeek.toMetric("token_plan_weekly", "week", now)
	if err != nil {
		return nil, fmt.Errorf("minimax: weekly window: %w", err)
	}

	return []provider.UsageMetric{fiveHour, weekly}, nil
}

// response mirrors the base envelope Minimax wraps every payload in.
// Verified against the live endpoint's auth-failure response ({"base_resp":
// {"status_code":1004,"status_msg":"login fail: ..."}}). status_code 0 ==
// success per Minimax's standard convention; non-zero is an error, with
// 1004 documented as the credential rejection code in the docs.
type response struct {
	BaseResp baseResp `json:"base_resp"`
	Data     data     `json:"data"`
}

type baseResp struct {
	StatusCode int    `json:"status_code"`
	StatusMsg  string `json:"status_msg"`
}

// data is the success payload. Field names mirror the documented naming
// ("current_5h_remains" / "current_week_remains"); the parser tolerates a
// missing reset_at by leaving UsageMetric.ResetAt nil (the contract allows
// that — see internal/provider/fetcher.go).
type data struct {
	Current5h   windowQuota `json:"current_5h"`
	CurrentWeek windowQuota `json:"current_week"`
}

// windowQuota is one window's quota block as the endpoint returns it.
// `remains` is the remaining count; `limit` is the total cap for the
// window; `reset_at` is when the window rolls over (parsed as RFC3339).
// Used = limit - remains (clamped at zero so a transient inconsistency
// between the two fields doesn't produce a negative used count).
type windowQuota struct {
	Remains *int64     `json:"remains"`
	Limit   *int64     `json:"limit"`
	ResetAt *time.Time `json:"reset_at"`
}

// toMetric projects a windowQuota into a provider.UsageMetric. name and
// window are the metric label and window label; now is included so the
// caller can verify reset_at is sane (a reset in the past is clamped to
// nil — a "reset at" that's already passed is no longer informative).
func (w windowQuota) toMetric(name, window string, now time.Time) (provider.UsageMetric, error) {
	if w.Remains == nil && w.Limit == nil {
		return provider.UsageMetric{}, errors.New("missing both remains and limit")
	}
	var used, limit, remaining int64
	if w.Limit != nil {
		limit = *w.Limit
	}
	if w.Remains != nil {
		remaining = *w.Remains
	}
	used = limit - remaining
	if used < 0 {
		// Provider's own invariant: used can't be negative. Clamp at 0
		// rather than failing the whole tick — a transient upstream
		// inconsistency shouldn't blackhole the provider.
		used = 0
	}

	m := provider.UsageMetric{
		Name:      name,
		Window:    window,
		Unit:      "prompts",
		Used:      used,
		Limit:     nilToNil(w.Limit),
		Remaining: nilToNil(w.Remains),
		ResetAt:   nilToNilTime(w.ResetAt, now),
	}
	return m, nil
}

// isAuthError reports whether the documented status_code values that mean
// "your credential was rejected" are present. 1004 is the value observed
// in the live auth-failure response; we treat any 1xxx as auth-class for
// forward-compatibility with sibling codes Minimax may add (1002/1003
// pattern from neighbouring APIs).
func isAuthError(statusCode int) bool {
	return statusCode >= 1000 && statusCode < 2000
}

func nilToNil(p *int64) *int64 {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func nilToNilTime(p *time.Time, now time.Time) *time.Time {
	if p == nil {
		return nil
	}
	t := p.UTC()
	if !t.After(now) {
		// Reset window already passed — clear so the dashboard doesn't
		// surface a stale "reset at <yesterday>" label.
		return nil
	}
	return &t
}

func truncate(b []byte, maxBytes int) string {
	if len(b) <= maxBytes {
		return string(b)
	}
	return string(b[:maxBytes]) + "..."
}
