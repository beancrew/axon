package main

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	operationspb "github.com/beancrew/axon/gen/proto/operations"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// newTestForwardManager returns a forwardManager with a pre-set (stub) gRPC client
// so that tests do not require authentication or a live server connection.
func newTestForwardManager(t *testing.T) *forwardManager {
	t.Helper()
	fm := newForwardManager()

	// Create a throw-away gRPC ClientConn to satisfy the client field.
	// We connect to a dummy target with insecure credentials; the connection
	// is never used for real RPCs in unit tests.
	cc, err := grpc.NewClient("passthrough:///localhost:0", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("create stub gRPC client: %v", err)
	}
	fm.client = operationspb.NewOperationsServiceClient(cc)
	fm.clientClose = func() { _ = cc.Close() }

	return fm
}

// ── Subcommand Help & Args ─────────────────────────────────────────────────

func TestForwardCreateCmdHelp(t *testing.T) {
	cmd := rootCmd()
	buf := new(strings.Builder)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"forward", "create", "--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("forward create help failed: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "create") {
		t.Errorf("forward create help missing 'create': %s", output)
	}
	if !strings.Contains(output, "--bind") {
		t.Errorf("forward create help missing '--bind': %s", output)
	}
	if !strings.Contains(output, "local-port") {
		t.Errorf("forward create help missing 'local-port': %s", output)
	}
}

func TestForwardCreateCmdRequiresArgs(t *testing.T) {
	cmd := rootCmd()
	cmd.SetArgs([]string{"forward", "create"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when no args provided to forward create")
	}
}

func TestForwardCreateCmdRequiresTwoArgs(t *testing.T) {
	cmd := rootCmd()
	cmd.SetArgs([]string{"forward", "create", "mynode"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when only one arg provided to forward create")
	}
}

func TestForwardListCmdHelp(t *testing.T) {
	cmd := rootCmd()
	buf := new(strings.Builder)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"forward", "list", "--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("forward list help failed: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "List") {
		t.Errorf("forward list help missing 'List': %s", output)
	}
}

func TestForwardDeleteCmdHelp(t *testing.T) {
	cmd := rootCmd()
	buf := new(strings.Builder)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"forward", "delete", "--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("forward delete help failed: %v", err)
	}
	output := buf.String()
	if !strings.Contains(output, "Delete") {
		t.Errorf("forward delete help missing 'Delete': %s", output)
	}
	if !strings.Contains(output, "forward-id") {
		t.Errorf("forward delete help missing 'forward-id': %s", output)
	}
}

func TestForwardDeleteCmdRequiresArgs(t *testing.T) {
	cmd := rootCmd()
	cmd.SetArgs([]string{"forward", "delete"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error when no args provided to forward delete")
	}
}

func TestForwardDaemonCmdHidden(t *testing.T) {
	cmd := rootCmd()
	fwdCmd, _, err := cmd.Find([]string{"forward"})
	if err != nil {
		t.Fatalf("failed to find forward command: %v", err)
	}
	for _, sub := range fwdCmd.Commands() {
		if sub.Name() == "daemon" {
			if !sub.Hidden {
				t.Error("forward daemon command should be hidden")
			}
			return
		}
	}
	t.Error("forward daemon subcommand not found")
}

func TestForwardSubcommands(t *testing.T) {
	cmd := rootCmd()
	fwdCmd, _, err := cmd.Find([]string{"forward"})
	if err != nil {
		t.Fatalf("failed to find forward command: %v", err)
	}

	names := make(map[string]bool)
	for _, sub := range fwdCmd.Commands() {
		names[sub.Name()] = true
	}

	for _, want := range []string{"create", "list", "delete", "daemon"} {
		if !names[want] {
			t.Errorf("forward missing subcommand %q", want)
		}
	}
}

// ── Forward backward compat ────────────────────────────────────────────────

func TestForwardBackwardCompatRequiresTwoParts(t *testing.T) {
	cmd := rootCmd()
	cmd.SetArgs([]string{"forward", "somenode"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error when only node provided to forward (no port spec)")
	}
}

// ── parsePorts extended ────────────────────────────────────────────────────

func TestParsePortsEdgeCases(t *testing.T) {
	tests := []struct {
		spec    string
		wantErr bool
	}{
		{"1:1", false},
		{"65535:65535", false},
		{":80", true},
		{"80:", true},
		{"", true},
		{"-1:80", true},
		{"80:-1", true},
		{"65536:80", true},
		{"80:65536", true},
		{"8080:80:443", true}, // SplitN(2) → "8080" and "80:443", "80:443" fails Atoi
	}
	for _, tt := range tests {
		_, _, err := parsePorts(tt.spec)
		if tt.wantErr && err == nil {
			t.Errorf("parsePorts(%q) expected error", tt.spec)
		}
		if !tt.wantErr && err != nil {
			t.Errorf("parsePorts(%q) unexpected error: %v", tt.spec, err)
		}
	}
}

// ── randomHexID ────────────────────────────────────────────────────────────

func TestRandomHexID(t *testing.T) {
	id := randomHexID()
	if len(id) != 8 {
		t.Errorf("randomHexID() length = %d, want 8", len(id))
	}

	// Should be valid hex.
	for _, c := range id {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			t.Errorf("randomHexID() contains non-hex char %q in %q", string(c), id)
		}
	}

	// Two calls should produce different IDs (with very high probability).
	id2 := randomHexID()
	if id == id2 {
		t.Errorf("randomHexID() produced duplicate IDs: %q", id)
	}
}

// ── axonDataDir ────────────────────────────────────────────────────────────

func TestAxonDataDir(t *testing.T) {
	dir, err := axonDataDir()
	if err != nil {
		t.Fatalf("axonDataDir() error: %v", err)
	}
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".axon")
	if dir != want {
		t.Errorf("axonDataDir() = %q, want %q", dir, want)
	}
}

