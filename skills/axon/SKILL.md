---
name: axon
description: >
  Manage remote machines via Axon — exec commands, read/write files, forward ports,
  manage nodes, users, tokens, and server/agent lifecycle.
  Use when: (1) running commands on remote nodes, (2) reading/writing remote files,
  (3) port forwarding, (4) listing/inspecting nodes, (5) managing users or tokens,
  (6) deploying or updating axon-server/axon-agent, (7) troubleshooting agent connectivity.
  NOT for: local-only operations, CI/CD pipeline config, or tasks unrelated to remote machine management.
---

# Axon

Axon connects AI agents and humans to remote machines. Three binaries: `axon` (CLI), `axon-server` (control plane), `axon-agent` (target daemon).

## Architecture

```
axon CLI ──gRPC──▶ axon-server ◀──reverse gRPC── axon-agent (on target)
```

- Agent connects **to** server (reverse connection, NAT-friendly)
- All communication is gRPC over HTTP/2
- Auth: JWT tokens, optional TLS (self-signed or BYO)

## Install / Update

```bash
# From GitHub Releases (auto-detect OS/arch)
curl -fsSL https://raw.githubusercontent.com/beancrew/axon/main/scripts/install.sh | sh -s -- cli
curl -fsSL https://raw.githubusercontent.com/beancrew/axon/main/scripts/install.sh | sh -s -- server
curl -fsSL https://raw.githubusercontent.com/beancrew/axon/main/scripts/install.sh | sh -s -- agent
```

## Quick Setup

### Server

```bash
axon-server init --admin admin --password <pass>
axon-server start --config ~/.axon-server/config.yaml
# Or daemon mode:
axon-server start --config ~/.axon-server/config.yaml --daemon
axon-server stop
```

Save the **join token** from init output — needed for agent enrollment.

### Agent

```bash
axon-agent join <server-addr>:9090 <join-token> --tls-insecure
# Reconnect later:
axon-agent start
```

### CLI

```bash
axon config set server <server-addr>:9090
axon config set token <admin-token>
```

## Core Commands

### Remote Execution

```bash
axon exec <node> "<command>"
axon exec web-1 "docker ps"
axon exec web-1 "tail -f /var/log/app.log"  # streams until Ctrl+C
```

Flags: `--timeout <sec>`, `--env KEY=VALUE`, `--workdir <path>`

Exit code is forwarded from remote command.

### File Operations

```bash
# Read remote file to stdout
axon read <node> /path/to/file > local.txt

# Write stdin to remote file
echo "content" | axon write <node> /tmp/file.txt
cat local.conf | axon write <node> /etc/app/config.conf --mode 0644
```

### Port Forwarding

```bash
# Non-blocking (daemon mode, v0.1.7+)
axon forward create <node> <local>:<remote>
axon forward list
axon forward delete <forward-id>

# Blocking shorthand (backward compat)
axon forward <node> <local>:<remote>
```

Flags: `--bind <addr>` (default `127.0.0.1`)

### Node Management

```bash
axon node list                    # list all nodes
axon node list --status online    # filter online
axon node info <node>             # detailed info
axon node remove <node>           # unregister
```

### Token Management

```bash
axon token create-join --max-uses 10 --expires 24h
axon token list-join
axon token revoke-join <id>
axon auth list-tokens
axon auth revoke <token-id>
```

### User Management

```bash
axon user create <name> --node-ids web-1,db-1
axon user list
axon user update <name> --node-ids web-1,db-1,db-2
axon user delete <name>
```

## Troubleshooting

| Symptom | Check |
|---------|-------|
| Node shows offline | `axon-agent status` on target; check network to server |
| Auth error (exit 2) | Token expired → `axon auth login` or re-create token |
| Connection refused | Server running? `axon-server start` or check listen addr |
| TLS errors | Match TLS config: `--tls-insecure` for no-TLS server, `--ca-cert` for self-signed |

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | General error |
| 2 | Auth error |
| 3 | Node error (not found / offline) |
| N | `exec`: remote command's exit code |

## Reference

For full CLI reference, config options, and protocol details: see `references/cli-reference.md`.
