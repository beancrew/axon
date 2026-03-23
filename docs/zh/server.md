# Axon Server 设计

## 概述

`axon-server` 是中央控制面。单二进制，用户自部署。管理节点注册、认证、请求路由和审计日志。

**所有流量经过 Server。CLI 和 Agent 不直接通信。**

## 架构

```
┌──────────────── axon-server ────────────────┐
│                                              │
│  ┌──────────┐  ┌──────────┐  ┌───────────┐  │
│  │ gRPC API │  │ gRPC API │  │ gRPC API  │  │
│  │ 管理面    │  │ 操作面    │  │ 控制面     │  │
│  └────┬─────┘  └────┬─────┘  └─────┬─────┘  │
│       │              │              │         │
│  ┌────▼──────────────▼──────────────▼─────┐  │
│  │              路由层                      │  │
│  │  CLI 请求 → 查找节点 → 派发任务          │  │
│  └────┬───────────────────────────┬───────┘  │
│       │                           │          │
│  ┌────▼─────┐             ┌──────▼───────┐  │
│  │ 注册中心  │             │ 认证 (JWT)   │  │
│  │          │             │              │  │
│  │ node_id  │             │ 签发/校验     │  │
│  │ 状态     │             │ token 作用域  │  │
│  │ stream   │             └──────────────┘  │
│  └────┬─────┘                                │
│       │                                      │
│  ┌────▼─────┐                                │
│  │ 审计日志  │                                │
│  │          │                                │
│  └──────────┘                                │
└──────────────────────────────────────────────┘
```

## 子模块

### 1. gRPC API 层

暴露三个 gRPC 服务（详见 [protocol.md](../protocol.md)）：

| 服务 | 调用方 | 说明 |
|------|--------|------|
| `ControlService` | Agent | Agent 注册、心跳、任务派发 |
| `OperationsService` | CLI | exec、read、write、forward |
| `ManagementService` | CLI | node list/info/remove、auth login |

单个 gRPC server 监听一个端口（默认：443）。三个服务注册在同一个 server 上。

### 2. 节点注册中心

所有已知节点的内存注册表。

```go
type NodeEntry struct {
    NodeID        string
    NodeName      string
    Status        string            // "online" | "offline"
    Info          NodeInfo          // hostname, arch, IP, version, uptime, OS 信息
    Labels        map[string]string
    ControlStream grpc.BidiStream   // 活跃的控制 stream 引用
    ConnectedAt   time.Time
    LastHeartbeat time.Time
    RegisteredAt  time.Time
}
```

**操作：**

| 操作 | 触发 | 说明 |
|------|------|------|
| 注册 | Agent 连接 | 添加/更新节点条目，状态设为 online |
| 心跳更新 | Agent 心跳 | 更新 `LastHeartbeat` |
| 标记离线 | 心跳超时 | 状态设为 offline，清除 ControlStream 引用 |
| 移除 | `axon node remove` | 删除条目，断开 Agent 连接 |
| 列表 | `axon node list` | 返回所有条目 |
| 查找 | 任何 CLI 操作 | 按名称或 ID 查找节点 |

**持久化：**
- Phase 1：仅内存。Server 重启后节点重新注册。
- Phase 2：持久化到 SQLite，加快恢复速度。

**心跳超时检测：**
- 后台 goroutine 每 5 秒检查一次
- 如果 `当前时间 - LastHeartbeat > 3 × 心跳间隔` → 标记 offline

### 3. 路由层

将 CLI 请求路由到正确的 Agent。

```
CLI 请求 (exec web-1 "ls")
    │
    ▼
路由层：
    1. 认证：验证 gRPC metadata 中的 JWT token
    2. 授权：检查 token.node_ids 包含 "web-1"
    3. 查找：在注册中心找到 "web-1"
    4. 检查状态：如果 offline → 返回 UNAVAILABLE
    5. 派发：通过控制 stream 发送 TaskSignal
    6. 桥接：在 CLI stream 和 Agent 操作 stream 之间代理数据
```

**操作的 Stream 桥接：**

```
CLI gRPC stream ←──→ Server（桥接）←──→ Agent gRPC stream
```

Server 作为透明代理：
- exec：将 Agent stream 的 ExecOutput 转发到 CLI stream
- read：将 Agent stream 的 ReadOutput 转发到 CLI stream
- write：将 CLI stream 的 WriteInput 转发到 Agent stream
- forward：在 CLI 和 Agent stream 之间双向中继 TunnelData