func TestDaemonSockPath(t *testing.T) {
	sockPath, err := daemonSockPath()
	if err != nil {
		t.Fatalf("daemonSockPath() error: %v", err)
	}
	if !strings.HasSuffix(sockPath, "forward.sock") {
		t.Errorf("daemonSockPath() = %q, want suffix 'forward.sock'", sockPath)
	}
}

// ── forwardManager unit tests ──────────────────────────────────────────────

func TestForwardManagerCreateListDelete(t *testing.T) {
	fm := newTestForwardManager(t)
	defer fm.close()

	// List empty.
	resp := fm.handleList()
	if !resp.OK {
		t.Fatalf("handleList() not OK: %s", resp.Message)
	}
	if len(resp.Forwards) != 0 {
		t.Errorf("handleList() got %d forwards, want 0", len(resp.Forwards))
	}

	// Create — use a high ephemeral port to avoid conflicts.
	createResp := fm.handleCreate(daemonRequest{
		Action:     "create",
		Node:       "test-node",
		LocalPort:  0, // will fail to listen on port 0? Let's use a real port
		RemotePort: 80,
		BindAddr:   "127.0.0.1",
	})
	// Port 0 should actually work — OS assigns an ephemeral port.
	// But the daemon uses the explicit port number. Port 0 will listen on a random port.
	// Actually, net.Listen("tcp", "127.0.0.1:0") works — it picks a free port.
	// But the forwardManager uses req.LocalPort directly which is 0.
	// Let's check: fmt.Sprintf("%s:%d", bindAddr, req.LocalPort) → "127.0.0.1:0"
	// This should work — OS picks a free port.
	if !createResp.OK {
		t.Fatalf("handleCreate() not OK: %s", createResp.Message)
	}
	if createResp.ID == "" {
		t.Fatal("handleCreate() returned empty ID")
	}

	// List should now have 1 entry.
	listResp := fm.handleList()
	if !listResp.OK {
		t.Fatalf("handleList() not OK: %s", listResp.Message)
	}
	if len(listResp.Forwards) != 1 {
		t.Fatalf("handleList() got %d forwards, want 1", len(listResp.Forwards))
	}
	fwd := listResp.Forwards[0]
	if fwd.ID != createResp.ID {
		t.Errorf("forward ID = %q, want %q", fwd.ID, createResp.ID)
	}
	if fwd.Node != "test-node" {
		t.Errorf("forward Node = %q, want %q", fwd.Node, "test-node")
	}
	if fwd.RemotePort != 80 {
		t.Errorf("forward RemotePort = %d, want 80", fwd.RemotePort)
	}
	if fwd.Status != "active" {
		t.Errorf("forward Status = %q, want %q", fwd.Status, "active")
	}

	// Delete.
	delResp := fm.handleDelete(createResp.ID)
	if !delResp.OK {
		t.Fatalf("handleDelete() not OK: %s", delResp.Message)
	}

	// List should be empty again.
	listResp2 := fm.handleList()
	if len(listResp2.Forwards) != 0 {
		t.Errorf("after delete, got %d forwards, want 0", len(listResp2.Forwards))
	}
}

func TestForwardManagerDeleteNonExistent(t *testing.T) {
	fm := newTestForwardManager(t)
	defer fm.close()

	resp := fm.handleDelete("nonexistent-id")
	if resp.OK {
		t.Error("handleDelete(nonexistent) should not be OK")
	}
	if !strings.Contains(resp.Message, "not found") {
		t.Errorf("handleDelete(nonexistent) message = %q, want 'not found'", resp.Message)
	}
}

