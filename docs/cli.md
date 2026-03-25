# Axon CLI Reference

> [中文版 / Chinese](zh/cli.md)

## Overview

`axon` is a single stateless binary. It reads local config (`~/.axon/config.yaml`) for server address and token, then talks to axon-server via gRPC.

## Global Flags

```
--ca-cert <path>     Path to CA certificate for TLS verification
--tls-insecure       Skip TLS certificate verification (dev only)
```

These override values from the config file.

## Config

Stored at `~/.axon/config.yaml`:

```yaml
server_addr: "axon.example.com:9090"
token: "eyJhbGciOiJIUzI1NiIs..."
output_format: "table"
ca_cert: "/path/to/ca.crt"
tls_insecure: false
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

### `axon forward <node> <local-port>:<remote-port>`

Forward a remote port to localhost.

```
$ axon forward db-1 5432:5432
Forwarding localhost:5432 → db-1:5432
Ready. Press Ctrl+C to stop.
```

- **gRPC**: `OperationsService.Forward` (BiDi stream, one per TCP connection)
- **Flags**: `--bind <address>` — bind address (default: `127.0.0.1`)

---

## Auth Commands

### `axon auth login`

Authenticate and obtain a JWT token.

```
$ axon auth login --server axon.example.com:9090
Username: gary
Password: ****
Login successful. Token saved.
```

- **gRPC**: `ManagementService.Login` (unary, no auth required)
- **Flags**: `--server <address>` — server address (saved to config)

### `axon auth token`

Display the current token.

```
$ axon auth token
eyJhbGciOiJIUzI1NiIs...
```

### `axon auth list-tokens`

List all issued tokens.

```
$ axon auth list-tokens
ID                                    KIND   USER    ISSUED              EXPIRES
550e8400-e29b-41d4-a716-446655440000  cli    gary    2026-03-25 10:00    2026-03-26 10:00
```

- **gRPC**: `ManagementService.ListTokens` (unary)

### `axon auth revoke <token-id>`

Revoke a token by its JTI.

```
$ axon auth revoke 550e8400-e29b-41d4-a716-446655440000
Token revoked.
```

- **gRPC**: `ManagementService.RevokeToken` (unary)

### `axon auth rotate`

*Not yet implemented.* Placeholder for revoke current + re-login.

---

## User Commands

### `axon user create <username>`

Create a new CLI user. Prompts for password.

```
$ axon user create deploy-bot --node-ids web-1,web-2
Password: ****
User "deploy-bot" created.
```

- **gRPC**: `ManagementService.CreateUser` (unary)
- **Flags**: `--node-ids <ids>` — comma-separated allowed node IDs (default: `*`)

### `axon user list`

List all users.

```
$ axon user list
USERNAME      NODE IDS      DISABLED   CREATED
admin         *             no         2026-03-25 09:00:00
deploy-bot    web-1,web-2   no         2026-03-25 10:30:00
```

- **gRPC**: `ManagementService.ListUsers` (unary)

### `axon user update <username>`

Update a user's node IDs or password.

```
$ axon user update deploy-bot --node-ids web-1,web-2,db-1
User "deploy-bot" updated.

$ axon user update deploy-bot --password
New password: ****
User "deploy-bot" updated.
```

- **gRPC**: `ManagementService.UpdateUser` (unary)
- **Flags**: `--node-ids <ids>`; `--password` — prompt for new password

### `axon user delete <username>`

Delete a user. Prompts for confirmation.

```
$ axon user delete deploy-bot
Are you sure you want to delete user "deploy-bot"? (y/N): y
User "deploy-bot" deleted.
```

- **gRPC**: `ManagementService.DeleteUser` (unary)
- **Flags**: `--force` / `-f` — skip confirmation

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
| `auth login` | ✅ | unary | ❌ |
| `auth token` | ❌ | — | — |
| `auth list-tokens` | ✅ | unary | ✅ |
| `auth revoke` | ✅ | unary | ✅ |
| `auth rotate` | — | — | — |
| `user create` | ✅ | unary | ✅ |
| `user list` | ✅ | unary | ✅ |
| `user update` | ✅ | unary | ✅ |
| `user delete` | ✅ | unary | ✅ |
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
