# GC-031 - SolarmanV5 transport

- **Epic:** D - Transports
- **Type:** Feature
- **Priority:** High
- **Status:** TODO
- **Estimate:** 2 days
- **Depends on:** GC-030
- **Blocks:** GC-050, GC-082

## Context

- Full spec + golden vectors:
  [docs/04-protocol-solarmanv5.md](../04-protocol-solarmanv5.md),
  [docs/10-compatibility-and-testing.md](../10-compatibility-and-testing.md) §4.
- Original: [`SolarmanV5Frame.cs`](../../../GbbEngine2/Drivers/000_SolarmanV5/SolarmanV5Frame.cs),
  [`SolarmanV5Driver.cs`](../../../GbbEngine2/Drivers/000_SolarmanV5/SolarmanV5Driver.cs).

## Description

Implement the SolarmanV5 transport (WiFi dongles, `driver: solarman_v5`):
frame build/parse, sequence handling, the send/receive retry loop, and TCP
connection management.

## Tasks

- `internal/driver/solarmanv5`:
  - Frame builder `CreateFrame(seq, serial, rtu) []byte` exactly per
    [04](../04-protocol-solarmanv5.md) §2 (start 0xA5, length LE, control
    0x10/0x45, seq at offset 5, padding 0, serial LE 4 bytes, 15-byte payload
    header, RTU, checksum, end 0x15).
  - Parser `ParseFrame(seq, serial, frame) (rtu, err)` per §3: length check
    `FrameLen == PayloadLength + 13`, start/end/control/seq/serial/frametype
    checks, **no** V5 checksum validation, extract `frame[25:len-2]`.
  - Sequence generator: random init 1..255, `(seq+1)&0xFF`.
  - `Transport.SendRTU`: implement `InternalSend` semantics: up to 11 reconnect
    attempts, each with up to 10 sequence-retries; blocking recv with 5 s
    timeout; `"Connection Lost (received 0 bytes)"` on zero read; close + 500 ms
    + reconnect on error; rethrow on attempt 11.
  - `Connect`: TCP, NoDelay, 5 s timeouts, DNS-resolve first address; port
    default 8899.
- Wire driver-trace logging behind the GC-002 flags (decoded vs raw hex).

## Acceptance criteria

- Builder output matches the golden request frame in
  [10](../10-compatibility-and-testing.md) §4 (with computed checksum).
- Parser extracts the inner RTU from a valid synthetic response and rejects each
  malformed case with the corresponding error.
- Sequence increments and matches on `frame[5]`.
- Against the mock dongle (GC-082), a read and a write round-trip succeed; a
  wrong-sequence response triggers a resend; a dropped connection triggers
  reconnect.

## Test notes

- Unit: builder/parser golden + negative vectors.
- Integration: loopback TCP server emulating a dongle (canned responses,
  injected wrong-sequence, injected disconnect).
