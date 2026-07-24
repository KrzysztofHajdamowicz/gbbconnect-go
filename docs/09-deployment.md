# 09 - Deployment

`gbbconnect-go` ships as a single static binary and targets four deployment
modes: Home Assistant Add-on, Docker, systemd (Linux), and Windows Service.

The original's deployment notes (Docker, Debian compile, systemd) are in
[`../README.md`](../README.md); the original Dockerfile is
[`../Dockerfile`](../Dockerfile). `gbbconnect-go` improves on these with small
multi-arch images and a proper HA add-on.

## 1. Build & cross-compilation

Go cross-compiles trivially. Target matrix:

| GOOS | GOARCH | GOARM | Use |
|------|--------|-------|-----|
| linux | amd64 | - | servers, x86 HA |
| linux | arm64 | - | Raspberry Pi 4/5 (64-bit), HA OS |
| linux | arm | 7 | Raspberry Pi 2/3 (32-bit) |
| windows | amd64 | - | Windows service |
| darwin | arm64/amd64 | - | dev |

> The serial transport (`modbus_serial`) uses cgo-free `go.bug.st/serial`, so
> `CGO_ENABLED=0` static builds remain possible. Confirm per ticket GC-034.

## 2. Docker (multi-arch)

The production image is defined by [`../deploy/Dockerfile`](../deploy/Dockerfile).
It uses a cached Go build stage and a `scratch` runtime containing only the
static binary and the CA certificate bundle required by MQTT/TLS. It runs as
UID/GID `65532` and supports `linux/amd64`, `linux/arm64`, and `linux/arm/v7`
through buildx:

```bash
docker buildx build \
  --platform linux/amd64,linux/arm64,linux/arm/v7 \
  --build-arg VERSION="$(git describe --tags --always --dirty)" \
  -f deploy/Dockerfile .
```

Run:

```bash
docker run -d --name gbbconnect --restart unless-stopped \
  -v /path/to/config:/config:ro \
  -v gbbconnect-state:/data \
  ghcr.io/<owner>/gbbconnect-go:latest
```

Notes:
- `/config` should be a read-only bind mount containing `gbbconnect.yaml`.
- `/data` must be writable by UID/GID `65532`; the named-volume example above
  inherits the correct ownership from the image. It stores state and daily logs.
- `modbus_serial` additionally requires `--device /dev/ttyUSB0` and access for
  UID/GID `65532` (for example an appropriate host device group or udev rule).

## 3. Home Assistant Add-on

HA add-ons are Docker images plus metadata. Structure (ticket GC-071):

```
addon/
  config.yaml          # add-on manifest + options schema
  Dockerfile           # FROM the HA base images per arch
  run.sh               # entrypoint: render config from options, exec binary
  README.md / DOCS.md
  icon.png / logo.png
```

`config.yaml` highlights:
- `arch: [aarch64, amd64, armv7]`
- `options:` + `schema:` describing the plant list (mirrors
  [07-configuration.md](07-configuration.md)).
- `map:` / `devices:` to expose serial devices if needed for `modbus_serial`.
- `host_network: true` if UDP discovery / broadcast is needed at runtime
  (usually only discovery; the bridge itself needs only outbound MQTT, so prefer
  default networking unless discovery-at-runtime is required).

`run.sh` reads `/data/options.json` (written by HA from the options schema) and
either passes it directly to the binary (the loader understands it, see
[07-configuration.md](07-configuration.md) §6) or renders a YAML file.

> HA options schema and the JSON Schema (ticket GC-014) should be kept in sync.
> Provide both an `aarch64`/`amd64`/`armv7` build.

## 4. systemd (Linux)

Mirror the original's unit (see [`../README.md`](../README.md)) but for the Go
binary. Unit file (ticket GC-072) `/etc/systemd/system/gbbconnect.service`:

```ini
[Unit]
Description=gbbconnect-go (unofficial GbbConnect2)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=gbbconnect
Group=gbbconnect
ExecStart=/usr/local/bin/gbbconnect run --config /etc/gbbconnect/gbbconnect.yaml
Restart=always
RestartSec=10
# hardening
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
StateDirectory=gbbconnect          # provides /var/lib/gbbconnect
ConfigurationDirectory=gbbconnect  # provides /etc/gbbconnect
# serial access (only if using modbus_serial):
# SupplementaryGroups=dialout

[Install]
WantedBy=multi-user.target
```

Behaviour: the binary runs in the foreground (no key wait), logs to
stdout/stderr -> journald. This replaces the original's `--dont-wait-for-key`.

## 5. Windows Service

The binary should support running as a Windows Service (ticket GC-073):
- Use `golang.org/x/sys/windows/svc` to detect service context and run the
  service control handler; fall back to console mode when run interactively.
- Provide `install` / `uninstall` subcommands (or document `sc.exe create`).
- Config at `%ProgramData%\gbbconnect\gbbconnect.yaml`; state at
  `%ProgramData%\gbbconnect\state`.
- Logs to the Windows Event Log and/or stdout.

## 6. Operational behaviour summary

- Foreground daemon by default; clean shutdown on SIGINT/SIGTERM (Linux/macOS)
  and service stop (Windows). On shutdown: disconnect MQTT, persist state.
- Restarts are safe: state files restore log-streaming positions.
- The process keeps retrying cloud/inverter connections (5 min prod backoff) and
  never exits on transient failures.

## 7. Release pipeline

CI (ticket GC-074) builds the matrix, produces checksummed archives per platform,
multi-arch container images (ghcr), and the HA add-on image set, on tags.
