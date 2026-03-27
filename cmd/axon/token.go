package main

import (
	"context"
	"fmt"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	managementpb "github.com/garysng/axon/gen/proto/management"
)

func tokenCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "token",
		Short: "Manage tokens (CLI tokens and join tokens)",
	}
	cmd.AddCommand(
		listTokensCmd(),
		revokeTokenCmd(),
		createJoinTokenCmd(),
		listJoinTokensCmd(),
		revokeJoinTokenCmd(),
	)
	return cmd
}

// ── token list ──────────────────────────────────────────────────────────────

func listTokensCmd() *cobra.Command {
	var kind string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List active (non-revoked) tokens",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, closer, err := dialManagement()
			if err != nil {
				return err
			}
			defer closer()

			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()

			resp, err := client.ListTokens(ctx, &managementpb.ListTokensRequest{
				Kind: kind,
			})
			if err != nil {
				return fmt.Errorf("list tokens: %w", err)
			}

			if len(resp.Tokens) == 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No active tokens.")
				return nil
			}

			for _, t := range resp.Tokens {
				var expiresStr string
				if t.ExpiresAt == 0 {
					expiresStr = "never"
				} else {
					expiresStr = time.Unix(t.ExpiresAt, 0).Format(time.RFC3339)
				}
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%-36s  %-6s  %-12s  expires=%s\n",
					t.Id, t.Kind, t.UserId, expiresStr)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "", "filter by token kind (cli, agent)")
	return cmd
}

// ── token revoke ────────────────────────────────────────────────────────────

func revokeTokenCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "revoke <token-id>",
		Short: "Revoke a token by its ID (jti)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, closer, err := dialManagement()
			if err != nil {
				return err
			}
			defer closer()

			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()

			resp, err := client.RevokeToken(ctx, &managementpb.RevokeTokenRequest{
				TokenId: args[0],
			})
			if err != nil {
				return fmt.Errorf("revoke: %w", err)
			}
			if !resp.Success {
				return fmt.Errorf("revoke failed: %s", resp.Error)
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Token revoked successfully.")
			return nil
		},
	}
}

// ── token create-join ───────────────────────────────────────────────────────

func createJoinTokenCmd() *cobra.Command {
	var (
		maxUses int32
		expires string
	)

	cmd := &cobra.Command{
		Use:   "create-join",
		Short: "Create a new join token for enrolling agent nodes",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, closer, err := dialManagement()
			if err != nil {
				return err
			}
			defer closer()

			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()

			var expiresSeconds int64
			if expires != "" {
				d, err := time.ParseDuration(expires)
				if err != nil {
					return fmt.Errorf("parse --expires %q: %w", expires, err)
				}
				expiresSeconds = int64(d.Seconds())
			}

			resp, err := client.CreateJoinToken(ctx, &managementpb.CreateJoinTokenRequest{
				MaxUses:        maxUses,
				ExpiresSeconds: expiresSeconds,
			})
			if err != nil {
				return fmt.Errorf("create-join: %w", err)
			}

			out := cmd.OutOrStdout()
			_, _ = fmt.Fprintf(out, "Join token created (ID: %s)\n\n", resp.Id)
			_, _ = fmt.Fprintf(out, "   %s\n\n", resp.Token)
			_, _ = fmt.Fprintln(out, "Enroll a node:")
			_, _ = fmt.Fprintf(out, "   axon-agent join <SERVER_ADDR> %s\n", resp.Token)
			return nil
		},
	}

	cmd.Flags().Int32Var(&maxUses, "max-uses", 0, "maximum number of uses (0 = unlimited)")
	cmd.Flags().StringVar(&expires, "expires", "", "token expiry duration (e.g. 24h, 168h)")
	return cmd
}

// ── token list-join ─────────────────────────────────────────────────────────

func listJoinTokensCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list-join",
		Short: "List join tokens",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, closer, err := dialManagement()
			if err != nil {
				return err
			}
			defer closer()

			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()

			resp, err := client.ListJoinTokens(ctx, &managementpb.ListJoinTokensRequest{})
			if err != nil {
				return fmt.Errorf("list-join: %w", err)
			}

			if len(resp.Tokens) == 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No join tokens.")
				return nil
			}

			out := cmd.OutOrStdout()
			_, _ = fmt.Fprintf(out, "%-36s  %-5s  %-5s  %-7s  %-25s  %s\n",
				"ID", "USES", "MAX", "REVOKED", "EXPIRES", "CREATED")
			for _, t := range resp.Tokens {
				maxStr := "inf"
				if t.MaxUses > 0 {
					maxStr = fmt.Sprintf("%d", t.MaxUses)
				}
				expiresStr := "never"
				if t.ExpiresAt > 0 {
					expiresStr = time.Unix(t.ExpiresAt, 0).Format(time.RFC3339)
				}
				revokedStr := "no"
				if t.Revoked {
					revokedStr = "yes"
				}
				created := time.Unix(t.CreatedAt, 0).Format(time.RFC3339)
				_, _ = fmt.Fprintf(out, "%-36s  %-5d  %-5s  %-7s  %-25s  %s\n",
					t.Id, t.Uses, maxStr, revokedStr, expiresStr, created)
			}
			return nil
		},
	}
}

// ── token revoke-join ───────────────────────────────────────────────────────

func revokeJoinTokenCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "revoke-join <token-id>",
		Short: "Revoke a join token by its ID",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, closer, err := dialManagement()
			if err != nil {
				return err
			}
			defer closer()

			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()

			resp, err := client.RevokeJoinToken(ctx, &managementpb.RevokeJoinTokenRequest{
				Id: args[0],
			})
			if err != nil {
				return fmt.Errorf("revoke-join: %w", err)
			}
			if !resp.Success {
				return fmt.Errorf("revoke failed: %s", resp.Error)
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Join token revoked.")
			return nil
		},
	}
}
