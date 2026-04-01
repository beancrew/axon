# Axon 项目结构

> [English Version](../project-structure.md)

## 目录布局

```
axon/
├── cmd/
│   ├── axon/                        # CLI 二进制
│   │   ├── main.go                  # 根命令 + 子命令
│   │   ├── client.go                # gRPC 连接（TLS、认证）
│   │   ├── node.go                  # node list/info/remove
│   │   ├── exec.go / read.go / write.go / forward.go
│   │   └── token.go                 # token list/revoke/create-join/list-join/revoke-join
│   ├── axon-server/main.go          # Server 入口
│   └── axon-agent/main.go           # Agent 入口
│
├── proto/                           # Protocol Buffers 定义
│   ├── control.proto                # Agent ↔ Server 控制面
│   ├── operations.proto             # 操作 + Agent 数据面
│   └── management.proto             # 节点/Token 管理
│
├── gen/proto/                       # 生成代码
│
├── pkg/                             # 公共包（稳定 API）
│   ├── auth/                        # 认证 & 授权
│   │   ├── auth.go                  # JWT 签发 & 验证
│   │   ├── interceptor.go           # gRPC 拦截器（含吊销检查）
│   │   ├── token_checker.go         # 内存吊销 JTI 集合
│   │   ├── token_store.go           # Token SQLite 持久化
│   │   └── user_store.go            # 用户 SQLite 存储（历史保留）
│   ├── audit/                       # 审计日志
│   │   ├── audit.go / store.go / writer.go
│   ├── config/config.go             # 共享配置类型
│   ├── display/display.go           # 输出格式化
│   └── tls/autotls.go               # Auto-TLS 证书管理
│
├── internal/                        # 私有包
│   ├── server/                      # Server 核心
│   │   ├── server.go                # 服务启动、DB 初始化、TLS
│   │   ├── control.go               # ControlService
│   │   ├── operations.go            # OperationsService
│   │   ├── management.go            # ManagementService（节点/Token/Join Token 管理）
│   │   ├── agent_ops.go             # AgentOpsService
│   │   ├── router.go / bridge.go    # 路由 + 流桥接
│   │   └── registry/                # 节点注册表
│   │       ├── registry.go          # 内存注册 + 心跳超时
│   │       ├── store.go             # SQLite 存储
│   │       └── heartbeat_batch.go   # 心跳批量持久化
│   │
│   └── agent/                       # Agent 核心
│       ├── agent.go                 # 主循环：连接、注册、心跳、重连
│       ├── dispatcher.go            # 任务分发
│       ├── exec.go / fileio.go / forward.go
│       └── sysinfo*.go              # 系统信息采集（各平台）
│
├── test/integration/                # 端到端集成测试
├── docs/                            # 文档（英文 + 中文）
├── Makefile
├── README.md
└── CONTRIBUTING.md
```

## 包依赖规则

```
cmd/axon        → gen/proto, pkg/config
cmd/axon-server → internal/server → gen/proto, pkg/auth, pkg/audit, pkg/config, pkg/tls
cmd/axon-agent  → internal/agent  → gen/proto, pkg/config
```

规则：
1. `cmd/` 只有入口点，逻辑在 `internal/` 或 `pkg/`
2. `internal/` 不可被外部模块导入
3. `pkg/` 是稳定 API
4. 无循环依赖
5. `internal/server` 和 `internal/agent` 互不导入

## 构建

```bash
make build          # 编译三个二进制 → bin/
make test           # 运行所有测试（含 race detector）
make lint           # golangci-lint
make proto          # 重新生成 protobuf
```
