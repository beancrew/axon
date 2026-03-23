# Axon 项目结构

## 目录布局

```
axon/
├── cmd/
│   ├── axon/                    # CLI 二进制入口
│   │   └── main.go
│   ├── axon-server/             # Server 二进制入口
│   │   └── main.go
│   └── axon-agent/              # Agent 二进制入口
│       └── main.go
│
├── proto/                       # Protocol Buffers 定义
│   ├── control.proto            # Agent ↔ Server 控制面
│   ├── operations.proto         # exec/read/write/forward
│   └── management.proto         # 节点管理 + 认证
│
├── gen/                         # proto 生成的代码（可 gitignore 或提交）
│   └── proto/
│       ├── control/
│       ├── operations/
│       └── management/
│
├── pkg/                         # 公共包（稳定 API）
│   ├── auth/                    # JWT token 生成与验证
│   │   ├── jwt.go
│   │   └── jwt_test.go
│   ├── audit/                   # 审计日志
│   │   ├── logger.go
│   │   ├── sqlite.go
│   │   └── logger_test.go
│   └── config/                  # 共享配置加载工具
│       └── config.go
│
├── internal/                    # 私有包（实现细节）
│   ├── server/                  # Server 核心逻辑
│   │   ├── server.go            # gRPC server 启动
│   │   ├── registry.go          # 节点注册中心（内存）
│   │   ├── router.go            # 请求路由 + stream 桥接
│   │   ├── control.go           # ControlService 实现
│   │   ├── operations.go        # OperationsService 实现
│   │   ├── management.go        # ManagementService 实现
│   │   └── heartbeat.go         # 心跳超时监控
│   │
│   ├── agent/                   # Agent 核心逻辑
│   │   ├── agent.go             # Agent 主循环
│   │   ├── control.go           # 控制面（注册、心跳）
│   │   ├── executor.go          # exec：进程启动 + 流式输出
│   │   ├── fileio.go            # read/write：文件操作
│   │   ├── tunnel.go            # forward：TCP 隧道
│   │   └── reconnect.go         # 指数退避重连
│   │
│   └── cli/                     # CLI 核心逻辑
│       ├── root.go              # 根命令（cobra）
│       ├── node.go              # node list/info/remove
│       ├── exec.go              # exec 命令
│       ├── read.go              # read 命令
│       ├── write.go             # write 命令
│       ├── forward.go           # forward 命令
│       ├── auth.go              # auth login/token
│       ├── config.go            # config set/get
│       └── output.go            # 输出格式化（表格、JSON）
│
├── docs/                        # 设计文档（英文）
│   ├── architecture.md
│   ├── protocol.md
│   ├── cli.md
│   ├── agent.md
│   ├── server.md
│   ├── project-structure.md
│   └── zh/                      # 设计文档（中文）
│       ├── architecture.md
│       ├── protocol.md
│       ├── cli.md
│       ├── agent.md
│       ├── server.md
│       └── project-structure.md
│
├── scripts/                     # 构建和开发脚本
│   ├── build.sh                 # 跨平台构建
│   ├── proto-gen.sh             # Protobuf 代码生成
│   └── dev-setup.sh             # 开发环境配置
│
├── Makefile                     # 构建目标
├── go.mod
├── go.sum
├── README.md
├── CONTRIBUTING.md
└── .gitignore
```

## 包依赖规则

```
cmd/axon        → internal/cli   → gen/proto, pkg/config
cmd/axon-server → internal/server → gen/proto, pkg/auth, pkg/audit, pkg/config
cmd/axon-agent  → internal/agent  → gen/proto, pkg/config

pkg/auth    → （独立，不依赖 internal）
pkg/audit   → （独立，不依赖 internal）
pkg/config  → （独立，不依赖 internal）
```

**规则：**
1. `cmd/` 只放 `main.go` —— 所有逻辑在 `internal/` 或 `pkg/` 中
2. `internal/` 包不能被模块外部导入
3. `pkg/` 包是稳定 API，跨组件共享
4. 禁止循环依赖
5. `internal/server`、`internal/agent`、`internal/cli` 互不导入

## 构建目标

```makefile
# 构建全部三个二进制
make build

# 单独构建
make build-cli
make build-server
make build-agent

# 生成 proto 代码
make proto

# 运行测试
make test

# 跨平台编译（linux/darwin × amd64/arm64）
make release

# 代码检查
make lint
```

## Makefile

```makefile
VERSION ?= $(shell git describe --tags --always --dirty)
LDFLAGS := -ldflags "-X main.version=$(VERSION)"

.PHONY: build build-cli build-server build-agent proto test lint clean

build: build-cli build-server build-agent

build-cli:
	go build $(LDFLAGS) -o bin/axon ./cmd/axon

build-server:
	go build $(LDFLAGS) -o bin/axon-server ./cmd/axon-server

build-agent:
	go build $(LDFLAGS) -o bin/axon-agent ./cmd/axon-agent

proto:
	./scripts/proto-gen.sh

test:
	go test ./...

lint:
	golangci-lint run ./...

clean:
	rm -rf bin/

release:
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o bin/axon-linux-amd64 ./cmd/axon
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o bin/axon-linux-arm64 ./cmd/axon
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o bin/axon-darwin-amd64 ./cmd/axon
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o bin/axon-darwin-arm64 ./cmd/axon
	# axon-server 和 axon-agent 同理...
```
