# Contributing to Axon

Thanks for your interest in contributing to Axon! This guide covers how to set up your environment, make changes, and submit a pull request.

> [中文版 / Chinese](#贡献指南)

## Getting Started

### Prerequisites

- Go 1.25+
- `protoc` (Protocol Buffers compiler) — for proto changes
- `make`

### Build

```bash
git clone https://github.com/beancrew/axon.git
cd axon
make build
```

Binaries are output to `bin/` (`axon`, `axon-server`, `axon-agent`).

### Test

```bash
make test     # unit tests
make lint     # golangci-lint
```

## Development Workflow

1. Fork the repo and clone your fork
2. Create a branch from `main`:
   ```bash
   git checkout -b your-branch origin/main
   ```
3. Make changes
4. Ensure `go build ./...`, `go test ./...`, and `make lint` pass
5. Commit with a descriptive message (see [Commit Convention](#commit-convention))
6. Push and open a PR against `main`

## Branch Naming

Use a descriptive prefix:

```
feat/forward-daemon
fix/tls-handshake
docs/update-quickstart
test/add-exec-tests
refactor/simplify-auth
```

## Commit Convention

Format: `type(scope): short description`

Types: `feat`, `fix`, `docs`, `test`, `refactor`, `chore`, `ci`

```
feat(agent): implement reverse connection to server
fix(cli): handle node offline gracefully
docs: add protocol design document
test(server): add node registration tests
```

- Use English for commit messages, PR titles, and PR bodies
- Use imperative mood ("add", "fix", not "added", "fixed")

## Pull Request Guidelines

- One PR per logical change
- PR title follows the same `type(scope): description` format
- Include a summary of what changed and why
- Reference related issues (e.g. `Fixes #42`)
- All CI checks must pass before merge
- PRs are squash-merged; branch is deleted after merge

## Code Style

- Follow standard Go conventions (`gofmt`, `go vet`, `golangci-lint`)
- Error messages: lowercase, no trailing punctuation
- Package names: short, lowercase, no underscores
- Comments: English

## Project Structure

```
axon/
├── cmd/
│   ├── axon/           # CLI
│   ├── axon-server/    # Server
│   └── axon-agent/     # Agent
├── internal/           # Shared internal packages
├── gen/proto/          # Generated protobuf code (tracked)
├── proto/              # Proto source files
├── docs/               # Documentation
├── scripts/            # Build and install scripts
└── skills/             # AgentSkills
```

## Documentation

- Documentation lives in `docs/`
- Bilingual (English + Chinese) where applicable
- Update docs when changing user-facing behavior

## Reporting Issues

- Use [GitHub Issues](https://github.com/beancrew/axon/issues)
- Include: what you did, what you expected, what happened, and environment info
- For security vulnerabilities, see [SECURITY.md](SECURITY.md)

---

# 贡献指南

感谢你对 Axon 的贡献！以下是开发设置、提交变更和发起 PR 的指南。

## 快速开始

### 前置条件

- Go 1.25+
- `protoc`（Protocol Buffers 编译器）— 修改 proto 时需要
- `make`

### 构建

```bash
git clone https://github.com/beancrew/axon.git
cd axon
make build
```

### 测试

```bash
make test     # 单元测试
make lint     # golangci-lint
```

## 开发流程

1. Fork 仓库并 clone 你的 fork
2. 从 `main` 创建分支
3. 修改代码
4. 确保 `go build ./...`、`go test ./...`、`make lint` 通过
5. 提交（参见上方 Commit Convention）
6. Push 并向 `main` 发起 PR

## 规范

- Commit message、PR title/body：**英文**
- 文档：中英双语
- 代码注释：英文
- 一个 PR 一个逻辑变更
- CI 全绿才能合并
- Squash merge，合并后删除分支

## 项目结构

```
axon/
├── cmd/                # 三个二进制入口
├── internal/           # 内部共享包
├── gen/proto/          # 生成的 protobuf 代码
├── proto/              # Proto 源文件
├── docs/               # 文档
├── scripts/            # 构建和安装脚本
└── skills/             # AgentSkills
```

## 报告问题

- 使用 [GitHub Issues](https://github.com/beancrew/axon/issues)
- 安全漏洞请参见 [SECURITY.md](SECURITY.md)
