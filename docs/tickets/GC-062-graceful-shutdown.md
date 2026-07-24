# GC-062 - Signals & graceful shutdown

- **Epic:** G - Runtime
- **Type:** Feature
- **Priority:** High
- **Status:** TODO
- **Estimate:** 0.5 day
- **Depends on:** GC-061
- **Blocks:** GC-070, GC-072, GC-073

## Context

- Shutdown behaviour: [docs/01-architecture.md](../01-architecture.md) §4.
- Original: `OurStopJobs` (saves state + cancels) in
  [`JobManager.cs`](../../../GbbEngine2/Server/JobManager.cs).

## Description

Handle termination signals and shut down cleanly: stop workers, disconnect MQTT,
persist state.

## Tasks

- Trap SIGINT/SIGTERM (Linux/macOS); cancel the root context.
- On shutdown: stop accepting new work, let in-flight transactions finish (with a
  bounded grace timeout), disconnect each MQTT client gracefully, persist all
  plant states (GC-013), then exit 0.
- Provide a hook the Windows service handler (GC-073) can call for the same
  shutdown path.
- A second signal forces immediate exit.

## Acceptance criteria

- SIGTERM triggers graceful shutdown within the grace period; state files are
  written; MQTT disconnects cleanly.
- A second SIGTERM forces immediate exit.
- Exit code 0 on clean shutdown.

## Test notes

- Integration test: start `run` with mock cloud, send shutdown via context/signal,
  assert state persisted and clients disconnected.
