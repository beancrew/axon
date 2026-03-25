# Configuration Reference

Complete reference for all Axon configuration files and environment variables.

> [中文版 / Chinese](zh/configuration.md)

---

## Server (`axon-server`)

Config file path: passed via `--config` flag (default: `./config.yaml`)

### Full Example

```yaml
listen: ":9090"

tls:
  auto: true                              # auto-generate self-signed CA + server cert (default when no cert/key)
  dir: "/var/lib/axon-server/tls"         # directory for auto-generated certs (default: ~/.axon-server/tls)
  cert: ""                                # path to explicit TLS certificate (disables auto-TLS)
  key: ""                                 # path to explicit TLS private key

auth:
  jwt_signing_key: "${AXON_JWT_SECRET}"   # HMAC-SHA256 signing key (required)

users:                                    # bootstrap users (seeded into DB on first start)
  - username: admin
    password_hash: "$2a$10$..."           # bcrypt hash
    node_ids: ["*"]                       # ["*"] = access to all nodes
  - username: deploy-agent
    password_hash: "$2a$10$..."
    node_ids: ["web-1", "web-2"]          # restricted to specific nodes

heartbeat:
  interval: "10s"                         # heartbeat interval sent to agents (default: 10s)
  timeout: "30s"                          # mark offline after no heartbeat (default: 30s)

audit:
  db_path: "/var/lib/axon-server/audit.db"  # SQLite path for audit log (default: in-memory)
```

### Fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `listen` | string | `:9090` | gRPC listen address |
| `tls.auto` | bool | `true` (when no cert/key) | Auto-generate self-signed CA + server certificate |
| `tls.dir` | string | `~/.axon-server/tls` | Directory for auto-generated TLS files |
| `tls.cert` | string | — | Path to TLS certificate (PEM). Disables auto-TLS |
| `tls.key` | string | — | Path to TLS private key (PEM) |
| `auth.jwt_signing_key` | string | — | **Required.** HMAC-SHA256 key for JWT signing |
| `users[].username` | string | — | Bootstrap user username |
| `users[].password_hash` | string | — | Bcrypt hash of password |
| `users[].node_ids` | []string | `["*"]` | Allowed node IDs; `["*"]` = all |
| `heartbeat.interval` | duration | `10s` | Heartbeat interval for agents |
| `heartbeat.timeout` | duration | `30s` | Offline threshold |
| `audit.db_path` | string | `:memory:` | SQLite path for audit log |

### Environment Variables

| Variable | Overrides |
|----------|-----------|
| `AXON_JWT_SECRET` | `auth.jwt_signing_key` |
| `AXON_TLS_CERT` | `tls.cert` |
| `AXON_TLS_KEY` | `tls.key` |
| `AXON_TLS_DIR` | `tls.dir` |

### Auto-TLS Details

When `tls.auto` is enabled (or not explicitly disabled) and no `tls.cert`/`tls.key` is provided:

1. Server checks `tls.dir` for existing `ca.crt`
2. If missing: generates ECDSA P-256 CA (10-year validity) + server cert (1-year validity)
3. If `ca.crt` exists but `server.crt` is missing or expires within 30 days: regenerates server cert
4. CA fingerprint is logged for distribution to agents and CLI clients

Generated files:

```
~/.axon-server/tls/
├── ca.crt          # CA certificate (distribute to agents/CLI)
├── ca.key          # CA private key (keep secure, 0600)
├── server.crt      # Server certificate
└── server.key      # Server private key (0600)
```

### Data Persistence

All persistent data (node registry, tokens, users) is stored in a single SQLite database. Currently the path is not configurable via YAML — it defaults to in-memory. Production persistence will be exposed in a future release.

### Bootstrap Users

Users defined in the config file are seeded into the database on startup using `INSERT OR IGNORE` — existing users are not overwritten. After initial bootstrap, manage users via the `axon user` CLI commands or the gRPC `ManagementService` RPCs.

---

## Agent (`axon-agent`)

Config file path: `~/.axon-agent/config.yaml` (auto-created on first `start`)

### Full Example

```yaml
server: "axon.example.com:9090"
token: "agent-token-xxx"
node_id: "a1b2c3d4"              # assigned by server after first registration
node_name: "web-1"               # user-specified or hostname
labels:
  env: production
  role: web
ca_cert: "/path/to/ca.crt"       # CA certificate for TLS verification
tls_insecure: false               # skip TLS verification (dev only)
```

### Fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `server` | string | — | **Required.** Server gRPC address (`host:port`) |
| `token` | string | — | **Required.** Agent token for registration |
| `node_id` | string | — | Auto-assigned by server after first registration |
| `node_name` | string | hostname | Human-readable node name |
| `labels` | map | — | Key-value labels for grouping |
| `ca_cert` | string | — | Path to CA certificate for TLS verification |
| `tls_insecure` | bool | `false` | Skip TLS certificate verification |

### Environment Variables

| Variable | Overrides |
|----------|-----------|
| `AXON_SERVER` | `server` |
| `AXON_TOKEN` | `token` |
| `AXON_CA_CERT` | `ca_cert` |

### TLS Behavior

The agent uses a 3-way TLS selection:

1. `tls_insecure: true` → no TLS verification (plaintext gRPC)
2. `ca_cert` set → verify server cert against specified CA
3. Neither → verify server cert against system CA pool

For auto-TLS setups, copy the server's `ca.crt` to the agent machine and set `ca_cert`.

---

## CLI (`axon`)

Config file path: `~/.axon/config.yaml`

### Full Example

```yaml
server_addr: "axon.example.com:9090"
token: "eyJhbGciOiJIUzI1NiIs..."
output_format: "table"            # "table" or "json"
ca_cert: "/path/to/ca.crt"       # CA certificate for TLS verification
tls_insecure: false               # skip TLS verification (dev only)
```

### Fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `server_addr` | string | — | Server gRPC address |
| `token` | string | — | JWT token (set by `axon auth login`) |
| `output_format` | string | `table` | Default output format |
| `ca_cert` | string | — | Path to CA certificate for TLS verification |
| `tls_insecure` | bool | `false` | Skip TLS certificate verification |

### Environment Variables

| Variable | Overrides |
|----------|-----------|
| `AXON_SERVER` | `server_addr` |
| `AXON_TOKEN` | `token` |
| `AXON_CA_CERT` | `ca_cert` |

### Global Flags

These flags override config-file values for any command:

```bash
axon --ca-cert /path/to/ca.crt <command>
axon --tls-insecure <command>
```

### Config Commands

```bash
axon config set server axon.example.com:9090
axon config get server
```

Supported keys: `server`, `token`, `output_format`
