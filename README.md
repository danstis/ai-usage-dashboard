# ai-usage-dashboard

A simple AI Subscription usage dashboard

## Development

Requires Go 1.24+.

```sh
make build   # go build ./cmd/aud -> bin/aud
make test    # go test ./... -race -covermode=atomic
make lint    # golangci-lint run
make run     # go run ./cmd/aud
```

The server reads `AUD_HTTP_PORT` (default `8080`) and exposes `GET /healthz`,
which returns `200` with `{"status":"ok"}`.

## Docker

```sh
make docker  # docker build -t aud:local .
make smoke   # run the image and curl /healthz
```

The image is a multi-stage, statically linked, non-root build on
`gcr.io/distroless/static:nonroot`.

## CI/CD

- `ci.yml` runs build, test, and lint on pull requests and branch pushes.
- `release-please.yml` maintains a release PR from conventional commits and
  tags releases on merge to `main`.
- `publish.yml` builds and publishes the container image to GHCR
  (`ghcr.io/danstis/ai-usage-dashboard`) on pushes to `main` and on `v*`
  tags, then smoke-tests the published image.
