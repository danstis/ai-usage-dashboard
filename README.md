# ai-usage-dashboard

A simple AI Subscription usage dashboard

## Development

Requires Go 1.24+.

```sh
make build   # go build ./cmd/aud -> bin/aud
make test    # go test ./... -race -covermode=atomic -coverprofile=coverage.out
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

- `ci.yml` runs build, test, lint, and a SonarQube scan on pull requests and
  branch pushes.
- SonarQube reads committed configuration from `sonar-project.properties`
  (`sonar.projectKey=danstis_ai-usage-dashboard`,
  `sonar.organization=danstis`) and reports Go coverage from `coverage.out`.
- The Sonar step is gated on `SONAR_TOKEN`, so repositories or forks without
  that secret skip the scan cleanly.
- `release-please.yml` maintains a release PR from conventional commits and
  tags releases on merge to `main`.
- `publish.yml` builds and publishes the container image to GHCR
  (`ghcr.io/danstis/ai-usage-dashboard`) on pushes to `main` and on `v*`
  tags, then smoke-tests the published image.
