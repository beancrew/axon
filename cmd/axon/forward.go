package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"text/tabwriter"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	operationspb "github.com/beancrew/axon/gen/proto/operations"
)

func forwardCmd() *cobra.Command {
	var bindAddr string

	cmd := &cobra.Command{
		Use:   "forward",
		Short: "Manage port forwards",
		Long:  "Manage port forwards to remote nodes. Use subcommands or provide <node> <local:remote> as shorthand for 'create'.",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Backward compat: axon forward <node> <local>:<remote>
			if len(args) == 2 {
				return runForwardCreate(cmd.OutOrStdout(), args[0], args[1], bindAddr)
			}
			return fmt.Errorf("expected subcommand or <node> <local-port>:<remote-port>")
		},
	}

	cmd.Flags().StringVar(&bindAddr, "bind", "127.0.0.1", "bind address")
	cmd.AddCommand(
		forwardCreateCmd(),
		forwardListCmd(),
		forwardDeleteCmd(),
		forwardDaemonCmd(),
	)

	return cmd
}

func forwardCreateCmd() *cobra.Command {
	var bindAddr string

	cmd := &cobra.Command{
		Use:   "create <node> <local-port>:<remote-port>",
		Short: "Create a port forward (non-blocking, returns forward ID)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runForwardCreate(cmd.OutOrStdout(), args[0], args[1], bindAddr)
		},
	}

	cmd.Flags().StringVar(&bindAddr, "bind", "127.0.0.1", "bind address")
	return cmd
}

func forwardListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List active port forwards",
		RunE: func(cmd *cobra.Command, args []string) error {
			forwards, err := daemonList()
			if err != nil {
				if errors.Is(err, errDaemonNotRunning) {
					_, _ = fmt.Fprintln(cmd.OutOrStdout(), err.Error())
				} else {
					_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Error: %v\n", err)
				}
				return nil
			}
			if len(forwards) == 0 {
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No active forwards.")
				return nil
			}
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			_, _ = fmt.Fprintln(w, "ID\tNODE\tLOCAL\tREMOTE\tSTATUS\tCREATED")
			for _, f := range forwards {
				_, _ = fmt.Fprintf(w, "%s\t%s\t%d\t%d\t%s\t%s\n",
					f.ID, f.Node, f.LocalPort, f.RemotePort, f.Status, relativeTime(f.CreatedAt.Unix()))
			}
			return w.Flush()
		},
	}
}

func forwardDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <forward-id>",
		Short: "Delete a port forward",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := daemonDelete(args[0]); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Forward %s deleted\n", args[0])
			return nil
		},
	}
}

func forwardDaemonCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "daemon",
		Short:  "Start forward daemon (internal use)",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDaemon()
		},
	}

	// --foreground is accepted for the re-exec convention (ensureDaemon passes it)
	// but has no effect — the daemon always runs in foreground when invoked directly.
	cmd.Flags().Bool("foreground", false, "run in foreground (accepted but no-op)")
	return cmd
}

func runForwardCreate(out io.Writer, node, portSpec, bindAddr string) error {
	localPort, remotePort, err := parsePorts(portSpec)
	if err != nil {
		return err
	}

	id, err := daemonCreate(node, int(localPort), int(remotePort), bindAddr)
	if err != nil {
		return fmt.Errorf("create forward: %w", err)
	}

	_, _ = fmt.Fprintf(out, "Forward %s created: %s:%d → %s:%d\n", id, bindAddr, localPort, node, remotePort)
	return nil
}

// handleForwardConn handles a single TCP connection by creating a gRPC
// bidirectional stream and relaying data in both directions.
func handleForwardConn(ctx context.Context, client operationspb.OperationsServiceClient, conn net.Conn, nodeID string, remotePort int32) {
	stream, err := client.Forward(ctx)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "forward stream error: %v\n", err)
		return
	}

	connID := uuid.NewString()
	_, _ = fmt.Fprintf(os.Stderr, "[forward] new connection %s -> %s:%d (conn=%s)\n", conn.RemoteAddr(), nodeID, remotePort, connID)

	// Send open message.
	if err := stream.Send(&operationspb.TunnelData{
		ConnectionId: connID,
		Open: &operationspb.TunnelOpen{
			NodeId:     nodeID,
			RemotePort: remotePort,
		},
	}); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "forward open error: %v\n", err)
		return
	}

	var wg sync.WaitGroup

	// TCP → gRPC
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 32*1024)
		for {
			n, readErr := conn.Read(buf)
			if n > 0 {
				if err := stream.Send(&operationspb.TunnelData{
					ConnectionId: connID,
					Payload:      buf[:n],
				}); err != nil {
					return
				}
			}
			if readErr != nil {
				// Send close signal.
				_ = stream.Send(&operationspb.TunnelData{
					ConnectionId: connID,
					Close:        true,
				})
				_ = stream.CloseSend()
				return
			}
		}
	}()

	// gRPC → TCP
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() { _ = conn.Close() }()
		for {
			msg, err := stream.Recv()
			if err == io.EOF {
				return
			}
			if err != nil {
				return
			}
			if msg.GetClose() {
				return
			}
			if len(msg.GetPayload()) > 0 {
				_, _ = conn.Write(msg.GetPayload())
			}
		}
	}()

	wg.Wait()
}

// parsePorts parses a "local:remote" port spec.
func parsePorts(spec string) (int32, int32, error) {
	parts := strings.SplitN(spec, ":", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid port spec %q, expected local:remote", spec)
	}
	local, err := strconv.Atoi(parts[0])
	if err != nil || local < 1 || local > 65535 {
		return 0, 0, fmt.Errorf("invalid local port %q", parts[0])
	}
	remote, err := strconv.Atoi(parts[1])
	if err != nil || remote < 1 || remote > 65535 {
		return 0, 0, fmt.Errorf("invalid remote port %q", parts[1])
	}
	return int32(local), int32(remote), nil
}
