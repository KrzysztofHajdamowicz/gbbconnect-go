# GC-044 - Remote LogLevel control

- **Epic:** E - Cloud gateway
- **Type:** Feature
- **Priority:** Medium
- **Status:** TODO
- **Estimate:** 0.5 day
- **Depends on:** GC-043, GC-002
- **Blocks:** -

## Context

- LogLevel mapping: [docs/03-protocol-json-app.md](../03-protocol-json-app.md) §5.
- Original: the "Change log level" block in
  [`JobManager-mqtt.cs`](../../../GbbEngine2/Server/JobManager-mqtt.cs).

## Description

Honor the cloud's `LogLevel` field in incoming requests: update runtime verbosity
and persist it.

## Tasks

- When `Header.LogLevel != nil`, match case-insensitively:
  - `OnlyErrors` -> verbose off, driver traces off.
  - `Min` -> verbose on, driver traces off.
  - `Max` -> verbose on, both driver traces on.
  - unknown -> log a warning, ignore.
- Apply to the GC-002 runtime logger immediately.
- Persist the change (update config file / a persisted runtime override) so it
  survives restart, mirroring the original `Parameters.Save()`.

## Acceptance criteria

- Each known value adjusts the live logger as specified.
- Unknown value logs a warning and changes nothing.
- The setting persists across a restart.

## Test notes

- Unit test the mapping (incl. case-insensitivity) and persistence.
