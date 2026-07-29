# GC-047 - Plaintext MQTT cloud connections

- **Epic:** E - Cloud gateway
- **Type:** Feature
- **Priority:** Medium
- **Status:** DONE
- **Estimate:** 0.5 day
- **Depends on:** GC-040
- **Blocks:** -

## Context

- Protocol doc: [docs/02-protocol-cloud-mqtt.md](../02-protocol-cloud-mqtt.md)
  §1 — TLS on by default, matching the original's production builds.
- Original: `UseTls` in
  [`JobManager-mqtt.cs`](../../../GbbEngine2/Server/JobManager-mqtt.cs)
  (`ConnectToMqtt`) — DEBUG builds of GbbConnect2 connect without TLS.
- gbbconnect-go hardcoded the `tls://` scheme and always applied a TLS
  configuration, so brokers without a TLS endpoint were unreachable.

## Description

Add a per-plant `cloud.use_tls` boolean (default `true`). When `false`, the
cloud client dials the broker over plaintext `tcp://` instead of `tls://`,
mirroring the original `UseTls` flag. Transport-level change only: payloads,
topics, client ID, credentials, and protocol version are unchanged.

## Tasks

- `config.Cloud.UseTLS` (`use_tls`, default `true` via `DefaultCloud()`),
  JSON Schema entry, and env override `GBB_PLANT_<N>_CLOUD_USE_TLS`.
- Scheme selection and conditional TLS configuration in
  `internal/cloud/client.go`; loud warning when TLS is off because the plant
  token travels in cleartext, plus a notice that `tls_insecure_skip_verify`
  has no effect without TLS.
- `cloudtest.Broker.CloudConfig` returns `UseTLS: !plaintext`, so the
  production client can be exercised against the plaintext test broker.
- Home Assistant add-on option `cloud_use_tls` (config.yaml, render.jq,
  translations, addon sync test) with version bump to 0.1.8.
- Legacy `Parameters.xml` has no UseTls attribute; imports inherit the
  default `true` through `config.DefaultPlant()`.

## Acceptance criteria

- Omitted `use_tls` decodes to `true` and preserves the previous `tls://`
  behaviour byte-for-byte.
- `use_tls: false` produces a `tcp://` broker URL with no TLS configuration
  and a warning log that never contains the plant token.
- The production client completes connect + keepalive against the plaintext
  `cloudtest` broker.

## Test notes

- Unit: `TestNewClientPlaintextDialsTCPWithoutTLS`,
  `TestNewClientPlaintextIgnoresInsecureSkipVerify` in
  `internal/cloud/client_test.go`; default-decode checks in
  `internal/config/model_test.go`; env override cases in
  `internal/config/loader_test.go`.
- Integration: `TestRealMQTTClientConnectsToPlaintextBroker` in
  `internal/cloud/mqtt_harness_test.go`.
