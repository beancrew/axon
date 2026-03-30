# 快速开始

5 分钟内跑起来：一个 Server、一个 Agent、执行第一条远程命令。

> [English Version](../quickstart.md)

## 前置条件

- Go 1.25+ 已安装（或下载预编译二进制 — 见[安装脚本](#安装脚本)）
- 两台机器（或同一台机器两个终端测试）

## 1. 编译

```bash
git clone https://github.com/beancrew/axon.git
cd axon
make build
```

产出三个二进制文件在 `bin/` 下：

| 二进制 | 用途 |
|--------|------|
| `axon` | CLI，给人和 AI agent 用 |
| `axon-server` | 中央控制面 |
| `axon-agent` | 每台目标机器上的守护进程 |

## 2. 初始化 Server

一条命令搞定 — 生成配置、JWT 密钥、admin token 和 join token：

```bash
./bin/axon-server init
```

输出：

```
Server initialized

   Config:     ~/.axon-server/config.yaml
   Database:   ~/.axon-server/axon.db
   Listen:     :9090

Admin token (save this):
   eyJhbGciOiJIUzI1NiIs...

Start the server:
   axon-server start --config ~/.axon-server/config.yaml

Join a node:
   axon-agent join <SERVER_IP>:9090 axon-join-ab12cd34...

Use CLI:
   axon config set server <SERVER_IP>:9090
   axon config set token <admin-token>
```

**保存 admin token 和 join token** — 配置 CLI 和注册 Agent 都要用。

### `init` 选项

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `--listen` | `:9090` | gRPC 监听地址 |
| `--data-dir` | `~/.axon-server` | 数据目录（配置、数据库、证书） |
| `--tls` | `false` | 启用自动 TLS（自签 CA + 服务端证书） |
| `--force` | `false` | 覆盖已有配置 |

## 3. 启动 Server

```bash
./bin/axon-server start --config ~/.axon-server/config.yaml
```

```
server: gRPC listening on :9090
```

> **注意：** TLS 默认关闭 — 使用明文 gRPC，适用于内网/私有网络。通过 init 时加 `--tls` 或在配置中设 `tls.auto: true` 启用。见 [TLS 选项](#tls-选项)。

## 4. 加入 Agent

在目标机器上，一条命令注册节点：

```bash
./bin/axon-agent join <SERVER_IP>:9090 axon-join-ab12cd34... --tls-insecure
```

> 连接无 TLS 的 Server 时需加 `--tls-insecure`。如果启用了 TLS，去掉此参数或改用 `--ca-cert`。

输出：

```
Node enrolled successfully

   Node ID:    a1b2c3d4-...
   Node Name:  my-node
   Server:     10.0.1.1:9090
   Config:     ~/.axon-agent/config.yaml

Starting agent... (Ctrl+C to stop)
```

### `join` 选项

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `--name` | 主机名 | 节点显示名称 |
| `--labels` | — | 标签 `key=value`（可重复） |
| `--ca-cert` | — | CA 证书路径（TLS 验证） |
| `--tls-insecure` | `false` | 跳过 TLS（连接无 TLS 的 Server） |

### 重新连接

首次 join 后，重连只需：

```bash
./bin/axon-agent start
```

## 5. 配置 CLI

```bash
# 设置 Server 地址
./bin/axon config set server <SERVER_IP>:9090

# 设置 init 输出的 admin token
./bin/axon config set token <admin-token>
```

## 6. 执行命令

```bash
# 列出已连接节点
axon node list

# 在远程节点执行命令
axon exec my-node "hostname"
axon exec my-node "ls -la /tmp"

# 读取文件
axon read my-node /etc/hostname

# 写入文件
echo "hello from axon" | axon write my-node /tmp/hello.txt

# 端口转发（非阻塞）
axon forward create my-node 8080:80
# Forward f1a2b3c4 created: 127.0.0.1:8080 → my-node:80

axon forward list
axon forward delete f1a2b3c4
```

## 7. 管理 Token

```bash
# 列出活跃 token
axon token list

# 创建新 join token（可选限制）
axon token create-join --max-uses 10 --expires 24h

# 列出所有 join token
axon token list-join

# 吊销 token
axon token revoke-join <token-id>
```

## TLS 选项

TLS **默认关闭** — 明文 gRPC 适用于内网/私有网络。

| 场景 | Server 配置 | Client/Agent 参数 |
|------|------------|-------------------|
| 无 TLS（默认） | `axon-server init`（不加 `--tls`） | `--tls-insecure`（用于 `axon-agent join`） |
| 自动 TLS | `axon-server init --tls` | `--ca-cert ~/.axon-server/tls/ca.crt` |
| 自备证书 | 配置 `tls.cert` + `tls.key` | 系统 CA 库 或 `--ca-cert` |

启用自动 TLS 时，Server 生成自签 CA 和服务端证书（ECDSA P-256）。Agent `join` 时 CA 证书会自动下发。

## 安装脚本

从 GitHub Releases 下载预编译二进制：

```bash
# 安装 Server
curl -fsSL https://raw.githubusercontent.com/beancrew/axon/main/scripts/install.sh | sh -s -- server

# 安装 Agent
curl -fsSL https://raw.githubusercontent.com/beancrew/axon/main/scripts/install.sh | sh -s -- agent

# 安装 CLI
curl -fsSL https://raw.githubusercontent.com/beancrew/axon/main/scripts/install.sh | sh -s -- cli
```

脚本自动检测 OS/架构，安装到 `/usr/local/bin`（无 root 权限时安装到 `~/.axon/bin`）。

## 下一步

- [配置参考](configuration.md) — 所有配置选项
- [架构总览](architecture.md) — 组件如何配合
- [CLI 参考](cli.md) — 完整命令参考
