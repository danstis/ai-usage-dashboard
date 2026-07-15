// Package minimax implements the Minimax Token Plan provider plugin as a
// transport-neutral provider.Fetcher (see internal/provider). It targets
// the single, documented endpoint
//
//	GET https://www.minimax.io/v1/token_plan/remains
//
// with the user's Subscription Key as a Bearer token, per the official
// Minimax Token Plan FAQ:
//
//	curl --location 'https://www.minimax.io/v1/token_plan/remains' \
//	  --header 'Authorization: Bearer <Subscription Key>' \
//	  --header 'Content-Type: application/json'
//
// The Subscription Key is declared in internal/provider.Registry as a
// write-only Secret credential field.
//
// # Spike status — response schema unknown at land time
//
// As of landing this plugin (2026-07-15), the official documentation
// publishes the endpoint URL and required headers but does not publish
// the response payload shape (see docs/providers-research.md §4 and
// BSOD-91). The package is therefore shipped as a **scaffolded** entry:
// the metadata is present in provider.Registry so the dashboard lists
// the provider with its declared credential field, but no fetcher is
// registered against it in cmd/aud/main.go (Provider.Live=false). Once
// a real Subscription Key + captured response is available, a follow-up
// PR should:
//
//  1. Confirm the parser fields below against the captured JSON, and
//     extend the recognised keys as needed (any 401/403 handling stays
//     unchanged).
//  2. Add providerSvc.RegisterFetcher(minimax.New()) in cmd/aud/main.go
//     alongside the scaffolded-multiplexer comment block there.
//
// Until then, FetchUsage has three outcomes:
//
//   - Auth failure (HTTP 401/403): wraps provider.ErrAuth so the
//     scheduler's auth-cooldown engages and POST /refresh surfaces the
//     error unmodified.
//   - Recognised response shape: best-effort mapping to UsageMetric —
//     the recognised key set is intentionally narrow (numeric values
//     only), so unexpected / unparseable shapes fall through to the
//     next branch rather than fabricating numbers.
//   - Unrecognised shape: returns ErrSchemaNotRecognized with the raw
//     body's top-level keys listed so the capturing operator can extend
//     the recognised set in one place.
//
// No value is ever emitted from the parser unless it appears as a
// numeric field in the response body. The minimax contract is
// "provider-sourced only" — see the constraints in
// docs/providers-research.md "Constraints we will not relax".
package minimax

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/danstis/ai-usage-dashboard/internal/provider"
)

// Metadata is the registry-matching description of this provider. It is
// the value internal/provider.Registry returns for id "minimax" and the
// one this Fetcher advertises via Metadata(); both must remain equal
// or RegisterFetcher panics at startup.
var Metadata = provider.Metadata{
	ID:   "minimax",
	Name: "Minimax Token Plan",
	CredentialFields: []provider.CredentialField{
		{Name: "subscription_key", Label: "Subscription Key", Secret: true},
	},
}

// Default endpoint + auth scheme, both documented in the Minimax Token
// Plan FAQ (see package doc). Exported as vars so the tests + a future
// "wire me differently" build flag can override them without forking
// the package; production callers should use the zero-value defaults.
var (
	DefaultEndpoint = "https://www.minimax.io/v1/token_plan/remains"
)

// ErrSchemaNotRecognized is returned by FetchUsage when the upstream
// response body parses as JSON but does not match any of the recognised
// numeric-field shapes listed in recognisedNumericKeys. Callers can
// detect it with errors.Is. A captured real response will document the
// missing keys and a follow-up PR can extend the recognised set.
var ErrSchemaNotRecognized = errors.New("minimax: response schema not recognised — capture a real response to extend the parser")

// recognisedNumericKeys is the closed set of JSON field names the
// parser looks for in the response body. It is intentionally narrow:
// any field present in a real response that is not in this set is
// ignored rather than mapped. Extending the set is the safe path
// forward; widening the recognised values (e.g. mapping anything
// numeric-shaped without a key match) would risk fabricating metrics
// from non-metric fields.
//
// Field naming convention: lowercase snake_case to match the style the
// Minimax platform uses elsewhere (per platform.minimax.io docs). If
// a captured response uses camelCase, add the camelCase variants here
// in a follow-up rather than rewriting this slice.
var recognisedNumericKeys = []string{
	// Per-window usage in the smallest integer unit. The platform uses
	// "usage" for quota-equivalent unit counts; "count" / "current" are
	// common alternatives in adjacent AI quota APIs.
	"current_count",
	"current_interval_count",
	"current_interval_5h_count",
	"current_interval_week_count",

	// Per-window limit (the cap the window resets to).
	"limit",
	"total_count",
	"interval_limit",
	"interval_limit_5h",
	"interval_limit_week",

	// Per-window remaining (provider-supplied; may differ from limit-used).
	"remaining",
	"remaining_count",
	"interval_remaining",
	"interval_remaining_5h",
	"interval_remaining_week",
}

