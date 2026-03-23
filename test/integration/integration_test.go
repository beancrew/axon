package integration_test

import (
	"context"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	controlpb "github.com/garysng/axon/gen/proto/control"
	managementpb "github.com/garysng/axon/gen/proto/management"
	operationspb "github.com/garysng/axon/gen/proto/operations"
	"github.com/garysng/axon/internal/server"
	"github.com/garysng/axon/internal/server/registry"
	"github.com/garysng/axon/test/integration/testharness"
)

// ── Agent lifecycle tests ──────────────────────────────────────────────────

func TestIntegration_AgentConnectAndRegister(t *testing.T) {
	h := testharness.NewHarness(t)

	nodeID := h.AgentNodeID()
	if nodeID == "" {
		t.Fatal("expected non-empty node ID")
	}

	entry, ok := h.Registry().Lookup(nodeID)
	if !ok {
		t.Fatalf("node %s not found in registry", nodeID)
	}
	if entry.Status != registry.StatusOnline {
		t.Errorf("status = %q, want %q", entry.Status, registry.StatusOnline)
	}
	if entry.NodeName != "integration-agent" {
		t.Errorf("NodeName = %q, want %q", entry.NodeName, "integration-agent")
	}
}

func TestIntegration_AgentHeartbeat(t *testing.T) {
	// The server protocol communicates heartbeat intervals in whole seconds,
	// so the minimum testable interval is 1s.
	h := testharness.NewHarness(t,
		testharness.WithHeartbeatInterval(1*time.Second),
		testharness.WithHeartbeatTimeout(10*time.Second),
	)

	nodeID := h.AgentNodeID()

	// Record the initial heartbeat time.
	entry, ok := h.Registry().Lookup(nodeID)
	if !ok {
		t.Fatalf("node %s not found in registry", nodeID)
	}
	initialHB := entry.LastHeartbeat

	// Wait for at least one heartbeat cycle (1s interval + margin).
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		entry, ok = h.Registry().Lookup(nodeID)
		if ok && entry.LastHeartbeat.After(initialHB) {
			return // success: heartbeat was updated
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Error("heartbeat was not updated within 5s")
}

func TestIntegration_AgentDisconnectReconnect(t *testing.T) {
	h := testharness.NewHarness(t,
		testharness.WithAgentNodeName("disc-reconnect-agent"),
	)

	nodeID := h.AgentNodeID()

	// Verify online.
	entry, ok := h.Registry().Lookup(nodeID)
	if !ok || entry.Status != registry.StatusOnline {
		t.Fatalf("expected node online, got status=%q ok=%v", entry.Status, ok)
	}

	// Stop the agent to simulate disconnect.
	h.StopAgent()

	// Wait for node to go offline.
	h.WaitForNodeStatus(nodeID, registry.StatusOffline)

	// Connect a new agent with the same name — it gets a new node ID.
	_, newNodeID := h.ConnectAgent("disc-reconnect-agent")
	if newNodeID == "" {
		t.Fatal("expected non-empty node ID after reconnect")
	}

	newEntry, ok := h.Registry().Lookup(newNodeID)
	if !ok {
		t.Fatalf("reconnected node %s not found", newNodeID)
	}
	if newEntry.Status != registry.StatusOnline {
		t.Errorf("reconnected status = %q, want %q", newEntry.Status, registry.StatusOnline)
	}
}

// ── Management service tests ───────────────────────────────────────────────

func TestIntegration_ListNodes(t *testing.T) {
	h := testharness.NewHarness(t)

	conn := h.CLIConn()
	client := managementpb.NewManagementServiceClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := client.ListNodes(ctx, &managementpb.ListNodesRequest{})
	if err != nil {
		t.Fatalf("ListNodes: %v", err)
	}

	if len(resp.GetNodes()) == 0 {
		t.Fatal("expected at least one node in ListNodes response")
	}

	found := false
	for _, n := range resp.GetNodes() {
		if n.GetNodeId() == h.AgentNodeID() {
			found = true
			if n.GetStatus() != registry.StatusOnline {
				t.Errorf("node status = %q, want %q", n.GetStatus(), registry.StatusOnline)
			}
			if n.GetNodeName() != "integration-agent" {
				t.Errorf("node name = %q, want %q", n.GetNodeName(), "integration-agent")
			}
		}
	}
	if !found {
		t.Errorf("agent node %s not found in ListNodes response", h.AgentNodeID())
	}
}

func TestIntegration_GetNode(t *testing.T) {
	h := testharness.NewHarness(t)

	conn := h.CLIConn()
	client := managementpb.NewManagementServiceClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := client.GetNode(ctx, &managementpb.GetNodeRequest{
		NodeId: h.AgentNodeID(),
	})
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}

	summary := resp.GetSummary()
	if summary == nil {
		t.Fatal("expected non-nil summary")
	}
	if summary.GetNodeId() != h.AgentNodeID() {
		t.Errorf("NodeId = %q, want %q", summary.GetNodeId(), h.AgentNodeID())
	}
	if summary.GetNodeName() != "integration-agent" {
		t.Errorf("NodeName = %q, want %q", summary.GetNodeName(), "integration-agent")
	}
	if summary.GetStatus() != registry.StatusOnline {
		t.Errorf("Status = %q, want %q", summary.GetStatus(), registry.StatusOnline)
	}
}

