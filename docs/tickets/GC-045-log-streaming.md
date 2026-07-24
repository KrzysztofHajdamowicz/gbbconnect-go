# GC-045 - Incremental log streaming

- **Epic:** E - Cloud gateway
- **Type:** Feature
- **Priority:** Low
- **Status:** TODO
- **Estimate:** 1 day
- **Depends on:** GC-043, GC-013, GC-002
- **Blocks:** -

## Context

- Behaviour: [docs/03-protocol-json-app.md](../03-protocol-json-app.md) §8.
- Original: the "Add Last log" block + `LastLog_Date`/`LastLog_Pos` in
  [`JobManager-mqtt.cs`](../../../GbbEngine2/Server/JobManager-mqtt.cs).

## Description

When the cloud sets `SendLastLog != 0`, attach incremental log text since the last
request to `Header.LastLog`, tracking position in per-plant state (GC-013).

## Tasks

- Read from today's daily log file (GC-002 file sink) starting at the stored byte
  position; append the new text to `Header.LastLog`; advance the position.
- Day-rollover handling per the original:
  - if stored date is unset/older than yesterday or position is nil -> jump to end
    of today's file (send nothing this round, set position to current length);
  - if stored date is yesterday -> send the remainder of yesterday, then reset to
    today position 0;
  - else read from the stored position in the stored day's file.
- Persist updated `last_log_date` / `last_log_pos` after publishing.

## Acceptance criteria

- With new log lines written between two `SendLastLog` requests, the second
  response's `LastLog` contains exactly those new lines.
- Day rollover behaves as specified (no missed/duplicated text across midnight).
- State position persists across restart.

## Notes

- Acceptable interim behaviour (per [03](../03-protocol-json-app.md) §8): return
  `null` LastLog and only maintain positions, deferring full streaming. If so,
  keep the state fields wired and leave a clear TODO.

## Test notes

- Simulate a log file with appended lines and assert the incremental slices.
- Simulate yesterday->today rollover with a fake clock.
