# 05 - Modbus: RTU, CRC, TCP, RTU-over-TCP, Serial

This covers the inner Modbus RTU format (shared by all transports), the CRC, and
the four wired transports: Modbus TCP (original `DriverNo=1`), Modbus RTU over TCP
(new), and Modbus over serial (new).

Authoritative sources:
[`ModbusRTUFrame.cs`](../../GbbEngine2/Drivers/000_SolarmanV5/ModbusRTUFrame.cs)
(`ModBus.CreateReadHeader`, `CreateWriteHeader`, `GetCRC`) and
[`ModbusTcpDriver.cs`](../../GbbEngine2/Drivers/001_ModusTCP/ModbusTcpDriver.cs).

## 1. Modbus RTU frame

A Modbus RTU frame is: `UnitID | Function | Data... | CRC16(2 bytes, LE)`.

### Read Holding Registers (function 0x03)

Request (8 bytes): `unit, 0x03, startHi, startLo, countHi, countLo, crcLo, crcHi`
- Address and count are **big-endian** (the C# uses
  `IPAddress.HostToNetworkOrder`).
- `count <= 125`.

Response: `unit, 0x03, byteCount(N), data[N], crcLo, crcHi` where `N = 2 *
registers`.

### Write Multiple Registers (function 0x10)

Request: `unit, 0x10, startHi, startLo, countHi, countLo, byteCount, data[...],
crcLo, crcHi`
- Built by `CreateWriteHeader(unit, start, count, numBytes, 16)` which allocates
  `numBytes + 9` bytes and fills header fields; the caller copies the register
  values to offset 7 and appends the CRC.
- `data` length `<= 250` bytes; if odd, padded up to even (`numBytes`).

Response: `unit, 0x10, startHi, startLo, countHi, countLo, crcLo, crcHi`.

### Exception response

Function byte has high bit set (`function > 128`). The byte after is the
exception code. The driver raises
`"Error response: function: {function-128}, error={code}"` (SolarmanV5 path) or a
descriptive string (Modbus TCP path, see §4 table).

## 2. CRC-16 (Modbus, polynomial 0xA001)

`ModBus.GetCRC(data)` computes CRC over `data[0 .. len-2]` (i.e. **excluding** the
final 2 CRC bytes) and returns `[crcLo, crcHi]` (little-endian on the wire):

```go
func CRC16(data []byte) (lo, hi byte) {
    crc := uint16(0xFFFF)
    for _, b := range data[:len(data)-2] { // exclude last 2 bytes (CRC slot)
        crc ^= uint16(b)
        for i := 0; i < 8; i++ {
            if crc&0x0001 != 0 {
                crc = (crc >> 1) ^ 0xA001
            } else {
                crc >>= 1
            }
        }
    }
    return byte(crc & 0xFF), byte((crc >> 8) & 0xFF)
}
```

> Important: the original computes CRC over `data[:len-2]`, treating the last two
> bytes of the passed buffer as the CRC slot. When validating a received frame,
> pass the full frame (including its CRC bytes) and compare the computed `lo/hi`
> with the last two bytes. When building, allocate the 2 trailing bytes first,
> then fill them. Golden vectors are in
> [10-compatibility-and-testing.md](10-compatibility-and-testing.md).

Known good vector, verified against the original `GetCRC` algorithm:
`01 03 00 9C 00 03` -> CRC `C5 E5` (full frame `0103009C0003C5E5`).

## 3. Hex string codec

`Modbus` JSON fields are hex strings. Match
[`GbbLibSmall/Convert.cs`](../../GbbLibSmall/Convert.cs):
- Encode: each byte -> 2 uppercase hex chars, concatenated, trimmed.
- Decode: take 2 chars at a time -> one byte. Input length must be even.

## 4. Modbus TCP transport (DriverNo = 1)

The wired/Ethernet-dongle transport. It wraps the **inner** Modbus RTU as a MBAP
frame, **stripping the inner CRC on send** and **rebuilding it on receive**.

### Connection parameters

| Parameter | Value |
|-----------|-------|
| Send/receive timeout | 1000 ms |
| TCP NoDelay | true |
| Receive buffer | 1024 bytes |
| Reconnect attempts | 11 |
| Reconnect delay | 500 ms |
| Transaction id range | 1..65535 (16-bit) |

### MBAP frame

```
Offset Size Field             Value
0x00   2    Transaction ID    sequential (BE on wire, see note)
0x02   2    Protocol ID       0x0000
0x04   2    Length (BE)       len(innerRTU) - 2   (i.e. RTU without its CRC)
0x06   N    PDU               unit + function + data (NO CRC)
```

Send algorithm (`SendDataToDevice`):
1. `tcp = make([]byte, len(rtu) + 6 - 2)`.
2. Transaction id: `GetNextSequenceNumber()` returns
   `BitConverter.GetBytes(seq)` (a UInt16). The original writes those two bytes
   as `tcp[0]=b[0]`, `tcp[1]=b[1]` — i.e. **little-endian** as produced by
   `BitConverter` on a little-endian host. The response check compares the same
   two bytes back, so the on-wire endianness of the transaction id is whatever
   `BitConverter` produced; since we only need request/response correlation,
   `gbbconnect-go` may write the transaction id big-endian (standard MBAP) as
   long as it compares the echoed bytes consistently. **To be safe and match the
   original exactly, replicate its byte order**: write the low byte first.
