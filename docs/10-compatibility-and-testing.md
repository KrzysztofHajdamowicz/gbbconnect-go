# 10 - Compatibility & Testing

This document defines how we prove `gbbconnect-go` is wire-compatible with the
original, and the golden test vectors implementers must satisfy.

## 1. Testing strategy

| Layer | Approach |
|-------|----------|
| CRC-16 / Modbus framing | Pure unit tests against golden vectors (§3) |
| Hex codec | Round-trip unit tests |
| SolarmanV5 frame build/parse | Golden byte vectors (§4) |
| Modbus TCP build/parse | Golden byte vectors (§5) |
| JSON Header/Line | Encode/decode round-trip + null-omission tests (§6) |
| Cloud handler logic | Unit tests with a mock `Transport` (error cascading, sub-inverter, loglevel) |
| Transports (real I/O) | Loopback TCP server / serial pty + recorded fixtures |
| End-to-end | Mock MQTT broker + mock inverter (§7) |

Shared wire fixtures live in [`../internal/testutil/testdata`](../internal/testutil/testdata)
and are embedded by `internal/testutil`, so tests can use the same vectors
without depending on their working directory. Byte comparisons use a shared
assertion that reports the first mismatching offset and a complete hexadecimal
diff.

`make coverage` runs the race-enabled suite and creates `coverage.out`,
`coverage.txt`, and `coverage.html`. CI uploads all three files. `make
coverage-protocol` separately enforces at least 85% statement coverage for
`internal/modbus`, `internal/driver/solarmanv5`,
`internal/driver/modbustcp`, and `internal/protocol`.

## 2. Authoritative source of truth

Where this doc/the design notes disagree with the actual C#, the **C# source
wins**. Known discrepancies to watch:

- Version string: code `APP_VERSION = "1.3.0"`
  ([`Parameters.cs`](../../GbbEngine2/Configuration/Parameters.cs)); the
  reverse-engineered note says `1.2.3`. Use the code.
- SolarmanV5 response length check is `FrameLen == PayloadLength + 13`
  ([`SolarmanV5Frame.GetModBusFrame`](../../GbbEngine2/Drivers/000_SolarmanV5/SolarmanV5Frame.cs)).
- V5 response checksum is **not** validated (commented out).
- Modbus TCP transaction id byte order follows `BitConverter` (little-endian on
  the wire in the original); correlation only requires echo-match. See
  [05-protocol-modbus.md](05-protocol-modbus.md) §4.

### Production capture validation

An offline analysis of a production GbbConnect2 capture from 2026-07-24
validated 1,207 complete request/response exchanges without adding the raw log
or its device identifiers to this repository:

- all 1,207 outgoing and incoming Modbus RTU frames passed CRC validation;
- 1,189 exchanges used function `0x03`, and 18 used function `0x10`;
- every request wrapper contained its RTU frame at offset 26;
- every response wrapper contained its RTU frame at offset 25;
- all sequence numbers and serial fields matched within their exchanges;
- no retry, wrong-sequence, Modbus exception, or CRC-error event occurred.

## 3. Golden vectors: CRC-16 (0xA001)

| Input (hex, full frame incl. CRC slot or PDU) | CRC `lo hi` |
|----------------------------------------------|-------------|
| `01 03 00 9C 00 03` | `C5 E5` |

CRC is computed over all bytes except the trailing 2 (the CRC slot). Validate by
recomputing and comparing the last two bytes. Implementers should add more
vectors by capturing real frames; the function under test is in
[`ModbusRTUFrame.cs`](../../GbbEngine2/Drivers/000_SolarmanV5/ModbusRTUFrame.cs).

### Read header

`CreateReadHeader(unit=1, start=0x009C, len=3, fn=3)` ->
`01 03 00 9C 00 03 C5 E5`.

### Hex codec

- `[]byte{0x01,0x03,0x00,0x9C,0x00,0x03,0xC5,0xE5}` <-> `"0103009C0003C5E5"`.
- Encode is uppercase, no separators; decode requires even length.

## 4. Golden vectors: SolarmanV5

Inputs: dongle serial `0x12345678` (decimal 305419896), sequence `0x2A`, inner
RTU `01 03 00 9C 00 03 C5 E5`.

Expected request frame (checksum byte computed per spec):
```
A5 17 00 10 45 2A 00 78 56 34 12 02 00 00 00 00 00 00
00 00 00 00 00 00 00 00 01 03 00 9C 00 03 C5 E5 F9 15
```
- `17 00` = length 0x0017 = len(rtu=8)+15 = 23.
- `2A 00` = seq, padding.
- `78 56 34 12` = serial little-endian.
- Request Modbus begins at offset 26; response Modbus begins at offset 25.
- `F9` = `sum(frame[1..len-2]) & 0xFF`.

