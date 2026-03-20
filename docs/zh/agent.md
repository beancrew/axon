# Axon Agent 设计

## 概述

`axon-agent` 是安装在每台目标机器上的轻量 daemon。它反向连接 axon-server，注册自身，维持心跳，响应 Server 派发的任务。

**Agent 不主动发起操作。它只响应 Server 下发的任务。**

## Agent 生命周期

```
安装二进制
    │
    ▼
axon-agent start --server <addr> --token <token> [--name <name>]
    │
    ├── 首次运行：
    │   1. 保存配置到 ~/.axon-agent/config.yaml
    │   2. 连接 Server（gRPC TLS）
    │   3. 发送 RegisterRequest（token + 节点信息）
    │   4. 收到 RegisterResponse（node_id + 心跳间隔）
    │   5. 开始心跳循环
    │
    ├── 后续运行：
    │   1. 从 ~/.axon-agent/config.yaml 读取配置
    │   2. 连接 Server
    │   3. 重新注册（Server 通过 node_id 识别回归节点）
    │   4. 开始心跳循环
    │
    ▼
运行中（等待任务）
    ├── 每 N 秒发送心跳（N 由 Server 配置）
    ├── 定期上报节点信息（或变更时上报）
    ├── 执行任务（按需）
    │
    ├── 连接断开 → 指数退避重连
    │   初始：1s，最大：60s，抖动：±20%
    │
    ▼
停止
    ├── axon-agent stop → 优雅关闭
    │   - 完成进行中的任务（有超时）
    │   - 关闭 gRPC stream
    │   - 退出
    └── kill -9 → Server 通过心跳超时检测 → 标记 offline
```

## 配置

存储在 `~/.axon-agent/config.yaml`：

```yaml
server: "axon.example.com:443"
token: "agent-token-xxx"
node_id: "a1b2c3d4"           # 首次注册后由 Server 分配
node_name: "web-1"            # 用户指定或使用 hostname
labels:
  env: production
  role: web
```

- `token`：用于初始注册，由 Server 验证
- `node_id`：首次注册后持久化，用于重连时的身份识别
- `node_name`：未指定时默认使用 hostname
- `labels`：可选的键值对，便于节点分组

## 命令

### `axon-agent start`

启动 Agent daemon。

```
$ axon-agent start --server axon.example.com:443 --token <token>
[INFO] connecting to axon.example.com:443...
[INFO] registered as node "web-1" (id: a1b2c3d4)
[INFO] heartbeat interval: 10s
[INFO] ready, waiting for tasks

# 后续启动（配置已保存）：
$ axon-agent start
[INFO] connecting to axon.example.com:443...
[INFO] reconnected as node "web-1" (id: a1b2c3d4)
[INFO] ready, waiting for tasks
```

- **Server**：✅ 需要
- **参数**：
  - `--server <address>` — Server 地址（首次运行时，保存到配置）
  - `--token <token>` — Agent token（首次运行时，保存到配置）
  - `--name <name>` — 节点名（可选，默认用 hostname）
  - `--labels key=value` — 标签（可重复）
  - `--foreground` — 前台运行（默认：守护进程）

### `axon-agent stop`

停止 Agent daemon。

```
$ axon-agent stop
[INFO] shutting down...
[INFO] completing 2 in-flight tasks...
[INFO] stopped
```

- **Server**：❌ 仅本地
- **行为**：向 daemon 进程发送 SIGTERM，等待优雅关闭

### `axon-agent status`

显示 Agent 状态。

```
$ axon-agent status
Status:      running
Server:      axon.example.com:443
Connection:  connected
Node ID:     a1b2c3d4
Node Name:   web-1
Uptime:      3d 12h 5m
Last Heartbeat: 2s ago
Active Tasks: 0
```

- **Server**：本地进程检查 + 连接健康 ping
- **参数**：
  - `--json` — JSON 输出

### `axon-agent config set/get`

管理本地配置。

```
$ axon-agent config set labels.env staging
$ axon-agent config get server
axon.example.com:443
```

- **Server**：❌ 仅本地

### `axon-agent version`

```
$ axon-agent version
axon-agent 0.1.0 (go1.22, linux/amd64)
```

- **Server**：❌ 仅本地

## 任务执行

Agent 通过控制面 stream 接收任务信号，然后开操作面 stream 执行任务。

### Exec

