# GC-040 - MQTT/TLS client

- **Epic:** E - Cloud gateway
- **Type:** Feature
- **Priority:** High
- **Status:** DONE
- **Estimate:** 1.5 days
- **Depends on:** GC-011
- **Blocks:** GC-041, GC-043, GC-061, GC-081

## Context

- Transport spec: [docs/02-protocol-cloud-mqtt.md](../02-protocol-cloud-mqtt.md).
- Original: `ConnectToMqtt` in
  [`JobManager-mqtt.cs`](../../../GbbEngine2/Server/JobManager-mqtt.cs).

## Description

Implement the per-plant MQTT/TLS client wrapper: connect, subscribe, publish,
with the exact client id, credentials, topics, and QoS levels.

## Tasks

- `internal/cloud` MQTT client using `eclipse/paho.mqtt.golang` (or
  `autopaho`/v5; choose and document).
- Client id `GbbConnect2_{PlantId}`, username = PlantId, password = PlantToken.
- TLS on by default with verification; honor `tls_insecure_skip_verify` (off by
  default, loud warning when on).
- API: `Connect(ctx)`, `Subscribe(handler)` for
  `{PlantId}/ModbusInMqtt/toDevice` (QoS 1), `Publish(topic, payload, qos)`,
  `Disconnect()`.
- Helpers to build the three topic strings from PlantId.
- Never log the token (use GC-002 redaction).

## Acceptance criteria

- Connecting to the mock broker (GC-081) uses the correct client id and
  credentials.
- Subscribes to `toDevice` at QoS 1; re-subscribes after reconnect.
- Publish honors the requested QoS (1 for keepalive, 2 for responses).
- TLS verification on by default; insecure mode requires explicit opt-in.

## Test notes

- Against the mock broker: assert subscription topic/QoS and that the handler
  fires on a published message.
- Assert token never appears in logs.
