# Axon 协议设计

> [English Version](../protocol.md)

## 概述

全链路 gRPC over HTTP/2，Protocol Buffers 序列化。三个 proto 文件定义全部 API。

## Proto 文件

| 文件 | 包 | 服务 | 用途 |
|------|-----|------|------|
| `control.proto` | `axon.control` | `ControlService` | Agent ↔ Server 控制面 |
| `operations.proto` | `axon.operations` | `OperationsService`、`AgentOpsService` | CLI 操作 + Agent 任务处理 |
| `management.proto` | `axon.management` | `ManagementService` | 节点/Token/Join Token 管理 + Agent 注册 |

## ControlService（Agent ↔ Server）

长连接双向流，管理 Agent 生命周期。

```protobuf
service ControlService {
  rpc Connect(stream AgentMessage) returns (stream ServerMessage);
}
```

Agent → Server：`RegisterRequest`、`Heartbeat`、`NodeInfo`
Server → Agent：`RegisterResponse`、`HeartbeatAck`、`TaskSignal`

## OperationsService（CLI → Server）

CLI 端操作，Server 路由到目标 Agent。

```protobuf
service OperationsService {
  rpc Exec(ExecRequest) returns (stream ExecOutput);
  rpc Read(ReadRequest) returns (stream ReadOutput);
  rpc Write(stream WriteInput) returns (WriteResponse);
  rpc Forward(stream TunnelData) returns (stream TunnelData);
}
```

## AgentOpsService（Agent → Server）

Agent 端数据面。收到 `TaskSignal` 后开启 `HandleTask` stream。

```protobuf
service AgentOpsService {
  rpc HandleTask(stream TaskDataUp) returns (stream TaskDataDown);
}
```

Server 作为透明桥接：CLI 的 `OperationsService` stream ↔ Agent 的 `HandleTask` stream。

## ManagementService（CLI → Server）

```protobuf
service ManagementService {
  // 节点管理
  rpc ListNodes(ListNodesRequest) returns (ListNodesResponse);
  rpc GetNode(GetNodeRequest) returns (GetNodeResponse);
  rpc RemoveNode(RemoveNodeRequest) returns (RemoveNodeResponse);

  // Token 管理
  rpc RevokeToken(RevokeTokenRequest) returns (RevokeTokenResponse);
  rpc ListTokens(ListTokensRequest) returns (ListTokensResponse);

  // Join Token 管理
  rpc CreateJoinToken(CreateJoinTokenRequest) returns (CreateJoinTokenResponse);
  rpc ListJoinTokens(ListJoinTokensRequest) returns (ListJoinTokensResponse);
  rpc RevokeJoinToken(RevokeJoinTokenRequest) returns (RevokeJoinTokenResponse);

  // Agent 注册（无需认证 — token 自验证）
  rpc JoinAgent(JoinAgentRequest) returns (JoinAgentResponse);
}
```

### Token 管理

- `RevokeToken`/`ListTokens`：需要 JWT 认证
- 吊销的 Token 在内存 set 中 O(1) 检查

### Join Token 管理

| RPC | 输入 | 输出 | 认证 |
|-----|------|------|------|
| `CreateJoinToken` | `max_uses, expires_seconds` | `token, id` | JWT |
| `ListJoinTokens` | — | `repeated JoinTokenInfo` | JWT |
| `RevokeJoinToken` | `id` | `success/error` | JWT |

Join token 是一次性或限次使用的令牌，允许新 Agent 无需现有 JWT 即可注册。

### Agent 注册

| RPC | 输入 | 输出 | 认证 |
|-----|------|------|------|
| `JoinAgent` | `join_token, node_name, info` | `agent_token, node_id, ca_cert_pem` | **无**（token 自验证） |

Agent 提交 join token → Server 验证 → 分配稳定 `node_id` → 签发 Agent JWT → 返回 CA 证书 PEM（如启用 TLS）。

## 认证流程

### Token 认证

使用预签发 Token 认证。`axon-server init` 生成：
- **Admin CLI Token**（永不过期，可访问所有节点）
- **初始 Join Token**（用于注册第一个 Agent）

没有 Login RPC — Token 在 init 时或通过 `CreateJoinToken` 签发。

### JWT 结构

```
Header: {"alg": "HS256", "typ": "JWT"}
Payload: {
  "sub": "admin",           // Token 身份
  "node_ids": ["*"],        // 允许的节点（"*" = 全部）
  "jti": "uuid-...",        // 唯一 Token ID（用于吊销）
  "exp": 0,                 // 过期时间（0 = 永不过期）
  "iat": 1234567890         // 签发时间
}
```

### gRPC 拦截器

1. 从 gRPC metadata 提取 `authorization: Bearer <token>`
2. 验证 HMAC-SHA256 签名
3. 检查过期
4. 检查 JTI 是否已吊销 → `UNAUTHENTICATED`
5. 注入 claims 到 context

**豁免**：`ManagementService/JoinAgent`（token 自验证）、`ControlService/Connect`（Handler 内验证）
