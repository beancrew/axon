# Axon 架构总览

## 组件

Axon 由三个组件组成，每个构建为单一静态二进制文件。

```
axon-cli ── gRPC ──→ axon-server ←── gRPC ── axon-agent
                     (控制面)              (反向连接)
```

| 组件 | 二进制文件 | 职责 |
|------|-----------|------|
| **axon-cli** | `axon` | 用户/Agent 操作接口。无状态。通过 gRPC 与 Server 通信。 |
| **axon-server** | `axon-server` | 中央控制面。节点注册、认证、路由、审计。 |
| **axon-agent** | `axon-agent` | 目标机器上的轻量 daemon。反向连接 Server。 |

## 通信

**全链路 gRPC over HTTP/2。** 不用 WebSocket、REST、自定义 TCP。

| 链路 | 协议 | 模式 | 原因 |
|------|------|------|------|
| CLI → Server | gRPC | unary + server/client/bidi stream | exec 需要流式输出；forward 需要双向流 |
| Agent → Server（控制面） | gRPC | BiDi stream（长连接） | 心跳、注册、节点信息上报 |
| Agent → Server（操作面） | gRPC | 按需 stream | exec/read/write/forward 每个任务独立 stream |

多个 gRPC stream 共享一条 HTTP/2 TCP 连接 —— 不会连接爆炸。

### 为什么全部用 gRPC？

1. `exec` 需要流式输出 stdout/stderr —— gRPC server stream 原生支持
2. `forward` 需要双向数据流 —— gRPC bidi stream 原生支持
3. Agent 反向连接需要长连接 —— gRPC bidi stream 原生支持
4. 用 HTTP 需要 SSE + WebSocket + chunked transfer 三种补丁 —— 不如统一一套协议
5. 统一技术栈：一套 proto 定义、一套 TLS 配置、一套代码生成

未来的 HTTP/REST 接入（Web 控制台、第三方集成）通过 grpc-gateway 层实现，Phase 3 再做。

## 连接模型

Agent **主动外连** Server。节点上不需要开入站端口。

```
axon-agent ──── 主动 gRPC 外连 ────→ axon-server ←──── gRPC ──── axon-cli
   (NAT/防火墙后面)                    (公网)              (任何位置)
```

这意味着：
- 不需要暴露 SSH 端口
- NAT 后面、企业防火墙后面、边缘网络都能用
- 云主机、本地服务器、边缘设备 —— 全部一样

## 控制面 vs 操作面

Agent 在同一条 HTTP/2 连接上维护两个逻辑通道：

```
┌─────────── HTTP/2 连接 ─────────────┐
│                                      │
│  控制面（1 条长连接 stream）           │
│  ├── 注册（连接时）                   │
│  ├── 心跳（定时）                     │
│  └── 节点信息上报（定时）              │
│                                      │
│  操作面（按需 stream）                │
│  ├── exec stream（每个命令）          │
│  ├── read stream（每个文件）          │
│  ├── write stream（每个文件）         │
│  └── forward stream（每个 TCP 连接）  │
│                                      │
└──────────────────────────────────────┘
```

控制面 stream 生命周期 = Agent 进程生命周期。
操作面 stream 生命周期 = 单个任务生命周期。

## 认证

基于 JWT。Server 持有签名密钥。

```
┌─── CLI Token (JWT) ──┐     ┌─── Agent Token ───────────┐
│ user_id              │     │ node_id                    │
│ node_ids: [...]      │     │ server_url                 │
│ exp                  │     │ (首次启动时使用)             │
│ iat                  │     └────────────────────────────┘
└──────────────────────┘
```

| Token 类型 | 作用域 | 生命周期 |
|-----------|--------|---------|
| CLI Token | 绑定用户 + 允许的节点列表 | 可配置过期时间 |
| Agent Token | 绑定节点身份 | 首次注册时使用，之后基于会话 |

CLI Token 绑定特定节点 —— 一个 token 只能操作其允许的节点列表。

## 节点状态

| 状态 | 含义 |
|------|------|
| **online** | Agent 连接正常，心跳健康 |
| **offline** | 心跳超时或连接断开 |

CLI 请求 offline 节点直接返回错误。不排队，不等待。

## 审计日志

所有经过 Server 的操作都会记录：

```json
{
  "timestamp": "2026-03-20T11:22:00Z",
  "user_id": "gary",
  "node_id": "web-1",
  "action": "exec",
  "command": "docker ps",
  "result": "success",
  "duration_ms": 120
}
```

存储：SQLite（Phase 1）。后续可扩展到外部存储。

## 技术栈

| 项目 | 选型 |
|------|------|
| 语言 | Go |
| 通信 | gRPC (HTTP/2)，全链路 |
| 认证 | JWT |
| 序列化 | Protocol Buffers |
| 构建 | 单二进制 × 3，跨平台 |
| 审计存储 | SQLite (Phase 1) |
| 配置格式 | YAML |
