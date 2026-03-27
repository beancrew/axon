package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/garysng/axon/internal/server"
)

var version = "dev"

func main() {
	if err := rootCmd().Execute(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "axon-server",
		Short:         "Axon Server — routes operations between CLI and agents",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(startCmd(), initCmd(), versionCmd(), statusCmd())
	return root
}

// ── start ──────────────────────────────────────────────────────────────────

func startCmd() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the Axon server",
		RunE: func(cmd *cobra.Command, args []string) error {
			if configPath == "" {
				home, err := os.UserHomeDir()
				if err != nil {
					return fmt.Errorf("get home dir: %w", err)
				}
				configPath = filepath.Join(home, ".axon-server", "config.yaml")
			}

			cfg, err := loadServerConfig(configPath)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			ctx, cancel := signal.NotifyContext(context.Background(),
				syscall.SIGINT, syscall.SIGTERM)
			defer cancel()

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "axon-server %s listening on %s (pid %d)\n",
				version, cfg.ListenAddr, os.Getpid())

			srv := server.NewServer(*cfg)
			if err := srv.Start(ctx); err != nil && !errors.Is(err, context.Canceled) {
				return fmt.Errorf("server: %w", err)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&configPath, "config", "", "config file path (default: ~/.axon-server/config.yaml)")
	return cmd
}

// ── version ────────────────────────────────────────────────────────────────

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version info",
		Run: func(cmd *cobra.Command, args []string) {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "axon-server %s (%s, %s/%s)\n",
				version, runtime.Version(), runtime.GOOS, runtime.GOARCH)
		},
	}
}

// ── config ─────────────────────────────────────────────────────────────────

type fileConfig struct {
	Listen    string          `yaml:"listen"`
	TLS       tlsConfig       `yaml:"tls"`
	Auth      authConfig      `yaml:"auth"`
	Heartbeat heartbeatConfig `yaml:"heartbeat"`
	Audit     auditConfig     `yaml:"audit"`
	Data      dataConfig      `yaml:"data"`
}

type tlsConfig struct {
	Auto *bool  `yaml:"auto"` // pointer to distinguish unset from explicit false
	Dir  string `yaml:"dir"`
	Cert string `yaml:"cert"`
	Key  string `yaml:"key"`
}

type authConfig struct {
	JWTSigningKey string `yaml:"jwt_signing_key"`
}

type heartbeatConfig struct {
	Interval string `yaml:"interval"`
	Timeout  string `yaml:"timeout"`
}

type auditConfig struct {
	DBPath string `yaml:"db_path"`
}

type dataConfig struct {
	DBPath string `yaml:"db_path"`
}

func loadServerConfig(path string) (*server.ServerConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	// Substitute environment variables: ${VAR_NAME}
	content := os.Expand(string(data), os.Getenv)

	var fc fileConfig
	if err := yaml.Unmarshal([]byte(content), &fc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	if fc.Listen == "" {
		fc.Listen = ":50051"
	}

	hbInterval, err := parseDuration(fc.Heartbeat.Interval, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("heartbeat.interval: %w", err)
	}
	hbTimeout, err := parseDuration(fc.Heartbeat.Timeout, 30*time.Second)
	if err != nil {
		return nil, fmt.Errorf("heartbeat.timeout: %w", err)
	}

	// TLS is disabled by default. Enable only when tls.auto is explicitly true,
	// or when tls.cert + tls.key are provided.
	tlsAuto := fc.TLS.Auto != nil && *fc.TLS.Auto

	cfg := &server.ServerConfig{
		ListenAddr:        fc.Listen,
		TLSCertPath:       fc.TLS.Cert,
		TLSKeyPath:        fc.TLS.Key,
		TLSAuto:           tlsAuto,
		TLSDir:            fc.TLS.Dir,
		JWTSecret:         strings.TrimSpace(fc.Auth.JWTSigningKey),
		HeartbeatInterval: hbInterval,
		HeartbeatTimeout:  hbTimeout,
		AuditDBPath:       fc.Audit.DBPath,
		DataDBPath:        fc.Data.DBPath,
	}

	return cfg, nil
}

func parseDuration(s string, def time.Duration) (time.Duration, error) {
	if s == "" {
		return def, nil
	}
	return time.ParseDuration(s)
}
