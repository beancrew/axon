# Axon CLI Reference


## Overview

Axon has three binaries — `axon` (CLI), `axon-server`, and `axon-agent`. This page covers all commands across all three.

## `axon` — CLI for Humans and AI Agents

Reads local config (`~/.axon/config.yaml`) for server address and token, then talks to axon-server via gRPC.

### Global Flags

```
--ca-cert <path>     Path to CA certificate for TLS verification
```

### Config

Stored at `~/.axon/config.yaml`:

```yaml
server_addr: "axon.example.com:9090"
token: "eyJhbGciOiJIUzI1NiIs..."
output_format: "table"
ca_cert: "/path/to/ca.crt"
```

See [Configuration Reference](configuration.md) for all fields.

---

## Node Commands

### `axon node list`

List all registered nodes.

```
$ axon node list
NAME      STATUS   OS                    ARCH    IP             VERSION   LAST SEEN
web-1     online   Ubuntu 24.04 LTS      amd64   10.0.1.10      0.1.0     2s ago
db-1      online   CentOS 9              amd64   10.0.1.20      0.1.0     5s ago
edge-1    offline  Debian 12             arm64   192.168.1.50   0.1.0     2m ago
```

- **gRPC**: `ManagementService.ListNodes` (unary)
- **Flags**: `--json` — JSON output; `--status <online|offline>` — filter

### `axon node info <node>`

Show detailed info for a node.

```
$ axon node info web-1
Name:           web-1
Status:         online
OS:             Ubuntu 24.04 LTS (linux 6.8.0-45-generic)
Arch:           amd64
IP:             10.0.1.10
Agent Version:  0.1.0
Labels:
  env: production
  role: web
```

- **gRPC**: `ManagementService.GetNode` (unary)
- **Flags**: `--json`

### `axon node remove <node>`

Remove a node from the registry. Disconnects the agent if online.

```
$ axon node remove edge-1
Node "edge-1" removed.
```

- **gRPC**: `ManagementService.RemoveNode` (unary)

---

## Core Operations

### `axon exec <node> <command>`

Execute a command on a remote node. Stdout/stderr stream in real time.

```
$ axon exec web-1 "docker ps"
CONTAINER ID   IMAGE     STATUS
abc123         nginx     Up 2 hours

$ axon exec web-1 "tail -f /var/log/app.log"
[2026-03-25 11:25:00] request received...
^C
```

- **gRPC**: `OperationsService.Exec` (server stream)
- **Behavior**:
  - stdout → stdout, stderr → stderr (preserves stream separation)
  - Exit code forwarded: `axon exec` exits with the remote command's exit code
  - Ctrl+C sends cancellation to server (gRPC context cancel)
- **Flags**:
  - `--timeout <seconds>` — kill command after timeout (0 = no timeout)
  - `--env KEY=VALUE` — set environment variable (repeatable)
  - `--workdir <path>` — set working directory

### `axon read <node> <path>`

Read a file from a remote node. Content written to stdout.

```
$ axon read web-1 /etc/nginx/nginx.conf > local-copy.conf
```

- **gRPC**: `OperationsService.Read` (server stream)
- **Flags**: `--meta` — metadata only; `--json` — JSON metadata

### `axon write <node> <path>`

Write to a file on a remote node. Content read from stdin.

```
$ echo "hello" | axon write web-1 /tmp/hello.txt
Written 6 bytes to /tmp/hello.txt
```

- **gRPC**: `OperationsService.Write` (client stream)
- **Flags**: `--mode <perm>` — file permissions (default: 0644)

### `axon forward`

Manage port forwards to remote nodes. Supports subcommands for non-blocking, daemon-managed forwards.

### `axon forward create <node> <local-port>:<remote-port>`

Create a port forward (non-blocking, returns forward ID). Auto-starts a background daemon if not running.

```
$ axon forward create db-1 5432:5432
Forward f1a2b3c4 created: 127.0.0.1:5432 → db-1:5432
```

- **Flags**: `--bind <address>` — bind address (default: `127.0.0.1`)

### `axon forward list`

List active port forwards.

```
$ axon forward list
ID        NODE     LOCAL  REMOTE  STATUS   CREATED
f1a2b3c4  db-1     5432   5432    active   2m ago
d5e6f7a8  web-1    8080   80      active   5s ago
```

### `axon forward delete <forward-id>`

Delete a port forward.

```
$ axon forward delete f1a2b3c4
Forward f1a2b3c4 deleted
```

### `axon forward <node> <local-port>:<remote-port>` (shorthand)

Backward-compatible blocking mode. Listens on a local port and forwards TCP connections to a remote port.

```
$ axon forward db-1 5432:5432
Forwarding localhost:5432 → db-1:5432
Ready. Press Ctrl+C to stop.
```

- **gRPC**: `OperationsService.Forward` (BiDi stream, one per TCP connection)
- **Flags**: `--bind <address>` — bind address (default: `127.0.0.1`)

---

## Token Commands

### `axon token list`

List all active (non-revoked) tokens.

```
$ axon token list
550e8400-e29b-41d4-a716-446655440000  cli     admin         expires=2026-03-26T10:00:00+08:00
a1b2c3d4-0000-0000-0000-000000000000  agent   web-1         expires=never
```

- **gRPC**: `ManagementService.ListTokens` (unary)
- **Flags**: `--kind <cli|agent>` — filter by token kind

### `axon token revoke <token-id>`

Revoke a token by its JTI.

```
$ axon token revoke 550e8400-e29b-41d4-a716-446655440000
Token revoked successfully.
```

- **gRPC**: `ManagementService.RevokeToken` (unary)

