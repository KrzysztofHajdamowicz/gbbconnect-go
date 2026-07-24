# GC-060 - App bootstrap & CLI

- **Epic:** G - Runtime
- **Type:** Feature
- **Priority:** High
- **Status:** DONE
- **Estimate:** 0.5 day
- **Depends on:** GC-011
- **Blocks:** GC-052, GC-061

## Context

- Startup behaviour: [docs/01-architecture.md](../01-architecture.md) §4.
- Original console entry: [`Program.cs`](../../../GbbConnect2Console/Program.cs).

## Description

Build the CLI surface and the `run` command bootstrap that loads config and starts
the supervisor.

## Tasks

- Root command with global flags: `--config`, `--state-dir`, `--log-level`,
  `--dev` (maps to `runtime.debug`).
- Subcommands: `run` (default daemon), `discover` (GC-052), `import-xml`
  (GC-012), `config validate` (GC-014), `version`.
- `run`: resolve + load config (GC-011), init logging (GC-002), build the
  supervisor (GC-061), block until shutdown (GC-062).
- Print a concise startup banner (version, config path, state dir, plant count) —
  echoing the spirit of the original console output, but never printing secrets.
- No interactive key-wait; run as a foreground daemon.

## Acceptance criteria

- `gbbconnect run --config x.yaml` starts and logs the banner; missing config
  prints a clear error + sample and exits non-zero.
- Global flags override config where applicable.
- `version`, `discover`, `import-xml`, `config validate` are reachable.

## Test notes

- Smoke test `run` with a mock cloud (GC-081) and one `random`/mock-driver plant:
  it connects and stays up until cancelled.
