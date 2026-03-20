# Axon Agent Design

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
    │   2. Connect to server (gRPC TLS)
    │   3. Send RegisterRequest (token + node info)
    │   4. Receive RegisterResponse (node_id + heartbeat interval)
    │   5. Begin heartbeat loop
    │
    ├── Subsequent runs:
    │   1. Read config from ~/.axon-agent/config.yaml
    │   2. Connect to server
    │   3. Re-register (server recognizes returning node by node_id)
    │   4. Begin heartbeat loop
    │
    ▼
Running (waiting for tasks)
    ├── Heartbeat every N seconds (server-configured)
    ├── Node info reporting (periodic, or on change)
    ├── Task execution (on demand)
    │
    ├── Connection lost → exponential backoff reconnect
    │   Initial: 1s, max: 60s, jitter: ±20%
    │
    ▼
Stop
    ├── axon-agent stop → graceful shutdown
    │   - Complete in-flight tasks (with timeout)
    │   - Close gRPC streams
    │   - Exit
    └── kill -9 → server detects via heartbeat timeout → mark offline
```

## Config

Stored at `~/.axon-agent/config.yaml`:

```yaml
server: "axon.example.com:443"
token: "agent-token-xxx"
node_id: "a1b2c3d4"           # Assigned by server after first registration
node_name: "web-1"            # User-specified or hostname
labels:
  env: production
  role: web
```

- `token`: used for initial registration, validated by server
- `node_id`: persisted after first registration, used for reconnection identity
- `node_name`: if not specified, defaults to hostname
- `labels`: optional key-value pairs for node grouping

## Commands

### `axon-agent start`

Start the agent daemon.

```
$ axon-agent start --server axon.example.com:443 --token <token>
[INFO] connecting to axon.example.com:443...
[INFO] registered as node "web-1" (id: a1b2c3d4)
[INFO] heartbeat interval: 10s
[INFO] ready, waiting for tasks

# Subsequent starts (config already saved):
$ axon-agent start
[INFO] connecting to axon.example.com:443...
[INFO] reconnected as node "web-1" (id: a1b2c3d4)
[INFO] ready, waiting for tasks
```

- **Server**: ✅ required
- **Flags**:
  - `--server <address>` — server address (first run, saved to config)
  - `--token <token>` — agent token (first run, saved to config)
  - `--name <name>` — node name (optional, defaults to hostname)
  - `--labels key=value` — labels (repeatable)
  - `--foreground` — run in foreground (default: daemonize)

### `axon-agent stop`

Stop the agent daemon.

```
$ axon-agent stop
[INFO] shutting down...
[INFO] completing 2 in-flight tasks...
[INFO] stopped
```

- **Server**: ❌ local only
- **Behavior**: sends SIGTERM to daemon process, waits for graceful shutdown

### `axon-agent status`

Show agent status.

```
$ axon-agent status
Status:      running
Server:      axon.example.com:443
Connection:  connected
Node ID:     a1b2c3d4
Node Name:   web-1
Uptime:      3d 12h 5m
Last Heartbeat: 2s ago
Active Tasks: 0
```

- **Server**: local process check + connection health ping
- **Flags**:
  - `--json` — JSON output

### `axon-agent config set/get`

Manage local config.

```
$ axon-agent config set labels.env staging
$ axon-agent config get server
axon.example.com:443
```

- **Server**: ❌ local only

### `axon-agent version`

```
$ axon-agent version
axon-agent 0.1.0 (go1.22, linux/amd64)
```

- **Server**: ❌ local only

## Task Execution

Agent receives tasks via the control plane stream and opens data plane streams to execute them.

### Exec

```
1. Server sends TaskSignal{task_id, TASK_EXEC} via control stream
2. Agent opens data stream, receives ExecRequest{command, env, workdir, timeout}
3. Agent spawns local process:
   - os/exec.Command(shell, "-c", command)
   - Pipe stdout/stderr
   - Set environment variables
   - Set working directory
