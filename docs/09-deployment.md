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

HA add-ons are Docker images plus metadata. The package lives at the
repository root as `gbbconnect_go/` — the Supervisor only discovers add-ons in
top-level directories, and the directory name must equal the add-on slug.
Structure (ticket GC-071):

```
gbbconnect_go/
  config.yaml          # add-on manifest + options schema
  Dockerfile           # pinned HA base image selected per architecture
  run.sh / render.jq   # render canonical config from UI options, exec binary
  README.md / DOCS.md
  icon.png / logo.png
```

`config.yaml` highlights:
- `arch: [aarch64, amd64, armv7]`
- `options:` + `schema:` describing typed plant and sub-inverter lists. Plant
  fields are flat to respect the Supervisor UI nesting limit, then `render.jq`
  produces the canonical model from [07](07-configuration.md).
- `uart: true` maps serial devices for `modbus_serial`.
- `host_network: false` keeps the daemon isolated by default. A local manifest
  used for runtime UDP broadcast discovery must explicitly opt into host
  networking.

`run.sh` reads `/data/options.json`, renders a mode-0600 canonical JSON file in
container-only `/tmp`, validates it with `gbbconnect config validate`, and then
executes the daemon with state under `/data/state`. The Dockerfile follows the
current BuildKit model and does not use the retired `build.yaml` fallback; see
the [official Home Assistant app build configuration](https://developers.home-assistant.io/docs/apps/configuration/).

> HA options schema and the JSON Schema (ticket GC-014) should be kept in sync.
> An automated Go test validates the defaults and shared enums. Provide
> `aarch64`/`amd64`/`armv7` builds.

## 4. systemd (Linux)

The production unit is
[`../deploy/systemd/gbbconnect.service`](../deploy/systemd/gbbconnect.service).
Install it as `/etc/systemd/system/gbbconnect.service`:

```bash
useradd --system --user-group \
  --home-dir /var/lib/gbbconnect --shell /usr/sbin/nologin gbbconnect
install -o root -g root -m 0755 gbbconnect /usr/local/bin/gbbconnect
install -o root -g root -m 0644 deploy/systemd/gbbconnect.service \
  /etc/systemd/system/gbbconnect.service
install -d -o gbbconnect -g gbbconnect -m 0750 /etc/gbbconnect
install -o root -g gbbconnect -m 0640 gbbconnect.yaml \
  /etc/gbbconnect/gbbconnect.yaml
systemctl daemon-reload
systemctl enable --now gbbconnect.service
```

The binary runs in the foreground and logs to stdout/stderr, so inspect it with
`journalctl -u gbbconnect.service -f`. `StateDirectory=gbbconnect` provides the
writable `/var/lib/gbbconnect` directory while `ProtectSystem=strict` keeps the
rest of the filesystem read-only. `Restart=always` recovers from crashes and
`systemctl enable` makes the service boot-persistent.

For `modbus_serial`, enable `SupplementaryGroups=dialout` in the unit or a
drop-in (use the actual serial-device group on the distribution). Full install,
validation, update, and serial instructions are in
[`../deploy/systemd/README.md`](../deploy/systemd/README.md).

## 5. Windows Service

The Windows binary detects whether Service Control Manager started it. In that
context it reports service state through `golang.org/x/sys/windows/svc` and
maps STOP/SHUTDOWN to the daemon's graceful cancellation path. When launched
interactively, the same EXE remains a foreground CLI.

From an elevated PowerShell session, after copying the binary to its permanent
location and creating the configuration:

```powershell
.\gbbconnect.exe config validate `
  --config "$env:ProgramData\gbbconnect\gbbconnect.yaml"
.\gbbconnect.exe service install
sc.exe start gbbconnect
sc.exe query gbbconnect
```

The installer registers an automatically started service, stores explicit
`%ProgramData%\gbbconnect` configuration/state arguments, and creates an
Application Event Log source named `gbbconnect`. Stop the service before
running `gbbconnect service uninstall`; configuration and state are preserved.
See [`../deploy/windows/README.md`](../deploy/windows/README.md) for complete
install, Event Viewer, update, and uninstall instructions.

## 6. Operational behaviour summary

- Foreground daemon by default; clean shutdown on SIGINT/SIGTERM (Linux/macOS)
  and service stop (Windows). On shutdown: disconnect MQTT, persist state.
- Restarts are safe: state files restore log-streaming positions.
- The process keeps retrying cloud/inverter connections (5 min prod backoff) and
  never exits on transient failures.

## 7. Release pipeline

Pushing a stable tag in the form `vX.Y.Z` starts the release workflow. The tag
without its leading `v` must exactly match `version` in
[`../gbbconnect_go/config.yaml`](../gbbconnect_go/config.yaml); the workflow stops
before publishing if the two differ.

The workflow:

- runs the race-enabled test suite and builds the complete GC-004 binary matrix;
- creates `.tar.gz` archives for Linux/macOS, a `.zip` archive for Windows, and
  a `SHA256SUMS` file, then attaches them to a GitHub Release with generated
  notes;
- publishes the main image as `vX.Y.Z`, `X.Y.Z`, and `latest`, with a manifest
  for `linux/amd64`, `linux/arm64`, and `linux/arm/v7`;
- publishes Home Assistant architecture images named
  `amd64-gbbconnect-go-addon`, `aarch64-gbbconnect-go-addon`, and
  `armv7-gbbconnect-go-addon`, then combines them into the preferred generic
  `gbbconnect-go-addon:X.Y.Z` multi-arch manifest (and `latest`).

The binary version is the full Git tag, for example:

```console
$ gbbconnect version
gbbconnect version v0.1.0
```

Before tagging, update the add-on manifest and changelog to the intended release
version. After the workflow finishes, verify `SHA256SUMS`, both image manifests,
and a native binary invocation. Publishing requires the repository's Actions
token to have package and release write access; the workflow requests only
those job-scoped permissions.
