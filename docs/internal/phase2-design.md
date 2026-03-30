# Phase 2 Design — Persistence, Security & Production Readiness

## Overview

Phase 1 delivers a working end-to-end system. Phase 2 makes it **production-ready**:

1. **Node Registry Persistence** — survive server restarts
2. **Stable Node Identity** — node_id preserved across reconnections
3. **Token Management** — revocation, rotation, server-side tracking
4. **User Store Persistence** — manage users without config restarts
5. **TLS by Default** — secure all links
6. **Command & Path Restrictions** — agent-side security policies

---

## Task P2-1: Node Registry Persistence (SQLite)

**Priority: Critical**

### Problem

Server restart → all nodes lost → agents reconnect with new UUIDs → CLI tokens with old node_ids stop working.

### Design

Add a SQLite-backed persistence layer behind the existing in-memory Registry. The in-memory map remains the hot path; SQLite is the durable store.

#### Schema

```sql
CREATE TABLE nodes (
    node_id       TEXT PRIMARY KEY,
    node_name     TEXT NOT NULL UNIQUE,
    token_hash    TEXT NOT NULL,      -- SHA-256 of the agent token used at first registration
    arch          TEXT,
    ip            TEXT,
    agent_version TEXT,
    os_info       TEXT,               -- JSON blob of OSInfo
    labels        TEXT,               -- JSON blob of labels map
    status        TEXT NOT NULL DEFAULT 'offline',
    connected_at  INTEGER,
    last_heartbeat INTEGER,
    registered_at INTEGER NOT NULL,
    updated_at    INTEGER NOT NULL
);

CREATE INDEX idx_nodes_name ON nodes(node_name);
CREATE INDEX idx_nodes_status ON nodes(status);
```

#### Behavior

| Event | Memory | SQLite |
|-------|--------|--------|
| Agent first registration | Insert new entry | INSERT node row |
| Agent reconnection | Update entry (same node_id) | UPDATE status, connected_at |
| Heartbeat | Update LastHeartbeat | UPDATE last_heartbeat (batched, every 30s) |
| Mark offline | Set status=offline | UPDATE status |
| Node remove | Delete from map | DELETE from table |
| Server startup | Load all nodes from SQLite | — |

#### Stable Node Identity

Current: `node_id = uuid.NewString()` on every registration.

New: Server assigns node_id only on **first** registration. On reconnection, the Agent sends its persisted `node_id` + original registration token. Server validates the token hash matches, then accepts the returning node.

**Registration flow change:**

```
// control.proto — update RegisterRequest
message RegisterRequest {
    string token = 1;
    string node_name = 2;
    NodeInfo info = 3;
    string node_id = 4;    // NEW: non-empty on reconnection
}
```

```
Agent first start:
  1. Send RegisterRequest{token, node_name, info, node_id=""}
  2. Server: validate token → generate node_id → INSERT to SQLite
  3. Server: return RegisterResponse{node_id}
  4. Agent: persist node_id to config

Agent reconnect:
  1. Send RegisterRequest{token, node_name, info, node_id="a1b2c3d4"}
  2. Server: lookup node_id in SQLite → verify token hash matches
  3. Server: UPDATE status=online → return RegisterResponse{node_id}
  4. CLI tokens referencing this node_id remain valid
```

#### Implementation

New file: `internal/server/registry/store.go`

```go
type Store interface {
    LoadAll() ([]NodeEntry, error)
    Save(entry *NodeEntry) error
    Delete(nodeID string) error
    UpdateHeartbeat(nodeID string, t time.Time) error
    UpdateStatus(nodeID string, status string) error
}

type SQLiteStore struct {
    db *sql.DB
}
```

Modify `Registry`:
```go
type Registry struct {
    mu               sync.RWMutex
    nodes            map[string]*NodeEntry
    heartbeatTimeout time.Duration
    store            Store  // NEW: persistence backend (nil = in-memory only)
}

func NewRegistry(heartbeatTimeout time.Duration, store Store) *Registry {
    reg := &Registry{
        nodes:            make(map[string]*NodeEntry),
        heartbeatTimeout: heartbeatTimeout,
        store:            store,
    }
    if store != nil {
        entries, _ := store.LoadAll()
        for _, e := range entries {
            e.Status = StatusOffline // all nodes offline until they reconnect
            cp := e
            reg.nodes[e.NodeID] = &cp
        }
    }
    return reg
}
```

