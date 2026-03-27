package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"

	managementpb "github.com/garysng/axon/gen/proto/management"
	"github.com/garysng/axon/internal/agent"
	"github.com/garysng/axon/pkg/config"
)

func joinCmd() *cobra.Command {
	var (
		flagName   string
		flagLabels []string
	)

	cmd := &cobra.Command{
		Use:   "join <server-addr> <join-token>",
		Short: "Enroll this node with an Axon server using a join token",
		Long: `Enroll this machine as an agent node.

Dials the server, validates the join token, receives a persistent agent JWT,
and saves the configuration to ~/.axon-agent/config.yaml. Then starts the
agent control-plane loop in the foreground.

If the node is already enrolled (node_id is present in config), this command
exits with an error. Use 'axon-agent start' to reconnect an enrolled node.

TLS is controlled via config (axon-agent config set tls_insecure true).
For non-TLS servers (default), no extra config is needed.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			serverAddr := args[0]
			joinToken := args[1]

			cfgPath := config.DefaultAgentConfigPath()

			// Load existing config for TLS settings (may not exist yet — defaults are fine).
			existingCfg, _ := config.LoadAgentConfig(cfgPath)

			// Refuse to re-enroll an already-enrolled node.
			if existingCfg != nil && existingCfg.NodeID != "" {
				return fmt.Errorf("node already enrolled (node_id=%s); use 'axon-agent start' to connect", existingCfg.NodeID)
			}

			// Collect system information for the enrollment request.
			info := agent.CollectNodeInfo()

			nodeName := flagName
			if nodeName == "" {
				nodeName = info.Hostname
			}

			// Build transport credentials from agent config.
			// Priority: ca_cert > tls_insecure > plaintext (default).
			var transportCreds grpc.DialOption
			switch {
			case existingCfg != nil && existingCfg.CACert != "":
				creds, err := credentials.NewClientTLSFromFile(existingCfg.CACert, "")
				if err != nil {
					return fmt.Errorf("load CA cert %q: %w", existingCfg.CACert, err)
				}
				transportCreds = grpc.WithTransportCredentials(creds)
			case existingCfg != nil && existingCfg.TLSInsecure:
				transportCreds = grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{InsecureSkipVerify: true})) //nolint:gosec
			default:
				transportCreds = grpc.WithTransportCredentials(insecure.NewCredentials())
			}

			conn, err := grpc.NewClient(serverAddr, transportCreds)
			if err != nil {
				return fmt.Errorf("dial %q: %w", serverAddr, err)
			}
			defer func() { _ = conn.Close() }()

			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()

			client := managementpb.NewManagementServiceClient(conn)
			resp, err := client.JoinAgent(ctx, &managementpb.JoinAgentRequest{
				JoinToken: joinToken,
				NodeName:  nodeName,
				Info:      info,
			})
			if err != nil {
				return fmt.Errorf("join: %w", err)
			}
			if !resp.Success {
				return fmt.Errorf("join failed: %s", resp.Error)
			}

			// Build and save the agent config.
			cfg := &config.AgentConfig{
				ServerAddr: serverAddr,
				Token:      resp.AgentToken,
				NodeID:     resp.NodeId,
				NodeName:   nodeName,
			}

			if len(flagLabels) > 0 {
				cfg.Labels = make(map[string]string)
				for _, kv := range flagLabels {
					parts := strings.SplitN(kv, "=", 2)
					if len(parts) != 2 {
						return fmt.Errorf("invalid label %q (expected key=value)", kv)
					}
					cfg.Labels[parts[0]] = parts[1]
				}
			}

			// If the server provided a CA certificate, write it to disk and
			// configure the agent to use it for subsequent TLS connections.
			if resp.CaCertPem != "" {
				caCertPath := filepath.Join(filepath.Dir(cfgPath), "ca.crt")
				if err := os.MkdirAll(filepath.Dir(caCertPath), 0700); err != nil {
					return fmt.Errorf("create config dir: %w", err)
				}
				if err := os.WriteFile(caCertPath, []byte(resp.CaCertPem), 0600); err != nil {
					return fmt.Errorf("write CA cert: %w", err)
				}
				cfg.CACert = caCertPath
				// Verify the CA cert is loadable before saving it.
				if _, err := credentials.NewClientTLSFromFile(caCertPath, ""); err != nil {
					return fmt.Errorf("load CA cert: %w", err)
				}
			}

			if err := config.SaveAgentConfig(cfgPath, cfg); err != nil {
				return fmt.Errorf("save config: %w", err)
			}

			out := cmd.OutOrStdout()
			_, _ = fmt.Fprintln(out, "Node enrolled successfully")
			_, _ = fmt.Fprintln(out)
			_, _ = fmt.Fprintf(out, "   Node ID:    %s\n", resp.NodeId)
			_, _ = fmt.Fprintf(out, "   Node Name:  %s\n", nodeName)
			_, _ = fmt.Fprintf(out, "   Server:     %s\n", serverAddr)
			_, _ = fmt.Fprintf(out, "   Config:     %s\n", cfgPath)
			_, _ = fmt.Fprintln(out)
			_, _ = fmt.Fprintln(out, "Start the agent:")
			_, _ = fmt.Fprintln(out, "   axon-agent start")
			return nil
		},
	}

	cmd.Flags().StringVar(&flagName, "name", "", "node name (default: hostname)")
	cmd.Flags().StringArrayVar(&flagLabels, "labels", nil, "label as key=value (repeatable)")
	return cmd
}
