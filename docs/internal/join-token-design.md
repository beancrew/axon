# Join Token & Simplified Deployment — Design Document

## Overview

This document describes the changes needed to make Axon deployable with minimal friction. The current deployment requires manual config file editing, bcrypt hash generation, TLS certificate distribution, and has no mechanism to generate Agent tokens.

**Goal:** Three commands to go from zero to a working Axon deployment.

```bash
# Server
axon-server init --admin admin --password mypass
axon-server start

# Edge node (one command)
axon-agent join 10.0.1.5:9090 axon-join-a3f8b2c1

# CLI
axon config set server 10.0.1.5:9090
axon auth login
```

---

## 1. TLS Default Change

### Current Behavior

TLS is enabled by default via auto-TLS. When no `tls.cert`/`tls.key` is configured, the server auto-generates a self-signed CA and server certificate. This forces all agents and CLI clients to either distribute the CA cert or use `--tls-insecure`.

### New Behavior

**TLS is disabled by default.** Plain-text gRPC for internal network deployments.

| Config | Behavior |
|--------|----------|
| No TLS config (default) | Plain-text gRPC |
| `tls.cert` + `tls.key` | User-provided certificates |
| `tls.auto: true` | Auto-TLS (self-signed CA), opt-in |

### Code Changes

**`cmd/axon-server/main.go`:**
```go
// Before:
tlsAuto = fc.TLS.Cert == "" && fc.TLS.Key == ""

// After:
var tlsAuto bool
if fc.TLS.Auto != nil {
    tlsAuto = *fc.TLS.Auto
}
// No implicit auto-TLS when cert/key are absent.
```

**`cmd/axon/client.go`:**
```go
// Default transport: insecure (no TLS).
// Only use TLS when ca_cert is set or tls_insecure is explicitly false with a CA.
switch {
case cfg.CACert != "":
    // Use specified CA cert for TLS verification.
case cfg.TLSEnabled:
    // Use system CA pool.
default:
    // Plain-text gRPC (insecure credentials).
}
```

**`internal/agent/agent.go`:** Same 3-way logic, default to insecure.

**`pkg/config/config.go`:** Add `TLSEnabled bool` field to `CLIConfig` and `AgentConfig` (opt-in for TLS without explicit CA cert).

---

## 2. `axon-server init`

New subcommand that initializes a server configuration. **Does not start the server.**

### Usage

```
axon-server init [flags]

Flags:
  --listen <addr>       Listen address (default: ":9090")
  --admin <username>    Admin username (default: "admin")
  --password <pass>     Admin password (required; interactive prompt if omitted)
  --data-dir <path>     Data directory (default: "~/.axon-server")
  --tls                 Enable auto-TLS
```

### Execution Flow

```
1. Check if ~/.axon-server/config.yaml already exists → error if so (--force to overwrite)
2. Generate random JWT secret (32 bytes, hex-encoded)
3. Bcrypt-hash the admin password
4. Generate join-token: "axon-join-" + 32 random hex chars
5. Create data directory (~/.axon-server/)
6. Open SQLite database at ~/.axon-server/axon.db
7. Create join_tokens table
8. Insert join-token (hash only, unlimited uses, no expiry)
9. Write ~/.axon-server/config.yaml
10. Print results
```

### Generated config.yaml

```yaml
listen: ":9090"

auth:
  jwt_signing_key: "a1b2c3d4e5f6..."  # auto-generated

users:
  - username: admin
    password_hash: "$2a$10$..."
    node_ids: ["*"]

data:
  db_path: "/home/user/.axon-server/axon.db"

audit:
  db_path: "/home/user/.axon-server/audit.db"
```

### Output

```
✅ Server initialized

   Config:     ~/.axon-server/config.yaml
   Database:   ~/.axon-server/axon.db
   Listen:     :9090
   Admin user: admin

📋 Start the server:
   axon-server start --config ~/.axon-server/config.yaml

📋 Join a node:
   axon-agent join <SERVER_IP>:9090 axon-join-a3f8b2c1d4e5f6a7b8c9d0e1f2a3b4c5

📋 Use CLI:
   axon config set server <SERVER_IP>:9090
   axon auth login
```

### Code Location

`cmd/axon-server/init.go` — new file.

---

## 3. Join Token Mechanism

### Data Model

```sql
CREATE TABLE join_tokens (
    id         TEXT PRIMARY KEY,           -- short display ID (8 hex chars)
    token_hash TEXT NOT NULL UNIQUE,       -- SHA-256 hash of full token
    created_at INTEGER NOT NULL,
    uses       INTEGER NOT NULL DEFAULT 0, -- current use count
    max_uses   INTEGER NOT NULL DEFAULT 0, -- 0 = unlimited
    expires_at INTEGER NOT NULL DEFAULT 0, -- 0 = never expires
    revoked    INTEGER NOT NULL DEFAULT 0
);
```

### Token Format

```
axon-join-<64-hex-chars>
```

