# Axon Server Design

> [中文版 / Chinese](zh/server.md)

## Overview

`axon-server` is the central control plane. Single binary, self-hosted. Manages node registration, authentication, request routing, persistence, TLS, and audit logging.

**All traffic flows through the server. CLI and Agent never communicate directly.**

## Architecture

```
┌──────────────── axon-server ────────────────┐
│                                              │
│  ┌──────────┐  ┌──────────┐  ┌───────────┐  │
│  │ gRPC API │  │ gRPC API │  │ gRPC API  │  │
│  │ Mgmt     │  │ Ops      │  │ Control   │  │
│  └────┬─────┘  └────┬─────┘  └─────┬─────┘  │
│       │              │              │         │
│  ┌────▼──────────────▼──────────────▼─────┐  │
│  │              Router                     │  │
│  │  CLI request → find node → dispatch     │  │
│  └────┬───────────────────────────┬───────┘  │
│       │                           │          │
│  ┌────▼─────┐             ┌──────▼───────┐  │
│  │ Registry │             │ Auth         │  │
│  │ (SQLite) │             │ JWT + Users  │  │
│  │          │             │ + Tokens     │  │
│  │ node_id  │             │              │  │
│  │ status   │             │ sign/verify  │  │
│  │ streams  │             │ revocation   │  │
│  └────┬─────┘             └──────────────┘  │
│       │                                      │
│  ┌────▼─────┐                                │
│  │ Audit    │                                │
│  │ Logger   │                                │
│  └──────────┘                                │
└──────────────────────────────────────────────┘
```

## Submodules

### 1. gRPC API Layer

Single gRPC server on one port. All three services registered on the same server.

| Service | Source | Description |
|---------|--------|-------------|
| `ControlService` | Agent | Registration, heartbeat, task dispatch |
| `OperationsService` | CLI | exec, read, write, forward |
| `ManagementService` | CLI | Node/token management |

### 2. Node Registry (SQLite-backed)

Persistent registry of all known nodes.

```sql
CREATE TABLE nodes (
    node_id      TEXT PRIMARY KEY,
    node_name    TEXT NOT NULL,
    status       TEXT NOT NULL DEFAULT 'offline',
    token_hash   TEXT NOT NULL,
    info_json    TEXT,
    labels_json  TEXT,
    connected_at INTEGER,
    last_heartbeat INTEGER,
    registered_at INTEGER NOT NULL
);
CREATE UNIQUE INDEX idx_nodes_name ON nodes(node_name);
```

**Operations:**

| Operation | Trigger | Description |
|-----------|---------|-------------|
| Register | Agent connects | Upsert node entry, set status = online |
| Heartbeat update | Agent heartbeat | Batched updates (30s flush interval) |
| Mark offline | Heartbeat timeout | Set status = offline, clear stream ref |
| Remove | `axon node remove` | Delete entry, disconnect agent |
| List | `axon node list` | Return all entries |

**Stable Node Identity:** Nodes get a UUID `node_id` on first registration, persisted in the agent's local config. On reconnect, server recognizes returning nodes by `node_id`.

**Heartbeat Batching:** Heartbeat timestamps are batched in memory and flushed to SQLite every 30 seconds (plus graceful-shutdown flush) to reduce write pressure.

**Startup Behavior:** On server start, all persisted nodes are loaded and marked `offline`. They return to `online` when agents reconnect.

### 3. Router

Routes CLI requests to the correct agent.

```
CLI request (exec web-1 "ls")
    │
    ▼
Router:
    1. Authenticate: verify JWT token (interceptor)
    2. Authorize: check token.node_ids contains target
    3. Lookup: find node in Registry
    4. Check status: if offline → UNAVAILABLE
    5. Dispatch: send TaskSignal via control stream
    6. Bridge: proxy data between CLI stream and Agent data stream
```

### 4. Auth Module

JWT-based authentication with token lifecycle management.

**Token types:**

| Type | Contains | Lifetime |
|------|----------|----------|
| CLI Token | `sub` (username), `node_ids`, `jti`, `exp`, `iat` | Default 24h |
| Agent Token | `node_id` | Registration |

**gRPC Interceptor:**

All RPCs (except `Login` and `Connect`) pass through interceptors that:
1. Extract bearer token from gRPC metadata
2. Verify HMAC-SHA256 signature
3. Check expiry
4. Check JTI against revoked set (O(1) in-memory lookup)
5. Inject claims into context

**Token Store (SQLite):**

