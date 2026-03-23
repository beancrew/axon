# Axon CLI Design

## Overview

`axon` is a single stateless binary. It reads local config for server address and token, then talks to axon-server via gRPC.

## Config

Stored at `~/.axon/config.yaml`:

```yaml
server: "axon.example.com:443"
token: "eyJhbGciOiJIUzI1NiIs..."
```

## Commands

### `axon node list`

List all registered nodes.

```
$ axon node list
NAME      STATUS   OS                    ARCH    IP             VERSION   LAST SEEN
web-1     online   Ubuntu 24.04 LTS      amd64   10.0.1.10      0.1.0     2s ago
db-1      online   CentOS 9              amd64   10.0.1.20      0.1.0     5s ago
edge-1    offline  Debian 12             arm64   192.168.1.50   0.1.0     2m ago
```

- **Server**: ✅ required
- **gRPC**: `ManagementService.ListNodes` (unary)
- **Auth**: JWT token in gRPC metadata
- **Flags**:
  - `--json` — JSON output
  - `--status <online|offline>` — filter by status

### `axon node info <node>`

Show detailed info for a node.

```
$ axon node info web-1
Name:           web-1
Status:         online
OS:             Ubuntu 24.04 LTS (linux 6.8.0-45-generic)
Arch:           amd64
IP:             10.0.1.10
Uptime:         3d 12h 5m
Agent Version:  0.1.0
Connected:      2026-03-17 23:15:00 UTC
Last Heartbeat: 2026-03-20 11:24:58 UTC
Labels:
  env: production
  role: web
```

- **Server**: ✅ required
- **gRPC**: `ManagementService.GetNode` (unary)
- **Flags**:
  - `--json` — JSON output

### `axon node remove <node>`

Remove a node from the registry. Disconnects the agent if online.

```
$ axon node remove edge-1
Node "edge-1" removed.
```

- **Server**: ✅ required
- **gRPC**: `ManagementService.RemoveNode` (unary)
- **Flags**: none

### `axon exec <node> <command>`

Execute a command on a remote node. Stdout/stderr stream in real time.

```
$ axon exec web-1 "docker ps"
CONTAINER ID   IMAGE     STATUS
abc123         nginx     Up 2 hours

$ axon exec web-1 "tail -f /var/log/app.log"
[2026-03-20 11:25:00] request received...
[2026-03-20 11:25:01] processing...
^C
```

- **Server**: ✅ required
- **gRPC**: `OperationsService.Exec` (server stream)
- **Behavior**:
  - stdout → stdout, stderr → stderr (preserves stream separation)
  - Exit code forwarded: `axon exec` exits with the remote command's exit code
  - Ctrl+C sends cancellation to server (gRPC context cancel)
  - Long-running commands stream until completion or cancellation
- **Flags**:
  - `--timeout <seconds>` — kill command after timeout (0 = no timeout)
  - `--env KEY=VALUE` — set environment variable (repeatable)
  - `--workdir <path>` — set working directory

### `axon read <node> <path>`

Read a file from a remote node. Content written to stdout.

```
$ axon read web-1 /etc/nginx/nginx.conf
worker_processes auto;
events { ... }
...

$ axon read web-1 /etc/nginx/nginx.conf > local-copy.conf
```

- **Server**: ✅ required
- **gRPC**: `OperationsService.Read` (server stream)
- **Behavior**:
  - First message: file metadata (size, permissions, modified time)
  - Subsequent messages: file content in chunks
  - Binary files supported (raw bytes to stdout)
- **Flags**:
  - `--meta` — print file metadata only (size, mode, mtime)
  - `--json` — output metadata as JSON

### `axon write <node> <path>`

Write to a file on a remote node. Content read from stdin.

```
$ axon write web-1 /etc/nginx/nginx.conf < nginx.conf
Written 2048 bytes to /etc/nginx/nginx.conf

$ echo "hello" | axon write web-1 /tmp/hello.txt
Written 6 bytes to /tmp/hello.txt

$ cat backup.sql | axon write db-1 /tmp/restore.sql
Written 15728640 bytes to /tmp/restore.sql
```

- **Server**: ✅ required
- **gRPC**: `OperationsService.Write` (client stream)
- **Behavior**:
  - First message: header (path, file mode)
  - Subsequent messages: file content in chunks
  - Large files streamed without loading into memory
- **Flags**:
  - `--mode <perm>` — file permissions (default: 0644)

### `axon forward <node> <local-port>:<remote-port>`

Forward a remote port to localhost.

```
$ axon forward db-1 5432:5432
Forwarding localhost:5432 → db-1:5432
Ready. Press Ctrl+C to stop.

# In another terminal:
$ psql -h localhost -p 5432 -U postgres
```

- **Server**: ✅ required
- **gRPC**: `OperationsService.Forward` (BiDi stream, one per TCP connection)
- **Behavior**:
  - CLI listens on `localhost:<local-port>`
  - Each incoming TCP connection → new gRPC bidi stream
  - Raw TCP bytes wrapped in `TunnelData` protobuf messages
  - Bidirectional: data flows both ways until either side closes
  - Ctrl+C stops the listener and closes all tunnels
- **Flags**:
  - `--bind <address>` — bind address (default: `127.0.0.1`)

### `axon auth login`

Authenticate with the server and obtain a JWT token.

```
$ axon auth login --server axon.example.com:443
Username: gary
Password: ****
Login successful. Token saved to ~/.axon/config.yaml
```

- **Server**: ✅ required
- **gRPC**: `ManagementService.Login` (unary)
- **Behavior**:
  - Prompts for username/password (Phase 1)
  - Saves token + server address to `~/.axon/config.yaml`
- **Flags**:
  - `--server <address>` — server address (saved to config)

### `axon auth token`

Display the current token.

```
$ axon auth token
eyJhbGciOiJIUzI1NiIs...
```

- **Server**: ❌ local only
- **Behavior**: reads and prints token from `~/.axon/config.yaml`

### `axon config set <key> <value>`

Set a config value.

```
$ axon config set server axon.example.com:443
```

- **Server**: ❌ local only
- **Supported keys**: `server`, `token`

### `axon config get <key>`

Get a config value.

```
$ axon config get server
axon.example.com:443
```

- **Server**: ❌ local only

### `axon version`

Print version info.

```
$ axon version
axon 0.1.0 (go1.22, linux/amd64)
```

- **Server**: ❌ local only
- Built-in at compile time via `-ldflags`

## Command Summary

| Command | Server | gRPC Mode | Auth |
|---------|:------:|-----------|:----:|
| `node list` | ✅ | unary | ✅ |
| `node info` | ✅ | unary | ✅ |
| `node remove` | ✅ | unary | ✅ |
| `exec` | ✅ | server stream | ✅ |
| `read` | ✅ | server stream | ✅ |
| `write` | ✅ | client stream | ✅ |
| `forward` | ✅ | bidi stream | ✅ |
| `auth login` | ✅ | unary | ❌ (obtaining token) |
| `auth token` | ❌ | — | — |
| `config set/get` | ❌ | — | — |
| `version` | ❌ | — | — |

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | General error (connection failed, server error) |
| 2 | Auth error (invalid/expired token) |
| 3 | Node error (not found, offline) |
| N | For `exec`: remote command's exit code is forwarded |
