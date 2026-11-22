.PHONY: build test lint cover check snapshot

build:
	go build -o bin/ ./cmd/bridge

test:
	go test ./...

lint:
	golangci-lint run ./...

# Coverage leaves out forgetest and tuitest, which only tests use.
cover:
	go test -coverprofile=cover.out ./...
	grep -vE '/(forgetest|tuitest)/' cover.out > cover.prod.out
	go tool cover -func=cover.prod.out | awk '/^total:/ { sub("%", "", $$3); print "coverage " $$3 "%"; if ($$3 + 0 < 80) { print "coverage is below 80%"; exit 1 } }'

check: lint cover

snapshot:
	goreleaser release --snapshot --clean
