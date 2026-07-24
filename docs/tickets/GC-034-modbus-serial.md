# GC-034 - Modbus serial transport (new)

- **Epic:** D - Transports
- **Type:** Feature
- **Priority:** Medium
- **Status:** TODO
- **Estimate:** 1.5 days
- **Depends on:** GC-030
- **Blocks:** GC-082

## Context

- Spec: [docs/05-protocol-modbus.md](../05-protocol-modbus.md) §6.
- Config fields: [docs/07-configuration.md](../07-configuration.md) §2
  (`serial_port`).
- Static-build constraint: [docs/09-deployment.md](../09-deployment.md) §1.

## Description

Implement Modbus RTU over a physical serial/RS485 port (`driver:
modbus_serial`), typically on Linux (`/dev/ttyUSB0`).

## Tasks

- `internal/driver/modbusserial` using `go.bug.st/serial` (cgo-free).
- Open the port with `device`, `baud`, `data_bits`, `parity`, `stop_bits` from
  config.
- `Transport.SendRTU(rtu)`:
  - flush input; write `rtu` (with CRC);
  - read the response using RTU framing: either a length-aware loop (like GC-033)
    with a read timeout, or an inter-frame silence gap (>= 3.5 char times);
  - validate CRC; return the full RTU frame.
- Reasonable read timeout derived from baud + expected length.
- Ensure builds stay static where possible; if a platform needs cgo, gate behind
  build tags so other platforms remain `CGO_ENABLED=0`.

## Acceptance criteria

- Round-trips a read and a write against a serial mock (pty or a fake
  `io.ReadWriteCloser`).
- Respects configured line settings (verified via the fake/port options).
- CRC mismatch yields an error; timeout yields a clear timeout error.
- Linux/amd64 and arm64 builds remain static (or cgo is isolated by build tag).

## Test notes

- Use a pty pair (`github.com/creack/pty`) or an in-memory
  `io.ReadWriteCloser` injected into the transport for deterministic tests.
- Test partial-read reassembly and timeout.
