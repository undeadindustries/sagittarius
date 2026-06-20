# Sagittarius — build and quality targets

GO ?= go
GO_VERSION := 1.26.4
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

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -ldflags "-X $(MODULE)/internal/version.Version=$(VERSION) \
	-X $(MODULE)/internal/version.Commit=$(COMMIT) \
	-X $(MODULE)/internal/version.BuildDate=$(BUILD_DATE)"

.PHONY: build test vet lint race clean tools vulncheck

build: $(BINARY)

$(BINARY):
	@mkdir -p $(BIN_DIR)
	$(GO) build $(LDFLAGS) -o $(BINARY) ./cmd/sagittarius

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

lint:
	$(GO) run $(GOLANGCI_LINT) run ./...

race:
	$(GO) test -race ./...

clean:
	rm -rf $(BIN_DIR)

# Optional: install dev tools into $(GOPATH)/bin with the local Go toolchain.
tools:
	$(GO) install $(GOLANGCI_LINT)
	$(GO) install $(GOVULNCHECK)

vulncheck:
	$(GO) run $(GOVULNCHECK) ./...
