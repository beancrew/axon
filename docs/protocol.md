# Axon Protocol Design

## Overview

All communication uses gRPC over HTTP/2 with Protocol Buffers serialization.

Three proto files:
- `control.proto` — Agent ↔ Server control plane
- `operations.proto` — CLI ↔ Server ↔ Agent data plane (exec/read/write/forward)
- `management.proto` — CLI ↔ Server management (node list/info/remove, auth)

## 1. Control Plane — `control.proto`

Long-lived bidirectional stream between Agent and Server.

```protobuf
syntax = "proto3";
package axon.control;

service ControlService {
  // Agent establishes a persistent control channel on startup.
  // Server uses this to track liveness and dispatch task signals.
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
  string token = 1;           // Agent token (one-time registration)
  string node_name = 2;       // Desired node name (falls back to hostname)
  NodeInfo info = 3;          // Initial node info
}

message Heartbeat {
  int64 timestamp = 1;        // Unix timestamp (ms)
}

message NodeInfo {
  string hostname = 1;
  string os = 2;              // e.g. "linux", "darwin"
  string arch = 3;            // e.g. "amd64", "arm64"
  string ip = 4;              // Primary IP
  int64 uptime_seconds = 5;
  string agent_version = 6;
}

// ─── Server → Agent ───

message ServerMessage {
  oneof payload {
    RegisterResponse register_response = 1;
    HeartbeatAck heartbeat_ack = 2;
    TaskSignal task_signal = 3;    // Notify agent to open a data plane stream
  }
}

message RegisterResponse {
  bool success = 1;
  string node_id = 2;         // Server-assigned node ID
  string error = 3;
  int32 heartbeat_interval_seconds = 4;  // Server tells agent how often to heartbeat
}

message HeartbeatAck {
  int64 server_timestamp = 1;
}

message TaskSignal {
  string task_id = 1;         // Agent uses this to open a data plane stream
  TaskType type = 2;
}

enum TaskType {
  TASK_EXEC = 0;
  TASK_READ = 1;
  TASK_WRITE = 2;
  TASK_FORWARD = 3;
}
```

### Flow

1. Agent starts → calls `Connect()` → sends `RegisterRequest`
2. Server validates token → responds `RegisterResponse` with node_id and heartbeat interval
3. Agent sends `Heartbeat` every N seconds
4. Server responds `HeartbeatAck`
5. When CLI sends a request for this node, Server sends `TaskSignal` through control stream
6. Agent receives `TaskSignal` → opens a new data plane stream with the task_id

## 2. Data Plane — `operations.proto`

Per-task streams for exec, read, write, forward.

```protobuf
syntax = "proto3";
package axon.operations;

service OperationsService {
  // CLI calls these RPCs. Server routes to the target agent.

  // Execute a command — streams stdout/stderr back
  rpc Exec(ExecRequest) returns (stream ExecOutput);

  // Read a file — streams file content in chunks
  rpc Read(ReadRequest) returns (stream ReadOutput);

  // Write a file — client streams file content in chunks
  rpc Write(stream WriteInput) returns (WriteResponse);

  // Port forward — bidirectional byte stream
  rpc Forward(stream TunnelData) returns (stream TunnelData);
}

// ─── Exec ───

message ExecRequest {
  string node_id = 1;
  string command = 2;
  map<string, string> env = 3;      // Optional environment variables
  string working_dir = 4;           // Optional working directory
  int32 timeout_seconds = 5;        // 0 = no timeout
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
  string error = 2;           // Non-empty if agent-level error (not command error)
}

// ─── Read ───

message ReadRequest {
  string node_id = 1;
  string path = 2;
}

message ReadOutput {
  oneof payload {
    bytes data = 1;           // File content chunk
    ReadMeta meta = 2;        // Sent first: file size, permissions, etc.
    string error = 3;
  }
}

message ReadMeta {
  int64 size = 1;
  int32 mode = 2;             // Unix file mode
  int64 modified_at = 3;      // Unix timestamp
}

// ─── Write ───

message WriteInput {
  oneof payload {
    WriteHeader header = 1;   // Sent first
    bytes data = 2;           // File content chunks
  }
}

message WriteHeader {
  string node_id = 1;
  string path = 2;
  int32 mode = 3;             // Unix file mode (0644 default)
}

message WriteResponse {
  bool success = 1;
  int64 bytes_written = 2;
  string error = 3;
}

// ─── Forward (Port Tunneling) ───

message TunnelData {
  string connection_id = 1;   // Identifies a single TCP connection
  bytes payload = 2;          // Raw TCP bytes
  bool close = 3;             // Signal to close this connection

  // Only in first message of a new connection:
  TunnelOpen open = 4;
}

message TunnelOpen {
  string node_id = 1;
  int32 remote_port = 2;
}
```

### Exec Flow

