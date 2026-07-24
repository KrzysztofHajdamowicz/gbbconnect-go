# GC-050 - UDP Solarman discovery

- **Epic:** F - Discovery
- **Type:** Feature
- **Priority:** Medium
- **Status:** DONE
- **Estimate:** 1 day
- **Depends on:** GC-031
- **Blocks:** GC-051, GC-052

## Context

- Method + parsing: [docs/08-discovery.md](../08-discovery.md) §1.
- Original: `OurSearchSolarman` in
  [`SolarmanV5Driver.cs`](../../../GbbEngine2/Drivers/000_SolarmanV5/SolarmanV5Driver.cs).

## Description

Implement UDP broadcast discovery of Solarman/LSW3 WiFi dongles and parse the
responses into structured results (IP, MAC, serial).

## Tasks

- `internal/discovery`: `DiscoverUDP(ctx, ifaceIP string, timeout) ([]Dongle,
  error)`.
- Bind UDP on the chosen local address, port 48899, broadcast enabled; send the
  ASCII payload `WIFIKIT-214028-READ` to `255.255.255.255:48899`.
- Collect responses until timeout; ignore echoes of the request.
- Parse each response (comma-separated) into `Dongle{IP, MAC, Serial, Raw}`;
  be tolerant (keep `Raw`).
- Optionally iterate all broadcast-capable interfaces when `ifaceIP` is empty.

## Acceptance criteria

- Against a mock UDP responder, returns the expected dongle(s) with parsed
  serial(s).
- Echoes of the request payload are ignored.
- Unparseable responses still appear with `Raw` populated.

## Test notes

- Spin up a local UDP listener that replies with a canned dongle string; assert
  parsing.
- Test timeout returns promptly with whatever was collected.
