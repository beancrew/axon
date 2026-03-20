# Axon Phase 1 任务拆分

## 概述

Phase 1 目标：**端到端可用的系统** —— CLI 可以通过 Server 在远程节点上执行 exec/read/write/forward，带认证和审计。

## 角色分工

| 角色 | 负责人 | 范围 |
|------|--------|------|
| ⚙️ 小 c (Coder) | 编码 | 所有业务代码 |
| 🔍 QA | 测试 + CI/CD | 测试用例、GitHub Actions、构建流水线 |
| 🧠 Planner | 设计 | 设计文档、任务拆分、review |
| 👤 老大 | 决策 | PR review、最终审批 |

---

## ⚙️ 小 c 任务

任务按依赖关系排序。每个任务 = 一个分支 + 一个 PR。

### 阶段 1：项目脚手架 + Proto

**任务 C-1：项目初始化**
- 分支：`coder/init-project`
- 范围：
  - `go mod init github.com/garysng/axon`
  - 目录结构：`cmd/`、`proto/`、`pkg/`、`internal/`、`docs/`、`scripts/`
  - `.gitignore`（二进制、gen/、vendor/、.env）
  - 三个二进制的空 `main.go`（必须能编译通过）
  - `Makefile`（build/test/lint/proto 目标）
- 完成标准：`make build` 编译出三个空二进制

**任务 C-2：Proto 定义 + 代码生成**
- 分支：`coder/proto`
- 依赖：C-1
- 范围：
  - `proto/control.proto` — ControlService、AgentMessage、ServerMessage、NodeInfo、OSInfo
  - `proto/operations.proto` — OperationsService、Exec/Read/Write/Forward 消息
  - `proto/management.proto` — ManagementService、NodeSummary、Login 消息
  - `scripts/proto-gen.sh` — protoc + Go 插件代码生成
  - 生成代码放在 `gen/proto/`
- 完成标准：`make proto && make build` 通过

### 阶段 2：共享库

**任务 C-3：Auth 模块 (pkg/auth)**
- 分支：`coder/auth`
- 依赖：C-1
- 范围：
  - JWT token 生成（HMAC-SHA256）
  - JWT token 验证（签名、过期）
  - CLI token：user_id + node_ids
  - Agent token：node_id
  - gRPC 拦截器（token 提取/验证）
- 完成标准：单元测试通过（签发/验证/过期/无效 token）

**任务 C-4：Audit 模块 (pkg/audit)**
- 分支：`coder/audit`
- 依赖：C-1
- 范围：
  - AuditEntry 结构体
  - SQLite 存储（建表、插入、查询）
  - 异步写入器（带缓冲的 channel + 后台 goroutine）
  - 查询接口（按时间/节点/用户）
- 完成标准：单元测试通过（写入/读取/异步刷新）

**任务 C-5：Config 模块 (pkg/config)**
- 分支：`coder/config`
- 依赖：C-1
- 范围：
  - YAML 配置加载（server、agent、CLI）
  - 环境变量覆盖支持
  - 配置文件路径：`~/.axon/config.yaml`、`~/.axon-agent/config.yaml`、`/etc/axon-server/config.yaml`
- 完成标准：单元测试通过（加载/覆盖/默认值）

### 阶段 3：Server 核心

**任务 C-6：节点注册中心 (internal/server/registry)**
- 分支：`coder/registry`
- 依赖：C-2
- 范围：
  - 内存节点注册表（NodeEntry 结构体）
  - 注册、更新心跳、标记离线、移除、列表、查找
  - 心跳超时监控（后台 goroutine）
  - 线程安全（sync.RWMutex）
- 完成标准：单元测试覆盖所有注册操作 + 超时检测

**任务 C-7：Server gRPC + 控制面**
- 分支：`coder/server-control`
- 依赖：C-2、C-3、C-6
- 范围：
  - gRPC server 启动（TLS）
  - ControlService 实现：Connect() 处理器
  - Agent 注册流程（验证 token → 注册 → 返回 node_id）
  - 心跳处理（接收 → 确认 → 更新注册中心）
  - TaskSignal 派发（供 router 使用）
- 完成标准：集成测试 —— mock agent 连接、注册、心跳，server 追踪状态

