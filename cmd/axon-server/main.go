package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/beancrew/axon/internal/server"
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
	root.AddCommand(startCmd(), stopCmd(), initCmd(), versionCmd(), statusCmd())
	return root
}

// ── path helpers ───────────────────────────────────────────────────────────

func defaultDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "~/.axon-server"
	}
	return filepath.Join(home, ".axon-server")
}

func pidFilePath() string {
	return filepath.Join(defaultDataDir(), "server.pid")
}

func logFilePath() string {
	return filepath.Join(defaultDataDir(), "server.log")
}

func writePID(path string, pid int) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create dir: %w", err)
	}
	return os.WriteFile(path, []byte(strconv.Itoa(pid)+"\n"), 0600)
}

func readPID(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, fmt.Errorf("invalid PID in file: %w", err)
	}
	return pid, nil
}

func isProcessRunning(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

// ── start ──────────────────────────────────────────────────────────────────

func startCmd() *cobra.Command {
	var (
		configPath     string
		flagForeground bool
	)

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the Axon server",
		RunE: func(cmd *cobra.Command, args []string) error {
			if configPath == "" {
				configPath = filepath.Join(defaultDataDir(), "config.yaml")
			}

			cfg, err := loadServerConfig(configPath)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			// Prevent double-start.
			pidPath := pidFilePath()
			if pid, err := readPID(pidPath); err == nil && isProcessRunning(pid) {
				return fmt.Errorf("server is already running (PID %d)", pid)
			}

			if flagForeground {
				return runServerForeground(cmd, cfg, pidPath)
			}
			return runServerDaemon(cmd, configPath)
		},
	}

	cmd.Flags().StringVar(&configPath, "config", "", "config file path (default: ~/.axon-server/config.yaml)")
	cmd.Flags().BoolVar(&flagForeground, "foreground", false, "run in foreground (do not daemonize)")
	return cmd
}

// runServerForeground runs the server in the foreground, blocking until signal.
func runServerForeground(cmd *cobra.Command, cfg *server.ServerConfig, pidPath string) error {
	if err := writePID(pidPath, os.Getpid()); err != nil {
		return fmt.Errorf("write PID: %w", err)
	}
	defer func() { _ = os.Remove(pidPath) }()

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
}

// runServerDaemon re-execs self with --foreground, detached from the terminal.
func runServerDaemon(cmd *cobra.Command, configPath string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable path: %w", err)
	}

	args := []string{"start", "--foreground"}
	if configPath != "" {
		args = append(args, "--config", configPath)
	}

	logPath := logFilePath()
	if err := os.MkdirAll(filepath.Dir(logPath), 0700); err != nil {
		return fmt.Errorf("create log dir: %w", err)
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}
	defer func() { _ = logFile.Close() }()

	daemonCmd := exec.Command(exe, args...)
	daemonCmd.Stdout = logFile
	daemonCmd.Stderr = logFile
	daemonCmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := daemonCmd.Start(); err != nil {
		return fmt.Errorf("start daemon: %w", err)
	}

	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "axon-server started (pid %d), log: %s\n", daemonCmd.Process.Pid, logPath)
	return nil
}

// ── stop ───────────────────────────────────────────────────────────────────

func stopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the running server",
		RunE: func(cmd *cobra.Command, args []string) error {
			pidPath := pidFilePath()
			pid, err := readPID(pidPath)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					return fmt.Errorf("server is not running (no PID file)")
				}
				return fmt.Errorf("read PID: %w", err)
			}

			if !isProcessRunning(pid) {
				_ = os.Remove(pidPath)
				return fmt.Errorf("server is not running (stale PID %d)", pid)
			}

			proc, err := os.FindProcess(pid)
			if err != nil {
				return fmt.Errorf("find process %d: %w", pid, err)
			}

			if err := proc.Signal(syscall.SIGTERM); err != nil {
				return fmt.Errorf("send SIGTERM to %d: %w", pid, err)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Sent SIGTERM to pid %d, waiting...\n", pid)

			deadline := time.Now().Add(10 * time.Second)
			for time.Now().Before(deadline) {
				if !isProcessRunning(pid) {
					_ = os.Remove(pidPath)
					_, _ = fmt.Fprintln(cmd.OutOrStdout(), "axon-server stopped")
					return nil
				}
				time.Sleep(200 * time.Millisecond)
			}

			// Process did not exit within 10s — escalate to SIGKILL.
			_ = proc.Signal(syscall.SIGKILL)
			_ = os.Remove(pidPath)
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "axon-server killed (did not exit within 10s)")
			return nil
		},
	}
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