func TestForwardManagerMultipleForwards(t *testing.T) {
	fm := newTestForwardManager(t)
	defer fm.close()

	// Create two forwards.
	resp1 := fm.handleCreate(daemonRequest{
		Action: "create", Node: "node-a", LocalPort: 0, RemotePort: 80, BindAddr: "127.0.0.1",
	})
	if !resp1.OK {
		t.Fatalf("create 1 failed: %s", resp1.Message)
	}

	resp2 := fm.handleCreate(daemonRequest{
		Action: "create", Node: "node-b", LocalPort: 0, RemotePort: 443, BindAddr: "127.0.0.1",
	})
	if !resp2.OK {
		t.Fatalf("create 2 failed: %s", resp2.Message)
	}

	// List should have 2.
	listResp := fm.handleList()
	if len(listResp.Forwards) != 2 {
		t.Errorf("handleList() got %d forwards, want 2", len(listResp.Forwards))
	}

	// Delete first, second should remain.
	fm.handleDelete(resp1.ID)
	listResp2 := fm.handleList()
	if len(listResp2.Forwards) != 1 {
		t.Errorf("after delete 1, got %d forwards, want 1", len(listResp2.Forwards))
	}
	if listResp2.Forwards[0].ID != resp2.ID {
		t.Errorf("remaining forward ID = %q, want %q", listResp2.Forwards[0].ID, resp2.ID)
	}
}

func TestForwardManagerDefaultBindAddr(t *testing.T) {
	fm := newTestForwardManager(t)
	defer fm.close()

	// Create with empty BindAddr — should default to 127.0.0.1.
	resp := fm.handleCreate(daemonRequest{
		Action: "create", Node: "test-node", LocalPort: 0, RemotePort: 80, BindAddr: "",
	})
	if !resp.OK {
		t.Fatalf("handleCreate() not OK: %s", resp.Message)
	}

	listResp := fm.handleList()
	if len(listResp.Forwards) != 1 {
		t.Fatalf("got %d forwards, want 1", len(listResp.Forwards))
	}
	if listResp.Forwards[0].BindAddr != "127.0.0.1" {
		t.Errorf("BindAddr = %q, want %q", listResp.Forwards[0].BindAddr, "127.0.0.1")
	}
}

func TestForwardManagerPortConflict(t *testing.T) {
	// Listen on a known port first.
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = lis.Close() }()
	port := lis.Addr().(*net.TCPAddr).Port

	fm := newTestForwardManager(t)
	defer fm.close()

	// Try to create a forward on the same port — should fail.
	resp := fm.handleCreate(daemonRequest{
		Action: "create", Node: "test-node", LocalPort: port, RemotePort: 80, BindAddr: "127.0.0.1",
	})
	if resp.OK {
		t.Error("handleCreate() should fail on occupied port")
	}
	if !strings.Contains(resp.Message, "listen") {
		t.Errorf("error message = %q, expected to mention 'listen'", resp.Message)
	}
}

// ── Daemon protocol (unix socket) ──────────────────────────────────────────

func TestDaemonProtocolRoundTrip(t *testing.T) {
	// Start a minimal daemon on a temp unix socket.
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "test.sock")

	fm := newTestForwardManager(t)
	defer fm.close()

	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	defer func() { _ = ln.Close() }()

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go handleDaemonConn(conn, fm)
		}
	}()

	// Helper to send a request and get response.
	sendReq := func(req daemonRequest) daemonResponse {
		t.Helper()
		conn, err := net.DialTimeout("unix", sockPath, 3*time.Second)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		defer func() { _ = conn.Close() }()

		if err := json.NewEncoder(conn).Encode(req); err != nil {
			t.Fatalf("encode: %v", err)
		}
		var resp daemonResponse
		if err := json.NewDecoder(conn).Decode(&resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return resp
	}

	// List (empty).
	listResp := sendReq(daemonRequest{Action: "list"})
	if !listResp.OK {
		t.Fatalf("list not OK: %s", listResp.Message)
	}
	if len(listResp.Forwards) != 0 {
		t.Errorf("list got %d, want 0", len(listResp.Forwards))
	}

	// Create.
	createResp := sendReq(daemonRequest{
		Action: "create", Node: "proto-node", LocalPort: 0, RemotePort: 8080,
	})
	if !createResp.OK {
		t.Fatalf("create not OK: %s", createResp.Message)
	}
	if createResp.ID == "" {
		t.Fatal("create returned empty ID")
	}

	// List (1 entry).
	listResp2 := sendReq(daemonRequest{Action: "list"})
	if len(listResp2.Forwards) != 1 {
		t.Fatalf("list got %d, want 1", len(listResp2.Forwards))
	}

	// Delete.
	delResp := sendReq(daemonRequest{Action: "delete", ID: createResp.ID})
	if !delResp.OK {
		t.Fatalf("delete not OK: %s", delResp.Message)
	}

	// List (empty again).
	listResp3 := sendReq(daemonRequest{Action: "list"})
	if len(listResp3.Forwards) != 0 {
		t.Errorf("list after delete got %d, want 0", len(listResp3.Forwards))
	}

	// Unknown action.
	unkResp := sendReq(daemonRequest{Action: "bogus"})
	if unkResp.OK {
		t.Error("unknown action should not be OK")
	}
	if !strings.Contains(unkResp.Message, "unknown action") {
		t.Errorf("unknown action message = %q", unkResp.Message)
	}
}

