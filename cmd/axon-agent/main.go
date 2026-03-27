package main

import (
	"context"
	"encoding/json"
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

	"github.com/garysng/axon/internal/agent"
	"github.com/garysng/axon/pkg/config"
	"github.com/garysng/axon/pkg/display"
)

// version is set at build time via -ldflags.
var version = "dev"

func main() {
	if err := rootCmd().Execute(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "Error:", err)
		var ee *exitError
		if errors.As(err, &ee) {
			os.Exit(ee.code)
		}
		os.Exit(1)
	}
}

type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string { return e.err.Error() }
func (e *exitError) Unwrap() error { return e.err }

func rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "axon-agent",
		Short:         "Axon agent — connects this machine to the Axon control plane",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(
		startCmd(),
		joinCmd(),
		stopCmd(),
		statusCmd(),
		agentConfigCmd(),
		versionCmd(),
	)
	return root
}

// ── path helpers ───────────────────────────────────────────────────────────

func pidFilePath() string {
	return filepath.Join(filepath.Dir(config.DefaultAgentConfigPath()), "agent.pid")
}

func logFilePath() string {
	return filepath.Join(filepath.Dir(config.DefaultAgentConfigPath()), "agent.log")
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

// agentStatus is the JSON-serialisable representation of the status command
// output.
type agentStatus struct {
	Status   string `json:"status"`
	PID      int    `json:"pid,omitempty"`
	Server   string `json:"server"`
	NodeID   string `json:"node_id"`
	NodeName string `json:"node_name"`
	Uptime   string `json:"uptime,omitempty"`
}

// formatUptime formats a duration as "Xd Yh Zm", omitting leading zero
// components (except always emitting at least minutes).
func formatUptime(d time.Duration) string {
	d = d.Truncate(time.Minute)
	total := int(d.Minutes())
	if total <= 0 {
		return "0m"
	}
	mins := total % 60
	total /= 60
	hours := total % 24
	days := total / 24

	var parts []string
	if days > 0 {
		parts = append(parts, fmt.Sprintf("%dd", days))
	}
	if hours > 0 || days > 0 {
		parts = append(parts, fmt.Sprintf("%dh", hours))
	}
	parts = append(parts, fmt.Sprintf("%dm", mins))
	return strings.Join(parts, " ")
}

// ── version ────────────────────────────────────────────────────────────────

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version info",
		Run: func(cmd *cobra.Command, args []string) {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "axon-agent %s (%s, %s/%s)\n", version, runtime.Version(), runtime.GOOS, runtime.GOARCH)
		},
	}
}

// ── start ──────────────────────────────────────────────────────────────────

func startCmd() *cobra.Command {
	var (
		flagServer     string
		flagToken      string
		flagName       string
		flagLabels     []string
		flagForeground bool
	)

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the agent",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Prevent double-start.
			pidPath := pidFilePath()
			if pid, err := readPID(pidPath); err == nil && isProcessRunning(pid) {
				return fmt.Errorf("agent is already running (PID %d)", pid)
			}

			cfgPath := config.DefaultAgentConfigPath()
			cfg, err := config.LoadAgentConfig(cfgPath)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			// Apply flag overrides only when the flag was explicitly set.
			if cmd.Flags().Changed("server") {
				cfg.ServerAddr = flagServer
			}
			if cmd.Flags().Changed("token") {
				cfg.Token = flagToken
			}
			if cmd.Flags().Changed("name") {
				cfg.NodeName = flagName
			}
			if len(flagLabels) > 0 {
				if cfg.Labels == nil {
					cfg.Labels = make(map[string]string)
				}
				for _, kv := range flagLabels {
					parts := strings.SplitN(kv, "=", 2)
					if len(parts) != 2 {
						return fmt.Errorf("invalid label %q (expected key=value)", kv)
					}
					cfg.Labels[parts[0]] = parts[1]
				}
			}

			if flagForeground {
				return runForeground(cfg, cfgPath)
			}
			return runDaemon(cmd, flagServer, flagToken, flagName, flagLabels)
		},
	}

	cmd.Flags().StringVar(&flagServer, "server", "", "Server address (overrides config)")
	cmd.Flags().StringVar(&flagToken, "token", "", "Auth token (overrides config)")
	cmd.Flags().StringVar(&flagName, "name", "", "Node name (overrides config)")
	cmd.Flags().StringArrayVar(&flagLabels, "labels", nil, "Label as key=value (repeatable)")
	cmd.Flags().BoolVar(&flagForeground, "foreground", false, "Run in foreground (do not daemonize)")

	return cmd
}

// runForeground runs the agent in the foreground, blocking until context
// cancellation (SIGINT/SIGTERM).
func runForeground(cfg *config.AgentConfig, cfgPath string) error {
	pidPath := pidFilePath()
	if err := writePID(pidPath, os.Getpid()); err != nil {
		return fmt.Errorf("write PID: %w", err)
	}
	defer func() { _ = os.Remove(pidPath) }()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	a := agent.NewAgent(*cfg, cfgPath)
	a.EnableDataPlane()
	if err := a.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("agent: %w", err)
	}
	return nil
}

// runDaemon re-execs self with --foreground, detached from the current
// process group. The parent exits immediately after spawning the daemon.
func runDaemon(cmd *cobra.Command, server, token, name string, labels []string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable path: %w", err)
	}

	// Reconstruct start args with --foreground.
	args := []string{"start", "--foreground"}
	if cmd.Flags().Changed("server") {
		args = append(args, "--server", server)
	}
	if cmd.Flags().Changed("token") {
		args = append(args, "--token", token)
	}
	if cmd.Flags().Changed("name") {
		args = append(args, "--name", name)
	}
	for _, l := range labels {
		args = append(args, "--labels", l)
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

	_, _ = fmt.Fprintf(os.Stdout, "axon-agent started (pid %d), log: %s\n", daemonCmd.Process.Pid, logPath)
	return nil
}

