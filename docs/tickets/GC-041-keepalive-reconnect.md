# GC-041 - Keepalive & reconnect loop

- **Epic:** E - Cloud gateway
- **Type:** Feature
- **Priority:** High
- **Status:** DONE
- **Estimate:** 1 day
- **Depends on:** GC-040
- **Blocks:** GC-061

## Context

- Loop + timing + backoff:
  [docs/02-protocol-cloud-mqtt.md](../02-protocol-cloud-mqtt.md) §4, §5.
- Original: `OurMqttService` + `OurMqttService_DoWork` in
  [`JobManager-mqtt.cs`](../../../GbbEngine2/Server/JobManager-mqtt.cs).

## Description

Implement the keepalive heartbeat and the connect/reconnect timing that keeps a
plant online.

## Tasks

- Per connected plant: publish an **empty** payload to `{PlantId}/keepalive`
  (QoS 1) every **60 s**, measured from loop start (skip remainder if a loop took
  longer).
- (Re)connect disconnected, enabled plants that have credentials each cycle;
  re-subscribe after connect.
- Outer backoff after a failure cycle: **5 min** (prod) / **10 s** (dev), driven
  by `runtime.debug` rather than a compile flag.
- If no plant ever connects, keep retrying (don't exit).
- Respect context cancellation for clean shutdown.

## Acceptance criteria

- Keepalive is published ~every 60 s while connected (test with an injected
  ticker/clock).
- After a simulated disconnect, the client reconnects and re-subscribes, and
  keepalive resumes.
- Backoff uses 10 s in debug mode, 5 min otherwise.

## Test notes

- Inject a fake clock/ticker to make the 60 s and backoff deterministic and fast.
- Against the mock broker (GC-081), count keepalive messages over simulated time.
