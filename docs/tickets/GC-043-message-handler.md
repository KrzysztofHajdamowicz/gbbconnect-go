# GC-043 - Message handler & error cascading

- **Epic:** E - Cloud gateway
- **Type:** Feature
- **Priority:** High
- **Status:** DONE
- **Estimate:** 1.5 days
- **Depends on:** GC-030, GC-042, GC-040
- **Blocks:** GC-044, GC-045, GC-046, GC-061

## Context

- Processing algorithm + error cascading:
  [docs/03-protocol-json-app.md](../03-protocol-json-app.md) §4, §9.
- Original: `MqttClient_MessageReceivedAsync` in
  [`JobManager-mqtt.cs`](../../../GbbEngine2/Server/JobManager-mqtt.cs).

## Description

Implement the core request handler: decode a `toDevice` message, run each line's
Modbus through the plant driver, build the response, and publish to `fromDevice`
(QoS 2). This ticket covers the happy path + error cascading; loglevel
(GC-044), log streaming (GC-045), and sub-inverter routing (GC-046) plug in.

## Tasks

- On message: decode `Header` (GC-042). If nil, ignore.
- Set `GbbVersion` (our version, e.g. `<semver>-go`) and `GbbEnvironment`
  (configurable, default OS name) on the response.
- Create + connect the plant driver (GC-030). On failure -> global error path.
- For each line with `Modbus != null`: decode hex -> bytes ->
  `driver.SendDataToDevice` -> encode hex -> store back in `line.Modbus`.
- **Error cascading**: on a line error, set `line.Error`, null `Modbus` of this
  and all subsequent lines, and break.
- **Global error**: set `Header.Error`, null all lines' `Modbus`.
- Always close the driver afterwards.
- Serialize and publish to `{PlantId}/ModbusInMqtt/fromDevice` at QoS 2.
- Per-plant serialization: route handling through the plant's executor so
  concurrent cloud messages don't overlap on one inverter.

## Acceptance criteria

- A read batch returns response hex per line; published at QoS 2.
- A 3-line batch where line 2 fails: line2.Error set, line2/line3 `Modbus` null,
  line1 has its response (matches [10](../10-compatibility-and-testing.md) §6).
- A driver-creation failure sets `Header.Error` and nulls all lines.
- The driver is always closed (no socket leak) — verified via the mock.

## Test notes

- Unit test with a mock `Transport` for: happy path, mid-batch failure, global
  failure.
- Assert QoS 2 publish and exact JSON shape against the mock cloud (GC-081).
