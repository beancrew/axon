# Axon CLI Full Reference

## Config File Locations

| Component | Config Path | Data Dir |
|-----------|------------|----------|
| axon (CLI) | `~/.axon/config.yaml` | `~/.axon/` |
| axon-server | `~/.axon-server/config.yaml` | `~/.axon-server/` |
| axon-agent | `~/.axon-agent/config.yaml` | `~/.axon-agent/` |

## CLI Config (`~/.axon/config.yaml`)

```yaml
server_addr: "axon.example.com:9090"
token: "eyJhbGciOiJIUzI1NiIs..."
output_format: "table"     # table | json
ca_cert: "/path/to/ca.crt" # optional, for TLS
```

## All Commands

### axon (CLI)

| Command | Description | gRPC Mode | Auth |
|---------|-------------|-----------|------|
| `exec <node> <cmd>` | Execute remote command | server stream | ✅ |
| `read <node> <path>` | Read remote file | server stream | ✅ |
| `write <node> <path>` | Write remote file (stdin) | client stream | ✅ |
| `forward create <node> <L:R>` | Create port forward | bidi stream | ✅ |
| `forward list` | List active forwards | — | — |
| `forward delete <id>` | Delete a forward | — | — |
| `forward <node> <L:R>` | Blocking forward (compat) | bidi stream | ✅ |
| `node list` | List nodes | unary | ✅ |
| `node info <node>` | Node details | unary | ✅ |
| `node remove <node>` | Remove node | unary | ✅ |
| `auth login` | Get JWT token | unary | ❌ |
| `auth token` | Show current token | local | — |
| `auth list-tokens` | List issued tokens | unary | ✅ |
| `auth revoke <id>` | Revoke token | unary | ✅ |
| `token create-join` | Create join token | unary | ✅ |
| `token list-join` | List join tokens | unary | ✅ |
| `token revoke-join <id>` | Revoke join token | unary | ✅ |
| `user create <name>` | Create user | unary | ✅ |
| `user list` | List users | unary | ✅ |
| `user update <name>` | Update user | unary | ✅ |
| `user delete <name>` | Delete user | unary | ✅ |
| `config set <k> <v>` | Set config | local | — |
| `config get <k>` | Get config | local | — |
| `version` | Show version | local | — |

### axon-server

| Command | Description |
|---------|-------------|
| `init` | Initialize config, DB, admin user, join token |
| `start` | Start gRPC server (add `--daemon` for background) |
| `stop` | Stop daemon |
| `status` | Show server status |
| `version` | Show version |

### axon-agent

| Command | Description |
|---------|-------------|
| `join <addr> <token>` | Enroll with server |
| `start` | Reconnect using saved config |
| `stop` | Stop agent |
| `status` | Show agent status |
| `version` | Show version |

## TLS Modes

| Scenario | Server Setup | Client/Agent Flag |
|----------|-------------|-------------------|
| No TLS (default) | `axon-server init` | `--tls-insecure` |
| Auto-TLS | `axon-server init --tls` | `--ca-cert ~/.axon-server/tls/ca.crt` |
| Custom cert | `tls.cert` + `tls.key` in config | system CA or `--ca-cert` |

## GitHub

- Repo: `beancrew/axon` (private)
- Releases: `https://github.com/beancrew/axon/releases`
- Install script: `scripts/install.sh` (auto-detect OS/arch)