#### Heartbeat Batching

Don't write every heartbeat to SQLite (too many writes). Instead:
- In-memory: update immediately (existing behavior)
- SQLite: batch flush every 30 seconds via a background goroutine
- On graceful shutdown: flush all pending heartbeats

---

## Task P2-2: Token Management

**Priority: High**

### Problem

- No token revocation — leaked tokens valid until expiry
- No tracking of issued tokens
- Can't list/audit active tokens

### Design

#### Token Store (SQLite)

```sql
CREATE TABLE tokens (
    id          TEXT PRIMARY KEY,    -- JWT ID (jti claim)
    kind        TEXT NOT NULL,       -- 'cli' or 'agent'
    user_id     TEXT,                -- for CLI tokens
    node_ids    TEXT,                -- JSON array, for CLI tokens
    issued_at   INTEGER NOT NULL,
    expires_at  INTEGER NOT NULL,
    revoked_at  INTEGER,            -- NULL if not revoked
    revoked_by  TEXT                -- who revoked it
);

CREATE INDEX idx_tokens_user ON tokens(user_id);
CREATE INDEX idx_tokens_kind ON tokens(kind);
```

#### Token Lifecycle

```
Issue:
  1. Login → sign JWT with unique jti → INSERT to tokens table
  2. Return token to CLI

Validate:
  1. Verify JWT signature + expiry (existing)
  2. Check tokens table: is jti revoked? (NEW)
  3. If revoked → reject

Revoke:
  1. axon auth revoke <token-id>
  2. UPDATE tokens SET revoked_at=now, revoked_by=user

List:
  1. axon auth list-tokens
  2. SELECT from tokens WHERE revoked_at IS NULL
```

#### JWT Changes

Add `jti` (JWT ID) claim:
```go
// pkg/auth/auth.go
claims := Claims{
    UserID:  userID,
    NodeIDs: nodeIDs,
    Kind:    KindCLI,
    RegisteredClaims: jwt.RegisteredClaims{
        ID:        uuid.NewString(),  // NEW: jti
        IssuedAt:  jwt.NewNumericDate(now),
        ExpiresAt: jwt.NewNumericDate(now.Add(expiry)),
    },
}
```

#### Revocation Check in Interceptor

Don't hit SQLite on every request. Use an in-memory bloom filter or set:

```go
type TokenChecker struct {
    mu      sync.RWMutex
    revoked map[string]struct{} // jti → revoked
}
```

- On startup: load all revoked jti from SQLite
- On revoke: add to in-memory set + UPDATE SQLite
- On validate: check in-memory set first (O(1))

#### New CLI Commands

```
axon auth revoke <token-id>     # Revoke a specific token
axon auth list-tokens           # List active (non-revoked, non-expired) tokens
axon auth rotate                # Revoke current token + issue new one
```

#### New Management RPC

```protobuf
// management.proto additions
rpc RevokeToken(RevokeTokenRequest) returns (RevokeTokenResponse);
rpc ListTokens(ListTokensRequest) returns (ListTokensResponse);

message RevokeTokenRequest { string token_id = 1; }
message RevokeTokenResponse { bool success = 1; string error = 2; }

message ListTokensRequest { string kind = 1; }  // "cli", "agent", or "" for all
message ListTokensResponse {
    repeated TokenInfo tokens = 1;
}
message TokenInfo {
    string id = 1;
    string kind = 2;
    string user_id = 3;
    int64 issued_at = 4;
    int64 expires_at = 5;
}
```

---

## Task P2-3: User Store Persistence

**Priority: Medium**

### Problem

Users are hardcoded in server config YAML. Adding/removing users requires config edit + server restart.

### Design

Move user store to SQLite. Keep config-based users as a bootstrap/fallback.