```sql
CREATE TABLE tokens (
    id         TEXT PRIMARY KEY,    -- JTI
    kind       TEXT NOT NULL,       -- "cli" or "agent"
    user_id    TEXT,
    node_ids   TEXT,                -- JSON array
    issued_at  INTEGER NOT NULL,
    expires_at INTEGER NOT NULL,
    revoked    INTEGER NOT NULL DEFAULT 0
);
```

On Login, the issued token is persisted. On startup, revoked JTIs are loaded into an in-memory set.

### 5. User Store (SQLite)

Persistent user management replacing the config-file-only approach.

```sql
CREATE TABLE users (
    username      TEXT PRIMARY KEY,
    password_hash TEXT NOT NULL,     -- bcrypt
    node_ids      TEXT NOT NULL,     -- JSON array
    created_at    INTEGER NOT NULL,
    updated_at    INTEGER NOT NULL,
    disabled      INTEGER NOT NULL DEFAULT 0
);
```

**Bootstrap:** Users defined in the config YAML are seeded on startup via `INSERT OR IGNORE` — existing DB users are not overwritten. After bootstrap, users are managed via gRPC RPCs (`CreateUser`, `UpdateUser`, `DeleteUser`, `ListUsers`).

**Login flow:** Server looks up user in SQLite, checks `disabled` flag, verifies bcrypt password, signs JWT with JTI, persists token to store.

### 6. TLS

**Auto-TLS (default):** When no explicit `tls.cert`/`tls.key` is configured:

1. Check `tls.dir` (default `~/.axon-server/tls/`) for `ca.crt`
2. If missing: generate ECDSA P-256 CA (10-year) + server cert (1-year)
3. If `ca.crt` exists but `server.crt` missing or expires within 30 days: regenerate server cert
4. Log CA path + SHA-256 fingerprint
5. SANs: always `localhost` + `127.0.0.1` + listen address hostname

**Explicit TLS:** Set `tls.cert` and `tls.key` to use your own certificates.

**Disable TLS:** Set `tls.auto: false` with no cert/key. Server logs a warning.

### 7. Audit Logger

Logs every operation through the server.

```go
type AuditEntry struct {
    Timestamp  time.Time
    UserID     string
    NodeID     string
    Action     string    // "exec", "read", "write", "forward", "node.remove"
    Detail     string    // command, path, or port
    Result     string    // "success", "error", "timeout"
    DurationMs int64
    Error      string
}
```

- Storage: SQLite (separate file from data DB)
- Async write: buffered channel → background writer
- Non-blocking: audit failure does not block operations

### 8. Shared Database

All persistent state (except audit) lives in a **single SQLite database** with WAL mode:

```go
db, err := sql.Open("sqlite", dataDBPath)
db.Exec("PRAGMA journal_mode=WAL")

registryStore := registry.NewSQLiteStoreFromDB(db)
tokenStore    := auth.NewTokenStoreFromDB(db)
userStore     := auth.NewUserStoreFromDB(db)
```

Each store creates its own tables via `CREATE TABLE IF NOT EXISTS`. The shared connection is closed last in `GracefulStop` after all stores are shut down.

## Server Config

See [Configuration Reference](configuration.md) for the full YAML spec.

## Startup Sequence

```
1. Load config (file + env vars)
2. Open shared SQLite database (WAL mode)
3. Initialize registry store → load all nodes (mark offline)
4. Initialize token store → load revoked JTIs into memory
5. Initialize token checker
6. Initialize user store → bootstrap config users (INSERT OR IGNORE)
7. Auto-TLS: generate or load certificates
8. Build gRPC server with auth interceptors
9. Initialize audit store + writer
10. Register all gRPC services
11. Start heartbeat monitor (background goroutine)
12. Serve
```

## Graceful Shutdown

```
1. Stop accepting new connections
2. GracefulStop gRPC (wait for in-flight RPCs)
3. Close audit writer (flush buffer)
4. Close user store
5. Close token store
6. Close registry (flush heartbeat batch)
7. Close shared database
8. Exit
```

## Command

### `axon-server start`

```
$ axon-server start --config /etc/axon-server/config.yaml
[INFO] server: auto-TLS: generated CA cert ~/.axon-server/tls/ca.crt (SHA-256: AA:BB:...)
[INFO] server: gRPC listening on :9090 (TLS)
```

- **Flags**: `--config <path>` — config file path (default: `./config.yaml`)

### `axon-server version`

```
$ axon-server version
axon-server 0.1.0 (go1.25, darwin/arm64)
```
