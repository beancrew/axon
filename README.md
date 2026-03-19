# Axon

**The nerve connecting AI agents to real machines.**

Axon is infrastructure for AI agents. It lets agents operate remote machines — execute commands, read/write files, forward ports — as naturally as operating locally.

No SSH keys to manage. No YAML to write. No complex APIs to learn. Just a CLI that any agent already knows how to use.

## Why Axon?

Today's infrastructure is built for humans: web dashboards, SSH terminals, config files. AI agents can't use any of that natively.

Axon bridges this gap. It provides **low-level, composable primitives** that agents combine with skills (knowledge) to accomplish any task. We don't over-abstract — agents are smart enough to figure out the "how" when given the right tools.

```
AI Agent (any framework)
    │
    │  CLI (exec / read / write / forward)
    ▼
┌────── Axon Server ──────┐
│  Auth · Routing · Audit  │
└──────────────────────────┘
    │         │         │
    ▼         ▼         ▼
  Node A    Node B    Node C
  (any machine: bare metal / VM / container / edge device)
```

## Core Principles

1. **Low-level primitives, not high-level abstractions** — We provide `exec`, `read`, `write`, `forward`. Not `deploy()` or `check_health()`. Agents decide how to combine them.

2. **CLI-first** — Every agent framework can call CLI tools. No SDK required, no protocol lock-in.

3. **Teach, don't hardcode** — Domain knowledge lives in skills (markdown files), not in code. Want the agent to deploy a Docker service? Write a skill. Want it to run a database backup? Write a skill. The CLI stays the same.

4. **Zero-config for nodes** — Install the agent binary, point it at the server, done. No SSH keys, no firewall rules, no port forwarding.

5. **Agent-native, human-friendly** — Designed for agents, but humans can use it too for debugging and inspection.

## Architecture

### Components

**Axon CLI** (`axon`)
- The interface for agents (and humans)
- Talks to Axon Server via gRPC
- Stateless — all state lives on the server

**Axon Server** (`axon-server`)
- Central control plane, user self-hosted
- Single binary, minimal config
- Manages node registration, authentication, routing
- Audit log: who did what, on which node, when

**Axon Agent** (`axon-agent`)
- Lightweight daemon on each target machine
- Reverse connection to server (no inbound ports needed)
- Auto-reconnect on network failure
- Runs as a system service

### Connection Model

```
axon-agent (node) ──── reverse connect ────→ axon-server ←──── gRPC ──── axon CLI
```

Nodes connect **outbound** to the server. This means:
- No SSH ports to expose
- Works behind NAT, firewalls, corporate networks
- Edge devices, cloud VMs, on-prem servers — all the same

### Node Lifecycle

```
Install axon-agent → Start with server URL + token → Auto-register → Online
                                                                       │
                                                          Agent operates via CLI
                                                                       │
                                                    Kill agent or `axon node remove` → Gone
```

No ceremony. No approval flow. Token-based auth, start and go.

## CLI Reference

### Node Management

```bash
# List all connected nodes
axon node list

# Node details (OS, IP, uptime, agent version)
axon node info <node>

# Remove a node
axon node remove <node>
```

### Core Operations

```bash
# Execute a command on a remote node
axon exec <node> <command>
axon exec web-1 "docker ps"
axon exec db-1 "pg_dump mydb > /tmp/backup.sql"

# Read a file from a remote node
axon read <node> <path>
axon read web-1 /etc/nginx/nginx.conf

# Write a file to a remote node (stdin)
axon write <node> <path> < local-file.yaml
echo "hello" | axon write web-1 /tmp/hello.txt

# Port forwarding (expose remote port locally)
axon forward <node> <local-port>:<remote-port>
axon forward db-1 5432:5432      # Access remote PostgreSQL locally
axon forward web-1 8080:80       # Access remote HTTP locally
```

### That's it.

4 operations. Everything else is a combination of these, guided by skills.

## How Agents Use Axon

An agent doesn't need special integration. It just calls the CLI:

```python
# Any agent framework
result = exec("axon exec web-1 'systemctl status nginx'")
config = exec("axon read web-1 /etc/nginx/nginx.conf")
exec("echo '...' | axon write web-1 /etc/nginx/nginx.conf")
exec("axon exec web-1 'systemctl reload nginx'")
```

Domain knowledge comes from **skills** — markdown files that teach the agent what to do:

```markdown
# skill: deploy-service
## Tools
- axon exec <node> <command>
- axon write <node> <path>

## Steps
1. axon write <node> /opt/<service>/docker-compose.yaml
2. axon exec <node> "cd /opt/<service> && docker compose pull"
3. axon exec <node> "cd /opt/<service> && docker compose up -d"
4. axon exec <node> "docker ps | grep <service>"  # verify
```

Different scenario? Different skill. Same CLI.

## Security

- **Token-based auth** — Server issues tokens, agents present tokens
- **Audit log** — Every operation logged with timestamp, caller, node, command, result
- **No inbound ports on nodes** — Reverse connection only
- **TLS everywhere** — Server ↔ Agent, CLI ↔ Server

