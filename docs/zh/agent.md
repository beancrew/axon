# Axon Agent 设计

> [English Version](../agent.md)

## 概述

`axon-agent` 是安装在目标机器上的轻量守护进程。反向连接 axon-server，注册自身，维持心跳，响应 Server 分发的任务。

**Agent 不主动发起操作，只响应 Server 分发的任务。**

## 生命周期

```
安装二进制
    │
    ▼
axon-agent join <server-addr> <join-token> [--name <name>]
    │
    ├── 注册（首次）：
    │   1. 用 join token 向 Server 验证（JoinAgent RPC）
    │   2. 收到 Agent JWT + 分配的 node_id + CA 证书（如启用 TLS）
    │   3. 保存配置到 ~/.axon-agent/config.yaml
    │   4. 连接 Server（gRPC）
    │   5. 发送 RegisterRequest（token + 节点名 + 系统信息）
    │   6. 收到 RegisterResponse（心跳间隔）
    │   7. 开始心跳循环
    │
    ▼
axon-agent start（后续运行）
    │
    ├── 1. 读取本地配置
    │   2. 连接 Server
    │   3. 用已有 node_id 重新注册
    │   4. 开始心跳循环
    │
    ▼
运行中（等待任务）
    ├── 定时心跳
    ├── 系统信息上报
    ├── 任务执行（按需）
    ├── 断线重连（指数退避：1s → 2s → 4s → ... → 60s）
```

## 配置

`~/.axon-agent/config.yaml`：

```yaml
server: "axon.example.com:9090"
token: "agent-token-xxx"
node_id: "a1b2c3d4"           # Server 分配的稳定 ID
node_name: "web-1"
labels:
  env: production
  role: web
ca_cert: "/path/to/ca.crt"    # TLS 验证用的 CA 证书
tls_insecure: false
```

详见 [配置参考](configuration.md)。

## TLS

Agent 的 TLS 三路选择：

| 优先级 | 条件 | 行为 |
|--------|------|------|
| 1 | `tls_insecure: true` | 不验证 TLS |
| 2 | `ca_cert` 已设置 | 用指定 CA 验证服务端证书 |
| 3 | 都未设置 | 用系统 CA 池验证 |

**Auto-TLS 场景下**：`axon-agent join` 时 Server 的 CA 证书自动下发并保存到 `~/.axon-agent/ca.crt`，无需手动复制。

## 任务执行

### exec

收到 TaskSignal → 开 HandleTask stream → 收到命令 → 创建子进程 → 流式输出 stdout/stderr → 发送退出码

### read

收到 TaskSignal → 开 HandleTask stream → stat 文件 → 发送元数据 → 分块读取（32KB）→ 流式发送

### write

收到 TaskSignal → 开 HandleTask stream → 收到文件头 → 创建文件 → 接收数据块 → 原子写入（临时文件 + rename）

### forward

收到 TaskSignal → 开 HandleTask stream → 连接本地端口 → 双向中继 TunnelData ↔ TCP

## 系统信息采集

Agent 上报 `NodeInfo`（主机名、架构、IP、运行时间、Agent 版本、OS 详情）：

- **Linux**：`/etc/os-release` + `uname`
- **macOS**：`sw_vers` + `uname`
- **Windows**：`RtlGetVersion`

## systemd 示例

```ini
[Unit]
Description=Axon Agent
After=network.target

[Service]
ExecStart=/usr/local/bin/axon-agent start --foreground
Restart=always
RestartSec=5
User=axon
Environment=AXON_CA_CERT=/etc/axon-agent/ca.crt

[Install]
WantedBy=multi-user.target
```
