# PlusClouds VM Agent

> **The foundation that thinks.**

The PlusClouds VM Agent is a lightweight, production-grade daemon that turns any Linux or Windows machine into an intelligent infrastructure node. It connects to the PlusClouds platform over NATS, streams real-time telemetry, executes remote commands, and announces its own capabilities — so your platform always knows exactly what each machine can do.

No REST API. No open ports. No certificates to manage. No config file, either — the agent reads its entire runtime configuration (identity, NATS connection, allowed operations, logging, autoheal) from the config-drive ISO the platform attaches at provisioning time, and caches a local copy so it keeps working on later boots even if the drive is detached.

---

## What it does

| Capability | Description |
|---|---|
| **Real-time telemetry** | CPU, memory, disk I/O, and network metrics pushed every 30 seconds |
| **Remote command execution** | Service management, OS user passwords, static network config, SSH keys, environment variables, disk resize, system updates, and custom operations — all triggered from the platform |
| **Boot-time provisioning** | Password, network config, disk resize, SSH keys, and environment variables are also applied automatically from the config-drive on every boot, independent of any live command |
| **Capability discovery** | On boot the agent publishes its full operation schema — the platform always knows what it can do |
| **Heartbeat** | Keeps the `is_alive` status current on the platform every 30 seconds |
| **Cross-platform** | Single codebase, two binaries: Linux (systemd) and Windows (native SCM service) — both installable as an auto-starting service with one script |
| **Zero open ports** | Outbound-only NATS WebSocket connection — no inbound firewall rules needed |

---

## Architecture

The agent communicates exclusively over NATS. It subscribes to its own command subject and publishes events to its own event subject.

```
agent.vm.{uuid}.cmd   ←  platform sends commands
agent.vm.{uuid}.evt   →  agent sends telemetry, heartbeat, capabilities, results
vm.{uuid}.telemetry   →  client-facing telemetry stream (VM_TELEMETRY JetStream, 15-min retention)
```

Authentication uses the `agent_api_key` from the config-drive's `pc-meta-data.json` (written by the platform during provisioning). The NATS auth callout validates every connection against the platform database and issues a scoped JWT — no static passwords, no shared secrets.

### Message envelope

Every NATS message in either direction shares a common JSON envelope:

```json
{
  "v":          1,
  "id":         "550e8400-e29b-41d4-a716-446655440000",
  "type":       "command|telemetry|heartbeat|capabilities|result|...",
  "agent_type": "vm",
  "agent_uuid": "550e8400-e29b-41d4-a716-446655440001",
  "timestamp":  1748000000,
  "payload":    {}
}
```

See [docs/protocol.md](docs/protocol.md) for the full payload schema for every event type.

---

## Telemetry payload

The agent publishes a structured telemetry snapshot every 30 seconds:

```json
{
  "cpu": {
    "usage_pct":  12.5,
    "core_count": 4,
    "load_avg":   [0.18, 0.23, 0.09],
    "cores": [
      { "id": 0, "usage_pct": 15.2 },
      { "id": 1, "usage_pct": 9.8 }
    ]
  },
  "memory": {
    "total_bytes": 4051935232,
    "used_bytes":  564654080,
    "usage_pct":   13.9
  },
  "disks": [
    {
      "device": "/dev/xvda2", "mountpoint": "/",
      "total_bytes": 42156257280, "used_bytes": 7149359104, "usage_pct": 17.7,
      "io": {
        "read_bytes_per_s": 1048576, "write_bytes_per_s": 524288,
        "read_iops": 120, "write_iops": 45, "util_pct": 23.4
      }
    }
  ],
  "network": [
    { "interface": "eth0", "bytes_sent": 95310, "bytes_recv": 1652348, "is_up": true }
  ]
}
```

Disk I/O rates are calculated as a delta between consecutive snapshots — the first telemetry event after boot omits the `io` field. Pseudo-filesystems (`tmpfs`, `devtmpfs`, etc.) and virtual network interfaces (`lo`, `docker*`, `veth*`) are automatically excluded.

---