### 4. 认证模块

基于 JWT 的认证。

**Token 类型：**

| 类型 | 签发给 | 包含 | 用途 |
|------|--------|------|------|
| CLI Token | 用户/Agent | `user_id`、`node_ids[]`、`exp`、`iat` | CLI → Server 认证 |
| Agent Token | 节点 | `node_id`、`exp` | Agent 注册（一次性） |

**Server 端：**

```go
type AuthConfig struct {
    JWTSigningKey string        // HMAC-SHA256 密钥
    TokenExpiry   time.Duration // 默认：CLI token 24 小时
}
```

**Token 验证流程：**

```
1. 从 gRPC metadata 提取 token（"authorization: Bearer <token>"）
2. 验证 JWT 签名
3. 检查过期时间
4. CLI Token：提取 node_ids，传给路由层做授权
5. Agent Token：提取 node_id，传给注册中心做注册
```

**登录流程（Phase 1）：**

```
CLI 发送 LoginRequest{username, password}
    │
    ▼
Server 对比本地用户存储（配置文件或 SQLite）
    │
    ▼
Server 签发 JWT（含 user_id + 允许的 node_ids）
    │
    ▼
返回 LoginResponse{token, expires_at}
```

**用户存储（Phase 1）：**

```yaml
# axon-server 配置
users:
  - username: gary
    password_hash: "$2a$10$..."   # bcrypt
    node_ids: ["*"]               # 可访问所有节点
  - username: deploy-agent
    password_hash: "$2a$10$..."
    node_ids: ["web-1", "web-2"]  # 受限访问
```

### 5. 审计日志

记录所有经过 Server 的操作。

**日志条目：**

```go
type AuditEntry struct {
    Timestamp  time.Time
    UserID     string
    NodeID     string
    Action     string    // "exec", "read", "write", "forward", "node.remove"
    Detail     string    // 命令字符串、文件路径或端口映射
    Result     string    // "success", "error", "timeout"
    DurationMs int64
    Error      string    // 失败时的错误信息
}
```

**存储（Phase 1）：** SQLite

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

**行为：**
- 异步写入（带缓冲的 channel → 后台写入 goroutine）
- 非阻塞：审计失败不应阻塞业务操作
- 保留策略：可配置，Phase 1 默认保留全部

## Server 配置

```yaml
# /etc/axon-server/config.yaml

listen: ":443"

tls:
  cert: "/etc/axon-server/tls/server.crt"
  key: "/etc/axon-server/tls/server.key"

auth:
  jwt_signing_key: "${AXON_JWT_KEY}"      # 从环境变量读取
  token_expiry: "24h"

users:
  - username: gary
    password_hash: "$2a$10$..."
    node_ids: ["*"]

heartbeat:
  interval_seconds: 10       # 发送给 Agent
  timeout_multiplier: 3      # 3 倍间隔未收到心跳 → offline

audit:
  db_path: "/var/lib/axon-server/audit.db"

agent_tokens:
  - token: "${AXON_AGENT_TOKEN_1}"
    allowed_node_name: "web-1"    # 可选：限制此 token 可注册的名称
  - token: "${AXON_AGENT_TOKEN_2}"
    allowed_node_name: ""         # 空 = 任意名称
```

## 启动序列

```
1. 加载配置（配置文件 + 环境变量）
2. 初始化 SQLite（审计日志）
3. 初始化节点注册中心（空）
4. 初始化认证模块（加载签名密钥、用户存储）
5. 启动 gRPC server（TLS）
   - 注册 ControlService
   - 注册 OperationsService
   - 注册 ManagementService
6. 启动心跳监控（后台 goroutine）
7. 就绪
```

## 优雅关闭

```
1. 停止接受新连接
2. 等待进行中的 RPC 完成（有超时）
3. 关闭所有 Agent 控制 stream（Agent 会自动重连）
4. 刷新审计日志缓冲
5. 关闭 SQLite
6. 退出
```

## 命令

### `axon-server start`

```
$ axon-server start --config /etc/axon-server/config.yaml
[INFO] loading config from /etc/axon-server/config.yaml
[INFO] audit database initialized at /var/lib/axon-server/audit.db
[INFO] gRPC server listening on :443 (TLS)
[INFO] ready
```

- **参数**：
  - `--config <path>` — 配置文件路径（默认：`./config.yaml`）
  - `--foreground` — 前台运行

### `axon-server version`

```
$ axon-server version
axon-server 0.1.0 (go1.22, linux/amd64)
```
