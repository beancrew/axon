# Axon Protocol Design

> [中文版 / Chinese](zh/protocol.md)

## Overview

All communication uses gRPC over HTTP/2 with Protocol Buffers serialization. Three proto files define the entire API surface.

## Proto Files

| File | Package | Services | Purpose |
|------|---------|----------|---------|
| `control.proto` | `axon.control` | `ControlService` | Agent ↔ Server control plane |
| `operations.proto` | `axon.operations` | `OperationsService`, `AgentOpsService` | CLI operations + agent task handling |
| `management.proto` | `axon.management` | `ManagementService` | Node/user/token management + auth |

## ControlService (Agent ↔ Server)

Long-lived bidirectional stream for agent lifecycle management.

```protobuf
service ControlService {
  rpc Connect(stream AgentMessage) returns (stream ServerMessage);
}
```

### Agent → Server Messages

| Message | Purpose |
|---------|---------|
| `RegisterRequest` | Initial registration (token, node_name, node_id, NodeInfo) |
| `Heartbeat` | Periodic liveness signal (timestamp) |
| `NodeInfo` | System info update (hostname, arch, IP, uptime, OSInfo) |

`RegisterRequest.node_id` is empty on first connect; non-empty on reconnect (stable identity).

### Server → Agent Messages

| Message | Purpose |
|---------|---------|
| `RegisterResponse` | Registration result (node_id, heartbeat_interval) |
| `HeartbeatAck` | Heartbeat acknowledgment (server timestamp) |
| `TaskSignal` | Dispatch a task — agent opens data plane stream |

### OSInfo

Detailed OS information collected per-platform:

```protobuf
message OSInfo {
  string os = 1;               // "linux", "darwin", "windows"
  string os_version = 2;       // kernel version
  string platform = 3;         // "ubuntu", "centos", "macOS"
  string platform_version = 4; // "24.04", "9", "14.4"
  string pretty_name = 5;      // "Ubuntu 24.04 LTS"
}
```

### TaskType

```protobuf
enum TaskType {
  TASK_EXEC = 0;
  TASK_READ = 1;
  TASK_WRITE = 2;
  TASK_FORWARD = 3;
}
```

## OperationsService (CLI → Server)

CLI-facing operations. Server routes to the target agent.

```protobuf
service OperationsService {
  rpc Exec(ExecRequest) returns (stream ExecOutput);
  rpc Read(ReadRequest) returns (stream ReadOutput);
  rpc Write(stream WriteInput) returns (WriteResponse);
  rpc Forward(stream TunnelData) returns (stream TunnelData);
}
```

### Exec

```
CLI sends ExecRequest{node_id, command, env, workdir, timeout}
  → Server routes to agent
  → Agent streams ExecOutput{stdout | stderr | exit}
```

### Read

```
CLI sends ReadRequest{node_id, path}
  → Server routes to agent
  → Agent streams ReadOutput{meta (first), then data chunks}
```

### Write

```
CLI streams WriteInput{header (first), then data chunks}
  → Server routes to agent
  → Agent returns WriteResponse{success, bytes_written}
```

### Forward (Port Tunneling)

```
CLI ←→ Server ←→ Agent: bidirectional TunnelData{connection_id, payload, close}
First message includes TunnelOpen{node_id, remote_port}
```

Each TCP connection gets an independent gRPC bidi stream. Raw TCP bytes are wrapped in `TunnelData` messages.

## AgentOpsService (Agent → Server)

Agent-facing data plane. After receiving a `TaskSignal` on the control stream, the agent opens a `HandleTask` stream.

```protobuf
service AgentOpsService {
  rpc HandleTask(stream TaskDataUp) returns (stream TaskDataDown);
}
```

### Task Data Flow

```
TaskDataUp (agent → server):
  task_id + {exec_output | read_output | write_response | tunnel_data}

TaskDataDown (server → agent):
  task_id + {exec_request | read_request | write_input | tunnel_data}
```

The server acts as a transparent bridge — relaying between the CLI's `OperationsService` stream and the agent's `HandleTask` stream.

## ManagementService (CLI → Server)