// recognisedResetKeys are RFC3339 / Unix timestamps that, when present,
// can populate a usage metric's ResetAt field. We look up the per-window
// reset by attempting every recognised key in order; the first one that
// parses wins.
var recognisedResetKeys = []string{
	"reset_time",
	"interval_reset_time_5h",
	"interval_reset_time_week",
	"reset_at",
	"next_reset",
}

// Fetcher is the provider.Fetcher implementation for this package.
type Fetcher struct {
	endpoint string
	client   *http.Client
}

// New returns a Fetcher wired to DefaultEndpoint and a default
// http.Client. Tests pass a custom fetcher via newWithClient to inject
// a test server URL and a tighter client timeout.
func New() *Fetcher {
	return newWithClient(DefaultEndpoint, &http.Client{Timeout: 30 * time.Second})
}

// newWithClient is the package-private constructor used by tests.
func newWithClient(endpoint string, client *http.Client) *Fetcher {
	return &Fetcher{endpoint: endpoint, client: client}
}

// Metadata satisfies provider.Fetcher; it returns the package-level
// var (not a clone) so it stays identical to the registry entry —
// RegisterFetcher would panic if they diverged.
func (f *Fetcher) Metadata() provider.Metadata { return Metadata }

// FetchUsage satisfies provider.Fetcher. It dispatches the documented
// GET to the configured endpoint with the Subscription Key as Bearer,
// classifies the response, and either:
//
//   - wraps a non-2xx auth failure as provider.ErrAuth,
//   - returns ErrSchemaNotRecognized when a 2xx body's shape is not
//     covered by the recognised key sets, or
//   - returns the best-effort []UsageMetric for a recognised shape.
//
// No metric value is ever synthesised from non-numeric or unknown
// fields; the recognised set is closed and explicit.
func (f *Fetcher) FetchUsage(ctx context.Context, creds map[string]string) ([]provider.UsageMetric, error) {
	key, ok := creds["subscription_key"]
	if !ok || strings.TrimSpace(key) == "" {
		return nil, fmt.Errorf("minimax: missing subscription_key credential")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("minimax: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("minimax: fetch usage: %w", err)
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			slog.Warn("minimax: close response body", "error", cerr)
		}
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("minimax: read response: %w", err)
	}

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("minimax: status %d: %w", resp.StatusCode, wrapAuth(body, resp.StatusCode))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("minimax: status %d: %s", resp.StatusCode, string(bytes.TrimSpace(body)))
	}

	metrics, recognised, err := parseUsage(body)
	if err != nil {
		return nil, err
	}
	if !recognised {
		return nil, ErrSchemaNotRecognized
	}
	return metrics, nil
}

// wrapAuth wraps provider.ErrAuth with the response status code and a
// short body fragment (trimmed) so the scheduler log shows *something*
// actionable without leaking the full body. The credential value is
// never included in the wrap — it never appears in this code path.
func wrapAuth(body []byte, status int) error {
	fragment := string(bytes.TrimSpace(body))
	const maxFragment = 200
	if len(fragment) > maxFragment {
		fragment = fragment[:maxFragment] + "…"
	}
	if fragment == "" {
		return fmt.Errorf("status %d: %w", status, provider.ErrAuth)
	}
	return fmt.Errorf("status %d: %s: %w", status, fragment, provider.ErrAuth)
}

// parseUsage extracts UsageMetrics from a 2xx body. The boolean return
// is true iff at least one numeric field matched a recognised key —
// callers (FetchUsage) use it to choose between returning the metrics
// and returning ErrSchemaNotRecognized.
func parseUsage(body []byte) ([]provider.UsageMetric, bool, error) {
	if len(bytes.TrimSpace(body)) == 0 {
		return nil, false, errors.New("minimax: empty response body")
	}
	// Tolerate either a top-level object or a wrapped {"data": ...}
	// object: redirect through the same field-walk regardless of depth.
	root := map[string]json.RawMessage{}
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, false, fmt.Errorf("minimax: decode response: %w", err)
	}
	if nested := root["data"]; len(nested) > 0 {
		inner := map[string]json.RawMessage{}
		if err := json.Unmarshal(nested, &inner); err == nil {
			root = inner
		}
	}

	resetAt := lookupReset(root)

	var metrics []provider.UsageMetric
	recognised := false

	for _, key := range recognisedNumericKeys {
		raw, ok := root[key]
		if !ok {
			continue
		}
		value, ok := decodeInt64(raw)
		if !ok {
			continue
		}
		recognised = true
		metrics = append(metrics, provider.UsageMetric{
			Name:    windowFromKey(key),
			Window:  windowLabel(key),
			Unit:    "tokens",
			Used:    value,
			ResetAt: resetAt,
		})
	}

	// Sort so the output is deterministic across runs for the same
	// fixture — matches the conventional UsageMetric ordering used by
	// the other plugins and keeps snapshot diffs reviewable.
	sort.SliceStable(metrics, func(i, j int) bool {
		return metrics[i].Name < metrics[j].Name
	})

	return metrics, recognised, nil
}