func TestIntegration_RemoveNode(t *testing.T) {
	h := testharness.NewHarness(t)

	conn := h.CLIConn()
	client := managementpb.NewManagementServiceClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := client.RemoveNode(ctx, &managementpb.RemoveNodeRequest{
		NodeId: h.AgentNodeID(),
	})
	if err != nil {
		t.Fatalf("RemoveNode: %v", err)
	}
	if !resp.GetSuccess() {
		t.Errorf("RemoveNode success = false, error = %q", resp.GetError())
	}

	// Verify node is gone.
	_, ok := h.Registry().Lookup(h.AgentNodeID())
	if ok {
		t.Error("node still in registry after RemoveNode")
	}
}

// ── Login tests ────────────────────────────────────────────────────────────

func hashPassword(t *testing.T, password string) string {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("bcrypt hash: %v", err)
	}
	return string(hash)
}

func TestIntegration_Login(t *testing.T) {
	users := []server.UserEntry{
		{
			Username:     "admin",
			PasswordHash: hashPassword(t, "secret123"),
			NodeIDs:      []string{"*"},
		},
	}

	h := testharness.NewHarnessServerOnly(t, testharness.WithUsers(users))

	conn := h.UnauthConn()
	client := managementpb.NewManagementServiceClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := client.Login(ctx, &managementpb.LoginRequest{
		Username: "admin",
		Password: "secret123",
	})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if resp.GetToken() == "" {
		t.Errorf("expected non-empty token, got error: %s", resp.GetError())
	}
	if resp.GetExpiresAt() == 0 {
		t.Error("expected non-zero ExpiresAt")
	}
}

func TestIntegration_LoginInvalidCredentials(t *testing.T) {
	users := []server.UserEntry{
		{
			Username:     "admin",
			PasswordHash: hashPassword(t, "secret123"),
			NodeIDs:      []string{"*"},
		},
	}

	h := testharness.NewHarnessServerOnly(t, testharness.WithUsers(users))

	conn := h.UnauthConn()
	client := managementpb.NewManagementServiceClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := client.Login(ctx, &managementpb.LoginRequest{
		Username: "admin",
		Password: "wrongpassword",
	})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if resp.GetToken() != "" {
		t.Error("expected empty token for invalid credentials")
	}
	if resp.GetError() == "" {
		t.Error("expected non-empty error message for invalid credentials")
	}
}

// ── Task signal tests ──────────────────────────────────────────────────────

