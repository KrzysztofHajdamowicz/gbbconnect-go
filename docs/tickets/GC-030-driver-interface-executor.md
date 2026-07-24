# GC-030 - Driver interface & transaction executor

- **Epic:** D - Transports
- **Type:** Feature
- **Priority:** High
- **Status:** TODO
- **Estimate:** 1.5 days
- **Depends on:** GC-020, GC-021, GC-002, GC-010
- **Blocks:** GC-031, GC-032, GC-033, GC-034, GC-043

## Context

- Interface design + executor: [docs/06-driver-interface.md](../06-driver-interface.md).
- Timing rules: [docs/05-protocol-modbus.md](../05-protocol-modbus.md) §7,
  [docs/04-protocol-solarmanv5.md](../04-protocol-solarmanv5.md) §6.
- Original `IDriver`: [`IDriver.cs`](../../../GbbEngine2/Drivers/IDriver.cs).

## Description

Define the `Transport` and `Driver` interfaces and the per-plant transaction
executor that serializes transactions and enforces inter-command timing for the
local helper path.

## Tasks

- `internal/driver`: define `Transport` and `Driver` interfaces per
  [06](../06-driver-interface.md) §1.
- Implement the executor: a serialized wrapper (mutex or single-goroutine +
  channel) that all transactions pass through; tracks `lastSend` for the
  100ms-read / 3000ms-write delay in the local helper API.
- Implement the `Driver` facade over a `Transport`:
  - `SendDataToDevice`: raw path, no delay, returns full RTU incl. CRC (cloud
    path semantics).
  - `ReadHoldingRegisters` / `WriteMultipleRegisters`: apply delay, build via
    GC-020, send via transport, interpret via GC-021.
- Implement the driver factory `New(plant, log) (Driver, error)` mapping
  `DriverType` -> transport, with the unknown-driver error.

## Acceptance criteria

- Two concurrent calls into one executor are serialized (never overlap).
- The local read path waits >=100 ms since the previous send; write path
  >=3000 ms; the raw `SendDataToDevice` path adds no delay.
- Factory returns the right transport per driver type and errors on unknown.

## Test notes

- Use a fake `Transport` recording call timestamps to assert serialization and
  delay enforcement (use an injectable clock to keep tests fast).
- Assert `SendDataToDevice` does not delay.
