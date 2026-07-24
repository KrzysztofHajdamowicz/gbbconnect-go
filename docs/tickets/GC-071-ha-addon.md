# GC-071 - Home Assistant Add-on

- **Epic:** H - Packaging
- **Type:** Feature
- **Priority:** High
- **Status:** DONE
- **Estimate:** 1.5 days
- **Depends on:** GC-070, GC-014
- **Blocks:** -

## Context

- HA structure + options mapping: [docs/09-deployment.md](../09-deployment.md) §3,
  [docs/07-configuration.md](../07-configuration.md) §6.
- Existing community add-ons for reference (see original
  [`../README.md`](../../../README.md) "GbbConnect on HA").

## Description

Package `gbbconnect-go` as a Home Assistant Add-on with a friendly options schema,
so users configure plants via the HA UI.

## Tasks

- `deploy/addon/` with:
  - `config.yaml` manifest: name, slug, version, `arch: [aarch64, amd64, armv7]`,
    `init: false`, `options:` defaults, and a `schema:` mirroring the config model
    (plant list with typed fields; see [07](../07-configuration.md)).
  - `Dockerfile` based on HA base images (`BUILD_FROM` arg) that copies the
    binary (from the GC-070 image or a build stage).
  - `run.sh` entrypoint: read `/data/options.json` (HA-written) and either pass it
    directly to the binary or render YAML; set state dir to `/data`.
  - `DOCS.md`, `README.md`, icon/logo.
- Expose serial devices via `devices:`/`uart: true` when `modbus_serial` is used;
  document discovery needing `host_network: true` if run at runtime.
- Keep the options `schema:` in sync with the JSON Schema (GC-014).

## Acceptance criteria

- The add-on builds for all three arches.
- Setting options in the HA UI produces a working config the binary loads from
  `/data/options.json`.
- State persists in `/data` across add-on restarts.

## Test notes

- Validate `config.yaml` schema parses; lint with HA add-on tooling if available.
- A scripted run that feeds a sample `/data/options.json` and asserts startup.