```
1. Server 通过控制 stream 发送 TaskSignal{task_id, TASK_EXEC}
2. Agent 开操作面 stream，接收 ExecRequest{command, env, workdir, timeout}
3. Agent 启动本地进程：
   - os/exec.Command(shell, "-c", command)
   - 管道 stdout/stderr
   - 设置环境变量
   - 设置工作目录
4. 流式回传 stdout/stderr 块作为 ExecOutput 消息
5. 进程退出 → 发送 ExecExit{exit_code} → 关闭 stream
```

**进程管理：**
- 每个 exec 作为 Agent 的子进程运行
- Agent 继承启动用户的权限（Phase 1 不做沙箱）
- 超时：Agent 先发 SIGTERM，5 秒后 SIGKILL
- 取消：gRPC context cancel → SIGTERM → SIGKILL

### Read

```
1. Server 发送 TaskSignal{task_id, TASK_READ}
2. Agent 开操作面 stream，接收 ReadRequest{path}
3. Agent stat() 文件 → 发送 ReadMeta{size, mode, mtime}
4. 打开文件，分块读取（默认 32KB）→ 每块发送 ReadOutput{data}
5. EOF → 关闭 stream
```

**错误情况：**
- 文件不存在 → gRPC NOT_FOUND
- 权限不足 → gRPC PERMISSION_DENIED
- 是目录 → gRPC INVALID_ARGUMENT

### Write

```
1. Server 发送 TaskSignal{task_id, TASK_WRITE}
2. Agent 开操作面 stream，接收 WriteHeader{path, mode}
3. 创建/截断文件，设置指定权限
4. 接收 WriteInput{data} 块 → 写入文件
5. 客户端关闭 stream → Agent 返回 WriteResponse{success, bytes_written}
```

**行为：**
- 父目录不存在时自动创建
- 原子写入：先写临时文件，再 rename（防止写入一半失败）
- 默认权限：0644

### Forward

```
1. Server 发送 TaskSignal{task_id, TASK_FORWARD}
2. Agent 开操作面 stream，接收 TunnelOpen{remote_port}
3. Agent 连接 localhost:<remote_port>（TCP）
4. 双向中继：
   - Server 来的 TunnelData{payload} → 写入 TCP 连接
   - TCP 连接读取的数据 → 发送 TunnelData{payload} 给 Server
5. 任一端关闭 → 发送 TunnelData{close} → 清理
```

**错误情况：**
- 无法连接目标端口 → gRPC UNAVAILABLE 并附错误详情
- 连接重置 → TunnelData{close}

## 心跳与节点信息

### 心跳

- Agent 每 N 秒发送 `Heartbeat{timestamp}`
- N 由 Server 在 `RegisterResponse.heartbeat_interval_seconds` 中配置
- 默认：10 秒
- Server 在 3 倍间隔（默认 30 秒）内未收到心跳 → 标记节点 offline

### 节点信息上报

Agent 在以下时机上报 `NodeInfo`：
1. 注册时（初始上报）
2. 定期更新（每 5 分钟，或可配置）
3. 发生重要变更时（如 IP 变化）

## 重连

连接断开时：

```
第 1 次：等 1s    → 重连
第 2 次：等 2s    → 重连
第 3 次：等 4s    → 重连
...
第 N 次：等 min(2^N, 60)s ± 20% 抖动 → 重连
```

重连成功后：
- 用已有的 `node_id` 重新注册（Server 识别回归节点）
- 恢复心跳
- 断连时进行中的任务视为失败

## 安全

### Phase 1（当前）

- Agent 以启动它的用户身份运行
- 不过滤命令，不限制路径
- 所有 exec/read/write 权限 = Agent 进程用户权限
- 信任边界：如果你有绑定到某节点的有效 CLI token，你可以做该 Agent 用户能做的任何事

### Phase 2（未来）

- 命令白名单/黑名单
- 路径限制（允许的目录）
- 资源限制（CPU、内存、每个 exec 的时间限制）

## 系统服务

生产环境中 Agent 应作为系统服务运行：

```bash
# systemd 示例
[Unit]
Description=Axon Agent
After=network.target

[Service]
ExecStart=/usr/local/bin/axon-agent start --foreground
Restart=always
RestartSec=5
User=axon

[Install]
WantedBy=multi-user.target
```

## 命令总表

| 命令 | 需要 Server | 说明 |
|------|:----------:|------|
| `start` | ✅ | 启动 daemon，连接并注册 |
| `stop` | ❌ | 停止 daemon（优雅关闭） |
| `status` | ⚠️ | 本地 + 连接健康检查 |
| `config set/get` | ❌ | 本地配置 |
| `version` | ❌ | 本地版本 |
