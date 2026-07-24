BINARY := gbbconnect
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
GOLANGCI_LINT_VERSION ?= v2.12.2
GOLANGCI_LINT := $(CURDIR)/bin/golangci-lint

.PHONY: build build-all coverage coverage-protocol test lint tidy

build:
	mkdir -p bin
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/gbbconnect

build-all:
	VERSION="$(VERSION)" ./scripts/build-all.sh

test:
	go test ./... -race -count=1

coverage:
	go test ./... -race -covermode=atomic -coverprofile=coverage.out -count=1
	go tool cover -func=coverage.out | tee coverage.txt
	go tool cover -html=coverage.out -o coverage.html

coverage-protocol:
	./scripts/check-protocol-coverage.sh

lint: $(GOLANGCI_LINT)
	$(GOLANGCI_LINT) run

tidy:
	go mod tidy

$(GOLANGCI_LINT):
	mkdir -p bin
	GOBIN=$(CURDIR)/bin go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
