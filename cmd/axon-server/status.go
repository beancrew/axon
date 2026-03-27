package main

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
)

func statusCmd() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Check if the Axon server is running",
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

			addr := cfg.ListenAddr
			conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
			if err != nil {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Server is not running (cannot reach %s)\n", addr)
				return nil
			}
			_ = conn.Close()
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Server is running on %s\n", addr)
			return nil
		},
	}

	cmd.Flags().StringVar(&configPath, "config", "", "config file path (default: ~/.axon-server/config.yaml)")
	return cmd
}