### `axon token create-join`

Create a new join token for agent enrollment.

```
$ axon token create-join --max-uses 10 --expires 24h
Join token created (ID: a1b2c3d4)

   axon-join-abcdef1234567890...

Enroll a node:
   axon-agent join <SERVER_ADDR> axon-join-abcdef1234567890...
```

- **gRPC**: `ManagementService.CreateJoinToken` (unary)
- **Flags**:
  - `--max-uses <n>` — maximum number of uses (0 = unlimited, default)
  - `--expires <duration>` — expiry duration (e.g. `24h`, `168h`)

### `axon token list-join`

List all join tokens with usage and status.

```
$ axon token list-join
ID        USES  MAX  REVOKED  EXPIRES                    CREATED
a1b2c3d4  3     10   no       2026-03-26T18:00:00+08:00  2026-03-25T18:00:00+08:00
e5f6g7h8  0     inf  no       never                      2026-03-25T10:00:00+08:00
```

- **gRPC**: `ManagementService.ListJoinTokens` (unary)

### `axon token revoke-join <token-id>`

Revoke a join token by its short ID.

```
$ axon token revoke-join a1b2c3d4
Join token revoked.
```

- **gRPC**: `ManagementService.RevokeJoinToken` (unary)

---

## Config Commands

### `axon config set <key> <value>`

```
$ axon config set server axon.example.com:9090
```

Supported keys: `server`, `token`, `output_format`

### `axon config get <key>`

```
$ axon config get server
axon.example.com:9090
```

### `axon version`

```
$ axon version
axon 0.1.0 (go1.25, darwin/arm64)
```

---

## `axon-server` — Server Commands

### `axon-server init`

Initialize server configuration. Creates config file, JWT secret, admin token, SQLite database, and an initial join token.

```
$ axon-server init
Server initialized

   Config:     ~/.axon-server/config.yaml
   Database:   ~/.axon-server/axon.db
   Listen:     :9090

Admin token (save this):
   eyJhbGciOiJIUzI1NiIs...

Start the server:
   axon-server start --config ~/.axon-server/config.yaml

Join a node:
   axon-agent join <SERVER_IP>:9090 axon-join-ab12cd34...
```

- **Flags**:
  - `--listen <addr>` — gRPC listen address (default: `:9090`)
  - `--data-dir <path>` — data directory (default: `~/.axon-server`)
  - `--tls` — enable auto-TLS
  - `--force` — overwrite existing config

### `axon-server start`

Start the server.

```
$ axon-server start --config ~/.axon-server/config.yaml
```

- **Flags**: `--config <path>` — config file path

### `axon-server version`

```
$ axon-server version
axon-server 0.1.0 (go1.25, darwin/arm64)
```

---

## `axon-agent` — Agent Commands

### `axon-agent join <server-addr> <join-token>`

Enroll this machine as an agent node. Validates the join token, receives an agent JWT, saves config, and starts the agent.

```
$ axon-agent join 10.0.1.1:9090 axon-join-ab12cd34... --tls-insecure
Node enrolled successfully

   Node ID:    a1b2c3d4-...
   Node Name:  my-node
   Server:     10.0.1.1:9090
   Config:     ~/.axon-agent/config.yaml

Starting agent... (Ctrl+C to stop)
```

- **Flags**:
  - `--name <name>` — node name (default: hostname)
  - `--labels <key=value>` — labels (repeatable)
  - `--ca-cert <path>` — CA certificate for TLS verification
  - `--tls-insecure` — skip TLS (for servers without TLS)

### `axon-agent start`

Reconnect an already-enrolled agent using saved config.

```
$ axon-agent start
```

- **Flags**: `--config <path>` — config file path (default: `~/.axon-agent/config.yaml`)

### `axon-agent stop`

Stop the agent daemon.

### `axon-agent status`

Show agent status (connected/disconnected, node ID, server address).

### `axon-agent version`

```
$ axon-agent version
axon-agent 0.1.0 (go1.25, darwin/arm64)
```

---

## Command Summary

### `axon` CLI

| Command | Server | gRPC Mode | Auth |
|---------|:------:|-----------|:----:|
| `node list` | ✅ | unary | ✅ |
| `node info` | ✅ | unary | ✅ |
| `node remove` | ✅ | unary | ✅ |
| `exec` | ✅ | server stream | ✅ |
| `read` | ✅ | server stream | ✅ |
| `write` | ✅ | client stream | ✅ |
| `forward create` | ✅ | bidi stream | ✅ |
| `forward list` | ❌ | — | — |
| `forward delete` | ❌ | — | — |
| `forward` (shorthand) | ✅ | bidi stream | ✅ |
| `token list` | ✅ | unary | ✅ |
| `token revoke` | ✅ | unary | ✅ |
| `token create-join` | ✅ | unary | ✅ |
| `token list-join` | ✅ | unary | ✅ |
| `token revoke-join` | ✅ | unary | ✅ |
| `config set/get` | ❌ | — | — |
| `version` | ❌ | — | — |

### `axon-server`

| Command | Description |
|---------|-------------|
| `init` | Initialize config, DB, admin token, join token |
| `start` | Start the gRPC server |
| `version` | Show version |

### `axon-agent`

| Command | Description |
|---------|-------------|
| `join` | Enroll with a server using a join token |
| `start` | Reconnect using saved config |
| `stop` | Stop the agent |
| `status` | Show agent status |
| `version` | Show version |

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | General error (connection failed, server error) |
| 2 | Auth error (invalid/expired token) |
| 3 | Node error (not found, offline) |
| N | For `exec`: remote command's exit code is forwarded |
