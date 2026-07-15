# Provider runtime contract

This document describes the **runtime** side of the provider catalog: the
`Fetcher` interface, the `UsageMetric` shape it returns, and how a fetch
implementation is wired in. For the static / metadata side (what providers
ship today, how to add a new entry, and how they are reconciled against
the database on boot), see [the **Shipped providers** section in
`README.md`](../README.md#shipped-providers). For the future out-of-process
shape (external binaries, `hashicorp/go-plugin`), see
[`docs/plugins.md`](plugins.md).

## The two registries

Providers are tracked in **two parallel structures** that share the
provider id as the join key:

| Registry            | Where                                       | Lifetime                          | Purpose                                                     |
| ------------------- | ------------------------------------------- | --------------------------------- | ----------------------------------------------------------- |
| `provider.Registry` | `internal/provider/provider.go`             | Compiled into the binary.         | Static catalog of known providers + their declared credential fields. |
| `provider.Service`  | `internal/provider/provider.go`             | One per process.                  | Merges the registry with persisted enabled/disabled state from the store. |
| Runtime `Fetcher` registry | `internal/provider/fetcher.go`        | Built fresh in `NewService`.      | `Fetcher` implementations keyed by metadata id, used by the future scheduler / on-demand refresh. |

A provider id present in `Registry` but with no registered `Fetcher` is
**not pollable**: the scheduler (P2/S5) skips it, and
`POST /api/v1/providers/{id}/refresh` returns `409 conflict` with code
`conflict` (see [the **Error responses** section in
`README.md`](../README.md#error-responses)). This split exists deliberately
so a metadata-only entry is observable as "not pollable" rather than as a
silent missing-method error.

## Live vs. scaffolded

**"Live" iff a `Fetcher` is registered for the id — that single condition
drives both the API and the boot sequence.** There is no separate flag to
keep in sync:

- `Service.HasFetcher(id)` (`internal/provider/fetcher.go`) is the single
  source of truth.
- The `Provider.live` API field (`internal/provider/provider.go`,
  `internal/api/provider_adapter.go`) is `HasFetcher(id)` at the moment
  `List`/`Get` is called. It is independent of `enabled` and of credential
  state, and it is **not a health signal** — a live provider with a bad or
  missing credential is still `live: true`; it just fails to collect.
- The scheduler's register-at-boot step (below) is the only thing that
  makes an id live in the first place.

A provider ships in one of two states along this axis:

| State      | Registry entry | Registered `Fetcher` | `Provider.live` | Pollable |
| ---------- | --------------- | --------------------- | ---------------- | -------- |
| Scaffolded | yes             | no                    | `false`           | no — `ErrFetcherNotFound` / `409 conflict` |
| Live       | yes             | yes                   | `true`            | yes |

`minimax` is a scaffolded entry today (BSOD-68). The plugin package at
`internal/plugins/minimax` builds the documented GET request and
classifies auth + schema outcomes, but the response schema is not
publicly documented at land time — so the recognised key set in the
plugin is intentionally narrow and `main.go` does not register the
fetcher. The follow-up that captures a real response extends the
recognised key set and toggles the boot-wiring on. See the package
doc comment for the spike status and `providers-research.md §4` for
the research context.

## Plugin package convention

In-tree provider plugins live under `internal/plugins/<provider>/` (e.g.
`internal/plugins/openai/`) — **not** the top-level [`/plugins`](../plugins/README.md)
directory, which is reserved for the future externally-compiled binaries
described in [`docs/plugins.md`](plugins.md). Each in-tree plugin package
exports a single constructor:

```go
// internal/plugins/openai/openai.go
package openai

func New() provider.Fetcher { /* ... */ }
```

`New()` returns a value satisfying the `Fetcher` interface below; the
package owns its own HTTP client, response parsing, and error wrapping
(including wrapping auth failures in `provider.ErrAuth` — see
[Auth-failure backoff](#auth-failure-backoff)) but exposes nothing else
publicly.

## Register-at-boot pattern

`cmd/aud/main.go` calls `providerSvc.RegisterFetcher(<pkg>.New())` once per
live provider, immediately after `provider.NewService(...)` /
`Reconcile(...)` and before the scheduler starts:

```go
providerSvc := provider.NewService(db, provider.Registry)
if err := providerSvc.Reconcile(ctx); err != nil { /* fail boot */ }

providerSvc.RegisterFetcher(openai.New())
providerSvc.RegisterFetcher(anthropic.New())

// ... credential service, collector, scheduler.New — see below.
```

A scaffolded provider (metadata in `Registry` with no plugin package yet, or
a plugin package that exists but hasn't wired in a real implementation) adds
no registration line at all — there is nothing to opt out of.

## Auth-failure backoff

A live `Fetcher` **must** wrap `provider.ErrAuth` (via
`fmt.Errorf("...: %w", provider.ErrAuth)`) when the upstream rejects
credentials as invalid (HTTP 401/403 or the provider's equivalent). The
scheduler (`internal/scheduler.Scheduler`) detects this via `errors.Is` and
engages a per-provider cooldown so a bad key isn't retried every tick; any
other error keeps the existing per-tick retry behavior. A successful
`internal/credential.Service.SetValues` call clears the cooldown for that
provider immediately. `POST /providers/{id}/refresh` is never gated by this
cooldown — see `internal/scheduler.AuthCooldownRegistry` for the
implementation and `internal/scheduler/scheduler_test.go` for the covered
scenarios.

## `Fetcher` interface

```go
// internal/provider/fetcher.go
type Fetcher interface {
    Metadata() Metadata
    FetchUsage(ctx context.Context, creds map[string]string) ([]UsageMetric, error)
}
```

`Metadata()` must return the **same** `Metadata` value that the id resolves
to in `Registry`. `FetchUsage` is called once per poll with a credentials
map keyed by the provider's declared credential field names (e.g.
`{"api_key": "<revealed value>"}`); the credential values come from the
encrypted store via `internal/credential.Service.Reveal` (see
[Secrets in `README.md`](../README.md#secrets)).

`FetchUsage` must be safe to call concurrently for **different** provider
ids. Implementations are responsible for any per-provider synchronization
they need.

The interface is **transport-neutral** by design (Architect decision 5 on
BSOD-61): the only types crossing the boundary are `context.Context`, a
`map[string]string` of credentials, and a `[]UsageMetric` of scalar fields.
No channels, funcs, or interfaces appear in either direction. That means
the same `Fetcher` contract can be satisfied by a process boundary instead
of a Go interface (the future `hashicorp/go-plugin` shape in
[`docs/plugins.md`](plugins.md)) with no change to the method signatures.

## `UsageMetric` field contract

```go
type UsageMetric struct {
    Name      string     // required, non-empty (e.g. "monthly_spend")
    Window    string     // required, non-empty (e.g. "month", "day", "hour")
    Unit      string     // required, non-empty — smallest integer unit (e.g. "usd_cents", "tokens")
    Used      int64      // required
    Limit     *int64     // nil = unlimited / unknown
    Remaining *int64     // nil = unknown; provider-supplied; callers MAY derive Limit-Used when nil
    ResetAt   *time.Time // nil = no reset time reported
}
```

Rules:

- **No floats.** All quantities are `int64` in the smallest integer unit
  named by `Unit`. Fractional quantities (e.g. spend) are expressed as
  integer cents / tokens / etc. This keeps the type deterministic and
  gRPC-friendly — every value survives a JSON / protobuf round-trip
  without precision loss.
- **Optional pointers use `nil`, not sentinels.** A `nil` `Limit` means
  "the provider does not disclose a cap" (unlimited or unknown). A `nil`
  `Remaining` means the provider did not return one — the consumer may
  derive `Limit - Used` if it wants an estimate, but the absence is
  informative, not a guarantee that the underlying field is zero.
- **Reset windows are opaque strings.** The dashboard interprets a small,
  well-known set ("month", "day", "hour") for grouping, but the contract
  does not enforce a closed enum — a new provider can ship its own window
  label and the UI will surface it as-is until it is added to the
  group-by vocabulary.

## Registering a fetcher

In `main` (today's compiled-in shape), after `provider.NewService`:

```go
providerSvc := provider.NewService(db, provider.Registry)
if err := providerSvc.Reconcile(ctx); err != nil { /* fail boot */ }

// Register one Fetcher per provider id that should be pollable.
providerSvc.RegisterFetcher(openai.NewFetcher())
providerSvc.RegisterFetcher(anthropic.NewFetcher())
```

Duplicate registration for the same id **panics at startup**. Two fetchers
claiming the same provider id is treated as a programmer error — failing
loud at boot rather than silently shadowing an existing fetcher at runtime
is deliberate.

`providerSvc.FetchUsage(ctx, id, creds)` looks up the registered fetcher
and returns:

- `provider.ErrFetcherNotFound` if `id` is not in the metadata registry.
- `provider.ErrFetcherNotFound` if `id` is in the metadata registry but
  has no registered `Fetcher` — both are "not pollable" and must be
  treated identically by callers.
- The fetcher's error wrapped with `fmt.Errorf("provider: fetch %s: %w", id, err)`
  otherwise — `errors.Is` / `errors.As` work through the wrap.

## Testing fetchers

The `runtimeRegistry` is built fresh in every `NewService` call. Tests
should construct their own `Service` (with a fixture registry and a stub
`store.ProviderRepository`) and register fake fetchers against it — that
way registration in tests cannot leak into production and vice versa.

See `internal/provider/fetcher_test.go` for table-driven RED-first tests
covering: `UsageMetric` field shape, fake `Fetcher` implementations (nil
optionals, error propagation, metric-copy isolation), runtime registry
(register / lookup / unknown / duplicate-panics), `Service.FetchUsage`
(registered fetcher, unknown id, metadata-only no fetcher, error
propagation), and a guard that the metadata `Registry` still carries
`CredentialFields` for every entry (the P1 API contract stays intact as
the runtime side is added).

### Testing an in-tree plugin (`internal/plugins/plugintest`)

Each plugin under `internal/plugins/<provider>/` should test its
`FetchUsage` implementation against `internal/plugins/plugintest.Stack`
rather than hand-rolling a `provider.Service` / `credential.Service` pair:

```go
stack := plugintest.NewStack(myMetadata) // in-memory store, provider enabled
stack.Providers.RegisterFetcher(New())   // the plugin's own Fetcher

creds, err := stack.Reveal(ctx, myMetadata.ID, map[string]string{"api_key": "sk-test"})
// ... err check

metrics, err := stack.Providers.FetchUsage(ctx, myMetadata.ID, creds)
```

`Stack.Reveal` seals and reveals credential values through the real
`credential.Service`, so a test's `creds` map is resolved the same way
`internal/scheduler.Collector` resolves it at runtime — not hand-assembled.
Cover at least two shapes per plugin:

1. **Live-mock**: register the plugin's `Fetcher`, resolve credentials, and
   assert `FetchUsage` returns the expected `[]provider.UsageMetric` (see
   the `UsageMetric` field contract above — integer units, `nil` for
   undisclosed `Limit`/`Remaining`/`ResetAt`, never floats).
2. **Scaffolded / missing-fetcher**: build a `Stack` and call `FetchUsage`
   *before* registering a `Fetcher`, asserting `errors.Is(err,
   provider.ErrFetcherNotFound)` — this is the state the plugin ships in
   before it's wired into `cmd/aud/main.go`.

See `internal/plugins/plugintest/harness_test.go` for both shapes exercised
against the harness itself.
