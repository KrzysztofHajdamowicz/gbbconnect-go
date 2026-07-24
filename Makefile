BINARY := gbbconnect
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
GOLANGCI_LINT_VERSION ?= v2.12.2
GOLANGCI_LINT := $(CURDIR)/bin/golangci-lint

.PHONY: build build-all test lint tidy

build:
	mkdir -p bin
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/gbbconnect

build-all:
	VERSION="$(VERSION)" ./scripts/build-all.sh

test:
	go test ./... -race -count=1

lint: $(GOLANGCI_LINT)
	$(GOLANGCI_LINT) run

tidy:
	go mod tidy

$(GOLANGCI_LINT):
	mkdir -p bin
	GOBIN=$(CURDIR)/bin go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

