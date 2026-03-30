# Axon

**连接 AI Agent 与真实机器的神经通路。**

Axon 是为 AI agent 构建的基础设施。它让 agent 操作远程机器——执行命令、读写文件、转发端口——像操作本地一样自然。

不需要管理 SSH 密钥，不需要写 YAML，不需要学复杂 API。一个 CLI，任何 agent 都天然会用。

> [English](README.md)

## 为什么做 Axon？

今天的基础设施是为人构建的：Web 管理后台、SSH 终端、配置文件。AI agent 无法原生使用这些东西。

Axon 填补这个空白。它提供**底层、可组合的原子操作**，agent 通过 skill（知识文件）学会如何组合这些操作完成任意任务。我们不做过度封装——agent 足够聪明，给它工具就够了。

```
AI Agent（任意框架）
    │
    │  CLI (exec / read / write / forward)
    ▼
┌────── Axon Server ──────┐
│  认证 · 路由 · 审计日志  │
└──────────────────────────┘
    │         │         │
    ▼         ▼         ▼
  节点 A    节点 B    节点 C
  (任何机器：物理机 / 虚拟机 / 容器 / 边缘设备)
```

## 核心原则

1. **底层原语，不是高层抽象** — 提供 `exec`、`read`、`write`、`forward`。不提供 `deploy()` 或 `check_health()`。让 agent 自己决定怎么组合。

2. **CLI 优先** — 所有 agent 框架都能调 CLI。不需要 SDK，不锁定协议。

3. **教而不是写死** — 领域知识放在 skill（markdown 文件）里，不写在代码里。想让 agent 部署 Docker 服务？写个 skill。想让它做数据库备份？写个 skill。CLI 不变。

4. **节点零配置** — 装上 agent 二进制，指向 server，完事。不需要 SSH 密钥、防火墙规则、端口映射。

5. **Agent 原生，人也能用** — 为 agent 设计，但人也可以直接用来调试和排查。

## 快速开始

```bash
# 安装（自动检测 OS/架构）
curl -fsSL https://raw.githubusercontent.com/beancrew/axon/main/scripts/install.sh | sh -s -- server
curl -fsSL https://raw.githubusercontent.com/beancrew/axon/main/scripts/install.sh | sh -s -- agent
curl -fsSL https://raw.githubusercontent.com/beancrew/axon/main/scripts/install.sh | sh -s -- cli

# 初始化 server
axon-server init

# 加入节点
axon-agent join <server-addr>:9090 <join-token>

# 使用
axon exec my-node "hostname"
```

→ 完整指南：[Quick Start](docs/quickstart.md)

## CLI 参考

### 节点管理

```bash
axon node list                 # 查看所有在线节点
axon node info <node>          # 节点详情
axon node remove <node>        # 移除节点
```

### 核心操作

```bash
# 远程执行命令
axon exec <node> <command>
axon exec web-1 "docker ps"
axon exec db-1 "pg_dump mydb > /tmp/backup.sql"

# 读取远程文件
axon read <node> <path>
axon read web-1 /etc/nginx/nginx.conf > local.conf

# 写文件到远程节点（stdin）
echo "hello" | axon write web-1 /tmp/hello.txt
cat config.yaml | axon write web-1 /etc/app/config.yaml

# 端口转发
axon forward create db-1 5432:5432    # 非阻塞，daemon 管理
axon forward list                      # 列出活跃转发
axon forward delete <id>               # 删除转发
axon forward db-1 5432:5432           # 阻塞式简写
```

4 个操作。其他一切都是这些的组合，由 skill 指导。

→ 完整参考：[CLI Reference](docs/cli.md)

## Agent 如何使用 Axon

Agent 不需要特殊集成，直接调 CLI：

```python
# 任意 agent 框架
result = exec("axon exec web-1 'systemctl status nginx'")
config = exec("axon read web-1 /etc/nginx/nginx.conf")
exec("echo '...' | axon write web-1 /etc/nginx/nginx.conf")
exec("axon exec web-1 'systemctl reload nginx'")
```

领域知识来自 **skill**——教 agent 怎么做的 markdown 文件：

```markdown
# skill: deploy-service
## 步骤
1. axon write <node> /opt/<service>/docker-compose.yaml
2. axon exec <node> "cd /opt/<service> && docker compose pull"
3. axon exec <node> "cd /opt/<service> && docker compose up -d"
4. axon exec <node> "docker ps | grep <service>"  # 验证
```

不同场景？换个 skill。CLI 不变。

本仓库包含一个 [Axon AgentSkill](skills/axon/)。

## 功能特性

- **远程执行** — 在任意节点运行命令，实时 stdout/stderr 流式输出
- **文件操作** — 通过 stdin/stdout 读写远程文件
- **端口转发** — 远程端口映射到本地，支持 daemon 管理多转发
- **反向连接** — 节点主动外连 server，无需开入站端口，NAT/防火墙无障碍
- **Token 认证** — JWT + JTI + 吊销，join-token 快速注册 agent
- **Token 管理** — 通过 CLI 列表、吊销 token 和 join token
- **自动 TLS** — 自签 CA + 服务器证书自动生成，也支持自带证书
- **审计日志** — 每个操作记录时间、调用者、节点和结果
- **单二进制部署** — 每个组件一个二进制，跨平台（Linux/macOS，amd64/arm64）
- **Server daemon 模式** — `--daemon` 后台运行，`axon-server stop` 停止

## 安全模型

- **节点无入站端口** — Agent 仅外连，不开 SSH，不开端口
- **Token 吊销** — 被泄露的 token 可通过 CLI 即时吊销
- **专利保护** — Apache 2.0 许可证包含专利授权和报复条款
- **完整审计链** — 谁、在哪台机器、什么时候、做了什么——全部记录

## 文档

- [Quick Start Guide](docs/quickstart.md) — 5 分钟上手
- [Configuration Reference](docs/configuration.md) — 所有配置选项
- [Architecture Overview](docs/architecture.md) — 组件架构
- [CLI Reference](docs/cli.md) — 完整命令参考
- [Protocol Design](docs/protocol.md) — gRPC/protobuf 协议细节
- [Server Design](docs/server.md) — Server 设计文档
- [Agent Design](docs/agent.md) — Agent 设计文档

## 贡献

参见 [CONTRIBUTING.md](CONTRIBUTING.md)。

## 许可证

Apache License 2.0 — 参见 [LICENSE](LICENSE)。
