# Sagittarius — build and quality targets

GO ?= go
GO_VERSION := 1.26.5
# Pin the toolchain to match go.mod for reproducible builds (local + CI).
# go(1) auto-downloads this version if the system Go is older.
export GOTOOLCHAIN := go$(GO_VERSION)

GOPATH := $(shell $(GO) env GOPATH)
export PATH := $(GOPATH)/bin:$(PATH)

GOLANGCI_LINT := github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2
GOVULNCHECK := golang.org/x/vuln/cmd/govulncheck@latest

BIN_DIR := bin
BINARY := $(BIN_DIR)/sagittarius
MODULE := github.com/undeadindustries/sagittarius

# Rebuild when any Go source or module metadata changes (avoid stale bin/sagittarius).
GO_PKG := ./...
GO_SOURCES := $(shell $(GO) list -f '{{range .GoFiles}}{{$$.Dir}}/{{.}} {{end}}' $(GO_PKG) 2>/dev/null)

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -ldflags "-X $(MODULE)/internal/version.Version=$(VERSION) \
	-X $(MODULE)/internal/version.Commit=$(COMMIT) \
	-X $(MODULE)/internal/version.BuildDate=$(BUILD_DATE)"

# BUILD_TAGS selects the embedded find_symbol grammar set. grammar_set_core
# ships the curated Core100 languages (smaller binary) instead of all ~200; the
# release build (.goreleaser.yaml) uses the same tag. Tests use it too so
# coverage matches what ships.
BUILD_TAGS := grammar_set_core

.PHONY: build test vet lint race clean tools vulncheck e2e e2e-mock release-snapshot

build: $(BINARY)

$(BINARY): go.mod go.sum $(GO_SOURCES)
	@mkdir -p $(BIN_DIR)
	$(GO) build -tags '$(BUILD_TAGS)' $(LDFLAGS) -o $(BINARY) ./cmd/sagittarius

test:
	$(GO) test -tags '$(BUILD_TAGS)' ./...

# e2e runs the live end-to-end suite against real providers using cheap models.
# Requires at least one provider API key (Gemini, OpenAI). Makes billable calls.
e2e: $(BINARY)
	SAGITTARIUS_E2E_LIVE=1 SAGITTARIUS_BIN=$(abspath $(BINARY)) $(GO) test -count=1 ./tests/e2e/...

# e2e-mock runs the same scenario table against an in-process mock; no keys.
e2e-mock: $(BINARY)
	SAGITTARIUS_E2E_MOCK=1 SAGITTARIUS_BIN=$(abspath $(BINARY)) $(GO) test -count=1 ./tests/e2e/...

vet:
	$(GO) vet -tags '$(BUILD_TAGS)' ./...

lint:
	$(GO) run $(GOLANGCI_LINT) run ./...

race:
	$(GO) test -tags '$(BUILD_TAGS)' -race ./...

clean:
	rm -rf $(BIN_DIR)

# Optional: install dev tools into $(GOPATH)/bin with the local Go toolchain.
tools:
	$(GO) install $(GOLANGCI_LINT)
	$(GO) install $(GOVULNCHECK)

vulncheck:
	$(GO) run $(GOVULNCHECK) ./...

release-snapshot:
	goreleaser release --snapshot --clean