Implementers must compute the checksum in code and assert the full frame equals the
builder output; also assert the parser, given a synthetic response frame (control
`10 15`, matching serial/seq, frame type `02`), extracts the inner RTU from
offset 25..len-2.

Parser negative tests: wrong start byte, wrong end byte, wrong control code,
sequence mismatch, serial mismatch, frame-type mismatch, and `FrameLen !=
PayloadLength + 13` each raise the corresponding error.

## 5. Golden vectors: Modbus TCP

Inner RTU `01 03 00 9C 00 03 C5 E5`, transaction id `0x0001`.

Expected MBAP send buffer (length = `len(rtu) + 6 - 2 = 12`):
```
<tid0> <tid1> 00 00 00 06 01 03 00 9C 00 03
```
- Length field `00 06` = `len(rtu) - 2 = 6` big-endian.
- PDU is the RTU without its 2 CRC bytes.
- `<tid0><tid1>` = transaction id bytes as produced (match original byte order).

Receive: given a synthetic response
`<tid0> <tid1> 00 00 00 05 01 03 02 00 FF`, the driver returns
`01 03 02 00 FF <crcLo> <crcHi>` (CRC recomputed).

Exception path: `resp[8] > 127` with `resp[9]=0x02` -> error
`"Error response: 2=Illegal Data Address"`.

## 6. Golden vectors: JSON protocol

- Decoding the request in
  [03-protocol-json-app.md](03-protocol-json-app.md) §2 yields the two lines.
- Encoding a response with `Error=null`, `Tag=null` omits those keys (null
  omission).
- Field names are PascalCase exactly.
- LogLevel `"min"` (any case) maps to verbose=on, driver traces off.
- Error cascading: a 3-line batch where line 2 fails yields line2.Error set,
  line2.Modbus=null, line3.Modbus=null, line1.Modbus = its response.

## 7. Mocks for integration tests

### Mock inverter (per transport)

The reusable registry and harness in `internal/invertertest` exercise every real
transport through its production `SendRTU` path. Network mocks bind ephemeral
loopback ports and shut down through `testing.T.Cleanup`; the serial mock
implements the transport's in-memory port contract. All mocks validate incoming
RTU requests and derive deterministic read/write responses.

Scenarios cover fragmented and coalesced frames, a zero-byte connection close,
a truncated response followed by reconnect, malformed frames/CRC, wrong
Solarman sequence, wrong Modbus TCP transaction id, and Modbus exception
responses. Integration tests can enumerate independent copies of the scenario
registry with `invertertest.Registry()`.

### Mock cloud (MQTT)

The reusable harness in `internal/cloudtest` embeds Mochi MQTT, binds an
ephemeral loopback port, and uses a generated self-signed certificate by
default. It publishes canonical QoS 1 requests and captures response/keepalive
publishes with their original topic, payload, retained flag, and QoS. Assertion
helpers compare response JSON semantically and count empty QoS 1 keepalives.
Plaintext mode is available for non-production test clients.

- An embedded MQTT broker (or a real broker in CI) where the test:
  1. publishes a request to `{PlantId}/ModbusInMqtt/toDevice`,
  2. asserts a response appears on `{PlantId}/ModbusInMqtt/fromDevice` with QoS 2
     and the expected JSON,
  3. asserts keepalive messages appear on `{PlantId}/keepalive` ~every 60 s
     (use a compressed clock / injected ticker in tests).

## 8. Acceptance matrix (must pass for "compatible")

- [x] CRC and RTU header vectors (§3) pass.
- [x] SolarmanV5 build + parse vectors (§4) pass, including all negative cases.
- [x] Modbus TCP build + parse + exception vectors (§5) pass.
- [x] JSON encode/decode + null omission + error cascading (§6) pass.
- [x] MQTT: correct client id, topics, QoS (sub 1 / keepalive 1 / response 2),
      60 s keepalive, 5 min backoff.
- [x] Sub-inverter routing resolves dongle serial/address/port and errors with
      the exact not-found message when missing.
- [x] LogLevel remote control updates verbosity and persists.
- [x] Incremental LastLog streaming persists its cursor and handles day rollover.
- [x] New transports (RTU-over-TCP, serial) round-trip a read and a write
      against their mocks, with CRC validation.
- [x] Discovery returns serials from a mock dongle responder over UDP.
- [x] Subnet discovery bounds concurrency and reports reachable dongle ports.
- [x] Graceful shutdown persists state and disconnects MQTT.

## 9. Manual / live validation (optional, pre-release)

Against a real plant in a test GbbOptimizer account: connect, confirm the cloud
shows the device online (keepalive), execute a read batch, and confirm values
match the official client. Do this read-only first; only test writes with care
(3 s write spacing protects inverter flash).
