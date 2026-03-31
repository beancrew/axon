package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/beancrew/axon/pkg/config"
)

// writeTemp writes content to a temp file and returns its path.
func writeTemp(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "axon-config-*.yaml")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	_ = f.Close()
	return f.Name()
}

// ---------- ServerConfig ----------

func TestLoadServerConfig_Defaults(t *testing.T) {
	// Non-existent file → defaults
	cfg, err := config.LoadServerConfig("/nonexistent/path/config.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ListenAddr != ":50051" {
		t.Errorf("ListenAddr = %q, want %q", cfg.ListenAddr, ":50051")
	}
	if cfg.HeartbeatTimeoutSeconds != 30 {
		t.Errorf("HeartbeatTimeoutSeconds = %d, want 30", cfg.HeartbeatTimeoutSeconds)
	}
}

func TestLoadServerConfig_YAML(t *testing.T) {
	yaml := `
listen_addr: ":9090"
tls_cert_path: "/certs/server.crt"
tls_key_path: "/certs/server.key"
jwt_secret: "supersecret"
audit_db_path: "/var/axon/audit.db"
heartbeat_timeout_seconds: 60
`
	path := writeTemp(t, yaml)
	cfg, err := config.LoadServerConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.ListenAddr != ":9090" {
		t.Errorf("ListenAddr = %q, want %q", cfg.ListenAddr, ":9090")
	}
	if cfg.TLSCertPath != "/certs/server.crt" {
		t.Errorf("TLSCertPath = %q", cfg.TLSCertPath)
	}
	if cfg.TLSKeyPath != "/certs/server.key" {
		t.Errorf("TLSKeyPath = %q", cfg.TLSKeyPath)
	}
	if cfg.JWTSecret != "supersecret" {
		t.Errorf("JWTSecret = %q", cfg.JWTSecret)
	}
	if cfg.AuditDBPath != "/var/axon/audit.db" {
		t.Errorf("AuditDBPath = %q", cfg.AuditDBPath)
	}
	if cfg.HeartbeatTimeoutSeconds != 60 {
		t.Errorf("HeartbeatTimeoutSeconds = %d, want 60", cfg.HeartbeatTimeoutSeconds)
	}
}

func TestLoadServerConfig_EnvOverride(t *testing.T) {
	yaml := `listen_addr: ":9090"
jwt_secret: "from-file"
`
	path := writeTemp(t, yaml)

	t.Setenv("AXON_LISTEN_ADDR", ":8080")
	t.Setenv("AXON_JWT_SECRET", "from-env")
	t.Setenv("AXON_TLS_CERT", "/env/cert.crt")
	t.Setenv("AXON_TLS_KEY", "/env/key.key")
	t.Setenv("AXON_AUDIT_DB_PATH", "/env/audit.db")

	cfg, err := config.LoadServerConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.ListenAddr != ":8080" {
		t.Errorf("ListenAddr = %q, want :8080 (env override)", cfg.ListenAddr)
	}
	if cfg.JWTSecret != "from-env" {
		t.Errorf("JWTSecret = %q, want from-env", cfg.JWTSecret)
	}
	if cfg.TLSCertPath != "/env/cert.crt" {
		t.Errorf("TLSCertPath = %q", cfg.TLSCertPath)
	}
	if cfg.TLSKeyPath != "/env/key.key" {
		t.Errorf("TLSKeyPath = %q", cfg.TLSKeyPath)
	}
	if cfg.AuditDBPath != "/env/audit.db" {
		t.Errorf("AuditDBPath = %q", cfg.AuditDBPath)
	}
}

func TestLoadServerConfig_InvalidYAML(t *testing.T) {
	path := writeTemp(t, "{unclosed: [bracket")
	_, err := config.LoadServerConfig(path)
	if err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}
}

// ---------- AgentConfig ----------

func TestLoadAgentConfig_Defaults(t *testing.T) {
	cfg, err := config.LoadAgentConfig("/nonexistent/path/config.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ServerAddr != "localhost:50051" {
		t.Errorf("ServerAddr = %q, want localhost:50051", cfg.ServerAddr)
	}
}

func TestLoadAgentConfig_YAML(t *testing.T) {
	yaml := `
server_addr: "axon.example.com:50051"
token: "agent-token"
node_name: "worker-1"
labels:
  zone: "us-east-1"
  role: "worker"
tls_insecure: true
`
	path := writeTemp(t, yaml)
	cfg, err := config.LoadAgentConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.ServerAddr != "axon.example.com:50051" {
		t.Errorf("ServerAddr = %q", cfg.ServerAddr)
	}
	if cfg.Token != "agent-token" {
		t.Errorf("Token = %q", cfg.Token)
	}
	if cfg.NodeName != "worker-1" {
		t.Errorf("NodeName = %q", cfg.NodeName)
	}
	if cfg.Labels["zone"] != "us-east-1" {
		t.Errorf("Labels[zone] = %q", cfg.Labels["zone"])
	}
	if cfg.Labels["role"] != "worker" {
		t.Errorf("Labels[role] = %q", cfg.Labels["role"])
	}
	if !cfg.TLSInsecure {
		t.Error("TLSInsecure = false, want true")
	}
}

