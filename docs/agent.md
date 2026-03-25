# Axon Agent Design

> [中文版 / Chinese](zh/agent.md)

## Overview

`axon-agent` is a lightweight daemon installed on each target machine. It reverse-connects to axon-server via gRPC, registers itself, maintains a heartbeat, and responds to tasks dispatched by the server.

**The agent never initiates actions. It only responds to server-dispatched tasks.**

## Agent Lifecycle

```
Install binary
    │
    ▼
axon-agent start --server <addr> --token <token> [--name <name>]
    │
    ├── First run:
    │   1. Save config to ~/.axon-agent/config.yaml
    │   2. Connect to server (gRPC + TLS)
    │   3. Send RegisterRequest (token + node_name + NodeInfo)
    │   4. Receive RegisterResponse (node_id + heartbeat interval)
    │   5. Save node_id to config (stable identity)
    │   6. Begin heartbeat loop
    │
    ├── Subsequent runs:
    │   1. Read config from ~/.axon-agent/config.yaml
    │   2. Connect to server
    │   3. Re-register with existing node_id (server recognizes returning node)
    │   4. Begin heartbeat loop
    │
    ▼
Running (waiting for tasks)
    ├── Heartbeat every N seconds (server-configured)
    ├── Node info reporting (periodic)
    ├── Task execution (on demand)
    │
    ├── Connection lost → exponential backoff reconnect
    │   Initial: 1s, max: 60s, jitter: ±20%
    │
    ▼
Stop → graceful shutdown → complete in-flight tasks → exit
```

## Config

Stored at `~/.axon-agent/config.yaml`:

```yaml
server: "axon.example.com:9090"
token: "agent-token-xxx"
node_id: "a1b2c3d4"           # assigned by server after first registration
node_name: "web-1"            # user-specified or hostname
labels:
  env: production
  role: web
ca_cert: "/path/to/ca.crt"    # CA certificate for TLS verification
tls_insecure: false            # skip TLS verification (dev only)
```

See [Configuration Reference](configuration.md) for all fields and environment variables.

## TLS

The agent uses a 3-way TLS selection:

| Priority | Condition | Behavior |
|----------|-----------|----------|
| 1 | `tls_insecure: true` | No TLS verification (plaintext gRPC) |
| 2 | `ca_cert` set | Verify server cert against specified CA file |
| 3 | Neither | Verify server cert against system CA pool |

**For auto-TLS setups:** Copy the server's `~/.axon-server/tls/ca.crt` to the agent machine and set `ca_cert` in config or pass `--ca-cert` flag.

## Commands

### `axon-agent start`

```
$ axon-agent start --server axon.example.com:9090 --token <token> --ca-cert /path/to/ca.crt
[INFO] connecting to axon.example.com:9090...
[INFO] registered as node "web-1" (id: a1b2c3d4)
[INFO] heartbeat interval: 10s
[INFO] ready, waiting for tasks
```

- **Flags**:
  - `--server <address>` — server address (saved to config on first run)
  - `--token <token>` — agent token (saved to config)
  - `--name <name>` — node name (optional, defaults to hostname)
  - `--labels key=value` — labels (repeatable)
  - `--ca-cert <path>` — CA certificate for TLS verification
  - `--tls-insecure` — skip TLS verification (dev only)
  - `--foreground` — run in foreground

### `axon-agent stop`

Stop the agent daemon gracefully.

### `axon-agent status`

Show agent status (running, connection, node info).

### `axon-agent version`

Print version info.

## Task Execution

Agent receives tasks via the control plane stream and opens data plane streams to execute them.

### Exec

```
1. Server sends TaskSignal{task_id, TASK_EXEC} via control stream
2. Agent opens HandleTask stream, receives ExecRequest
3. Agent spawns local process (os/exec.Command)
4. Stream stdout/stderr chunks as ExecOutput messages
5. Process exits → send ExecExit{exit_code} → close stream
```

- Each exec runs as a child process of the agent
- Agent inherits its user's permissions
- Timeout: SIGTERM → wait 5s → SIGKILL
- Cancellation: gRPC context cancel → SIGTERM → SIGKILL

### Read

```
1. TaskSignal → agent opens HandleTask stream
2. Receives ReadRequest{path}
3. stat() → send ReadMeta{size, mode, mtime}
4. Read in chunks (32KB) → send ReadOutput{data}
5. EOF → close stream
```

### Write

```
1. TaskSignal → agent opens HandleTask stream
2. Receives WriteHeader{path, mode}
3. Create/truncate file → receive WriteInput{data} chunks
4. Complete → send WriteResponse{success, bytes_written}
```

- Creates parent directories if needed
- Atomic write: temp file + rename

### Forward

```
1. TaskSignal → agent opens HandleTask stream
2. Receives TunnelOpen{remote_port}
3. Dial localhost:<remote_port> via TCP
4. Bidirectional relay: TunnelData ↔ TCP socket
5. Either side closes → TunnelData{close} → cleanup
```

## Heartbeat & Node Info

### Heartbeat

- Interval configured by server in `RegisterResponse.heartbeat_interval_seconds`
- Default: 10s
- Server marks offline if no heartbeat within timeout (default 30s)

### Node Info

Agent reports `NodeInfo` (hostname, arch, IP, uptime, agent version, OSInfo) on:
1. Registration (initial report)
2. Periodic update

OS info is collected per-platform:
- **Linux**: `/etc/os-release` + `uname`
- **macOS**: `sw_vers` + `uname`
- **Windows**: `RtlGetVersion`

## Reconnection

```
Attempt 1: wait 1s    → reconnect
Attempt 2: wait 2s    → reconnect
Attempt 3: wait 4s    → reconnect
...
Attempt N: wait min(2^N, 60)s ± 20% jitter → reconnect
```

On successful reconnect:
- Re-register with existing `node_id`
- Resume heartbeat
- In-flight tasks at disconnect time are considered failed

## System Service

```ini
# systemd example
[Unit]
Description=Axon Agent
After=network.target

[Service]
ExecStart=/usr/local/bin/axon-agent start --foreground
Restart=always
RestartSec=5
User=axon
Environment=AXON_CA_CERT=/etc/axon-agent/ca.crt

[Install]
WantedBy=multi-user.target
```