## Supported operations

The agent announces its available operations on boot via a `capabilities` event. The platform can also request a fresh capabilities list at any time by sending `agent.allowed_operations`.

| Operation | Description | Parameters |
|---|---|---|
| `agent.allowed_operations` | Re-publish the capabilities list | — |
| `services.list` | List all loaded systemd services | — |
| `services.get` | Get status of a single service | `name` (string) |
| `services.start` | Start a service | `name` (string) |
| `services.stop` | Stop a service | `name` (string) |
| `services.restart` | Restart a service | `name` (string) |
| `services.reload` | Reload a service | `name` (string) |
| `services.enable` | Enable a service on boot | `name` (string) |
| `services.disable` | Disable a service on boot | `name` (string) |
| `system.info` | Hostname, OS, kernel, uptime | — |
| `system.metrics` | Full resource snapshot | — |
| `system.cpu` | CPU usage + per-core breakdown | — |
| `system.memory` | RAM utilisation | — |
| `system.disk` | Disk usage + I/O rates | — |
| `system.network` | Network interface counters | — |
| `system.update` | `apt-get update && upgrade -y` | — (Ubuntu/Debian only) |
| `cron.list` | List scheduled tasks — cron-style files (`/etc/crontab`, `/etc/cron.d`, per-user crontabs) and systemd timers | — (read-only) |
| `identity.set_password` | Set the login password for a local OS user | `username` (string), `password` (string) |
| `network.configure` | Statically apply IP/netmask/gateway/DNS/MTU to a NIC, matched by MAC address | `network` (object — same shape as a `virtual_network_cards` entry) |
| `sshkeys.add` | Add SSH public keys to a user's `authorized_keys` (additive only) | `username` (string), `keys` (array) |
| `sshkeys.remove` | Remove SSH public keys from a user's `authorized_keys`, matched by key identity | `username` (string), `keys` (array) |
| `sshkeys.list` | List the SSH public keys in a user's `authorized_keys` | `username` (string) — (read-only) |
| `env.set` | Set a system-wide persistent environment variable | `env_key` (string), `env_value` (string) |
| `env.unset` | Remove a system-wide persistent environment variable | `env_key` (string) |
| `env.list` | List all system-wide persistent environment variables | — (read-only) |
| `telemetry.set_interval` | Change telemetry push interval | `interval_s` (integer, min 5) |
| `vm.reboot` | Reboot the machine | — |
| `vm.shutdown` | Shut down the machine | — |
| `exec` | Run an allowed binary | `command` (string), `args` (array) |

All operations are opt-in. Remove any entry from `allowed_operations` in the config-drive's `agent` settings and the platform receives a `rejected` result instead of executing it. `identity.set_password`, `network.configure`, `sshkeys.add`, `sshkeys.remove`, `env.set`, and `env.unset` are invasive and, like `disk.resize`/`exec`, are **not** in the built-in defaults — they must be explicitly added to `allowed_operations` to be callable. `sshkeys.list` and `env.list` are read-only and enabled by default.

The same five (password, network, disk resize, SSH keys, env vars) are also applied automatically at boot from the config-drive metadata, independent of `allowed_operations` — see "Boot-time provisioning" below.

---

## Boot-time provisioning

On every startup, before connecting to NATS, the agent applies the config-drive metadata's desired state directly, the same way it has always additively provisioned SSH keys:

| From `pc-meta-data.json` | Applied via |
|---|---|
| `username` + `password` | `identity.set_password` logic (`chpasswd` / `net user`) |
| `virtual_network_cards` | `network.configure` logic, once per NIC |
| — | disk resize of the root/system volume, in case a cloud volume was expanded since last boot |
| `username` + `ssh_keys` | additive `sshkeys.add` logic (never removes a key) |
| `env_vars` | `env.set` logic, once per entry |

Every step is best-effort and non-fatal — a bad NIC config or an unresolvable username is logged as a warning and does not prevent the agent from starting and reaching NATS.

