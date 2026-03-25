# Quick Start Guide

Get Axon running in under 5 minutes: a server, an agent, and your first remote command.

> [中文版 / Chinese](zh/quickstart.md)

## Prerequisites

- Go 1.25+ installed
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

## 2. Start the Server

Create a minimal config file:

```yaml
# server.yaml
listen: ":9090"

auth:
  jwt_signing_key: "your-secret-key-change-me"

users:
  - username: admin
    password_hash: "$2a$10$..."   # bcrypt hash of your password
    node_ids: ["*"]               # access to all nodes
```

Generate a bcrypt hash for your password:

```bash
# Using htpasswd (Apache utils)
htpasswd -nbBC 10 "" your-password | cut -d: -f2

# Or using Python
python3 -c "import bcrypt; print(bcrypt.hashpw(b'your-password', bcrypt.gensalt()).decode())"
```

Start the server:

```bash
./bin/axon-server --config server.yaml
```

You should see:

```
server: auto-TLS: generated CA cert ~/.axon-server/tls/ca.crt (SHA-256: AA:BB:CC:...)
server: gRPC listening on :9090 (TLS)
```

**Auto-TLS** generates a self-signed CA and server certificate automatically. No manual cert setup needed for getting started.

## 3. Connect an Agent

On the target machine (or another terminal), start the agent:

```bash
./bin/axon-agent start \
  --server localhost:9090 \
  --token your-agent-token \
  --name my-node \
  --tls-insecure    # for testing with self-signed certs
```

Or use the CA certificate for proper TLS verification:

```bash
./bin/axon-agent start \
  --server localhost:9090 \
  --token your-agent-token \
  --name my-node \
  --ca-cert ~/.axon-server/tls/ca.crt
```

The agent connects to the server and registers itself.

## 4. CLI Login

```bash
# Point CLI at the server
./bin/axon config set server localhost:9090

# Login (prompts for username/password)
./bin/axon auth login --tls-insecure
```

Or with the CA certificate:

```bash
./bin/axon auth login --ca-cert ~/.axon-server/tls/ca.crt
```

## 5. Run Commands

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

## 6. Manage Users (Optional)

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

## 7. Manage Tokens (Optional)

```bash
# List issued tokens
axon auth list-tokens

# Revoke a token
axon auth revoke <token-id>
```

## TLS Options

| Scenario | Server Config | Client/Agent Flag |
|----------|--------------|-------------------|
| Auto-TLS (default) | No `tls.cert`/`tls.key` → auto-generates | `--ca-cert ~/.axon-server/tls/ca.crt` |
| Bring your own cert | `tls.cert` + `tls.key` in config | System CA pool or `--ca-cert` |
| Disable TLS (dev only) | `tls.auto: false` (no cert/key) | `--tls-insecure` |

## What's Next?

- [Configuration Reference](configuration.md) — all config options for server, agent, and CLI
- [Architecture Overview](architecture.md) — how the components fit together
- [CLI Reference](cli.md) — full command reference
