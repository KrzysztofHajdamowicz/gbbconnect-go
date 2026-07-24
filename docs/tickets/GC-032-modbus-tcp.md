# GC-032 - Modbus TCP transport

- **Epic:** D - Transports
- **Type:** Feature
- **Priority:** High
- **Status:** TODO
- **Estimate:** 1.5 days
- **Depends on:** GC-030
- **Blocks:** GC-082

## Context

- Spec + golden vectors: [docs/05-protocol-modbus.md](../05-protocol-modbus.md)
  §4, [docs/10-compatibility-and-testing.md](../10-compatibility-and-testing.md) §5.
- Original: [`ModbusTcpDriver.cs`](../../../GbbEngine2/Drivers/001_ModusTCP/ModbusTcpDriver.cs).

## Description

Implement the Modbus TCP transport (wired/Ethernet dongles, `driver:
modbus_tcp`). It wraps the inner RTU as an MBAP frame, stripping the inner CRC on
send and rebuilding it on receive.

## Tasks

- `internal/driver/modbustcp`:
  - `Transport.SendRTU(rtu)`:
    - build MBAP: transaction id (see endianness note below), protocol id
      `0000`, length = `len(rtu)-2` big-endian, PDU = `rtu[:len-2]`.
    - send; recv (single read, up to 1024); require `>=10` bytes; compare
      transaction id; on `resp[8] > 127` map `resp[9]` via the exception table
      and error; else read `len2 = resp[4]<<8|resp[5]`, copy `resp[6:6+len2]`,
      append recomputed CRC; return.
  - Transaction id generator: random 1..65535, `(seq+1)&0xFFFF`.
  - `InternalSend`: 11 attempts, close+500ms+reconnect on error, rethrow on 11th.
  - `Connect`: explicit (original doesn't connect in ctor); 1 s timeouts,
    NoDelay.
- Endianness note: the original writes the transaction id via `BitConverter`
  (little-endian on the wire) and only echo-compares it. Replicate the original
  byte order to be safe; correlation is the only requirement.

## Acceptance criteria

- Send buffer for `0103009C0003D5CA`, tid `0x0001` matches
  [10](../10-compatibility-and-testing.md) §5 (length `00 06`, PDU without CRC).
- A synthetic response `... 01 03 02 00 FF` returns
  `01 03 02 00 FF <crc>` with correct CRC.
- Exception `resp[9]=0x02` -> `"Error response: 2=Illegal Data Address"`.
- Reconnect on simulated error; rethrow after 11 attempts.

## Test notes

- Unit: build/parse/exception vectors.
- Integration: loopback TCP server emulating a Modbus TCP dongle (GC-082).
