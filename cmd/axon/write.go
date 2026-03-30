package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	operationspb "github.com/beancrew/axon/gen/proto/operations"
)

func writeCmd() *cobra.Command {
	var fileMode int32

	cmd := &cobra.Command{
		Use:   "write <node> <path>",
		Short: "Write to a file on a remote node",
		Long:  "Writes stdin content to a file on the specified node. Data is streamed without loading into memory.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			nodeID := args[0]
			path := args[1]

			client, closer, err := dialOperations()
			if err != nil {
				return err
			}
			defer closer()

			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()

			stream, err := client.Write(ctx)
			if err != nil {
				return fmt.Errorf("write: %w", err)
			}

			// Send header first.
			if err := stream.Send(&operationspb.WriteInput{
				Payload: &operationspb.WriteInput_Header{
					Header: &operationspb.WriteHeader{
						NodeId: nodeID,
						Path:   path,
						Mode:   fileMode,
					},
				},
			}); err != nil {
				return fmt.Errorf("send header: %w", err)
			}

			// Stream stdin in chunks.
			buf := make([]byte, 32*1024) // 32KB chunks
			for {
				n, readErr := os.Stdin.Read(buf)
				if n > 0 {
					if err := stream.Send(&operationspb.WriteInput{
						Payload: &operationspb.WriteInput_Data{
							Data: buf[:n],
						},
					}); err != nil {
						return fmt.Errorf("send data: %w", err)
					}
				}
				if readErr == io.EOF {
					break
				}
				if readErr != nil {
					return fmt.Errorf("read stdin: %w", readErr)
				}
			}

			resp, err := stream.CloseAndRecv()
			if err != nil {
				return fmt.Errorf("write close: %w", err)
			}

			if !resp.GetSuccess() {
				return fmt.Errorf("write failed: %s", resp.GetError())
			}

			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Written %d bytes to %s\n", resp.GetBytesWritten(), path)
			return nil
		},
	}

	cmd.Flags().Int32Var(&fileMode, "mode", 0644, "file permissions")
	return cmd
}
