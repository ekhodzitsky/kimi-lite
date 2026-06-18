.PHONY: all build test coverage lint lint-fix install clean cross-compile deps fmt fmt-check vet generate vuln tidy-check bench graphify-install graphify-build graphify-watch graphify-serve graphify-query graphify-explain graphify-path

BINARY_NAME := kimi-lite
BIN_DIR := bin
CMD_DIR := ./cmd/kimi-lite

VERSION ?= dev
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE := $(shell git log -1 --format=%cI 2>/dev/null || date -u +%Y-%m-%dT%H:%M:%SZ)
BUILD_LDFLAGS := -s -w -buildid= -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

all: build

build:
	CGO_ENABLED=0 go build -trimpath -ldflags="$(BUILD_LDFLAGS)" -o $(BIN_DIR)/$(BINARY_NAME) $(CMD_DIR)

test:
	go test -race -coverprofile=coverage.out ./...

coverage:
	go tool cover -html=coverage.out

coverage-gate: test
	@echo "Checking coverage..."
	@COVERAGE=$$(go tool cover -func=coverage.out | tail -1 | awk '{print $$3}' | tr -d '%'); \
	MIN=$${MIN_COVERAGE:-70}; \
	if awk "BEGIN {exit !($$COVERAGE < $$MIN)}"; then \
		echo "Coverage $$COVERAGE% is below minimum $$MIN%"; \
		exit 1; \
	fi; \
	echo "Coverage $$COVERAGE% meets minimum $$MIN%"

lint:
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2 run ./...

lint-fix:
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2 run --fix ./...

install:
	CGO_ENABLED=0 go install -trimpath -ldflags="$(BUILD_LDFLAGS)" $(CMD_DIR)

clean:
	rm -rf $(BIN_DIR)
	rm -f coverage.out

cross-compile:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="$(BUILD_LDFLAGS)" -o $(BIN_DIR)/$(BINARY_NAME)-linux-amd64 $(CMD_DIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags="$(BUILD_LDFLAGS)" -o $(BIN_DIR)/$(BINARY_NAME)-linux-arm64 $(CMD_DIR)
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags="$(BUILD_LDFLAGS)" -o $(BIN_DIR)/$(BINARY_NAME)-darwin-amd64 $(CMD_DIR)
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags="$(BUILD_LDFLAGS)" -o $(BIN_DIR)/$(BINARY_NAME)-darwin-arm64 $(CMD_DIR)

deps:
	go mod tidy
	go mod download

fmt:
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2 fmt

fmt-check:
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2 fmt --diff

vet:
	go vet ./...

generate:
	go generate ./...

vuln:
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

tidy-check:
	go mod tidy && git diff --exit-code -- go.mod go.sum

bench:
	go test -bench=. -benchmem ./...

bench-regression:
	go test -bench=. -benchmem ./... > bench.new.txt
	@if [ ! -f bench.baseline.txt ]; then \
		cp bench.new.txt bench.baseline.txt; \
		echo "Created bench.baseline.txt"; \
	fi
	go run ./scripts/benchregression bench.baseline.txt bench.new.txt 0.20

fuzz:
	go test -run=^$$ -fuzz=FuzzHeuristicTokenEstimator -fuzztime=5s ./internal/core
	go test -run=^$$ -fuzz=FuzzRiskEvaluator -fuzztime=5s ./internal/core

# Graphify dev tooling
PYTHON        ?= python3
GRAPHIFY_VENV ?= .venv-graphify
GRAPHIFY_OUT  ?= graphify-out
GRAPHIFY_BIN  ?= $(GRAPHIFY_VENV)/bin/graphify

graphify-install: ## install graphify into a local venv
	@if ! command -v $(PYTHON) >/dev/null 2>&1; then \
		echo "$(PYTHON) is required for graphify-install"; \
		exit 1; \
	fi
	$(PYTHON) -m venv $(GRAPHIFY_VENV)
	$(GRAPHIFY_VENV)/bin/pip install --upgrade pip
	$(GRAPHIFY_VENV)/bin/pip install "graphifyy[mcp]"

graphify-build: ## build knowledge graph for this repo
	@if [ ! -x $(GRAPHIFY_BIN) ]; then \
		echo "graphify not found; run 'make graphify-install' first"; \
		exit 1; \
	fi
	$(GRAPHIFY_BIN) . --no-viz

graphify-watch: ## watch files and rebuild graph incrementally
	$(GRAPHIFY_BIN) . --watch

graphify-serve: ## serve graph.json as an MCP stdio server
	$(GRAPHIFY_VENV)/bin/python -m graphify.serve $(GRAPHIFY_OUT)/graph.json

graphify-query: ## usage: make graphify-query QUESTION="..."
	@if [ -z "$(QUESTION)" ]; then \
		echo "QUESTION is required; usage: make graphify-query QUESTION=\"...\""; \
		exit 1; \
	fi
	$(GRAPHIFY_BIN) query "$(QUESTION)"

graphify-explain: ## usage: make graphify-explain ENTITY="..."
	@if [ -z "$(ENTITY)" ]; then \
		echo "ENTITY is required; usage: make graphify-explain ENTITY=\"...\""; \
		exit 1; \
	fi
	$(GRAPHIFY_BIN) explain "$(ENTITY)"

graphify-path: ## usage: make graphify-path FROM="..." TO="..."
	@if [ -z "$(FROM)" ] || [ -z "$(TO)" ]; then \
		echo "FROM and TO are required; usage: make graphify-path FROM=\"...\" TO=\"...\""; \
		exit 1; \
	fi
	$(GRAPHIFY_BIN) path "$(FROM)" "$(TO)"
