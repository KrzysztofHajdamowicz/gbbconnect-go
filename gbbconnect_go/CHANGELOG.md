# Changelog

## 0.1.2

- The add-on package moved from `deploy/addon/` to `gbbconnect_go/` at the
  repository root, so this repository can be added directly as a Home
  Assistant add-on repository. If you previously installed the add-on as a
  local copy under `/addons/gbbconnect_go`, that install is a separate add-on
  identity: note down your options, uninstall the local add-on, and install
  from the repository (state under `/data` does not carry over). Existing
  users of the repository URL may need **Check for updates** before the
  add-on appears.

## 0.1.1

- Fix start-up failure for serial-port plants: Supervisor stored
  `serial_stop_bits` as a string (`list(1|2)` schema), which the rendered
  configuration rejected. The option now uses an `int(1,2)` schema and the
  renderer coerces legacy string values back to numbers.

## 0.1.0

- Initial Home Assistant add-on package.
- Native options mapping for all gbbconnect-go drivers.
- Persistent state/log directories and serial-device support.
