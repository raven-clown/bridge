.PHONY: build test lint check

build:
	go build -o bin/ ./cmd/bridge

test:
	go test ./...

lint:
	golangci-lint run ./...

check: lint
	go test -race -cover ./...
