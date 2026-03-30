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

## Quick Start

```bash
# Install (auto-detects OS/arch)
curl -fsSL https://raw.githubusercontent.com/beancrew/axon/main/scripts/install.sh | sh -s -- server
curl -fsSL https://raw.githubusercontent.com/beancrew/axon/main/scripts/install.sh | sh -s -- agent
curl -fsSL https://raw.githubusercontent.com/beancrew/axon/main/scripts/install.sh | sh -s -- cli

# Initialize server
axon-server init

# Join a node
axon-agent join <server-addr>:9090 <join-token>

# Use it
axon exec my-node "hostname"
```

→ Full guide: [Quick Start](docs/quickstart.md)

## CLI Reference

### Node Management

```bash
axon node list                 # List all connected nodes
axon node info <node>          # Node details
axon node remove <node>        # Remove a node
```

### Core Operations

```bash
# Execute a command on a remote node
axon exec <node> <command>
axon exec web-1 "docker ps"
axon exec db-1 "pg_dump mydb > /tmp/backup.sql"

# Read a file from a remote node
axon read <node> <path>
axon read web-1 /etc/nginx/nginx.conf > local.conf

# Write a file to a remote node (stdin)
echo "hello" | axon write web-1 /tmp/hello.txt
cat config.yaml | axon write web-1 /etc/app/config.yaml

# Port forwarding
axon forward create db-1 5432:5432    # Non-blocking, managed
axon forward list                      # List active forwards
axon forward delete <id>               # Remove a forward
axon forward db-1 5432:5432           # Blocking shorthand
```

4 operations. Everything else is a combination of these, guided by skills.

→ Full reference: [CLI Reference](docs/cli.md)

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
## Steps
1. axon write <node> /opt/<service>/docker-compose.yaml
2. axon exec <node> "cd /opt/<service> && docker compose pull"
3. axon exec <node> "cd /opt/<service> && docker compose up -d"
4. axon exec <node> "docker ps | grep <service>"  # verify
```

Different scenario? Different skill. Same CLI.

An [AgentSkill for Axon](skills/axon/) is included in this repo.

## Features

- **Remote execution** — Run commands on any connected node, real-time stdout/stderr streaming
- **File operations** — Read and write files on remote nodes via stdin/stdout
- **Port forwarding** — Map remote ports to localhost, with daemon-managed multi-forward support
- **Reverse connection** — Nodes connect outbound to server; no inbound ports, works behind NAT/firewalls
- **Token-based auth** — JWT with JTI, revocation, and join-token enrollment for agents
- **User management** — Create, list, update, delete users with per-node access control
- **Auto-TLS** — Self-signed CA and server certificate generated automatically; BYO cert supported
- **Audit logging** — Every operation recorded with timestamp, caller, node, and result
- **Single-binary deployment** — One binary per component, cross-platform (Linux/macOS, amd64/arm64)
- **Server daemon mode** — Run server in background with `--daemon`, stop with `axon-server stop`

## Security

- **Token-based auth** — JWT with unique JTI, revocation support
- **User management** — Per-user node access control
- **Audit log** — Every operation logged with timestamp, caller, node, command, result
- **No inbound ports on nodes** — Reverse connection only
- **Auto-TLS** — Self-signed CA + server cert generated automatically; BYO cert supported

## Documentation

- [Quick Start Guide](docs/quickstart.md) — Get running in 5 minutes
- [Configuration Reference](docs/configuration.md) — All config options
- [Architecture Overview](docs/architecture.md) — How components fit together
- [CLI Reference](docs/cli.md) — Full command reference
- [Protocol Design](docs/protocol.md) — gRPC/protobuf details
- [Server Design](docs/server.md) — Server internals
- [Agent Design](docs/agent.md) — Agent internals

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup, branch conventions, and PR guidelines.

## License

Apache License 2.0 — see [LICENSE](LICENSE).
