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

## 3. Golden vectors: CRC-16 (0xA001)

| Input (hex, full frame incl. CRC slot or PDU) | CRC `lo hi` |
|----------------------------------------------|-------------|
| `01 03 00 9C 00 03` | `D5 CA` |

CRC is computed over all bytes except the trailing 2 (the CRC slot). Validate by
recomputing and comparing the last two bytes. Implementers should add more
vectors by capturing real frames; the function under test is in
[`ModbusRTUFrame.cs`](../../GbbEngine2/Drivers/000_SolarmanV5/ModbusRTUFrame.cs).

### Read header

`CreateReadHeader(unit=1, start=0x009C, len=3, fn=3)` ->
`01 03 00 9C 00 03 D5 CA`.

### Hex codec

- `[]byte{0x01,0x03,0x00,0x9C,0x00,0x03,0xD5,0xCA}` <-> `"0103009C0003D5CA"`.
- Encode is uppercase, no separators; decode requires even length.

## 4. Golden vectors: SolarmanV5

Inputs: dongle serial `0x12345678` (decimal 305419896), sequence `0x2A`, inner
RTU `01 03 00 9C 00 03 D5 CA`.

Expected request frame (checksum byte computed per spec):
```
A5 17 00 10 45 2A 00 78 56 34 12 02 00 00 00 00
00 00 00 00 00 00 00 00 00 01 03 00 9C 00 03 D5
CA <CK> 15
```
- `17 00` = length 0x0017 = len(rtu=8)+15 = 23.
- `2A 00` = seq, padding.
- `78 56 34 12` = serial little-endian.
- `<CK>` = `sum(frame[1..len-2]) & 0xFF`.

Implementers must compute `<CK>` in code and assert the full frame equals the
builder output; also assert the parser, given a synthetic response frame (control
`10 15`, matching serial/seq, frame type `02`), extracts the inner RTU from
offset 25..len-2.

Parser negative tests: wrong start byte, wrong end byte, wrong control code,
sequence mismatch, serial mismatch, frame-type mismatch, and `FrameLen !=
PayloadLength + 13` each raise the corresponding error.

## 5. Golden vectors: Modbus TCP

Inner RTU `01 03 00 9C 00 03 D5 CA`, transaction id `0x0001`.

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

- A TCP listener that, for SolarmanV5, validates the request frame and replies
  with a well-formed V5 response wrapping a canned Modbus response; for Modbus
  TCP, echoes the transaction id and returns a canned PDU; for RTU-over-TCP,
  returns a canned RTU frame (optionally fragmented across writes to exercise the
  reassembly logic); for serial, a pty.

### Mock cloud (MQTT)

- An embedded MQTT broker (or a real broker in CI) where the test:
  1. publishes a request to `{PlantId}/ModbusInMqtt/toDevice`,
  2. asserts a response appears on `{PlantId}/ModbusInMqtt/fromDevice` with QoS 2
     and the expected JSON,
  3. asserts keepalive messages appear on `{PlantId}/keepalive` ~every 60 s
     (use a compressed clock / injected ticker in tests).

## 8. Acceptance matrix (must pass for "compatible")

- [ ] CRC and RTU header vectors (§3) pass.
- [ ] SolarmanV5 build + parse vectors (§4) pass, including all negative cases.
- [ ] Modbus TCP build + parse + exception vectors (§5) pass.
- [ ] JSON encode/decode + null omission + error cascading (§6) pass.
- [ ] MQTT: correct client id, topics, QoS (sub 1 / keepalive 1 / response 2),
      60 s keepalive, 5 min backoff.
- [ ] Sub-inverter routing resolves dongle serial/address/port and errors with
      the exact not-found message when missing.
- [ ] LogLevel remote control updates verbosity and persists.
- [ ] New transports (RTU-over-TCP, serial) round-trip a read and a write
      against their mocks, with CRC validation.
- [ ] Discovery returns serials from a mock dongle responder on UDP 48899.
- [ ] Graceful shutdown persists state and disconnects MQTT.

## 9. Manual / live validation (optional, pre-release)

Against a real plant in a test GbbOptimizer account: connect, confirm the cloud
shows the device online (keepalive), execute a read batch, and confirm values
match the official client. Do this read-only first; only test writes with care
(3 s write spacing protects inverter flash).
