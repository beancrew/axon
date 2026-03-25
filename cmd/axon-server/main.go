package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/garysng/axon/internal/server"
	"github.com/garysng/axon/pkg/auth"
)

var version = "dev"

func main() {
	if err := rootCmd().Execute(); err != nil {
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
	root.AddCommand(startCmd(), versionCmd())
	return root
}

// ── start ──────────────────────────────────────────────────────────────────

func startCmd() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the Axon server",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadServerConfig(configPath)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			ctx, cancel := signal.NotifyContext(context.Background(),
				syscall.SIGINT, syscall.SIGTERM)
			defer cancel()

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "axon-server %s starting on %s\n", version, cfg.ListenAddr)

			srv := server.NewServer(*cfg)
			if err := srv.Start(ctx); err != nil && !errors.Is(err, context.Canceled) {
				return fmt.Errorf("server: %w", err)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&configPath, "config", "./config.yaml", "config file path")
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
	Listen    string         `yaml:"listen"`
	TLS       tlsConfig      `yaml:"tls"`
	Auth      authConfig     `yaml:"auth"`
	Users     []userConfig   `yaml:"users"`
	Heartbeat heartbeatConfig `yaml:"heartbeat"`
	Audit     auditConfig    `yaml:"audit"`
}

type tlsConfig struct {
	Auto *bool  `yaml:"auto"`  // pointer to distinguish unset from explicit false
	Dir  string `yaml:"dir"`
	Cert string `yaml:"cert"`
	Key  string `yaml:"key"`
}

type authConfig struct {
	JWTSigningKey string `yaml:"jwt_signing_key"`
	TokenExpiry   string `yaml:"token_expiry"`
}

type userConfig struct {
	Username     string   `yaml:"username"`
	PasswordHash string   `yaml:"password_hash"`
	NodeIDs      []string `yaml:"node_ids"`
}

type heartbeatConfig struct {
	Interval string `yaml:"interval"`
	Timeout  string `yaml:"timeout"`
}

type auditConfig struct {
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

	users := make([]auth.UserEntry, len(fc.Users))
	for i, u := range fc.Users {
		users[i] = auth.UserEntry{
			Username:     u.Username,
			PasswordHash: u.PasswordHash,
			NodeIDs:      u.NodeIDs,
		}
	}

	// TLSAuto defaults to true when no explicit cert/key is provided.
	// Set tls.auto: false in the config file to disable auto-TLS entirely.
	var tlsAuto bool
	if fc.TLS.Auto != nil {
		// Explicit setting: honor it.
		tlsAuto = *fc.TLS.Auto
	} else {
		// Not set: auto-generate when no cert/key configured.
		tlsAuto = fc.TLS.Cert == "" && fc.TLS.Key == ""
	}
	if !tlsAuto && fc.TLS.Cert == "" && fc.TLS.Key == "" {
		log.Println("WARNING: TLS disabled (tls.auto: false) with no cert/key — connections will be unencrypted")
	}

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
		Users:             users,
	}

	return cfg, nil
}

func parseDuration(s string, def time.Duration) (time.Duration, error) {
	if s == "" {
		return def, nil
	}
	return time.ParseDuration(s)
}