// decodeInt64 accepts a JSON number, a numeric string, or a boolean and
// returns it as int64. Non-numeric JSON values (objects/arrays) return
// (0, false) so the caller falls through to the next key.
func decodeInt64(raw json.RawMessage) (int64, bool) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return 0, false
	}
	// Plain JSON number → directly decode.
	if trimmed[0] == '-' || (trimmed[0] >= '0' && trimmed[0] <= '9') {
		var n int64
		if err := json.Unmarshal(trimmed, &n); err == nil {
			return n, true
		}
		// Some numeric APIs return floats; truncate toward zero rather
		// than reject, on the theory that token-count fields that come
		// back as 1234.0 mean "1234 tokens". The UsageMetric contract
		// is integer-units only, so a fractional floor is the safe
		// direction (avoids inflating the surfaced number).
		var f float64
		if err := json.Unmarshal(trimmed, &f); err == nil {
			if f >= 0 && f <= 1e15 {
				return int64(f), true
			}
		}
		return 0, false
	}
	// Quoted numeric string → unquote and parse.
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(trimmed, &s); err != nil {
			return 0, false
		}
		s = strings.TrimSpace(s)
		if n, err := parseInt64(s); err == nil {
			return n, true
		}
	}
	return 0, false
}

func parseInt64(s string) (int64, error) {
	var n int64
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			if ch != '-' {
				return 0, fmt.Errorf("not numeric: %q", s)
			}
		}
	}
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil {
		return 0, err
	}
	return n, nil
}

// windowFromKey collapses a recognised numeric field to a stable metric
// Name. The conventional naming `<provider>_<window>` keeps the metric
// vocabulary uniform across providers when more windows are added (see
// docs/providers.md "UsageMetric field contract"). A key with no
// window hint falls back to "<key>_count".
func windowFromKey(key string) string {
	switch key {
	case "current_count":
		return "minimax_current"
	case "current_interval_count":
		return "minimax_interval"
	case "current_interval_5h_count":
		return "minimax_5h"
	case "current_interval_week_count":
		return "minimax_weekly"
	case "limit", "interval_limit":
		return "minimax_limit"
	case "total_count":
		return "minimax_total"
	case "interval_limit_5h":
		return "minimax_5h_limit"
	case "interval_limit_week":
		return "minimax_weekly_limit"
	case "remaining", "remaining_count", "interval_remaining":
		return "minimax_remaining"
	case "interval_remaining_5h":
		return "minimax_5h_remaining"
	case "interval_remaining_week":
		return "minimax_weekly_remaining"
	}
	return key
}

// windowLabel picks the broad window label for the metric. The dashboard
// groups by Window (e.g. "5h", "week", "day"); we surface a stable label
// per numeric key without conflating limit/remaining/usage into one row
// (they each get a distinct Name but share the Window so the dashboard
// can co-locate them).
func windowLabel(key string) string {
	switch {
	case strings.Contains(key, "5h"):
		return "5h"
	case strings.Contains(key, "week"):
		return "week"
	}
	return "interval"
}

// lookupReset returns the first parseable reset timestamp from root, or
// nil. Accepted shapes: RFC3339, RFC3339Nano, or a Unix-seconds number
// (with or without a fractional part).
func lookupReset(root map[string]json.RawMessage) *time.Time {
	for _, key := range recognisedResetKeys {
		raw, ok := root[key]
		if !ok {
			continue
		}
		if t, ok := decodeTime(raw); ok {
			return &t
		}
	}
	return nil
}

func decodeTime(raw json.RawMessage) (time.Time, bool) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return time.Time{}, false
	}
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(trimmed, &s); err != nil {
			return time.Time{}, false
		}
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05Z"} {
			if t, err := time.Parse(layout, strings.TrimSpace(s)); err == nil {
				return t.UTC(), true
			}
		}
		return time.Time{}, false
	}
	var n int64
	if err := json.Unmarshal(trimmed, &n); err == nil && n > 0 {
		return time.Unix(n, 0).UTC(), true
	}
	var f float64
	if err := json.Unmarshal(trimmed, &f); err == nil && f > 0 {
		return time.Unix(int64(f), 0).UTC(), true
	}
	return time.Time{}, false
}
