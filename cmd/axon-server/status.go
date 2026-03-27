package main

import (
	"fmt"
	"net"
	"os"
	"time"

	"github.com/spf13/cobra"
)

func statusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Check if the Axon server is running",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()

			// Check PID file.
			pidPath := pidFilePath()
			pid, pidErr := readPID(pidPath)
			running := pidErr == nil && isProcessRunning(pid)

			if running {
				_, _ = fmt.Fprintf(out, "Status:  running\n")
				_, _ = fmt.Fprintf(out, "PID:     %d\n", pid)

				// Show uptime from PID file mtime.
				if info, err := os.Stat(pidPath); err == nil {
					uptime := time.Since(info.ModTime()).Truncate(time.Second)
					_, _ = fmt.Fprintf(out, "Uptime:  %s\n", uptime)
				}
			} else {
				_, _ = fmt.Fprintf(out, "Status:  stopped\n")
			}

			// Also try TCP dial to check if port is reachable.
			configPath := defaultDataDir() + "/config.yaml"
			if cfg, err := loadServerConfig(configPath); err == nil {
				conn, err := net.DialTimeout("tcp", cfg.ListenAddr, 2*time.Second)
				if err == nil {
					_ = conn.Close()
					_, _ = fmt.Fprintf(out, "Listen:  %s (reachable)\n", cfg.ListenAddr)
				} else if running {
					_, _ = fmt.Fprintf(out, "Listen:  %s (not reachable)\n", cfg.ListenAddr)
				}
			}

			_, _ = fmt.Fprintf(out, "Log:     %s\n", logFilePath())
			return nil
		},
	}
	return cmd
}
