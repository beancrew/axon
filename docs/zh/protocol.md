# Axon 协议设计

## 概述

全链路通信使用 gRPC over HTTP/2，Protocol Buffers 序列化。

三个 proto 文件：
- `control.proto` — Agent ↔ Server 控制面
- `operations.proto` — CLI ↔ Server ↔ Agent 操作面（exec/read/write/forward）
- `management.proto` — CLI ↔ Server 管理面（节点管理、认证）

## 1. 控制面 — `control.proto`

Agent 与 Server 之间的长连接双向 stream。

```protobuf
syntax = "proto3";
package axon.control;

service ControlService {
  // Agent 启动时建立持久控制通道。
  // Server 通过此通道追踪存活状态并派发任务信号。
  rpc Connect(stream AgentMessage) returns (stream ServerMessage);
}

// ─── Agent → Server ───

message AgentMessage {
  oneof payload {
    RegisterRequest register = 1;
    Heartbeat heartbeat = 2;
    NodeInfo node_info = 3;
  }
}

message RegisterRequest {
  string token = 1;           // Agent token（一次性注册用）
  string node_name = 2;       // 期望的节点名（不填则用 hostname）
  NodeInfo info = 3;          // 初始节点信息
}

message Heartbeat {
  int64 timestamp = 1;        // Unix 时间戳（毫秒）
}

message NodeInfo {
  string hostname = 1;
  string arch = 2;            // 例如 "amd64", "arm64"
  string ip = 3;              // 主 IP
  int64 uptime_seconds = 4;
  string agent_version = 5;
  OSInfo os_info = 6;         // 详细 OS 信息
}

message OSInfo {
  string os = 1;              // 内核名称："linux", "darwin", "windows"
  string os_version = 2;      // 内核版本："6.8.0-45-generic", "24.3.0"
  string platform = 3;        // 发行版/平台："ubuntu", "centos", "debian", "macOS"
  string platform_version = 4;// 发行版版本："24.04", "9", "14.4"
  string pretty_name = 5;     // 可读名称："Ubuntu 24.04 LTS", "macOS 14.4 Sonoma"
}

// ─── Server → Agent ───

message ServerMessage {
  oneof payload {
    RegisterResponse register_response = 1;
    HeartbeatAck heartbeat_ack = 2;
    TaskSignal task_signal = 3;    // 通知 Agent 开新的操作面 stream
  }
}

message RegisterResponse {
  bool success = 1;
  string node_id = 2;         // Server 分配的节点 ID
  string error = 3;
  int32 heartbeat_interval_seconds = 4;  // Server 告诉 Agent 心跳间隔
}

message HeartbeatAck {
  int64 server_timestamp = 1;
}

message TaskSignal {
  string task_id = 1;         // Agent 用此 ID 开操作面 stream
  TaskType type = 2;
}

enum TaskType {
  TASK_EXEC = 0;
  TASK_READ = 1;
  TASK_WRITE = 2;
  TASK_FORWARD = 3;
}
```

### 流程

1. Agent 启动 → 调用 `Connect()` → 发送 `RegisterRequest`
2. Server 验证 token → 返回 `RegisterResponse`（含 node_id 和心跳间隔）
3. Agent 每 N 秒发送 `Heartbeat`
4. Server 回复 `HeartbeatAck`
5. CLI 发起请求时，Server 通过控制 stream 发送 `TaskSignal`
6. Agent 收到 `TaskSignal` → 开新的操作面 stream（带 task_id）

## 2. 操作面 — `operations.proto`

每个任务独立 stream。

```protobuf
syntax = "proto3";
package axon.operations;

service OperationsService {
  // CLI 调用这些 RPC。Server 路由到目标 Agent。

  // 执行命令 — 流式返回 stdout/stderr
  rpc Exec(ExecRequest) returns (stream ExecOutput);

  // 读文件 — 流式返回文件内容
  rpc Read(ReadRequest) returns (stream ReadOutput);

  // 写文件 — 客户端流式上传内容
  rpc Write(stream WriteInput) returns (WriteResponse);

  // 端口转发 — 双向字节流
  rpc Forward(stream TunnelData) returns (stream TunnelData);
}

// ─── Exec ───

message ExecRequest {
  string node_id = 1;
  string command = 2;
  map<string, string> env = 3;      // 可选环境变量
  string working_dir = 4;           // 可选工作目录
  int32 timeout_seconds = 5;        // 0 = 不超时
}

message ExecOutput {
  oneof payload {
    bytes stdout = 1;
    bytes stderr = 2;
    ExecExit exit = 3;
  }
}

message ExecExit {
  int32 exit_code = 1;
  string error = 2;           // Agent 层面错误时非空（非命令本身错误）
}

// ─── Read ───

message ReadRequest {
  string node_id = 1;
  string path = 2;
}

message ReadOutput {
  oneof payload {
    bytes data = 1;           // 文件内容块
    ReadMeta meta = 2;        // 首条消息：文件大小、权限等
    string error = 3;
  }
}

message ReadMeta {
  int64 size = 1;
  int32 mode = 2;             // Unix 文件权限
  int64 modified_at = 3;      // Unix 时间戳
}

// ─── Write ───

message WriteInput {
  oneof payload {
    WriteHeader header = 1;   // 首条消息
    bytes data = 2;           // 文件内容块
  }
}

message WriteHeader {
  string node_id = 1;
  string path = 2;
  int32 mode = 3;             // Unix 文件权限（默认 0644）
}

message WriteResponse {
  bool success = 1;
  int64 bytes_written = 2;
  string error = 3;
}

// ─── Forward（端口隧道） ───

message TunnelData {
  string connection_id = 1;   // 标识单个 TCP 连接
  bytes payload = 2;          // 原始 TCP 字节
  bool close = 3;             // 关闭此连接的信号

  // 仅在新连接的首条消息中：
  TunnelOpen open = 4;
}

message TunnelOpen {
  string node_id = 1;
  int32 remote_port = 2;
}
```

