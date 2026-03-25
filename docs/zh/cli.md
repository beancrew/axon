# Axon CLI 参考

> [English Version](../cli.md)

## 概述

`axon` 是一个无状态的单一二进制文件。读取本地配置 `~/.axon/config.yaml` 获取 Server 地址和 Token，通过 gRPC 与 Server 通信。

## 全局参数

```
--ca-cert <path>     TLS 验证用的 CA 证书路径
--tls-insecure       跳过 TLS 验证（仅开发用）
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
| `axon auth rotate` | \[未实现\] 吊销当前 Token + 重新登录 |

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

## 配置命令

```bash
axon config set server axon.example.com:9090
axon config get server
axon version
```

---

## 命令总览

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
| `user create` | ✅ | unary | ✅ |
| `user list` | ✅ | unary | ✅ |
| `user update` | ✅ | unary | ✅ |
| `user delete` | ✅ | unary | ✅ |
| `config set/get` | ❌ | — | — |
| `version` | ❌ | — | — |

## 退出码

| 码 | 含义 |
|-----|------|
| 0 | 成功 |
| 1 | 通用错误 |
| 2 | 认证错误 |
| 3 | 节点错误（未找到/离线） |
| N | `exec` 时转发远程命令的退出码 |
