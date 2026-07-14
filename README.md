# ai-usage-dashboard

A simple AI Subscription usage dashboard.

## Overview

`aud` is a small Go HTTP service that exposes operational endpoints for the
AI Usage Dashboard. The binary lives in `cmd/aud` and the HTTP handlers in
`internal/server`. The frontend is tracked under `web/` (currently a
placeholder pending a later phase of the project).

Providers ship compiled into the `aud` binary; an externally-loadable plugin
host is a planned future phase. See [`docs/plugins.md`](docs/plugins.md) for
the design. If you are extending `aud` itself (adding a fetcher for an
existing provider id, etc.) see [`docs/providers.md`](docs/providers.md).

## Development

Requires Go 1.25.7+ (matches `go.mod`; bumped from 1.24.3 by the `modernc.org/sqlite`
and `goose` dependencies). CI builds and tests against Go 1.26 (`Dockerfile`,
`.github/workflows/ci.yml`).

```sh
make build      # go build -trimpath -o bin/aud ./cmd/aud
make test       # go test ./... -race -covermode=atomic -coverprofile=coverage.out
make lint       # golangci-lint run
make spec-lint  # validate api/openapi.yaml
make generate   # regenerate internal/api/types.gen.go from api/openapi.yaml
make run        # go run ./cmd/aud
```

`make test` produces `coverage.out`, matching the command CI runs
(`.github/workflows/ci.yml`) and the file SonarQube consumes
(`sonar-project.properties`). The file is matched by the `*.out` rule in
`.gitignore`.

