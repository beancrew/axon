# Axon Architecture Overview

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

Future HTTP/REST access (web dashboard, third-party integrations) will be served by a grpc-gateway layer in Phase 3.

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
│ exp                  │     │ (used once at first start) │
│ iat                  │     └────────────────────────────┘
└──────────────────────┘
```

| Token Type | Scope | Lifetime |
|------------|-------|----------|
| CLI Token | Bound to user + allowed node list | Configurable expiry |
| Agent Token | Bound to node identity | Used at first registration, then session-based |

CLI Token binds to specific nodes — a token can only operate on its allowed node list.

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
  "timestamp": "2026-03-20T11:22:00Z",
  "user_id": "gary",
  "node_id": "web-1",
  "action": "exec",
  "command": "docker ps",
  "result": "success",
  "duration_ms": 120
}
```

Storage: SQLite (Phase 1). Extensible to external stores later.

## Tech Stack

| Item | Choice |
|------|--------|
| Language | Go |
| Communication | gRPC (HTTP/2), full link |
| Auth | JWT |
| Serialization | Protocol Buffers |
| Build | Single binary × 3, cross-platform |
| Audit storage | SQLite (Phase 1) |
| Config format | YAML |
