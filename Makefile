BINARY := aud
IMAGE  := aud:local
PORT   := 8080

.PHONY: build
build:
	go build -trimpath -o bin/$(BINARY) ./cmd/aud

.PHONY: test
test:
	go test ./... -race -covermode=atomic

.PHONY: lint
lint:
	golangci-lint run

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
