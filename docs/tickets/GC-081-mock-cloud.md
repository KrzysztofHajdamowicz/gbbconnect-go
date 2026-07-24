# GC-081 - Mock MQTT cloud harness

- **Epic:** I - Testing & QA
- **Type:** Test
- **Priority:** Medium
- **Status:** DONE
- **Estimate:** 1 day
- **Depends on:** GC-040
- **Blocks:** GC-083

## Context

- Mock cloud expectations:
  [docs/10-compatibility-and-testing.md](../10-compatibility-and-testing.md) §7.
- Protocol/topics: [docs/02-protocol-cloud-mqtt.md](../02-protocol-cloud-mqtt.md),
  [docs/03-protocol-json-app.md](../03-protocol-json-app.md).

## Description

Provide a test harness that emulates the GbbOptimizer MQTT side so cloud-facing
code can be tested without the real service.

## Tasks

- Embed an MQTT broker for tests (e.g. `mochi-mqtt/server`) or script a real
  broker in CI; expose helpers to:
  - publish a request to `{PlantId}/ModbusInMqtt/toDevice`;
  - capture publishes to `{PlantId}/ModbusInMqtt/fromDevice` (with QoS) and
    `{PlantId}/keepalive`.
- Assertion helpers: expect a response JSON, expect N keepalives within a window,
  assert QoS levels and topic names.
- Support TLS optionally (self-signed) to exercise the TLS path.

## Acceptance criteria

- A test can drive a full request/response round-trip through the real client
  (GC-040) and assert the response.
- Keepalive counting works with the injected clock from GC-041.
- QoS of captured publishes is observable.

## Test notes

- Keep the broker in-process for speed; bind to an ephemeral port.
