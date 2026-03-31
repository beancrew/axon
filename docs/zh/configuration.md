# 配置参考

所有 Axon 配置文件和环境变量的完整参考。

> [English Version](../configuration.md)

---

## Server（`axon-server`）

### 用 `init` 快速配置

推荐用 `axon-server init` 生成配置：

```bash
axon-server init --admin admin --password secret --listen :9090
```

自动生成 `~/.axon-server/config.yaml`，包含随机 JWT 密钥、管理员用户，并创建 SQLite 数据库。完整流程见[快速开始](quickstart.md)。

### 配置文件

路径：通过 `--config` 参数指定（默认：`./config.yaml`）

### 完整示例

```yaml
listen: ":9090"

tls:
  auto: false                             # TLS 默认关闭；设 true 自动生成证书
  dir: "/var/lib/axon-server/tls"         # 自动生成证书的目录（默认：~/.axon-server/tls）
  cert: ""                                # 显式 TLS 证书路径（会覆盖 auto-TLS）
  key: ""                                 # 显式 TLS 私钥路径

auth:
  jwt_signing_key: "${AXON_JWT_SECRET}"   # HMAC-SHA256 签名密钥（必需；init 自动生成）

users:                                    # 引导用户（首次启动时写入数据库）
  - username: admin
    password_hash: "$2a$10$..."           # bcrypt 哈希
    node_ids: ["*"]                       # ["*"] = 可访问所有节点
  - username: deploy-agent
    password_hash: "$2a$10$..."
    node_ids: ["web-1", "web-2"]          # 限制到特定节点

data:
  db_path: "/var/lib/axon-server/axon.db" # SQLite 持久化路径（默认：内存）

heartbeat:
  interval: "10s"                         # 心跳间隔（默认：10s）
  timeout: "30s"                          # 超时标记离线（默认：30s）

audit:
  db_path: "/var/lib/axon-server/audit.db"  # 审计日志 SQLite 路径（默认：内存）
```

### 字段说明

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `listen` | string | `:9090` | gRPC 监听地址 |
| `tls.auto` | bool | `false` | 自动生成自签 CA + 服务端证书 |
| `tls.dir` | string | `~/.axon-server/tls` | 自动生成 TLS 文件的目录 |
| `tls.cert` | string | — | TLS 证书路径（PEM），覆盖 auto-TLS |
| `tls.key` | string | — | TLS 私钥路径（PEM） |
| `auth.jwt_signing_key` | string | — | **必需。** HMAC-SHA256 JWT 签名密钥 |
| `users[].username` | string | — | 引导用户名 |
| `users[].password_hash` | string | — | 密码 bcrypt 哈希 |
| `users[].node_ids` | []string | `["*"]` | 允许的节点 ID；`["*"]` = 全部 |
| `data.db_path` | string | `:memory:` | 节点注册、Token、用户、join token 的 SQLite 路径 |
| `heartbeat.interval` | duration | `10s` | Agent 心跳间隔 |
| `heartbeat.timeout` | duration | `30s` | 离线阈值 |
| `audit.db_path` | string | `:memory:` | 审计日志 SQLite 路径（独立于数据 DB） |

### 环境变量

| 变量 | 覆盖 |
|------|------|
| `AXON_JWT_SECRET` | `auth.jwt_signing_key` |
| `AXON_TLS_CERT` | `tls.cert` |
| `AXON_TLS_KEY` | `tls.key` |
| `AXON_TLS_DIR` | `tls.dir` |

### TLS

TLS **默认关闭** — 明文 gRPC 适用于内网/私有网络。

启用自动 TLS：
- 运行 `axon-server init --tls`
- 或在配置文件中设 `tls.auto: true`
- 或提供显式 `tls.cert` + `tls.key`

#### Auto-TLS 详细说明

当 `tls.auto: true` 且未提供 `tls.cert`/`tls.key` 时：

