# Axon Phase 1 Task Breakdown

## Overview

Phase 1 goal: **A working end-to-end system** — CLI can exec/read/write/forward on remote nodes through Server, with auth and audit.

### Role Assignment

| Role | Owner | Scope |
|------|-------|-------|
| ⚙️ Axon Coder (小 c) | Coding | All application code |
| 🔍 Axon QA | Testing + CI/CD | Test suites, GitHub Actions, build pipeline |
| 🧠 Axon Planner | Design | Design docs, task breakdown, review |
| 👤 老大 | Decision | PR review, final approval |

---

## ⚙️ Coder Tasks (小 c)

Tasks are ordered by dependency. Each task = one branch + one PR.

### Stage 1: Project Scaffold + Proto

**Task C-1: Project initialization**
- Branch: `coder/init-project`
- Scope:
  - `go mod init github.com/garysng/axon`
  - Directory structure: `cmd/`, `proto/`, `pkg/`, `internal/`, `docs/`, `scripts/`
  - `.gitignore` (binary, gen/, vendor/, .env)
  - Empty `main.go` for all three binaries (build must pass)
  - `Makefile` with build/test/lint/proto targets
- Done when: `make build` compiles three empty binaries

**Task C-2: Proto definitions + code generation**
- Branch: `coder/proto`
- Depends on: C-1
- Scope:
  - `proto/control.proto` — ControlService, AgentMessage, ServerMessage, NodeInfo, OSInfo
  - `proto/operations.proto` — OperationsService, Exec/Read/Write/Forward messages
  - `proto/management.proto` — ManagementService, NodeSummary, Login messages
  - `scripts/proto-gen.sh` — protoc + go plugin code generation
  - Generated code in `gen/proto/`
- Done when: `make proto && make build` passes

### Stage 2: Shared Libraries

**Task C-3: Auth module (pkg/auth)**
- Branch: `coder/auth`
- Depends on: C-1
- Scope:
  - JWT token generation (HMAC-SHA256)
  - JWT token validation (signature, expiry)
  - CLI token: user_id + node_ids
  - Agent token: node_id
  - gRPC interceptor for token extraction/validation
- Done when: unit tests pass for sign/verify/expiry/invalid-token

**Task C-4: Audit module (pkg/audit)**
- Branch: `coder/audit`
- Depends on: C-1
- Scope:
  - AuditEntry struct
  - SQLite storage (create table, insert, query)
  - Async writer (buffered channel + background goroutine)
  - Query interface (by time/node/user)
- Done when: unit tests pass for write/read/async-flush

**Task C-5: Config module (pkg/config)**
- Branch: `coder/config`
- Depends on: C-1
- Scope:
  - YAML config loading for server, agent, CLI
  - Environment variable override support
  - Config file paths: `~/.axon/config.yaml`, `~/.axon-agent/config.yaml`, `/etc/axon-server/config.yaml`
- Done when: unit tests pass for load/override/defaults

### Stage 3: Server Core

**Task C-6: Node registry (internal/server/registry)**
- Branch: `coder/registry`
- Depends on: C-2
- Scope:
  - In-memory node registry (NodeEntry struct)
  - Register, update heartbeat, mark offline, remove, list, lookup
  - Heartbeat timeout monitor (background goroutine)
  - Thread-safe (sync.RWMutex)
- Done when: unit tests pass for all registry operations + timeout detection

**Task C-7: Server gRPC + Control plane**
- Branch: `coder/server-control`
- Depends on: C-2, C-3, C-6
- Scope:
  - gRPC server setup (TLS)
  - ControlService implementation: Connect() handler
  - Agent registration flow (validate token → register → return node_id)
  - Heartbeat handling (receive → ack → update registry)
  - TaskSignal dispatch (to be used by router)
- Done when: integration test — mock agent connects, registers, heartbeats, server tracks state

**Task C-8: Server router + Operations bridge**
- Branch: `coder/server-router`
- Depends on: C-7, C-4
- Scope:
  - Router: authenticate → authorize (check node_ids in JWT) → lookup node → dispatch
  - OperationsService implementation: Exec, Read, Write, Forward
  - Stream bridging: CLI stream ↔ Agent stream
  - Audit logging on each operation
  - ManagementService: ListNodes, GetNode, RemoveNode, Login
- Done when: integration test — full request flow from CLI mock → Server → Agent mock

### Stage 4: Agent

**Task C-9: Agent control plane**
- Branch: `coder/agent-control`
- Depends on: C-2, C-5
- Scope:
  - Agent main loop: connect → register → heartbeat loop
  - Config loading (first run save, subsequent read)
  - Start-and-register flow
  - Heartbeat sender (interval from server)
  - NodeInfo + OSInfo collection (Linux: /etc/os-release + uname, macOS: sw_vers + uname)
  - Exponential backoff reconnection (1s → 60s, ±20% jitter)
  - TaskSignal receiver (dispatch to data plane handlers)
