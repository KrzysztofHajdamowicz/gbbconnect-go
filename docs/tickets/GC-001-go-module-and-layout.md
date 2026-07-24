# GC-001 - Go module & package layout

- **Epic:** A - Scaffolding & tooling
- **Type:** Chore
- **Priority:** High
- **Status:** DONE
- **Estimate:** 0.5 day
- **Depends on:** -
- **Blocks:** everything

## Context

- Architecture & layout: [docs/01-architecture.md](../01-architecture.md) §2.
- Target language decision: [README.md](../../README.md) §3.

## Description

Create the Go module and the package skeleton so subsequent tickets have a place
to put code. No business logic yet — just compiling stubs and the directory
structure.

Module path suggestion: `github.com/<owner>/gbbconnect-go` (adjust to the real
repo). Binary: `gbbconnect`.

## Tasks

- `go mod init` with the chosen module path; pin a recent Go version in `go.mod`.
- Create the package tree from [01-architecture.md](../01-architecture.md) §2:
  `cmd/gbbconnect`, `internal/{config,state,cloud,protocol,modbus,driver,
  discovery,supervisor,logbuf}` and driver sub-packages.
- Add a `cmd/gbbconnect/main.go` with a Cobra (or stdlib `flag`) root command and
  two subcommands stubbed: `run` and `discover` (see GC-060, GC-052).
- Add a `version` variable wired for `-ldflags "-X ..."` injection.
- Add `Makefile` or `Taskfile` targets: `build`, `test`, `lint`, `tidy`.

## Acceptance criteria

- `go build ./...` succeeds.
- `gbbconnect --help`, `gbbconnect run --help`, `gbbconnect discover --help`
  print usage.
- `gbbconnect version` prints the injected version.
- Package directories exist with at least a `doc.go` or stub each, so imports
  resolve.

## Test notes

- A trivial `TestVersion` confirming the version variable is wired.
- CI (GC-003) will enforce build/test on this skeleton.
