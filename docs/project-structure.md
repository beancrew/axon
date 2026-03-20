# Axon Project Structure

## Directory Layout

```
axon/
├── cmd/
│   ├── axon/                    # CLI binary entry point
│   │   └── main.go
│   ├── axon-server/             # Server binary entry point
│   │   └── main.go
│   └── axon-agent/              # Agent binary entry point
│       └── main.go
│
├── proto/                       # Protocol Buffers definitions
│   ├── control.proto            # Agent ↔ Server control plane
│   ├── operations.proto         # exec/read/write/forward
│   └── management.proto         # Node management + auth
│
├── gen/                         # Generated code from proto (gitignored or committed)
│   └── proto/
│       ├── control/
│       ├── operations/
│       └── management/
│
├── pkg/                         # Public packages (stable API)
│   ├── auth/                    # JWT token generation & validation
│   │   ├── jwt.go
│   │   └── jwt_test.go
│   ├── audit/                   # Audit logging
│   │   ├── logger.go
│   │   ├── sqlite.go
│   │   └── logger_test.go
│   └── config/                  # Shared config loading utilities
│       └── config.go
│
├── internal/                    # Private packages (implementation details)
│   ├── server/                  # Server core logic
│   │   ├── server.go            # gRPC server setup
│   │   ├── registry.go          # Node registry (in-memory)
│   │   ├── router.go            # Request routing + stream bridging
│   │   ├── control.go           # ControlService implementation
│   │   ├── operations.go        # OperationsService implementation
│   │   ├── management.go        # ManagementService implementation
│   │   └── heartbeat.go         # Heartbeat timeout monitor
│   │
│   ├── agent/                   # Agent core logic
│   │   ├── agent.go             # Main agent loop
│   │   ├── control.go           # Control plane (register, heartbeat)
│   │   ├── executor.go          # exec: process spawning + streaming
│   │   ├── fileio.go            # read/write: file operations
│   │   ├── tunnel.go            # forward: TCP tunneling
│   │   └── reconnect.go         # Exponential backoff reconnection
│   │
│   └── cli/                     # CLI core logic
│       ├── root.go              # Root command (cobra)
│       ├── node.go              # node list/info/remove
│       ├── exec.go              # exec command
│       ├── read.go              # read command
│       ├── write.go             # write command
│       ├── forward.go           # forward command
│       ├── auth.go              # auth login/token
│       ├── config.go            # config set/get
│       └── output.go            # Output formatting (table, JSON)
│
├── docs/                        # Design documents
│   ├── architecture.md          # Architecture overview
│   ├── protocol.md              # Protocol design (proto details)
│   ├── cli.md                   # CLI design
│   ├── agent.md                 # Agent design
│   ├── server.md                # Server design
│   └── project-structure.md     # This file
│
├── scripts/                     # Build & dev scripts
│   ├── build.sh                 # Cross-platform build
│   ├── proto-gen.sh             # Protobuf code generation
│   └── dev-setup.sh             # Dev environment setup
│
├── Makefile                     # Build targets
├── go.mod
├── go.sum
├── README.md
├── CONTRIBUTING.md
└── .gitignore
```

## Package Dependency Rules

```
cmd/axon        → internal/cli   → gen/proto, pkg/config
cmd/axon-server → internal/server → gen/proto, pkg/auth, pkg/audit, pkg/config
cmd/axon-agent  → internal/agent  → gen/proto, pkg/config

pkg/auth    → (standalone, no internal imports)
pkg/audit   → (standalone, no internal imports)
pkg/config  → (standalone, no internal imports)
```

**Rules:**
1. `cmd/` only contains `main.go` — all logic in `internal/` or `pkg/`
2. `internal/` packages cannot be imported outside the module
3. `pkg/` packages are stable APIs shared across components
4. No circular dependencies
5. `internal/server`, `internal/agent`, `internal/cli` do not import each other

## Build Targets

```makefile
# Build all three binaries
make build

# Build individual
make build-cli
make build-server
make build-agent

# Generate proto
make proto

# Run tests
make test

# Cross-compile (linux/darwin × amd64/arm64)
make release

# Lint
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
	# Repeat for axon-server and axon-agent...
```

---

# Axon 项目结构

## 目录

- `cmd/` — 三个二进制入口（axon, axon-server, axon-agent），只放 main.go
- `proto/` — Protocol Buffers 定义（control, operations, management）
- `gen/` — proto 生成的 Go 代码
- `pkg/` — 公共包（auth, audit, config），跨组件共享
- `internal/` — 私有包（server, agent, cli 核心逻辑），不对外暴露
- `docs/` — 设计文档
- `scripts/` — 构建和开发脚本

## 依赖规则

- cmd → internal → gen/proto + pkg
- pkg 包独立，不依赖 internal
- internal/server、internal/agent、internal/cli 互不依赖
- 禁止循环依赖
