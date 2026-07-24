# GC-070 - Multi-arch Dockerfile

- **Epic:** H - Packaging
- **Type:** Feature
- **Priority:** High
- **Status:** DONE
- **Estimate:** 0.5 day
- **Depends on:** GC-004, GC-062
- **Blocks:** GC-071, GC-074

## Context

- Dockerfile sketch + run: [docs/09-deployment.md](../09-deployment.md) §2.
- Original Dockerfile (for comparison): [`../Dockerfile`](../../../Dockerfile).

## Description

Provide a small, non-root, multi-arch container image.

## Tasks

- `deploy/Dockerfile` using a Go build stage and a distroless/scratch runtime
  stage (see [09](../09-deployment.md) §2).
- `CGO_ENABLED=0`, `TARGETOS`/`TARGETARCH` from buildx; `-ldflags "-s -w"` +
  version injection.
- Non-root user; `ENTRYPOINT` the binary; default `CMD` runs `run --config
  /config/gbbconnect.yaml`.
- Document volumes: `/config` (ro) and `/data` (state + logs), and `--device` for
  serial.
- A `.dockerignore`.

## Acceptance criteria

- `docker buildx build --platform linux/amd64,linux/arm64,linux/arm/v7` succeeds.
- Image runs as non-root and starts with a mounted config.
- Image size is small (single static binary on distroless/scratch).

## Test notes

- CI builds the multi-arch image (GC-074).
- A smoke run with a sample config + mock broker URL confirms startup logs.
