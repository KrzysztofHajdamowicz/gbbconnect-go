# GC-051 - Subnet scan discovery

- **Epic:** F - Discovery
- **Type:** Feature
- **Priority:** Low
- **Status:** TODO
- **Estimate:** 1 day
- **Depends on:** GC-050
- **Blocks:** GC-052

## Context

- Spec + limitations: [docs/08-discovery.md](../08-discovery.md) §2.

## Description

Add a subnet scanner for networks where UDP broadcast is blocked: probe each host
in a CIDR on the dongle port and, where possible, read back the logger serial.

## Tasks

- `ScanSubnet(ctx, cidr string, port int, concurrency int) ([]Dongle, error)`.
- For each host in the CIDR, attempt a TCP connect (short timeout) to `port`.
- On connect, optionally attempt a minimal SolarmanV5 read and extract the logger
  serial from the response (`frame[7..10]`, little-endian) using GC-031.
- Bounded concurrency (e.g. 64 workers); respect context cancellation.
- Report reachable hosts even when the serial can't be obtained.

## Acceptance criteria

- Scans a small CIDR against a mock dongle listener and reports the host (+ serial
  when obtainable).
- Concurrency is bounded; cancellation stops the scan promptly.
- Documents that serial may be unavailable without a valid read.

## Test notes

- Use a loopback listener bound to a few ports on 127.0.0.0/30-style ranges (or
  inject the host list) to keep the test hermetic and fast.
