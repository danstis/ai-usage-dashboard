BINARY := aud
IMAGE  := aud:local
PORT   := 8080

.PHONY: build
build:
	go build -trimpath -o bin/$(BINARY) ./cmd/aud

.PHONY: test
test:
	go test ./... -race -covermode=atomic -coverprofile=coverage.out

.PHONY: lint
lint:
	golangci-lint run

.PHONY: spec-lint
spec-lint:
	go run ./cmd/spec-lint api/openapi.yaml

.PHONY: generate
generate:
	go tool oapi-codegen -config oapi-codegen.yaml api/openapi.yaml

.PHONY: run
run:
	go run ./cmd/aud

.PHONY: tidy
tidy:
	go mod tidy

.PHONY: docker
docker:
	docker build -t $(IMAGE) .

.PHONY: smoke
smoke:
	docker run -d --rm -p $(PORT):$(PORT) --name aud-smoke $(IMAGE)
	sleep 1
	curl -fsS http://localhost:$(PORT)/healthz
	docker stop aud-smoke
