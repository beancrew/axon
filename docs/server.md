# Axon Server Design

## Overview

`axon-server` is the central control plane. Single binary, self-hosted. Manages node registration, authentication, request routing, and audit logging.

**All traffic flows through the server. CLI and Agent never communicate directly.**

## Architecture

```
┌──────────────── axon-server ────────────────┐
│                                              │
│  ┌──────────┐  ┌──────────┐  ┌───────────┐  │
│  │ gRPC API │  │ gRPC API │  │ gRPC API  │  │
│  │ Mgmt     │  │ Ops      │  │ Control   │  │
│  └────┬─────┘  └────┬─────┘  └─────┬─────┘  │
│       │              │              │         │
│  ┌────▼──────────────▼──────────────▼─────┐  │
│  │              Router                     │  │
│  │  CLI request → find node → dispatch     │  │
│  └────┬───────────────────────────┬───────┘  │
│       │                           │          │
│  ┌────▼─────┐             ┌──────▼───────┐  │
│  │ Registry │             │ Auth (JWT)   │  │
│  │          │             │              │  │
│  │ node_id  │             │ sign/verify  │  │
│  │ status   │             │ token scope  │  │
│  │ streams  │             └──────────────┘  │
│  └────┬─────┘                                │
│       │                                      │
│  ┌────▼─────┐                                │
│  │ Audit    │                                │
│  │ Logger   │                                │
│  └──────────┘                                │
└──────────────────────────────────────────────┘
```

## Submodules

### 1. gRPC API Layer

Exposes three gRPC services (see [protocol.md](protocol.md)):

| Service | Source | Description |
|---------|--------|-------------|
| `ControlService` | Agent | Agent registration, heartbeat, task dispatch |
| `OperationsService` | CLI | exec, read, write, forward |
| `ManagementService` | CLI | node list/info/remove, auth login |

Single gRPC server on one port (default: 443). All three services registered on the same server.

### 2. Node Registry

In-memory registry of all known nodes.

```go
type NodeEntry struct {
    NodeID        string
    NodeName      string
    Status        string            // "online" | "offline"
    Info          NodeInfo          // OS, arch, IP, version, uptime
    Labels        map[string]string
    ControlStream grpc.BidiStream   // Reference to active control stream
    ConnectedAt   time.Time
    LastHeartbeat time.Time
    RegisteredAt  time.Time
}
```

**Operations:**

| Operation | Trigger | Description |
|-----------|---------|-------------|
| Register | Agent connects | Add/update node entry, set status = online |
| Heartbeat update | Agent heartbeat | Update `LastHeartbeat` |
| Mark offline | Heartbeat timeout | Set status = offline, clear ControlStream ref |
| Remove | `axon node remove` | Delete entry entirely, disconnect agent |
| List | `axon node list` | Return all entries |
| Lookup | Any CLI operation | Find node by name or ID |

**Persistence:**
- Phase 1: in-memory only. Nodes re-register on server restart.
- Phase 2: persist to SQLite for fast recovery.

**Heartbeat timeout detection:**
- Background goroutine checks every 5 seconds
- If `time.Now() - LastHeartbeat > 3 × heartbeat_interval` → mark offline

### 3. Router

Routes CLI requests to the correct agent.

```
CLI request (exec web-1 "ls")
    │
    ▼
Router:
    1. Authenticate: verify JWT token from gRPC metadata
    2. Authorize: check token.node_ids contains "web-1"
    3. Lookup: find "web-1" in Registry
    4. Check status: if offline → return UNAVAILABLE
    5. Dispatch: send TaskSignal via control stream
    6. Bridge: proxy data between CLI stream and Agent data stream
```

**Stream bridging for operations:**

```
CLI gRPC stream ←──→ Server (bridge) ←──→ Agent gRPC stream
```

Server acts as a transparent proxy:
- exec: forward ExecOutput from Agent stream to CLI stream
- read: forward ReadOutput from Agent stream to CLI stream
- write: forward WriteInput from CLI stream to Agent stream
- forward: bidirectional relay of TunnelData between CLI and Agent streams

### 4. Auth Module

JWT-based authentication.

**Token types:**

| Type | Issued to | Contains | Used for |
|------|-----------|----------|----------|
| CLI Token | Users/agents | `user_id`, `node_ids[]`, `exp`, `iat` | CLI → Server auth |
| Agent Token | Nodes | `node_id`, `exp` | Agent registration (one-time) |

**Server-side:**

```go
type AuthConfig struct {
    JWTSigningKey string        // HMAC-SHA256 key
    TokenExpiry   time.Duration // Default: 24h for CLI tokens
}
```

**Token validation flow:**

```
1. Extract token from gRPC metadata ("authorization: Bearer <token>")
2. Verify JWT signature
3. Check expiry
4. For CLI tokens: extract node_ids, pass to Router for authorization
5. For Agent tokens: extract node_id, pass to Registry for registration
```

**Login flow (Phase 1):**