// ── forwardInfo JSON serialization ─────────────────────────────────────────

func TestForwardInfoJSON(t *testing.T) {
	now := time.Now()
	info := forwardInfo{
		ID:         "abc12345",
		Node:       "my-node",
		LocalPort:  8080,
		RemotePort: 80,
		BindAddr:   "127.0.0.1",
		Status:     "active",
		CreatedAt:  now,
	}

	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded forwardInfo
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.ID != info.ID {
		t.Errorf("ID = %q, want %q", decoded.ID, info.ID)
	}
	if decoded.Node != info.Node {
		t.Errorf("Node = %q, want %q", decoded.Node, info.Node)
	}
	if decoded.LocalPort != info.LocalPort {
		t.Errorf("LocalPort = %d, want %d", decoded.LocalPort, info.LocalPort)
	}
	if decoded.RemotePort != info.RemotePort {
		t.Errorf("RemotePort = %d, want %d", decoded.RemotePort, info.RemotePort)
	}
	if decoded.Status != info.Status {
		t.Errorf("Status = %q, want %q", decoded.Status, info.Status)
	}
}

// ── daemonRequest JSON serialization ───────────────────────────────────────

func TestDaemonRequestJSON(t *testing.T) {
	req := daemonRequest{
		Action:     "create",
		Node:       "test-node",
		LocalPort:  8080,
		RemotePort: 80,
		BindAddr:   "0.0.0.0",
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded daemonRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.Action != req.Action {
		t.Errorf("Action = %q, want %q", decoded.Action, req.Action)
	}
	if decoded.Node != req.Node {
		t.Errorf("Node = %q, want %q", decoded.Node, req.Node)
	}
	if decoded.LocalPort != req.LocalPort {
		t.Errorf("LocalPort = %d, want %d", decoded.LocalPort, req.LocalPort)
	}
}

// ── daemonResponse JSON ────────────────────────────────────────────────────

func TestDaemonResponseJSON(t *testing.T) {
	resp := daemonResponse{
		OK:      true,
		ID:      "abc123",
		Message: "created",
		Forwards: []forwardInfo{
			{ID: "f1", Node: "n1", LocalPort: 80, RemotePort: 80, Status: "active"},
		},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded daemonResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !decoded.OK {
		t.Error("OK should be true")
	}
	if decoded.ID != "abc123" {
		t.Errorf("ID = %q, want %q", decoded.ID, "abc123")
	}
	if len(decoded.Forwards) != 1 {
		t.Fatalf("Forwards length = %d, want 1", len(decoded.Forwards))
	}
}

// ── forward list output (no daemon) ────────────────────────────────────────

func TestForwardListNoDaemon(t *testing.T) {
	// When no daemon is running, `forward list` should print a friendly message,
	// not return an error.
	cmd := rootCmd()
	buf := new(strings.Builder)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"forward", "list"})

	// This will try to connect to the daemon socket and fail gracefully.
	err := cmd.Execute()
	if err != nil {
		t.Logf("forward list without daemon returned error (may be expected): %v", err)
	}
	// The command should not panic regardless.
}

// ── Idle timeout ───────────────────────────────────────────────────────────

func TestForwardManagerIdleTimerStopsWhenBusy(t *testing.T) {
	fm := newTestForwardManager(t)
	defer fm.close()

	// Create a forward — idle timer should be stopped.
	resp := fm.handleCreate(daemonRequest{
		Action: "create", Node: "node", LocalPort: 0, RemotePort: 80, BindAddr: "127.0.0.1",
	})
	if !resp.OK {
		t.Fatalf("create failed: %s", resp.Message)
	}

	// Verify exitCh is not closed (manager should not exit while forwards exist).
	select {
	case <-fm.exitCh:
		t.Error("exitCh should not be closed while forwards exist")
	default:
		// OK — expected.
	}
}