- Done when: agent connects to test server, registers, heartbeats, reconnects on disconnect

**Task C-10: Agent exec handler**
- Branch: `coder/agent-exec`
- Depends on: C-9
- Scope:
  - Receive ExecRequest → spawn local process
  - Stream stdout/stderr back as ExecOutput
  - Exit code forwarding
  - Timeout handling (SIGTERM → 5s → SIGKILL)
  - Cancellation (gRPC context cancel → SIGTERM)
  - Environment variables + working directory support
- Done when: end-to-end test — CLI exec → Server → Agent → real command execution

**Task C-11: Agent read/write handlers**
- Branch: `coder/agent-fileio`
- Depends on: C-9
- Scope:
  - Read: stat file → send meta → stream chunks (32KB)
  - Write: receive header → create dirs → atomic write (tmpfile + rename)
  - Error handling: not found, permission denied, is directory
- Done when: end-to-end test — read/write files through full stack

**Task C-12: Agent forward (port tunneling)**
- Branch: `coder/agent-forward`
- Depends on: C-9
- Scope:
  - Receive TunnelOpen → dial localhost:port
  - Bidirectional relay: gRPC stream ↔ TCP connection
  - Connection close handling
  - Error: target port unreachable
- Done when: end-to-end test — forward port, TCP data flows both ways

### Stage 5: CLI

**Task C-13: CLI framework + config commands**
- Branch: `coder/cli-framework`
- Depends on: C-2, C-5
- Scope:
  - cobra command structure (root, node, exec, read, write, forward, auth, config, version)
  - Config commands: `config set/get`, `auth token`, `version`
  - Output formatting: table + JSON (`--json` flag)
  - gRPC client connection helper (read server + token from config, TLS)
- Done when: `axon version`, `axon config set/get` work

**Task C-14: CLI node + auth commands**
- Branch: `coder/cli-node-auth`
- Depends on: C-13, C-8
- Scope:
  - `axon node list` (with `--status` filter)
  - `axon node info <node>`
  - `axon node remove <node>`
  - `axon auth login` (prompt username/password, save token)
- Done when: CLI commands work against running server

**Task C-15: CLI core operations**
- Branch: `coder/cli-operations`
- Depends on: C-13, C-8
- Scope:
  - `axon exec <node> <cmd>` — stream stdout/stderr, forward exit code, Ctrl+C cancel
  - `axon read <node> <path>` — stream to stdout, `--meta` flag
  - `axon write <node> <path>` — stream from stdin, `--mode` flag
  - `axon forward <node> <L>:<R>` — local listener, per-connection stream, `--bind` flag
- Done when: all four operations work end-to-end (CLI → Server → Agent → real target)

### Stage 6: Agent Binary + Daemon

**Task C-16: Agent binary (cmd/axon-agent)**
- Branch: `coder/agent-binary`
- Depends on: C-9, C-10, C-11, C-12
- Scope:
  - `axon-agent start` (--server, --token, --name, --labels, --foreground)
  - `axon-agent stop` (SIGTERM to daemon, graceful shutdown)
  - `axon-agent status` (process check + connection health)
  - `axon-agent config set/get`
  - `axon-agent version`
  - Daemonization (or foreground mode)
- Done when: full agent binary works as daemon, can be started/stopped/status-checked

---

## 🔍 QA Tasks

### CI/CD (QA scope)

**Task Q-1: GitHub Actions — CI pipeline**
- Branch: `qa/ci-pipeline`
- Scope:
  - `.github/workflows/ci.yml`
  - Trigger: push to any branch, PR to main
  - Steps:
    1. Go setup (version matrix: 1.22+)
    2. `make proto` (install protoc + plugins)
    3. `make lint` (golangci-lint)
    4. `make test` (go test ./... -race -cover)
    5. `make build` (compile all three binaries)
  - Coverage report upload
  - Status check required for PR merge
- Done when: CI runs green on PR

**Task Q-2: GitHub Actions — proto validation**
- Branch: `qa/proto-check`
- Depends on: Q-1, C-2
- Scope:
  - CI step: generate proto → check no diff (ensure generated code is committed and up to date)
  - `buf lint` for proto style validation (optional but recommended)
- Done when: proto changes trigger validation in CI

**Task Q-3: Integration test framework**
- Branch: `qa/integration-tests`
- Depends on: C-8
- Scope:
  - Test harness: spin up server + agent in-process
  - Helper functions for common assertions
  - End-to-end test suite structure
  - Tests for: connect, register, heartbeat, exec, read, write, forward
- Done when: integration test suite runs in CI

