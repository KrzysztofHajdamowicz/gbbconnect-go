# 06 - Driver / Transport Interface

This is the "core meat" we re-engineer cleanly. The original exposes
[`IDriver`](../../GbbEngine2/Drivers/IDriver.cs):

```csharp
public interface IDriver {
    Task<byte[]> ReadHoldingRegister(byte unit, ushort startAddress, ushort numInputs);
    Task        WriteMultipleRegister(byte unit, ushort startAddress, byte[] values);
    Task<byte[]> SendDataToDevice(byte[] data);
    void        Dispose();
}
```

The MQTT bridge path uses only `SendDataToDevice` + `Dispose`. The
read/write helpers are used by local/diagnostic code and apply timing + Modbus
interpretation.

## 1. Go interfaces

Two layers: a thin **Transport** (raw send/receive of a Modbus RTU frame over a
particular medium) and a **Driver** facade that adds Modbus helpers + timing.

```go
// Transport is medium-specific: SolarmanV5, Modbus TCP, RTU-over-TCP, serial.
// SendRTU takes a complete Modbus RTU frame (incl. CRC) and returns the
// response as a complete Modbus RTU frame (incl. CRC). This mirrors the
// original SendDataToDevice contract used by the cloud bridge path.
type Transport interface {
    Connect(ctx context.Context) error
    SendRTU(ctx context.Context, rtu []byte) (resp []byte, err error)
    Close() error
}

// Driver wraps a Transport with Modbus-level helpers and the inter-command
// timing constraints. Used by local/diagnostic callers.
type Driver interface {
    // Explicit, idempotent connection used by the cloud handler so setup
    // failures can be reported as header-level errors.
    Connect(ctx context.Context) error

    // Cloud bridge path: no extra delay, returns full RTU incl. CRC.
    SendDataToDevice(ctx context.Context, rtu []byte) ([]byte, error)

    // Local helpers: apply 100ms/3000ms delays + interpret function codes.
    ReadHoldingRegisters(ctx context.Context, unit byte, start, count uint16) ([]byte, error)
    WriteMultipleRegisters(ctx context.Context, unit byte, start uint16, values []byte) error

    Close() error
}
```

Design rules:
- `SendDataToDevice` for the cloud path must behave like the original: return the
  full RTU frame with CRC, **no** 100ms/3000ms delay, **no** function-code
  interpretation (the cloud gets raw frames; see
  [03-protocol-json-app.md](03-protocol-json-app.md) and
  [04-protocol-solarmanv5.md](04-protocol-solarmanv5.md) §6).
- `ReadHoldingRegisters` / `WriteMultipleRegisters` are the "local" API that
  applies the delay before sending, validates the response Modbus CRC, and
  interprets the function code (exception / write / read), matching
  `WriteSyncData`.

## 2. Transaction executor (serialization + timing)

A `Driver` wraps a `Transport` and a per-plant **executor** that:
- serializes all transactions (mutex or single goroutine + channel);
- tracks `lastSend` time and enforces the read/write minimum delay for the local
  helper path;
- owns reconnect-on-error policy (the retry counts live in each transport's
  `SendRTU`, matching the original 11x/10x/500ms behaviour).

```mermaid
flowchart LR
    caller["Cloud handler / local helper"] --> exec["Executor (per plant, serialized)"]
    exec --> transport["Transport.SendRTU"]
    transport --> medium["TCP / UDP-discovered TCP / serial"]
```

## 3. Transport selection (driver registry)

The original maps `Plant.DriverNo` to a concrete driver in
[`JobManager-mqtt.cs`](../../GbbEngine2/Server/JobManager-mqtt.cs) using
[`DriverInfo`](../../GbbEngine2/Drivers/DriverInfo.cs):

| DriverNo | Original driver |
|----------|-----------------|
| 0 | SolarmanV5 (wifi-dongle) |
| 1 | ModbusTCP (wired-dongle) (BETA) |
| 999 | Random (debug only) |

`gbbconnect-go` uses **string driver types** in YAML config (friendlier), with a
numeric-compatibility mapping for legacy import:

| Config `driver` | Legacy DriverNo | Doc |
|-----------------|-----------------|-----|
| `solarman_v5` | 0 | [04](04-protocol-solarmanv5.md) |
| `modbus_tcp` | 1 | [05](05-protocol-modbus.md) §4 |
| `modbus_rtu_tcp` | (new) | [05](05-protocol-modbus.md) §5 |
| `modbus_serial` | (new) | [05](05-protocol-modbus.md) §6 |
| `random` | 999 | test only |

A factory `func New(plant Config, log Logger) (Driver, error)` instantiates the
right transport. Unknown driver -> error
`"Unknown driver: {driver}"` (mirrors original `"Unknown driver no: ..."`).

## 4. Construction parameters

Each transport needs: target `host`/`ip`, `port`, and a `serial` (dongle serial,
used by SolarmanV5 framing; for sub-inverter routing this is the
`DongleSerialNumber`). Serial transport needs the device path + line settings
instead of host/port. Validation:
- SolarmanV5: ip, port, serial all required (original throws
  `"Missing IP Address" / "Missing Port Number" / "Missing SerialNumber"`).
- Modbus TCP / RTU-over-TCP: ip + port required; serial optional.
- Serial: device path + baud required.

## 5. Lifecycle

- `Connect` is explicit and idempotent (safe to call to (re)establish).
- `Close` shuts the socket/port down and is safe to call multiple times
  (original guards with a `disposed` flag).
- For the cloud path, the original constructs a driver per message and disposes
  it. `gbbconnect-go` may keep the transport open across messages within a plant
  worker (reconnect on error); either approach is acceptable as long as the
  on-wire behaviour and serialization are preserved.

## 6. Error model

- Transport errors are returned as Go `error`; the cloud handler turns them into
  per-line / header errors (see error cascading in
  [03-protocol-json-app.md](03-protocol-json-app.md)).
- Distinguish: connection errors (trigger reconnect), Modbus exception responses
  (surface message, no reconnect), and protocol-validation errors (bad
  frame/CRC/sequence).

## 7. Testability

- `Transport` is an interface -> easy to mock for the cloud-handler unit tests.
- Each real transport is tested against a recorded byte-stream fixture / loopback
  server (see [10-compatibility-and-testing.md](10-compatibility-and-testing.md)
  and the GC-08x tickets).
