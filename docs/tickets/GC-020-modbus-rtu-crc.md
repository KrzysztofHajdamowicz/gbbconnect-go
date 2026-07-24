# GC-020 - Modbus RTU framing, CRC-16, hex codec

- **Epic:** C - Modbus core
- **Type:** Feature
- **Priority:** High
- **Status:** DONE
- **Estimate:** 1 day
- **Depends on:** GC-001
- **Blocks:** GC-021, GC-030, GC-031, GC-032, GC-033, GC-034, GC-080

## Context

- Spec + golden vectors: [docs/05-protocol-modbus.md](../05-protocol-modbus.md)
  §1-§3 and [docs/10-compatibility-and-testing.md](../10-compatibility-and-testing.md)
  §3.
- Original: [`ModbusRTUFrame.cs`](../../../GbbEngine2/Drivers/000_SolarmanV5/ModbusRTUFrame.cs)
  (`GetCRC`, `CreateReadHeader`, `CreateWriteHeader`),
  [`GbbLibSmall/Convert.cs`](../../../GbbLibSmall/Convert.cs).

## Description

Implement the foundational Modbus RTU helpers in `internal/modbus`: CRC-16, read
and write header builders, and the hex string codec. These must be byte-identical
to the original.

## Tasks

- `CRC16(data []byte) (lo, hi byte)` computing over `data[:len-2]` with polynomial
  0xA001 (see [05](../05-protocol-modbus.md) §2 for reference code).
- `AppendCRC(frame []byte)` / `ValidateCRC(frame []byte) bool` helpers.
- `BuildReadHoldingRegisters(unit byte, start, count uint16) []byte` (function
  0x03, big-endian address/count, count<=125, CRC appended).
- `BuildWriteMultipleRegisters(unit byte, start uint16, values []byte) []byte`
  (function 0x10, values<=250, even-padded byte count, CRC appended).
- Hex codec: `EncodeHex([]byte) string` (uppercase, no separators),
  `DecodeHex(string) ([]byte, error)` (even length required).

## Acceptance criteria

- `CRC16` of `01 03 00 9C 00 03` (with 2-byte CRC slot) yields `C5 E5`.
- `BuildReadHoldingRegisters(1, 0x009C, 3)` == `0103009C0003C5E5`.
- Hex round-trip: bytes <-> uppercase string; odd-length decode errors.
- Write header matches `CreateWriteHeader` layout for a sample.

## Test notes

- Implement all golden vectors from
  [10](../10-compatibility-and-testing.md) §3.
- Fuzz the hex codec round-trip.
- Property test: `ValidateCRC(AppendCRC(frame))` is always true.