```sql
CREATE TABLE users (
    username      TEXT PRIMARY KEY,
    password_hash TEXT NOT NULL,
    node_ids      TEXT NOT NULL,      -- JSON array
    created_at    INTEGER NOT NULL,
    updated_at    INTEGER NOT NULL,
    disabled      INTEGER NOT NULL DEFAULT 0
);
```

#### Bootstrap Flow

```
Server startup:
  1. Open users table
  2. For each user in config YAML:
     - If not in DB → INSERT (bootstrap)
     - If already in DB → skip (DB is authoritative)
  3. ManagementService reads from DB instead of config slice
```

#### New Management RPCs

```protobuf
rpc CreateUser(CreateUserRequest) returns (CreateUserResponse);
rpc UpdateUser(UpdateUserRequest) returns (UpdateUserResponse);
rpc DeleteUser(DeleteUserRequest) returns (DeleteUserResponse);
rpc ListUsers(ListUsersRequest) returns (ListUsersResponse);
```

#### New CLI Commands

```
axon user create <username> --node-ids '*'    # prompts for password
axon user list
axon user update <username> --node-ids 'web-1,web-2'
axon user delete <username>
```

---

## Task P2-4: TLS by Default

**Priority: High**

### Problem

All connections currently use `insecure.NewCredentials()`. Anyone on the network can intercept tokens and data.

### Design

#### Auto-TLS (Self-Signed)

On first start, if no TLS cert/key configured, server auto-generates a self-signed CA + server cert:

```
~/.axon-server/tls/
├── ca.crt        # CA certificate
├── ca.key        # CA private key
├── server.crt    # Server certificate (signed by CA)
└── server.key    # Server private key
```

Agent and CLI trust the CA cert. Distribution:

```
# Server generates and shows fingerprint:
$ axon-server start
[INFO] auto-generated TLS certificates
[INFO] CA fingerprint: SHA256:abc123...
[INFO] distribute ca.crt to agents and CLI clients

# Agent trusts the CA:
$ axon-agent start --server axon.example.com:443 --ca-cert /path/to/ca.crt

# CLI trusts the CA:
$ axon config set ca_cert /path/to/ca.crt
```

#### Config Changes

```yaml
# Server config
tls:
  cert: ""          # Empty = auto-generate
  key: ""
  auto: true        # Generate self-signed if cert/key empty
  
# Agent config
server: "axon.example.com:443"
ca_cert: "/path/to/ca.crt"     # NEW
tls_insecure: false             # Existing, default changes to false

# CLI config
server: "axon.example.com:443"
ca_cert: "/path/to/ca.crt"     # NEW
```

#### CLI & Agent Connection Changes

```go
// Replace insecure.NewCredentials() with:
if cfg.CACert != "" {
    creds, err := credentials.NewClientTLSFromFile(cfg.CACert, "")
    opts = append(opts, grpc.WithTransportCredentials(creds))
} else if cfg.TLSInsecure {
    opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
} else {
    // Use system CA pool (for proper certs)
    opts = append(opts, grpc.WithTransportCredentials(credentials.NewClientTLSFromCert(nil, "")))
}
```

---

## Task P2-5: Agent Security Policies

**Priority: Medium**

### Problem

Agent executes any command, reads/writes any file the agent user can access. No restrictions.

### Design

Agent-side policy file: `~/.axon-agent/policy.yaml`

```yaml
exec:
  allow:
    - "ls *"
    - "cat /var/log/*"
    - "systemctl status *"
    - "docker ps"
  deny:
    - "rm -rf *"
    - "shutdown *"
    - "reboot *"
  mode: allowlist    # allowlist | denylist | unrestricted

read:
  allowed_paths:
    - "/var/log/"
    - "/etc/"
    - "/home/deploy/"
  denied_paths:
    - "/etc/shadow"
    - "/root/"

write:
  allowed_paths:
    - "/home/deploy/"
    - "/tmp/"
  denied_paths:
    - "/etc/"
    - "/root/"
  max_file_size: "100MB"

forward:
  allowed_ports: [80, 443, 5432, 6379]
  denied_ports: [22]

resources:
  max_concurrent_exec: 10
  exec_timeout_max: "1h"
  max_memory_per_exec: "512MB"
```

