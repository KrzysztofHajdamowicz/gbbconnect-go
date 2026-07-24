# GC-002 - Structured logging & log buffer

- **Epic:** A - Scaffolding & tooling
- **Type:** Feature
- **Priority:** High
- **Status:** DONE
- **Estimate:** 1 day
- **Depends on:** GC-001
- **Blocks:** GC-030, GC-043, GC-045

## Context

- Logging model: [docs/01-architecture.md](../01-architecture.md) §7.
- LogLevel mapping: [docs/03-protocol-json-app.md](../03-protocol-json-app.md) §5.
- Original logger: [`GbbConnect2Console/Program.cs`](../../../GbbConnect2Console/Program.cs)
  (`Log` class) and the verbosity flags in
  [`Parameters.cs`](../../../GbbEngine2/Configuration/Parameters.cs).
- Log streaming consumer: [docs/03-protocol-json-app.md](../03-protocol-json-app.md) §8.

## Description

Provide a structured logger used across the app, plus an optional daily-file sink
that the cloud log-streaming feature (GC-045) reads from.

Map the original's three booleans to one runtime model:
- `IsVerboseLog` -> info level enabled (vs errors-only).
- `IsDriverLog` -> driver decoded-Modbus trace flag.
- `IsDriverLog2` -> driver raw-frame-hex trace flag.

## Tasks

- Choose `log/slog` (stdlib) as the base logger; expose a small `Logger`
  interface so packages don't import slog directly.
- Levels: error/warn/info/debug. A runtime-settable level + two trace flags
  (`driverTrace`, `driverTraceRaw`).
- stdout handler (text or JSON, configurable) for Docker/systemd/HA.
- Daily file sink: append to `<logdir>/yyyy-MM-dd.txt` with the same timestamped
  line format the original uses (so streamed logs look familiar). Writes must be
  safe under concurrency.
- Provide a redaction helper so secrets (plant_token) are never emitted.

## Acceptance criteria

- Setting level to errors-only suppresses info logs; `Min`/`Max` mapping behaves
  per [03](../03-protocol-json-app.md) §5.
- Driver trace flags gate the corresponding driver logs (verified later by
  GC-031/GC-032).
- Daily file contains timestamped lines; rolling at midnight by filename.
- No test or code path logs a token value.

## Test notes

- Unit test the level/flag gating and the file line format.
- Concurrency test: many goroutines logging to the file sink without interleaved
  partial lines.
