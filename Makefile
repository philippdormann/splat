.PHONY: build run test test-race vet bench docker fmt

build:
	go build -o bin/errgotrack ./cmd/server

run: build
	./bin/errgotrack

test:
	go test ./...

test-race:
	go test -race ./...

vet:
	go vet ./...

bench:
	go test -bench=. -benchmem ./internal/ingestion/... ./internal/issues/...

docker:
	docker build -t errgotrack:latest .

fmt:
	gofmt -l -w .
