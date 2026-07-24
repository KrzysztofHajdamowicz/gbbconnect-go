# 04 - SolarmanV5 Protocol (WiFi loggers)

Used for Solarman/LSW3 WiFi dongles (logger serials like `17xxxxxxx`,
`21xxxxxxx`, `40xxxxxxx`). This is the original's `DriverNo = 0` and the most
important transport to get byte-exact.

Authoritative sources:
[`SolarmanV5Frame.cs`](../../GbbEngine2/Drivers/000_SolarmanV5/SolarmanV5Frame.cs)
and
[`SolarmanV5Driver.cs`](../../GbbEngine2/Drivers/000_SolarmanV5/SolarmanV5Driver.cs).

## 1. Connection parameters

| Parameter | Value |
|-----------|-------|
| Default TCP port | 8899 |
| Send timeout | 5000 ms |
| Receive timeout | 5000 ms |
| TCP NoDelay | true |
| Receive buffer | 1024 bytes |
| Reconnect attempts | 11 |
| Sequence retries per attempt | 10 |
| Reconnect delay | 500 ms |
| Inter-read delay | 100 ms min |
| Inter-write delay | 3000 ms min |

Host resolution: if `AddressIP` is not a literal IP, resolve via DNS and use the
first address (`Dns.GetHostEntry(...).AddressList[0]`).

## 2. Request frame layout

Total frame size = `28 + len(modbusRTU)`. The builder is
`SolarmanV5Frame.CreateFrame`.

```
Offset  Size  Field                Value
------  ----  -----                -----
0x00    1     Start                0xA5
0x01    2     Length (LE)          len(modbusRTU) + 15
0x03    1     Control code 1       0x10
0x04    1     Control code 2       0x45   (request)
0x05    1     Sequence number      seq (1..255, see Sequence)
0x06    1     Padding              0x00
0x07    4     Logger serial (LE)   dongle serial, little-endian 4 bytes
0x0B    1     Frame type           0x02
0x0C    2     Sensor type          0x00 0x00
0x0E    4     Total working time   0x00000000
0x12    4     Power-on time        0x00000000
0x16    4     Offset time          0x00000000
0x1A    N     Modbus RTU frame     full RTU incl. CRC
0x1A+N  1     Checksum             sum(frame[1 .. end-2]) & 0xFF
0x1B+N  1     End                  0x15
```

Notes:
- The 15-byte "payload header" at offset 0x0B is the constant byte sequence
  `02 00 00 00 00 00 00 00 00 00 00 00 00 00 00` in the source.
- "Logger serial (LE)": the C# uses `BitConverter.GetBytes((long)serial)` and
  copies the first 4 bytes, i.e. the low 32 bits little-endian. Implement as
  `uint32(serial)` written little-endian.
- The sequence byte is written at offset 0x05; offset 0x06 is always 0. The C#
  writes `BitConverter.GetBytes((UInt16)SequenceNumber)[0]` then a literal `0`.

### Checksum

```
checksum = 0
for b in frame[1 .. len-2]:   # from offset 1 up to (but not including) the last 2 bytes
    checksum = (checksum + b) & 0xFF
```

The checksum byte is placed immediately before the `0x15` end byte.

## 3. Response frame & validation

Parsing is `SolarmanV5Frame.GetModBusFrame`. Validations performed (in order):

1. `FrameLen >= 5` else "Frame too short".
2. `PayloadLength = u16le(frame[1..3])`; require `FrameLen == PayloadLength + 13`
   else error. (This is the length sanity check; note it is `+13`, not `+11`.)
