.PHONY: build run test test-race vet bench docker fmt frontend-install frontend-build

build: frontend-build
	go build -o bin/splat ./cmd/server

frontend-install:
	cd frontend && bun install

frontend-build: frontend-install
	cd frontend && bun run build

run: build
	./bin/splat

test:
	go test ./...

test-race:
	go test -race ./...

vet:
	go vet ./...

bench:
	go test -bench=. -benchmem ./internal/ingestion/... ./internal/issues/...

docker:
	docker build -t ghcr.io/philippdormann/splat:latest .

fmt:
	gofmt -l -w .
