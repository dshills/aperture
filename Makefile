.PHONY: help build install test lint fmt bench bench-prepare bench-clean smoke

# Pin the recipe shell to /bin/sh so the install target's case /
# parameter-expansion syntax doesn't have to survive cmd.exe. Windows
# users should build via WSL, Git Bash, or a POSIX-shell-enabled
# toolchain — attempting make under cmd.exe was never supported.
SHELL := /bin/sh

# `make` with no args should print the help menu, not silently do nothing.
.DEFAULT_GOAL := help

VERSION ?= dev
COMMIT := $(shell git rev-parse HEAD 2>/dev/null || echo unknown)
BUILD_DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

# Strip single AND double quotes from every value that ends up inside
# -ldflags so a stray quote in VERSION, COMMIT, or BUILD_DATE can't
# break out of either the `'…'` wrapping on the -X values or the
# outer `"…"` the recipe uses. Null-byte-style injection through
# shell-special characters like `$` / `\` / backticks is not guarded
# against — if you need that level of paranoia on a developer tool,
# ship a version string via a file, not the make command line.
SAFE_VERSION    := $(subst ",,$(subst ',,$(VERSION)))
SAFE_COMMIT     := $(subst ",,$(subst ',,$(COMMIT)))
SAFE_BUILD_DATE := $(subst ",,$(subst ',,$(BUILD_DATE)))

# On Windows, `go build` / `go install` produce aperture.exe. Derive
# the OS-specific extension from `go env GOEXE` so every binary
# reference in this Makefile stays correct across platforms.
GOEXE := $(shell go env GOEXE)
BINARY_NAME := aperture$(GOEXE)

# Install destination resolution:
#   - If GOBIN is set in the env, honor it.
#   - Otherwise leave INSTALL_DIR unset and let `go install` resolve
#     the default ($GOPATH/bin or $HOME/go/bin) via its own logic.
#     Make can't parse GOPATH portably because the path-list separator
#     differs across platforms (":" on Unix, ";" on Windows) and
#     Windows drive letters like `C:\` contain a colon themselves.
# Users can override explicitly via `make install INSTALL_DIR=/path`;
# when they do, the install target exports INSTALL_DIR so the recipe
# can safely read it at shell-eval time, and expands a leading ~ via a
# case statement (NOT eval, which would be a command-injection surface,
# and NOT sed, whose replacement string interprets &/|/\ specially).
GOBIN_ENV := $(shell go env GOBIN)
# abspath canonicalizes a relative GOBIN (e.g. the user set GOBIN=./bin
# before invoking make) so the install target works regardless of the
# working directory the sub-shells inherit. Leaves an empty GOBIN_ENV
# alone so the default-install branch still fires.
ifneq ($(GOBIN_ENV),)
GOBIN_ENV := $(abspath $(GOBIN_ENV))
endif
INSTALL_DIR ?= $(GOBIN_ENV)

# Each -X value is single-quoted so a SAFE_VERSION / SAFE_COMMIT /
# SAFE_BUILD_DATE carrying spaces (rare but legal for a development
# build tag) doesn't confuse the linker into parsing the rest as
# additional flags. The SAFE_* variables have quote characters
# stripped above to prevent quote-balance breakouts.
LDFLAGS := -X 'github.com/dshills/aperture/internal/version.Version=$(SAFE_VERSION)' \
           -X 'github.com/dshills/aperture/internal/version.Commit=$(SAFE_COMMIT)' \
           -X 'github.com/dshills/aperture/internal/version.BuildDate=$(SAFE_BUILD_DATE)'

help:
	@echo "Aperture — available targets:"
	@echo ""
	@echo "  build           compile ./cmd/aperture into bin/$(BINARY_NAME) (version-stamped)"
	@echo "  install         install $(BINARY_NAME) into \$$INSTALL_DIR (default: go install target dir)"
	@echo "  test            run 'go test ./...'"
	@echo "  lint            run 'golangci-lint run ./...'"
	@echo "  fmt             run 'gofmt -s -w .'"
	@echo ""
	@echo "  bench           build, materialize fixtures, and run the §8.2 harness"
	@echo "  bench-prepare   regenerate testdata/bench/{small,medium} fixtures"
	@echo "  bench-clean     remove generated bench fixtures"
	@echo ""
	@echo "  help            show this message (default target)"
	@echo ""
	@echo "Overrides:"
	@echo "  VERSION=<tag>       set the build's version string (default: dev)"
	@echo "  INSTALL_DIR=<path>  override install destination"

build:
	go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY_NAME) ./cmd/aperture

# `install` builds with the same ldflag stamping as `build` so the
# installed binary reports the correct version/commit/date. The
# explicit GOBIN= prefix forces `go install` to honor INSTALL_DIR even
# when the invoking shell has its own GOBIN exported — otherwise a
# user-supplied `make install INSTALL_DIR=...` would silently install
# to the environment's GOBIN while the success message reported the
# override path.
# `export` makes INSTALL_DIR visible to the shell WITHOUT Make
# interpolating it into the recipe text. That matters because a value
# carrying shell metacharacters (e.g. a stray `"; rm -rf /; #`) would
# otherwise inject commands via make's textual substitution. Accessing
# the variable as $$INSTALL_DIR inside the recipe reads the env var at
# shell-eval time, which is safe regardless of the value's contents.
export INSTALL_DIR

install:
	@# When INSTALL_DIR is empty, let `go install` use its own
	@# platform-aware default (GOBIN or $$GOPATH/bin). When set,
	@# expand a leading ~ via a case statement — sed s|^~|$$HOME|
	@# would fail if $$HOME contained &, |, or \, but case matching
	@# doesn't interpret the replacement string.
	@if [ -n "$$INSTALL_DIR" ]; then \
		case "$$INSTALL_DIR" in \
			'~')    dir="$$HOME" ;; \
			'~/'*)  dir="$$HOME/$${INSTALL_DIR#~/}" ;; \
			*)      dir="$$INSTALL_DIR" ;; \
		esac; \
		case "$$dir" in \
			/*)              ;; \
			[A-Za-z]:[\\/]*) ;; \
			'\\\\'*)         ;; \
			*)               dir="$$PWD/$$dir" ;; \
		esac; \
		mkdir -p "$$dir" && \
		GOBIN="$$dir" go install -ldflags "$(LDFLAGS)" ./cmd/aperture && \
		echo "$(BINARY_NAME) installed to $$dir/$(BINARY_NAME)"; \
	else \
		go install -ldflags "$(LDFLAGS)" ./cmd/aperture && \
		echo "$(BINARY_NAME) installed via 'go install' defaults"; \
	fi

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
	go run ./cmd/apbench -bin ./bin/$(BINARY_NAME) -fixtures testdata/bench

# Materializes testdata/bench/{small,medium}/. The fixtures themselves
# are NOT committed (see .gitignore) — CI regenerates them on demand so
# the repo stays small.
bench-prepare:
	go run ./cmd/apbenchfixtures -root testdata/bench

bench-clean:
	rm -rf testdata/bench/small testdata/bench/medium

# Opt-in real-agent smoke test. Drives `aperture run claude` against a
# copy of testdata/fixtures/small_go using the real claude CLI on PATH.
# Hits the network and consumes tokens — not part of `make test`. The
# test itself auto-skips when claude is not on PATH.
smoke:
	go test -tags smoke -run TestSmoke_RealClaudeAdapter -v ./internal/cli

# -----------------------------------------------------------------------
# v1.1 eval harness shortcuts. `make eval` runs the committed
# selection-quality regression gate; `make eval-baseline` is the
# reviewer-only helper for regenerating baseline.json after a
# deliberate scoring change (PLAN §Phase 1 / §Phase 2).
.PHONY: eval eval-baseline eval-ripgrep
eval: build
	./bin/$(BINARY_NAME) eval run --fixtures testdata/eval

eval-baseline: build
	./bin/$(BINARY_NAME) eval baseline --fixtures testdata/eval --force

# §12.2 ripgrep comparator — runs as an informational CI step
# right now; gating on the 1.2× F1 bar is deferred until the
# §7.5.0 viability threshold is retuned (a few small fixtures
# score 0 on both sides).
eval-ripgrep: build
	./bin/$(BINARY_NAME) eval ripgrep --fixtures testdata/eval --top-n 20 --format markdown

# v1.1 tier-2 grammar license check per PLAN §Phase 4. Asserts that
# the upstream tree-sitter modules ship LICENSE files (they do —
# smacker/go-tree-sitter vendors LICENSE into every language subdir).
# The check here runs against the Go module cache rather than a
# vendored tree; CI invokes this as a required step.
.PHONY: check-grammar-licenses
check-grammar-licenses:
	@set -e; \
	MOD=$$(go env GOMODCACHE)/github.com/smacker/go-tree-sitter@$$(go list -m -f '{{.Version}}' github.com/smacker/go-tree-sitter); \
	for lang in typescript/typescript typescript/tsx javascript python; do \
		if [ ! -s "$$MOD/LICENSE" ] && [ ! -s "$$MOD/$$lang/LICENSE" ]; then \
			echo "missing LICENSE for $$lang under $$MOD"; exit 1; \
		fi; \
	done
	@echo "grammar license check: OK"

# -----------------------------------------------------------------------
# v1.1 §12 release gate. Runs every blocking step in the order
# PLAN §Phase 7 requires:
#   1. build + vet
#   2. test (unit + integration)
#   3. lint
#   4. race tests on the eval-critical packages
#   5. check-grammar-licenses
#   6. eval run (catches any F1 regression vs. committed baseline)
#
# Any failing step aborts with a non-zero exit. `clarion verify`
# runs as an advisory final step (doesn't gate the release; per
# PLAN §Phase 7 the normative contract is the 11 §12 acceptance
# criteria, not the external pipeline gates).
.PHONY: release
release: build
	@set -e; \
	echo "==> go vet ./..."; go vet ./...; \
	echo "==> go test ./..."; go test ./...; \
	echo "==> golangci-lint run ./..."; golangci-lint run ./...; \
	echo "==> go test -race (eval + cli + pipeline)"; \
	go test -race ./internal/eval ./internal/cli ./internal/pipeline ./internal/selection; \
	echo "==> make check-grammar-licenses"; $(MAKE) check-grammar-licenses; \
	echo "==> aperture eval run (v1.1 regression gate)"; \
	./bin/$(BINARY_NAME) eval run --fixtures testdata/eval; \
	echo; echo "release gate: all checks passed"
