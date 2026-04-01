# Axon Protocol Design

> [中文](zh/protocol.md)

## Overview

All communication uses gRPC over HTTP/2 with Protocol Buffers serialization. Three proto files define the entire API surface.

## Proto Files

| File | Package | Services | Purpose |
|------|---------|----------|---------|
| `control.proto` | `axon.control` | `ControlService` | Agent ↔ Server control plane |
| `operations.proto` | `axon.operations` | `OperationsService`, `AgentOpsService` | CLI operations + agent task handling |
| `management.proto` | `axon.management` | `ManagementService` | Node/token/join-token management + agent enrollment |

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

Node management, token management, and join token management.

```protobuf
service ManagementService {
  // Node management
  rpc ListNodes(ListNodesRequest) returns (ListNodesResponse);
  rpc GetNode(GetNodeRequest) returns (GetNodeResponse);
  rpc RemoveNode(RemoveNodeRequest) returns (RemoveNodeResponse);

  // Token management
  rpc RevokeToken(RevokeTokenRequest) returns (RevokeTokenResponse);
  rpc ListTokens(ListTokensRequest) returns (ListTokensResponse);

  // Join token management
  rpc CreateJoinToken(CreateJoinTokenRequest) returns (CreateJoinTokenResponse);
  rpc ListJoinTokens(ListJoinTokensRequest) returns (ListJoinTokensResponse);
  rpc RevokeJoinToken(RevokeJoinTokenRequest) returns (RevokeJoinTokenResponse);

  // Agent enrollment (no auth — token is self-authenticating)
  rpc JoinAgent(JoinAgentRequest) returns (JoinAgentResponse);
}
```

### Node Management

| RPC | Input | Output | Auth |
|-----|-------|--------|------|
| `ListNodes` | — | `repeated NodeSummary` | JWT |
| `GetNode` | `node_id` | `NodeSummary + labels + uptime` | JWT |
| `RemoveNode` | `node_id` | `success/error` | JWT |

### Token Management

| RPC | Input | Output | Auth |
|-----|-------|--------|------|
| `RevokeToken` | `token_id (jti)` | `success/error` | JWT |
| `ListTokens` | `kind (optional)` | `repeated TokenInfo` | JWT |

`TokenInfo` includes: `id (jti)`, `kind`, `user_id`, `issued_at`, `expires_at`.

Revoked tokens are loaded into an in-memory set on startup and checked in the gRPC interceptor (O(1) per request).

### Join Token Management

| RPC | Input | Output | Auth |
|-----|-------|--------|------|
| `CreateJoinToken` | `max_uses, expires_seconds` | `token, id` | JWT |
| `ListJoinTokens` | — | `repeated JoinTokenInfo` | JWT |
| `RevokeJoinToken` | `id` | `success/error` | JWT |

`JoinTokenInfo` includes: `id`, `created_at`, `uses`, `max_uses`, `expires_at`, `revoked`.

Join tokens are one-time-use or limited-use tokens that allow new agents to enroll without needing a pre-existing JWT.

### Agent Enrollment

| RPC | Input | Output | Auth |
|-----|-------|--------|------|
| `JoinAgent` | `join_token, node_name, info` | `agent_token, node_id, ca_cert_pem` | **None** (token is self-authenticating) |

The agent presents a join token. The server validates it, assigns a stable `node_id`, signs an agent JWT, and returns the CA certificate PEM (if TLS is enabled). The agent saves the config and uses the JWT for subsequent `Connect` calls.

## Authentication Flow

### Token-Based Auth

Authentication uses pre-issued tokens. `axon-server init` generates:
- An **admin CLI token** (no expiry, access to all nodes)
- An **initial join token** (for enrolling the first agent)

There is no login RPC — tokens are issued at init time or via `CreateJoinToken`.

### JWT Structure

```
Header: {"alg": "HS256", "typ": "JWT"}
Payload: {
  "sub": "admin",           // token identity
  "node_ids": ["*"],        // allowed nodes ("*" = all)
  "jti": "uuid-...",        // unique token ID (for revocation)
  "exp": 0,                 // expiry (0 = never)
  "iat": 1234567890         // issued at
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
- `ManagementService/JoinAgent` — join token is self-authenticating
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
| `join-token` | Unary | — | — |
