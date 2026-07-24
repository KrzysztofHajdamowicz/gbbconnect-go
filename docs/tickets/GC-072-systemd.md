# GC-072 - systemd unit

- **Epic:** H - Packaging
- **Type:** Feature
- **Priority:** Medium
- **Status:** DONE
- **Estimate:** 0.5 day
- **Depends on:** GC-062
- **Blocks:** -

## Context

- Unit file + hardening: [docs/09-deployment.md](../09-deployment.md) §4.
- Original systemd guide: [`../README.md`](../../../README.md).

## Description

Provide a systemd unit and install docs to run `gbbconnect-go` as a Linux service.

## Tasks

- `deploy/systemd/gbbconnect.service` per [09](../09-deployment.md) §4
  (dedicated user, `After/Wants=network-online.target`, `Restart=always`,
  `StateDirectory`/`ConfigurationDirectory`, hardening options).
- Document install steps (create user, place binary, config at
  `/etc/gbbconnect/`, enable/start, journald logs).
- Note `SupplementaryGroups=dialout` for `modbus_serial`.

## Acceptance criteria

- The unit starts the service in foreground mode logging to journald.
- `systemctl enable` makes it boot-persistent; `Restart=always` recovers crashes.
- Hardening directives don't break normal operation (config/state dirs accessible).

## Test notes

- On a Linux test box/VM/container with systemd: install, start, confirm
  `active (running)` and logs in journald; restart picks up config changes.
