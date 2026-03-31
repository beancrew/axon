package agent

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	operationspb "github.com/beancrew/axon/gen/proto/operations"
)

// outputCollector collects ExecOutput messages for assertions.
type outputCollector struct {
	mu      sync.Mutex
	outputs []*operationspb.ExecOutput
}

func (c *outputCollector) send(out *operationspb.ExecOutput) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.outputs = append(c.outputs, out)
	return nil
}

func (c *outputCollector) stdout() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var b strings.Builder
	for _, o := range c.outputs {
		if s := o.GetStdout(); s != nil {
			b.Write(s)
		}
	}
	return b.String()
}

func (c *outputCollector) stderr() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var b strings.Builder
	for _, o := range c.outputs {
		if s := o.GetStderr(); s != nil {
			b.Write(s)
		}
	}
	return b.String()
}

func (c *outputCollector) exitCode() (int32, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, o := range c.outputs {
		if e := o.GetExit(); e != nil {
			return e.ExitCode, true
		}
	}
	return 0, false
}

func (c *outputCollector) exitError() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, o := range c.outputs {
		if e := o.GetExit(); e != nil {
			return e.Error
		}
	}
	return ""
}

// ── Tests ──────────────────────────────────────────────────────────────────

func TestExec_SimpleCommand(t *testing.T) {
	h := &ExecHandler{}
	c := &outputCollector{}

	h.Handle(context.Background(), &operationspb.ExecRequest{
		Command: "echo hello world",
	}, c.send)

	out := strings.TrimSpace(c.stdout())
	if out != "hello world" {
		t.Errorf("stdout = %q, want %q", out, "hello world")
	}

	code, ok := c.exitCode()
	if !ok {
		t.Fatal("no exit message received")
	}
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
}

func TestExec_StderrOutput(t *testing.T) {
	h := &ExecHandler{}
	c := &outputCollector{}

	h.Handle(context.Background(), &operationspb.ExecRequest{
		Command: "echo error >&2",
	}, c.send)

	errOut := strings.TrimSpace(c.stderr())
	if errOut != "error" {
		t.Errorf("stderr = %q, want %q", errOut, "error")
	}

	code, _ := c.exitCode()
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
}

func TestExec_NonZeroExit(t *testing.T) {
	h := &ExecHandler{}
	c := &outputCollector{}

	h.Handle(context.Background(), &operationspb.ExecRequest{
		Command: "exit 42",
	}, c.send)

	code, ok := c.exitCode()
	if !ok {
		t.Fatal("no exit message received")
	}
	if code != 42 {
		t.Errorf("exit code = %d, want 42", code)
	}
}

func TestExec_WorkingDir(t *testing.T) {
	h := &ExecHandler{}
	c := &outputCollector{}

	h.Handle(context.Background(), &operationspb.ExecRequest{
		Command:    "pwd",
		WorkingDir: "/tmp",
	}, c.send)

	// On macOS /tmp is a symlink to /private/tmp.
	out := strings.TrimSpace(c.stdout())
	if !strings.HasSuffix(out, "/tmp") {
		t.Errorf("stdout = %q, want to end with /tmp", out)
	}

	code, _ := c.exitCode()
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
}

func TestExec_EnvVars(t *testing.T) {
	h := &ExecHandler{}
	c := &outputCollector{}

	h.Handle(context.Background(), &operationspb.ExecRequest{
		Command: "echo $AXON_TEST_VAR",
		Env:     map[string]string{"AXON_TEST_VAR": "foobar"},
	}, c.send)

	out := strings.TrimSpace(c.stdout())
	if out != "foobar" {
		t.Errorf("stdout = %q, want %q", out, "foobar")
	}
}

func TestExec_Timeout(t *testing.T) {
	h := &ExecHandler{}
	c := &outputCollector{}

	start := time.Now()
	h.Handle(context.Background(), &operationspb.ExecRequest{
		Command:        "sleep 60",
		TimeoutSeconds: 1,
	}, c.send)
	elapsed := time.Since(start)

	if elapsed > 10*time.Second {
		t.Errorf("timeout took %v, expected ~1s", elapsed)
	}

	code, ok := c.exitCode()
	if !ok {
		t.Fatal("no exit message received")
	}
	if code == 0 {
		t.Error("expected non-zero exit code for timeout")
	}
}

func TestExec_Cancellation(t *testing.T) {
	h := &ExecHandler{}
	c := &outputCollector{}

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel after 500ms.
	go func() {
		time.Sleep(500 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	h.Handle(ctx, &operationspb.ExecRequest{
		Command: "sleep 60",
	}, c.send)
	elapsed := time.Since(start)

	if elapsed > 10*time.Second {
		t.Errorf("cancellation took %v, expected ~500ms", elapsed)
	}

	code, ok := c.exitCode()
	if !ok {
		t.Fatal("no exit message received")
	}
	if code == 0 {
		t.Error("expected non-zero exit code for cancellation")
	}
}

func TestExec_InvalidCommand(t *testing.T) {
	h := &ExecHandler{}
	c := &outputCollector{}

	h.Handle(context.Background(), &operationspb.ExecRequest{
		Command: "nonexistent_command_xyz_12345",
	}, c.send)

	code, ok := c.exitCode()
	if !ok {
		t.Fatal("no exit message received")
	}
	if code == 0 {
		t.Error("expected non-zero exit code for invalid command")
	}
}

func TestExec_LargeOutput(t *testing.T) {
	h := &ExecHandler{}
	c := &outputCollector{}

	// Generate ~100KB of output using yes + head.
	h.Handle(context.Background(), &operationspb.ExecRequest{
		Command: "yes 'abcdefghijklmnopqrstuvwxyz0123456789' | head -3000",
	}, c.send)

	out := c.stdout()
	if len(out) < 10000 {
		t.Errorf("stdout length = %d, expected > 10000 bytes", len(out))
	}

	code, _ := c.exitCode()
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
}

func TestExec_InvalidWorkingDir(t *testing.T) {
	h := &ExecHandler{}
	c := &outputCollector{}

	h.Handle(context.Background(), &operationspb.ExecRequest{
		Command:    "echo hello",
		WorkingDir: "/nonexistent_dir_xyz",
	}, c.send)

	code, ok := c.exitCode()
	if !ok {
		t.Fatal("no exit message received")
	}
	// Should fail because the working directory doesn't exist.
	exitErr := c.exitError()
	if code == 0 && exitErr == "" {
		t.Error("expected error for invalid working directory")
	}
}
