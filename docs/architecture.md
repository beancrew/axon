# Axon Architecture Overview

> [中文](zh/architecture.md)

## Components

Axon consists of three components, each built as a single static binary.

```
axon-cli ── gRPC ──→ axon-server ←── gRPC ── axon-agent
                     (control plane)        (reverse connect)
```

| Component | Binary | Role |
|-----------|--------|------|
| **axon-cli** | `axon` | User/agent interface. Stateless. Talks to Server via gRPC. |
| **axon-server** | `axon-server` | Central control plane. Node registry, auth, routing, audit. |
| **axon-agent** | `axon-agent` | Lightweight daemon on target machines. Reverse-connects to Server. |

## Communication

**All links use gRPC over HTTP/2.** No WebSocket, no REST, no custom TCP.

| Link | Protocol | Pattern | Why |
|------|----------|---------|-----|
| CLI → Server | gRPC | unary + server/client/bidi stream | exec needs streaming; forward needs bidi |
| Agent → Server (control plane) | gRPC | BiDi stream (long-lived) | Heartbeat, registration, node info |
| Agent → Server (data plane) | gRPC | Per-task stream | exec/read/write/forward each get an independent stream |

Multiple gRPC streams share a single HTTP/2 TCP connection — no connection explosion.

### Why gRPC Over Everything?

1. `exec` requires streaming stdout/stderr — gRPC server stream is native
2. `forward` requires bidirectional data — gRPC bidi stream is native
3. Agent reverse connection requires long-lived connection — gRPC bidi stream is native
4. HTTP would need SSE + WebSocket + chunked transfer — three patches instead of one protocol
5. Unified tech stack: one proto definition, one TLS config, one codegen pipeline

## Connection Model

Agents connect **outbound** to the Server. No inbound ports required on nodes.

```
axon-agent ──── outbound gRPC ────→ axon-server ←──── gRPC ──── axon-cli
   (behind NAT/firewall)              (public)           (anywhere)
```

This means:
- No SSH ports to expose
- Works behind NAT, corporate firewalls, edge networks
- Cloud VMs, on-prem servers, edge devices — all the same

## Control Plane vs Data Plane

Agent maintains two logical channels over the same HTTP/2 connection:

```
┌─────────── HTTP/2 Connection ───────────┐
│                                          │
│  Control Plane (1 long-lived stream)     │
│  ├── Registration (on connect)           │
│  ├── Heartbeat (periodic)                │
│  └── Node info reporting (periodic)      │
│                                          │
│  Data Plane (on-demand streams)          │
│  ├── exec stream (per command)           │
│  ├── read stream (per file)              │
│  ├── write stream (per file)             │
│  └── forward stream (per TCP connection) │
│                                          │
└──────────────────────────────────────────┘
```

Control plane stream lifecycle = agent process lifetime.
Data plane stream lifecycle = individual task lifetime.

## Authentication

JWT-based. Server holds the signing key.

```
┌─── CLI Token (JWT) ──┐     ┌─── Agent Token ───────────┐
│ user_id              │     │ node_id                    │
│ node_ids: [...]      │     │ server_url                 │
│ jti (unique token ID)│     │ (used once at first start) │
│ exp                  │     └────────────────────────────┘
│ iat                  │
└──────────────────────┘
```

| Token Type | Scope | Lifetime |
|------------|-------|----------|
| CLI Token | Bound to identity + allowed node list | No expiry (issued by `init`) |
| Agent Token | Bound to node identity | No expiry (issued during `join`) |
| Join Token | Agent enrollment | Configurable (max uses / expiry) |

### Token Management

- Each issued CLI token gets a unique **JTI** (JWT ID)
- Tokens are persisted in SQLite — can be listed and revoked
- Revoked tokens are checked in-memory (O(1) lookup) via gRPC interceptor
- CLI commands: `axon token list`, `axon token revoke <id>`

## Persistence

All persistent state lives in a **single shared SQLite database** (WAL mode):

| Table | Contents |
|-------|----------|
| `nodes` | Node registry (ID, name, status, metadata, token hash) |
| `tokens` | Issued JWT tokens (JTI, kind, nodes, timestamps, revoked) |
| `join_tokens` | Join tokens for agent enrollment (hash, uses, expiry) |
| `audit_log` | Operation audit trail (separate SQLite file) |

### Node Identity

Nodes get a stable `node_id` (UUID) on first registration, persisted in the agent's local config. On reconnect, the server identifies returning nodes by `node_id`. Heartbeats are batched (30s flush interval) to reduce write load.

## TLS

### Auto-TLS

When `tls.auto: true` is set and no explicit TLS certificates are configured, the server auto-generates:
- **CA**: ECDSA P-256, 10-year validity, stored at `~/.axon-server/tls/ca.crt`
- **Server cert**: ECDSA P-256, 1-year validity, auto-renewed when expiring within 30 days
- SANs: always include `localhost` + `127.0.0.1` + configured hostname

The CA certificate must be distributed to agents and CLI clients for TLS verification.

### TLS Modes

| Mode | Server Config | Client/Agent |
|------|--------------|--------------|
| No TLS (default) | `tls.auto: false` (default) | `--tls-insecure` |
| Auto-TLS | `tls.auto: true` or `init --tls` | `--ca-cert ca.crt` |
| Explicit cert | `tls.cert` + `tls.key` | System CA or `--ca-cert` |

## Node States

| State | Meaning |
|-------|---------|
| **online** | Agent connected, heartbeat healthy |
| **offline** | Heartbeat timeout or connection lost |

CLI requests to offline nodes return an immediate error. No queuing, no waiting.

## Audit Logging

Every operation through the Server is logged:

```json
{
  "timestamp": "2026-03-25T11:22:00Z",
  "user_id": "gary",
  "node_id": "web-1",
  "action": "exec",
  "command": "docker ps",
  "result": "success",
  "duration_ms": 120
}
```

Storage: SQLite (async write, non-blocking, buffered channel → background writer).

## Tech Stack

| Item | Choice |
|------|--------|
| Language | Go |
| Communication | gRPC (HTTP/2), full link |
| Auth | JWT (HMAC-SHA256) with JTI + revocation |
| Serialization | Protocol Buffers |
| Persistence | SQLite (WAL mode, single shared DB) |
| TLS | Auto-TLS (ECDSA P-256) or explicit certs |
| Build | Single binary × 3, cross-platform |
| Config format | YAML |