```
CLI sends LoginRequest{username, password}
    │
    ▼
Server validates against local user store (config file or SQLite)
    │
    ▼
Server signs JWT with user_id + allowed node_ids
    │
    ▼
Returns LoginResponse{token, expires_at}
```

**User store (Phase 1):**

```yaml
# axon-server config
users:
  - username: gary
    password_hash: "$2a$10$..."   # bcrypt
    node_ids: ["*"]               # Access to all nodes
  - username: deploy-agent
    password_hash: "$2a$10$..."
    node_ids: ["web-1", "web-2"]  # Restricted access
```

### 5. Audit Logger

Logs every operation that passes through the server.

**Log entry:**

```go
type AuditEntry struct {
    Timestamp  time.Time
    UserID     string
    NodeID     string
    Action     string    // "exec", "read", "write", "forward", "node.remove"
    Detail     string    // Command string, file path, or port mapping
    Result     string    // "success", "error", "timeout"
    DurationMs int64
    Error      string    // Error message if failed
}
```

**Storage (Phase 1):** SQLite

```sql
CREATE TABLE audit_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp TEXT NOT NULL,
    user_id TEXT NOT NULL,
    node_id TEXT NOT NULL,
    action TEXT NOT NULL,
    detail TEXT,
    result TEXT NOT NULL,
    duration_ms INTEGER,
    error TEXT
);

CREATE INDEX idx_audit_timestamp ON audit_log(timestamp);
CREATE INDEX idx_audit_node ON audit_log(node_id);
CREATE INDEX idx_audit_user ON audit_log(user_id);
```

**Behavior:**
- Async write (buffered channel → background writer)
- Non-blocking: audit failure should not block operations
- Retention: configurable, default keep all (Phase 1)

## Server Config

```yaml
# /etc/axon-server/config.yaml

listen: ":443"

tls:
  cert: "/etc/axon-server/tls/server.crt"
  key: "/etc/axon-server/tls/server.key"

auth:
  jwt_signing_key: "${AXON_JWT_KEY}"      # From environment variable
  token_expiry: "24h"

users:
  - username: gary
    password_hash: "$2a$10$..."
    node_ids: ["*"]

heartbeat:
  interval_seconds: 10       # Sent to agents
  timeout_multiplier: 3      # offline after 3× missed heartbeats

audit:
  db_path: "/var/lib/axon-server/audit.db"

agent_tokens:
  - token: "${AXON_AGENT_TOKEN_1}"
    allowed_node_name: "web-1"    # Optional: restrict which name this token can register
  - token: "${AXON_AGENT_TOKEN_2}"
    allowed_node_name: ""         # Empty = any name
```

## Startup Sequence

```
1. Load config (file + env vars)
2. Initialize SQLite (audit log)
3. Initialize node registry (empty)
4. Initialize auth module (load signing key, user store)
5. Start gRPC server (TLS)
   - Register ControlService
   - Register OperationsService
   - Register ManagementService
6. Start heartbeat monitor (background goroutine)
7. Ready
```

## Graceful Shutdown

```
1. Stop accepting new connections
2. Wait for in-flight RPCs to complete (with timeout)
3. Close all agent control streams (agents will reconnect)
4. Flush audit log buffer
5. Close SQLite
6. Exit
```

## Command

### `axon-server start`

```
$ axon-server start --config /etc/axon-server/config.yaml
[INFO] loading config from /etc/axon-server/config.yaml
[INFO] audit database initialized at /var/lib/axon-server/audit.db
[INFO] gRPC server listening on :443 (TLS)
[INFO] ready
```

- **Flags**:
  - `--config <path>` — config file path (default: `./config.yaml`)
  - `--foreground` — run in foreground

### `axon-server version`

```
$ axon-server version
axon-server 0.1.0 (go1.22, linux/amd64)
```

---

# Axon Server 设计

## 概述

`axon-server` 是中央控制面。单二进制，自部署。管理节点注册、认证、请求路由、审计日志。

所有流量经过 Server，CLI 和 Agent 不直接通信。

## 子模块

1. **gRPC API 层** — 三个 gRPC service 注册在同一端口
2. **节点注册中心** — 内存存储，管理节点状态（online/offline）
3. **路由层** — 认证 → 授权 → 查找节点 → 派发任务 → 桥接数据流
4. **认证模块** — JWT 签发/校验，CLI Token 绑定节点列表
5. **审计日志** — 异步写入 SQLite，不阻塞操作

## 认证

- CLI Token (JWT)：user_id + node_ids + 过期时间
- Agent Token：节点注册用，一次性验证
- 用户存储：Phase 1 配置文件 + bcrypt

## 节点管理

- 注册：Agent 连接时自动注册
- 心跳超时：3× interval 未收到心跳 → 标记 offline
- 移除：`axon node remove` 删除注册 + 断开连接

## 审计

异步写入 SQLite，按时间/节点/用户可查询。审计失败不阻塞业务操作。