// ── stop ───────────────────────────────────────────────────────────────────

func stopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the running agent",
		RunE: func(cmd *cobra.Command, args []string) error {
			pidPath := pidFilePath()
			pid, err := readPID(pidPath)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					return fmt.Errorf("agent is not running (no PID file)")
				}
				return fmt.Errorf("read PID: %w", err)
			}

			proc, err := os.FindProcess(pid)
			if err != nil {
				return fmt.Errorf("find process %d: %w", pid, err)
			}

			if err := proc.Signal(syscall.SIGTERM); err != nil {
				return fmt.Errorf("send SIGTERM to %d: %w", pid, err)
			}
			_, _ = fmt.Fprintf(os.Stdout, "Sent SIGTERM to pid %d, waiting...\n", pid)

			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				if !isProcessRunning(pid) {
					_ = os.Remove(pidPath)
					_, _ = fmt.Fprintln(os.Stdout, "axon-agent stopped")
					return nil
				}
				time.Sleep(200 * time.Millisecond)
			}

			// Process did not exit within 5 s — escalate to SIGKILL.
			_ = proc.Signal(syscall.SIGKILL)
			_ = os.Remove(pidPath)
			_, _ = fmt.Fprintln(os.Stdout, "axon-agent killed (did not exit within 5s)")
			return nil
		},
	}
}

// ── status ─────────────────────────────────────────────────────────────────

func statusCmd() *cobra.Command {
	var flagJSON bool

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show agent status",
		Run: func(cmd *cobra.Command, args []string) {
			cfg, _ := config.LoadAgentConfig(config.DefaultAgentConfigPath())
			if cfg == nil {
				cfg = &config.AgentConfig{ServerAddr: "localhost:50051"}
			}

			pidPath := pidFilePath()
			pid, pidErr := readPID(pidPath)

			running := pidErr == nil && isProcessRunning(pid)
			var uptime string
			if running {
				if info, err := os.Stat(pidPath); err == nil {
					uptime = formatUptime(time.Since(info.ModTime()))
				}
			}

			out := cmd.OutOrStdout()

			if flagJSON {
				s := agentStatus{
					Server:   cfg.ServerAddr,
					NodeID:   cfg.NodeID,
					NodeName: cfg.NodeName,
				}
				if running {
					s.Status = "running"
					s.PID = pid
					s.Uptime = uptime
				} else {
					s.Status = "stopped"
				}
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				_ = enc.Encode(s)
				return
			}

			if running {
				_, _ = fmt.Fprintf(out, "Status:     running\n")
				_, _ = fmt.Fprintf(out, "PID:        %d\n", pid)
				_, _ = fmt.Fprintf(out, "Server:     %s\n", cfg.ServerAddr)
				_, _ = fmt.Fprintf(out, "Node ID:    %s\n", cfg.NodeID)
				_, _ = fmt.Fprintf(out, "Node Name:  %s\n", cfg.NodeName)
				if uptime != "" {
					_, _ = fmt.Fprintf(out, "Uptime:     %s\n", uptime)
				}
			} else {
				_, _ = fmt.Fprintf(out, "Status:     stopped\n")
				_, _ = fmt.Fprintf(out, "Server:     %s\n", cfg.ServerAddr)
				_, _ = fmt.Fprintf(out, "Node ID:    %s\n", cfg.NodeID)
				_, _ = fmt.Fprintf(out, "Node Name:  %s\n", cfg.NodeName)
			}
		},
	}

	cmd.Flags().BoolVar(&flagJSON, "json", false, "Output as JSON")
	return cmd
}

// ── config ─────────────────────────────────────────────────────────────────

func agentConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage agent configuration",
	}
	cmd.AddCommand(agentConfigGetCmd(), agentConfigSetCmd())
	return cmd
}

func agentConfigGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <key>",
		Short: "Get a config value",
		Long:  "Supported keys: server, token, name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadAgentConfig(config.DefaultAgentConfigPath())
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			switch args[0] {
			case "server":
				_, _ = fmt.Fprintln(os.Stdout, cfg.ServerAddr)
			case "token":
				_, _ = fmt.Fprintln(os.Stdout, display.MaskToken(cfg.Token))
			case "name":
				_, _ = fmt.Fprintln(os.Stdout, cfg.NodeName)
			default:
				return fmt.Errorf("unknown config key: %s (supported: server, token, name)", args[0])
			}
			return nil
		},
	}
}

func agentConfigSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a config value",
		Long:  "Supported keys: server, token, name",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfgPath := config.DefaultAgentConfigPath()
			cfg, err := config.LoadAgentConfig(cfgPath)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			key, value := args[0], args[1]
			switch key {
			case "server":
				cfg.ServerAddr = value
			case "token":
				cfg.Token = value
			case "name":
				cfg.NodeName = value
			default:
				return fmt.Errorf("unknown config key: %s (supported: server, token, name)", key)
			}
			if err := config.SaveAgentConfig(cfgPath, cfg); err != nil {
				return fmt.Errorf("save config: %w", err)
			}
			_, _ = fmt.Fprintf(os.Stdout, "Set %s = %s\n", key, value)
			return nil
		},
	}
}
