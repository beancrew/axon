# Axon Server 设计

> [English Version](../server.md)

## 概述

`axon-server` 是中心控制面。单一二进制，自托管。管理节点注册、认证、请求路由、持久化、TLS 和审计日志。

**所有流量经过 Server。CLI 和 Agent 不直接通信。**

## 子模块

### 1. gRPC API 层

单一 gRPC 服务端，同一端口注册三个服务：

| 服务 | 来源 | 说明 |
|------|------|------|
| `ControlService` | Agent | 注册、心跳、任务分发 |
| `OperationsService` | CLI | exec、read、write、forward |
| `ManagementService` | CLI | 节点/Token/Join Token 管理、Agent 注册 |

### 2. 节点注册表（SQLite 持久化）

```sql
CREATE TABLE nodes (
    node_id      TEXT PRIMARY KEY,
    node_name    TEXT NOT NULL,
    status       TEXT NOT NULL DEFAULT 'offline',
    token_hash   TEXT NOT NULL,
    info_json    TEXT,
    labels_json  TEXT,
    connected_at INTEGER,
    last_heartbeat INTEGER,
    registered_at INTEGER NOT NULL
);
```

- **稳定节点 ID**：首次注册分配 UUID，Agent 本地持久化
- **心跳批量持久化**：内存中攒 30s 后批量刷盘
- **启动行为**：加载所有节点，全部标记 offline，Agent 重连后回到 online

### 3. 路由器

```
CLI 请求 → 认证（JWT 拦截器）→ 鉴权（检查 node_ids）→ 查找节点
  → 离线？返回 UNAVAILABLE
  → 在线？发 TaskSignal → 桥接 CLI stream ↔ Agent stream
```

### 4. 认证模块

- **Token 认证**：使用预签发 Token，`init` 生成 admin token + 初始 join token
- **JWT Token**：HMAC-SHA256，含 JTI（唯一 ID）
- **Token 持久化**：SQLite，签发时自动 Insert
- **吊销检查**：启动时加载已吊销 JTI 到内存 set，O(1) 拦截
- **gRPC 拦截器**：除 JoinAgent 和 Connect 外所有 RPC 都需认证

Token 类型：

| 类型 | 包含 | 生命周期 |
|------|------|----------|
| CLI Token | `sub`, `node_ids`, `jti`, `exp`, `iat` | 永不过期（`init` 签发） |
| Agent Token | `node_id`, `jti` | 永不过期（`join` 时签发） |
| Join Token | 明文，DB 存 hash | 可配置（最大使用次数 / 过期时间） |

### 5. TLS

- **TLS 默认关闭** — 明文 gRPC，适用于内网
- **Auto-TLS**：`tls.auto: true` 时，ECDSA P-256 CA（10 年）+ 服务端证书（1 年），30 天内过期自动续签
- **自带证书**：配置 `tls.cert` + `tls.key`

### 6. 审计日志

SQLite 存储，异步缓冲写入，不阻塞业务。记录每个操作的时间、用户、节点、动作、结果、耗时。

### 7. 共享数据库

所有持久状态（审计除外）在**单一 SQLite 数据库**（WAL 模式）：

```go
db, err := sql.Open("sqlite", dataDBPath)
db.Exec("PRAGMA journal_mode=WAL")

registryStore  := registry.NewSQLiteStoreFromDB(db)
tokenStore     := auth.NewTokenStoreFromDB(db)
joinTokenStore := auth.NewJoinTokenStoreFromDB(db)
```

## 启动流程

```
1. 加载配置（文件 + 环境变量）
2. 打开共享 SQLite（WAL 模式）
3. 初始化注册表 → 加载节点（标记 offline）
4. 初始化 Token 存储 → 加载已吊销 JTI
5. 初始化 Join Token 存储
6. Auto-TLS：生成或加载证书（如启用）
7. 构建 gRPC 服务（含认证拦截器）
8. 初始化审计
9. 注册所有 gRPC 服务
10. 启动心跳监控
11. 开始服务
```

## 优雅关闭

```
1. 停止接收新连接
2. GracefulStop gRPC
3. 关闭审计（刷新缓冲区）
4. 关闭 Token 存储
5. 关闭注册表（刷新心跳批次）
6. 关闭共享数据库
7. 退出
```

## 配置

详见 [配置参考](configuration.md)。