Example: `axon-join-a3f8b2c1d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1`

Server stores only `SHA-256(token)`. The plaintext is shown once at generation time.

### Storage: `pkg/auth/join_store.go`

```go
type JoinTokenStore struct {
    db *sql.DB
}

type JoinTokenEntry struct {
    ID        string
    TokenHash string
    CreatedAt int64
    Uses      int
    MaxUses   int  // 0 = unlimited
    ExpiresAt int64 // 0 = never
    Revoked   bool
}

func NewJoinTokenStoreFromDB(db *sql.DB) (*JoinTokenStore, error)
func (s *JoinTokenStore) Insert(entry *JoinTokenEntry) error
func (s *JoinTokenStore) Validate(tokenHash string) (*JoinTokenEntry, error) // checks revoked/expired/max_uses, increments uses
func (s *JoinTokenStore) List() ([]*JoinTokenEntry, error)
func (s *JoinTokenStore) Revoke(id string) error
```

`Validate` is atomic: check validity + increment `uses` in one transaction.

### Proto Additions (`management.proto`)

```protobuf
// ─── Join Token Management (requires JWT auth) ───

rpc CreateJoinToken(CreateJoinTokenRequest) returns (CreateJoinTokenResponse);
rpc ListJoinTokens(ListJoinTokensRequest) returns (ListJoinTokensResponse);
rpc RevokeJoinToken(RevokeJoinTokenRequest) returns (RevokeJoinTokenResponse);

// ─── Agent Join (no auth required — token is self-authenticating) ───

rpc JoinAgent(JoinAgentRequest) returns (JoinAgentResponse);

// Messages

message CreateJoinTokenRequest {
    int32 max_uses = 1;        // 0 = unlimited
    int64 expires_seconds = 2; // 0 = never; seconds from now
}

message CreateJoinTokenResponse {
    string token = 1;          // full plaintext token (shown once)
    string id = 2;             // short ID for management
}

message ListJoinTokensRequest {}

message ListJoinTokensResponse {
    repeated JoinTokenInfo tokens = 1;
}

message JoinTokenInfo {
    string id = 1;
    int64 created_at = 2;
    int32 uses = 3;
    int32 max_uses = 4;        // 0 = unlimited
    int64 expires_at = 5;      // 0 = never
    bool revoked = 6;
}

message RevokeJoinTokenRequest {
    string id = 1;
}

message RevokeJoinTokenResponse {
    bool success = 1;
    string error = 2;
}

message JoinAgentRequest {
    string join_token = 1;     // full plaintext token
    string node_name = 2;     // desired name (defaults to hostname)
    axon.control.NodeInfo info = 3;
}

message JoinAgentResponse {
    bool success = 1;
    string error = 2;
    string agent_token = 3;    // JWT for subsequent Connect calls
    string node_id = 4;       // assigned stable node ID
    string ca_cert_pem = 5;   // CA certificate PEM (empty if TLS disabled)
    int32 heartbeat_interval_seconds = 6;
}
```

### JoinAgent RPC Flow (`internal/server/management.go`)

```
1. Receive JoinAgentRequest{join_token, node_name, info}
2. Hash the token: SHA-256(join_token)
3. Call joinTokenStore.Validate(hash)
   → checks: exists? revoked? expired? max_uses exceeded?
   → atomically increments uses
   → returns error if invalid
4. Generate node_id (UUID)
5. Register node in registry (name, info, status=offline)
6. Sign Agent JWT: auth.SignAgentToken(secret, node_id, expiry)
7. If TLS enabled: read CA cert PEM from tls.dir/ca.crt
8. Return JoinAgentResponse{agent_token, node_id, ca_cert_pem, heartbeat_interval}
```

### gRPC Interceptor Bypass

Add `JoinAgent` to the interceptor whitelist alongside `Login`:

```go
var noAuthMethods = map[string]bool{
    "/axon.management.ManagementService/Login":     true,
    "/axon.management.ManagementService/JoinAgent": true,
}
```

---

## 4. `axon-agent join`

New subcommand that combines enrollment + start in one step.

### Usage

```
axon-agent join <server-addr> <join-token> [flags]

Flags:
  --name <node-name>    Node name (default: hostname)
  --labels key=value    Labels (repeatable)
```

### Execution Flow

```
1. Collect system info (NodeInfo)
2. Dial server (plain-text gRPC by default)
3. Call ManagementService.JoinAgent(join_token, node_name, info)
4. On success:
   a. Save ~/.axon-agent/config.yaml:
      server: <addr>
      token: <agent_token>
      node_id: <node_id>
      node_name: <name>
   b. If ca_cert_pem is non-empty:
      - Write ~/.axon-agent/ca.crt
      - Set ca_cert in config
   c. Print success message
5. Start normal agent loop (Connect stream with the new token)
```

### Idempotency

If `~/.axon-agent/config.yaml` already exists with a `node_id`, the agent should use `Connect` (normal re-registration) instead of `JoinAgent`. The `join` command is for first-time enrollment only.

