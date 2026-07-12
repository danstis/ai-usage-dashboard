# ai-usage-dashboard

A simple AI Subscription usage dashboard

## Development

Requirements:

- Go 1.24.1
- Docker
- golangci-lint

Run the service locally:

```sh
make run
curl -fsS http://localhost:8080/healthz
```

The HTTP port defaults to `8080` and can be changed with `AUD_HTTP_PORT`.

## Checks

```sh
make build
make test
make lint
```

## Container

Build and smoke test the local image:

```sh
make docker
make smoke
```

The image runs as a non-root user and serves `GET /healthz` with `{"status":"ok"}`.

## CI/CD

GitHub Actions runs build, race-enabled tests, linting, and SonarQube Cloud scanning on pull requests and non-main branch pushes. SonarQube Cloud uses repository variable `SONAR_PROJECT` and secret `SONAR_TOKEN`.

Merges to `main` run release-please. Pushes to `main` and release tags publish the container image to GitHub Container Registry as `ghcr.io/danstis/ai-usage-dashboard`.