3. `frame[0] == 0xA5` (start) else error.
4. `frame[FrameLen-1] == 0x15` (end) else error.
5. **Checksum is NOT validated** — the verification code is intentionally
   commented out in the source ("Źle działa z SofarSolar" = "works badly with
   SofarSolar"). Do not validate the V5 checksum on responses.
6. `frame[3] == 0x10 && frame[4] == 0x15` (response control code) else
   "Wrong ControlCode".
7. `frame[5] == sequenceNumber` else "Wrong SequenceNumber".
8. `frame[7..11] == serial[0..4]` (the 4 little-endian serial bytes) else
   "Wrong SerialNumber".
9. `frame[11] == 0x02` (frame type) else "Wrong FrameType".

### Extracting the inner Modbus RTU

```
modbusRTU = frame[25 : FrameLen - 2]   # from offset 25 to (len - 2), exclusive
```

i.e. drop the first 25 bytes and the trailing checksum+end (2 bytes). Require the
result length `>= 5` else "frame does not contain a valid Modbus RTU frame". The
returned bytes are a full Modbus RTU frame **including its CRC**.

> The byte at offset 0x06 (request padding) vs the response layout: in the
> request the serial starts at 0x07; on extraction the code reads serial at
> offsets 7..10 and modbus at 25, consistent with the same header layout for
> responses.

## 4. Sequence number

From `GetNextSequenceNumber`:

- First use: random initial value `Random.Next(1, 255)` (so 1..254 inclusive in
  .NET semantics; an implementation may use 1..255 — the exact initial value is
  not protocol-significant as long as it is in 1..255 and echoed back correctly).
- Subsequent: `seq = (seq + 1) & 0xFF`.
- The response is matched on `frame[5]` only.

Stored as a `UInt16` internally but only the low byte is on the wire.

## 5. Send / receive algorithm

`SendDataToDevice` (public) wraps `InternalSend`, which is serialized by a lock
(per [01-architecture.md](01-architecture.md), this becomes per-plant
serialization in Go):

```
InternalSend(frameBytes):
  for attempt in 1..11:
    try:
      for seqTry in 0..9:
        send(frameBytes)
        resp = recv(up to 1024 bytes, 5s timeout)   # blocking read
        if len(resp) == 0: error "Connection Lost (received 0 bytes)"
        if resp[5] == seq: return resp               # sequence matched
        # else: wrong sequence -> resend same frame
      return resp   # after 10 tries, return last (will fail later validation)
    catch cancel/oom: rethrow
    catch other:
      if attempt == 11: rethrow
      close socket; sleep 500ms; reconnect
```

After `InternalSend` returns, the response is validated and the inner Modbus RTU
extracted (see §3).

## 6. Higher-level read/write & timing

`WriteSyncData` (used by `ReadHoldingRegister` / `WriteMultipleRegister`) applies
the inter-command delay **before** sending:

- read: at least 100 ms since last send completion;
- write: at least 3000 ms since last send completion.

It then calls `SendDataToDevice`, validates the **Modbus** CRC of the response
(this CRC *is* checked, unlike the V5 checksum), and interprets the function
code:

- `function > 128`: Modbus exception ->
  `"Error response: function: {function-128}, error={data[2]}"`.
- `function >= 5 && function != 23`: write-type response, return full buffer.
- otherwise: read response, return the `len`-byte data region (`buf[3 : 3+len]`).

> Important nuance: the **MQTT path does not use `WriteSyncData`**. The MQTT
> handler calls `SendDataToDevice` directly (see
> [03-protocol-json-app.md](03-protocol-json-app.md) step 6), which returns the
> **full Modbus RTU frame including CRC**, and does NOT apply the 100ms/3000ms
> delay or the function-code interpretation. The delay/CRC/interpretation logic
> lives in `WriteSyncData`, which is only used by the local read/write helpers.
>
> For `gbbconnect-go`: the cloud bridge path must mirror `SendDataToDevice`
> (return full RTU incl. CRC, no extra delay). The 100ms/3000ms timing is part of
> the local helper API; expose it through the driver interface for the
> non-MQTT/diagnostic paths and discovery, but DO NOT silently insert delays into
> the cloud request path beyond what the original does. See
> [06-driver-interface.md](06-driver-interface.md) for how the interface
> separates these.

## 7. Discovery (UDP)

`OurSearchSolarman(IPAddress address)`:

- Bind a UDP socket to the given local address, port **48899**, broadcast
  enabled, 5 s send/recv timeouts.
- Send the ASCII request `WIFIKIT-214028-READ` to `255.255.255.255:48899`.
- Collect every response string that is not an echo of the request. Responses are
  comma-ish ASCII strings containing the dongle IP, MAC, and serial.

See [08-discovery.md](08-discovery.md) for the CLI built on top of this.

## 8. Worked example

Reading 3 registers (start 0x009C=156) from unit 1, dongle serial `0x12345678`,
sequence 0x2A:

Request Modbus RTU: `01 03 00 9C 00 03 D5 CA`

V5 frame:
```
A5 17 00 10 45 2A 00 78 56 34 12 02 00 00 00 00 00 00 00 00 00 00 00 00 00
01 03 00 9C 00 03 D5 CA <checksum> 15
```
- `17 00` = length = 8 + 15 = 23 (0x0017) little-endian.
- `2A 00` = seq 0x2A, padding 0.
- `78 56 34 12` = serial 0x12345678 little-endian.

The inverter's V5 response carries a Modbus RTU response such as
`01 03 06 00 12 00 34 00 56 <crc>` which is extracted from offset 25..len-2 and
returned (with its CRC) to the cloud as hex.

## 9. Compatibility checklist

- [ ] Start 0xA5 / end 0x15; control 0x10 0x45 (req), 0x10 0x15 (resp).
- [ ] Length field = len(modbus)+15, little-endian at offset 1.
- [ ] Serial little-endian 4 bytes at offset 7; seq byte at offset 5, 0 at 6.
- [ ] V5 checksum computed for requests; NOT validated on responses.
- [ ] Response length check `FrameLen == PayloadLength + 13`.
- [ ] Inner modbus extracted from offset 25 to len-2.
- [ ] Sequence init random 1..255, increment `(seq+1)&0xFF`, match on frame[5].
- [ ] 11 reconnect attempts x 10 sequence retries; 500 ms reconnect delay.
- [ ] Modbus CRC validated on the local read/write path.