4. Stream stdout/stderr chunks back as ExecOutput messages
5. Process exits → send ExecExit{exit_code} → close stream
```

**Process management:**
- Each exec runs as a child process of the agent
- Agent inherits its user's permissions (no sandboxing in Phase 1)
- Timeout: agent kills the process with SIGTERM, then SIGKILL after 5s
- Cancellation: gRPC context cancel → SIGTERM → SIGKILL

### Read

```
1. Server sends TaskSignal{task_id, TASK_READ}
2. Agent opens data stream, receives ReadRequest{path}
3. Agent stat() the file → send ReadMeta{size, mode, mtime}
4. Open file, read in chunks (default 32KB) → send ReadOutput{data} per chunk
5. EOF → close stream
```

**Error cases:**
- File not found → gRPC NOT_FOUND
- Permission denied → gRPC PERMISSION_DENIED
- Is a directory → gRPC INVALID_ARGUMENT

### Write

```
1. Server sends TaskSignal{task_id, TASK_WRITE}
2. Agent opens data stream, receives WriteHeader{path, mode}
3. Create/truncate file with specified mode
4. Receive WriteInput{data} chunks → write to file
5. Client closes stream → agent responds WriteResponse{success, bytes_written}
```

**Behavior:**
- Creates parent directories if they don't exist
- Atomic write: write to temp file, then rename (prevents partial writes)
- Default mode: 0644

### Forward

```
1. Server sends TaskSignal{task_id, TASK_FORWARD}
2. Agent opens data stream, receives TunnelOpen{remote_port}
3. Agent dials localhost:<remote_port> via TCP
4. Bidirectional relay:
   - TunnelData{payload} from server → write to TCP connection
   - TCP data read → send TunnelData{payload} to server
5. Either side closes → send TunnelData{close} → clean up
```

**Error cases:**
- Cannot connect to target port → gRPC UNAVAILABLE with error detail
- Connection reset → TunnelData{close}

## Heartbeat & Node Info

### Heartbeat

- Agent sends `Heartbeat{timestamp}` every N seconds
- N is configured by server in `RegisterResponse.heartbeat_interval_seconds`
- Default: 10 seconds
- Server marks node offline if no heartbeat received within 3× interval (30s default)

### Node Info Reporting

Agent reports `NodeInfo` on:
1. Registration (initial report)
2. Periodic update (every 5 minutes, or configurable)
3. On significant change (IP change, etc.)

## Reconnection

On connection loss:

```
Attempt 1: wait 1s    → reconnect
Attempt 2: wait 2s    → reconnect
Attempt 3: wait 4s    → reconnect
...
Attempt N: wait min(2^N, 60)s ± 20% jitter → reconnect
```

On successful reconnect:
- Re-register with existing `node_id` (server recognizes returning node)
- Resume heartbeat
- In-flight tasks at disconnect time are considered failed

## Security

### Phase 1 (current)

- Agent runs as whatever user starts it
- No command filtering, no path restrictions
- All exec/read/write permissions = agent process user permissions
- Trust boundary: if you have a valid CLI token bound to a node, you can do anything the agent user can do

### Phase 2 (future)

- Command allowlist/denylist
- Path restrictions (allowed directories)
- Resource limits (CPU, memory, time per exec)

## System Service

Agent should run as a system service for production:

```bash
# systemd example
[Unit]
Description=Axon Agent
After=network.target

[Service]
ExecStart=/usr/local/bin/axon-agent start --foreground
Restart=always
RestartSec=5
User=axon

[Install]
WantedBy=multi-user.target
```

## Command Summary

| Command | Server | Description |
|---------|:------:|-------------|
| `start` | ✅ | Start daemon, connect & register |
| `stop` | ❌ | Stop daemon (graceful) |
| `status` | ⚠️ | Local + connection health |
| `config set/get` | ❌ | Local config |
| `version` | ❌ | Local version |
