# 配置参考

Axon 所有配置文件和环境变量的完整参考。

> [English Version](../configuration.md)

---

## Server（`axon-server`）

配置文件路径：通过 `--config` 参数指定（默认：`./config.yaml`）

### 完整示例

```yaml
listen: ":9090"

tls:
  auto: true                              # 自动生成自签 CA + 服务端证书（无 cert/key 时默认开启）
  dir: "/var/lib/axon-server/tls"         # 自动生成证书的目录（默认：~/.axon-server/tls）
  cert: ""                                # 显式 TLS 证书路径（禁用 auto-TLS）
  key: ""                                 # 显式 TLS 私钥路径

auth:
  jwt_signing_key: "${AXON_JWT_SECRET}"   # HMAC-SHA256 签名密钥（必填）

users:                                    # 引导用户（首次启动时写入数据库）
  - username: admin
    password_hash: "$2a$10$..."           # bcrypt 哈希
    node_ids: ["*"]                       # ["*"] = 可访问所有节点
  - username: deploy-agent
    password_hash: "$2a$10$..."
    node_ids: ["web-1", "web-2"]          # 限制到指定节点

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
| `tls.auto` | bool | `true`（无 cert/key 时） | 自动生成自签 CA + 服务端证书 |
| `tls.dir` | string | `~/.axon-server/tls` | 自动生成证书的目录 |
| `tls.cert` | string | — | TLS 证书路径（PEM），设置后禁用 auto-TLS |
| `tls.key` | string | — | TLS 私钥路径（PEM） |
| `auth.jwt_signing_key` | string | — | **必填。** JWT 签名密钥 |
| `users[].username` | string | — | 引导用户名 |
| `users[].password_hash` | string | — | bcrypt 密码哈希 |
| `users[].node_ids` | []string | `["*"]` | 允许访问的节点 |
| `heartbeat.interval` | duration | `10s` | Agent 心跳间隔 |
| `heartbeat.timeout` | duration | `30s` | 离线阈值 |
| `audit.db_path` | string | `:memory:` | 审计日志 SQLite 路径 |

### 环境变量

| 变量 | 覆盖 |
|------|------|
| `AXON_JWT_SECRET` | `auth.jwt_signing_key` |
| `AXON_TLS_CERT` | `tls.cert` |
| `AXON_TLS_KEY` | `tls.key` |
| `AXON_TLS_DIR` | `tls.dir` |

### Auto-TLS 细节

当 `tls.auto` 启用（或未显式禁用）且未配置 `tls.cert`/`tls.key` 时：

1. 检查 `tls.dir` 下是否有 `ca.crt`
2. 如果没有：生成 ECDSA P-256 CA（10 年有效期）+ 服务端证书（1 年有效期）
3. 如果 `ca.crt` 存在但 `server.crt` 缺失或 30 天内过期：重新生成服务端证书
4. 日志输出 CA 指纹，方便分发给 Agent/CLI

生成的文件：

```
~/.axon-server/tls/
├── ca.crt          # CA 证书（分发给 Agent 和 CLI）
├── ca.key          # CA 私钥（保密，权限 0600）
├── server.crt      # 服务端证书
└── server.key      # 服务端私钥（权限 0600）
```

### 引导用户

配置文件中的用户在启动时通过 `INSERT OR IGNORE` 写入数据库——已存在的用户不会被覆盖。初始引导后，通过 `axon user` CLI 命令或 gRPC RPC 管理用户。

---

## Agent（`axon-agent`）

配置文件路径：`~/.axon-agent/config.yaml`（首次 `start` 时自动创建）

### 完整示例

```yaml
server: "axon.example.com:9090"
token: "agent-token-xxx"
node_id: "a1b2c3d4"              # 首次注册后由 Server 分配
node_name: "web-1"               # 用户指定或使用主机名
labels:
  env: production
  role: web
ca_cert: "/path/to/ca.crt"       # TLS 验证用的 CA 证书
tls_insecure: false               # 跳过 TLS 验证（仅开发用）
```

### 字段说明

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `server` | string | — | **必填。** Server gRPC 地址 |
| `token` | string | — | **必填。** Agent 注册 Token |
| `node_id` | string | — | Server 分配的稳定节点 ID |
| `node_name` | string | 主机名 | 节点名称 |
| `labels` | map | — | 键值标签 |
| `ca_cert` | string | — | CA 证书路径 |
| `tls_insecure` | bool | `false` | 跳过 TLS 验证 |

### 环境变量

| 变量 | 覆盖 |
|------|------|
| `AXON_SERVER` | `server` |
| `AXON_TOKEN` | `token` |
| `AXON_CA_CERT` | `ca_cert` |

### TLS 行为

Agent 的 TLS 三路选择：

1. `tls_insecure: true` → 不验证 TLS（明文 gRPC）
2. `ca_cert` 已设置 → 用指定 CA 验证服务端证书
3. 都未设置 → 用系统 CA 池验证

---

## CLI（`axon`）

配置文件路径：`~/.axon/config.yaml`

### 完整示例

```yaml
server_addr: "axon.example.com:9090"
token: "eyJhbGciOiJIUzI1NiIs..."
output_format: "table"            # "table" 或 "json"
ca_cert: "/path/to/ca.crt"       # TLS 验证用的 CA 证书
tls_insecure: false               # 跳过 TLS 验证
```

### 字段说明

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `server_addr` | string | — | Server gRPC 地址 |
| `token` | string | — | JWT Token（`axon auth login` 设置） |
| `output_format` | string | `table` | 默认输出格式 |
| `ca_cert` | string | — | CA 证书路径 |
| `tls_insecure` | bool | `false` | 跳过 TLS 验证 |

### 环境变量

| 变量 | 覆盖 |
|------|------|
| `AXON_SERVER` | `server_addr` |
| `AXON_TOKEN` | `token` |
| `AXON_CA_CERT` | `ca_cert` |

### 全局参数

这些参数覆盖配置文件的值：

```bash
axon --ca-cert /path/to/ca.crt <command>
axon --tls-insecure <command>
```
