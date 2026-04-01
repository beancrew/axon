# Axon 架构概览

> [English Version](../architecture.md)

## 组件

Axon 由三个组件组成，每个都是单一静态二进制文件。

```
axon-cli ── gRPC ──→ axon-server ←── gRPC ── axon-agent
                     (控制面)              (反向连接)
```

| 组件 | 二进制 | 角色 |
|------|--------|------|
| **axon-cli** | `axon` | 用户/Agent 接口，无状态，通过 gRPC 与 Server 通信 |
| **axon-server** | `axon-server` | 中心控制面，节点注册、认证、路由、审计 |
| **axon-agent** | `axon-agent` | 目标机器上的轻量守护进程，反向连接 Server |

## 通信

**全链路 gRPC over HTTP/2。** 不用 WebSocket、REST、自定义 TCP。

| 链路 | 协议 | 模式 | 原因 |
|------|------|------|------|
| CLI → Server | gRPC | unary + 各种 stream | exec 需要流式，forward 需要双向 |
| Agent → Server（控制面）| gRPC | BiDi stream（长连接）| 心跳、注册、节点信息 |
| Agent → Server（数据面）| gRPC | 按任务独立 stream | exec/read/write/forward 各自独立 |

## 连接模型

Agent **主动外连** Server，节点不需要开放入站端口。

```
axon-agent ──── 主动 gRPC ────→ axon-server ←──── gRPC ──── axon-cli
   (NAT/防火墙后)                 (公网)             (任意位置)
```

## 控制面 vs 数据面

```
┌─────────── HTTP/2 连接 ─────────────┐
│                                      │
│  控制面（1 个长连接 stream）           │
│  ├── 注册                            │
│  ├── 心跳                            │
│  └── 节点信息上报                     │
│                                      │
│  数据面（按需 stream）                │
│  ├── exec stream（每条命令）          │
│  ├── read stream（每个文件）          │
│  ├── write stream（每个文件）         │
│  └── forward stream（每个 TCP 连接）  │
│                                      │
└──────────────────────────────────────┘
```

## 认证

基于预签发 Token。`axon-server init` 生成 admin token + 初始 join token。

| Token 类型 | 作用域 | 生命周期 |
|-----------|--------|---------|
| CLI Token | 绑定身份 + 允许的节点列表 | 永不过期（`init` 签发） |
| Agent Token | 绑定节点身份 | 永不过期（`join` 时签发） |
| Join Token | Agent 注册 | 可配置（最大使用次数 / 过期时间） |

### Token 管理

- 每个 Token 有唯一 **JTI**（JWT ID）
- Token 持久化到 SQLite，可列表和吊销
- 吊销的 Token 在内存中 O(1) 检查（gRPC 拦截器）
- CLI 命令：`axon token list`、`axon token revoke <id>`
- Join Token 管理：`axon token create-join`、`axon token list-join`、`axon token revoke-join`

## 持久化

所有持久状态存储在**单一共享 SQLite 数据库**（WAL 模式）：

| 表 | 内容 |
|-----|------|
| `nodes` | 节点注册表（ID、名称、状态、元数据、token hash） |
| `tokens` | 已签发的 JWT Token（JTI、类型、节点、时间戳、吊销状态） |
| `join_tokens` | Join Token（hash、使用次数、过期时间） |
| `audit_log` | 操作审计（独立 SQLite 文件） |

### 节点身份

节点首次注册获得稳定的 `node_id`（UUID），保存在 Agent 本地配置。重连时 Server 通过 `node_id` 识别返回的节点。心跳批量持久化（30s 刷新间隔）。

## TLS

TLS **默认关闭** — 明文 gRPC 适用于内网/私有网络。

### TLS 模式

| 模式 | Server 配置 | 客户端/Agent |
|------|-----------|------------|
| 禁用 TLS（默认） | `tls.auto: false`（默认） | `--tls-insecure` |
| Auto-TLS | `tls.auto: true` 或 `init --tls` | `--ca-cert ca.crt` |
| 自带证书 | `tls.cert` + `tls.key` | 系统 CA 或 `--ca-cert` |

### Auto-TLS

启用 `tls.auto: true` 后，Server 自动生成：
- **CA**：ECDSA P-256，10 年有效期
- **服务端证书**：ECDSA P-256，1 年有效期，30 天内过期自动续签
- SAN：始终包含 `localhost` + `127.0.0.1` + 配置的主机名

`axon-agent join` 时 CA 证书自动下发给 Agent。

## 节点状态

| 状态 | 含义 |
|------|------|
| **online** | Agent 已连接，心跳正常 |
| **offline** | 心跳超时或连接断开 |

## 审计日志

Server 记录每一个操作：时间戳、调用者、节点、操作、结果、耗时。SQLite 存储，异步写入，不阻塞业务。

## 技术栈

| 项目 | 选择 |
|------|------|
| 语言 | Go |
| 通信 | gRPC（HTTP/2），全链路 |
| 认证 | JWT（HMAC-SHA256）+ JTI + 吊销 |
| 序列化 | Protocol Buffers |
| 持久化 | SQLite（WAL 模式，单一共享 DB） |
| TLS | Auto-TLS（ECDSA P-256）或自带证书 |
| 构建 | 单一二进制 × 3，跨平台 |
| 配置 | YAML |