**Task Q-4: GitHub Actions — release pipeline**
- Branch: `qa/release-pipeline`
- Depends on: Q-1
- Scope:
  - `.github/workflows/release.yml`
  - Trigger: tag push (`v*`)
  - Steps:
    1. Cross-compile: linux/darwin × amd64/arm64 × 3 binaries
    2. Create GitHub Release
    3. Upload binaries as release assets
    4. Generate checksums
- Done when: pushing a tag creates a release with all binaries

---

## Task Dependency Graph

```
C-1 (scaffold)
 ├── C-2 (proto) ─────┐
 ├── C-3 (auth) ──────┤
 ├── C-4 (audit) ─────┤
 └── C-5 (config) ────┤
                       │
 C-6 (registry) ◄─── C-2
                       │
 C-7 (server-ctrl) ◄─ C-2 + C-3 + C-6
                       │
 C-8 (server-router) ◄ C-7 + C-4
                       │
 C-9 (agent-ctrl) ◄── C-2 + C-5
 ├── C-10 (exec) ◄─── C-9
 ├── C-11 (fileio) ◄── C-9
 └── C-12 (forward) ◄─ C-9
                       │
 C-13 (cli-frame) ◄── C-2 + C-5
 ├── C-14 (cli-node) ◄ C-13 + C-8
 └── C-15 (cli-ops) ◄─ C-13 + C-8
                       │
 C-16 (agent-bin) ◄── C-9 + C-10 + C-11 + C-12

 Q-1 (CI) ── standalone
 Q-2 (proto-check) ◄── Q-1 + C-2
 Q-3 (integration) ◄── C-8
 Q-4 (release) ◄────── Q-1
```

## Suggested Execution Order

**小 c (Coder) 建议顺序：**

| 顺序 | 任务 | 预估 |
|:----:|------|------|
| 1 | C-1 项目初始化 | 0.5d |
| 2 | C-2 Proto 定义 + 代码生成 | 1d |
| 3 | C-3 Auth 模块 | 1d |
| 4 | C-4 Audit 模块 | 1d |
| 5 | C-5 Config 模块 | 0.5d |
| 6 | C-6 节点注册中心 | 1d |
| 7 | C-7 Server 控制面 | 1.5d |
| 8 | C-8 Server 路由 + 操作桥接 | 2d |
| 9 | C-9 Agent 控制面 | 1.5d |
| 10 | C-10 Agent exec | 1d |
| 11 | C-11 Agent read/write | 1d |
| 12 | C-12 Agent forward | 1d |
| 13 | C-13 CLI 框架 | 1d |
| 14 | C-14 CLI node + auth | 1d |
| 15 | C-15 CLI 核心操作 | 1.5d |
| 16 | C-16 Agent 二进制 | 1d |
| | **合计** | **~16d** |

**QA 建议顺序（可与 Coder 并行）：**

| 顺序 | 任务 | 依赖 | 预估 |
|:----:|------|------|------|
| 1 | Q-1 CI pipeline | C-1 完成后即可开始 | 1d |
| 2 | Q-2 Proto validation | Q-1 + C-2 | 0.5d |
| 3 | Q-3 Integration tests | C-8 完成后 | 2d |
| 4 | Q-4 Release pipeline | Q-1 | 1d |

---

# Axon Phase 1 任务拆分

## 角色分工

| 角色 | 负责人 | 范围 |
|------|--------|------|
| ⚙️ 小 c | 编码 | 所有业务代码 |
| 🔍 QA | 测试 + CI/CD | 测试用例、GitHub Actions、构建流水线 |
| 🧠 Planner | 设计 | 设计文档、任务拆分、review |
| 👤 老大 | 决策 | PR review、最终审批 |

## 小 c 任务（16 个，按依赖排序）

1. **C-1** 项目初始化（go mod, 目录结构, Makefile）
2. **C-2** Proto 定义 + 代码生成
3. **C-3** Auth 模块（JWT 签发/校验）
4. **C-4** Audit 模块（SQLite 异步写入）
5. **C-5** Config 模块（YAML + 环境变量）
6. **C-6** 节点注册中心（内存 + 心跳超时）
7. **C-7** Server 控制面（gRPC + 注册 + 心跳）
8. **C-8** Server 路由 + 操作桥接 + 管理接口
9. **C-9** Agent 控制面（连接 + 注册 + 心跳 + 重连 + OS 采集）
10. **C-10** Agent exec 处理器
11. **C-11** Agent read/write 处理器
12. **C-12** Agent forward 处理器
13. **C-13** CLI 框架 + config 命令
14. **C-14** CLI node + auth 命令
15. **C-15** CLI 核心操作（exec/read/write/forward）
16. **C-16** Agent 二进制（daemon 化 + start/stop/status）

## QA 任务（4 个，与 Coder 并行）

1. **Q-1** CI pipeline（lint + test + build）
2. **Q-2** Proto 验证（生成代码一致性检查）
3. **Q-3** 集成测试框架
4. **Q-4** Release pipeline（跨平台编译 + GitHub Release）
