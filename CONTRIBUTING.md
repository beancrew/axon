# Contributing to Axon

## Workspace Rules

- **Each agent has its own workspace and repo clone** — no cross-directory operations
- Draft in your workspace, apply to project repo as a single clean commit

| Role | Workspace | Repo Clone |
|------|-----------|------------|
| Planner | `~/.openclaw/workspace-axon-planner/` | `/Users/mac/claw-project/axon-planner/axon/` |
| Coder | `~/.openclaw/workspace-axon-coder/` | `/Users/mac/claw-project/axon-coder/axon/` |
| QA | `~/.openclaw/workspace-axon-qa/` | `/Users/mac/claw-project/axon-qa/axon/` |

## Branch Rules

- **`main` is the base branch**
- Branch prefixes: `planner/`, `coder/`, `qa/`
- One PR per task
- Delete branch after merge
- No cross-branch merges

```bash
git fetch origin
git checkout -b {prefix}/your-task origin/main
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

## Language Rules (Mandatory)

| What | Language |
|------|----------|
| Commit messages | English |
| PR title/body | English |
| Documentation | Bilingual (EN + ZH) |
| Code comments | English |

**Violations = PR rejected.**

## PR Checklist

- [ ] Branch from latest `origin/main`
- [ ] `go build ./...` passes
- [ ] `go test ./...` passes
- [ ] PR body in English
- [ ] CI fully green before reporting done

## Code Style

- Follow standard Go conventions (`gofmt`, `go vet`)
- Error messages: lowercase, no trailing punctuation
- Package names: short, lowercase, no underscores

---

# 贡献指南

## 工作区规则
- 每个 agent 有独立的工作区和 repo clone，禁止交叉操作
- 在 workspace 起草，应用到项目 repo 时单个 clean commit

## 分支规则
- `main` 是唯一 base branch
- 分支前缀：`planner/`、`coder/`、`qa/`
- 一个 PR 一个任务，merge 后立即删分支

## 语言规则（必须遵守）
- Commit、PR title/body：英文
- 文档：中英双语
- 代码注释：英文
- 违反 = PR 打回
