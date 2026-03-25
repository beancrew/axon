# Quick Start Guide

Get Axon running in under 5 minutes: a server, an agent, and your first remote command.

> [中文版 / Chinese](zh/quickstart.md)

## Prerequisites

- Go 1.25+ installed (or download pre-built binaries — see [Install Script](#install-script))
- Two machines (or two terminals on the same machine for testing)

## 1. Build

```bash
git clone https://github.com/garysng/axon.git
cd axon
make build
```

This produces three binaries in `bin/`:

| Binary | Purpose |
|--------|---------|
| `axon` | CLI for humans and AI agents |
| `axon-server` | Central control plane |
| `axon-agent` | Daemon on each target machine |

## 2. Initialize the Server

One command sets up everything — config, JWT secret, admin user, and a join token:

```bash
./bin/axon-server init --admin admin --password your-secret-password
```

Output:

```
Server initialized

   Config:     ~/.axon-server/config.yaml
   Database:   ~/.axon-server/axon.db
   Listen:     :9090
   Admin user: admin

Start the server:
   axon-server start --config ~/.axon-server/config.yaml

Join a node:
   axon-agent join <SERVER_IP>:9090 axon-join-ab12cd34...

Use CLI:
   axon config set server <SERVER_IP>:9090
   axon auth login
```

**Save the join token** — you'll need it to enroll agents.

### `init` Options

| Flag | Default | Description |
|------|---------|-------------|
| `--listen` | `:9090` | gRPC listen address |
| `--admin` | `admin` | Admin username |
| `--password` | *(prompted)* | Admin password (interactive if omitted) |
| `--data-dir` | `~/.axon-server` | Data directory for config, DB, and certs |
| `--tls` | `false` | Enable auto-TLS (self-signed CA + server cert) |
| `--force` | `false` | Overwrite existing config |

## 3. Start the Server

```bash
./bin/axon-server start --config ~/.axon-server/config.yaml
```

```
server: gRPC listening on :9090
```

> **Note:** TLS is disabled by default — connections use plaintext gRPC, suitable for internal/private networks. Enable TLS with `--tls` during init or set `tls.auto: true` in config. See [TLS Options](#tls-options).

## 4. Join an Agent

On the target machine, one command enrolls the node:

```bash
./bin/axon-agent join <SERVER_IP>:9090 axon-join-ab12cd34... --tls-insecure
```

> Use `--tls-insecure` when connecting to a server without TLS. If TLS is enabled, omit this flag or use `--ca-cert` instead.

Output:

```
Node enrolled successfully

   Node ID:    a1b2c3d4-...
   Node Name:  my-node
   Server:     10.0.1.1:9090
   Config:     ~/.axon-agent/config.yaml

Starting agent... (Ctrl+C to stop)
```

This does everything in one step:
- Validates the join token with the server
- Receives a persistent agent JWT
- Saves config to `~/.axon-agent/config.yaml`
- Starts the agent control-plane loop

### `join` Options

| Flag | Default | Description |
|------|---------|-------------|
| `--name` | hostname | Node display name |
| `--labels` | — | Labels as `key=value` (repeatable) |
| `--ca-cert` | — | CA certificate path for TLS verification |
| `--tls-insecure` | `false` | Skip TLS (for servers without TLS) |

### Reconnecting

After the first join, reconnect with:

```bash
./bin/axon-agent start
```

The agent reads its saved config (`~/.axon-agent/config.yaml`) automatically.

## 5. CLI Login

```bash
# Point CLI at the server
./bin/axon config set server <SERVER_IP>:9090

# Login (prompts for username/password)
./bin/axon auth login
```

## 6. Run Commands

```bash
# List connected nodes
axon node list

# Execute a command on a remote node
axon exec my-node "hostname"
axon exec my-node "ls -la /tmp"

# Read a file
axon read my-node /etc/hostname

# Write a file
echo "hello from axon" | axon write my-node /tmp/hello.txt

# Port forward
axon forward my-node 8080:80
# Now localhost:8080 → my-node:80
```

## 7. Manage Join Tokens

```bash
# Create a new join token (with optional limits)
axon token create-join --max-uses 10 --expires 24h

# List all join tokens
axon token list-join

# Revoke a token
axon token revoke-join <token-id>
```

## 8. Manage Users (Optional)

```bash
# Create a new user
axon user create deploy-bot --node-ids web-1,web-2

# List users
axon user list

# Update node access
axon user update deploy-bot --node-ids web-1,web-2,db-1

# Delete a user
axon user delete deploy-bot
```

## TLS Options

TLS is **disabled by default** — plaintext gRPC is suitable for internal/private networks.

| Scenario | Server Setup | Client/Agent Flag |
|----------|-------------|-------------------|
| No TLS (default) | `axon-server init` (no `--tls`) | `--tls-insecure` (for `axon-agent join`) |
| Auto-TLS | `axon-server init --tls` | `--ca-cert ~/.axon-server/tls/ca.crt` |
| Bring your own cert | `tls.cert` + `tls.key` in config | System CA pool or `--ca-cert` |

When auto-TLS is enabled, the server generates a self-signed CA and server certificate (ECDSA P-256). The CA cert is automatically provided to agents during `join`.

## Install Script

Download pre-built binaries from GitHub Releases:

```bash
# Install the server
curl -fsSL https://axon.dev/install | sh -s -- server

# Install the agent
curl -fsSL https://axon.dev/install | sh -s -- agent

# Install the CLI
curl -fsSL https://axon.dev/install | sh -s -- cli
```

The script auto-detects OS/architecture and installs to `/usr/local/bin` (or `~/.axon/bin` if no root access).

## What's Next?

- [Configuration Reference](configuration.md) — all config options for server, agent, and CLI
- [Architecture Overview](architecture.md) — how the components fit together
- [CLI Reference](cli.md) — full command reference
