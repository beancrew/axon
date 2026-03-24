package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	managementpb "github.com/garysng/axon/gen/proto/management"
	"github.com/garysng/axon/pkg/config"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func authCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Authentication commands",
	}
	cmd.AddCommand(authLoginCmd(), authTokenCmd())
	return cmd
}

// ── auth login ─────────────────────────────────────────────────────────────

func authLoginCmd() *cobra.Command {
	var serverAddr string

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Authenticate with the Axon server",
		Long:  "Prompts for username and password, then requests a JWT token from the server.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfgPath := config.DefaultCLIConfigPath()
			cfg, err := config.LoadCLIConfig(cfgPath)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			// Use --server flag if provided, otherwise fall back to config.
			if serverAddr != "" {
				cfg.ServerAddr = serverAddr
			}
			if cfg.ServerAddr == "" {
				return fmt.Errorf("server address not configured; use --server flag or run: axon config set server <addr>")
			}

			// Prompt for username.
			reader := bufio.NewReader(os.Stdin)
			_, _ = fmt.Fprint(cmd.OutOrStdout(), "Username: ")
			username, err := reader.ReadString('\n')
			if err != nil {
				return fmt.Errorf("read username: %w", err)
			}
			username = strings.TrimSpace(username)

			// Prompt for password (hide input).
			_, _ = fmt.Fprint(cmd.OutOrStdout(), "Password: ")
			passwordBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
			_, _ = fmt.Fprintln(cmd.OutOrStdout()) // newline after hidden input
			if err != nil {
				return fmt.Errorf("read password: %w", err)
			}
			password := string(passwordBytes)

			// Connect without auth — Login RPC is unauthenticated.
			conn, err := grpc.NewClient(cfg.ServerAddr,
				grpc.WithTransportCredentials(insecure.NewCredentials()),
			)
			if err != nil {
				return fmt.Errorf("connect to server %q: %w", cfg.ServerAddr, err)
			}
			defer func() { _ = conn.Close() }()

			client := managementpb.NewManagementServiceClient(conn)
			resp, err := client.Login(context.Background(), &managementpb.LoginRequest{
				Username: username,
				Password: password,
			})
			if err != nil {
				return fmt.Errorf("login: %w", err)
			}

			if resp.GetError() != "" {
				return fmt.Errorf("login failed: %s", resp.GetError())
			}

			// Save token and server address to config.
			cfg.Token = resp.GetToken()
			if err := config.SaveCLIConfig(cfgPath, cfg); err != nil {
				return fmt.Errorf("save config: %w", err)
			}

			expiry := time.Unix(resp.GetExpiresAt(), 0)
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Login successful. Token saved to %s\nToken expires at %s\n", cfgPath, expiry.Format(time.RFC3339))
			return nil
		},
	}

	cmd.Flags().StringVar(&serverAddr, "server", "", "server address (saved to config)")
	return cmd
}

// ── auth token ─────────────────────────────────────────────────────────────

func authTokenCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "token",
		Short: "Display the current authentication token",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.LoadCLIConfig(config.DefaultCLIConfigPath())
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}
			if cfg.Token == "" {
				return fmt.Errorf("not authenticated; run: axon auth login")
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), cfg.Token)
			return nil
		},
	}
}
