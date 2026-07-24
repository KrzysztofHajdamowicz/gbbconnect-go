# GC-004 - Cross-compile build matrix

- **Epic:** A - Scaffolding & tooling
- **Type:** Chore
- **Priority:** Medium
- **Status:** DONE
- **Estimate:** 0.5 day
- **Depends on:** GC-001
- **Blocks:** GC-070, GC-074

## Context

- Targets and rationale: [docs/09-deployment.md](../09-deployment.md) §1.

## Description

Make the binary cross-compile cleanly for all deployment targets, with static
(`CGO_ENABLED=0`) builds where possible.

## Tasks

- Add a build script / Make target that builds the matrix:
  linux/amd64, linux/arm64, linux/arm (GOARM=7), windows/amd64, darwin/arm64.
- Ensure `CGO_ENABLED=0` works; if the serial library (GC-034) needs cgo on some
  platform, document and isolate it behind build tags so the default stays
  static.
- Embed version via `-ldflags "-s -w -X main.version=..."`.
- Produce named artifacts (e.g. `gbbconnect_<os>_<arch>[.exe]`).

## Acceptance criteria

- All matrix targets build from a clean checkout.
- Linux binaries are statically linked (verify with `file`/`ldd`).
- Version is embedded and printed by `gbbconnect version`.

## Test notes

- A CI job (GC-003/GC-074) runs the matrix build to catch platform breakage early.