func TestLoadAgentConfig_EnvOverride(t *testing.T) {
	yaml := `server_addr: "from-file:50051"
token: "file-token"
`
	path := writeTemp(t, yaml)

	t.Setenv("AXON_SERVER_ADDR", "env-server:9999")
	t.Setenv("AXON_TOKEN", "env-token")

	cfg, err := config.LoadAgentConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ServerAddr != "env-server:9999" {
		t.Errorf("ServerAddr = %q, want env-server:9999", cfg.ServerAddr)
	}
	if cfg.Token != "env-token" {
		t.Errorf("Token = %q, want env-token", cfg.Token)
	}
}

func TestLoadAgentConfig_InvalidYAML(t *testing.T) {
	path := writeTemp(t, "{unclosed: [bracket")
	_, err := config.LoadAgentConfig(path)
	if err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}
}

// ---------- CLIConfig ----------

func TestLoadCLIConfig_Defaults(t *testing.T) {
	cfg, err := config.LoadCLIConfig("/nonexistent/path/config.yaml")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ServerAddr != "localhost:50051" {
		t.Errorf("ServerAddr = %q, want localhost:50051", cfg.ServerAddr)
	}
	if cfg.OutputFormat != "table" {
		t.Errorf("OutputFormat = %q, want table", cfg.OutputFormat)
	}
}

func TestLoadCLIConfig_YAML(t *testing.T) {
	yaml := `
server_addr: "axon.example.com:50051"
token: "cli-token"
output_format: "json"
tls_insecure: true
`
	path := writeTemp(t, yaml)
	cfg, err := config.LoadCLIConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ServerAddr != "axon.example.com:50051" {
		t.Errorf("ServerAddr = %q", cfg.ServerAddr)
	}
	if cfg.Token != "cli-token" {
		t.Errorf("Token = %q", cfg.Token)
	}
	if cfg.OutputFormat != "json" {
		t.Errorf("OutputFormat = %q, want json", cfg.OutputFormat)
	}
	if !cfg.TLSInsecure {
		t.Error("TLSInsecure = false, want true")
	}
}

func TestLoadCLIConfig_EnvOverride(t *testing.T) {
	yaml := `server_addr: "from-file:50051"
output_format: "table"
`
	path := writeTemp(t, yaml)

	t.Setenv("AXON_SERVER_ADDR", "env-server:1234")
	t.Setenv("AXON_TOKEN", "env-cli-token")

	cfg, err := config.LoadCLIConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ServerAddr != "env-server:1234" {
		t.Errorf("ServerAddr = %q, want env-server:1234", cfg.ServerAddr)
	}
	if cfg.Token != "env-cli-token" {
		t.Errorf("Token = %q, want env-cli-token", cfg.Token)
	}
}

func TestLoadCLIConfig_InvalidYAML(t *testing.T) {
	path := writeTemp(t, "{unclosed: [bracket")
	_, err := config.LoadCLIConfig(path)
	if err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}
}

// ---------- Default paths ----------

func TestDefaultPaths(t *testing.T) {
	serverPath := config.DefaultServerConfigPath()
	if serverPath != "/etc/axon-server/config.yaml" {
		t.Errorf("DefaultServerConfigPath = %q", serverPath)
	}

	agentPath := config.DefaultAgentConfigPath()
	if filepath.Base(agentPath) != "config.yaml" {
		t.Errorf("DefaultAgentConfigPath base = %q, want config.yaml", filepath.Base(agentPath))
	}
	if filepath.Base(filepath.Dir(agentPath)) != ".axon-agent" {
		t.Errorf("DefaultAgentConfigPath dir = %q, want .axon-agent", filepath.Dir(agentPath))
	}

	cliPath := config.DefaultCLIConfigPath()
	if filepath.Base(cliPath) != "config.yaml" {
		t.Errorf("DefaultCLIConfigPath base = %q, want config.yaml", filepath.Base(cliPath))
	}
	if filepath.Base(filepath.Dir(cliPath)) != ".axon" {
		t.Errorf("DefaultCLIConfigPath dir = %q, want .axon", filepath.Dir(cliPath))
	}
}
