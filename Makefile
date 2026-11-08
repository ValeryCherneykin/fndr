# Developer shortcuts. CI runs the same commands, so `make check` passing
# locally means CI should pass too.

BINARY  := fndr
PKG     := ./cmd/fndr
BIN_DIR := bin

# Version from git: the nearest tag, commit hash and "-dirty" for uncommitted
# changes, e.g. v0.1.0-3-gabc1234-dirty. Falls back to "dev" before the first
# commit, when git describe has nothing to describe.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

GOLANGCI_LINT ?= golangci-lint

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show this help
	@awk 'BEGIN {FS = ":.*## "} /^[a-zA-Z_-]+:.*## / {printf "  \033[36m%-8s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

.PHONY: build
build: ## Build the binary into bin/
	go build -trimpath -ldflags '$(LDFLAGS)' -o $(BIN_DIR)/$(BINARY) $(PKG)

.PHONY: install
install: ## Install fndr into $GOBIN (~/go/bin) with the version stamped
	go install -trimpath -ldflags '$(LDFLAGS)' $(PKG)

.PHONY: run
run: ## Run fndr; pass flags with ARGS="-version"
	go run $(PKG) $(ARGS)

.PHONY: test
test: ## Run tests with the race detector
	go test -race -count=1 ./...

.PHONY: cover
cover: ## Run tests and open an HTML coverage report
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "open coverage.html"

.PHONY: lint
lint: ## Run golangci-lint (formatters included)
	$(GOLANGCI_LINT) run ./...

.PHONY: fmt
fmt: ## Format code with gofmt and goimports via golangci-lint
	$(GOLANGCI_LINT) fmt ./...

.PHONY: tidy
tidy: ## Tidy go.mod and go.sum
	go mod tidy

.PHONY: check
check: ## Everything CI checks: tidy, vet, lint, test
	go mod tidy -diff
	go vet ./...
	$(GOLANGCI_LINT) run ./...
	go test -race -count=1 ./...

.PHONY: clean
clean: ## Remove build and coverage artifacts
	rm -rf $(BIN_DIR) dist coverage.out coverage.html
