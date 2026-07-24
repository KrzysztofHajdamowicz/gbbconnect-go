# GC-090 - User documentation

- **Epic:** J - Docs
- **Type:** Docs
- **Priority:** Low
- **Status:** TODO
- **Estimate:** 1 day
- **Depends on:** GC-060, GC-070, GC-071, GC-072
- **Blocks:** -

## Context

- Source material: all of [docs/](../) (especially
  [07-configuration.md](../07-configuration.md),
  [08-discovery.md](../08-discovery.md), [09-deployment.md](../09-deployment.md)).

## Description

Write end-user-facing documentation: install, configure, discover, deploy,
troubleshoot.

## Tasks

- A user guide covering:
  - quick start (Docker, systemd, HA add-on);
  - configuration reference (YAML + HA options) with examples per transport;
  - migrating from official GbbConnect2 (`import-xml`);
  - using `discover` to find dongle serials;
  - troubleshooting (TLS, connectivity, timing, logs, LogLevel).
- Keep secrets guidance prominent (use env vars for tokens).
- Link from the project [README.md](../../README.md).

## Acceptance criteria

- A new user can go from zero to a connected plant using only the user guide.
- Each transport has a worked config example.
- Migration and discovery are documented with copy-pasteable commands.

## Test notes

- Doc-only; validate by following the quick start against a mock/staging setup.
