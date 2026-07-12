.PHONY: build test lint run tidy docker smoke

IMAGE ?= aud:local
PORT ?= 8080

build:
	go build ./...

test:
	go test ./... -race -covermode=atomic

lint:
	golangci-lint run

run:
	go run ./cmd/aud

tidy:
	go mod tidy

docker:
	docker build -t $(IMAGE) .

smoke:
	@set -e; \
	cid=$$(docker run -d --rm -p $(PORT):8080 $(IMAGE)); \
	trap 'docker stop $$cid >/dev/null' EXIT; \
	for i in $$(seq 1 20); do \
		body=$$(curl -fsS http://localhost:$(PORT)/healthz || true); \
		if [ "$$body" = '{"status":"ok"}' ]; then \
			echo "$$body"; \
			exit 0; \
		fi; \
		sleep 1; \
	done; \
	echo "health check failed" >&2; \
	exit 1
