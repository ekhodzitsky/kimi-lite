.PHONY: all build test coverage lint lint-fix install clean cross-compile deps fmt fmt-check vet generate vuln tidy-check bench

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
