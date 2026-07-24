# 08 - Discovery

`gbbconnect-go` provides a discovery CLI to find Solarman WiFi dongles on the
local network (or a provided subnet) and print discovered inverter/dongle serial
numbers, so users can fill in their config quickly.

Authoritative source for the UDP method:
[`SolarmanV5Driver.OurSearchSolarman`](../../GbbEngine2/Drivers/000_SolarmanV5/SolarmanV5Driver.cs).

## 1. UDP broadcast discovery (Solarman/LSW3)

- Open a UDP socket bound to a chosen local interface address, port **48899**,
  with broadcast enabled and ~5 s timeouts.
- Send the ASCII payload `WIFIKIT-214028-READ` to `255.255.255.255:48899`.
- Receive responses until timeout. Each dongle replies with an ASCII string,
  typically comma-separated, of the form:

  ```
  <ip>,<mac>,<serial>
  ```

  (exact format varies by firmware; capture the whole string and parse the IP,
  MAC, and the numeric serial heuristically). Ignore any echo of the request
  string itself.

### Parsing

- Split on commas; identify:
  - the field that parses as an IPv4 address -> dongle IP,
  - the field matching a MAC pattern -> MAC,
  - the long numeric field -> serial number.
- Be tolerant: print the raw response too, so users can recover info if parsing
  misses a field.

## 2. Subnet scan (new, optional)

For networks where UDP broadcast is blocked (VLANs, some APs), support scanning a
provided CIDR by attempting a SolarmanV5 probe to each host on the dongle port:

- Input: `--subnet 192.168.1.0/24` (and optional `--port 8899`).
- For each host, attempt a TCP connect to the port with a short timeout; on
  success, optionally send a minimal SolarmanV5 read and try to read back the
  logger serial from the response frame (`frame[7..10]`, little-endian).
- Concurrency-limited (e.g. 64 workers) to scan quickly without flooding.
- Print discovered hosts and any serials obtained.

> Subnet scanning cannot always reveal the serial without a valid read; at
> minimum it reports reachable dongle ports. Document this limitation in the CLI
> help.

## 3. CLI design

Discovery is a subcommand of the main binary:

```
gbbconnect discover [flags]

Flags:
  --interface string   local interface IP to bind for UDP broadcast (default: auto)
  --broadcast          use UDP broadcast discovery (default true)
  --subnet string      additionally scan this CIDR (e.g. 192.168.1.0/24)
  --port int           dongle TCP port for subnet scan (default 8899)
  --timeout duration   discovery timeout (default 5s)
  --json               output machine-readable JSON
```

### Output (human)

```
Discovered Solarman dongles:
  IP              MAC                Serial
  192.168.1.100   AC:1F:0B:xx:xx:xx  1720000000
  192.168.1.105   AC:1F:0B:yy:yy:yy  4012345678

2 dongle(s) found.
```

### Output (`--json`)

```json
{
  "dongles": [
    { "ip": "192.168.1.100", "mac": "AC:1F:0B:xx:xx:xx", "serial": 1720000000, "raw": "..." }
  ]
}
```

The JSON output lets automation tools (and the user's HA setup) consume results
directly and build/patch a config (see [07-configuration.md](07-configuration.md)).

## 4. Relationship to config

A future convenience (separate ticket, optional) could emit a ready-to-paste
`plants:` YAML snippet per discovered dongle. For now, discovery just prints
serials/IPs; the user copies them into `gbbconnect.yaml`.

## 5. Compatibility notes

- The broadcast string `WIFIKIT-214028-READ` and port `48899` are fixed and must
  match exactly.
- Discovery is a local-only, setup-time feature; it does not talk to the cloud.
- Some dongles only answer when queried from the same subnet/interface; the
  `--interface` flag exists for multi-homed hosts.
