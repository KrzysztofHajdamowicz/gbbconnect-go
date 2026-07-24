# GC-013 - Per-plant state persistence

- **Epic:** B - Configuration & domain
- **Type:** Feature
- **Priority:** High
- **Status:** DONE
- **Estimate:** 0.5 day
- **Depends on:** GC-010
- **Blocks:** GC-045, GC-061

## Context

- State design: [docs/07-configuration.md](../07-configuration.md) §7.
- Original state: `PlantState` (`lastLog_Date`, `lastLog_Pos`),
  `OurLoadState`/`OurSaveState` referenced in
  [`JobManager.cs`](../../../GbbEngine2/Server/JobManager.cs) and
  [`JobManager-mqtt.cs`](../../../GbbEngine2/Server/JobManager-mqtt.cs).
- Atomic save pattern: [`Parameters.Save`](../../../GbbEngine2/Configuration/Parameters.cs)
  (`.tmp` then move).

## Description

Persist per-plant runtime state (currently just the log-streaming position) so
restarts resume cleanly.

## Tasks

- `internal/state` package with `Load(plantNumber)` / `Save(plantNumber, state)`.
- State file `state/plant-<number>.json` with `last_log_date`, `last_log_pos`.
- State dir resolution: `--state-dir` / `GBB_STATE_DIR` / OS data dir / `/data`
  (HA). Create dir if missing.
- Atomic writes (temp + rename). Safe for concurrent saves of different plants;
  guard same-file writes.

## Acceptance criteria

- Save then Load returns identical state.
- Missing state file yields a sensible zero value (not an error).
- Writes are atomic (no truncated files on crash mid-write — temp+rename).

## Test notes

- Round-trip unit test.
- Concurrent save test for distinct plant numbers.