**任务 C-8：Server 路由 + 操作桥接**
- 分支：`coder/server-router`
- 依赖：C-7、C-4
- 范围：
  - 路由层：认证 → 授权（检查 JWT 中的 node_ids）→ 查找节点 → 派发
  - OperationsService 实现：Exec、Read、Write、Forward
  - Stream 桥接：CLI stream ↔ Agent stream
  - 每个操作记审计日志
  - ManagementService：ListNodes、GetNode、RemoveNode、Login
- 完成标准：集成测试 —— CLI mock → Server → Agent mock 完整请求流程

### 阶段 4：Agent

**任务 C-9：Agent 控制面**
- 分支：`coder/agent-control`
- 依赖：C-2、C-5
- 范围：
  - Agent 主循环：连接 → 注册 → 心跳循环
  - 配置加载（首次保存，后续读取）
  - 启动即注册流程
  - 心跳发送（间隔来自 server）
  - NodeInfo + OSInfo 采集（Linux: /etc/os-release + uname，macOS: sw_vers + uname）
  - 指数退避重连（1s → 60s，±20% jitter）
  - TaskSignal 接收器（分发到操作面处理器）
- 完成标准：agent 连接测试 server，注册、心跳、断线重连

**任务 C-10：Agent exec 处理器**
- 分支：`coder/agent-exec`
- 依赖：C-9
- 范围：
  - 接收 ExecRequest → 启动本地进程
  - 流式回传 stdout/stderr（ExecOutput）
  - Exit code 转发
  - 超时处理（SIGTERM → 5s → SIGKILL）
  - 取消处理（gRPC context cancel → SIGTERM）
  - 环境变量 + 工作目录支持
- 完成标准：端到端测试 —— CLI exec → Server → Agent → 真实命令执行

**任务 C-11：Agent read/write 处理器**
- 分支：`coder/agent-fileio`
- 依赖：C-9
- 范围：
  - Read：stat 文件 → 发送 meta → 分块流式传输（32KB）
  - Write：接收 header → 创建目录 → 原子写入（临时文件 + rename）
  - 错误处理：文件不存在、权限不足、是目录
- 完成标准：端到端测试 —— 通过完整链路读写文件

**任务 C-12：Agent forward（端口隧道）**
- 分支：`coder/agent-forward`
- 依赖：C-9
- 范围：
  - 接收 TunnelOpen → 连接 localhost:port
  - 双向中继：gRPC stream ↔ TCP 连接
  - 连接关闭处理
  - 错误：目标端口不可达
- 完成标准：端到端测试 —— 转发端口，TCP 数据双向流通

### 阶段 5：CLI

**任务 C-13：CLI 框架 + config 命令**
- 分支：`coder/cli-framework`
- 依赖：C-2、C-5
- 范围：
  - cobra 命令结构（root、node、exec、read、write、forward、auth、config、version）
  - Config 命令：`config set/get`、`auth token`、`version`
  - 输出格式化：表格 + JSON（`--json` flag）
  - gRPC 客户端连接辅助（从配置读取 server + token，TLS）
- 完成标准：`axon version`、`axon config set/get` 可用

**任务 C-14：CLI node + auth 命令**
- 分支：`coder/cli-node-auth`
- 依赖：C-13、C-8
- 范围：
  - `axon node list`（含 `--status` 过滤）
  - `axon node info <node>`
  - `axon node remove <node>`
  - `axon auth login`（提示用户名/密码，保存 token）
- 完成标准：CLI 命令对运行中的 server 可用

**任务 C-15：CLI 核心操作**
- 分支：`coder/cli-operations`
- 依赖：C-13、C-8
- 范围：
  - `axon exec <node> <cmd>` — 流式 stdout/stderr，转发 exit code，Ctrl+C 取消
  - `axon read <node> <path>` — 流式输出到 stdout，`--meta` flag
  - `axon write <node> <path>` — 从 stdin 流式上传，`--mode` flag
  - `axon forward <node> <L>:<R>` — 本地监听，每连接一个 stream，`--bind` flag
- 完成标准：四个操作端到端可用（CLI → Server → Agent → 真实目标）

### 阶段 6：Agent 二进制 + 守护进程

