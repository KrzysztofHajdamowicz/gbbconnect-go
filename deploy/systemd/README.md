# systemd installation

The supplied unit runs `gbbconnect` in the foreground as an unprivileged,
non-login user. Standard output and standard error go to journald. systemd
creates `/etc/gbbconnect` and `/var/lib/gbbconnect` with restrictive
permissions; the latter persists runtime state.

Commands below require root privileges:

```bash
useradd --system --user-group \
  --home-dir /var/lib/gbbconnect --shell /usr/sbin/nologin gbbconnect

install -o root -g root -m 0755 gbbconnect /usr/local/bin/gbbconnect
install -o root -g root -m 0644 \
  deploy/systemd/gbbconnect.service /etc/systemd/system/gbbconnect.service
install -d -o gbbconnect -g gbbconnect -m 0750 /etc/gbbconnect
install -o root -g gbbconnect -m 0640 \
  gbbconnect.yaml /etc/gbbconnect/gbbconnect.yaml

systemctl daemon-reload
systemctl enable --now gbbconnect.service
systemctl status gbbconnect.service
journalctl -u gbbconnect.service -f
```

Validate a changed configuration before restarting:

```bash
/usr/local/bin/gbbconnect config validate \
  --config /etc/gbbconnect/gbbconnect.yaml
systemctl restart gbbconnect.service
```

`Restart=always` restarts the daemon after unexpected exits as well as clean
ones. `systemctl stop` is not restarted because systemd considers an explicit
stop operation authoritative.

## Serial Modbus

For the `modbus_serial` driver, copy the unit and uncomment:

```ini
SupplementaryGroups=dialout
```

Alternatively, add a drop-in with `systemctl edit gbbconnect.service`:

```ini
[Service]
SupplementaryGroups=dialout
```

Use the group owning the serial device on the target distribution (`dialout`
is common; some use `uucp`). Do not relax the other hardening directives.

After replacing the binary or configuration, run `systemctl restart
gbbconnect.service`. The daemon handles SIGTERM, disconnects MQTT, and saves
plant state before `TimeoutStopSec=40s` expires.
