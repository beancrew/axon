# Contributing to Axon

Thanks for your interest in contributing to Axon! This guide covers how to set up your environment, make changes, and submit a pull request.

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


