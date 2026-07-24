# GC-033 - Modbus RTU-over-TCP transport (new)

- **Epic:** D - Transports
- **Type:** Feature
- **Priority:** Medium
- **Status:** DONE
- **Estimate:** 1.5 days
- **Depends on:** GC-030
- **Blocks:** GC-082

## Context

- Spec: [docs/05-protocol-modbus.md](../05-protocol-modbus.md) §5.
- No original equivalent (new functionality). Reuse RTU/CRC from GC-020.

## Description

Implement the raw "Modbus RTU over TCP" transport (`driver: modbus_rtu_tcp`) for
transparent RS485-to-Ethernet gateways (e.g. Waveshare). Raw RTU frames (with
CRC) are sent verbatim over TCP; responses are raw RTU frames.

## Tasks

- `internal/driver/modbusrtutcp`:
  - `Transport.SendRTU(rtu)`: write `rtu` as-is; read response with a
    length-aware reassembly loop (TCP may fragment/coalesce):
    - read header bytes (unit, function);
    - determine expected total length from function code: read response =
      `3 + byteCount + 2`; write response = `8`; exception = `5`;
    - keep reading until the full frame (incl. 2 CRC bytes) is buffered, or the
      read timeout fires;
    - validate CRC; return the full RTU frame.
  - Connection params mirror Modbus TCP: 1 s timeouts, NoDelay, 11 attempts,
    500 ms reconnect.
  - Correlation relies on strict serialization (the executor guarantees it); no
    transaction id exists.

## Acceptance criteria

- A read and a write round-trip against the mock gateway (GC-082) succeed.
- A response delivered in two TCP writes (fragmented) is reassembled correctly.
- Two responses coalesced into one read are not mixed up (only the first frame is
  consumed; remainder buffered or discarded per design — document the choice).
- CRC mismatch yields an error.

## Test notes

- Loopback TCP server that can: send a full frame, send it in fragments, and
  coalesce. Assert correct reassembly and CRC validation.
- Include a 0x10 write-response framing case (fixed 8-byte length).