### Code Location

`cmd/axon-agent/join.go` — new file. Reuses agent startup logic from `main.go`.

---

## 5. CLI Join Token Management

### Commands

```bash
# Create a new join token
axon token create-join [--max-uses N] [--expires 24h]

# List join tokens
axon token list-join

# Revoke a join token
axon token revoke-join <token-id>
```

### Code Location

`cmd/axon/token.go` — new file (or extend `auth.go`).

---

## 6. Install Script

### `scripts/install.sh`

```bash
#!/bin/sh
# Usage: curl -fsSL https://raw.githubusercontent.com/beancrew/axon/main/scripts/install.sh | sh -s -- [server|agent|cli]
#
# Detects OS/ARCH, downloads the appropriate binary from GitHub Releases,
# installs to /usr/local/bin (or ~/.axon/bin if no root).

set -e

COMPONENT="${1:-cli}"   # default: install CLI
REPO="beancrew/axon"
INSTALL_DIR="/usr/local/bin"

# Detect platform
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
case "$ARCH" in
    x86_64) ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    *) echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

# Map component to binary name
case "$COMPONENT" in
    server) BINARY="axon-server" ;;
    agent)  BINARY="axon-agent" ;;
    cli)    BINARY="axon" ;;
    *)      echo "Unknown component: $COMPONENT"; exit 1 ;;
esac

# Get latest release tag
VERSION=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | grep tag_name | cut -d '"' -f4)

# Download
URL="https://github.com/${REPO}/releases/download/${VERSION}/${BINARY}-${OS}-${ARCH}.tar.gz"
echo "Downloading ${BINARY} ${VERSION} for ${OS}/${ARCH}..."
curl -fsSL "$URL" | tar xz -C /tmp
install -m 755 "/tmp/${BINARY}" "${INSTALL_DIR}/${BINARY}"

echo "✅ ${BINARY} ${VERSION} installed to ${INSTALL_DIR}/${BINARY}"

# Next steps
case "$COMPONENT" in
    server) echo "   Run: axon-server init --admin admin --password <pass>" ;;
    agent)  echo "   Run: axon-agent join <server-addr> <join-token>" ;;
    cli)    echo "   Run: axon config set server <server-addr> && axon auth login" ;;
esac
```

### GitHub Actions Release Workflow

`.github/workflows/release.yml` — triggered on `v*` tags:

1. Run tests
2. Cross-compile: 3 binaries × 4 platforms (linux/darwin × amd64/arm64)
3. Package as `.tar.gz`
4. Create GitHub Release with all artifacts

---

## 7. Server Startup Changes

### `axon-server start`

Add startup logging for join token availability:

```go
// After DB init, check for join tokens
count := joinTokenStore.CountActive()
if count == 0 {
    log.Println("WARNING: no active join tokens. Run 'axon token create-join' to generate one.")
} else {
    log.Printf("server: %d active join token(s) available", count)
}
```

### `server.go` Init Changes

Add `joinTokenStore` to the shared DB initialization:

```go
joinTokenStore, err := auth.NewJoinTokenStoreFromDB(db)
// ... pass to newManagementService
```

---

## 8. Config Changes

### `ServerConfig` additions

```go
type ServerConfig struct {
    // ... existing fields ...
    DataDBPath string  // exposed in YAML as data.db_path
}
```

### `fileConfig` additions

```go
type dataConfig struct {
    DBPath string `yaml:"db_path"`
}

type fileConfig struct {
    // ... existing fields ...
    Data dataConfig `yaml:"data"`
}
```

---

## 9. Migration Notes

### Breaking Changes

- **TLS default off**: Existing deployments that relied on auto-TLS will need to add `tls.auto: true` to their config.
- **Agent token format**: Agents enrolled via `join` get a standard JWT. Existing agents using manually-crafted tokens continue to work.

### Non-breaking

- All existing RPCs unchanged.
- `axon-server start` with existing config works as before.
- New RPCs are additive (no proto field number conflicts).

---

## 10. Task Breakdown

| # | Task | Priority | Dependency |
|---|------|----------|------------|
| 1 | TLS default off | P0 | None |
| 2 | `data.db_path` config field | P0 | None |
| 3 | `join_tokens` SQLite store | P0 | #2 |
| 4 | Proto additions (JoinAgent + token mgmt RPCs) | P0 | None |
| 5 | JoinAgent RPC implementation | P0 | #3, #4 |
| 6 | Join token management RPCs | P0 | #3, #4 |
| 7 | `axon-server init` command | P0 | #2, #3 |
| 8 | `axon-agent join` command | P0 | #5 |
| 9 | CLI `axon token create-join/list/revoke` | P1 | #6 |
| 10 | Install script | P1 | GitHub Releases |
| 11 | Release workflow (GitHub Actions) | P1 | None |
| 12 | Update docs (quickstart, config ref) | P1 | #7, #8 |
