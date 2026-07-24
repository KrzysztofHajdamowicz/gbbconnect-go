# GC-052 - `discover` CLI subcommand

- **Epic:** F - Discovery
- **Type:** Feature
- **Priority:** Medium
- **Status:** TODO
- **Estimate:** 0.5 day
- **Depends on:** GC-050, GC-060
- **Blocks:** -

## Context

- CLI design + output: [docs/08-discovery.md](../08-discovery.md) §3.

## Description

Wire the discovery functions into the `gbbconnect discover` subcommand with
human and JSON output, so users can list dongle serials for their config.

## Tasks

- Implement flags: `--interface`, `--broadcast` (default true), `--subnet`,
  `--port` (default 8899), `--timeout` (default 5s), `--json`.
- Run UDP discovery (GC-050) and, if `--subnet` given, the subnet scan (GC-051);
  merge/dedupe results by serial/IP.
- Human output: table of IP / MAC / Serial + a count line.
- `--json` output: the documented JSON object.
- Non-zero exit if nothing found (optional; document the choice).

## Acceptance criteria

- `gbbconnect discover` prints discovered dongles in the documented table.
- `gbbconnect discover --json` emits valid JSON matching
  [08](../08-discovery.md) §3.
- `--subnet` triggers the scan and merges results.

## Test notes

- Exercise the command end-to-end against the mock UDP responder; capture stdout
  and assert format (table + JSON).
