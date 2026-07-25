# Contributing to gbbconnect-go

Thank you for helping improve `gbbconnect-go`. The project is a wire-compatible
reimplementation of GbbConnect2, so a small-looking protocol change can affect
real inverters and the GbbOptimizer cloud. Keep changes scoped, testable, and
grounded in the original behaviour.

## 1. Prerequisites

Required:

- Git;
- Go `1.26.5`, as declared by [`go.mod`](go.mod);
- GNU Make or a compatible `make`;
- network access for the first module and linter download.

Optional:

- Docker with buildx for container validation;
- a Linux host/VM for systemd verification;
- Home Assistant or its development environment for add-on validation;
- Windows for the final Windows Service lifecycle check.

The Go implementation and tests are cgo-free. No MQTT broker, inverter, serial
adapter, or cloud account is required for the default suite: it uses in-process
MQTT and inverter harnesses.

## 2. Clean-checkout setup

```bash
git clone https://github.com/KrzysztofHajdamowicz/gbbconnect-go.git
cd gbbconnect-go
go mod download
make build
./bin/gbbconnect version
```

`make build` writes the native binary to `bin/gbbconnect`. The embedded version
comes from `git describe` unless `VERSION` is supplied:

```bash
make build VERSION=dev-local
```

Before changing code, run the baseline:

```bash
make test
go vet ./...
make lint
make coverage-protocol
```

`make lint` installs the pinned golangci-lint version into `bin/` on first use;
it does not depend on a globally installed linter.

## 3. Repository layout

The detailed architecture and dependency direction are documented in
[docs/01-architecture.md](docs/01-architecture.md). The working layout is:

```text
cmd/gbbconnect/             CLI, daemon bootstrap, platform service hooks
internal/cloud/             MQTT client, request handler, keepalive, log control
internal/cloudtest/         in-process TLS MQTT broker for tests
internal/config/            YAML/JSON model, loading, schema validation
internal/config/xmlimport/  legacy Parameters.xml importer
internal/discovery/         UDP and bounded subnet discovery
internal/driver/            Driver facade, executor, and transport factory
internal/driver/*/          concrete inverter transports
internal/invertertest/      reusable device-side transport mocks
internal/logbuf/            runtime logging and daily files
internal/modbus/            RTU framing, CRC, response parsing, hex codec
internal/protocol/          cloud Header/Line JSON protocol
internal/state/             atomic persistent state
internal/supervisor/        per-plant workers and graceful lifecycle
internal/testutil/          embedded golden fixtures and byte assertions
schema/                     canonical configuration JSON Schema
gbbconnect_go/              Home Assistant add-on package (repo root so the
                            Supervisor can discover it; name equals the slug)
deploy/                     container image, systemd, and Windows Service docs
scripts/                    cross-build, coverage, and release helpers
docs/                       design, compatibility evidence, and ticket history
```

Keep dependency direction downward. In particular, low-level protocol packages
must not import the supervisor or CLI, and a transport package must not create
an import cycle with `internal/driver`.

## 4. Everyday development workflow

Format edited Go files:

```bash
gofmt -w path/to/file.go
```

The linter also enforces `goimports`. Run focused tests while iterating, then
the same checks used by CI:

```bash
go test ./internal/driver/solarmanv5 -race -count=1
make coverage
make coverage-protocol
go vet ./...
make lint
make build-all VERSION=dev
```

`make coverage` creates ignored `coverage.out`, `coverage.txt`, and
`coverage.html`. `make build-all` creates ignored static artifacts in `dist/`
for:

- Linux amd64, arm64, and arm/v7;
- Windows amd64;
- macOS arm64.

After changing dependencies:

```bash
go mod tidy
git diff -- go.mod go.sum
```

Do not commit `bin/`, `dist/`, coverage files, editor settings, credentials, or
raw production captures.

## 5. Compatibility is the primary rule