3. Protocol id = `00 00`.
4. Length = `uint16(len(rtu) - 2)` big-endian (`tcp[4]=len>>8`, `tcp[5]=len&0xFF`).
5. Copy `rtu[0 : len(rtu)-2]` (PDU without CRC) to offset 6.
6. Send.

Receive algorithm:
1. Read response (single `recv`, up to 1024 bytes).
2. Require `len(resp) >= 10` else "Response too short".
3. Compare `resp[0],resp[1]` with the sent transaction id bytes else
   "Wrong TransactionId".
4. If `resp[8] > 127`: exception; map `resp[9]` to a message (table below) and
   raise `"Error response: {code}={msg}"`.
5. `len2 = uint16(resp[4]<<8 | resp[5])` (big-endian length).
6. `ret = make([]byte, len2 + 2)`; copy `resp[6 : 6+len2]` into `ret[0:len2]`.
7. Compute CRC over `ret` and write it into `ret[len2], ret[len2+1]`.
8. Return `ret` (full Modbus RTU incl. reconstructed CRC).

### Sequence (transaction id)

`GetNextSequenceNumber`: first use random `1..65535`; then `(seq+1) & 0xFFFF`.

### Exception code table

| Code | Message |
|------|---------|
| 0x01 | Illegal Function |
| 0x02 | Illegal Data Address |
| 0x03 | Illegal Data Value |
| 0x04 | Slave Device Failure |
| 0x05 | Acknowledge |
| 0x06 | Slave Device Busy |
| 0x08 | Memory Parity Error |
| 0x0A | Gateway Path Unavailable |
| 0x0B | Gateway Target Device Failed to Respond |

### Retry

`InternalSend`: up to 11 attempts; on error close socket, sleep 500 ms,
reconnect, retry; rethrow on the 11th.

> Note: unlike SolarmanV5, the Modbus TCP driver does **not** call `Connect()` in
> its constructor (it is commented out); the caller invokes `Connect()`
> explicitly (the MQTT handler does `sm.Connect()` after construction). Preserve
> "connect is explicit" semantics or simply always connect before first use.

## 5. Modbus RTU over TCP (NEW transport)

For gateways like Waveshare RS485-to-Ethernet in "RTU over TCP / transparent"
mode: the **raw Modbus RTU frame, including its CRC**, is sent over the TCP
socket verbatim, and the response is the raw RTU frame (with CRC).

Design:
- Connection params mirror Modbus TCP (1000 ms timeouts, NoDelay, 1024 buffer,
  11 attempts, 500 ms reconnect).
- `SendDataToDevice(rtu)`:
  1. Send `rtu` as-is (it already contains the CRC).
  2. Read response bytes. Because TCP is a stream, a single `recv` may return a
     partial frame; implement a read loop that accumulates until a complete RTU
     frame is available. Frame completeness is determined by Modbus parsing:
     - read at least 3 bytes (unit, function, then either byteCount for 0x03 or
       fixed length for writes / exceptions);
     - compute expected total length from the function code and byte count;
     - keep reading until that many bytes (+2 CRC) are present, or timeout.
  3. Validate the CRC of the assembled response; on mismatch, error.
  4. Return the full RTU frame (with CRC).
- If one TCP read contains more than one complete response, retain the bytes
  after the first frame and consume them during the next serialized exchange.
  Closing or reconnecting clears this pending buffer.
- No MBAP header, no transaction id. Correlation relies on the
  request/response being strictly serialized (the per-plant executor guarantees
  this).

> This transport must handle TCP fragmentation/coalescing carefully, since there
> is no length-prefixed framing. See the test notes in the GC-033 ticket.

## 6. Modbus over serial (NEW transport)

Direct RS485 / serial attachment (typically `/dev/ttyUSB0` on Linux). Standard
Modbus RTU over a serial line.

Design:
- Config fields: `serial_device` (path / COM port), `baud` (default 9600),
  `data_bits` (8), `parity` (none/even/odd), `stop_bits` (1), plus optional RS485
  settings. See [07-configuration.md](07-configuration.md).
- Suggested library: `go.bug.st/serial`.
- `SendDataToDevice(rtu)`:
  1. Flush input; write `rtu` (with CRC).
  2. Read the response using RTU inter-frame timing (silent interval of >= 3.5
     character times) or, more pragmatically, parse-by-length like §5 with a read
     timeout.
  3. Validate CRC; return full RTU frame.
- Serialization is inherent (one bus); the per-plant executor still applies.

## 7. Timing constraints (all wired transports)

The same inter-command delays as SolarmanV5 apply in the local read/write helper
path: 100 ms min between reads, 3000 ms min between writes (protects inverter
flash). As noted in [04-protocol-solarmanv5.md](04-protocol-solarmanv5.md) §6,
the cloud MQTT path calls `SendDataToDevice` directly and does not add these
delays; preserve that behaviour. The delays belong to the diagnostic/local API.

## 8. Compatibility checklist

- [ ] CRC-16 0xA001 over `data[:len-2]`, stored `[lo, hi]`.
- [ ] RTU read/write headers byte-identical to `CreateReadHeader`/`CreateWriteHeader`.
- [ ] Hex codec uppercase encode / even-length decode.
- [ ] Modbus TCP: strip inner CRC on send, length = rtuLen-2, rebuild CRC on recv.
- [ ] Modbus TCP: transaction id correlation, exception table, 11 retries.
- [ ] RTU-over-TCP and serial: handle stream framing, validate CRC.
