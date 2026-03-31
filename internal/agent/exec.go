package agent

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"sync"
	"syscall"
	"time"

	operationspb "github.com/beancrew/axon/gen/proto/operations"
)

const (
	// killGracePeriod is the time to wait after SIGTERM before sending SIGKILL.
	killGracePeriod = 5 * time.Second

	// streamBufSize is the buffer size for stdout/stderr readers.
	streamBufSize = 32 * 1024
)

// ExecHandler processes an ExecRequest by spawning a local process, streaming
// stdout/stderr back, and forwarding the exit code.
type ExecHandler struct{}

// Handle runs the command described by req and streams output via send.
// It respects ctx cancellation and req.TimeoutSeconds.
func (h *ExecHandler) Handle(ctx context.Context, req *operationspb.ExecRequest, send func(*operationspb.ExecOutput) error) {
	if req.TimeoutSeconds > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(req.TimeoutSeconds)*time.Second)
		defer cancel()
	}

	shell, flag := shellCommand()
	cmd := exec.CommandContext(ctx, shell, flag, req.Command)

	// Set working directory if specified.
	if req.WorkingDir != "" {
		cmd.Dir = req.WorkingDir
	}

	// Set environment variables (inherit current env + overrides).
	if len(req.Env) > 0 {
		cmd.Env = buildEnv(req.Env)
	}

	// Enable process group kill so child processes are cleaned up.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// Use cmd.Cancel to SIGTERM the process group on context cancellation,
	// and WaitDelay to escalate to SIGKILL after the grace period.
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		pgid, err := syscall.Getpgid(cmd.Process.Pid)
		if err != nil {
			return cmd.Process.Kill()
		}
		return syscall.Kill(-pgid, syscall.SIGTERM)
	}
	cmd.WaitDelay = killGracePeriod

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		sendError(send, fmt.Sprintf("stdout pipe: %v", err))
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		sendError(send, fmt.Sprintf("stderr pipe: %v", err))
		return
	}

	if err := cmd.Start(); err != nil {
		sendError(send, fmt.Sprintf("start: %v", err))
		return
	}

	// Stream stdout and stderr concurrently.
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		streamOutput(stdout, func(data []byte) error {
			return send(&operationspb.ExecOutput{
				Payload: &operationspb.ExecOutput_Stdout{Stdout: data},
			})
		})
	}()

	go func() {
		defer wg.Done()
		streamOutput(stderr, func(data []byte) error {
			return send(&operationspb.ExecOutput{
				Payload: &operationspb.ExecOutput_Stderr{Stderr: data},
			})
		})
	}()

	// Wait for all output to be consumed before calling cmd.Wait.
	wg.Wait()

	// Wait for process exit.
	exitCode := 0
	var exitErr string

	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			// Context cancelled or timed out — process group already killed
			// via cmd.Cancel / cmd.WaitDelay.
			exitCode = -1
			if ctx.Err() == context.DeadlineExceeded {
				exitErr = "timeout exceeded"
			} else {
				exitErr = "cancelled"
			}
		} else if exitError, ok := err.(*exec.ExitError); ok {
			exitCode = exitError.ExitCode()
		} else {
			exitCode = -1
			exitErr = err.Error()
		}
	}

	_ = send(&operationspb.ExecOutput{
		Payload: &operationspb.ExecOutput_Exit{
			Exit: &operationspb.ExecExit{
				ExitCode: int32(exitCode),
				Error:    exitErr,
			},
		},
	})
}

// streamOutput reads from r in chunks and calls send for each chunk.
func streamOutput(r io.Reader, send func([]byte) error) {
	reader := bufio.NewReaderSize(r, streamBufSize)
	buf := make([]byte, streamBufSize)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			// Copy the slice to avoid data races.
			data := make([]byte, n)
			copy(data, buf[:n])
			if sendErr := send(data); sendErr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

// sendError sends an ExecExit with exit code -1 and the given error message.
func sendError(send func(*operationspb.ExecOutput) error, msg string) {
	_ = send(&operationspb.ExecOutput{
		Payload: &operationspb.ExecOutput_Exit{
			Exit: &operationspb.ExecExit{
				ExitCode: -1,
				Error:    msg,
			},
		},
	})
}

// shellCommand returns the shell binary and flag for the current platform.
func shellCommand() (string, string) {
	if runtime.GOOS == "windows" {
		return "cmd", "/C"
	}
	return "/bin/sh", "-c"
}

// buildEnv creates an environment slice by combining the current environment
// with the provided overrides.
func buildEnv(overrides map[string]string) []string {
	// Start with current environment.
	env := append([]string{}, syscall.Environ()...)
	// Apply overrides (appended last; Go exec uses last-wins for duplicate keys).
	for k, v := range overrides {
		env = append(env, k+"="+v)
	}
	return env
}
