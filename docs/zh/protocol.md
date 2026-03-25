# Axon 协议设计

> [English Version](../protocol.md)

## 概述

全链路 gRPC over HTTP/2，Protocol Buffers 序列化。三个 proto 文件定义全部 API。

## Proto 文件

| 文件 | 包 | 服务 | 用途 |
|------|-----|------|------|
| `control.proto` | `axon.control` | `ControlService` | Agent ↔ Server 控制面 |
| `operations.proto` | `axon.operations` | `OperationsService`、`AgentOpsService` | CLI 操作 + Agent 任务处理 |
| `management.proto` | `axon.management` | `ManagementService` | 节点/用户/Token 管理 + 认证 |

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

  // 认证
  rpc Login(LoginRequest) returns (LoginResponse);

  // Token 管理
  rpc RevokeToken(RevokeTokenRequest) returns (RevokeTokenResponse);
  rpc ListTokens(ListTokensRequest) returns (ListTokensResponse);

  // 用户管理
  rpc CreateUser(CreateUserRequest) returns (CreateUserResponse);
  rpc UpdateUser(UpdateUserRequest) returns (UpdateUserResponse);
  rpc DeleteUser(DeleteUserRequest) returns (DeleteUserResponse);
  rpc ListUsers(ListUsersRequest) returns (ListUsersResponse);
}
```

### 认证相关

- `Login`：无需认证，验证用户名密码，返回带 JTI 的 JWT
- `RevokeToken`/`ListTokens`：需要 JWT 认证
- 吊销的 Token 在内存 set 中 O(1) 检查

### 用户管理

- `CreateUser`：用户名 + 密码 + 节点权限
- `UpdateUser`：密码为空 = 不改；node_ids 为 nil = 不改
- `DeleteUser`：按用户名删除
- `ListUsers`：返回所有用户信息（不含密码哈希）

## 认证流程

### JWT 结构

```
Header: {"alg": "HS256", "typ": "JWT"}
Payload: {
  "sub": "gary",           // 用户名
  "node_ids": ["*"],       // 允许的节点
  "jti": "uuid-...",       // 唯一 Token ID
  "exp": 1234567890,       // 过期时间
  "iat": 1234567890        // 签发时间
}
```

### gRPC 拦截器

1. 从 gRPC metadata 提取 `authorization: Bearer <token>`
2. 验证 HMAC-SHA256 签名
3. 检查过期
4. 检查 JTI 是否已吊销 → `UNAUTHENTICATED`
5. 注入 claims 到 context

**豁免**：`ManagementService/Login`（无需认证）、`ControlService/Connect`（Handler 内验证）
