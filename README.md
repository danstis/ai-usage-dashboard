# ai-usage-dashboard

A simple AI Subscription usage dashboard.

## Overview

`aud` is a small Go HTTP service that exposes operational endpoints for the
AI Usage Dashboard. The binary lives in `cmd/aud` and the HTTP handlers in
`internal/server`. The frontend is tracked under `web/` (currently a
placeholder pending a later phase of the project).

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

`make run` requires `AUD_MASTER_KEY` to be set (see [Configuration](#configuration)):

```sh
AUD_MASTER_KEY=$(openssl rand -base64 32) make run
```

## Configuration

The service reads its configuration from environment variables. `loadConfig`
returns an error on any invalid value, and `run()` fails fast before the HTTP
server starts.

| Variable            | Default          | Description                                                                  |
| ------------------- | ---------------- | ----------------------------------------------------------------------------- |
| `AUD_HTTP_PORT`      | `8080`           | TCP port the HTTP server binds to.                                            |
| `AUD_MASTER_KEY`     | _(required)_     | Standard base64, must decode to exactly 32 bytes (AES-256), used to encrypt provider credentials at rest. **Required** — boot fails fast if it is unset, not valid base64, or the wrong length; the value is never logged. Generate one with `openssl rand -base64 32`. |
| `AUD_POLL_INTERVAL`  | `5m`              | Parsed as a Go `time.Duration` (e.g. `90s`, `5m`). An invalid value fails boot. |
| `AUD_DB_PATH`        | `./data/aud.db`  | Filesystem path to the SQLite database file.                                  |

Startup logs one `configuration loaded` line with `port`, `pollInterval`,
`dbPath`, and `masterKeySet` (a boolean) — the master key bytes themselves are
never included in logs (see `config.LogValue` in `cmd/aud/main.go`).

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

### `/api/v1` contract

[`api/openapi.yaml`](api/openapi.yaml) is the source-of-truth OpenAPI 3
contract for the `/api/v1` provider registry surface. `make spec-lint`
validates the committed spec and fails the build if it is malformed;
`make generate` regenerates the request/response types in
`internal/api/types.gen.go` via `oapi-codegen`.

`GET /swaggerui` serves a Swagger UI (`internal/docs`) so developers can
browse and exercise `/api/v1` routes directly from a browser. The page and
the spec it renders (`GET /swaggerui/openapi.yaml`) are both embedded at
build time from `api/openapi.yaml` via `go:embed` (see `api/openapi.go`), so
they never drift from the committed contract. The Swagger UI assets
themselves (`swagger-ui-dist`) load from a version-pinned CDN (`jsdelivr`),
so `/swaggerui` requires outbound internet access from the browser — the
`/api/v1` routes it documents do not.

The HTTP skeleton (`internal/api`) is mounted under `/api/v1` in
`server.New()`:

- Every request passes through a middleware chain: a request-id injector
  (`X-Request-Id` response header), structured request logging
  (`log/slog`: method, path, status, duration, request id), and panic
  recovery (a recovered panic becomes a structured `500` — the panic value
  is logged server-side only, never sent to the client).
- Every error response uses the canonical envelope
  `{"error":{"code":"...","message":"..."}}` — an unknown `/api/v1` route
  returns a structured `404 not_found`, and a wrong-method request to a
  known route returns a structured `405`.
- `GET /api/v1/providers` currently returns `[]`: it is wired to a
  `ProviderLister` interface seam with an in-memory stub
  (`internal/api`) so the contract is exercised end-to-end before SQLite
  persistence and the real provider registry land in later phases.

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