`make run` requires `AUD_MASTER_KEY` to be set — see [Generating the master
key](#generating-the-master-key) below for instructions and one-liners.

## Configuration

The service reads its configuration from environment variables. `loadConfig`
returns an error on any invalid value, and `run()` fails fast before the HTTP
server starts.

| Variable           | Default         | Description                                                                  |
| ------------------ | --------------- | ---------------------------------------------------------------------------- |
| `AUD_HTTP_PORT`    | `8080`          | TCP port the HTTP server binds to.                                           |
| `AUD_MASTER_KEY`   | _(required)_    | Standard base64, must decode to exactly 32 bytes (AES-256). **Required** — boot fails fast if it is unset, not valid base64, or the wrong length; the value is never logged. See [Generating the master key](#generating-the-master-key) below. |
| `AUD_POLL_INTERVAL`| `5m`            | Parsed as a Go `time.Duration` (e.g. `90s`, `5m`). An invalid value fails boot. |
| `AUD_DB_PATH`      | `./data/aud.db` | Filesystem path to the SQLite database file. See [Database](#database).      |

Startup logs one `configuration loaded` line with `port`, `pollInterval`,
`dbPath`, and `masterKeySet` (a boolean) — the master key bytes themselves are
never included in logs (see `config.LogValue` in `cmd/aud/main.go`).

## Generating the master key

`AUD_MASTER_KEY` must be **32 random bytes** (`256` bits), encoded as **standard
base64** (`base64.StdEncoding` — no URL-safe alphabet, no line wrapping). Any
of the one-liners below produce a valid value; rotate by generating a new one
everywhere it is stored and restarting `aud` — there is no key-versioning
layer yet, so existing ciphertext will fail to decrypt after a rotation
([Secrets](#secrets) describes what is encrypted).

```sh
# OpenSSL — preferred when available.
openssl rand -base64 32

# /dev/urandom, no OpenSSL dependency.
head -c 32 /dev/urandom | base64

# Python.
python3 -c 'import os, base64; print(base64.b64encode(os.urandom(32)).decode())'

# Go.
go run - <<'EOF'
package main
import ("encoding/base64"; "crypto/rand"; "fmt")
func main() {
    b := make([]byte, 32); _, _ = rand.Read(b)
    fmt.Println(base64.StdEncoding.EncodeToString(b))
}
EOF
```

The full 44-character base64 string goes into the env var verbatim:

```sh
export AUD_MASTER_KEY="$(openssl rand -base64 32)"
make run
```

Treat it like any other long-lived secret — do not commit it, do not log it,
and store it in your secrets manager (Vault, AWS Secrets Manager, etc.).
The image's default entrypoint takes the same env var, so the same value works
in Docker / Kubernetes as in bare `make run`.

## Secrets

Provider credentials (e.g. `openai` / `anthropic` API keys) are encrypted at
rest in the SQLite database under [`internal/secret`](internal/secret) using
**AES-256-GCM** with the `AUD_MASTER_KEY` bytes as the symmetric key.

- **Algorithm.** `crypto/aes` + `crypto/cipher` GCM mode (`nonce = 12` bytes
  drawn fresh from `crypto/rand` per seal; authentication tag is the GCM
  default `16` bytes).
- **On-disk blob layout.** `version(1) ‖ nonce(12) ‖ ciphertext ‖ tag(16)` —
  the leading version byte lets a future scheme or rotation coexist with
  today's blobs without ambiguity.
- **AAD domain separation.** The additional authenticated data for every
  ciphertext is `"aud/cred/v1" ‖ 0x00 ‖ provider_id ‖ 0x00 ‖ field_name`,
  with explicit NUL separators so a value sealed for `(provider "a",
  field "bc")` cannot be opened as if it were sealed for `(provider "ab",
  field "c")`. This binds each ciphertext to the exact row it belongs in.
- **Failure modes** are static and never include plaintext or key material:
  `secret.ErrInvalidKeyLength`, `secret.ErrInvalidBlob`,
  `secret.ErrDecryptionFailed`. `loadConfig` enforces the 32-byte length
  before `run()` ever starts; configure wrongness fails fast at boot.
- **The HTTP credential API is write-only.** `GET
  /api/v1/providers/{id}/credentials` returns presence-only (`{"name": ...,
  "configured": true|false}`) and never the plaintext or a masked hint —
  ciphertext is only opened on the future fetch / scheduler path, never on a
  read endpoint. See [`internal/credential`](internal/credential).

HTTP server timeouts (`cmd/aud/main.go`) are compiled-in constants and are
not configurable at runtime:

| Timeout              | Value  |
| -------------------- | ------ |
| `ReadHeaderTimeout`  | `5s`   |
| `ReadTimeout`        | `10s`  |
| `WriteTimeout`       | `10s`  |
| `IdleTimeout`        | `60s`  |
| `ShutdownTimeout`    | `10s`  |

## Runtime

The process traps `SIGINT` and `SIGTERM` and performs a graceful shutdown:
it stops accepting new connections, waits up to `ShutdownTimeout` (10s) for
in-flight requests to complete, then exits 0. Hard kills (second signal,
`SIGKILL`) bypass this path.

## Logging

Logs are emitted to `stdout` as one JSON object per line using Go's
[`log/slog`](https://pkg.go.dev/log/slog) `JSONHandler`. The shape is:

| Key     | Type   | Notes                                          |
| ------- | ------ | ---------------------------------------------- |
| `time`  | string | RFC3339 timestamp.                             |
| `level` | string | `INFO` or `ERROR`.                             |
| `msg`   | string | Event message (e.g. `starting server`).        |
| `addr`  | string | Bind address (`:<port>`) on the startup line.  |
| `error` | string | Error string on failure paths.                 |

Pipe stdout straight into your aggregator (Loki, Splunk, CloudWatch Logs,
etc.) — no parser required.

## HTTP API

| Method | Path                       | Response                       | Purpose                  |
| ------ | -------------------------- | ------------------------------ | ------------------------ |
| `GET`  | `/healthz`                 | `200 {"status":"ok"}`          | Liveness probe.          |
| `GET`  | `/swaggerui`               | `200 text/html`                | Swagger UI for `/api/v1`.|
| `GET`  | `/swaggerui/openapi.yaml`  | `200 application/yaml`         | The spec Swagger UI renders (embedded from `api/openapi.yaml`). |

`/healthz` is intentionally minimal: it confirms the process is up and the
HTTP handler is reachable. It does **not** check downstream dependencies, so
it is suitable as both a startup probe and a liveness probe but is **not**
a readiness probe once external services are wired in. When downstream
checks are added they should land on a separate endpoint (e.g. `/readyz`)
so that liveness is not coupled to dependency health.

### `/api/v1` endpoints

| Method  | Path                                       | Response                                                  | Purpose                                                                 |
| ------- | ------------------------------------------ | --------------------------------------------------------- | ----------------------------------------------------------------------- |
| `GET`   | `/api/v1/providers`                        | `200 [Provider, ...]`                                     | List known providers with enabled state.                               |
| `GET`   | `/api/v1/providers/{id}`                   | `200 Provider` / `404`                                    | Get one provider (metadata + enabled state + declared credential fields). |
| `POST`  | `/api/v1/providers/{id}/enable`            | `200 Provider` / `404`                                    | Enable a provider. Idempotent — re-enabling an enabled provider succeeds. |
| `POST`  | `/api/v1/providers/{id}/disable`           | `200 Provider` / `404`                                    | Disable a provider. Idempotent.                                         |
| `PUT`   | `/api/v1/providers/{id}/credentials`       | `204` / `400` / `404` / `415`                             | Replace every credential value for a provider. Full set; unknown / missing fields rejected. |
| `GET`   | `/api/v1/providers/{id}/credentials`       | `200 {"fields":[{"name":...,"configured":true\|false}]}` | Presence-only — never returns the secret or any masked hint.            |
| `DELETE`| `/api/v1/providers/{id}/credentials`       | `204` / `404`                                             | Clear all stored credential values for a provider.                     |

A wrong-method request to a known `/api/v1` path returns `405` with an
`Allow` header set to the supported methods. An unknown `/api/v1` path
returns `404 not_found`. Both share the canonical error envelope below.

### Error responses

Every `/api/v1` error response uses the canonical envelope:

```json
{"error": {"code": "<code>", "message": "<message>", "details": {...optional}}}
```

Stable `code` values:

| Code                     | Typical status | When                                                            |
| ------------------------ | -------------- | --------------------------------------------------------------- |
| `validation_error`       | `400`          | Request body fails validation (e.g. unknown / missing credential field). May include structured `details`. |
| `not_found`              | `404`          | The requested provider id is not in the registry.               |
| `conflict`               | `409`          | Reserved for the future `POST /refresh` endpoint when a metadata-only provider has no registered `Fetcher`. |
| `unsupported_media_type` | `415`          | The request's `Content-Type` is not `application/json`.         |
| `internal_error`         | `500`          | An unexpected server error. Detail is never leaked to clients; the panic / error is logged server-side only. |

### `/api/v1` contract

[`api/openapi.yaml`](api/openapi.yaml) is the source-of-truth OpenAPI 3
contract for the `/api/v1` surface above. Workflow:

1. Edit `api/openapi.yaml` to change or add an endpoint.
2. Run `make generate` to regenerate the request/response types in
   `internal/api/types.gen.go` (do **not** hand-edit that file — it is
   overwritten by `oapi-codegen`).
3. Run `make spec-lint` — this is wired into `ci.yml` and fails the build if
   the committed spec is malformed.

`GET /swaggerui` serves a Swagger UI (`internal/docs`) so developers can
browse and exercise `/api/v1` routes directly from a browser. The page and
the spec it renders (`GET /swaggerui/openapi.yaml`) are both embedded at
build time from `api/openapi.yaml` via `go:embed` (see `api/openapi.go`), so
they never drift from the committed contract. The Swagger UI assets
themselves (`swagger-ui-dist`) load from a version-pinned CDN (`jsdelivr`),
so `/swaggerui` requires outbound internet access from the browser — the
`/api/v1` routes it documents do not.

The Swagger UI page loads `swagger-ui-dist` from a pinned CDN version and
embeds SRI integrity hashes on the `<script>` / `<link>` tags. To bump the
pinned version: download the new `swagger-ui-dist` release, compute the
SHA-384 of each asset (`openssl dgst -sha384 -binary <file> | openssl base64 -A`),
and update the matching `integrity` attribute in
[`internal/docs/ui.html`](internal/docs/ui.html) alongside the URL. Verify
`make test` still passes — `internal/docs` has tests that pin the
`integrity` attributes.

The HTTP skeleton (`internal/api`) is mounted under `/api/v1` in
`server.New()`:

- Every request passes through a middleware chain: a request-id injector
  (`X-Request-Id` response header), structured request logging
  (`log/slog`: method, path, status, duration, request id), and panic
  recovery (a recovered panic becomes a structured `500` — the panic value
  is logged server-side only, never sent to the client).
- Every error response uses the canonical envelope above — an unknown
  `/api/v1` route returns `404 not_found`, and a wrong-method request to a
  known route returns `405` with the canonical envelope and an `Allow`
  header.

## Shipped providers

The compiled-in registry at
[`internal/provider/provider.go`](internal/provider/provider.go) ships the
following providers today. Adding a new provider means appending an entry
to `Registry` in that file and shipping a new build (Model A — there is no
dynamic / plugin registration yet; see [`docs/plugins.md`](docs/plugins.md)
for the planned future shape).

| Provider id | Display name | Declared credential fields          |
| ----------- | ------------ | ----------------------------------- |
| `openai`    | OpenAI       | `api_key` (`Secret = true`)         |
| `anthropic` | Anthropic    | `api_key` (`Secret = true`)         |

The `api_key` value for each provider is set at runtime via
`PUT /api/v1/providers/{id}/credentials` (see [HTTP API](#http-api)) and
stored encrypted under [`internal/secret`](internal/secret). Each provider
has its own metadata entry and is reconciled against the database on boot —
disabling a provider is persisted across restarts; a provider that ships in
a future build is created in the database on first boot.

For the runtime side (how providers are polled and how `UsageMetric` is
shaped), see [`docs/providers.md`](docs/providers.md).

## Database

`aud` uses [SQLite](https://www.sqlite.org/) via the pure-Go
[`modernc.org/sqlite`](https://pkg.go.dev/modernc.org/sqlite) driver — no
CGO toolchain is required at build time, which is why the
[`Dockerfile`](Dockerfile) builds with `CGO_ENABLED=0` against a minimal
`gcr.io/distroless/static:nonroot` image. The DB path defaults to
`./data/aud.db` and is overridden by `AUD_DB_PATH`.

### Schema

| Table         | Columns                                                                                          | Notes                                                                                  |
| ------------- | ------------------------------------------------------------------------------------------------ | -------------------------------------------------------------------------------------- |
| `providers`   | `id TEXT PK`, `enabled INTEGER NOT NULL DEFAULT 0`, `created_at DATETIME NOT NULL`, `updated_at` | One row per compiled-in provider, reconciled on every boot. See [`internal/store/migrations/00001_create_providers.sql`](internal/store/migrations/00001_create_providers.sql). |
| `credentials` | `provider_id TEXT`, `field TEXT`, `ciphertext BLOB NOT NULL`, `created_at DATETIME NOT NULL`, `updated_at DATETIME NOT NULL`, PK `(provider_id, field)` | One row per `(provider, declared field)` with the AES-256-GCM ciphertext. See [`internal/store/migrations/00002_create_credentials.sql`](internal/store/migrations/00002_create_credentials.sql). |

There are no foreign keys declared yet — the migration does not add them
because no per-row reference is enforced — but the connection sets
`PRAGMA foreign_keys = ON` defensively for future migrations that need it.

### Connection behavior

The store is opened with these pragmas applied on every connect:

- `journal_mode = WAL` — write-ahead logging so readers do not block writers.
  Requires a filesystem that supports it (most do — including the named
  volume pattern in [Docker](#docker)).
- `busy_timeout = 5000` — wait up to five seconds for the write lock before
  returning a busy error.
- `foreign_keys = ON` — reserved for future migrations that use FKs.

`db.SetMaxOpenConns(1)` is set because `modernc.org/sqlite` is single-writer;
pooling more connections would just queue them. The parent directory of
`AUD_DB_PATH` is created with mode `0o750` on open if it does not exist, so
a fresh `./data/` works without a manual `mkdir`.

### Migrations

Migrations are SQL files in
[`internal/store/migrations/`](internal/store/migrations), embedded at
compile time via [`goose`](https://github.com/pressly/goose) into an
`embed.FS`. They run on every open (`sqlite.New`), are tracked in the
database itself, and are idempotent — re-running against an
already-migrated file is a no-op.

To add a new migration: create the next numbered file
(`NNNNN_<description>.sql`) using goose's `-- +goose Up` / `-- +goose Down`
markers (see the existing files for the format), then rebuild. There is no
`make migrate` target — the store migrates itself on boot.

## Docker

```sh
make docker  # docker build -t aud:local .
make smoke   # run the image and curl /healthz
```

The image is a multi-stage, statically linked, non-root build on
`gcr.io/distroless/static:nonroot`. The published image lives at
`ghcr.io/danstis/ai-usage-dashboard` and is tagged with the commit SHA, the
default branch (`latest`), and the semver on `v*` tag pushes.

The image sets `AUD_DB_PATH=/data/aud.db` and declares `/data` as a `VOLUME`,
pre-owned by the `nonroot` user/group (uid/gid `65532`) baked into the image.
Mount a **named volume** at `/data` to persist the database across container
restarts — Docker seeds a named volume from the image's `/data` directory
(and its ownership) on first use:

```sh
docker volume create aud-data
docker run -d -p 8080:8080 -v aud-data:/data ghcr.io/danstis/ai-usage-dashboard
```

A bind mount (`-v /host/path:/data`) instead uses the host directory's
existing ownership, which will not be writable by uid `65532` unless the
host path is created with matching permissions first.

## CI/CD

- `ci.yml` runs build, test, lint, and a SonarQube scan on pull requests and
  branch pushes.
- SonarQube reads committed configuration from `sonar-project.properties`
  (`sonar.projectKey=danstis_ai-usage-dashboard`,
  `sonar.organization=danstis`) and reports Go coverage from `coverage.out`.
- The Sonar step is gated on `SONAR_TOKEN`, so repositories or forks without
  that secret skip the scan cleanly.
- All GitHub Actions are pinned to commit SHAs (`helpers:pinGitHubActionDigests`)
  to keep the supply chain reproducible; bump via Renovate.
- `release-please.yml` maintains a release PR from conventional commits and
  tags releases on merge to `main`.
- `publish.yml` builds and publishes the container image to GHCR
  (`ghcr.io/danstis/ai-usage-dashboard`) on pushes to `main` and on `v*`
  tags, then smoke-tests the published image.

## Dependency updates

[Renovate](https://docs.renovatebot.com/) is configured via `renovate.json`.
Pull requests it opens are labelled by package manager so they can be
filtered and triaged independently:

| Manager          | Labels                              |
| ---------------- | ----------------------------------- |
| `gomod`          | `dependencies`, `deps:go-modules`   |
| `github-actions` | `dependencies`, `deps:github-actions` |
| `dockerfile`     | `dependencies`, `deps:docker`       |

GitHub Actions are pinned to commit digests
(`helpers:pinGitHubActionDigests`) and release notes are fetched for digest
updates (`helpers:githubDigestChangelogs`). The Dependency Dashboard issue
is enabled (`dependencyDashboard: true`) and lists every pending or
rate-limited update in one place — search the issue tracker for the
"Renovate" issues on this repo.

## Releases

Releases are driven by [release-please](https://github.com/googleapis/release-please):

1. Use [Conventional Commits](https://www.conventionalcommits.org/) on
   every PR. `feat:` triggers a minor bump, `fix:` and `perf:` trigger a
   patch bump, and `feat!:` / `BREAKING CHANGE:` triggers a major bump.
   `chore:`, `docs:`, `refactor:`, `test:`, `ci:`, `build:`, and `style:`
   do not bump the version but still appear in the changelog.
2. On every push to `main`, `release-please` opens or updates a "Release
   PR" containing the version bump, the updated `CHANGELOG.md`, and the
   release commit.
3. Merging the Release PR publishes the GitHub release and pushes a
   `vX.Y.Z` tag, which `publish.yml` consumes to publish the container
   image to GHCR with the semver tag.
