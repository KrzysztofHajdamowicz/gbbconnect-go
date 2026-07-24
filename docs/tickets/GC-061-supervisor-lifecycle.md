# GC-061 - Supervisor & plant workers

- **Epic:** G - Runtime
- **Type:** Feature
- **Priority:** High
- **Status:** TODO
- **Estimate:** 1.5 days
- **Depends on:** GC-040, GC-041, GC-043, GC-013, GC-060
- **Blocks:** GC-062

## Context

- Concurrency + lifecycle: [docs/01-architecture.md](../01-architecture.md)
  §3, §4, §5.
- Original orchestration: [`JobManager.cs`](../../../GbbEngine2/Server/JobManager.cs),
  [`JobManager-mqtt.cs`](../../../GbbEngine2/Server/JobManager-mqtt.cs).

## Description

Implement the supervisor that starts one worker per enabled plant, wires the MQTT
client, keepalive loop, and message handler together, and manages the
per-plant lifecycle/backoff.

## Tasks

- `internal/supervisor`: start a goroutine per enabled plant with credentials.
- Each worker:
  - loads plant state (GC-013);
  - owns an MQTT client (GC-040) + keepalive loop (GC-041);
  - on `toDevice` message -> the handler (GC-043) via the plant's transaction
    executor (GC-030);
  - applies the connect/backoff state machine
    ([01](../01-architecture.md) §4);
  - recovers from panics and transitions to Backoff instead of crashing.
- Disabled or credential-less plants are skipped (with a log line).
- All workers share a root context for cancellation.

## Acceptance criteria

- Multiple plants run concurrently; one plant's slow/failed inverter does not
  block another plant's keepalive or handling.
- A panic in one worker is recovered and the worker re-enters Backoff.
- Disabled plants are not started.

## Test notes

- Two-plant test against the mock cloud + two mock inverters; assert independent
  progress and isolation (inject a fault in plant 1, verify plant 2 unaffected).
