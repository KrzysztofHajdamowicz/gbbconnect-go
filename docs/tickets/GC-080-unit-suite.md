# GC-080 - Unit test suite & golden vectors

- **Epic:** I - Testing & QA
- **Type:** Test
- **Priority:** High
- **Status:** TODO
- **Estimate:** 1 day (ongoing)
- **Depends on:** GC-020
- **Blocks:** -

## Context

- Strategy + golden vectors:
  [docs/10-compatibility-and-testing.md](../10-compatibility-and-testing.md).

## Description

Establish the unit-test foundation and the shared golden-vector fixtures that
protocol tickets reference, so compatibility is locked down early.

## Tasks

- A `testdata/` location for shared golden vectors (CRC, RTU headers, SolarmanV5
  frames, Modbus TCP frames, JSON samples) from
  [10](../10-compatibility-and-testing.md) §3-§6.
- Helper assertions for byte-slice equality with readable hex diffs.
- Ensure every protocol package (`modbus`, `solarmanv5`, `modbustcp`,
  `protocol`) has table-driven tests covering happy + negative cases.
- Add `go test -race` to CI (GC-003) and a coverage report.

## Acceptance criteria

- All golden vectors in [10](../10-compatibility-and-testing.md) are encoded as
  tests and pass.
- Negative cases (bad CRC, wrong control code, sequence/serial mismatch, length
  mismatch, Modbus exceptions) are covered.
- Coverage for the protocol packages is high (target >=85%).

## Test notes

- This ticket is largely "meta": it sets conventions and the shared fixtures;
  individual transport tickets add their own cases.
