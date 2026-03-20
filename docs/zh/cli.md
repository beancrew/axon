# Axon CLI 设计

## 概述

`axon` 是一个无状态的单二进制文件。从本地配置中读取 Server 地址和 token，通过 gRPC 与 axon-server 通信。

## 配置

存储在 `~/.axon/config.yaml`：

```yaml
server: "axon.example.com:443"
token: "eyJhbGciOiJIUzI1NiIs..."
```

## 命令

### `axon node list`

列出所有已注册节点。

```
$ axon node list
NAME      STATUS   OS                    ARCH    IP             VERSION   LAST SEEN
web-1     online   Ubuntu 24.04 LTS      amd64   10.0.1.10      0.1.0     2s ago
db-1      online   CentOS 9              amd64   10.0.1.20      0.1.0     5s ago
edge-1    offline  Debian 12             arm64   192.168.1.50   0.1.0     2m ago
```

- **Server**：✅ 需要
- **gRPC**：`ManagementService.ListNodes`（unary）
- **认证**：gRPC metadata 中的 JWT token
- **参数**：
  - `--json` — JSON 输出
  - `--status <online|offline>` — 按状态过滤

### `axon node info <node>`

查看节点详细信息。

```
$ axon node info web-1
Name:           web-1
Status:         online
OS:             Ubuntu 24.04 LTS (linux 6.8.0-45-generic)
Arch:           amd64
IP:             10.0.1.10
Uptime:         3d 12h 5m
Agent Version:  0.1.0
Connected:      2026-03-17 23:15:00 UTC
Last Heartbeat: 2026-03-20 11:24:58 UTC
Labels:
  env: production
  role: web
```

- **Server**：✅ 需要
- **gRPC**：`ManagementService.GetNode`（unary）
- **参数**：
  - `--json` — JSON 输出

### `axon node remove <node>`

从注册中心移除节点。如果在线会断开连接。

```
$ axon node remove edge-1
Node "edge-1" removed.
```

- **Server**：✅ 需要
- **gRPC**：`ManagementService.RemoveNode`（unary）

### `axon exec <node> <command>`

在远程节点上执行命令。stdout/stderr 实时流式返回。

```
$ axon exec web-1 "docker ps"
CONTAINER ID   IMAGE     STATUS
abc123         nginx     Up 2 hours

$ axon exec web-1 "tail -f /var/log/app.log"
[2026-03-20 11:25:00] request received...
[2026-03-20 11:25:01] processing...
^C
```

- **Server**：✅ 需要
- **gRPC**：`OperationsService.Exec`（server stream）
- **行为**：
  - stdout → 本地 stdout，stderr → 本地 stderr（保持流分离）
  - Exit code 原样转发：`axon exec` 的退出码 = 远程命令的退出码
  - Ctrl+C 向 Server 发送取消（gRPC context cancel）
  - 长运行命令持续流式输出直到完成或取消
- **参数**：
  - `--timeout <seconds>` — 超时后终止命令（0 = 不超时）
  - `--env KEY=VALUE` — 设置环境变量（可重复）
  - `--workdir <path>` — 设置工作目录

### `axon read <node> <path>`

从远程节点读取文件。内容输出到 stdout。

```
$ axon read web-1 /etc/nginx/nginx.conf
worker_processes auto;
events { ... }
...

$ axon read web-1 /etc/nginx/nginx.conf > local-copy.conf
```

- **Server**：✅ 需要
- **gRPC**：`OperationsService.Read`（server stream）
- **行为**：
  - 首条消息：文件元信息（大小、权限、修改时间）
  - 后续消息：文件内容分块传输
  - 支持二进制文件（原始字节到 stdout）
- **参数**：
  - `--meta` — 仅打印文件元信息（大小、权限、修改时间）
  - `--json` — 元信息以 JSON 格式输出

### `axon write <node> <path>`

写文件到远程节点。从 stdin 读取内容。