```
CLI                         Server                      Agent
 │── ExecRequest ──────────→│── TaskSignal ────────────→│
 │  {node:web-1, cmd:...}   │                           │── open data stream
 │                           │                           │── run command
 │←─ ExecOutput(stdout) ────│←─ stdout bytes ───────────│
 │←─ ExecOutput(stderr) ────│←─ stderr bytes ───────────│
 │←─ ExecOutput(exit) ──────│←─ exit code ──────────────│
 │  stream closes            │  stream closes            │
```

### Forward Flow

```
TCP client      CLI                    Server                  Agent               TCP target
  │──connect──→│                        │                       │                      │
  │             │──TunnelData{open}───→│──TaskSignal──────────→│                      │
  │             │  {node:db-1,port:5432}│                       │──TCP connect────────→│
  │             │                       │                       │                      │
  │──data─────→│──TunnelData{payload}─→│──TunnelData{payload}→│──data────────────────→│
  │←─data──────│←─TunnelData{payload}──│←─TunnelData{payload}─│←─data─────────────────│
  │   ...       │   ...                 │   ...                 │   ...                 │
  │──close────→│──TunnelData{close}───→│──TunnelData{close}──→│──TCP close────────────→│
```

Each incoming TCP connection gets its own `connection_id`. Multiple TCP connections can be multiplexed over a single gRPC bidi stream, or each can open a separate stream (implementation choice — we use **one stream per TCP connection** for simplicity in Phase 1).

## 3. Management — `management.proto`

Simple unary RPCs for node management and auth.

```protobuf
syntax = "proto3";
package axon.management;

service ManagementService {
  rpc ListNodes(ListNodesRequest) returns (ListNodesResponse);
  rpc GetNode(GetNodeRequest) returns (GetNodeResponse);
  rpc RemoveNode(RemoveNodeRequest) returns (RemoveNodeResponse);
  rpc Login(LoginRequest) returns (LoginResponse);
}

// ─── Node Management ───

message ListNodesRequest {}

message ListNodesResponse {
  repeated NodeSummary nodes = 1;
}

message NodeSummary {
  string node_id = 1;
  string node_name = 2;
  string status = 3;          // "online" | "offline"
  string os = 4;
  string arch = 5;
  string ip = 6;
  string agent_version = 7;
  int64 connected_at = 8;     // Unix timestamp
  int64 last_heartbeat = 9;   // Unix timestamp
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

// ─── Auth ───

message LoginRequest {
  string username = 1;
  string password = 2;        // Phase 1: simple username/password
}

message LoginResponse {
  string token = 1;           // JWT token
  int64 expires_at = 2;       // Unix timestamp
  string error = 3;
}
```

## Error Handling

All RPCs use standard gRPC status codes:

| Scenario | gRPC Status Code |
|----------|-----------------|
| Node not found | `NOT_FOUND` |
| Node offline | `UNAVAILABLE` |
| Auth failed / token expired | `UNAUTHENTICATED` |
| Token doesn't cover target node | `PERMISSION_DENIED` |
| File not found (read) | `NOT_FOUND` |
| Command timeout (exec) | `DEADLINE_EXCEEDED` |
| Agent internal error | `INTERNAL` |
| Invalid request | `INVALID_ARGUMENT` |

## TLS

All gRPC connections use TLS:
- CLI → Server: standard TLS (server cert)
- Agent → Server: standard TLS (server cert) + Agent token for identity

Phase 1: Server uses a single TLS cert. mTLS is a Phase 2 consideration.

---

# Axon 协议设计

## 概述

全链路 gRPC over HTTP/2，Protocol Buffers 序列化。

三个 proto 文件：
- `control.proto` — Agent ↔ Server 控制面（注册、心跳、信息上报）
- `operations.proto` — CLI ↔ Server ↔ Agent 操作面（exec/read/write/forward）
- `management.proto` — CLI ↔ Server 管理面（节点管理、认证）

## 控制面

Agent 启动后调 `Connect()` 建立长连接 BiDi stream。通过这条 stream 完成注册、心跳、节点信息上报。Server 通过这条 stream 下发 `TaskSignal` 通知 Agent 开新的操作面 stream。

## 操作面

每个任务（exec/read/write/forward）独立开一个 gRPC stream，任务结束 stream 关闭。

- exec：server stream，流式返回 stdout/stderr + exit code
- read：server stream，先发 meta（文件大小/权限），再分块发内容
- write：client stream，先发 header（路径/权限），再分块发内容
- forward：BiDi stream，TCP 字节封装在 protobuf message 里双向搬运

## 端口转发

每个 TCP 连接开一个独立 gRPC stream（Phase 1）。gRPC/HTTP2 多路复用保证不会连接爆炸。

## 错误处理

使用标准 gRPC status code。节点离线 → `UNAVAILABLE`，认证失败 → `UNAUTHENTICATED`，越权 → `PERMISSION_DENIED`。

## TLS

全链路 TLS。Phase 1 单向 TLS（Server 证书），Phase 2 考虑 mTLS。
