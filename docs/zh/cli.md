# Axon CLI 参考

> [English Version](../cli.md)

## 概述

Axon 有三个二进制 — `axon`（CLI）、`axon-server` 和 `axon-agent`。本页覆盖所有命令。

## `axon` — CLI

读取本地配置 `~/.axon/config.yaml` 获取 Server 地址和 Token，通过 gRPC 与 Server 通信。

### 全局参数

```
--ca-cert <path>     TLS 验证用的 CA 证书路径
```

---

## 节点命令

| 命令 | 说明 |
|------|------|
| `axon node list` | 列出所有注册节点 |
| `axon node info <node>` | 查看节点详情 |
| `axon node remove <node>` | 移除节点 |

---

## 核心操作

### `axon exec <node> <command>`

在远程节点执行命令，实时流式输出 stdout/stderr。

```bash
axon exec web-1 "docker ps"
axon exec web-1 "tail -f /var/log/app.log"   # Ctrl+C 停止
```

参数：`--timeout <seconds>`、`--env KEY=VALUE`、`--workdir <path>`

### `axon read <node> <path>`

读取远程文件，输出到 stdout。

```bash
axon read web-1 /etc/nginx/nginx.conf > local-copy.conf
```

### `axon write <node> <path>`

从 stdin 写入远程文件。

```bash
echo "hello" | axon write web-1 /tmp/hello.txt
```

参数：`--mode <perm>`（默认 0644）

### `axon forward <node> <local-port>:<remote-port>`

端口转发。

```bash
axon forward db-1 5432:5432
# localhost:5432 → db-1:5432
```

参数：`--bind <address>`（默认 127.0.0.1）

---

## 认证命令

| 命令 | 说明 |
|------|------|
| `axon auth login` | 登录获取 JWT Token |
| `axon auth token` | 显示当前 Token |
| `axon auth list-tokens` | 列出所有已签发的 Token |
| `axon auth revoke <id>` | 吊销指定 Token |

---

## 用户命令

| 命令 | 说明 |
|------|------|
| `axon user create <username>` | 创建用户（提示输入密码） |
| `axon user list` | 列出所有用户 |
| `axon user update <username>` | 更新用户的节点权限或密码 |
| `axon user delete <username>` | 删除用户（有确认提示） |

`create` 参数：`--node-ids <ids>`（逗号分隔，默认 `*`）
`update` 参数：`--node-ids <ids>`、`--password`
`delete` 参数：`--force` / `-f`（跳过确认）

---

## Join Token 命令

### `axon token create-join`

创建新的 join token。

```bash
axon token create-join --max-uses 10 --expires 24h
```

参数：`--max-uses <n>`（0 = 无限制）、`--expires <duration>`（如 `24h`、`168h`）

### `axon token list-join`

列出所有 join token。

```
ID        USES  MAX  REVOKED  EXPIRES                    CREATED
a1b2c3d4  3     10   no       2026-03-26T18:00:00+08:00  2026-03-25T18:00:00+08:00
```

### `axon token revoke-join <token-id>`

吊销指定 join token。

```bash
axon token revoke-join a1b2c3d4
```

---

## 配置命令

```bash
axon config set server axon.example.com:9090
axon config get server
axon version
```

---

## `axon-server` — Server 命令

### `axon-server init`

初始化 Server 配置。生成配置文件、JWT 密钥、管理员用户、SQLite 数据库和初始 join token。

```bash
axon-server init --admin admin --password secret
```

参数：
- `--listen <addr>` — gRPC 监听地址（默认：`:9090`）
- `--admin <name>` — 管理员用户名（默认：`admin`）
- `--password <pass>` — 管理员密码（不提供则交互输入）
- `--data-dir <path>` — 数据目录（默认：`~/.axon-server`）
- `--tls` — 启用自动 TLS
- `--force` — 覆盖已有配置

### `axon-server start`

启动 Server。

```bash
axon-server start --config ~/.axon-server/config.yaml
```

### `axon-server version`

显示版本信息。

---

## `axon-agent` — Agent 命令

### `axon-agent join <server-addr> <join-token>`

注册节点。验证 join token、获取 Agent JWT、保存配置、启动 Agent。

```bash
axon-agent join 10.0.1.1:9090 axon-join-ab12cd34... --tls-insecure
```

参数：
- `--name <name>` — 节点名称（默认：主机名）
- `--labels <key=value>` — 标签（可重复）
- `--ca-cert <path>` — CA 证书路径
- `--tls-insecure` — 跳过 TLS

### `axon-agent start`

使用已保存的配置重新连接。

### `axon-agent stop`

停止 Agent。

### `axon-agent status`

显示 Agent 状态。

---

## 命令总览

### `axon` CLI

| 命令 | 需要 Server | gRPC 模式 | 需要认证 |
|------|:----------:|-----------|:-------:|
| `node list` | ✅ | unary | ✅ |
| `node info` | ✅ | unary | ✅ |
| `node remove` | ✅ | unary | ✅ |
| `exec` | ✅ | server stream | ✅ |
| `read` | ✅ | server stream | ✅ |
| `write` | ✅ | client stream | ✅ |
| `forward` | ✅ | bidi stream | ✅ |
| `auth login` | ✅ | unary | ❌ |
| `auth list-tokens` | ✅ | unary | ✅ |
| `auth revoke` | ✅ | unary | ✅ |
| `token create-join` | ✅ | unary | ✅ |
| `token list-join` | ✅ | unary | ✅ |
| `token revoke-join` | ✅ | unary | ✅ |
| `user create` | ✅ | unary | ✅ |
| `user list` | ✅ | unary | ✅ |
| `user update` | ✅ | unary | ✅ |
| `user delete` | ✅ | unary | ✅ |
| `config set/get` | ❌ | — | — |
| `version` | ❌ | — | — |

### `axon-server`

| 命令 | 说明 |
|------|------|
| `init` | 初始化配置、数据库、管理员、join token |
| `start` | 启动 gRPC Server |
| `version` | 显示版本 |

### `axon-agent`

| 命令 | 说明 |
|------|------|
| `join` | 用 join token 注册到 Server |
| `start` | 使用已保存配置重连 |
| `stop` | 停止 Agent |
| `status` | 显示状态 |
| `version` | 显示版本 |

## 退出码

| 码 | 含义 |
|-----|------|
| 0 | 成功 |
| 1 | 通用错误 |
| 2 | 认证错误 |
| 3 | 节点错误（未找到/离线） |
| N | `exec` 时转发远程命令的退出码 |
