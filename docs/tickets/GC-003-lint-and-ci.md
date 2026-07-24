# GC-003 - Lint, format, unit-test CI

- **Epic:** A - Scaffolding & tooling
- **Type:** Chore
- **Priority:** Medium
- **Status:** DONE
- **Estimate:** 0.5 day
- **Depends on:** GC-001
- **Blocks:** GC-074

## Context

- [docs/10-compatibility-and-testing.md](../10-compatibility-and-testing.md) for
  the test strategy CI must run.

## Description

Set up continuous integration so every change is built, vetted, linted, and
tested.

## Tasks

- GitHub Actions workflow (`.github/workflows/ci.yml`) running on push/PR:
  - `go build ./...`
  - `go vet ./...`
  - `golangci-lint run` (add `.golangci.yml` with a sensible ruleset:
    govet, staticcheck, errcheck, ineffassign, gofmt/goimports).
  - `go test ./... -race -count=1`
- Cache the Go build/module cache.
- Fail the build on lint or test failure.

## Acceptance criteria

- CI passes on the GC-001 skeleton.
- A deliberately unformatted file or unused variable fails CI locally via
  `golangci-lint run`.
- `-race` is enabled for tests.

## Test notes

- This ticket is itself validated by CI going green on a trivial PR.
