.PHONY: build test lint fmt bench

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

bench:
	@echo "bench target: populated by Phase 6"
