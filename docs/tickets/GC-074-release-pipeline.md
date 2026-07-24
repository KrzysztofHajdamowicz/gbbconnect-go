# GC-074 - Release pipeline

- **Epic:** H - Packaging
- **Type:** Chore
- **Priority:** Medium
- **Status:** TODO
- **Estimate:** 1 day
- **Depends on:** GC-003, GC-004, GC-070
- **Blocks:** -

## Context

- Release goals: [docs/09-deployment.md](../09-deployment.md) §7.

## Description

Automate releases: build the platform matrix, publish container images and the HA
add-on image set, attach checksummed archives to GitHub Releases on tags.

## Tasks

- Tag-triggered GitHub Actions workflow (consider GoReleaser).
- Build binaries for the GC-004 matrix; produce `.tar.gz`/`.zip` archives + a
  `SHA256SUMS` file.
- Build and push multi-arch images to GHCR (`ghcr.io/<owner>/gbbconnect-go`),
  tags `latest` + semver.
- Build/push the HA add-on images (GC-071) per arch.
- Generate release notes/changelog.

## Acceptance criteria

- Pushing a `vX.Y.Z` tag produces a GitHub Release with all artifacts +
  checksums.
- Multi-arch images are pullable and run on amd64 and arm64.
- Version embedded in binaries matches the tag.

## Test notes

- Dry-run on a pre-release tag; verify artifacts, image manifests
  (`docker manifest inspect`), and that `gbbconnect version` matches the tag.