## Roadmap

### Phase 1: Foundation
- [ ] axon-server: gRPC server, node registry, auth
- [ ] axon-agent: reverse connection, command execution, file I/O
- [ ] axon CLI: exec, read, write, forward, node management
- [ ] Token-based authentication
- [ ] Audit logging

### Phase 2: Production Hardening
- [ ] Agent auto-update
- [ ] Connection multiplexing
- [ ] Rate limiting and resource quotas
- [ ] Multi-tenant support

### Phase 3: Ecosystem
- [ ] Plugin system for custom node capabilities
- [ ] Web dashboard (read-only status, for humans)
- [ ] Pre-built skills library

## Tech Stack

- **Language**: Go
- **Communication**: gRPC (CLI ↔ Server), WebSocket/gRPC (Server ↔ Agent)
- **Auth**: Token-based (JWT or simple bearer)
- **Build**: Single binary per component, cross-platform

---

# Axon

**连接 AI Agent 与真实机器的神经通路。**

Axon 是为 AI agent 构建的基础设施。它让 agent 操作远程机器——执行命令、读写文件、转发端口——像操作本地一样自然。

不需要管理 SSH 密钥，不需要写 YAML，不需要学复杂 API。一个 CLI，任何 agent 都天然会用。

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

## 架构

### 组件

**Axon CLI** (`axon`)
- Agent（和人）的操作接口
- 通过 gRPC 与 Server 通信
- 无状态——所有状态在 Server 端

**Axon Server** (`axon-server`)
- 中央控制面，用户自部署
- 单二进制，最小配置
- 管理节点注册、认证、路由
- 审计日志：谁、在哪台机器、做了什么、什么时候

**Axon Agent** (`axon-agent`)
- 每台目标机器上的轻量 daemon
- 反向连接到 Server（不需要开入站端口）
- 断线自动重连
- 作为系统服务运行

### 连接模型

```
axon-agent (节点) ──── 反向连接 ────→ axon-server ←──── gRPC ──── axon CLI
```

节点**主动外连**到 Server，意味着：
- 不需要暴露 SSH 端口
- NAT 后面、防火墙后面、企业内网都能用
- 边缘设备、云主机、本地服务器——全部一样

### 节点生命周期

```
安装 axon-agent → 启动（指定 server 地址 + token）→ 自动注册 → 上线
                                                                  │
                                                    Agent 通过 CLI 操作
                                                                  │
                                               kill agent 或 axon node remove → 下线
```

没有仪式。Token 认证，启动即用。

## CLI 参考

### 节点管理

```bash
# 查看所有在线节点
axon node list

# 节点详情（OS、IP、在线时长、agent 版本）
axon node info <node>

# 移除节点
axon node remove <node>
```

### 核心操作

```bash
# 远程执行命令
axon exec <node> <command>
axon exec web-1 "docker ps"
axon exec db-1 "pg_dump mydb > /tmp/backup.sql"

# 读取远程文件
axon read <node> <path>
axon read web-1 /etc/nginx/nginx.conf

# 写文件到远程节点（stdin）
axon write <node> <path> < local-file.yaml
echo "hello" | axon write web-1 /tmp/hello.txt

# 端口转发（远程端口映射到本地）
axon forward <node> <local-port>:<remote-port>
axon forward db-1 5432:5432      # 本地访问远程 PostgreSQL
axon forward web-1 8080:80       # 本地访问远程 HTTP
```

### 就这些。

4 个操作。其他一切都是这些的组合，由 skill 指导。

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
## 工具
- axon exec <node> <command>
- axon write <node> <path>

## 步骤
1. axon write <node> /opt/<service>/docker-compose.yaml
2. axon exec <node> "cd /opt/<service> && docker compose pull"
3. axon exec <node> "cd /opt/<service> && docker compose up -d"
4. axon exec <node> "docker ps | grep <service>"  # 验证
```

不同场景？换个 skill。CLI 不变。

## 安全

- **Token 认证** — Server 发 token，使用时出示 token
- **审计日志** — 每个操作记录时间、调用者、节点、命令、结果
- **节点无入站端口** — 纯反向连接
- **全链路 TLS** — Server ↔ Agent，CLI ↔ Server

## 路线图

### Phase 1: 基础
- [ ] axon-server：gRPC 服务、节点注册、认证
- [ ] axon-agent：反向连接、命令执行、文件读写
- [ ] axon CLI：exec、read、write、forward、节点管理
- [ ] Token 认证
- [ ] 审计日志

### Phase 2: 生产加固
- [ ] Agent 自动更新
- [ ] 连接多路复用
- [ ] 限流和资源配额
- [ ] 多租户支持

### Phase 3: 生态
- [ ] 插件系统（自定义节点能力）
- [ ] Web 控制台（只读状态，给人看的）
- [ ] 预置 skill 库

## 技术栈

- **语言**：Go
- **通信**：gRPC（CLI ↔ Server）、WebSocket/gRPC（Server ↔ Agent）
- **认证**：Token（JWT 或简单 Bearer）
- **构建**：每个组件单二进制，跨平台
