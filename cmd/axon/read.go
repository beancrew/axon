package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	operationspb "github.com/beancrew/axon/gen/proto/operations"
)

func readCmd() *cobra.Command {
	var metaOnly bool
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "read <node> <path>",
		Short: "Read a file from a remote node",
		Long:  "Reads a file from the specified node. Content is written to stdout. Use --meta for metadata only.",
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

			stream, err := client.Read(ctx, &operationspb.ReadRequest{
				NodeId: nodeID,
				Path:   path,
			})
			if err != nil {
				return fmt.Errorf("read: %w", err)
			}

			for {
				msg, err := stream.Recv()
				if err == io.EOF {
					break
				}
				if err != nil {
					return fmt.Errorf("read stream: %w", err)
				}

				switch p := msg.GetPayload().(type) {
				case *operationspb.ReadOutput_Meta:
					if metaOnly || jsonOutput {
						return printReadMeta(cmd, p.Meta, path, jsonOutput)
					}
				case *operationspb.ReadOutput_Data:
					if !metaOnly {
						_, _ = os.Stdout.Write(p.Data)
					}
				case *operationspb.ReadOutput_Error:
					return fmt.Errorf("remote error: %s", p.Error)
				}
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&metaOnly, "meta", false, "print file metadata only")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output metadata as JSON")
	return cmd
}

// printReadMeta displays file metadata in table or JSON format.
func printReadMeta(cmd *cobra.Command, meta *operationspb.ReadMeta, path string, asJSON bool) error {
	if asJSON {
		data := map[string]any{
			"path":     path,
			"size":     meta.GetSize(),
			"mode":     fmt.Sprintf("%04o", meta.GetMode()),
			"modified": time.Unix(meta.GetModifiedAt(), 0).Format(time.RFC3339),
		}
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(data)
	}

	w := cmd.OutOrStdout()
	_, _ = fmt.Fprintf(w, "Path:     %s\n", path)
	_, _ = fmt.Fprintf(w, "Size:     %d bytes\n", meta.GetSize())
	_, _ = fmt.Fprintf(w, "Mode:     %04o\n", meta.GetMode())
	_, _ = fmt.Fprintf(w, "Modified: %s\n", time.Unix(meta.GetModifiedAt(), 0).Format(time.RFC3339))
	return nil
}
