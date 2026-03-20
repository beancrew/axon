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

---

# Axon 架构总览

## 组件

Axon 由三个组件组成，每个构建为单一静态二进制。

| 组件 | 二进制 | 职责 |
|------|--------|------|
| **axon-cli** | `axon` | 用户/Agent 操作接口，无状态，通过 gRPC 与 Server 通信 |
| **axon-server** | `axon-server` | 中央控制面，节点注册、认证、路由、审计 |
| **axon-agent** | `axon-agent` | 目标机器上的轻量 daemon，反向连接 Server |

## 通信

**全链路 gRPC over HTTP/2。** 不用 WebSocket、REST、自定义 TCP。

| 链路 | 模式 | 说明 |
|------|------|------|
| CLI → Server | unary + stream | node list/info 用 unary；exec 用 server stream |
| Agent → Server 控制面 | BiDi stream 长连接 | 心跳、注册、节点信息上报 |
| Agent → Server 操作面 | 按需 stream | 每个 exec/read/write/forward 任务独立 stream |

### 为什么选 gRPC？

exec 要流式输出，forward 要双向流，Agent 要长连接。HTTP 需要三种补丁（SSE + WebSocket + chunked），gRPC 一套全解决。

## 控制面 vs 操作面

Agent 在同一条 HTTP/2 连接上维护两个逻辑通道：
- **控制面**：一条长连接 BiDi stream（心跳、注册、信息上报）
- **操作面**：按需开 stream（exec/read/write/forward），任务结束 stream 关闭

## 认证

JWT Token。CLI Token 绑定用户 + 允许的节点列表。Agent Token 用于首次注册。

## 节点状态

online / offline。请求 offline 节点直接报错，不排队。

## 审计日志

所有经过 Server 的操作全部记录。Phase 1 存 SQLite。