### Exec 流程

```
CLI                         Server                      Agent
 │── ExecRequest ──────────→│── TaskSignal ────────────→│
 │  {node:web-1, cmd:...}   │                           │── 开操作面 stream
 │                           │                           │── 执行命令
 │←─ ExecOutput(stdout) ────│←─ stdout 字节 ────────────│
 │←─ ExecOutput(stderr) ────│←─ stderr 字节 ────────────│
 │←─ ExecOutput(exit) ──────│←─ exit code ──────────────│
 │  stream 关闭              │  stream 关闭              │
```

### Forward 流程

```
TCP 客户端    CLI                    Server                  Agent               TCP 目标
  │──连接───→│                        │                       │                      │
  │           │──TunnelData{open}───→│──TaskSignal──────────→│                      │
  │           │  {node:db-1,port:5432}│                       │──TCP 连接───────────→│
  │           │                       │                       │                      │
  │──数据───→│──TunnelData{payload}─→│──TunnelData{payload}→│──数据────────────────→│
  │←─数据────│←─TunnelData{payload}──│←─TunnelData{payload}─│←─数据─────────────────│
  │   ...     │   ...                 │   ...                 │   ...                 │
  │──关闭───→│──TunnelData{close}───→│──TunnelData{close}──→│──TCP 关闭────────────→│
```

每个 TCP 连接有自己的 `connection_id`。Phase 1 采用 **每个 TCP 连接一个独立 gRPC stream** 的方案，简单清晰。gRPC/HTTP2 多路复用保证不会连接爆炸。

## 3. 管理面 — `management.proto`

简单的 unary RPC，用于节点管理和认证。

```protobuf
syntax = "proto3";
package axon.management;

service ManagementService {
  rpc ListNodes(ListNodesRequest) returns (ListNodesResponse);
  rpc GetNode(GetNodeRequest) returns (GetNodeResponse);
  rpc RemoveNode(RemoveNodeRequest) returns (RemoveNodeResponse);
  rpc Login(LoginRequest) returns (LoginResponse);
}

// ─── 节点管理 ───

message ListNodesRequest {}

message ListNodesResponse {
  repeated NodeSummary nodes = 1;
}

message NodeSummary {
  string node_id = 1;
  string node_name = 2;
  string status = 3;          // "online" | "offline"
  string arch = 4;
  string ip = 5;
  string agent_version = 6;
  int64 connected_at = 7;     // Unix 时间戳
  int64 last_heartbeat = 8;   // Unix 时间戳
  OSInfo os_info = 9;         // 详细 OS 信息
}

message GetNodeRequest {
  string node_id = 1;
}

message GetNodeResponse {
  NodeSummary summary = 1;
  int64 uptime_seconds = 2;
  map<string, string> labels = 3;
}

message RemoveNodeRequest {
  string node_id = 1;
}

message RemoveNodeResponse {
  bool success = 1;
  string error = 2;
}

// ─── 认证 ───

message LoginRequest {
  string username = 1;
  string password = 2;        // Phase 1：用户名/密码
}

message LoginResponse {
  string token = 1;           // JWT token
  int64 expires_at = 2;       // Unix 时间戳
  string error = 3;
}
```

## 错误处理

所有 RPC 使用标准 gRPC 状态码：

| 场景 | gRPC 状态码 |
|------|------------|
| 节点不存在 | `NOT_FOUND` |
| 节点离线 | `UNAVAILABLE` |
| 认证失败 / token 过期 | `UNAUTHENTICATED` |
| Token 无权访问目标节点 | `PERMISSION_DENIED` |
| 文件不存在（read） | `NOT_FOUND` |
| 命令超时（exec） | `DEADLINE_EXCEEDED` |
| Agent 内部错误 | `INTERNAL` |
| 无效请求 | `INVALID_ARGUMENT` |

## TLS

全链路 gRPC 连接使用 TLS：
- CLI → Server：标准 TLS（Server 证书）
- Agent → Server：标准 TLS（Server 证书）+ Agent token 身份验证

Phase 1：Server 使用单一 TLS 证书。mTLS 是 Phase 2 的考虑事项。
