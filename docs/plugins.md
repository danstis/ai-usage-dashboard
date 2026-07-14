# External provider plugins (design stub)

> **Status: design stub only.** No loader ships in this phase (P2). This
> document describes a future model for hosting externally-compiled
> provider binaries; nothing here is implemented yet. See
> [`plugins/README.md`](../plugins/README.md) for the placeholder directory
> those binaries would eventually live in.

## Today: in-tree, compiled-in providers

`aud` currently ships providers as Go code compiled into the `aud` binary.
Each provider satisfies the runtime `Fetcher` contract defined in
`internal/provider/fetcher.go`:

```go
type Fetcher interface {
    Metadata() Metadata
    FetchUsage(ctx context.Context, creds map[string]string) ([]UsageMetric, error)
}
```

A provider registers itself with `Service.RegisterFetcher` at startup, keyed
by `Metadata().ID`. `UsageMetric` — the data a `Fetcher` returns — is a
scalar struct (`Name`, `Window`, `Unit`, `Used`, and optional `Limit`,
`Remaining`, `ResetAt`); see the doc comment on `UsageMetric` in
`internal/provider/fetcher.go` for the full field contract.

## Future: out-of-process providers via go-plugin

The `Fetcher` interface was deliberately designed to be **transport-neutral**
(Architect decision 5 on BSOD-61): the only types that cross the interface
boundary are `context.Context`, a `map[string]string` of credentials, and a
`[]UsageMetric` of scalar fields. No channels, funcs, or interfaces appear in
either direction.

That constraint means the same `Fetcher` contract can be satisfied by a
process boundary instead of a Go interface, with no change to its method
signatures. The intended future shape:

- Externally-compiled provider binaries are dropped into `/plugins` (see the
  placeholder directory).
- `aud` hosts them using [`hashicorp/go-plugin`](https://github.com/hashicorp/go-plugin)
  over its gRPC transport, launching each binary as a subprocess and
  communicating over a local gRPC connection.
- A thin adapter on the `aud` side implements `Fetcher` by translating
  `Metadata()` / `FetchUsage(ctx, creds)` calls into gRPC requests to the
  plugin subprocess, and translates the gRPC response back into
  `[]UsageMetric`. Because only scalar types cross the boundary today, that
  translation is a direct field-for-field mapping — no redesign of
  `UsageMetric` or `Fetcher` is needed to support it.
- The plugin adapter registers itself with `Service.RegisterFetcher` exactly
  like an in-tree provider, so the scheduler (P2/S5) and the on-demand
  refresh endpoint treat compiled-in and externally-loaded providers
  identically once registered.

## Explicitly out of scope for this phase

No loader, no `hashicorp/go-plugin` dependency, and no gRPC code are added in
P2. This phase only adds the `/plugins` placeholder directory and this
document. A later phase would add:

- the go-plugin handshake/host wiring and subprocess lifecycle management,
- a `.proto` definition for `Fetcher` and generated gRPC stubs,
- discovery/loading of binaries from `/plugins` at startup,
- plugin-specific error handling and sandboxing (crash isolation, timeouts,
  binary verification) beyond what in-tree providers need.
