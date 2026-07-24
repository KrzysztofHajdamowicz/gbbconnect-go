# GC-021 - Read/Write register codecs & response interpretation

- **Epic:** C - Modbus core
- **Type:** Feature
- **Priority:** High
- **Status:** DONE
- **Estimate:** 0.5 day
- **Depends on:** GC-020
- **Blocks:** GC-030

## Context

- Function-code interpretation & exceptions:
  [docs/05-protocol-modbus.md](../05-protocol-modbus.md) §1, and SolarmanV5
  `WriteSyncData` interpretation in
  [docs/04-protocol-solarmanv5.md](../04-protocol-solarmanv5.md) §6.
- Original: `WriteSyncData` in
  [`SolarmanV5Driver.cs`](../../../GbbEngine2/Drivers/000_SolarmanV5/SolarmanV5Driver.cs)
  and [`ModbusTcpDriver.cs`](../../../GbbEngine2/Drivers/001_ModusTCP/ModbusTcpDriver.cs).

## Description

Implement response parsing/interpretation used by the local read/write helpers
(not the raw cloud path). Given a full RTU response frame, classify it and extract
data.

## Tasks

- `ParseResponse(rtu []byte) (kind, data, err)` where:
  - CRC is validated first (`"Wrong CRC!"` on mismatch).
  - `function > 128` -> Modbus exception ->
    error `"Error response: function: {function-128}, error={data[2]}"`.
  - `function >= 5 && function != 23` -> write response; return whole frame.
  - else -> read response; return `rtu[3 : 3+rtu[2]]` (the data region).
- Helper to decode read data into `[]uint16` registers (big-endian) for
  diagnostics/logging.

## Acceptance criteria

- A read response `01 03 06 00 12 00 34 00 56 <crc>` parses to registers
  `[0x0012,0x0034,0x0056]`.
- An exception frame yields the exact error string.
- A bad CRC yields `"Wrong CRC!"`.
- Write response returns the full frame unchanged.

## Test notes

- Vectors for read, write, exception, and bad-CRC cases.
- Confirm classification thresholds match the C# (`>=5 && !=23`).