#### Enforcement

Policy is checked **in the Agent** before executing any operation:

```go
type PolicyChecker struct {
    cfg PolicyConfig
}

func (p *PolicyChecker) CheckExec(command string) error { ... }
func (p *PolicyChecker) CheckRead(path string) error { ... }
func (p *PolicyChecker) CheckWrite(path string, size int64) error { ... }
func (p *PolicyChecker) CheckForward(port int32) error { ... }
```

Integrated into handlers:
```go
func (h *ExecHandler) Handle(ctx context.Context, req *ExecRequest, send ...) {
    if err := h.policy.CheckExec(req.Command); err != nil {
        send(execError("policy denied: " + err.Error()))
        return
    }
    // ... existing logic
}
```

#### Pattern Matching

Use `filepath.Match` style globbing for exec commands and paths:
- `*` matches any sequence of non-separator characters
- `**` matches any path depth (for paths only)
- Deny rules take precedence over allow rules

---

## Task Breakdown & Dependencies

```
P2-1  Registry Persistence ──┐
      └─ Stable Node ID      │
                              ├──→ P2-4  TLS by Default
P2-2  Token Management ──────┤
                              │
P2-3  User Store ─────────────┘
                              
P2-5  Agent Security Policies (independent)
```

### Implementation Order

| Task | Description | Depends On | Estimated Effort |
|------|-------------|------------|-----------------|
| **P2-1a** | Registry SQLite store + LoadAll/Save/Delete | — | Medium |
| **P2-1b** | Stable node_id (proto change + control.go) | P2-1a | Medium |
| **P2-1c** | Heartbeat batching | P2-1a | Small |
| **P2-2a** | Token store (SQLite + jti claim) | — | Medium |
| **P2-2b** | Revocation check in interceptor | P2-2a | Small |
| **P2-2c** | CLI commands (revoke/list/rotate) + proto | P2-2a | Medium |
| **P2-3** | User store (SQLite + bootstrap + CRUD RPCs) | — | Medium |
| **P2-4a** | Auto-TLS cert generation | — | Medium |
| **P2-4b** | CLI/Agent CA cert config + connection changes | P2-4a | Small |
| **P2-5a** | Policy config loading + checker | — | Medium |
| **P2-5b** | Handler integration + tests | P2-5a | Medium |

### Suggested Priority Order

1. **P2-1** (Registry Persistence) — fixes the biggest pain point
2. **P2-2** (Token Management) — security critical
3. **P2-4** (TLS) — security critical
4. **P2-3** (User Store) — operational improvement
5. **P2-5** (Security Policies) — defense in depth

---

## Database Architecture

All Phase 2 persistence uses a single SQLite database file:

```
/var/lib/axon-server/axon.db
├── audit_log    (existing from Phase 1)
├── nodes        (P2-1)
├── tokens       (P2-2)
└── users        (P2-3)
```

Single DB simplifies backup, migration, and operational management. SQLite handles the expected scale (hundreds of nodes, thousands of tokens) easily.

#### Migration Strategy

```go
// pkg/database/migrate.go
var migrations = []string{
    // v1 — Phase 1 (audit only)
    schemaV1,
    // v2 — Phase 2
    `CREATE TABLE nodes (...)`,
    `CREATE TABLE tokens (...)`,
    `CREATE TABLE users (...)`,
    `CREATE TABLE schema_version (version INTEGER)`,
}
```

Run migrations on server startup. Track version in `schema_version` table.

---

## Config Changes Summary

### Server Config (new fields)

```yaml
database:
  path: "/var/lib/axon-server/axon.db"  # Single SQLite for everything

tls:
  auto: true  # NEW: auto-generate self-signed if cert/key empty
```

### Agent Config (new fields)

```yaml
ca_cert: ""           # NEW: path to CA certificate
tls_insecure: false   # Default changes from implicit true to explicit false
```

### CLI Config (new fields)

```yaml
ca_cert: ""           # NEW: path to CA certificate
```
