# 快速开始

5 分钟内跑通 Axon：启动 Server、连接 Agent、执行第一条远程命令。

> [English Version](../quickstart.md)

## 前置条件

- Go 1.25+ 已安装
- 两台机器（或同一台机器上的两个终端用于测试）

## 1. 编译

```bash
git clone https://github.com/garysng/axon.git
cd axon
make build
```

产出三个二进制文件在 `bin/` 下：

| 二进制 | 用途 |
|--------|------|
| `axon` | CLI，人和 AI agent 的操作接口 |
| `axon-server` | 中心控制面 |
| `axon-agent` | 目标机器上的守护进程 |

## 2. 启动 Server

创建最小配置文件：

```yaml
# server.yaml
listen: ":9090"

auth:
  jwt_signing_key: "your-secret-key-change-me"

users:
  - username: admin
    password_hash: "$2a$10$..."   # 密码的 bcrypt 哈希
    node_ids: ["*"]               # 可访问所有节点
```

生成密码的 bcrypt 哈希：

```bash
# 使用 htpasswd（Apache 工具）
htpasswd -nbBC 10 "" your-password | cut -d: -f2

# 或使用 Python
python3 -c "import bcrypt; print(bcrypt.hashpw(b'your-password', bcrypt.gensalt()).decode())"
```

启动 Server：

```bash
./bin/axon-server --config server.yaml
```

你会看到：

```
server: auto-TLS: generated CA cert ~/.axon-server/tls/ca.crt (SHA-256: AA:BB:CC:...)
server: gRPC listening on :9090 (TLS)
```

**Auto-TLS** 会自动生成自签 CA 和服务端证书，无需手动配置证书。

## 3. 连接 Agent

在目标机器上（或另一个终端）启动 Agent：

```bash
./bin/axon-agent start \
  --server localhost:9090 \
  --token your-agent-token \
  --name my-node \
  --tls-insecure    # 测试环境使用自签证书
```

或使用 CA 证书做正式 TLS 验证：

```bash
./bin/axon-agent start \
  --server localhost:9090 \
  --token your-agent-token \
  --name my-node \
  --ca-cert ~/.axon-server/tls/ca.crt
```

Agent 连接 Server 后自动注册。

## 4. CLI 登录

```bash
# 配置 Server 地址
./bin/axon config set server localhost:9090

# 登录（提示输入用户名/密码）
./bin/axon auth login --tls-insecure
```

或使用 CA 证书：

```bash
./bin/axon auth login --ca-cert ~/.axon-server/tls/ca.crt
```

## 5. 执行操作

```bash
# 列出已连接的节点
axon node list

# 在远程节点上执行命令
axon exec my-node "hostname"
axon exec my-node "ls -la /tmp"

# 读取文件
axon read my-node /etc/hostname

# 写入文件
echo "hello from axon" | axon write my-node /tmp/hello.txt

# 端口转发
axon forward my-node 8080:80
# 现在 localhost:8080 → my-node:80
```

## 6. 用户管理（可选）

```bash
# 创建新用户
axon user create deploy-bot --node-ids web-1,web-2

# 列出用户
axon user list

# 更新节点权限
axon user update deploy-bot --node-ids web-1,web-2,db-1

# 删除用户
axon user delete deploy-bot
```

## 7. Token 管理（可选）

```bash
# 列出已签发的 Token
axon auth list-tokens

# 吊销 Token
axon auth revoke <token-id>
```

## TLS 选项

| 场景 | Server 配置 | 客户端/Agent 参数 |
|------|-----------|-----------------|
| Auto-TLS（默认） | 不配 `tls.cert`/`tls.key` → 自动生成 | `--ca-cert ~/.axon-server/tls/ca.crt` |
| 自带证书 | 配置 `tls.cert` + `tls.key` | 系统 CA 或 `--ca-cert` |
| 禁用 TLS（仅开发） | `tls.auto: false`（不配证书） | `--tls-insecure` |

## 下一步

- [配置参考](configuration.md) — Server、Agent、CLI 的全部配置选项
- [架构概览](architecture.md) — 组件如何协作
- [CLI 参考](cli.md) — 完整命令参考
