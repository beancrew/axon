package agent

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"

	operationspb "github.com/garysng/axon/gen/proto/operations"
)

// readCollector collects ReadOutput messages.
type readCollector struct {
	mu      sync.Mutex
	outputs []*operationspb.ReadOutput
}

func (c *readCollector) send(out *operationspb.ReadOutput) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.outputs = append(c.outputs, out)
	return nil
}

func (c *readCollector) meta() *operationspb.ReadMeta {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, o := range c.outputs {
		if m := o.GetMeta(); m != nil {
			return m
		}
	}
	return nil
}

func (c *readCollector) data() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	var buf bytes.Buffer
	for _, o := range c.outputs {
		if d := o.GetData(); d != nil {
			buf.Write(d)
		}
	}
	return buf.Bytes()
}

func (c *readCollector) errMsg() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, o := range c.outputs {
		if e := o.GetError(); e != "" {
			return e
		}
	}
	return ""
}

// ── Read Tests ─────────────────────────────────────────────────────────────

func TestRead_SimpleFile(t *testing.T) {
	content := []byte("hello, axon!\n")
	path := writeTempFile(t, "read-test-*.txt", content, 0644)

	h := &FileIOHandler{}
	c := &readCollector{}

	h.HandleRead(&operationspb.ReadRequest{Path: path}, c.send)

	// Check meta.
	meta := c.meta()
	if meta == nil {
		t.Fatal("no ReadMeta received")
	}
	if meta.Size != int64(len(content)) {
		t.Errorf("meta.Size = %d, want %d", meta.Size, len(content))
	}
	if meta.Mode != 0644 {
		t.Errorf("meta.Mode = %04o, want 0644", meta.Mode)
	}

	// Check data.
	got := c.data()
	if !bytes.Equal(got, content) {
		t.Errorf("data = %q, want %q", got, content)
	}

	// No error.
	if e := c.errMsg(); e != "" {
		t.Errorf("unexpected error: %s", e)
	}
}

func TestRead_LargeFile(t *testing.T) {
	// Create a file larger than readChunkSize (32KB).
	content := bytes.Repeat([]byte("x"), 100*1024)
	path := writeTempFile(t, "read-large-*.bin", content, 0644)

	h := &FileIOHandler{}
	c := &readCollector{}

	h.HandleRead(&operationspb.ReadRequest{Path: path}, c.send)

	got := c.data()
	if !bytes.Equal(got, content) {
		t.Errorf("data length = %d, want %d", len(got), len(content))
	}
}

func TestRead_NotFound(t *testing.T) {
	h := &FileIOHandler{}
	c := &readCollector{}

	h.HandleRead(&operationspb.ReadRequest{Path: "/nonexistent/file.txt"}, c.send)

	e := c.errMsg()
	if e == "" {
		t.Fatal("expected error for nonexistent file")
	}
	if !contains(e, "not found") {
		t.Errorf("error = %q, expected to contain 'not found'", e)
	}
}

func TestRead_IsDirectory(t *testing.T) {
	h := &FileIOHandler{}
	c := &readCollector{}

	h.HandleRead(&operationspb.ReadRequest{Path: os.TempDir()}, c.send)

	e := c.errMsg()
	if e == "" {
		t.Fatal("expected error for directory")
	}
	if !contains(e, "is a directory") {
		t.Errorf("error = %q, expected to contain 'is a directory'", e)
	}
}

func TestRead_PermissionDenied(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("skipping as root")
	}

	path := writeTempFile(t, "read-noperm-*.txt", []byte("secret"), 0000)

	h := &FileIOHandler{}
	c := &readCollector{}

	h.HandleRead(&operationspb.ReadRequest{Path: path}, c.send)

	e := c.errMsg()
	if e == "" {
		t.Fatal("expected error for permission denied")
	}
	if !contains(e, "permission denied") {
		t.Errorf("error = %q, expected to contain 'permission denied'", e)
	}
}

// ── Write Tests ────────────────────────────────────────────────────────────