Node management, authentication, token management, and user management.

```protobuf
service ManagementService {
  // Node management
  rpc ListNodes(ListNodesRequest) returns (ListNodesResponse);
  rpc GetNode(GetNodeRequest) returns (GetNodeResponse);
  rpc RemoveNode(RemoveNodeRequest) returns (RemoveNodeResponse);

  // Authentication
  rpc Login(LoginRequest) returns (LoginResponse);

  // Token management
  rpc RevokeToken(RevokeTokenRequest) returns (RevokeTokenResponse);
  rpc ListTokens(ListTokensRequest) returns (ListTokensResponse);

  // User management
  rpc CreateUser(CreateUserRequest) returns (CreateUserResponse);
  rpc UpdateUser(UpdateUserRequest) returns (UpdateUserResponse);
  rpc DeleteUser(DeleteUserRequest) returns (DeleteUserResponse);
  rpc ListUsers(ListUsersRequest) returns (ListUsersResponse);
}
```

### Node Management

| RPC | Input | Output | Auth |
|-----|-------|--------|------|
| `ListNodes` | — | `repeated NodeSummary` | JWT |
| `GetNode` | `node_id` | `NodeSummary + labels + uptime` | JWT |
| `RemoveNode` | `node_id` | `success/error` | JWT |

### Authentication

| RPC | Input | Output | Auth |
|-----|-------|--------|------|
| `Login` | `username, password` | `token, expires_at` | **None** (public endpoint) |

Login validates credentials against the user store (SQLite). On success, issues a JWT with a unique JTI. The token is also persisted to the token store for listing/revocation.

### Token Management

| RPC | Input | Output | Auth |
|-----|-------|--------|------|
| `RevokeToken` | `token_id (jti)` | `success/error` | JWT |
| `ListTokens` | `kind (optional)` | `repeated TokenInfo` | JWT |

`TokenInfo` includes: `id (jti)`, `kind`, `user_id`, `issued_at`, `expires_at`.

Revoked tokens are loaded into an in-memory set on startup and checked in the gRPC interceptor (O(1) per request).

### User Management

| RPC | Input | Output | Auth |
|-----|-------|--------|------|
| `CreateUser` | `username, password, node_ids` | `success/error` | JWT |
| `UpdateUser` | `username, password?, node_ids?` | `success/error` | JWT |
| `DeleteUser` | `username` | `success/error` | JWT |
| `ListUsers` | — | `repeated UserInfo` | JWT |

`UserInfo` includes: `username`, `node_ids`, `created_at`, `updated_at`, `disabled`.

Update semantics:
- `password` empty → leave unchanged
- `node_ids` nil → leave unchanged
- `node_ids` empty list → clear all node access

## Authentication Flow

### JWT Structure

```
Header: {"alg": "HS256", "typ": "JWT"}
Payload: {
  "sub": "gary",           // username
  "node_ids": ["*"],       // allowed nodes
  "jti": "uuid-...",       // unique token ID
  "exp": 1234567890,       // expiry
  "iat": 1234567890        // issued at
}
```

### gRPC Interceptor

All authenticated RPCs pass through a unary/stream interceptor that:

1. Extracts `authorization: Bearer <token>` from gRPC metadata
2. Verifies JWT signature (HMAC-SHA256)
3. Checks expiry
4. Checks if JTI is in the revoked set → `UNAUTHENTICATED`
5. Injects claims into context

**Bypass exceptions:**
- `ManagementService/Login` — no auth required
- `ControlService/Connect` — agent token validated in handler

## Wire Patterns

| Operation | CLI → Server | Server → Agent | Streaming |
|-----------|-------------|----------------|-----------|
| `exec` | Unary request → server stream | TaskSignal + HandleTask bidi | stdout/stderr chunks |
| `read` | Unary request → server stream | TaskSignal + HandleTask bidi | file chunks |
| `write` | Client stream → unary response | TaskSignal + HandleTask bidi | file chunks |
| `forward` | Bidi stream | TaskSignal + HandleTask bidi | raw TCP bytes |
| `node list` | Unary | — | — |
| `token list` | Unary | — | — |