Password and network config are **declarative**, unlike SSH keys: boot re-applies the config-drive's value outright. If the platform pushes a live `identity.set_password` or `network.configure` command over NATS, a later reboot will re-assert whatever is cached in `pc-meta-data.json` unless the platform also keeps that file in sync with any live change it issues.

---

## Installation

### Quick install (recommended)

`scripts/install.sh` installs (or upgrades) the agent as a systemd service in one step: it resolves the latest GitHub release (or a version you pin), downloads `plusclouds.linux` and the systemd unit, sets up `/var/log/plusclouds` and `/etc/plusclouds`, and enables + starts the service. It's safe to re-run for upgrades.

```bash
# Install the latest release
curl -fsSL https://raw.githubusercontent.com/plusclouds/vm.agent/master/scripts/install.sh | sudo bash

# Or pin a specific release
curl -fsSL https://raw.githubusercontent.com/plusclouds/vm.agent/master/scripts/install.sh | sudo bash -s v2.0.0

# Or, if you already have the repo checked out
sudo ./scripts/install.sh          # latest
sudo ./scripts/install.sh v2.0.0   # pinned
```

Requirements: root, `curl`, systemd, and `amd64` (the only architecture published today). There's no config file to set up afterward — the agent reads its identity and runtime settings from the config-drive ISO the platform attaches to the VM (see [Attach the config-drive](#3-attach-the-config-drive) below). If the VM has no config-drive (e.g. local testing), set `PLUSCLOUDS_AGENT_NATS_AGENT_UUID` / `PLUSCLOUDS_AGENT_NATS_API_KEY` in `/etc/plusclouds/environment` and restart the service.

### Quick install — Windows

`scripts/install.windows.ps1` does the same thing for Windows: resolves the latest GitHub release (or a version you pin), downloads `plusclouds.windows`, installs it under `%ProgramFiles%\PlusClouds`, registers it as a native Windows service (`PlusCloudsAgent` — the agent responds to the Service Control Manager directly, no third-party wrapper needed) set to start automatically on boot and restart on failure, and starts it. Safe to re-run for upgrades.

```powershell
# Install the latest release (run PowerShell as Administrator)
irm https://raw.githubusercontent.com/plusclouds/vm.agent/master/scripts/install.windows.ps1 | iex

# Or pin a specific release
$script = irm https://raw.githubusercontent.com/plusclouds/vm.agent/master/scripts/install.windows.ps1
& ([scriptblock]::Create($script)) -Version v2.2.0

# Or, if you already have the repo checked out
.\scripts\install.windows.ps1                   # latest
.\scripts\install.windows.ps1 -Version v2.2.0   # pinned
```

Requirements: Administrator, and `amd64` (the only architecture published today). As on Linux, there's no config file — the agent reads its identity and runtime settings from the config-drive the platform attaches to the VM (located automatically by volume label, no drive letter to configure). Unlike systemd, a Windows service has no console to capture stdout, so the installer also points the agent at a log file (`%ProgramData%\PlusClouds\logs\agent.log`) via a machine-wide `PLUSCLOUDS_AGENT_LOG_FILE` environment variable. If the VM has no config-drive attached (e.g. local testing), set `PLUSCLOUDS_AGENT_NATS_AGENT_UUID` / `PLUSCLOUDS_AGENT_NATS_API_KEY` the same way (`[Environment]::SetEnvironmentVariable(..., 'Machine')`) and restart the service.

### Manual installation

#### 1. Deploy the binary

```bash
# Linux
scp bin/plusclouds.linux root@<server-ip>:/usr/local/bin/plusclouds-agent
chmod +x /usr/local/bin/plusclouds-agent
```

```powershell
# Windows
Copy-Item bin\plusclouds.windows "\\<server>\C$\Program Files\PlusClouds\plusclouds-agent.exe"
```

#### 2. Create the runtime directories

```bash
# Linux
mkdir -p /var/log/plusclouds /var/lib/plusclouds/cache /media/plusclouds-config
chmod 0750 /var/log/plusclouds
```