func TestWrite_SimpleFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "output.txt")
	content := []byte("written by axon")

	h := &FileIOHandler{}
	resp := h.HandleWrite(
		func() (*operationspb.WriteHeader, error) {
			return &operationspb.WriteHeader{Path: path, Mode: 0644}, nil
		},
		singleChunkRecv(content),
	)

	if !resp.Success {
		t.Fatalf("write failed: %s", resp.Error)
	}
	if resp.BytesWritten != int64(len(content)) {
		t.Errorf("BytesWritten = %d, want %d", resp.BytesWritten, len(content))
	}

	// Verify file content.
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("file content = %q, want %q", got, content)
	}

	// Verify permissions.
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0644 {
		t.Errorf("file mode = %04o, want 0644", info.Mode().Perm())
	}
}

func TestWrite_CreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a", "b", "c", "file.txt")
	content := []byte("nested")

	h := &FileIOHandler{}
	resp := h.HandleWrite(
		func() (*operationspb.WriteHeader, error) {
			return &operationspb.WriteHeader{Path: path, Mode: 0644}, nil
		},
		singleChunkRecv(content),
	)

	if !resp.Success {
		t.Fatalf("write failed: %s", resp.Error)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("content = %q, want %q", got, content)
	}
}

func TestWrite_DefaultMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "default-mode.txt")

	h := &FileIOHandler{}
	resp := h.HandleWrite(
		func() (*operationspb.WriteHeader, error) {
			return &operationspb.WriteHeader{Path: path, Mode: 0}, nil // 0 = use default
		},
		singleChunkRecv([]byte("data")),
	)

	if !resp.Success {
		t.Fatalf("write failed: %s", resp.Error)
	}

	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0644 {
		t.Errorf("file mode = %04o, want 0644 (default)", info.Mode().Perm())
	}
}

func TestWrite_MultipleChunks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "multi.txt")

	chunks := [][]byte{
		[]byte("chunk1-"),
		[]byte("chunk2-"),
		[]byte("chunk3"),
	}
	idx := 0

	h := &FileIOHandler{}
	resp := h.HandleWrite(
		func() (*operationspb.WriteHeader, error) {
			return &operationspb.WriteHeader{Path: path, Mode: 0644}, nil
		},
		func() ([]byte, error) {
			if idx >= len(chunks) {
				return nil, io.EOF
			}
			data := chunks[idx]
			idx++
			return data, nil
		},
	)

	if !resp.Success {
		t.Fatalf("write failed: %s", resp.Error)
	}

	expected := []byte("chunk1-chunk2-chunk3")
	got, _ := os.ReadFile(path)
	if !bytes.Equal(got, expected) {
		t.Errorf("content = %q, want %q", got, expected)
	}
	if resp.BytesWritten != int64(len(expected)) {
		t.Errorf("BytesWritten = %d, want %d", resp.BytesWritten, len(expected))
	}
}

func TestWrite_Overwrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "overwrite.txt")

	// Write initial content.
	_ = os.WriteFile(path, []byte("old content"), 0644)

	h := &FileIOHandler{}
	resp := h.HandleWrite(
		func() (*operationspb.WriteHeader, error) {
			return &operationspb.WriteHeader{Path: path, Mode: 0644}, nil
		},
		singleChunkRecv([]byte("new content")),
	)

	if !resp.Success {
		t.Fatalf("write failed: %s", resp.Error)
	}

	got, _ := os.ReadFile(path)
	if string(got) != "new content" {
		t.Errorf("content = %q, want %q", got, "new content")
	}
}

// ── Helpers ────────────────────────────────────────────────────────────────

func writeTempFile(t *testing.T, pattern string, content []byte, mode os.FileMode) string {
	t.Helper()
	f, err := os.CreateTemp("", pattern)
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	if _, err := f.Write(content); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := f.Chmod(mode); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	_ = f.Close()
	t.Cleanup(func() { _ = os.Remove(f.Name()) })
	return f.Name()
}

func singleChunkRecv(data []byte) func() ([]byte, error) {
	sent := false
	return func() ([]byte, error) {
		if sent {
			return nil, io.EOF
		}
		sent = true
		return data, nil
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && bytes.Contains([]byte(s), []byte(sub))
}