1. Server 检查 `tls.dir` 下是否有 `ca.crt`
2. 如果没有：生成 ECDSA P-256 CA（10 年有效期）+ 服务端证书（1 年有效期）
3. 如果 `ca.crt` 存在但 `server.crt` 缺失或 30 天内过期：重新生成服务端证书
4. CA 指纹会记录到日志
5. `axon-agent join` 时，CA 证书 PEM 自动下发给 Agent

生成的文件：

```
~/.axon-server/tls/
├── ca.crt          # CA 证书（join 时自动下发给 Agent）
├── ca.key          # CA 私钥（保持安全，0600）
├── server.crt      # 服务端证书
└── server.key      # 服务端私钥（0600）
```

### 数据持久化

所有持久数据（节点注册、Token、用户、join token）存储在单一 SQLite 数据库中（WAL 模式）。通过 `data.db_path` 设置路径：

- `axon-server init` 自动设为 `~/.axon-server/axon.db`
- 未设置时默认为内存数据库（重启丢失）
- 审计日志使用独立 SQLite 文件（`audit.db_path`）

---

## Agent（`axon-agent`）

配置文件路径：`~/.axon-agent/config.yaml`（`axon-agent join` 时自动创建）

### 注册

推荐用 `axon-agent join` 配置 Agent：

```bash
axon-agent join <server-addr> <join-token> --tls-insecure
```

验证 token、获取 Agent JWT、保存配置、启动 Agent。详见[快速开始](quickstart.md)。

### 完整示例

```yaml
server: "axon.example.com:9090"
token: "eyJhbGciOiJIUzI1NiIs..."  # Agent JWT（join 时签发）
node_id: "a1b2c3d4-..."            # Server 在 join 时分配
node_name: "web-1"                  # 用户指定或主机名
labels:
  env: production
  role: web
ca_cert: "/path/to/ca.crt"         # CA 证书（TLS 场景下 join 时自动保存）
```

### 字段说明

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `server` | string | — | **必需。** Server gRPC 地址（`host:port`） |
| `token` | string | — | **必需。** Agent JWT |
| `node_id` | string | — | join 时由 Server 分配 |
| `node_name` | string | 主机名 | 节点显示名称 |
| `labels` | map | — | 键值标签 |
| `ca_cert` | string | — | CA 证书路径（TLS 验证） |

### 环境变量

| 变量 | 覆盖 |
|------|------|
| `AXON_SERVER` | `server` |
| `AXON_TOKEN` | `token` |
| `AXON_CA_CERT` | `ca_cert` |

### TLS 行为

Agent 根据配置选择传输方式：

1. 设置了 `ca_cert` → 使用指定 CA 验证服务端证书
2. 未设置 → 明文 gRPC（无 TLS）

启用自动 TLS 时，`axon-agent join` 会自动保存 Server 的 CA 证书到 `~/.axon-agent/ca.crt` 并配置 Agent 使用。

---

## CLI（`axon`）

配置文件路径：`~/.axon/config.yaml`

### 完整示例

```yaml
server_addr: "axon.example.com:9090"
token: "eyJhbGciOiJIUzI1NiIs..."   # axon config set token 设置
output_format: "table"               # "table" 或 "json"
ca_cert: "/path/to/ca.crt"          # CA 证书（TLS 验证）
```

### 字段说明

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `server_addr` | string | — | Server gRPC 地址 |
| `token` | string | — | JWT token（`axon config set token` 设置） |
| `output_format` | string | `table` | 默认输出格式 |
| `ca_cert` | string | — | CA 证书路径（TLS 验证） |

### 环境变量

| 变量 | 覆盖 |
|------|------|
| `AXON_SERVER` | `server_addr` |
| `AXON_TOKEN` | `token` |
| `AXON_CA_CERT` | `ca_cert` |

### 全局参数

```bash
axon --ca-cert /path/to/ca.crt <command>
```

### 配置命令

```bash
axon config set server axon.example.com:9090
axon config get server
```

支持的 key：`server`, `token`, `output_format`
