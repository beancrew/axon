package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	operationspb "github.com/garysng/axon/gen/proto/operations"
)

func forwardCmd() *cobra.Command {
	var bindAddr string

	cmd := &cobra.Command{
		Use:   "forward <node> <local-port>:<remote-port>",
		Short: "Forward a remote port to localhost",
		Long:  "Listens on a local port and forwards TCP connections to a remote port on the specified node.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			nodeID := args[0]

			localPort, remotePort, err := parsePorts(args[1])
			if err != nil {
				return err
			}

			client, closer, err := dialOperations()
			if err != nil {
				return err
			}
			defer closer()

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			go func() {
				<-sigCh
				cancel()
			}()

			listenAddr := fmt.Sprintf("%s:%d", bindAddr, localPort)
			listener, err := net.Listen("tcp", listenAddr)
			if err != nil {
				return fmt.Errorf("listen on %s: %w", listenAddr, err)
			}
			defer func() { _ = listener.Close() }()

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Forwarding %s → %s:%d\nReady. Press Ctrl+C to stop.\n", listenAddr, nodeID, remotePort)

			// Close listener on context cancel.
			go func() {
				<-ctx.Done()
				_ = listener.Close()
			}()

			for {
				conn, err := listener.Accept()
				if err != nil {
					if ctx.Err() != nil {
						return nil // graceful shutdown
					}
					return fmt.Errorf("accept: %w", err)
				}
				go handleForwardConn(ctx, client, conn, nodeID, remotePort)
			}
		},
	}

	cmd.Flags().StringVar(&bindAddr, "bind", "127.0.0.1", "bind address")
	return cmd
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
