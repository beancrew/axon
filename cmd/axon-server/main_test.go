package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseDuration_Default(t *testing.T) {
	d, err := parseDuration("", 10*time.Second)
	if err != nil {
		t.Fatalf("parseDuration empty: %v", err)
	}
	if d != 10*time.Second {
		t.Errorf("got %v, want 10s", d)
	}
}

func TestParseDuration_Valid(t *testing.T) {
	d, err := parseDuration("5m", 0)
	if err != nil {
		t.Fatalf("parseDuration 5m: %v", err)
	}
	if d != 5*time.Minute {
		t.Errorf("got %v, want 5m", d)
	}
}

func TestParseDuration_Invalid(t *testing.T) {
	_, err := parseDuration("not-a-duration", 0)
	if err == nil {
		t.Fatal("expected error for invalid duration")
	}
}

func TestLoadServerConfig_Valid(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	content := `
listen: ":9090"
tls:
  cert: /etc/ssl/cert.pem
  key: /etc/ssl/key.pem
auth:
  jwt_signing_key: "test-secret"
heartbeat:
  interval: "15s"
  timeout: "45s"
users:
  - username: admin
    password_hash: "$2a$10$hash"
    node_ids: ["*"]
audit:
  db_path: /tmp/audit.db
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadServerConfig(cfgPath)
	if err != nil {
		t.Fatalf("loadServerConfig: %v", err)
	}

	if cfg.ListenAddr != ":9090" {
		t.Errorf("ListenAddr = %q, want %q", cfg.ListenAddr, ":9090")
	}
	if cfg.TLSCertPath != "/etc/ssl/cert.pem" {
		t.Errorf("TLSCertPath = %q", cfg.TLSCertPath)
	}
	if cfg.JWTSecret != "test-secret" {
		t.Errorf("JWTSecret = %q", cfg.JWTSecret)
	}
	if cfg.HeartbeatInterval != 15*time.Second {
		t.Errorf("HeartbeatInterval = %v", cfg.HeartbeatInterval)
	}
	if cfg.HeartbeatTimeout != 45*time.Second {
		t.Errorf("HeartbeatTimeout = %v", cfg.HeartbeatTimeout)
	}
	if cfg.AuditDBPath != "/tmp/audit.db" {
		t.Errorf("AuditDBPath = %q", cfg.AuditDBPath)
	}
}

func TestLoadServerConfig_Defaults(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	content := `
auth:
  jwt_signing_key: "secret"
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadServerConfig(cfgPath)
	if err != nil {
		t.Fatalf("loadServerConfig: %v", err)
	}

	if cfg.ListenAddr != ":50051" {
		t.Errorf("default ListenAddr = %q, want %q", cfg.ListenAddr, ":50051")
	}
	if cfg.HeartbeatInterval != 10*time.Second {
		t.Errorf("default HeartbeatInterval = %v, want 10s", cfg.HeartbeatInterval)
	}
	if cfg.HeartbeatTimeout != 30*time.Second {
		t.Errorf("default HeartbeatTimeout = %v, want 30s", cfg.HeartbeatTimeout)
	}
}

func TestLoadServerConfig_EnvSubstitution(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	content := `
auth:
  jwt_signing_key: "${TEST_JWT_KEY}"
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("TEST_JWT_KEY", "env-secret-value")

	cfg, err := loadServerConfig(cfgPath)
	if err != nil {
		t.Fatalf("loadServerConfig: %v", err)
	}

	if cfg.JWTSecret != "env-secret-value" {
		t.Errorf("JWTSecret = %q, want %q", cfg.JWTSecret, "env-secret-value")
	}
}

func TestLoadServerConfig_FileNotFound(t *testing.T) {
	_, err := loadServerConfig("/nonexistent/config.yaml")
	if err == nil {
		t.Fatal("expected error for missing config file")
	}
}

func TestVersionCmd(t *testing.T) {
	cmd := versionCmd()
	cmd.SetArgs([]string{})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("version: %v", err)
	}
}
