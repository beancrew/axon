package main

import (
	"errors"
	"fmt"
	"os"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/garysng/axon/pkg/config"
)

// version is set at build time via -ldflags.
var version = "dev"

func main() {
	if err := rootCmd().Execute(); err != nil {
		var ee *exitError
		if errors.As(err, &ee) {
			os.Exit(ee.code)
		}
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "axon",
		Short: "Axon CLI — remote operations on agent nodes",
		Long:  "Axon connects AI agents to real machines. Use this CLI to execute commands, read/write files, and forward ports on remote nodes.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.AddCommand(
		versionCmd(),
		configCmd(),
		nodeCmd(),
		authCmd(),
		execCmd(),
		readCmd(),
		writeCmd(),
		forwardCmd(),
	)

	return root
}

// ── version ────────────────────────────────────────────────────────────────

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version info",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("axon %s (%s, %s/%s)\n", version, runtime.Version(), runtime.GOOS, runtime.GOARCH)
		},
	}
}

// ── config ─────────────────────────────────────────────────────────────────

func configCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage local CLI configuration",
	}
	cmd.AddCommand(configGetCmd(), configSetCmd())
	return cmd
}

func configGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <key>",
		Short: "Get a config value",
		Long:  "Supported keys: server, token, output_format",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadCLIConfig(config.DefaultCLIConfigPath())
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			key := args[0]
			switch key {
			case "server":
				fmt.Println(cfg.ServerAddr)
			case "token":
				fmt.Println(maskToken(cfg.Token))
			case "output_format":
				fmt.Println(cfg.OutputFormat)
			default:
				return fmt.Errorf("unknown config key: %s (supported: server, token, output_format)", key)
			}
			return nil
		},
	}
}

// maskToken returns a masked version of a token for display (e.g. "eyJhbG...4F0").
func maskToken(token string) string {
	if token == "" {
		return "(not set)"
	}
	if len(token) < 12 {
		return "***"
	}
	return token[:6] + "..." + token[len(token)-3:]
}

func configSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a config value",
		Long:  "Supported keys: server, token, output_format",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfgPath := config.DefaultCLIConfigPath()
			cfg, err := config.LoadCLIConfig(cfgPath)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			key, value := args[0], args[1]
			switch key {
			case "server":
				cfg.ServerAddr = value
			case "token":
				cfg.Token = value
			case "output_format":
				cfg.OutputFormat = value
			default:
				return fmt.Errorf("unknown config key: %s (supported: server, token, output_format)", key)
			}
			if err := config.SaveCLIConfig(cfgPath, cfg); err != nil {
				return fmt.Errorf("save config: %w", err)
			}
			fmt.Printf("Set %s = %s\n", key, value)
			return nil
		},
	}
}
