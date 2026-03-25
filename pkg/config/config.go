// Package config provides configuration loading for Axon components.
// It supports YAML files and environment variable overrides.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// ServerConfig holds configuration for the Axon server.
type ServerConfig struct {
	ListenAddr              string `yaml:"listen_addr"`
	TLSCertPath             string `yaml:"tls_cert_path"`
	TLSKeyPath              string `yaml:"tls_key_path"`
	TLSAuto                 bool   `yaml:"tls_auto"`
	TLSDir                  string `yaml:"tls_dir"`
	JWTSecret               string `yaml:"jwt_secret"`
	AuditDBPath             string `yaml:"audit_db_path"`
	HeartbeatTimeoutSeconds int    `yaml:"heartbeat_timeout_seconds"`
}

// AgentConfig holds configuration for the Axon agent.
type AgentConfig struct {
	ServerAddr  string            `yaml:"server_addr"`
	Token       string            `yaml:"token"`
	NodeID      string            `yaml:"node_id,omitempty"` // assigned by server after first registration
	NodeName    string            `yaml:"node_name"`
	Labels      map[string]string `yaml:"labels"`
	CACert      string            `yaml:"ca_cert"`
	TLSInsecure bool              `yaml:"tls_insecure"`
}

// CLIConfig holds configuration for the Axon CLI.
type CLIConfig struct {
	ServerAddr   string `yaml:"server_addr"`
	Token        string `yaml:"token"`
	OutputFormat string `yaml:"output_format"`
	CACert       string `yaml:"ca_cert"`
	TLSInsecure  bool   `yaml:"tls_insecure"`
}

// DefaultServerConfigPath returns the default path for server configuration.
func DefaultServerConfigPath() string {
	return "/etc/axon-server/config.yaml"
}

// DefaultAgentConfigPath returns the default path for agent configuration.
func DefaultAgentConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "~/.axon-agent/config.yaml"
	}
	return filepath.Join(home, ".axon-agent", "config.yaml")
}

// DefaultCLIConfigPath returns the default path for CLI configuration.
func DefaultCLIConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "~/.axon/config.yaml"
	}
	return filepath.Join(home, ".axon", "config.yaml")
}

// LoadServerConfig loads server configuration from the given path.
// If the file does not exist, default values are returned.
// Environment variables take precedence over file values.
func LoadServerConfig(path string) (*ServerConfig, error) {
	cfg := &ServerConfig{
		ListenAddr:              ":50051",
		HeartbeatTimeoutSeconds: 30,
	}

	if err := loadYAML(path, cfg); err != nil {
		return nil, err
	}

	applyServerEnv(cfg)
	return cfg, nil
}

// LoadAgentConfig loads agent configuration from the given path.
// If the file does not exist, default values are returned.
// Environment variables take precedence over file values.
func LoadAgentConfig(path string) (*AgentConfig, error) {
	cfg := &AgentConfig{
		ServerAddr: "localhost:50051",
	}

	if err := loadYAML(path, cfg); err != nil {
		return nil, err
	}

	applyAgentEnv(cfg)
	return cfg, nil
}

// LoadCLIConfig loads CLI configuration from the given path.
// If the file does not exist, default values are returned.
// Environment variables take precedence over file values.
func LoadCLIConfig(path string) (*CLIConfig, error) {
	cfg := &CLIConfig{
		ServerAddr:   "localhost:50051",
		OutputFormat: "table",
	}

	if err := loadYAML(path, cfg); err != nil {
		return nil, err
	}

	applyCLIEnv(cfg)
	return cfg, nil
}

// SaveAgentConfig writes the agent configuration to the given path, creating
// parent directories as needed. The file is created with mode 0600 to protect
// the token.
func SaveAgentConfig(path string, cfg *AgentConfig) error {
	return saveYAML(path, cfg)
}

// SaveCLIConfig writes the CLI configuration to the given path, creating
// parent directories as needed. The file is created with mode 0600 to protect
// the token.
func SaveCLIConfig(path string, cfg *CLIConfig) error {
	return saveYAML(path, cfg)
}

// saveYAML marshals src to YAML and writes it to path, creating parent
// directories with mode 0700 and the file with mode 0600.
func saveYAML(path string, src any) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("config: create dir %q: %w", dir, err)
	}
	data, err := yaml.Marshal(src)
	if err != nil {
		return fmt.Errorf("config: marshal: %w", err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("config: write %q: %w", path, err)
	}
	return nil
}

// loadYAML reads a YAML file into dst. If the file does not exist, it returns nil
// (caller keeps defaults). Any other error is returned wrapped.
func loadYAML(path string, dst any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("config: read %q: %w", path, err)
	}

	if err := yaml.Unmarshal(data, dst); err != nil {
		return fmt.Errorf("config: parse %q: %w", path, err)
	}
	return nil
}

func applyServerEnv(cfg *ServerConfig) {
	if v := os.Getenv("AXON_LISTEN_ADDR"); v != "" {
		cfg.ListenAddr = v
	}
	if v := os.Getenv("AXON_TLS_CERT"); v != "" {
		cfg.TLSCertPath = v
	}
	if v := os.Getenv("AXON_TLS_KEY"); v != "" {
		cfg.TLSKeyPath = v
	}
	if v := os.Getenv("AXON_TLS_DIR"); v != "" {
		cfg.TLSDir = v
	}
	if v := os.Getenv("AXON_JWT_SECRET"); v != "" {
		cfg.JWTSecret = v
	}
	if v := os.Getenv("AXON_AUDIT_DB_PATH"); v != "" {
		cfg.AuditDBPath = v
	}
}

func applyAgentEnv(cfg *AgentConfig) {
	if v := os.Getenv("AXON_SERVER_ADDR"); v != "" {
		cfg.ServerAddr = v
	}
	if v := os.Getenv("AXON_TOKEN"); v != "" {
		cfg.Token = v
	}
	if v := os.Getenv("AXON_CA_CERT"); v != "" {
		cfg.CACert = v
	}
}

func applyCLIEnv(cfg *CLIConfig) {
	if v := os.Getenv("AXON_SERVER_ADDR"); v != "" {
		cfg.ServerAddr = v
	}
	if v := os.Getenv("AXON_TOKEN"); v != "" {
		cfg.Token = v
	}
	if v := os.Getenv("AXON_CA_CERT"); v != "" {
		cfg.CACert = v
	}
}
