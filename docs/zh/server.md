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
| `ManagementService` | CLI | 节点/用户/Token 管理、登录 |

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

- **JWT Token**：HMAC-SHA256，含 JTI（唯一 ID）
- **Token 持久化**：SQLite，Login 时自动 Insert
- **吊销检查**：启动时加载已吊销 JTI 到内存 set，O(1) 拦截
- **gRPC 拦截器**：除 Login 和 Connect 外所有 RPC 都需认证

### 5. 用户存储（SQLite）

```sql
CREATE TABLE users (
    username      TEXT PRIMARY KEY,
    password_hash TEXT NOT NULL,     -- bcrypt
    node_ids      TEXT NOT NULL,     -- JSON 数组
    created_at    INTEGER NOT NULL,
    updated_at    INTEGER NOT NULL,
    disabled      INTEGER NOT NULL DEFAULT 0
);
```

- **引导**：配置文件用户通过 `INSERT OR IGNORE` 写入
- **登录流程**：SQLite 查找用户 → 检查 disabled → 验证 bcrypt → 签发 JWT + 持久化 Token

### 6. TLS

- **Auto-TLS（默认）**：ECDSA P-256 CA（10 年）+ 服务端证书（1 年），30 天内过期自动续签
- **自带证书**：配置 `tls.cert` + `tls.key`
- **禁用 TLS**：`tls.auto: false`，打印警告

### 7. 审计日志

SQLite 存储，异步缓冲写入，不阻塞业务。记录每个操作的时间、用户、节点、动作、结果、耗时。

### 8. 共享数据库

所有持久状态（审计除外）在**单一 SQLite 数据库**（WAL 模式）：

```go
db, err := sql.Open("sqlite", dataDBPath)
db.Exec("PRAGMA journal_mode=WAL")

registryStore := registry.NewSQLiteStoreFromDB(db)
tokenStore    := auth.NewTokenStoreFromDB(db)
userStore     := auth.NewUserStoreFromDB(db)
```

## 启动流程

```
1. 加载配置（文件 + 环境变量）
2. 打开共享 SQLite（WAL 模式）
3. 初始化注册表 → 加载节点（标记 offline）
4. 初始化 Token 存储 → 加载已吊销 JTI
5. 初始化用户存储 → 引导配置用户
6. Auto-TLS：生成或加载证书
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
4. 关闭用户存储
5. 关闭 Token 存储
6. 关闭注册表（刷新心跳批次）
7. 关闭共享数据库
8. 退出
```

## 配置

详见 [配置参考](configuration.md)。
