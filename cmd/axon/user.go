package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	managementpb "github.com/garysng/axon/gen/proto/management"
)

func userCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "user",
		Short: "Manage CLI users",
	}
	cmd.AddCommand(userCreateCmd(), userListCmd(), userUpdateCmd(), userDeleteCmd())
	return cmd
}

// ── user create ────────────────────────────────────────────────────────────

func userCreateCmd() *cobra.Command {
	var nodeIDs string

	cmd := &cobra.Command{
		Use:   "create <username>",
		Short: "Create a new CLI user",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			username := args[0]

			// Prompt for password (hide input).
			_, _ = fmt.Fprint(cmd.OutOrStdout(), "Password: ")
			passwordBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
			_, _ = fmt.Fprintln(cmd.OutOrStdout())
			if err != nil {
				return fmt.Errorf("read password: %w", err)
			}
			password := strings.TrimSpace(string(passwordBytes))
			if password == "" {
				return fmt.Errorf("password cannot be empty")
			}

			client, closer, err := dialManagement()
			if err != nil {
				return err
			}
			defer closer()

			ids := parseNodeIDs(nodeIDs)
			resp, err := client.CreateUser(context.Background(), &managementpb.CreateUserRequest{
				Username: username,
				Password: password,
				NodeIds:  ids,
			})
			if err != nil {
				return fmt.Errorf("create user: %w", err)
			}
			if !resp.GetSuccess() {
				return fmt.Errorf("create user failed: %s", resp.GetError())
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "User %q created.\n", username)
			return nil
		},
	}

	cmd.Flags().StringVar(&nodeIDs, "node-ids", "*", "allowed node IDs (comma-separated; '*' for all)")
	return cmd
}

// ── user list ──────────────────────────────────────────────────────────────

func userListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all CLI users",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, closer, err := dialManagement()
			if err != nil {
				return err
			}
			defer closer()

			resp, err := client.ListUsers(context.Background(), &managementpb.ListUsersRequest{})
			if err != nil {
				return fmt.Errorf("list users: %w", err)
			}

			if len(resp.GetUsers()) == 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No users found.")
				return nil
			}

			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 3, ' ', 0)
			_, _ = fmt.Fprintln(w, "USERNAME\tNODE IDS\tDISABLED\tCREATED")
			for _, u := range resp.GetUsers() {
				nodeIDs := strings.Join(u.GetNodeIds(), ",")
				disabled := "no"
				if u.GetDisabled() {
					disabled = "yes"
				}
				created := time.Unix(u.GetCreatedAt(), 0).Format("2006-01-02 15:04:05")
				_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", u.GetUsername(), nodeIDs, disabled, created)
			}
			return w.Flush()
		},
	}
}

// ── user update ────────────────────────────────────────────────────────────

func userUpdateCmd() *cobra.Command {
	var nodeIDs string
	var changePassword bool

	cmd := &cobra.Command{
		Use:   "update <username>",
		Short: "Update a CLI user's node IDs or password",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			username := args[0]

			var password string
			if changePassword {
				_, _ = fmt.Fprint(cmd.OutOrStdout(), "New password: ")
				passwordBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
				_, _ = fmt.Fprintln(cmd.OutOrStdout())
				if err != nil {
					return fmt.Errorf("read password: %w", err)
				}
				password = strings.TrimSpace(string(passwordBytes))
				if password == "" {
					return fmt.Errorf("password cannot be empty")
				}
			}

			client, closer, err := dialManagement()
			if err != nil {
				return err
			}
			defer closer()

			ids := parseNodeIDs(nodeIDs)
			resp, err := client.UpdateUser(context.Background(), &managementpb.UpdateUserRequest{
				Username: username,
				Password: password,
				NodeIds:  ids,
			})
			if err != nil {
				return fmt.Errorf("update user: %w", err)
			}
			if !resp.GetSuccess() {
				return fmt.Errorf("update user failed: %s", resp.GetError())
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "User %q updated.\n", username)
			return nil
		},
	}

	cmd.Flags().StringVar(&nodeIDs, "node-ids", "", "allowed node IDs (comma-separated; '*' for all)")
	cmd.Flags().BoolVar(&changePassword, "password", false, "prompt to change the password")
	return cmd
}

// ── user delete ────────────────────────────────────────────────────────────

func userDeleteCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "delete <username>",
		Short: "Delete a CLI user",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			username := args[0]

			if !force {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Are you sure you want to delete user %q? (y/N): ", username)
				var answer string
				if _, err := fmt.Fscan(os.Stdin, &answer); err != nil {
					answer = ""
				}
				answer = strings.TrimSpace(strings.ToLower(answer))
				if answer != "y" && answer != "yes" {
					_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Aborted.")
					return nil
				}
			}

			client, closer, err := dialManagement()
			if err != nil {
				return err
			}
			defer closer()

			resp, err := client.DeleteUser(context.Background(), &managementpb.DeleteUserRequest{
				Username: username,
			})
			if err != nil {
				return fmt.Errorf("delete user: %w", err)
			}
			if !resp.GetSuccess() {
				return fmt.Errorf("delete user failed: %s", resp.GetError())
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "User %q deleted.\n", username)
			return nil
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "skip confirmation prompt")
	return cmd
}

// parseNodeIDs splits a comma-separated node IDs string into a slice.
// An empty string returns nil (no update to node IDs).
func parseNodeIDs(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	ids := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			ids = append(ids, p)
		}
	}
	return ids
}
