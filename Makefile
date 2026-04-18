.PHONY: build test lint fmt bench bench-prepare bench-clean

VERSION ?= dev
COMMIT := $(shell git rev-parse HEAD 2>/dev/null || echo unknown)
BUILD_DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -X github.com/dshills/aperture/internal/version.Version=$(VERSION) \
           -X github.com/dshills/aperture/internal/version.Commit=$(COMMIT) \
           -X github.com/dshills/aperture/internal/version.BuildDate=$(BUILD_DATE)

build:
	go build -ldflags "$(LDFLAGS)" -o bin/aperture ./cmd/aperture

test:
	go test ./...

lint:
	golangci-lint run ./...

fmt:
	gofmt -s -w .

# `make bench` drives the §8.2 harness. Requires `bench-prepare` to have
# materialized the fixtures first; the target depends on `build` so the
# binary under test is current.
bench: build bench-prepare
	go run ./cmd/apbench -bin ./bin/aperture -fixtures testdata/bench

# Materializes testdata/bench/{small,medium}/. The fixtures themselves
# are NOT committed (see .gitignore) — CI regenerates them on demand so
# the repo stays small.
bench-prepare:
	go run ./cmd/apbenchfixtures -root testdata/bench

bench-clean:
	rm -rf testdata/bench/small testdata/bench/medium
