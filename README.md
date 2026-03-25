# Axon

**The nerve connecting AI agents to real machines.**

Axon is infrastructure for AI agents. It lets agents operate remote machines — execute commands, read/write files, forward ports — as naturally as operating locally.

No SSH keys to manage. No YAML to write. No complex APIs to learn. Just a CLI that any agent already knows how to use.

> [中文版 / Chinese](README_zh.md)

## Why Axon?

Today's infrastructure is built for humans: web dashboards, SSH terminals, config files. AI agents can't use any of that natively.

Axon bridges this gap. It provides **low-level, composable primitives** that agents combine with skills (knowledge) to accomplish any task. We don't over-abstract — agents are smart enough to figure out the "how" when given the right tools.

```
AI Agent (any framework)
    │
    │  CLI (exec / read / write / forward)
    ▼
┌────── Axon Server ──────┐
│  Auth · Routing · Audit  │
└──────────────────────────┘
    │         │         │
    ▼         ▼         ▼
  Node A    Node B    Node C
  (any machine: bare metal / VM / container / edge device)
```

## Core Principles

1. **Low-level primitives, not high-level abstractions** — We provide `exec`, `read`, `write`, `forward`. Not `deploy()` or `check_health()`. Agents decide how to combine them.

2. **CLI-first** — Every agent framework can call CLI tools. No SDK required, no protocol lock-in.

3. **Teach, don't hardcode** — Domain knowledge lives in skills (markdown files), not in code. Want the agent to deploy a Docker service? Write a skill. Want it to run a database backup? Write a skill. The CLI stays the same.

4. **Zero-config for nodes** — Install the agent binary, point it at the server, done. No SSH keys, no firewall rules, no port forwarding.

5. **Agent-native, human-friendly** — Designed for agents, but humans can use it too for debugging and inspection.

## Architecture

### Components

**Axon CLI** (`axon`)
- The interface for agents (and humans)
- Talks to Axon Server via gRPC
- Stateless — all state lives on the server

**Axon Server** (`axon-server`)
- Central control plane, user self-hosted
- Single binary, minimal config
- Manages node registration, authentication, routing
- Audit log: who did what, on which node, when

**Axon Agent** (`axon-agent`)
- Lightweight daemon on each target machine
- Reverse connection to server (no inbound ports needed)
- Auto-reconnect on network failure
- Runs as a system service

### Connection Model

```
axon-agent (node) ──── reverse connect ────→ axon-server ←──── gRPC ──── axon CLI
```

Nodes connect **outbound** to the server. This means:
- No SSH ports to expose
- Works behind NAT, firewalls, corporate networks
- Edge devices, cloud VMs, on-prem servers — all the same

### Node Lifecycle

```
Install axon-agent → Start with server URL + token → Auto-register → Online
                                                                       │
                                                          Agent operates via CLI
                                                                       │
                                                    Kill agent or `axon node remove` → Gone
```

No ceremony. No approval flow. Token-based auth, start and go.

## CLI Reference

### Node Management

```bash
# List all connected nodes
axon node list

# Node details (OS, IP, uptime, agent version)
axon node info <node>

# Remove a node
axon node remove <node>
```

### Core Operations

```bash
# Execute a command on a remote node
axon exec <node> <command>
axon exec web-1 "docker ps"
axon exec db-1 "pg_dump mydb > /tmp/backup.sql"

# Read a file from a remote node
axon read <node> <path>
axon read web-1 /etc/nginx/nginx.conf

# Write a file to a remote node (stdin)
axon write <node> <path> < local-file.yaml
echo "hello" | axon write web-1 /tmp/hello.txt

# Port forwarding (expose remote port locally)
axon forward <node> <local-port>:<remote-port>
axon forward db-1 5432:5432      # Access remote PostgreSQL locally
axon forward web-1 8080:80       # Access remote HTTP locally
```

### That's it.

4 operations. Everything else is a combination of these, guided by skills.

## How Agents Use Axon

An agent doesn't need special integration. It just calls the CLI:

```python
# Any agent framework
result = exec("axon exec web-1 'systemctl status nginx'")
config = exec("axon read web-1 /etc/nginx/nginx.conf")
exec("echo '...' | axon write web-1 /etc/nginx/nginx.conf")
exec("axon exec web-1 'systemctl reload nginx'")
```

Domain knowledge comes from **skills** — markdown files that teach the agent what to do:

```markdown
# skill: deploy-service
## Tools
- axon exec <node> <command>
- axon write <node> <path>

## Steps
1. axon write <node> /opt/<service>/docker-compose.yaml
2. axon exec <node> "cd /opt/<service> && docker compose pull"
3. axon exec <node> "cd /opt/<service> && docker compose up -d"
4. axon exec <node> "docker ps | grep <service>"  # verify
```

Different scenario? Different skill. Same CLI.

## Security

- **Token-based auth** — JWT with unique JTI, revocation support
- **User management** — SQLite-backed users with bcrypt passwords, CRUD via CLI
- **Audit log** — Every operation logged with timestamp, caller, node, command, result
- **No inbound ports on nodes** — Reverse connection only
- **Auto-TLS** — Self-signed CA + server cert generated automatically; bring-your-own-cert supported
- **Token lifecycle** — List, revoke, and rotate tokens via CLI

## Roadmap

### Phase 1: Foundation ✅
- [x] axon-server: gRPC server, node registry, auth, routing, audit
- [x] axon-agent: reverse connection, exec, read, write, forward
- [x] axon CLI: full command set (exec, read, write, forward, node management)
- [x] Token-based authentication (JWT)
- [x] Audit logging (SQLite)
- [x] Data plane bridge (CLI ↔ Server ↔ Agent streaming)

### Phase 2: Production Hardening (in progress)
- [x] P2-1: Registry SQLite persistence + stable node_id
- [x] P2-2: Token management (JTI, revoke, list)
- [x] P2-3: User store persistence (SQLite CRUD, bootstrap from config)
- [x] P2-4: Auto-TLS (self-signed CA, server cert, auto-renewal)
- [ ] P2-5: Agent security policies (command allowlist, path restrictions)

### Phase 3: Ecosystem
- [ ] Web dashboard (read-only status, grpc-gateway)
- [ ] Plugin system for custom node capabilities
- [ ] Pre-built skills library
- [ ] Agent auto-update

## Documentation

- [Quick Start Guide](docs/quickstart.md) — Get running in 5 minutes
- [Configuration Reference](docs/configuration.md) — All config options
- [Architecture Overview](docs/architecture.md) — How components fit together
- [CLI Reference](docs/cli.md) — Full command reference
- [Protocol Design](docs/protocol.md) — gRPC/protobuf details
- [Server Design](docs/server.md) — Server internals
- [Agent Design](docs/agent.md) — Agent internals

## Tech Stack

- **Language**: Go
- **Communication**: gRPC over HTTP/2 (full chain)
- **Auth**: JWT (HMAC-SHA256) with JTI + revocation
- **Persistence**: SQLite (WAL mode, single shared DB)
- **TLS**: Auto-TLS (ECDSA P-256) or explicit certs
- **Build**: Single binary per component, cross-platform

## License

TBD
