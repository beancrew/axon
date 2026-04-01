# Axon Project Structure

> [中文版 / Chinese](zh/project-structure.md)

## Directory Layout

```
axon/
├── cmd/
│   ├── axon/                        # CLI binary
│   │   ├── main.go                  # Root command + subcommands
│   │   ├── client.go                # gRPC connection helper (TLS, auth)
│   │   ├── node.go                  # node list/info/remove
│   │   ├── exec.go                  # exec command
│   │   ├── read.go                  # read command
│   │   ├── write.go                 # write command
│   │   ├── forward.go               # forward command
│   │   └── token.go                 # token list/revoke/create-join/list-join/revoke-join
│   ├── axon-server/                 # Server binary
│   │   └── main.go                  # Config loading + server start
│   └── axon-agent/                  # Agent binary
│       └── main.go                  # Config loading + agent start
│
├── proto/                           # Protocol Buffers definitions
│   ├── control.proto                # Agent ↔ Server control plane
│   ├── operations.proto             # exec/read/write/forward + AgentOps
│   └── management.proto             # Node/token/join-token management + enrollment
│
├── gen/                             # Generated code from proto
│   └── proto/
│       ├── control/
│       ├── operations/
│       └── management/
│
├── pkg/                             # Public packages (stable API)
│   ├── auth/                        # Authentication & authorization
│   │   ├── auth.go                  # JWT signing & verification
│   │   ├── interceptor.go           # gRPC auth interceptors (with revocation check)
│   │   ├── token_checker.go         # In-memory revoked JTI set
│   │   ├── token_store.go           # Token persistence (SQLite)
│   │   └── user_store.go            # User persistence (SQLite CRUD)
│   ├── audit/                       # Audit logging
│   │   ├── audit.go                 # Entry types
│   │   ├── store.go                 # SQLite store
│   │   └── writer.go                # Async buffered writer
│   ├── config/                      # Shared config types + loading
│   │   └── config.go                # ServerConfig, AgentConfig, CLIConfig
│   ├── display/                     # Output formatting
│   │   └── display.go               # Table + JSON output helpers
│   └── tls/                         # TLS certificate management
│       └── autotls.go               # Auto-TLS: CA + server cert generation
│
├── internal/                        # Private packages
│   ├── server/                      # Server core logic
│   │   ├── server.go                # gRPC server setup, DB init, TLS, lifecycle
│   │   ├── control.go               # ControlService (agent registration + heartbeat)
│   │   ├── operations.go            # OperationsService (CLI → agent routing)
│   │   ├── management.go            # ManagementService (node/token/join-token management)
│   │   ├── agent_ops.go             # AgentOpsService (agent data plane)
│   │   ├── router.go                # Request routing + stream bridging
│   │   ├── bridge.go                # CLI ↔ Agent stream bridge
│   │   ├── testing.go               # Test helper (bufconn server)
│   │   └── registry/                # Node registry subsystem
│   │       ├── registry.go          # In-memory registry + heartbeat timeout
│   │       ├── store.go             # SQLite backing store (CRUD + upsert)
│   │       └── heartbeat_batch.go   # Batched heartbeat persistence
│   │
│   └── agent/                       # Agent core logic
│       ├── agent.go                 # Main loop: connect, register, heartbeat, reconnect
│       ├── dispatcher.go            # Task dispatch from control stream
│       ├── exec.go                  # exec: process spawning + streaming
│       ├── fileio.go                # read/write: file operations
│       ├── forward.go               # forward: TCP tunneling
│       ├── sysinfo.go               # System info collection (shared)
│       ├── sysinfo_linux.go         # Linux-specific (os-release)
│       ├── sysinfo_darwin.go        # macOS-specific (sw_vers)
│       ├── sysinfo_windows.go       # Windows-specific (RtlGetVersion)
│       └── testing.go               # Test helpers
│
├── test/
│   └── integration/                 # End-to-end integration tests
│       ├── integration_test.go
│       └── testharness/
│           └── harness.go           # Test server + agent setup
│
├── docs/                            # Documentation (English)
│   ├── quickstart.md                # Quick start guide
│   ├── configuration.md             # Configuration reference
│   ├── architecture.md              # Architecture overview
│   ├── protocol.md                  # Protocol design (proto details)
│   ├── cli.md                       # CLI command reference
│   ├── server.md                    # Server design
│   ├── agent.md                     # Agent design
│   ├── project-structure.md         # This file
│   ├── internal/                    # Internal design docs (historical)
│   │   ├── phase2-design.md
│   │   ├── data-plane-bridge.md
│   │   ├── join-token-design.md
│   │   └── tasks-phase1.md
│   └── zh/                          # Documentation (Chinese)
│
├── Makefile
├── go.mod
├── go.sum
├── README.md
├── CONTRIBUTING.md
└── .gitignore
```

## Package Dependency Rules

```
cmd/axon        → gen/proto, pkg/config
cmd/axon-server → internal/server → gen/proto, pkg/auth, pkg/audit, pkg/config, pkg/tls
cmd/axon-agent  → internal/agent  → gen/proto, pkg/config

pkg/auth    → (standalone, no internal imports)
pkg/audit   → (standalone, no internal imports)
pkg/config  → (standalone, no internal imports)
pkg/tls     → (standalone, no internal imports)
```

**Rules:**
1. `cmd/` contains entry points — all logic in `internal/` or `pkg/`
2. `internal/` packages cannot be imported outside the module
3. `pkg/` packages are stable APIs shared across components
4. No circular dependencies
5. `internal/server` and `internal/agent` do not import each other

## Build Targets

```bash
make build          # Build all three binaries → bin/
make test           # Run all tests with race detector
make lint           # Run golangci-lint
make proto          # Regenerate protobuf code
make clean          # Remove bin/
```