**任务 C-16：Agent 二进制 (cmd/axon-agent)**
- 分支：`coder/agent-binary`
- 依赖：C-9、C-10、C-11、C-12
- 范围：
  - `axon-agent start`（--server, --token, --name, --labels, --foreground）
  - `axon-agent stop`（SIGTERM → 优雅关闭）
  - `axon-agent status`（进程检查 + 连接健康）
  - `axon-agent config set/get`
  - `axon-agent version`
  - 守护进程化（或前台模式）
- 完成标准：完整 agent 二进制可作为 daemon 运行，可 start/stop/status

---

## 🔍 QA 任务

### CI/CD

**任务 Q-1：GitHub Actions — CI 流水线**
- 分支：`qa/ci-pipeline`
- 范围：
  - `.github/workflows/ci.yml`
  - 触发：push 到任意分支，PR 到 main
  - 步骤：
    1. Go 环境（版本矩阵：1.22+）
    2. `make proto`（安装 protoc + 插件）
    3. `make lint`（golangci-lint）
    4. `make test`（go test ./... -race -cover）
    5. `make build`（编译三个二进制）
  - 覆盖率报告上传
  - PR 合并需要状态检查通过
- 完成标准：CI 在 PR 上跑绿

**任务 Q-2：GitHub Actions — Proto 验证**
- 分支：`qa/proto-check`
- 依赖：Q-1、C-2
- 范围：
  - CI 步骤：生成 proto → 检查无 diff（确保生成代码已提交且最新）
  - `buf lint` proto 风格验证（推荐）
- 完成标准：proto 变更触发 CI 验证

**任务 Q-3：集成测试框架**
- 分支：`qa/integration-tests`
- 依赖：C-8
- 范围：
  - 测试工具：进程内启动 server + agent
  - 常用断言辅助函数
  - 端到端测试套件结构
  - 测试覆盖：连接、注册、心跳、exec、read、write、forward
- 完成标准：集成测试套件在 CI 中运行

**任务 Q-4：GitHub Actions — Release 流水线**
- 分支：`qa/release-pipeline`
- 依赖：Q-1
- 范围：
  - `.github/workflows/release.yml`
  - 触发：tag push（`v*`）
  - 步骤：
    1. 跨平台编译：linux/darwin × amd64/arm64 × 3 个二进制
    2. 创建 GitHub Release
    3. 上传二进制到 release assets
    4. 生成 checksum
- 完成标准：push tag 自动创建 release 并附带所有二进制

---

## 任务依赖图

```
C-1 (脚手架)
 ├── C-2 (proto) ─────┐
 ├── C-3 (auth) ──────┤
 ├── C-4 (audit) ─────┤
 └── C-5 (config) ────┤
                       │
 C-6 (注册中心) ◄──── C-2
                       │
 C-7 (server控制面) ◄─ C-2 + C-3 + C-6
                       │
 C-8 (server路由) ◄── C-7 + C-4
                       │
 C-9 (agent控制面) ◄── C-2 + C-5
 ├── C-10 (exec) ◄─── C-9
 ├── C-11 (fileio) ◄── C-9
 └── C-12 (forward) ◄─ C-9
                       │
 C-13 (cli框架) ◄──── C-2 + C-5
 ├── C-14 (cli-node) ◄ C-13 + C-8
 └── C-15 (cli-ops) ◄─ C-13 + C-8
                       │
 C-16 (agent二进制) ◄─ C-9 + C-10 + C-11 + C-12

 Q-1 (CI) ── 独立
 Q-2 (proto校验) ◄──── Q-1 + C-2
 Q-3 (集成测试) ◄───── C-8
 Q-4 (release) ◄────── Q-1
```

## 建议执行顺序

**小 c (Coder)：**

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

**QA（与 Coder 并行）：**

| 顺序 | 任务 | 依赖 | 预估 |
|:----:|------|------|------|
| 1 | Q-1 CI 流水线 | C-1 完成后 | 1d |
| 2 | Q-2 Proto 验证 | Q-1 + C-2 | 0.5d |
| 3 | Q-3 集成测试 | C-8 完成后 | 2d |
| 4 | Q-4 Release 流水线 | Q-1 | 1d |
