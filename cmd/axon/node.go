package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	managementpb "github.com/garysng/axon/gen/proto/management"
)

func nodeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "node",
		Short: "Manage agent nodes",
	}
	cmd.AddCommand(nodeListCmd(), nodeInfoCmd(), nodeRemoveCmd())
	return cmd
}

// ── node list ──────────────────────────────────────────────────────────────

func nodeListCmd() *cobra.Command {
	var statusFilter string
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all registered nodes",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, closer, err := dialServer(true)
			if err != nil {
				return err
			}
			defer closer()

			resp, err := client.ListNodes(context.Background(), &managementpb.ListNodesRequest{})
			if err != nil {
				return fmt.Errorf("list nodes: %w", err)
			}

			nodes := resp.GetNodes()
			if statusFilter != "" {
				nodes = filterByStatus(nodes, statusFilter)
			}

			if jsonOutput {
				return printJSON(cmd, nodes)
			}

			if len(nodes) == 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No nodes found.")
				return nil
			}

			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 3, ' ', 0)
			_, _ = fmt.Fprintln(w, "NAME\tSTATUS\tOS\tARCH\tIP\tVERSION\tLAST SEEN")
			for _, n := range nodes {
				osName := ""
				if n.GetOsInfo() != nil {
					osName = truncate(n.GetOsInfo().GetPrettyName(), 20)
				}
				_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
					n.GetNodeName(),
					n.GetStatus(),
					osName,
					n.GetArch(),
					n.GetIp(),
					n.GetAgentVersion(),
					relativeTime(n.GetLastHeartbeat()),
				)
			}
			return w.Flush()
		},
	}

	cmd.Flags().StringVar(&statusFilter, "status", "", "filter by status (online, offline)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output in JSON format")
	return cmd
}

// ── node info ──────────────────────────────────────────────────────────────

func nodeInfoCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "info <node-id|node-name>",
		Short: "Show detailed info for a node",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, closer, err := dialServer(true)
			if err != nil {
				return err
			}
			defer closer()

			resp, err := client.GetNode(context.Background(), &managementpb.GetNodeRequest{
				NodeId: args[0],
			})
			if err != nil {
				return fmt.Errorf("get node: %w", err)
			}

			if jsonOutput {
				return printJSON(cmd, resp)
			}

			s := resp.GetSummary()
			w := cmd.OutOrStdout()
			_, _ = fmt.Fprintf(w, "Name:           %s\n", s.GetNodeName())
			_, _ = fmt.Fprintf(w, "Status:         %s\n", s.GetStatus())
			if osInfo := s.GetOsInfo(); osInfo != nil {
				_, _ = fmt.Fprintf(w, "OS:             %s\n", osInfo.GetPrettyName())
			}
			_, _ = fmt.Fprintf(w, "Arch:           %s\n", s.GetArch())
			_, _ = fmt.Fprintf(w, "IP:             %s\n", s.GetIp())
			_, _ = fmt.Fprintf(w, "Uptime:         %s\n", formatDuration(resp.GetUptimeSeconds()))
			_, _ = fmt.Fprintf(w, "Agent Version:  %s\n", s.GetAgentVersion())
			_, _ = fmt.Fprintf(w, "Connected:      %s\n", time.Unix(s.GetConnectedAt(), 0).Format("2006-01-02 15:04:05 MST"))
			_, _ = fmt.Fprintf(w, "Last Heartbeat: %s\n", time.Unix(s.GetLastHeartbeat(), 0).Format("2006-01-02 15:04:05 MST"))

			if labels := resp.GetLabels(); len(labels) > 0 {
				_, _ = fmt.Fprintf(w, "Labels:\n")
				for k, v := range labels {
					_, _ = fmt.Fprintf(w, "  %s: %s\n", k, v)
				}
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output in JSON format")
	return cmd
}

// ── node remove ────────────────────────────────────────────────────────────

func nodeRemoveCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "remove <node-id>",
		Short: "Remove a node from the registry",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !force {
				reader := bufio.NewReader(os.Stdin)
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Are you sure you want to remove node %q? (y/N): ", args[0])
				answer, err := reader.ReadString('\n')
				if err != nil {
					return fmt.Errorf("read confirmation: %w", err)
				}
				answer = strings.TrimSpace(strings.ToLower(answer))
				if answer != "y" && answer != "yes" {
					_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Aborted.")
					return nil
				}
			}

			client, closer, err := dialServer(true)
			if err != nil {
				return err
			}
			defer closer()

			resp, err := client.RemoveNode(context.Background(), &managementpb.RemoveNodeRequest{
				NodeId: args[0],
			})
			if err != nil {
				return fmt.Errorf("remove node: %w", err)
			}

			if !resp.GetSuccess() {
				return fmt.Errorf("remove failed: %s", resp.GetError())
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Node %q removed.\n", args[0])
			return nil
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "skip confirmation prompt")
	return cmd
}

// ── helpers ────────────────────────────────────────────────────────────────

func filterByStatus(nodes []*managementpb.NodeSummary, status string) []*managementpb.NodeSummary {
	filtered := make([]*managementpb.NodeSummary, 0, len(nodes))
	for _, n := range nodes {
		if strings.EqualFold(n.GetStatus(), status) {
			filtered = append(filtered, n)
		}
	}
	return filtered
}

// relativeTime returns a human-readable relative time string from a Unix timestamp.
func relativeTime(unix int64) string {
	if unix == 0 {
		return "never"
	}
	d := time.Since(time.Unix(unix, 0))
	if d < 0 {
		return "just now"
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

// truncate shortens s to maxLen characters, appending "..." if truncated.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

func formatDuration(seconds int64) string {
	if seconds <= 0 {
		return "0m"
	}
	d := time.Duration(seconds) * time.Second
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	mins := int(d.Minutes()) % 60
	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm", days, hours, mins)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, mins)
	}
	return fmt.Sprintf("%dm", mins)
}

func printJSON(cmd *cobra.Command, v any) error {
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