`/media/plusclouds-config` must exist before the service is started — the systemd unit's `ReadWritePaths=` (under `ProtectSystem=strict`) is applied before any `ExecStartPre` runs, so a missing path fails the whole unit with `226/NAMESPACE` instead of being created on demand.

```powershell
# Windows — no config-drive mount path to create (located by volume label,
# not a fixed path), just somewhere to log to and cache metadata
New-Item -ItemType Directory -Force -Path "$env:ProgramData\PlusClouds\logs", "$env:ProgramData\PlusClouds\cache"
```

#### 3. Attach the config-drive

The platform attaches a config-drive (on Linux, a standard cloud-init NoCloud ISO labelled `cidata`; on Windows, a volume with the same label, whatever drive letter it's assigned) containing `pc-meta-data.json` to the VM during provisioning. The agent finds and reads it automatically at boot and caches a local copy so it keeps working on later boots even if the drive is later detached. There's nothing to deploy manually — if you're running the agent somewhere the config-drive isn't attached (e.g. local testing), see [Configuration reference](#configuration-reference) below for the built-in defaults and environment variable overrides.

#### 4. Install and start the service

```bash
# Linux
scp systemd/plusclouds-agent.service root@<server-ip>:/etc/systemd/system/
systemctl daemon-reload
systemctl enable --now plusclouds-agent
```

```powershell
# Windows
New-Service -Name PlusCloudsAgent `
  -BinaryPathName '"C:\Program Files\PlusClouds\plusclouds-agent.exe"' `
  -DisplayName "PlusClouds VM Agent" `
  -StartupType Automatic
sc.exe failure PlusCloudsAgent reset= 86400 actions= restart/5000/restart/5000/restart/5000
Start-Service PlusCloudsAgent
```

#### 5. Verify

```bash
# Linux
journalctl -fu plusclouds-agent
# or
tail -f /var/log/plusclouds/agent.log | jq .
```

```powershell
# Windows
Get-Service PlusCloudsAgent
Get-Content "$env:ProgramData\PlusClouds\logs\agent.log" -Wait
```

You should see:
```
agent identity resolved   {"agent_uuid": "..."}
connected to NATS         {"url": "wss://nats.plusclouds.com:443"}
capabilities published    {"operation_count": 23}
heartbeat published
telemetry published
```

---

## Configuration reference

Configuration is layered: **built-in defaults** → the `agent` object inside `pc-meta-data.json` on the config-drive (or its local cache) → **environment variables** (`PLUSCLOUDS_AGENT_*`, e.g. `PLUSCLOUDS_AGENT_NATS_API_KEY`), each layer overriding the previous one. There is no config file — this is the shape of the `agent` object the platform writes into `pc-meta-data.json`:

```json
{
  "agent": {
    "nats": {
      "connection_type": "websocket",
      "url": "nats://nats.plusclouds.com:4222",
      "websocket_url": "wss://nats.plusclouds.com:443",
      "agent_uuid": "<vm-uuid>",
      "api_key": "<agent-api-key>",
      "max_reconnects": -1,
      "reconnect_wait": "5s"
    },
    "agent": {
      "heartbeat_interval": "30s",
      "telemetry_interval": "30s",
      "allowed_operations": [
        "agent.allowed_operations",
        "agent.version",
        "services.list",
        "services.get",
        "services.start",
        "services.stop",
        "services.restart",
        "services.reload",
        "services.enable",
        "services.disable",
        "system.info",
        "system.metrics",
        "system.cpu",
        "system.memory",
        "system.disk",
        "system.network",
        "system.update",
        "cron.list",
        "telemetry.set_interval",
        "sshkeys.list",
        "env.list"
      ],
      "allowed_commands": ["/usr/bin/journalctl", "/usr/bin/df", "/usr/bin/free"]
    },
    "iso": {
      "mount_path": "/media/plusclouds-config"
    },
    "log": {
      "level": "info",
      "format": "json",
      "file": "/var/log/plusclouds/agent.log"
    },
    "autoheal": {
      "enabled": true,
      "restart_delay": "10s",
      "services": ["nginx", "myapp"]
    }
  }
}
```

The top-level `virtual_machine_id`/`agent_api_key` fields of `pc-meta-data.json` (VM identity) always take precedence over `agent.nats.agent_uuid`/`agent.nats.api_key` if the two ever differ.

---

## Building from source

Requires Go 1.25+ (see `go.mod`).

```bash
# Development build (current OS)
make build

# Production build — Linux amd64, static binary, stripped
make build-linux

# Production build — Windows amd64
make build-windows

# Both platforms at once
make build-all

# Run tests
make test
```

Outputs:
```
bin/plusclouds.linux    — ELF 64-bit, statically linked, ~12 MB
bin/plusclouds.windows  — PE32+, ~12 MB
```

---

## Platform compatibility

| Feature | Linux | Windows |
|---|---|---|
| NATS connection | ✅ | ✅ |
| Telemetry (CPU, RAM, disk, network) | ✅ | ✅ (load_avg = 0) |
| Heartbeat | ✅ | ✅ |
| Capabilities event | ✅ | ✅ |
| Agent installed as a native service | ✅ systemd (`scripts/install.sh`) | ✅ SCM (`scripts/install.windows.ps1`) |
| Service management (other services on the VM) | ✅ systemd/D-Bus | ⚙ stub (SCM planned) |
| `system.update` | ✅ Ubuntu/Debian | ✗ |
| `vm.reboot` / `vm.shutdown` | ✅ `systemctl` | ✅ `shutdown /r` |
| `identity.set_password` | ✅ `chpasswd` (via stdin) | ✅ `net user` (password visible in argv — see Security model) |
| `network.configure` | ✅ netplan | ✅ PowerShell `New-NetIPAddress` etc. |
| `sshkeys.add` / `.remove` / `.list` | ✅ `~/.ssh/authorized_keys` | ✅ `%USERPROFILE%\.ssh\authorized_keys` (no admin ACL support) |
| `env.set` / `.unset` / `.list` | ✅ `/etc/environment` (new sessions only) | ✅ registry + `WM_SETTINGCHANGE` broadcast |

---

## Security model

- **Outbound-only** — the agent never listens on any port
- **Scoped JWT** — the NATS auth callout issues a JWT granting publish/subscribe only to this agent's own subjects
- **Operation allowlist** — `allowed_operations` in the config-drive's `agent` settings is a hard gate; unknown or unlisted operations return `rejected`
- **Exec allowlist** — when `exec` is enabled, only binaries explicitly listed in `allowed_commands` can be invoked
- **Token revocation** — remove `events_token` from the platform database and the agent is rejected on its next connection attempt
- **Password handling** — `identity.set_password`'s new password is piped to `chpasswd` over stdin on Linux, so it never appears in argv or in the command audit log. The Debug-level command-params log line redacts the `password` field for this operation specifically. On Windows, `net user` requires the password as a plain argument — it is briefly visible to anything enumerating process command lines (Task Manager, `Get-Process`, Sysmon) for the duration of that call; this is a known, accepted limitation, not mitigated in the current implementation
- **SSH key removal has no lockout guard** — `sshkeys.remove` will happily remove the last key in a user's `authorized_keys`; the agent has no way to know which key an operator depends on, and NATS control-plane access doesn't depend on SSH access anyway

---

## Support

Having trouble? We're here to help.

📧 **[support@plusclouds.com](mailto:support@plusclouds.com)**

For bug reports and feature requests, open an issue on GitHub.

---

## Our Libraries

This agent is part of the **PlusClouds open-source ecosystem** — precision infrastructure and intelligence tools built for SaaS companies and tech-forward businesses.

Browse all available libraries and building blocks:
[https://plusclouds.com/us/solutions/libraries](https://plusclouds.com/us/solutions/libraries)

---

## Join the Community

Great infrastructure is built together. The PlusClouds developer community is where engineers share ideas, ask questions, and help shape the direction of the platform. Whether you're integrating a single agent or building an entire infrastructure layer on our stack — you're welcome here.

[https://plusclouds.com/us/community](https://plusclouds.com/us/community)