```
$ axon write web-1 /etc/nginx/nginx.conf < nginx.conf
Written 2048 bytes to /etc/nginx/nginx.conf

$ echo "hello" | axon write web-1 /tmp/hello.txt
Written 6 bytes to /tmp/hello.txt

$ cat backup.sql | axon write db-1 /tmp/restore.sql
Written 15728640 bytes to /tmp/restore.sql
```

- **Server**：✅ 需要
- **gRPC**：`OperationsService.Write`（client stream）
- **行为**：
  - 首条消息：header（路径、文件权限）
  - 后续消息：文件内容分块传输
  - 大文件流式传输，不需要全部加载到内存
- **参数**：
  - `--mode <perm>` — 文件权限（默认：0644）

### `axon forward <node> <local-port>:<remote-port>`

将远程端口转发到本地。

```
$ axon forward db-1 5432:5432
Forwarding localhost:5432 → db-1:5432
Ready. Press Ctrl+C to stop.

# 在另一个终端：
$ psql -h localhost -p 5432 -U postgres
```

- **Server**：✅ 需要
- **gRPC**：`OperationsService.Forward`（BiDi stream，每个 TCP 连接一个）
- **行为**：
  - CLI 在 `localhost:<local-port>` 上监听
  - 每个 TCP 连接进来 → 开一个新的 gRPC bidi stream
  - 原始 TCP 字节封装在 `TunnelData` protobuf 消息中
  - 双向传输：数据双向流动直到任一端关闭
  - Ctrl+C 停止监听并关闭所有隧道
- **参数**：
  - `--bind <address>` — 绑定地址（默认：`127.0.0.1`）

### `axon auth login`

向 Server 认证并获取 JWT token。

```
$ axon auth login --server axon.example.com:443
Username: gary
Password: ****
Login successful. Token saved to ~/.axon/config.yaml
```

- **Server**：✅ 需要
- **gRPC**：`ManagementService.Login`（unary）
- **行为**：
  - 提示输入用户名/密码（Phase 1）
  - 保存 token + Server 地址到 `~/.axon/config.yaml`
- **参数**：
  - `--server <address>` — Server 地址（保存到配置）

### `axon auth token`

显示当前 token。

```
$ axon auth token
eyJhbGciOiJIUzI1NiIs...
```

- **Server**：❌ 仅本地
- **行为**：读取并打印 `~/.axon/config.yaml` 中的 token

### `axon config set <key> <value>`

设置配置项。

```
$ axon config set server axon.example.com:443
```

- **Server**：❌ 仅本地
- **支持的 key**：`server`、`token`

### `axon config get <key>`

获取配置项。

```
$ axon config get server
axon.example.com:443
```

- **Server**：❌ 仅本地

### `axon version`

打印版本信息。

```
$ axon version
axon 0.1.0 (go1.22, linux/amd64)
```

- **Server**：❌ 仅本地
- 编译时通过 `-ldflags` 注入

## 命令总表

| 命令 | 需要 Server | gRPC 模式 | 需要认证 |
|------|:----------:|-----------|:-------:|
| `node list` | ✅ | unary | ✅ |
| `node info` | ✅ | unary | ✅ |
| `node remove` | ✅ | unary | ✅ |
| `exec` | ✅ | server stream | ✅ |
| `read` | ✅ | server stream | ✅ |
| `write` | ✅ | client stream | ✅ |
| `forward` | ✅ | bidi stream | ✅ |
| `auth login` | ✅ | unary | ❌（正在获取 token） |
| `auth token` | ❌ | — | — |
| `config set/get` | ❌ | — | — |
| `version` | ❌ | — | — |

## 退出码

| 退出码 | 含义 |
|-------|------|
| 0 | 成功 |
| 1 | 一般错误（连接失败、Server 错误） |
| 2 | 认证错误（无效/过期 token） |
| 3 | 节点错误（不存在、离线） |
| N | `exec` 命令：转发远程命令的退出码 |