func TestIntegration_ExecTaskSignal(t *testing.T) {
	taskCh := make(chan testharness.TaskSignal, 1)
	h := testharness.NewHarnessServerOnly(t)
	_, nodeID := h.ConnectAgentWithHandler("exec-agent", func(taskID string, taskType controlpb.TaskType) {
		taskCh <- testharness.TaskSignal{TaskID: taskID, TaskType: taskType}
	})

	conn := h.CLIConn()
	client := operationspb.NewOperationsServiceClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := client.Exec(ctx, &operationspb.ExecRequest{
		NodeId:  nodeID,
		Command: "echo hello",
	})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	// Drain the response stream.
	for {
		_, err := stream.Recv()
		if err != nil {
			break
		}
	}

	select {
	case sig := <-taskCh:
		if sig.TaskType != controlpb.TaskType_TASK_EXEC {
			t.Errorf("task type = %v, want TASK_EXEC", sig.TaskType)
		}
		if sig.TaskID == "" {
			t.Error("expected non-empty task ID")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for task signal")
	}
}

func TestIntegration_ReadTaskSignal(t *testing.T) {
	taskCh := make(chan testharness.TaskSignal, 1)
	h := testharness.NewHarnessServerOnly(t)
	_, nodeID := h.ConnectAgentWithHandler("read-agent", func(taskID string, taskType controlpb.TaskType) {
		taskCh <- testharness.TaskSignal{TaskID: taskID, TaskType: taskType}
	})

	conn := h.CLIConn()
	client := operationspb.NewOperationsServiceClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := client.Read(ctx, &operationspb.ReadRequest{
		NodeId: nodeID,
		Path:   "/tmp/testfile",
	})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	for {
		_, err := stream.Recv()
		if err != nil {
			break
		}
	}

	select {
	case sig := <-taskCh:
		if sig.TaskType != controlpb.TaskType_TASK_READ {
			t.Errorf("task type = %v, want TASK_READ", sig.TaskType)
		}
		if sig.TaskID == "" {
			t.Error("expected non-empty task ID")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for task signal")
	}
}

func TestIntegration_WriteTaskSignal(t *testing.T) {
	taskCh := make(chan testharness.TaskSignal, 1)
	h := testharness.NewHarnessServerOnly(t)
	_, nodeID := h.ConnectAgentWithHandler("write-agent", func(taskID string, taskType controlpb.TaskType) {
		taskCh <- testharness.TaskSignal{TaskID: taskID, TaskType: taskType}
	})

	conn := h.CLIConn()
	client := operationspb.NewOperationsServiceClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := client.Write(ctx)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	// Send WriteHeader as first message.
	if err := stream.Send(&operationspb.WriteInput{
		Payload: &operationspb.WriteInput_Header{
			Header: &operationspb.WriteHeader{
				NodeId: nodeID,
				Path:   "/tmp/testfile",
			},
		},
	}); err != nil {
		t.Fatalf("Send WriteHeader: %v", err)
	}

	// Close and receive response.
	_, _ = stream.CloseAndRecv()

	select {
	case sig := <-taskCh:
		if sig.TaskType != controlpb.TaskType_TASK_WRITE {
			t.Errorf("task type = %v, want TASK_WRITE", sig.TaskType)
		}
		if sig.TaskID == "" {
			t.Error("expected non-empty task ID")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for task signal")
	}
}

func TestIntegration_ForwardTaskSignal(t *testing.T) {
	taskCh := make(chan testharness.TaskSignal, 1)
	h := testharness.NewHarnessServerOnly(t)
	_, nodeID := h.ConnectAgentWithHandler("forward-agent", func(taskID string, taskType controlpb.TaskType) {
		taskCh <- testharness.TaskSignal{TaskID: taskID, TaskType: taskType}
	})

	conn := h.CLIConn()
	client := operationspb.NewOperationsServiceClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := client.Forward(ctx)
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}

	// Send TunnelOpen as first message.
	if err := stream.Send(&operationspb.TunnelData{
		Open: &operationspb.TunnelOpen{
			NodeId:     nodeID,
			RemotePort: 8080,
		},
	}); err != nil {
		t.Fatalf("Send TunnelOpen: %v", err)
	}

	// Read response (server sends a close signal).
	_, _ = stream.Recv()

	select {
	case sig := <-taskCh:
		if sig.TaskType != controlpb.TaskType_TASK_FORWARD {
			t.Errorf("task type = %v, want TASK_FORWARD", sig.TaskType)
		}
		if sig.TaskID == "" {
			t.Error("expected non-empty task ID")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for task signal")
	}
}

// ── Router error tests ─────────────────────────────────────────────────────

func TestIntegration_RouterNodeNotFound(t *testing.T) {
	h := testharness.NewHarnessServerOnly(t)

	conn := h.CLIConn()
	client := operationspb.NewOperationsServiceClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := client.Exec(ctx, &operationspb.ExecRequest{
		NodeId:  "non-existent-node-id",
		Command: "echo hello",
	})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}

	_, err = stream.Recv()
	if err == nil {
		t.Fatal("expected error for non-existent node")
	}

	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected gRPC status error, got: %v", err)
	}
	if st.Code() != codes.NotFound {
		t.Errorf("status code = %v, want NotFound", st.Code())
	}
}

func TestIntegration_RouterNodeOffline(t *testing.T) {
	h := testharness.NewHarnessServerOnly(t)

	// Connect an agent, then stop it.
	_, nodeID := h.ConnectAgent("offline-agent")
	h.Registry().MarkOffline(nodeID)

	conn := h.CLIConn()
	client := operationspb.NewOperationsServiceClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := client.Exec(ctx, &operationspb.ExecRequest{
		NodeId:  nodeID,
		Command: "echo hello",
	})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}

	_, err = stream.Recv()
	if err == nil {
		t.Fatal("expected error for offline node")
	}

	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected gRPC status error, got: %v", err)
	}
	if st.Code() != codes.Unavailable {
		t.Errorf("status code = %v, want Unavailable", st.Code())
	}
}

func TestIntegration_UnauthorizedAccess(t *testing.T) {
	h := testharness.NewHarness(t)

	// Create a CLI connection that only has access to a different node.
	conn := h.CLIConnWithAccess("some-other-node-id")
	client := operationspb.NewOperationsServiceClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := client.Exec(ctx, &operationspb.ExecRequest{
		NodeId:  h.AgentNodeID(),
		Command: "echo hello",
	})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}

	_, err = stream.Recv()
	if err == nil {
		t.Fatal("expected error for unauthorized access")
	}

	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected gRPC status error, got: %v", err)
	}
	if st.Code() != codes.PermissionDenied {
		t.Errorf("status code = %v, want PermissionDenied", st.Code())
	}
}
