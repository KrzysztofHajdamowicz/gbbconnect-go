# GC-073 - Windows Service support

- **Epic:** H - Packaging
- **Type:** Feature
- **Priority:** Low
- **Status:** DONE
- **Estimate:** 1 day
- **Depends on:** GC-062
- **Blocks:** -

## Context

- Windows service notes: [docs/09-deployment.md](../09-deployment.md) §5.

## Description

Allow the binary to run as a Windows Service, in addition to console mode.

## Tasks

- Use `golang.org/x/sys/windows/svc` to detect service context
  (`svc.IsWindowsService`) and run the service control handler; fall back to the
  normal foreground `run` when interactive.
- Wire service stop/shutdown to the GC-062 graceful shutdown hook.
- Add `gbbconnect service install` / `service uninstall` (or document `sc.exe`).
- Default config `%ProgramData%\gbbconnect\gbbconnect.yaml`, state
  `%ProgramData%\gbbconnect\state`; log to Event Log and/or stdout.
- Guard Windows-only code behind `//go:build windows` so other platforms build.

## Acceptance criteria

- Installed service starts/stops via the Services console and `sc.exe`.
- Stop performs graceful shutdown (state saved, MQTT disconnected).
- Non-Windows builds are unaffected (build tags).

## Test notes

- Manual validation on Windows: install, start, stop, uninstall.
- CI builds the windows/amd64 target (GC-004) to catch compile breakage.