When sources disagree, the actual C# implementation in
[gbbsoft/GbbConnect2](https://github.com/gbbsoft/GbbConnect2) is authoritative.
The reverse-engineering documents explain intent, but they do not override
observed code or wire behaviour.

Compatibility-sensitive details include:

- byte order, offsets, checksums, CRC, frame lengths, and retry counts;
- MQTT topic names, QoS, keepalive cadence, and reconnect timing;
- PascalCase JSON names and omission of nil fields;
- exact legacy error text, including unusual capitalization;
- line-error cascading and sub-inverter routing.

For a compatibility change:

1. identify the C# source or sanitized capture that establishes the behaviour;
2. add or update a golden vector before changing implementation;
3. preserve the exact error and on-wire contract where required;
4. update the relevant protocol document and acceptance evidence;
5. explain intentional divergence from GbbConnect2 in the pull request.

Never add a production capture containing plant IDs, tokens, public IPs, MAC
addresses, logger serials, or other device identifiers. Reduce it to a minimal,
sanitized fixture and record how it was derived.

## 6. How to add a transport

Read the interface contract in
[docs/06-driver-interface.md](docs/06-driver-interface.md), the Modbus framing
rules in [docs/05-protocol-modbus.md](docs/05-protocol-modbus.md), and the
completed implementation tickets
[GC-030](docs/tickets/GC-030-driver-interface-executor.md) and
[GC-082](docs/tickets/GC-082-mock-inverter.md).

### Step 1: define the configuration contract

Add a stable string to `config.DriverType` and `config.DriverTypes()` in
`internal/config/model.go`. Decide which address, port, serial, or serial-line
fields are required, then update:

- `internal/config/validate.go`;
- `schema/gbbconnect.schema.json`;
- JSON Schema/model synchronization tests;
- `gbbconnect_go/config.yaml` and `gbbconnect_go/render.jq` if users can select it
  in Home Assistant;
- the configuration and user guides.

Only add a legacy numeric mapping when the original GbbConnect2 actually has
one. New transports should not invent a `DriverNo`.

### Step 2: implement the raw transport

Create `internal/driver/<name>/`. Its concrete type must satisfy:

```go
type Transport interface {
    Connect(ctx context.Context) error
    SendRTU(ctx context.Context, rtu []byte) ([]byte, error)
    Close() error
}
```

The contract uses complete Modbus RTU frames, including CRC, at both sides.
Medium-specific wrapping belongs inside the transport.

Implementation expectations:

- lazy or explicit `Connect` is idempotent;
- `Close` is idempotent;
- blocking I/O has context-bounded deadlines;
- writes handle short writes and reads handle fragmentation;
- reconnect/retry policy distinguishes connection loss from a valid Modbus
  exception;
- protocol response identifiers are correlated with the request;
- transport logs never include credentials;
- the raw cloud path does not add local-helper timing or interpret the response.

Use the existing transport packages as concrete examples. Avoid copying their
compatibility quirks unless the new protocol requires them.

### Step 3: register the factory

Import the package and add its `DriverType` case in
`internal/driver/factory.go`. The factory wraps the transport with
`driver.Wrap`, which supplies serialization and local read/write timing.

Add factory tests for the new type and keep the unknown-driver error unchanged.

### Step 4: add golden vectors

Place reusable sanitized byte fixtures under
`internal/testutil/testdata/<protocol>/`. Tests should cover:

- exact request bytes;
- a parsed read response and write response;
- checksum/CRC and request-response correlation;
- malformed, truncated, exception, and wrong-identifier responses;
- the first mismatching byte through `testutil.AssertBytesEqual`.

If the transport is compatibility-critical, add its package to
`scripts/check-protocol-coverage.sh` and meet the 85% statement floor.

### Step 5: add the inverter mock

Extend `internal/invertertest` with a protocol registry entry and deterministic
scenarios. Network listeners must bind an ephemeral loopback port and close via
`testing.T.Cleanup`. A serial-like transport can expose an in-memory port
contract.

At minimum, drive the real transport through:

- a read and a write round trip;
- fragmented input;
- a zero-byte close or truncated response followed by the expected reconnect;
- a malformed protocol response;
- the protocol's correlation/exception faults.

Do not test only private frame helpers. The acceptance test must call the real
transport's `Connect`/`SendRTU` path.

### Step 6: prove integration and portability

Add an end-to-end case around the supervisor when the new transport changes
routing or lifecycle behaviour. Then run:

```bash
go test ./... -race -count=1
make coverage-protocol
go vet ./...
make lint
make build-all VERSION=dev
```

For serial or OS-specific support, also validate the relevant Docker,
Home Assistant, systemd, or Windows packaging path.

## 7. Test conventions

- Prefer table-driven tests for input/error matrices.
- Use `t.Parallel()` only when the test and all shared fixtures are safe.
- Use ephemeral ports; never assume port `8899` or `8883` is free in CI.
- Register cleanup immediately after acquiring a socket, file, broker, or
  goroutine-owned resource.
- Use injected clocks and channels instead of long sleeps.
- Run concurrent and network changes with `-race` and repeat potentially flaky
  tests with `-count=20` or more.
- Compare structured JSON semantically and protocol bytes exactly.
- Keep fault injection deterministic: “fail once” state must be synchronized.
- Tests must be hermetic by default. Put real-device or real-cloud tests behind
  an explicit build tag and document required credentials.

## 8. CI and pull requests

The main CI workflow runs on every push and pull request. It requires:

- module download and `go build ./...`;
- `go vet ./...`;
- the pinned golangci-lint rules;
- the full race-enabled suite with coverage artifacts;
- at least 85% coverage in the protected protocol packages;
- the complete static cross-build matrix.

Release tags additionally build archives, checksums, multi-architecture
container images, Home Assistant images, and a GitHub Release. Do not create a
release tag merely to test a pull request.

Keep pull requests small enough to review against the compatibility source.
The description should include:

- the ticket/problem and intended behaviour;
- the authoritative source or reason for a deliberate extension;
- tests and platform checks actually run;
- manual validation still outstanding;
- configuration, migration, or security impact.

Use short imperative commits consistent with the repository, for example:

```text
feat: add example transport
fix: correlate gateway responses
test: cover fragmented frames
docs: document serial permissions
```

Do not mix unrelated formatting or generated-file churn into a behavioural
commit. Preserve unrelated changes already present in the worktree.

## 9. Security and safety

- Never log or commit cloud tokens.
- Keep TLS verification enabled by default.
- Treat Modbus writes as physical-world actions; use read-only live validation
  first.
- Do not repeatedly power-cycle an inverter or logger during testing.
- Bound discovery concurrency and scan only networks you are authorized to
  probe.
- Report a suspected credential leak or remotely exploitable issue privately
  to the maintainer before opening a public issue.

For installation and operational testing, use the
[end-user guide](docs/user-guide.md). For the current compatibility matrix, see
[docs/10-compatibility-and-testing.md](docs/10-compatibility-and-testing.md).
