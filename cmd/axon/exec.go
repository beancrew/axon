package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	operationspb "github.com/garysng/axon/gen/proto/operations"
)

type exitError struct{ code int }

func (e *exitError) Error() string { return fmt.Sprintf("exit code %d", e.code) }

func execCmd() *cobra.Command {
	var timeout int32
	var envVars []string
	var workdir string

	cmd := &cobra.Command{
		Use:   "exec <node> <command>",
		Short: "Execute a command on a remote node",
		Long:  "Runs a command on the specified node. Stdout and stderr stream in real time. Exit code is forwarded.",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			nodeID := args[0]
			command := strings.Join(args[1:], " ")

			client, closer, err := dialOperations()
			if err != nil {
				return err
			}
			defer closer()

			// Build environment map from --env flags.
			envMap := make(map[string]string)
			for _, e := range envVars {
				k, v, ok := strings.Cut(e, "=")
				if !ok {
					return fmt.Errorf("invalid --env format %q, expected KEY=VALUE", e)
				}
				envMap[k] = v
			}

			// Create cancellable context for Ctrl+C handling.
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			go func() {
				<-sigCh
				cancel()
			}()

			stream, err := client.Exec(ctx, &operationspb.ExecRequest{
				NodeId:         nodeID,
				Command:        command,
				Env:            envMap,
				WorkingDir:     workdir,
				TimeoutSeconds: timeout,
			})
			if err != nil {
				return fmt.Errorf("exec: %w", err)
			}

			exitCode := 0
			for {
				msg, err := stream.Recv()
				if err == io.EOF {
					break
				}
				if err != nil {
					// Context cancelled (Ctrl+C) is expected.
					if ctx.Err() != nil {
						return nil
					}
					return fmt.Errorf("exec stream: %w", err)
				}

				switch p := msg.GetPayload().(type) {
				case *operationspb.ExecOutput_Stdout:
					_, _ = os.Stdout.Write(p.Stdout)
				case *operationspb.ExecOutput_Stderr:
					_, _ = os.Stderr.Write(p.Stderr)
				case *operationspb.ExecOutput_Exit:
					if p.Exit.GetError() != "" {
						return fmt.Errorf("remote error: %s", p.Exit.GetError())
					}
					exitCode = int(p.Exit.GetExitCode())
				}
			}

			if exitCode != 0 {
				return &exitError{code: exitCode}
			}
			return nil
		},
	}

	cmd.Flags().Int32Var(&timeout, "timeout", 0, "kill command after timeout seconds (0 = no timeout)")
	cmd.Flags().StringArrayVar(&envVars, "env", nil, "set environment variable (KEY=VALUE, repeatable)")
	cmd.Flags().StringVar(&workdir, "workdir", "", "set working directory on remote node")
	return cmd
}
