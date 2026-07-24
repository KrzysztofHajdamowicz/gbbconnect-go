# GC-082 - Mock inverter harness

- **Epic:** I - Testing & QA
- **Type:** Test
- **Priority:** Medium
- **Status:** TODO
- **Estimate:** 1 day
- **Depends on:** GC-030, GC-031, GC-032
- **Blocks:** GC-083

## Context

- Mock inverter expectations:
  [docs/10-compatibility-and-testing.md](../10-compatibility-and-testing.md) §7.
- Per-transport framing: [docs/04](../04-protocol-solarmanv5.md),
  [docs/05](../05-protocol-modbus.md).

## Description

Provide loopback servers (and a serial fake) that emulate each transport's device
side, with controllable faults, for transport and integration tests.

## Tasks

- SolarmanV5 mock: TCP listener that validates the V5 request frame, echoes
  serial/sequence, and returns a canned Modbus response wrapped in a V5 response
  frame. Fault injection: wrong sequence, short read / zero-byte close, malformed
  frame.
- Modbus TCP mock: TCP listener echoing the transaction id and returning a canned
  PDU; fault injection: exception response, wrong tid, short response.
- RTU-over-TCP mock: returns canned RTU frames, optionally fragmented or
  coalesced across writes.
- Serial fake: a pty or in-memory `io.ReadWriteCloser` returning canned RTU.
- A small registry so integration tests (GC-083) can pick a transport + scenario.

## Acceptance criteria

- Each real transport (GC-031..034) round-trips a read and a write against its
  mock.
- Fault scenarios trigger the expected retry/reconnect/error behaviour.

## Test notes

- Keep listeners on ephemeral ports; ensure clean shutdown to avoid leaks.
