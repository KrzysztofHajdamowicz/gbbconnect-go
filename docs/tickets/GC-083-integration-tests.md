# GC-083 - End-to-end integration & compat tests

- **Epic:** I - Testing & QA
- **Type:** Test
- **Priority:** Medium
- **Status:** DONE
- **Estimate:** 1.5 days
- **Depends on:** GC-081, GC-082, GC-061
- **Blocks:** -

## Context

- Acceptance matrix:
  [docs/10-compatibility-and-testing.md](../10-compatibility-and-testing.md) §8.

## Description

Wire the mock cloud (GC-081) and mock inverters (GC-082) around the supervisor
(GC-061) to validate the full flow and the compatibility acceptance matrix.

## Tasks

- End-to-end test: config with one SolarmanV5 plant -> mock cloud publishes a read
  batch -> assert correct `fromDevice` response (QoS 2) with expected hex.
- Error cascading e2e: a batch with a failing middle line yields the documented
  response shape.
- Sub-inverter routing e2e: a `SubInverterSN` request hits the right mock target;
  unknown SN returns the exact error.
- Keepalive e2e: assert ~60 s keepalives (injected clock).
- Multi-plant isolation: a fault in plant 1 doesn't stall plant 2.
- Shutdown e2e: graceful shutdown persists state and disconnects.
- Drive the acceptance matrix items in [10](../10-compatibility-and-testing.md)
  §8 as explicit test cases.

## Acceptance criteria

- All acceptance-matrix items in [10](../10-compatibility-and-testing.md) §8 are
  covered by passing tests (or explicitly marked manual, e.g. live validation §9).
- Tests are hermetic and run in CI under `-race`.

## Test notes

- Use injected clocks to keep timing tests fast and deterministic.
- Mark any real-broker/live tests with a build tag so default CI stays hermetic.
